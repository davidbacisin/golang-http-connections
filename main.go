package main

import (
	"context"
	"io"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/semaphore"
)

const (
	packageName    = "github.com/davidbacisin/golang-http-connections"
	targetHost     = "https://127.0.0.1:8443/"
	defaultTimeout = 700 * time.Millisecond
)

var (
	logger          = otelslog.NewLogger(packageName)
	meter           = otel.Meter(packageName)
	tracer          = otel.Tracer(packageName)
	durationBuckets = []float64{0.0001, 0.00025, 0.0005, 0.00075, 0.001, 0.0025, 0.005, 0.0075, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

	MetricNumGoroutines = must(meter.Int64Gauge("go.goroutine.count"))
	MetricNumActiveVUs  = must(meter.Int64UpDownCounter("scenario.vus.count", metric.WithDescription("the number of active VUs")))
)

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func main() {
	runtime.GOMAXPROCS(4)
	debug.SetMaxThreads(30_000) // increase max threads to prevent crashes due to high thread count

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	scenario := selectScenerio()

	rs, _ := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("golang-http-connections"),
			semconv.ServiceInstanceIDKey.String(scenario.Name),
		),
	)

	shutdownOtel := initOtel(ctx, rs)
	defer func() {
		otelCtx, otelCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer otelCancel()
		shutdownOtel(otelCtx)
	}()

	// Update a few metrics periodically
	metricTicker := time.NewTicker(500 * time.Millisecond)
	defer metricTicker.Stop()
	go func() {
		for range metricTicker.C {
			MetricNumGoroutines.Record(ctx, int64(runtime.NumGoroutine()))

			// Run netstat
			_, err := RecordActiveConnectionCount(ctx)
			if err != nil {
				logger.Error("netstat failed", "error", err)
			}
		}
	}()

	iter := runScenario(ctx, scenario)
	stdlog.Printf("Performed %d iterations from scenario %s", iter, scenario.Name)
}

func selectScenerio() Scenario {
	if len(os.Args) < 2 {
		stdlog.Fatalln("fatal: provide a scenario id as the only unnamed argument")
		return Scenario{}
	}

	id := os.Args[1]
	s, ok := Scenarios[id]
	if !ok {
		ids := maps.Keys(Scenarios)
		slices.Sort(ids)
		stdlog.Fatalf("fatal: provide a valid scenario name, one of: %s", strings.Join(ids, ", "))
	}

	return s
}

func runScenario(ctx context.Context, scenario Scenario) int64 {
	ctx, span := tracer.Start(ctx, "Scenario")
	defer span.End()

	// Add request tracing to the client
	client := scenario.NewClient()
	client.Transport = &TracingRoundTripper{
		Transport: client.Transport,
	}

	var iter int64
	for i, stage := range scenario.Stages {
		stdlog.Printf("Starting %s stage %d", scenario.Name, i)
		ctx, span := tracer.Start(ctx, "Stage", trace.WithAttributes(attribute.Int("index", i)))
		defer span.End()

		dur := must(time.ParseDuration(stage.Duration))
		sem := semaphore.NewWeighted(int64(stage.VUs))
		start := time.Now()
		for time.Since(start) < dur {
			select {
			case <-ctx.Done():
				stdlog.Printf("received cancellation signal")
				return iter
			default:
				sem.Acquire(ctx, 1)
				go func(id int64) {
					defer sem.Release(1)
					MetricNumActiveVUs.Add(ctx, 1)
					defer MetricNumActiveVUs.Add(ctx, -1)

					// Sleep very briefly to give time for connections to return to the idle pool
					time.Sleep(1 * time.Microsecond)

					req, _ := http.NewRequestWithContext(ctx, http.MethodGet, targetHost, http.NoBody)
					resp, err := client.Do(req)
					if err != nil {
						logger.Error("request error", "error", err)
						return
					}
					defer resp.Body.Close()

					// Note that if we don't read the full response body, then the HTTP connection won't be reused.
					io.Copy(io.Discard, resp.Body)
				}(iter)
				iter++
			}
		}
	}

	return iter
}
