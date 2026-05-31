// Shop backend — REST API for items and orders of a single Shop tenant.
//
// Configuration is read from the environment so the operator can inject the
// CNPG-generated connection string. See README.md for the contract.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	pool, err := openPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := ensureSchema(context.Background(), pool); err != nil {
		slog.Error("ensure schema", "err", err)
		os.Exit(1)
	}

	// Web3 payment verifier (D12): a background sweep that confirms pending
	// orders against Sepolia. Disabled (with a warning) if it can't be wired,
	// so the API still serves without on-chain config.
	verifierCtx, stopVerifier := context.WithCancel(context.Background())
	defer stopVerifier()
	if v, err := newVerifier(cfg); err != nil {
		slog.Warn("payment verifier disabled", "err", err)
	} else {
		go (&paymentVerifier{pool: pool, v: v, decimals: cfg.TokenDecimals}).run(verifierCtx)
	}

	shutdownTracing, err := initTracing(context.Background())
	if err != nil {
		slog.Error("init tracing", "err", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(ctx)
	}()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           buildRouter(pool, cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("shop backend listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}

// buildRouter wires every HTTP route. Handlers themselves live in items.go and
// orders.go so this function stays a single source of truth for the API surface.
func buildRouter(pool *pgxpool.Pool, cfg config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(otelgin.Middleware(serviceName()))
	r.Use(requestLogger())
	r.Use(metricsMiddleware())

	r.GET("/probe/liveness", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/probe/readiness", readinessHandler(pool))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := r.Group("/api")
	{
		api.GET("/shop-info", shopInfo(cfg))

		items := api.Group("/items")
		items.GET("", listItems(pool))
		items.POST("", createItem(pool))
		items.PUT("/:id", updateItem(pool))
		items.DELETE("/:id", deleteItem(pool))

		orders := api.Group("/orders")
		orders.GET("", listOrders(pool))
		orders.POST("", createOrder(pool))
		orders.GET("/:id", getOrder(pool))
	}

	return r
}

// readinessHandler reports ready only when the pool can answer a Ping. Liveness
// stays cheap and unconditional so kubelet doesn't restart us during a brief
// Postgres blip.
func readinessHandler(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			c.String(http.StatusServiceUnavailable, "db not ready: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	}
}
