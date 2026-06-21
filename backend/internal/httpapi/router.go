// Package httpapi wires the Shop backend's HTTP surface: item/order endpoints,
// the admin gate, the storefront SPA, and the metrics/probe routes.
package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/shophub-devoops/shop/backend/internal/config"
	"github.com/shophub-devoops/shop/backend/internal/observability"
	"github.com/shophub-devoops/shop/backend/internal/payment"
	"github.com/shophub-devoops/shop/backend/internal/store"
)

// BuildRouter wires every HTTP route. Handlers themselves live in items.go and
// orders.go so this stays a single source of truth for the API surface.
func BuildRouter(s store.Store, cfg config.Config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(otelgin.Middleware(observability.ServiceName()))
	r.Use(observability.RequestLogger())
	r.Use(observability.Middleware())

	r.GET("/probe/liveness", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/probe/readiness", readinessHandler(s))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Admin gate (spec: Shop "Admin" role). The operator injects ADMIN_PASSWORD
	// from the per-shop admin Secret; without it (local dev, tests) the gate is
	// a pass-through.
	admin := NewAdminAuth(cfg.AdminPassword)

	api := r.Group("/api")
	{
		api.GET("/shop-info", payment.ShopInfo(cfg))
		api.POST("/auth/login", adminLogin(admin))

		items := api.Group("/items")
		items.GET("", listItems(s))
		items.POST("", admin.require(), createItem(s))
		items.PUT("/:id", admin.require(), updateItem(s))
		items.DELETE("/:id", admin.require(), deleteItem(s))

		orders := api.Group("/orders")
		// Listing all orders is admin-only (spec 2.2); buyers poll their own
		// order by id, and order creation stays open to the storefront.
		orders.GET("", admin.require(), listOrders(s))
		orders.POST("", createOrder(s))
		orders.GET("/:id", getOrder(s))
		// Buyer attaches the payment tx hash after paying (reserve → pay → attach).
		orders.POST("/:id/tx", attachOrderTx(s))
	}

	mountStorefront(r)
	return r
}

// mountStorefront serves the built frontend SPA from WEB_DIR (populated in the
// unified container image) so the shop is a real storefront at "/", not just an
// API. No-op when no bundled UI is present (local API-only runs / tests).
func mountStorefront(r *gin.Engine) {
	webDir := config.EnvOr("WEB_DIR", "/app/web")
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
func readinessHandler(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := s.Ping(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "db not ready: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	}
}
