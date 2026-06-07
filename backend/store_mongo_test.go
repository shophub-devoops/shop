package main

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

	store, err := newMongoStore(ctx, uri, "shoptest")
	if err != nil {
		t.Fatalf("new mongo store: %v", err)
	}
	defer store.Close()

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Item CRUD.
	if err := store.CreateItem(ctx, item{ID: "m1", Name: "Widget", Price: "9.99", Stock: 5}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	if items, err := store.ListItems(ctx); err != nil || len(items) != 1 || items[0].Name != "Widget" || items[0].Stock != 5 {
		t.Fatalf("list items = %+v, err %v", items, err)
	}
	if err := store.UpdateItem(ctx, "m1", item{Name: "Widget", Price: "9.99", Stock: 12}); err != nil {
		t.Fatalf("update item: %v", err)
	}
	if items, _ := store.ListItems(ctx); items[0].Stock != 12 {
		t.Fatalf("stock after update = %d, want 12", items[0].Stock)
	}
	if err := store.UpdateItem(ctx, "ghost", item{Name: "x", Price: "1", Stock: 1}); !errors.Is(err, errNotFound) {
		t.Fatalf("update missing = %v, want errNotFound", err)
	}
	if err := store.DeleteItem(ctx, "ghost"); !errors.Is(err, errNotFound) {
		t.Fatalf("delete missing = %v, want errNotFound", err)
	}

	// Order creation enforces item existence and stock.
	if err := store.CreateOrder(ctx, order{ID: "ord-unknown", BuyerWallet: "0xBUY", AmountUSDT: "1", ItemID: "nope", ItemQuantity: 1}); !errors.Is(err, errNotFound) {
		t.Fatalf("order unknown item = %v, want errNotFound", err)
	}
	if err := store.CreateOrder(ctx, order{ID: "ord-toomuch", BuyerWallet: "0xBUY", AmountUSDT: "99", ItemID: "m1", ItemQuantity: 99}); !errors.Is(err, errInsufficientStock) {
		t.Fatalf("order over stock = %v, want errInsufficientStock", err)
	}
	tx := "0xtx"
	if err := store.CreateOrder(ctx, order{ID: "ord-ok", BuyerWallet: "0xBUY", TxHash: &tx, AmountUSDT: "20", ItemID: "m1", ItemQuantity: 2}); err != nil {
		t.Fatalf("create order: %v", err)
	}
	if o, err := store.GetOrder(ctx, "ord-ok"); err != nil || o.Status != "pending" {
		t.Fatalf("get order = %+v err %v", o, err)
	}
	if _, err := store.GetOrder(ctx, "missing"); !errors.Is(err, errNotFound) {
		t.Fatalf("get missing order = %v, want errNotFound", err)
	}

	// The order with a tx hash shows up for the verifier sweep.
	if pending, err := store.ListPendingOrders(ctx); err != nil || len(pending) != 1 || pending[0].id != "ord-ok" {
		t.Fatalf("pending = %+v, err %v", pending, err)
	}

	// ConfirmOrder is idempotent: 3 calls decrement stock exactly once (12-2=10).
	for i := 0; i < 3; i++ {
		if err := store.ConfirmOrder(ctx, "ord-ok", "m1", 2); err != nil {
			t.Fatalf("confirm #%d: %v", i, err)
		}
	}
	if o, _ := store.GetOrder(ctx, "ord-ok"); o.Status != "confirmed" {
		t.Errorf("status = %q, want confirmed", o.Status)
	}
	if items, _ := store.ListItems(ctx); items[0].Stock != 10 {
		t.Errorf("stock = %d, want 10 (decremented once by 2)", items[0].Stock)
	}
}
