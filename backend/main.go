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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
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
	if cfg.AdminPassword == "" {
		slog.Warn("ADMIN_PASSWORD not set — admin endpoints are NOT protected (dev mode)")
	}

	store, err := newStore(context.Background(), cfg.DatabaseURL, cfg.DBName)
	if err != nil {
		slog.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.EnsureSchema(context.Background()); err != nil {
		slog.Error("ensure schema", "err", err)
		os.Exit(1)
	}

	// Web3 payment verifier (D12): a background sweep that confirms pending
	// orders against Sepolia and expires abandoned ones (releasing their stock
	// reservation). Without on-chain config the sweep still runs expiry-only.
	verifierCtx, stopVerifier := context.WithCancel(context.Background())
	defer stopVerifier()
	v, err := newVerifier(cfg)
	if err != nil {
		slog.Warn("on-chain verification disabled (sweep runs expiry-only)", "err", err)
	}
	go (&paymentVerifier{store: store, v: v, decimals: cfg.TokenDecimals}).run(verifierCtx)

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
		Handler:           buildRouter(store, cfg),
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
func buildRouter(store Store, cfg config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(otelgin.Middleware(serviceName()))
	r.Use(requestLogger())
	r.Use(metricsMiddleware())

	r.GET("/probe/liveness", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/probe/readiness", readinessHandler(store))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Admin gate (spec: Shop "Admin" role). The operator injects ADMIN_PASSWORD
	// from the per-shop admin Secret; without it (local dev, tests) the gate is
	// a pass-through.
	admin := newAdminAuth(cfg.AdminPassword)

	api := r.Group("/api")
	{
		api.GET("/shop-info", shopInfo(cfg))
		api.POST("/auth/login", adminLogin(admin))

		items := api.Group("/items")
		items.GET("", listItems(store))
		items.POST("", admin.require(), createItem(store))
		items.PUT("/:id", admin.require(), updateItem(store))
		items.DELETE("/:id", admin.require(), deleteItem(store))

		orders := api.Group("/orders")
		// Listing all orders is admin-only (spec 2.2); buyers poll their own
		// order by id, and order creation stays open to the storefront.
		orders.GET("", admin.require(), listOrders(store))
		orders.POST("", createOrder(store))
		orders.GET("/:id", getOrder(store))
	}

	mountStorefront(r)
	return r
}

// mountStorefront serves the built frontend SPA from WEB_DIR (populated in the
// unified container image) so the shop is a real storefront at "/", not just an
// API. No-op when no bundled UI is present (local API-only runs / tests).
func mountStorefront(r *gin.Engine) {
	webDir := envOr("WEB_DIR", "/app/web")
	index := filepath.Join(webDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return
	}
	r.Static("/assets", filepath.Join(webDir, "assets"))
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if c.Request.Method != http.MethodGet ||
			strings.HasPrefix(p, "/api") || strings.HasPrefix(p, "/metrics") || strings.HasPrefix(p, "/probe") {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(index)
	})
}

// readinessHandler reports ready only when the store can answer a Ping. Liveness
// stays cheap and unconditional so kubelet doesn't restart us during a brief
// database blip.
func readinessHandler(store Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := store.Ping(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "db not ready: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	}
}
