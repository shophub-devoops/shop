package observability

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracing wires an OTLP/HTTP trace exporter when OTEL_EXPORTER_OTLP_ENDPOINT
// is set (the operator points it at Tempo). Returns a shutdown func — a no-op if
// tracing is disabled, so local runs don't need a collector.
//
// The exporter reads its endpoint from OTEL_EXPORTER_OTLP_ENDPOINT per the OTel
// spec; the operator sets that plus OTEL_SERVICE_NAME on the Shop container.
func InitTracing(ctx context.Context) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop, nil
	}

	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", ServiceName())),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// ServiceName labels traces per Shop tenant (operator sets OTEL_SERVICE_NAME to
// the shop name); falls back to a generic name for local runs.
func ServiceName() string {
	if n := os.Getenv("OTEL_SERVICE_NAME"); n != "" {
		return n
	}
	return "shop"
}
