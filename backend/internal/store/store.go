package store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrAlreadyExists     = errors.New("already exists")
)

// cena je string a ne float, bson za mongo
type Item struct {
	ID    string `json:"id" bson:"_id"`
	Name  string `json:"name" binding:"required" bson:"name"`
	Price string `json:"price_usdt" binding:"required" bson:"price_usdt"`
	Stock int    `json:"stock" bson:"stock"`
}

// txHash je opcion jer porudzbina moze biti bez njega dok se ne plati
type Order struct {
	ID           string    `json:"id" bson:"_id"`
	BuyerWallet  string    `json:"buyer_wallet" binding:"required" bson:"buyer_wallet"`
	TxHash       *string   `json:"tx_hash,omitempty" bson:"tx_hash"`
	AmountUSDT   string    `json:"amount_usdt" bson:"amount_usdt"`
	Status       string    `json:"status" bson:"status"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	ItemID       string    `json:"item_id,omitempty" binding:"required" bson:"item_id"`
	ItemQuantity int       `json:"item_quantity,omitempty" binding:"required,min=1" bson:"item_quantity"`
}

// podskup za verifikaciju placanja
type PendingOrder struct {
	ID        string
	TxHash    string
	Amount    string
	ItemID    string
	Qty       int
	CreatedAt time.Time
}

type Store interface {
	EnsureSchema(ctx context.Context) error // napravi tabele
	Ping(ctx context.Context) error         // ping za readiness probe
	Close()
	// crud
	ListItems(ctx context.Context) ([]Item, error)
	CreateItem(ctx context.Context, it Item) error
	UpdateItem(ctx context.Context, id string, it Item) error
	DeleteItem(ctx context.Context, id string) error

	ListOrders(ctx context.Context) ([]Order, error)
	GetOrder(ctx context.Context, id string) (Order, error)
	// ovo automatski rezervise order, pa 2 kupca ne mogu da zaglave na istoj stvari
	CreateOrder(ctx context.Context, o Order) (Order, error)
	// ovo dodaje tx hash na order koji nema tx hash (kada se potrvrdi placanje)
	SetOrderTx(ctx context.Context, orderID, txHash string) error
	// za replay zastitu, sabira iznose svih ordera sa istim tx hashom
	ListActiveAmountsForTx(ctx context.Context, txHash string) ([]string, error)
	ListPendingOrders(ctx context.Context) ([]PendingOrder, error)
	// menja pending u confrimed samo jednom, discord poruka samo jednom stize
	ConfirmOrder(ctx context.Context, orderID string) (bool, error)
	// iz pending u failed, vraca nazad pare
	FailOrder(ctx context.Context, orderID, itemID string, qty int) error
}

func OrderTotal(price string, qty int) (string, error) {
	r, ok := new(big.Rat).SetString(price) // is stringa u veliki racionalan broj
	if !ok {
		return "", fmt.Errorf("invalid price %q", price)
	}
	r.Mul(r, new(big.Rat).SetInt64(int64(qty)))    // mnozimo sa kolicinom
	s := strings.TrimRight(r.FloatString(18), "0") // formatiraj, skini nule
	return strings.TrimSuffix(s, "."), nil
}

// jedan backend, 2 baze
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
