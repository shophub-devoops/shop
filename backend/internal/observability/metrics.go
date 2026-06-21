// Package observability holds the Shop backend's cross-cutting telemetry:
// Prometheus metrics, structured request logging, and OTLP tracing.
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

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

// Middleware records request count, latency and response bytes per route.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			// No matched route. For 404s use the raw request path so the
			// dashboard's "404 endpoints" panel shows the actual endpoint
			// (spec 4.1) — bounded enough since only misses hit this branch.
			// Everything else (e.g. SPA fallback 200s) stays "unknown" to keep
			// label cardinality down.
			if c.Writer.Status() == http.StatusNotFound {
				route = c.Request.URL.Path
			} else {
				route = "unknown"
			}
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
