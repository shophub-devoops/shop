package store

import (
	"context"
	"errors"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/mongodb"
)

// TestMongoStore exercises the MongoDB Store implementation against a throwaway
// mongod (Testcontainers), mirroring the Postgres handler tests so the "light"
// database path is proven, not just compiled. A standalone mongod is enough:
// ConfirmOrder uses guarded single-document updates, not multi-doc transactions.
func TestMongoStore(t *testing.T) {
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:6")
	if err != nil {
		t.Fatalf("start mongo: %v", err)
	}
	defer func() { _ = container.Terminate(ctx) }()

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}

	st, err := newMongoStore(ctx, uri, "shoptest")
	if err != nil {
		t.Fatalf("new mongo store: %v", err)
	}
	defer st.Close()

	if err := st.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := st.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Item CRUD.
	if err := st.CreateItem(ctx, Item{ID: "m1", Name: "Widget", Price: "9.99", Stock: 5}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	if items, err := st.ListItems(ctx); err != nil || len(items) != 1 || items[0].Name != "Widget" || items[0].Stock != 5 {
		t.Fatalf("list items = %+v, err %v", items, err)
	}
	if err := st.UpdateItem(ctx, "m1", Item{Name: "Widget", Price: "9.99", Stock: 12}); err != nil {
		t.Fatalf("update item: %v", err)
	}
	if items, _ := st.ListItems(ctx); items[0].Stock != 12 {
		t.Fatalf("stock after update = %d, want 12", items[0].Stock)
	}
	if err := st.UpdateItem(ctx, "ghost", Item{Name: "x", Price: "1", Stock: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing = %v, want ErrNotFound", err)
	}
	if err := st.DeleteItem(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}

	// Order creation enforces item existence and stock.
	if _, err := st.CreateOrder(ctx, Order{ID: "ord-unknown", BuyerWallet: "0xBUY", ItemID: "nope", ItemQuantity: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("order unknown item = %v, want ErrNotFound", err)
	}
	if _, err := st.CreateOrder(ctx, Order{ID: "ord-toomuch", BuyerWallet: "0xBUY", ItemID: "m1", ItemQuantity: 99}); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("order over stock = %v, want ErrInsufficientStock", err)
	}
	tx := "0xtx"
	// The client-sent amount lies ("1"); the store must compute 9.99 × 2 = 19.98.
	created, err := st.CreateOrder(ctx, Order{ID: "ord-ok", BuyerWallet: "0xBUY", TxHash: &tx, AmountUSDT: "1", ItemID: "m1", ItemQuantity: 2})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if created.AmountUSDT != "19.98" {
		t.Fatalf("amount = %q, want %q (server-computed price × qty)", created.AmountUSDT, "19.98")
	}
	if o, err := st.GetOrder(ctx, "ord-ok"); err != nil || o.Status != "pending" || o.AmountUSDT != "19.98" {
		t.Fatalf("get order = %+v err %v", o, err)
	}
	if _, err := st.GetOrder(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing order = %v, want ErrNotFound", err)
	}

	// Stock is reserved at order creation (12-2=10).
	if items, _ := st.ListItems(ctx); items[0].Stock != 10 {
		t.Fatalf("stock after order = %d, want 10 (reserved at creation)", items[0].Stock)
	}

	// The pending order shows up for the verifier sweep.
	if pending, err := st.ListPendingOrders(ctx); err != nil || len(pending) != 1 || pending[0].ID != "ord-ok" {
		t.Fatalf("pending = %+v, err %v", pending, err)
	}

	// ConfirmOrder is idempotent and never touches stock again.
	for i := 0; i < 3; i++ {
		if err := st.ConfirmOrder(ctx, "ord-ok"); err != nil {
			t.Fatalf("confirm #%d: %v", i, err)
		}
	}
	if o, _ := st.GetOrder(ctx, "ord-ok"); o.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", o.Status)
	}
	if items, _ := st.ListItems(ctx); items[0].Stock != 10 {
		t.Errorf("stock = %d, want 10 (reserved once at creation)", items[0].Stock)
	}

	// FailOrder releases the reservation exactly once (idempotent claim).
	if _, err := st.CreateOrder(ctx, Order{ID: "ord-fail", BuyerWallet: "0xBUY", ItemID: "m1", ItemQuantity: 3}); err != nil {
		t.Fatalf("create order to fail: %v", err)
	}
	if items, _ := st.ListItems(ctx); items[0].Stock != 7 {
		t.Fatalf("stock after second order = %d, want 7", items[0].Stock)
	}
	for i := 0; i < 3; i++ {
		if err := st.FailOrder(ctx, "ord-fail", "m1", 3); err != nil {
			t.Fatalf("fail #%d: %v", i, err)
		}
	}
	if o, _ := st.GetOrder(ctx, "ord-fail"); o.Status != "failed" {
		t.Errorf("status = %q, want failed", o.Status)
	}
	if items, _ := st.ListItems(ctx); items[0].Stock != 10 {
		t.Errorf("stock = %d, want 10 (restored exactly once)", items[0].Stock)
	}

	// Reserve → pay → attach: the tx hash attaches to a pending unpaid order
	// exactly once, and never to a settled one.
	if _, err := st.CreateOrder(ctx, Order{ID: "ord-attach", BuyerWallet: "0xBUY", ItemID: "m1", ItemQuantity: 1}); err != nil {
		t.Fatalf("create unpaid order: %v", err)
	}
	if err := st.SetOrderTx(ctx, "ord-attach", "0xpay"); err != nil {
		t.Fatalf("attach tx: %v", err)
	}
	if err := st.SetOrderTx(ctx, "ord-attach", "0xother"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-attach = %v, want ErrNotFound", err)
	}
	if err := st.SetOrderTx(ctx, "ord-ok", "0xother"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach to settled order = %v, want ErrNotFound", err)
	}
	if o, _ := st.GetOrder(ctx, "ord-attach"); o.TxHash == nil || *o.TxHash != "0xpay" {
		t.Errorf("tx after attach = %v, want 0xpay", o.TxHash)
	}

	// Replay accounting: non-failed orders sharing a tx hash are summed by the
	// verifier; failed ones drop out of the sum. The amount is server-computed
	// from the item price (9.99 × 1).
	if amounts, err := st.ListActiveAmountsForTx(ctx, "0xpay"); err != nil || len(amounts) != 1 || amounts[0] != "9.99" {
		t.Fatalf("active amounts = %v, err %v, want [9.99]", amounts, err)
	}
	if err := st.FailOrder(ctx, "ord-attach", "m1", 1); err != nil {
		t.Fatalf("fail attached order: %v", err)
	}
	if amounts, _ := st.ListActiveAmountsForTx(ctx, "0xpay"); len(amounts) != 0 {
		t.Fatalf("active amounts after fail = %v, want empty", amounts)
	}
}
