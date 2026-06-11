package main

import (
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Test404MetricCarriesEndpoint guards the spec 4.1 "404s with their endpoints"
// metric: a request that matches no route must be counted under its raw URL
// path (not "unknown"), so the dashboard's 404 panel can show the endpoint.
func Test404MetricCarriesEndpoint(t *testing.T) {
	r := buildRouter(testStore, config{TokenDecimals: 6})

	const missing = "/no/such/endpoint"
	if w := do(r, http.MethodGet, missing, nil); w.Code != http.StatusNotFound {
		t.Fatalf("unmatched route = %d, want 404", w.Code)
	}

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(http.MethodGet, missing, "404"))
	if got < 1 {
		t.Fatalf("shop_http_requests_total{route=%q,status=\"404\"} = %v, want >= 1", missing, got)
	}
}
