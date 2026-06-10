package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type config struct {
	Port        string
	DatabaseURL string
	// DBName is the database the Mongo client uses (operator injects SHOP_DB_NAME
	// = shop name). Ignored by Postgres, whose URI already names the database.
	DBName string

	// Web3 payment (D12). WalletAddress is this shop's on-chain recipient — the
	// operator injects it from Shop.spec.walletAddress. Token/RPC default to the
	// project's Sepolia test setup and can be overridden by env.
	WalletAddress string
	RPCURL        string
	TokenContract string
	TokenDecimals int

	// AdminPassword guards item writes and order listing. Injected by the
	// operator from the per-shop admin Secret; empty disables the gate (dev).
	AdminPassword string
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
		DBName:        os.Getenv("SHOP_DB_NAME"),
		WalletAddress: os.Getenv("WALLET_ADDRESS"),
		RPCURL:        envOr("SEPOLIA_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com"),
		TokenContract: envOr("USDT_CONTRACT", "0x74b0ef872a9f1a4bbb07a01a6b4376379737ff6f"),
		TokenDecimals: 6,
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
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
