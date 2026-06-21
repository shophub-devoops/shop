package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoStore is the MongoDB community-operator / "light" database implementation
// of Store. Items and orders map to two collections keyed by their id (_id);
// the same Item/Order structs (bson-tagged) are persisted directly.
type mongoStore struct {
	client *mongo.Client
	items  *mongo.Collection
	orders *mongo.Collection
}

func newMongoStore(ctx context.Context, dsn, dbName string) (*mongoStore, error) {
	if dbName == "" {
		return nil, fmt.Errorf("SHOP_DB_NAME is required for a mongodb DATABASE_URL")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(dsn))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("initial ping: %w", err)
	}
	db := client.Database(dbName)
	return &mongoStore{
		client: client,
		items:  db.Collection("items"),
		orders: db.Collection("orders"),
	}, nil
}

func (s *mongoStore) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.client.Disconnect(ctx)
}

func (s *mongoStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.client.Ping(pingCtx, nil)
}

// EnsureSchema is a no-op: MongoDB creates collections lazily on first write and
// items/orders are keyed by _id, so no DDL or index setup is required.
func (s *mongoStore) EnsureSchema(_ context.Context) error { return nil }

func (s *mongoStore) ListItems(ctx context.Context) ([]Item, error) {
	cur, err := s.items.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := []Item{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *mongoStore) CreateItem(ctx context.Context, it Item) error {
	_, err := s.items.InsertOne(ctx, it)
	return err
}

func (s *mongoStore) UpdateItem(ctx context.Context, id string, it Item) error {
	res, err := s.items.UpdateByID(ctx, id, bson.M{"$set": bson.M{
		"name":       it.Name,
		"price_usdt": it.Price,
		"stock":      it.Stock,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *mongoStore) DeleteItem(ctx context.Context, id string) error {
	res, err := s.items.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *mongoStore) ListOrders(ctx context.Context) ([]Order, error) {
	cur, err := s.orders.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := []Order{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *mongoStore) GetOrder(ctx context.Context, id string) (Order, error) {
	var o Order
	err := s.orders.FindOne(ctx, bson.M{"_id": id}).Decode(&o)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Order{}, ErrNotFound
	}
	return o, err
}

// CreateOrder reserves stock with a guarded $inc (only matches when enough
// stock remains — atomic on the replica set, so no oversell), then records the
// pending order. If the insert fails the reservation is rolled back.
// FindOneAndUpdate returns the matched item, whose stored price is used to
// compute the order amount server-side (price × qty); the client-supplied
// amount is never trusted.
func (s *mongoStore) CreateOrder(ctx context.Context, o Order) (Order, error) {
	dec := s.items.FindOneAndUpdate(ctx,
		bson.M{"_id": o.ItemID, "stock": bson.M{"$gte": o.ItemQuantity}},
		bson.M{"$inc": bson.M{"stock": -o.ItemQuantity}})
	if errors.Is(dec.Err(), mongo.ErrNoDocuments) {
		// Either the item doesn't exist or there isn't enough stock.
		err := s.items.FindOne(ctx, bson.M{"_id": o.ItemID}).Err()
		if errors.Is(err, mongo.ErrNoDocuments) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, err
		}
		return Order{}, ErrInsufficientStock
	}
	if dec.Err() != nil {
		return Order{}, dec.Err()
	}

	var it Item
	if err := dec.Decode(&it); err != nil {
		// Roll the reservation back so a failed decode doesn't leak stock.
		_, _ = s.items.UpdateByID(ctx, o.ItemID, bson.M{"$inc": bson.M{"stock": o.ItemQuantity}})
		return Order{}, err
	}
	amount, err := OrderTotal(it.Price, o.ItemQuantity)
	if err != nil {
		_, _ = s.items.UpdateByID(ctx, o.ItemID, bson.M{"$inc": bson.M{"stock": o.ItemQuantity}})
		return Order{}, err
	}

	o.AmountUSDT = amount
	o.Status = "pending"
	o.CreatedAt = time.Now()
	if _, err := s.orders.InsertOne(ctx, o); err != nil {
		// Roll the reservation back so a failed insert doesn't leak stock.
		_, _ = s.items.UpdateByID(ctx, o.ItemID, bson.M{"$inc": bson.M{"stock": o.ItemQuantity}})
		return Order{}, err
	}
	return o, nil
}

// SetOrderTx is conditional on "pending and unpaid" so a hash can be attached
// exactly once — a second attempt (or an attempt on a settled order) is a 404.
// "tx_hash": nil matches both a null and a missing field.
func (s *mongoStore) SetOrderTx(ctx context.Context, orderID, txHash string) error {
	res, err := s.orders.UpdateOne(ctx,
		bson.M{"_id": orderID, "status": "pending", "tx_hash": nil},
		bson.M{"$set": bson.M{"tx_hash": txHash}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *mongoStore) ListActiveAmountsForTx(ctx context.Context, txHash string) ([]string, error) {
	cur, err := s.orders.Find(ctx, bson.M{"tx_hash": txHash, "status": bson.M{"$ne": "failed"}})
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := cur.All(ctx, &orders); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.AmountUSDT)
	}
	return out, nil
}

func (s *mongoStore) ListPendingOrders(ctx context.Context) ([]PendingOrder, error) {
	cur, err := s.orders.Find(ctx, bson.M{"status": "pending"})
	if err != nil {
		return nil, err
	}
	var orders []Order
	if err := cur.All(ctx, &orders); err != nil {
		return nil, err
	}
	out := make([]PendingOrder, 0, len(orders))
	for _, o := range orders {
		tx := ""
		if o.TxHash != nil {
			tx = *o.TxHash
		}
		out = append(out, PendingOrder{
			ID: o.ID, TxHash: tx, Amount: o.AmountUSDT, ItemID: o.ItemID, Qty: o.ItemQuantity,
			CreatedAt: o.CreatedAt,
		})
	}
	return out, nil
}

// ConfirmOrder flips the order out of 'pending' with a conditional update so
// concurrent replica sweeps confirm exactly once. Stock was already reserved
// when the order was created.
func (s *mongoStore) ConfirmOrder(ctx context.Context, orderID string) error {
	claim := s.orders.FindOneAndUpdate(ctx,
		bson.M{"_id": orderID, "status": "pending"},
		bson.M{"$set": bson.M{"status": "confirmed", "verified_at": time.Now()}})
	if errors.Is(claim.Err(), mongo.ErrNoDocuments) {
		return nil // already handled by another sweep/replica
	}
	return claim.Err()
}

// FailOrder claims the pending order with a conditional update, then restores
// its reserved stock. The claim guards against a concurrent sweep restoring
// the same reservation twice.
func (s *mongoStore) FailOrder(ctx context.Context, orderID, itemID string, qty int) error {
	claim := s.orders.FindOneAndUpdate(ctx,
		bson.M{"_id": orderID, "status": "pending"},
		bson.M{"$set": bson.M{"status": "failed"}})
	if errors.Is(claim.Err(), mongo.ErrNoDocuments) {
		return nil // already handled by another sweep/replica
	}
	if claim.Err() != nil {
		return claim.Err()
	}
	if itemID != "" && qty > 0 {
		_, err := s.items.UpdateByID(ctx, itemID, bson.M{"$inc": bson.M{"stock": qty}})
		return err
	}
	return nil
}
