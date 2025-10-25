// Package telemetry provides distributed tracing setup using OpenTelemetry.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracerProvider *sdktrace.TracerProvider
	isTracingEnabled = false
)

// InitTracing initializes OpenTelemetry tracing with OTLP/gRPC exporter.
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is disabled (no-op).
func InitTracing(serviceName, serviceVersion string) (func(), error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		slog.Info("tracing disabled: OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return func() {}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(), // Use insecure for local development
		otlptracegrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider with batch span processor
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample all traces; adjust for production
	)

	otel.SetTracerProvider(tracerProvider)
	isTracingEnabled = true
	slog.Info("tracing initialized", slog.String("service", serviceName), slog.String("endpoint", endpoint))

	// Return shutdown function
	return func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shutdown tracer provider", slog.Any("err", err))
		}
	}, nil
}

// IsTracingEnabled returns whether tracing is active.
func IsTracingEnabled() bool {
	return isTracingEnabled
}

// StartSpan is a helper to start a span with common attributes and correlation ID.
func StartSpan(ctx context.Context, tracerName, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	
	// Add correlation ID if present
	if corr := GetCorrelation(ctx); corr != "" {
		attrs = append(attrs, attribute.String("correlation_id", corr))
	}
	
	ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
	return ctx, span
}

// RecordError records an error on the span and sets error status.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetSpanSuccess sets span status to OK.
func SetSpanSuccess(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}
