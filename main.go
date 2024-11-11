package main

import (
	"context"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"golang.org/x/sync/semaphore"
)

const (
	packageName    = "github.com/davidbacisin/golang-http-connections"
	targetHost     = "https://localhost:8443/"
	defaultTimeout = 700 * time.Millisecond
	concurrency    = 10
)

var (
	logger          = otelslog.NewLogger(packageName)
	meter           = otel.Meter(packageName)
	durationBuckets = []float64{0.0001, 0.00025, 0.0005, 0.00075, 0.001, 0.0025, 0.005, 0.0075, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

	CounterNumSelfGoroutines atomic.Int64

	MetricNumGoroutines     = must(meter.Int64Gauge("go.goroutine.count"))
	MetricNumSelfGoroutines = must(meter.Int64Gauge("go.goroutine.count.self", metric.WithDescription("the number of active goroutines started directly by the process, exclusive of those started by the standard library")))
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

	client, name := NewHttp2KeepAlive()

	rs, _ := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("golang-http-connections"),
			semconv.ServiceInstanceIDKey.String(name),
		),
	)

	defer initOtel(ctx, rs)(ctx)

	// Add request tracing to the client
	client.Transport = &TracingRoundTripper{
		Transport: client.Transport,
	}
	iter := runBenchmark(ctx, client)
	stdlog.Printf("Performed %d iterations with client %s", iter, name)
}

func runBenchmark(ctx context.Context, client *http.Client) int64 {
	const duration = 5 * time.Second

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	go func() {
		for range t.C {
			MetricNumGoroutines.Record(ctx, int64(runtime.NumGoroutine()))
			MetricNumSelfGoroutines.Record(ctx, CounterNumSelfGoroutines.Load())
		}
	}()

	sem := semaphore.NewWeighted(concurrency)

	start := time.Now()
	var iter int64
	for time.Since(start) < duration {
		select {
		case <-ctx.Done():
			stdlog.Printf("cancellation signal received")
			return iter
		default:
			sem.Acquire(ctx, 1)
			go func() {
				CounterNumSelfGoroutines.Add(1)
				defer CounterNumSelfGoroutines.Add(-1)
				defer sem.Release(1)

				resp, err := client.Get(targetHost)
				if err != nil {
					logger.Error("request error", "error", err)
					return
				}
				defer resp.Body.Close()

				// Note that if we don't read the full response body, then the HTTP connection probably won't be reused.
				io.Copy(io.Discard, resp.Body)
			}()
			iter++
		}
	}

	return iter
}
