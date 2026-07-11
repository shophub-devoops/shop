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

func BuildRouter(s store.Store, cfg config.Config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())                                  // hvata panic, 500 umesto pada
	r.Use(otelgin.Middleware(observability.ServiceName())) // za tracing, span i tempo za svkaih zahtev
	r.Use(observability.RequestLogger())                   // logovi za Loki
	r.Use(observability.Middleware())                      // Prometheus metrike
	// ova 4 reda iznad su observability stack, to se sve automatski kaci na svaku rutu
	r.GET("/probe/liveness", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/probe/readiness", readinessHandler(s)) // ovaj pinguje bazu
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	admin := NewAdminAuth(cfg.AdminPassword)

	api := r.Group("/api")
	{
		api.GET("/shop-info", payment.ShopInfo(cfg))
		api.POST("/auth/login", adminLogin(admin))

		items := api.Group("/items")
		items.GET("", listItems(s))
		items.POST("", admin.require(), createItem(s)) // samo admin sme
		items.PUT("/:id", admin.require(), updateItem(s))
		items.DELETE("/:id", admin.require(), deleteItem(s))

		orders := api.Group("/orders")
		orders.GET("", admin.require(), listOrders(s)) // samo admin vidi sve ordere
		orders.POST("", createOrder(s))
		orders.GET("/:id", getOrder(s))
		orders.POST("/:id/tx", attachOrderTx(s))
	}

	mountStorefront(r)
	return r
}

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

func readinessHandler(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := s.Ping(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "db not ready: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	}
}
