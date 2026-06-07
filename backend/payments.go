package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// paymentVerifier periodically checks pending orders against the chain and
// confirms (decrementing stock) or fails them. A sweep loop — rather than a
// per-request goroutine — survives pod restarts: pending orders are re-checked
// whenever the process is up. All persistence goes through Store so the same
// loop works on Postgres or MongoDB.
type paymentVerifier struct {
	store    Store
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

func (p *paymentVerifier) sweep(ctx context.Context) {
	pending, err := p.store.ListPendingOrders(ctx)
	if err != nil {
		slog.Error("sweep query", "err", err)
		return
	}
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
			if err := p.store.ConfirmOrder(ctx, o.id, o.itemID, o.qty); err != nil {
				slog.Error("confirm order", "order", o.id, "err", err)
			} else {
				slog.Info("order confirmed", "order", o.id, "tx", o.txHash)
			}
		case statusFailed:
			if err := p.store.FailOrder(ctx, o.id); err != nil {
				slog.Error("fail order", "order", o.id, "err", err)
			}
		}
	}
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
