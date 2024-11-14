package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var (
	MetricHttpConnection       = must(meter.Int64Counter("http.client.connection"))
	MetricHttpActiveRoundtrips = must(meter.Int64UpDownCounter("http.client.active_roundtrip"))
	MetricConnectDuration      = must(meter.Float64Histogram("http.client.connect.duration",
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
	MetricRequestDuration = must(meter.Float64Histogram("http.client.request.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
		metric.WithDescription("Duration to perform HTTP connection"),
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

	MetricTcpNewConnection   = must(meter.Int64Counter("tcp.connection.new"))
	MetricTcpCloseConnection = must(meter.Int64Counter("tcp.connection.close"))
	MetricHttpResponseClose  = must(meter.Int64Counter("http.client.response.close"))
)

type TracingConn struct {
	net.Conn
	ctx context.Context
}

func (c *TracingConn) Close() error {
	MetricTcpCloseConnection.Add(c.ctx, 1)
	return c.Conn.Close()
}

type TracingDialer struct {
	net.Dialer
}

func (d *TracingDialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	MetricTcpNewConnection.Add(ctx, 1)
	c, err := d.Dialer.DialContext(ctx, network, address)
	return &TracingConn{
		Conn: c,
		ctx:  ctx,
	}, err
}

type TracingRoundTripper struct {
	Transport http.RoundTripper
}

func (t *TracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	reqStart := time.Now()
	ctx := req.Context()
	MetricHttpActiveRoundtrips.Add(ctx, 1)
	defer MetricHttpActiveRoundtrips.Add(ctx, -1)

	req = req.WithContext(httptrace.WithClientTrace(ctx, newTracer(ctx, t)))
	resp, err := t.Transport.RoundTrip(req)

	attrs := make([]attribute.KeyValue, 0, 2)
	if err != nil {
		attrs = append(attrs, semconv.ErrorTypeOther)
	} else {
		attrs = append(attrs, semconv.HTTPResponseStatusCode(resp.StatusCode))

		if _, version, ok := strings.Cut(resp.Proto, "/"); ok {
			attrs = append(attrs, semconv.NetworkProtocolVersion(version))
		}

		if resp.StatusCode/100 == 3 {
			logger.DebugContext(req.Context(), "redirect", "from", req.URL.String(), "to", resp.Header.Get("Location"))
		}

		if resp.Close {
			MetricHttpResponseClose.Add(req.Context(), 1)
		}
	}
	MetricRequestDuration.Record(ctx, time.Since(reqStart).Seconds(), metric.WithAttributes(attrs...))

	return resp, err
}

func newTracer(ctx context.Context, _ *TracingRoundTripper) *httptrace.ClientTrace {
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
			logger.Debug("GotConn", "address", gci.Conn.RemoteAddr().String())

			MetricHttpConnection.Add(ctx, 1, metric.WithAttributes(
				attribute.Bool("reused", gci.Reused),
				attribute.Bool("was_idle", gci.WasIdle),
			))

			MetricIdleDuration.Record(ctx, gci.IdleTime.Seconds())
		},
		PutIdleConn: func(err error) {
			// PutIdleConn is only called for HTTP/1.1, but we'll log the errors for debugging
			if err != nil {
				logger.Debug("PutIdleConn", "error", err)
			}
		},
	}
}
