package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// paymentVerifier periodically checks pending orders against the chain and
// confirms (decrementing stock) or fails them. A sweep loop — rather than a
// per-request goroutine — survives pod restarts: pending orders are re-checked
// whenever the process is up.
type paymentVerifier struct {
	pool     *pgxpool.Pool
	v        *verifier
	decimals int
}

func (p *paymentVerifier) run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	slog.Info("payment verifier started")
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.sweep(ctx)
		}
	}
}

type pendingOrder struct {
	id, txHash, amount, itemID string
	qty                        int
}

func (p *paymentVerifier) sweep(ctx context.Context) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, tx_hash, amount_usdt::text, COALESCE(item_id, ''), COALESCE(item_quantity, 0)
		 FROM orders WHERE status = 'pending' AND tx_hash IS NOT NULL`)
	if err != nil {
		slog.Error("sweep query", "err", err)
		return
	}
	var pending []pendingOrder
	for rows.Next() {
		var o pendingOrder
		if err := rows.Scan(&o.id, &o.txHash, &o.amount, &o.itemID, &o.qty); err != nil {
			slog.Error("sweep scan", "err", err)
			rows.Close()
			return
		}
		pending = append(pending, o)
	}
	rows.Close()

	for _, o := range pending {
		minAmount, err := toBaseUnits(o.amount, p.decimals)
		if err != nil {
			slog.Error("amount parse", "order", o.id, "err", err)
			continue
		}
		st, err := p.v.verify(ctx, o.txHash, minAmount)
		if err != nil {
			slog.Warn("verify", "order", o.id, "err", err)
			continue
		}
		switch st {
		case statusConfirmed:
			if err := p.confirm(ctx, o.id, o.itemID, o.qty); err != nil {
				slog.Error("confirm order", "order", o.id, "err", err)
			} else {
				slog.Info("order confirmed", "order", o.id, "tx", o.txHash)
			}
		case statusFailed:
			if _, err := p.pool.Exec(ctx, `UPDATE orders SET status = 'failed' WHERE id = $1`, o.id); err != nil {
				slog.Error("fail order", "order", o.id, "err", err)
			}
		}
	}
}

// confirm marks the order confirmed and decrements stock in one transaction.
// It first claims the order with a conditional UPDATE: only the transaction
// that flips it out of 'pending' proceeds to decrement stock, so when several
// replicas sweep the same order concurrently exactly one of them adjusts stock.
func (p *paymentVerifier) confirm(ctx context.Context, orderID, itemID string, qty int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'confirmed', verified_at = now()
		 WHERE id = $1 AND status = 'pending'`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Another sweep/replica already handled this order.
		return tx.Commit(ctx)
	}

	var stock int
	if err := tx.QueryRow(ctx, `SELECT stock FROM items WHERE id = $1 FOR UPDATE`, itemID).Scan(&stock); err != nil {
		return err
	}
	if stock < qty {
		// Sold out between order and confirmation — fail rather than oversell.
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'failed' WHERE id = $1`, orderID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE items SET stock = stock - $1 WHERE id = $2`, qty, itemID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// shopInfo exposes the on-chain payment parameters the frontend needs to build
// the ERC-20 transfer.
func shopInfo(cfg config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"wallet_address": cfg.WalletAddress,
			"token_contract": cfg.TokenContract,
			"token_decimals": cfg.TokenDecimals,
			"chain_id":       11155111, // Sepolia
		})
	}
}
