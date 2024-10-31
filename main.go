package main

import (
	"context"
	"crypto/tls"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
)

const (
	packageName    = "github.com/davidbacisin/golang-http-connections"
	targetHost     = "https://www.google.com/"
	defaultTimeout = 500 * time.Millisecond
)

var (
	logger          = otelslog.NewLogger(packageName)
	meter           = otel.Meter(packageName)
	durationBuckets = []float64{0.0001, 0.00025, 0.0005, 0.00075, 0.001, 0.0025, 0.005, 0.0075, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}

	MetricNumGoroutines = must(meter.Int64Gauge("go.goroutine.count"))
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

	client := newHttp11ClientKeepAlive()
	client.Transport = &TracingRoundTripper{
		Transport: client.Transport,
	}
	runBenchmark(client)
}

func newDefaultClient() *http.Client {
	client := http.DefaultClient
	client.Transport = &TracingRoundTripper{
		Transport: http.DefaultTransport,
	}
	return client
}

func newHttp11ClientKeepAlive() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}
}

func runBenchmark(client *http.Client) {
	const duration = 5 * time.Second

	ctx := context.Background()

	t := time.NewTicker(time.Second)
	defer t.Stop()
	go func() {
		for range t.C {
			MetricNumGoroutines.Record(ctx, int64(runtime.NumGoroutine()))
		}
	}()

	start := time.Now()
	iter := 0
	for time.Since(start) < duration {
		func() {
			resp, err := client.Get(targetHost)
			if err != nil {
				logger.ErrorContext(resp.Request.Context(), "request error", "error", err)
				return
			}
			defer resp.Body.Close()
		}()
		iter++
	}

	stdlog.Printf("Performed %d iterations", iter)
}
