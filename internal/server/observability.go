package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type Observability struct {
	Tracer oteltrace.Tracer
	Meter  metric.Meter

	traceProvider *sdktrace.TracerProvider
	RunCounter    metric.Int64Counter
	SuiteDuration metric.Int64Histogram
	HardGateHits  metric.Int64Counter
	BudgetBlocked metric.Int64Counter
}

func SetupObservability(ctx context.Context, cfg ObservabilityConfig) (*Observability, error) {
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "probe-api"
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}
	sampler := sdktrace.TraceIDRatioBased(cfg.SampleRatio)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	if cfg.OTLPEndpoint != "" {
		exporter, exportErr := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if exportErr != nil {
			return nil, fmt.Errorf("create otlp trace exporter: %w", exportErr)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
		)
	} else {
		slog.Info("otel trace exporter not configured; using local tracer provider")
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	meter := otel.Meter(serviceName)
	tracer := otel.Tracer(serviceName)
	runCounter, _ := meter.Int64Counter("probe_run_total")
	suiteDuration, _ := meter.Int64Histogram("probe_suite_duration_ms")
	hardGateHits, _ := meter.Int64Counter("probe_hard_gate_hits_total")
	budgetBlocked, _ := meter.Int64Counter("probe_budget_block_total")
	return &Observability{
		Tracer:        tracer,
		Meter:         meter,
		traceProvider: tp,
		RunCounter:    runCounter,
		SuiteDuration: suiteDuration,
		HardGateHits:  hardGateHits,
		BudgetBlocked: budgetBlocked,
	}, nil
}

func (o *Observability) Shutdown(ctx context.Context) error {
	if o == nil || o.traceProvider == nil {
		return nil
	}
	return o.traceProvider.Shutdown(ctx)
}

func (o *Observability) MarkRun(ctx context.Context, status string) {
	if o == nil {
		return
	}
	o.RunCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
	))
}

func (o *Observability) MarkSuite(ctx context.Context, suite string, durationMS int64) {
	if o == nil {
		return
	}
	o.SuiteDuration.Record(ctx, durationMS, metric.WithAttributes(
		attribute.String("suite", suite),
	))
}

func (o *Observability) MarkHardGate(ctx context.Context, rule string) {
	if o == nil {
		return
	}
	o.HardGateHits.Add(ctx, 1, metric.WithAttributes(attribute.String("rule", rule)))
}

func (o *Observability) MarkBudgetBlocked(ctx context.Context, reason string) {
	if o == nil {
		return
	}
	o.BudgetBlocked.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
	))
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}
