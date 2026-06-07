package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Domain errors the HTTP layer maps to status codes, so a Store implementation
// never needs to know about HTTP.
var (
	errNotFound          = errors.New("not found")
	errInsufficientStock = errors.New("insufficient stock")
)

// pendingOrder is a paid-but-unconfirmed order the payment verifier sweeps.
type pendingOrder struct {
	id, txHash, amount, itemID string
	qty                        int
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
	CreateOrder(ctx context.Context, o order) error

	ListPendingOrders(ctx context.Context) ([]pendingOrder, error)
	// ConfirmOrder flips a pending order to confirmed and decrements stock
	// exactly once, even across concurrent replicas (oversell -> failed).
	ConfirmOrder(ctx context.Context, orderID, itemID string, qty int) error
	FailOrder(ctx context.Context, orderID string) error
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
