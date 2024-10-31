package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	MetricHttpConnection  = must(meter.Int64Counter("http.client.connection"))
	MetricConnectDuration = must(meter.Float64Histogram("http.client.connect.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to perform HTTP connection"),
	))
	MetricDNSDuration = must(meter.Float64Histogram("http.client.dns.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to resolve DNS name"),
	))
	MetricIdleDuration = must(meter.Float64Histogram("http.client.idle_connection.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration that the connection was idle"),
	))
	MetricTimeToFirstByte = must(meter.Float64Histogram("http.client.ttfb.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Time to first byte"),
	))
	MetricTLSHandshakeDuration = must(meter.Float64Histogram("http.client.tls_handshake.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to negotiate the TLS handshake"),
	))
)

type TracingRoundTripper struct {
	Transport http.RoundTripper
}

func (t *TracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	req = req.WithContext(httptrace.WithClientTrace(ctx, newTracer(ctx)))
	return t.Transport.RoundTrip(req)
}

func newTracer(ctx context.Context) *httptrace.ClientTrace {
	requestStart := time.Now()

	var dnsStart, connectStart, tlsStart time.Time
	return &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			MetricTimeToFirstByte.Record(ctx, time.Since(requestStart).Seconds())
		},
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			MetricDNSDuration.Record(ctx, time.Since(dnsStart).Seconds())
		},
		ConnectStart: func(_, _ string) {
			connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			MetricConnectDuration.Record(ctx, time.Since(connectStart).Seconds())
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			MetricTLSHandshakeDuration.Record(ctx, time.Since(tlsStart).Seconds())
		},
		GotConn: func(gci httptrace.GotConnInfo) {
			MetricHttpConnection.Add(ctx, 1, metric.WithAttributes(
				attribute.Bool("reused", gci.Reused),
				attribute.Bool("was_idle", gci.WasIdle),
			))
		},
	}
}
