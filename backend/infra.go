package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type config struct {
	Port        string
	DatabaseURL string

	// Web3 payment (D12). WalletAddress is this shop's on-chain recipient — the
	// operator injects it from Shop.spec.walletAddress. Token/RPC default to the
	// project's Sepolia test setup and can be overridden by env.
	WalletAddress string
	RPCURL        string
	TokenContract string
	TokenDecimals int
}

func loadConfig() (config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	cfg := config{
		Port:          port,
		DatabaseURL:   dbURL,
		WalletAddress: os.Getenv("WALLET_ADDRESS"),
		RPCURL:        envOr("SEPOLIA_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com"),
		TokenContract: envOr("USDT_CONTRACT", "0x74b0ef872a9f1a4bbb07a01a6b4376379737ff6f"),
		TokenDecimals: 6,
	}
	if d := os.Getenv("USDT_DECIMALS"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil {
			return config{}, fmt.Errorf("USDT_DECIMALS: %w", err)
		}
		cfg.TokenDecimals = n
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ensureSchema creates the application tables and adds the D12 payment columns
// idempotently. The operator seeds base tables via CNPG postInitApplicationSQL
// only on first bootstrap, so running this on every start lets existing shops
// pick up new columns in place.
func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
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
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
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
	return pool, nil
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			// user_agent lets Loki count unique visitors as distinct
			// (client_ip, user_agent) pairs (spec 4.1.d).
			"user_agent", c.Request.UserAgent(),
		)
	}
}

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shop_http_requests_total",
			Help: "Total HTTP requests received, partitioned by method, route and status.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shop_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
	httpResponseBytes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shop_http_response_bytes_total",
			Help: "Total bytes written in HTTP response bodies, partitioned by method, route and status.",
		},
		[]string{"method", "route", "status"},
	)
)

func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
		// Writer.Size() is -1 until a body is written (e.g. Gin's NoRoute 404
		// writes its body after this middleware runs). Adding a negative value
		// panics a Prometheus counter, so only record real, non-negative writes.
		if size := c.Writer.Size(); size > 0 {
			httpResponseBytes.WithLabelValues(c.Request.Method, route, status).Add(float64(size))
		}
	}
}
