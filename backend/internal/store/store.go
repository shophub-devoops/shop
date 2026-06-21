// Package store is the persistence boundary for one Shop tenant: items, orders,
// and the payment-confirmation transaction. Two implementations back it —
// Postgres (CNPG) and MongoDB (community operator) — chosen by the DATABASE_URL
// scheme so the same backend serves whichever database the Shop CR requested
// ("standard" vs "light").
package store

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
	ErrNotFound          = errors.New("not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

// Item is a catalogue entry. Prices travel as strings end-to-end (numeric in
// Postgres) to avoid float drift.
type Item struct {
	ID    string `json:"id" bson:"_id"`
	Name  string `json:"name" binding:"required" bson:"name"`
	Price string `json:"price_usdt" binding:"required" bson:"price_usdt"`
	Stock int    `json:"stock" bson:"stock"`
}

// Order is a buyer's purchase. AmountUSDT is output-only: the store computes it
// from the item's stored price × quantity at creation, so a crafted request
// can't understate what the payment verifier expects on-chain.
type Order struct {
	ID          string  `json:"id" bson:"_id"`
	BuyerWallet string  `json:"buyer_wallet" binding:"required" bson:"buyer_wallet"`
	TxHash      *string `json:"tx_hash,omitempty" bson:"tx_hash"`
	AmountUSDT  string  `json:"amount_usdt" bson:"amount_usdt"`
	Status      string  `json:"status" bson:"status"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
	ItemID      string    `json:"item_id,omitempty" binding:"required" bson:"item_id"`
	ItemQuantity int      `json:"item_quantity,omitempty" binding:"required,min=1" bson:"item_quantity"`
}

// PendingOrder is an unconfirmed order the payment verifier sweeps: orders with
// a tx hash get verified on-chain, abandoned ones (no tx hash) expire after the
// TTL so their stock reservation is released.
type PendingOrder struct {
	ID        string
	TxHash    string
	Amount    string
	ItemID    string
	Qty       int
	CreatedAt time.Time
}

// Store is the persistence interface. Implementations return ErrNotFound /
// ErrInsufficientStock for the business cases; everything else is an
// infrastructure error.
type Store interface {
	EnsureSchema(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()

	ListItems(ctx context.Context) ([]Item, error)
	CreateItem(ctx context.Context, it Item) error
	UpdateItem(ctx context.Context, id string, it Item) error
	DeleteItem(ctx context.Context, id string) error

	ListOrders(ctx context.Context) ([]Order, error)
	GetOrder(ctx context.Context, id string) (Order, error)
	// CreateOrder atomically reserves stock (guarded decrement) and records the
	// pending order, so two buyers can never oversell the same units. The
	// reservation is released by FailOrder (payment failed / order expired).
	// The order amount is computed here from the item's stored price × quantity
	// — never trusted from the client — and returned in the persisted order.
	CreateOrder(ctx context.Context, o Order) (Order, error)
	// SetOrderTx attaches the payment transaction hash to a pending order that
	// has none yet. Returns ErrNotFound when there is no pending, unpaid order
	// with that id.
	SetOrderTx(ctx context.Context, orderID, txHash string) error
	// ListActiveAmountsForTx returns amount_usdt of every non-failed order
	// referencing txHash. The payment verifier sums these (replay protection).
	ListActiveAmountsForTx(ctx context.Context, txHash string) ([]string, error)

	ListPendingOrders(ctx context.Context) ([]PendingOrder, error)
	// ConfirmOrder flips a pending order to confirmed exactly once, even across
	// concurrent replicas. Stock was already reserved at creation.
	ConfirmOrder(ctx context.Context, orderID string) error
	// FailOrder flips a pending order to failed and restores its reserved stock
	// exactly once (the status claim guards against double-restores).
	FailOrder(ctx context.Context, orderID, itemID string, qty int) error
}

// OrderTotal computes price × qty as a decimal string. Prices travel as strings
// end-to-end (numeric in Postgres), so the multiplication uses big.Rat to avoid
// float drift; trailing zeros are trimmed ("19.980000" -> "19.98").
func OrderTotal(price string, qty int) (string, error) {
	r, ok := new(big.Rat).SetString(price)
	if !ok {
		return "", fmt.Errorf("invalid price %q", price)
	}
	r.Mul(r, new(big.Rat).SetInt64(int64(qty)))
	s := strings.TrimRight(r.FloatString(18), "0")
	return strings.TrimSuffix(s, "."), nil
}

// New selects the implementation from the DATABASE_URL scheme. dbName is the
// database the Mongo client should use (the operator injects it as SHOP_DB_NAME
// because, unlike a Postgres URI, the Mongo connection string the community
// operator publishes carries no default database).
func New(ctx context.Context, dsn, dbName string) (Store, error) {
	switch {
	case strings.HasPrefix(dsn, "mongodb://"), strings.HasPrefix(dsn, "mongodb+srv://"):
		return newMongoStore(ctx, dsn, dbName)
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return newPostgresStore(ctx, dsn)
	default:
		return nil, fmt.Errorf("unsupported DATABASE_URL scheme (want postgres:// or mongodb://)")
	}
}
