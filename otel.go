package main

import (
	"context"
	"errors"
	stdlog "log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func initOtel(ctx context.Context, rs *resource.Resource) (shutdown func(context.Context) error) {
	const exportInterval = 1 * time.Second
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	logExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
	if err != nil {
		stdlog.Fatalf("could not initialize log exporter: %+v", err)
		return
	}

	loggerProcessor := log.NewBatchProcessor(logExporter, log.WithExportInterval(exportInterval))
	loggerProvider := log.NewLoggerProvider(log.WithResource(rs), log.WithProcessor(loggerProcessor))
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithInsecure())
	if err != nil {
		stdlog.Fatalf("could not initialize meter exporter: %+v", err)
		return
	}

	meterReader := metric.NewPeriodicReader(metricExporter, metric.WithInterval(exportInterval))
	meterProvider := metric.NewMeterProvider(metric.WithResource(rs), metric.WithReader(meterReader))
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	return
}
