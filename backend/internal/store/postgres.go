package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresStore struct {
	pool *pgxpool.Pool // ne otvara novu konekciju stalno, nego ima pool
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

// za readiness probe
func (s *postgresStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.pool.Ping(pingCtx)
}

// idempotentne migracije, sve je if not exist, to je bitno jer se kolone status i verified
// tek kasnije popune
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

func (s *postgresStore) ListItems(ctx context.Context) ([]Item, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, price_usdt::text, stock FROM items ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Price, &it.Stock); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *postgresStore) CreateItem(ctx context.Context, it Item) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO items (id, name, price_usdt, stock) VALUES ($1, $2, $3::numeric, $4)`,
		it.ID, it.Name, it.Price, it.Stock)
	// 23505 = unique_violation: vec postoji item sa istim id-jem
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAlreadyExists
	}
	return err
}

func (s *postgresStore) UpdateItem(ctx context.Context, id string, it Item) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE items SET name = $1, price_usdt = $2::numeric, stock = $3 WHERE id = $4`,
		it.Name, it.Price, it.Stock, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postgresStore) DeleteItem(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postgresStore) ListOrders(ctx context.Context) ([]Order, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, buyer_wallet, tx_hash, amount_usdt::text, status,
		        COALESCE(item_id, ''), COALESCE(item_quantity, 0), created_at
		 FROM orders ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.BuyerWallet, &o.TxHash, &o.AmountUSDT,
			&o.Status, &o.ItemID, &o.ItemQuantity, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *postgresStore) GetOrder(ctx context.Context, id string) (Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx,
		`SELECT id, buyer_wallet, tx_hash, amount_usdt::text, status,
		        COALESCE(item_id, ''), COALESCE(item_quantity, 0), created_at
		 FROM orders WHERE id = $1`, id).
		Scan(&o.ID, &o.BuyerWallet, &o.TxHash, &o.AmountUSDT,
			&o.Status, &o.ItemID, &o.ItemQuantity, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	return o, err
}

// rezervise i zabelezi order u jednoj transakciji, neda dvojici da se zaglave
func (s *postgresStore) CreateOrder(ctx context.Context, o Order) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // ako se ne comitu-uje sve se ponisti

	var price string       // ovde radimo onaj guarded decrement
	err = tx.QueryRow(ctx, // where uslov, samo ako ima dovoljno, where je atomski u bazi
		`UPDATE items SET stock = stock - $1 WHERE id = $2 AND stock >= $1 RETURNING price_usdt::text`,
		o.ItemQuantity, o.ItemID).Scan(&price) // cena se cita iz baze a ne iz requesta, da nema prevare
	if errors.Is(err, pgx.ErrNoRows) {
		// ili item ne postoji ili ga nema dovoljno, proveri koji je slucaj
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM items WHERE id = $1)`, o.ItemID).Scan(&exists); err != nil {
			return Order{}, err
		}
		if !exists {
			return Order{}, ErrNotFound // ako ne postoji
		}
		return Order{}, ErrInsufficientStock // ako ga nema dovoljno
	}
	if err != nil {
		return Order{}, err
	}

	if o.AmountUSDT, err = OrderTotal(price, o.ItemQuantity); err != nil {
		return Order{}, err
	}
	o.Status = "pending"
	if err := tx.QueryRow(ctx,
		`INSERT INTO orders (id, buyer_wallet, tx_hash, amount_usdt, item_id, item_quantity, status)
		 VALUES ($1, $2, $3, $4::numeric, $5, $6, 'pending') RETURNING created_at`,
		o.ID, o.BuyerWallet, o.TxHash, o.AmountUSDT, o.ItemID, o.ItemQuantity).Scan(&o.CreatedAt); err != nil {
		return Order{}, err
	}
	return o, tx.Commit(ctx)
}

// ako je pending i nemamo hash samo onda mozemo da zakacimo hash
func (s *postgresStore) SetOrderTx(ctx context.Context, orderID, txHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE orders SET tx_hash = $1 WHERE id = $2 AND status = 'pending' AND tx_hash IS NULL`,
		txHash, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// za slucaj da niko ne preda isti hash na 2 porudzbine, zbir sa tog hashsa mora da se poklapa
func (s *postgresStore) ListActiveAmountsForTx(ctx context.Context, txHash string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT amount_usdt::text FROM orders WHERE tx_hash = $1 AND status <> 'failed'`, txHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *postgresStore) ListPendingOrders(ctx context.Context) ([]PendingOrder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, COALESCE(tx_hash, ''), amount_usdt::text, COALESCE(item_id, ''),
		        COALESCE(item_quantity, 0), created_at
		 FROM orders WHERE status = 'pending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingOrder
	for rows.Next() {
		var o PendingOrder
		if err := rows.Scan(&o.ID, &o.TxHash, &o.Amount, &o.ItemID, &o.Qty, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// iz pending u confirmed
func (s *postgresStore) ConfirmOrder(ctx context.Context, orderID string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE orders SET status = 'confirmed', verified_at = now()
		 WHERE id = $1 AND status = 'pending'`, orderID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// iz pending u failed
func (s *postgresStore) FailOrder(ctx context.Context, orderID, itemID string, qty int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'failed' WHERE id = $1 AND status = 'pending'`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx) // already handled by another sweep/replica
	}
	if itemID != "" && qty > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE items SET stock = stock + $1 WHERE id = $2`, qty, itemID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
