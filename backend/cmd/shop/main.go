// Command shop is the Shop backend: a REST API for one Shop tenant's items and
// orders, a storefront SPA server, and a background sweep that confirms USDT
// payments on Sepolia. Configuration is read from the environment so the
// operator can inject the database connection string and on-chain settings.
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

	"github.com/shophub-devoops/shop/backend/internal/config"
	"github.com/shophub-devoops/shop/backend/internal/httpapi"
	"github.com/shophub-devoops/shop/backend/internal/notify"
	"github.com/shophub-devoops/shop/backend/internal/observability"
	"github.com/shophub-devoops/shop/backend/internal/payment"
	"github.com/shophub-devoops/shop/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}
	if cfg.AdminPassword == "" {
		slog.Warn("ADMIN_PASSWORD not set — admin endpoints are NOT protected (dev mode)")
	}

	st, err := store.New(context.Background(), cfg.DatabaseURL, cfg.DBName)
	if err != nil {
		slog.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.EnsureSchema(context.Background()); err != nil {
		slog.Error("ensure schema", "err", err)
		os.Exit(1)
	}

	// Web3 payment verifier (D12): a background sweep that confirms pending
	// orders against Sepolia and expires abandoned ones (releasing their stock
	// reservation). Without on-chain config the sweep still runs expiry-only.
	verifierCtx, stopVerifier := context.WithCancel(context.Background())
	defer stopVerifier()
	v, err := payment.NewVerifier(cfg)
	if err != nil {
		slog.Warn("on-chain verification disabled (sweep runs expiry-only)", "err", err)
	}
	go (&payment.PaymentVerifier{
		Store:    st,
		V:        v,
		Decimals: cfg.TokenDecimals,
		// nil when the shop has no Discord channel — notifications are skipped.
		Notify: notify.NewDiscord(cfg.DiscordWebhookURL),
	}).Run(verifierCtx)

	shutdownTracing, err := observability.InitTracing(context.Background())
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
		Handler:           httpapi.BuildRouter(st, cfg),
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
