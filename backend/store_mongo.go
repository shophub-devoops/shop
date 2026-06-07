package main

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
// the same item/order structs (bson-tagged) are persisted directly.
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

func (s *mongoStore) ListItems(ctx context.Context) ([]item, error) {
	cur, err := s.items.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := []item{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *mongoStore) CreateItem(ctx context.Context, it item) error {
	_, err := s.items.InsertOne(ctx, it)
	return err
}

func (s *mongoStore) UpdateItem(ctx context.Context, id string, it item) error {
	res, err := s.items.UpdateByID(ctx, id, bson.M{"$set": bson.M{
		"name":       it.Name,
		"price_usdt": it.Price,
		"stock":      it.Stock,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errNotFound
	}
	return nil
}

func (s *mongoStore) DeleteItem(ctx context.Context, id string) error {
	res, err := s.items.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return errNotFound
	}
	return nil
}

func (s *mongoStore) ListOrders(ctx context.Context) ([]order, error) {
	cur, err := s.orders.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	out := []order{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *mongoStore) GetOrder(ctx context.Context, id string) (order, error) {
	var o order
	err := s.orders.FindOne(ctx, bson.M{"_id": id}).Decode(&o)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return order{}, errNotFound
	}
	return o, err
}

func (s *mongoStore) CreateOrder(ctx context.Context, o order) error {
	var it item
	err := s.items.FindOne(ctx, bson.M{"_id": o.ItemID}).Decode(&it)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if it.Stock < o.ItemQuantity {
		return errInsufficientStock
	}
	o.Status = "pending"
	o.CreatedAt = time.Now()
	_, err = s.orders.InsertOne(ctx, o)
	return err
}

func (s *mongoStore) ListPendingOrders(ctx context.Context) ([]pendingOrder, error) {
	cur, err := s.orders.Find(ctx, bson.M{"status": "pending", "tx_hash": bson.M{"$ne": nil}})
	if err != nil {
		return nil, err
	}
	var orders []order
	if err := cur.All(ctx, &orders); err != nil {
		return nil, err
	}
	out := make([]pendingOrder, 0, len(orders))
	for _, o := range orders {
		tx := ""
		if o.TxHash != nil {
			tx = *o.TxHash
		}
		out = append(out, pendingOrder{
			id: o.ID, txHash: tx, amount: o.AmountUSDT, itemID: o.ItemID, qty: o.ItemQuantity,
		})
	}
	return out, nil
}

// ConfirmOrder claims the order with a conditional FindOneAndUpdate (only the
// update that flips it out of 'pending' proceeds), then decrements stock with a
// guarded $inc that only matches when stock is sufficient — so concurrent
// replica sweeps decrement exactly once and oversell fails the order instead of
// driving stock negative. The replica-set (MongoDBCommunity) makes both updates
// individually atomic.
func (s *mongoStore) ConfirmOrder(ctx context.Context, orderID, itemID string, qty int) error {
	claim := s.orders.FindOneAndUpdate(ctx,
		bson.M{"_id": orderID, "status": "pending"},
		bson.M{"$set": bson.M{"status": "confirmed", "verified_at": time.Now()}})
	if errors.Is(claim.Err(), mongo.ErrNoDocuments) {
		return nil // already handled by another sweep/replica
	}
	if claim.Err() != nil {
		return claim.Err()
	}

	dec := s.items.FindOneAndUpdate(ctx,
		bson.M{"_id": itemID, "stock": bson.M{"$gte": qty}},
		bson.M{"$inc": bson.M{"stock": -qty}})
	if errors.Is(dec.Err(), mongo.ErrNoDocuments) {
		// Sold out between order and confirmation — fail rather than oversell.
		_, err := s.orders.UpdateByID(ctx, orderID, bson.M{"$set": bson.M{"status": "failed"}})
		return err
	}
	return dec.Err()
}

func (s *mongoStore) FailOrder(ctx context.Context, orderID string) error {
	_, err := s.orders.UpdateByID(ctx, orderID, bson.M{"$set": bson.M{"status": "failed"}})
	return err
}
