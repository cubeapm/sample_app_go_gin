package main

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func setupOTelSDK(ctx context.Context) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	hostname, _ := os.Hostname()
	res := resource.Default()
	res = resource.NewWithAttributes(
		res.SchemaURL(),
		append(res.Attributes(), attribute.KeyValue{
			Key:   semconv.HostNameKey,
			Value: attribute.StringValue(hostname),
		})...,
	)

	// Set up trace provider.
	tracerProvider, err := newTraceProvider(ctx, res)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up meter provider.
	meterProvider, err := newMeterProvider(ctx, res)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	err = host.Start()
	if err != nil {
		handleErr(err)
		return
	}
	// enable deprecated runtime metrics to get garbage collection details
	os.Setenv("OTEL_GO_X_DEPRECATED_RUNTIME_METRICS", "true")
	err = runtime.Start(runtime.WithMeterProvider(meterProvider))
	if err != nil {
		handleErr(err)
		return
	}

	return
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTraceProvider(ctx context.Context, res *resource.Resource) (*trace.TracerProvider, error) {
	var traceExporter trace.SpanExporter
	var err error
	if os.Getenv("OTEL_LOG_LEVEL") == "debug" {
		traceExporter, err = stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
	} else {
		traceExporter, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(traceExporter),
	)
	return traceProvider, nil
}

func newMeterProvider(ctx context.Context, res *resource.Resource) (*metric.MeterProvider, error) {
	var metricExporter metric.Exporter
	var err error
	var opts []metric.PeriodicReaderOption
	if os.Getenv("OTEL_LOG_LEVEL") == "debug" {
		metricExporter, err = stdoutmetric.New(
			stdoutmetric.WithPrettyPrint(),
		)
		// Default is 1m. Set to 10s to get output faster.
		opts = append(opts, metric.WithInterval(10*time.Second))
	} else {
		metricExporter, err = otlpmetrichttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(
			metric.NewPeriodicReader(metricExporter, opts...),
		),
	)
	return meterProvider, nil
}
