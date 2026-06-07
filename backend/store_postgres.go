package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresStore is the CNPG / "standard" database implementation of Store.
type postgresStore struct {
	pool *pgxpool.Pool
}

func newPostgresStore(ctx context.Context, dsn string) (*postgresStore, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("initial ping: %w", err)
	}
	return &postgresStore{pool: pool}, nil
}

func (s *postgresStore) Close() { s.pool.Close() }

func (s *postgresStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.pool.Ping(pingCtx)
}

// EnsureSchema creates the application tables and adds the D12 payment columns
// idempotently. The operator seeds base tables via CNPG postInitApplicationSQL
// only on first bootstrap, so running this on every start lets existing shops
// pick up new columns in place.
func (s *postgresStore) EnsureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS items (
			id text PRIMARY KEY,
			name text NOT NULL,
			price_usdt numeric(36,18) NOT NULL,
			stock int NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id text PRIMARY KEY,
			buyer_wallet text NOT NULL,
			tx_hash text,
			amount_usdt numeric(36,18) NOT NULL,
			created_at timestamptz DEFAULT now()
		)`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'pending'`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS item_id text`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS item_quantity int`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS verified_at timestamptz`,
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func (s *postgresStore) ListItems(ctx context.Context) ([]item, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, price_usdt::text, stock FROM items ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.Price, &it.Stock); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *postgresStore) CreateItem(ctx context.Context, it item) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO items (id, name, price_usdt, stock) VALUES ($1, $2, $3::numeric, $4)`,
		it.ID, it.Name, it.Price, it.Stock)
	return err
}

func (s *postgresStore) UpdateItem(ctx context.Context, id string, it item) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE items SET name = $1, price_usdt = $2::numeric, stock = $3 WHERE id = $4`,
		it.Name, it.Price, it.Stock, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

func (s *postgresStore) DeleteItem(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound
	}
	return nil
}

func (s *postgresStore) ListOrders(ctx context.Context) ([]order, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, buyer_wallet, tx_hash, amount_usdt::text, status,
		        COALESCE(item_id, ''), COALESCE(item_quantity, 0), created_at
		 FROM orders ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []order{}
	for rows.Next() {
		var o order
		if err := rows.Scan(&o.ID, &o.BuyerWallet, &o.TxHash, &o.AmountUSDT,
			&o.Status, &o.ItemID, &o.ItemQuantity, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *postgresStore) GetOrder(ctx context.Context, id string) (order, error) {
	var o order
	err := s.pool.QueryRow(ctx,
		`SELECT id, buyer_wallet, tx_hash, amount_usdt::text, status,
		        COALESCE(item_id, ''), COALESCE(item_quantity, 0), created_at
		 FROM orders WHERE id = $1`, id).
		Scan(&o.ID, &o.BuyerWallet, &o.TxHash, &o.AmountUSDT,
			&o.Status, &o.ItemID, &o.ItemQuantity, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return order{}, errNotFound
	}
	return o, err
}

func (s *postgresStore) CreateOrder(ctx context.Context, o order) error {
	var stock int
	err := s.pool.QueryRow(ctx, `SELECT stock FROM items WHERE id = $1`, o.ItemID).Scan(&stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if stock < o.ItemQuantity {
		return errInsufficientStock
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO orders (id, buyer_wallet, tx_hash, amount_usdt, item_id, item_quantity, status)
		 VALUES ($1, $2, $3, $4::numeric, $5, $6, 'pending')`,
		o.ID, o.BuyerWallet, o.TxHash, o.AmountUSDT, o.ItemID, o.ItemQuantity)
	return err
}

func (s *postgresStore) ListPendingOrders(ctx context.Context) ([]pendingOrder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tx_hash, amount_usdt::text, COALESCE(item_id, ''), COALESCE(item_quantity, 0)
		 FROM orders WHERE status = 'pending' AND tx_hash IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []pendingOrder
	for rows.Next() {
		var o pendingOrder
		if err := rows.Scan(&o.id, &o.txHash, &o.amount, &o.itemID, &o.qty); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ConfirmOrder claims the order with a conditional UPDATE: only the transaction
// that flips it out of 'pending' proceeds to decrement stock, so concurrent
// replica sweeps adjust stock exactly once. Oversell between order and
// confirmation fails the order rather than driving stock negative.
func (s *postgresStore) ConfirmOrder(ctx context.Context, orderID, itemID string, qty int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'confirmed', verified_at = now()
		 WHERE id = $1 AND status = 'pending'`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx) // already handled by another sweep/replica
	}

	var stock int
	if err := tx.QueryRow(ctx, `SELECT stock FROM items WHERE id = $1 FOR UPDATE`, itemID).Scan(&stock); err != nil {
		return err
	}
	if stock < qty {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'failed' WHERE id = $1`, orderID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE items SET stock = stock - $1 WHERE id = $2`, qty, itemID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *postgresStore) FailOrder(ctx context.Context, orderID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orders SET status = 'failed' WHERE id = $1`, orderID)
	return err
}
