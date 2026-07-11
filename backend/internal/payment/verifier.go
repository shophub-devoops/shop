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

// 30 minuta cekamo da oslobodimo stock ako se zaglavi
const pendingOrderTTL = 30 * time.Minute

type PaymentVerifier struct {
	Store    store.Store // radi na postgress i mongo
	V        *Verifier   // ako je order abandoned nikad nece biti potvrdjen, samo ce isteci
	Decimals int
	Notify   *notify.Discord // on chain verifier i discord notifier su pokazivaci, mogu biti nil
}

func (p *PaymentVerifier) Run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second) // svakih 15 sekundi gledamo jel stigla transakcija
	defer t.Stop()
	slog.Info("payment verifier started")
	for {
		select {
		case <-ctx.Done(): // SIGTERM za cist, uredan izlaz
			return
		case <-t.C: // ako nije otkazano radimo sweep
			p.sweep(ctx)
		}
	}
}

func (p *PaymentVerifier) sweep(ctx context.Context) {
	pending, err := p.Store.ListPendingOrders(ctx) // za svaku PENDING porudzbinu
	if err != nil {
		slog.Error("sweep query", "err", err)
		return
	}
	for _, o := range pending {
		if o.TxHash == "" { // napusten checkout, nema uplate
			if time.Since(o.CreatedAt) > pendingOrderTTL { // ako prodje 30 minuta onda istekni
				if err := p.Store.FailOrder(ctx, o.ID, o.ItemID, o.Qty); err != nil {
					slog.Error("expire order", "order", o.ID, "err", err)
				} else {
					slog.Info("order expired (no payment)", "order", o.ID)
				}
			}
			continue
		}
		if p.V == nil {
			continue
		}
		minAmount, err := p.RequiredForTx(ctx, o.TxHash) // da li ima dovoljno USDT
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
			} else if claimed { // ako sam je potvrdio onda salji discord poruku
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

func (p *PaymentVerifier) RequiredForTx(ctx context.Context, txHash string) (*big.Int, error) {
	amounts, err := p.Store.ListActiveAmountsForTx(ctx, txHash) // svi ne failed orderi sa tim hashom
	if err != nil {
		return nil, err
	}
	total := new(big.Int) // saberi njihove iznose
	for _, a := range amounts {
		v, err := ToBaseUnits(a, p.Decimals)
		if err != nil {
			return nil, err
		}
		total.Add(total, v)
	}
	return total, nil // transfer mora da pokrije zbir svih ordera sa istim tx hashom
}

// best effort sto se tice discorda, ako discord zapne ne kocimo transakciju zbog toga
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
