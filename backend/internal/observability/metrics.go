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
	httpRequestsTotal = promauto.NewCounterVec( // broj zahteva
		prometheus.CounterOpts{
			Name: "shop_http_requests_total",
			Help: "Total HTTP requests received, partitioned by method, route and status.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec( // histogram latencije
		prometheus.HistogramOpts{
			Name:    "shop_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)
	httpResponseBytes = promauto.NewCounterVec( // protok
		prometheus.CounterOpts{
			Name: "shop_http_response_bytes_total",
			Help: "Total bytes written in HTTP response bodies, partitioned by method, route and status.",
		},
		[]string{"method", "route", "status"},
	)
)

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()              // izvrsi handler
		route := c.FullPath() // koja ruta je mathcovana
		if route == "" {
			// ako nema matchovane rute onda je not found i belezino sirovu putanju
			// da pokazemo sta je gadjano
			if c.Writer.Status() == http.StatusNotFound {
				route = c.Request.URL.Path
			} else {
				route = "unknown"
			}
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()                          // inkrement
		httpRequestDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds()) // trajanje
		if size := c.Writer.Size(); size > 0 {                                                            // bajtovi
			httpResponseBytes.WithLabelValues(c.Request.Method, route, status).Add(float64(size))
		}
	}
}
