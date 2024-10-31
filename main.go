package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
)

const (
	targetHost     = "https://health.aws.amazon.com/health/status"
	defaultTimeout = 500 * time.Millisecond
)

var (
	meter           = otel.Meter("github.com/davidbacisin/golang-http-connections")
	durationBuckets = []float64{0.0001, 0.00025, 0.0005, 0.00075, 0.001, 0.0025, 0.005, 0.0075, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

	MetricNumGoroutines   = must(meter.Int64Gauge("go.goroutine.count"))
	MetricRequestDuration = must(meter.Float64Histogram("http.client.request.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to perform HTTP connection"),
	))
)

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	defer initOtel(ctx)(ctx)

	client := http.DefaultClient
	client.Transport = &TracingRoundTripper{
		Transport: http.DefaultTransport,
	}

	runBenchmark(client)
}

/*
func makeHttp11ClientNoKeepAlive() *http.Client {
	transport := http.DefaultTransport

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}
}*/

func runBenchmark(client *http.Client) {
	const duration = 10 * time.Second

	ctx := context.Background()

	t := time.NewTicker(time.Second)
	defer t.Stop()
	go func() {
		for range t.C {
			MetricNumGoroutines.Record(ctx, int64(runtime.NumGoroutine()))
		}
	}()

	start := time.Now()
	for time.Since(start) < duration {
		func() {
			reqStart := time.Now()
			resp, err := client.Get(targetHost)
			defer func() {
				opts := make([]metric.RecordOption, 0, 1)
				if err != nil {
					opts = append(opts, metric.WithAttributes(semconv.ErrorTypeKey.Int(resp.StatusCode)))
				}
				MetricRequestDuration.Record(ctx, time.Since(reqStart).Seconds(), opts...)
				resp.Body.Close()
			}()
		}()
	}
}
