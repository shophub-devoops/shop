package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Domain errors the HTTP layer maps to status codes, so a Store implementation
// never needs to know about HTTP.
var (
	errNotFound          = errors.New("not found")
	errInsufficientStock = errors.New("insufficient stock")
)

// pendingOrder is an unconfirmed order the payment verifier sweeps: orders with
// a tx hash get verified on-chain, abandoned ones (no tx hash) expire after
// pendingOrderTTL so their stock reservation is released.
type pendingOrder struct {
	id, txHash, amount, itemID string
	qty                        int
	createdAt                  time.Time
}

// Store is the persistence boundary for one Shop tenant: items, orders, and the
// payment-confirmation transaction. Two implementations back it — Postgres
// (CNPG) and MongoDB (community operator) — chosen by the DATABASE_URL scheme so
// the same backend serves whichever database the Shop CR requested ("standard"
// vs "light"). Implementations return errNotFound / errInsufficientStock for the
// business cases; everything else is an infrastructure error.
type Store interface {
	EnsureSchema(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()

	ListItems(ctx context.Context) ([]item, error)
	CreateItem(ctx context.Context, it item) error
	UpdateItem(ctx context.Context, id string, it item) error
	DeleteItem(ctx context.Context, id string) error

	ListOrders(ctx context.Context) ([]order, error)
	GetOrder(ctx context.Context, id string) (order, error)
	// CreateOrder atomically reserves stock (guarded decrement) and records the
	// pending order, so two buyers can never oversell the same units. The
	// reservation is released by FailOrder (payment failed / order expired).
	// The order amount is computed here from the item's stored price × quantity
	// — never trusted from the client — and returned in the persisted order so
	// the payment verifier checks the real total.
	CreateOrder(ctx context.Context, o order) (order, error)
	// SetOrderTx attaches the payment transaction hash to a pending order that
	// has none yet (the storefront reserves stock first, then pays, then
	// attaches the resulting hash). Returns errNotFound when there is no
	// pending, unpaid order with that id.
	SetOrderTx(ctx context.Context, orderID, txHash string) error
	// ListActiveAmountsForTx returns amount_usdt of every non-failed order
	// referencing txHash. The payment verifier sums these, so one transaction
	// can never cover more orders than it actually paid for (replay protection).
	ListActiveAmountsForTx(ctx context.Context, txHash string) ([]string, error)

	ListPendingOrders(ctx context.Context) ([]pendingOrder, error)
	// ConfirmOrder flips a pending order to confirmed exactly once, even across
	// concurrent replicas. Stock was already reserved at creation.
	ConfirmOrder(ctx context.Context, orderID string) error
	// FailOrder flips a pending order to failed and restores its reserved stock
	// exactly once (the status claim guards against double-restores).
	FailOrder(ctx context.Context, orderID, itemID string, qty int) error
}

// orderTotal computes price × qty as a decimal string. Prices travel as strings
// end-to-end (numeric in Postgres), so the multiplication uses big.Rat to avoid
// float drift; trailing zeros are trimmed ("19.980000" -> "19.98").
func orderTotal(price string, qty int) (string, error) {
	r, ok := new(big.Rat).SetString(price)
	if !ok {
		return "", fmt.Errorf("invalid price %q", price)
	}
	r.Mul(r, new(big.Rat).SetInt64(int64(qty)))
	s := strings.TrimRight(r.FloatString(18), "0")
	return strings.TrimSuffix(s, "."), nil
}

// newStore selects the implementation from the DATABASE_URL scheme. dbName is
// the database the Mongo client should use (the operator injects it as
// SHOP_DB_NAME because, unlike a Postgres URI, the Mongo connection string the
// community operator publishes carries no default database).
func newStore(ctx context.Context, dsn, dbName string) (Store, error) {
	switch {
	case strings.HasPrefix(dsn, "mongodb://"), strings.HasPrefix(dsn, "mongodb+srv://"):
		return newMongoStore(ctx, dsn, dbName)
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return newPostgresStore(ctx, dsn)
	default:
		return nil, fmt.Errorf("unsupported DATABASE_URL scheme (want postgres:// or mongodb://)")
	}
}
