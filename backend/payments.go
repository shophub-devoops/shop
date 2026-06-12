package main

import (
	"context"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// pendingOrderTTL is how long an order without a payment transaction may hold
// its stock reservation. Buyers who abandon checkout (MetaMask closed, never
// signed) would otherwise reserve stock forever.
const pendingOrderTTL = 30 * time.Minute

// paymentVerifier periodically checks pending orders against the chain and
// confirms or fails them (stock is reserved at order creation; failing
// restores it). A sweep loop — rather than a per-request goroutine — survives
// pod restarts: pending orders are re-checked whenever the process is up. All
// persistence goes through Store so the same loop works on Postgres or
// MongoDB. v may be nil (no on-chain config): the sweep then only expires
// abandoned orders.
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
		// No tx hash: the buyer never completed payment. Expire the order after
		// the TTL so its stock reservation is released.
		if o.txHash == "" {
			if time.Since(o.createdAt) > pendingOrderTTL {
				if err := p.store.FailOrder(ctx, o.id, o.itemID, o.qty); err != nil {
					slog.Error("expire order", "order", o.id, "err", err)
				} else {
					slog.Info("order expired (no payment)", "order", o.id)
				}
			}
			continue
		}
		if p.v == nil {
			continue // no on-chain config — leave paid orders pending
		}
		minAmount, err := p.requiredForTx(ctx, o.txHash)
		if err != nil {
			slog.Error("amount sum", "order", o.id, "err", err)
			continue
		}
		st, err := p.v.verify(ctx, o.txHash, minAmount)
		if err != nil {
			slog.Warn("verify", "order", o.id, "err", err)
			continue
		}
		switch st {
		case statusConfirmed:
			if err := p.store.ConfirmOrder(ctx, o.id); err != nil {
				slog.Error("confirm order", "order", o.id, "err", err)
			} else {
				slog.Info("order confirmed", "order", o.id, "tx", o.txHash)
			}
		case statusFailed:
			if err := p.store.FailOrder(ctx, o.id, o.itemID, o.qty); err != nil {
				slog.Error("fail order", "order", o.id, "err", err)
			}
		}
	}
}

// requiredForTx sums the amounts of every non-failed order referencing the
// same transaction. One cart checkout legitimately records several orders
// sharing a tx (the buyer pays the cart total in a single transfer), so the
// transfer must cover their sum — and a replayed tx hash can never pay for
// more orders than the original transfer covered: an extra order pushes the
// sum past the on-chain amount and fails verification.
func (p *paymentVerifier) requiredForTx(ctx context.Context, txHash string) (*big.Int, error) {
	amounts, err := p.store.ListActiveAmountsForTx(ctx, txHash)
	if err != nil {
		return nil, err
	}
	total := new(big.Int)
	for _, a := range amounts {
		v, err := toBaseUnits(a, p.decimals)
		if err != nil {
			return nil, err
		}
		total.Add(total, v)
	}
	return total, nil
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
