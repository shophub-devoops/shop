package payment

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/shophub-devoops/shop/backend/internal/notify"
	"github.com/shophub-devoops/shop/backend/internal/store"
)

// pendingOrderTTL is how long an order without a payment transaction may hold
// its stock reservation. Buyers who abandon checkout (MetaMask closed, never
// signed) would otherwise reserve stock forever.
const pendingOrderTTL = 30 * time.Minute

// PaymentVerifier periodically checks pending orders against the chain and
// confirms or fails them (stock is reserved at order creation; failing
// restores it). A sweep loop — rather than a per-request goroutine — survives
// pod restarts: pending orders are re-checked whenever the process is up. All
// persistence goes through store.Store so the same loop works on Postgres or
// MongoDB. V may be nil (no on-chain config): the sweep then only expires
// abandoned orders.
type PaymentVerifier struct {
	Store    store.Store
	V        *Verifier
	Decimals int
	// Notify posts a message to the shop's Discord channel when an order is
	// confirmed. Nil when DISCORD_WEBHOOK_URL is not injected (shop without a
	// Discord channel) — notifications are then skipped. ConfirmOrder's claim
	// semantics guarantee the message is sent once even with multiple replicas.
	Notify *notify.Discord
}

func (p *PaymentVerifier) Run(ctx context.Context) {
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

func (p *PaymentVerifier) sweep(ctx context.Context) {
	pending, err := p.Store.ListPendingOrders(ctx)
	if err != nil {
		slog.Error("sweep query", "err", err)
		return
	}
	for _, o := range pending {
		// No tx hash: the buyer never completed payment. Expire the order after
		// the TTL so its stock reservation is released.
		if o.TxHash == "" {
			if time.Since(o.CreatedAt) > pendingOrderTTL {
				if err := p.Store.FailOrder(ctx, o.ID, o.ItemID, o.Qty); err != nil {
					slog.Error("expire order", "order", o.ID, "err", err)
				} else {
					slog.Info("order expired (no payment)", "order", o.ID)
				}
			}
			continue
		}
		if p.V == nil {
			continue // no on-chain config — leave paid orders pending
		}
		minAmount, err := p.RequiredForTx(ctx, o.TxHash)
		if err != nil {
			slog.Error("amount sum", "order", o.ID, "err", err)
			continue
		}
		st, err := p.V.verify(ctx, o.TxHash, minAmount)
		if err != nil {
			slog.Warn("verify", "order", o.ID, "err", err)
			continue
		}
		switch st {
		case statusConfirmed:
			claimed, err := p.Store.ConfirmOrder(ctx, o.ID)
			if err != nil {
				slog.Error("confirm order", "order", o.ID, "err", err)
			} else if claimed {
				slog.Info("order confirmed", "order", o.ID, "tx", o.TxHash)
				p.notifyConfirmed(ctx, o)
			}
		case statusFailed:
			if err := p.Store.FailOrder(ctx, o.ID, o.ItemID, o.Qty); err != nil {
				slog.Error("fail order", "order", o.ID, "err", err)
			}
		}
	}
}

// RequiredForTx sums the amounts of every non-failed order referencing the same
// transaction. One cart checkout legitimately records several orders sharing a
// tx (the buyer pays the cart total in a single transfer), so the transfer must
// cover their sum — and a replayed tx hash can never pay for more orders than
// the original transfer covered: an extra order pushes the sum past the
// on-chain amount and fails verification.
func (p *PaymentVerifier) RequiredForTx(ctx context.Context, txHash string) (*big.Int, error) {
	amounts, err := p.Store.ListActiveAmountsForTx(ctx, txHash)
	if err != nil {
		return nil, err
	}
	total := new(big.Int)
	for _, a := range amounts {
		v, err := ToBaseUnits(a, p.Decimals)
		if err != nil {
			return nil, err
		}
		total.Add(total, v)
	}
	return total, nil
}

// notifyConfirmed posts the confirmed order to the shop's Discord channel.
// Best-effort: a Discord hiccup must not affect order settlement, so failures
// are only logged.
func (p *PaymentVerifier) notifyConfirmed(ctx context.Context, o store.PendingOrder) {
	if p.Notify == nil {
		return
	}
	msg := fmt.Sprintf("🛒 New order confirmed: %d× %s — %s USDT (order %s)",
		o.Qty, o.ItemID, o.Amount, o.ID)
	if err := p.Notify.Send(ctx, msg); err != nil {
		slog.Warn("discord order notification failed", "order", o.ID, "err", err)
	}
}
