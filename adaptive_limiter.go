package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/davidbacisin/go-mwnd"
	"go.opentelemetry.io/otel/metric"
)

var MetricAdaptiveConcurrency = must(meter.Int64Gauge("limiter.adaptive_concurrency",
	metric.WithDescription("the current concurrency limit of the adaptive concurrency limiter"),
))

var MetricAdaptiveTransport = must(meter.Int64Gauge("limiter.adaptive_transport_active",
	metric.WithDescription("the current number of active requests in the adaptive transport"),
))

// Gradient2Limiter is a concurrency limiter that adjusts its limit based on observed latencies.
// It increases the limit when latencies are low and decreases it when latencies rise,
// using a gradient-based approached modeled closely after the Gradient2 algorithm from
// https://github.com/Netflix/concurrency-limits.
type Gradient2Limiter struct {
	mux sync.RWMutex

	currentLimit       float64
	maxLimit, minLimit float64
	smoothing          float64
	probeSize          float64

	shortAvg *mwnd.ExponentialWindow[int64]
	longAvg  *mwnd.ExponentialWindow[int64]
}

func NewGradient2Limiter(initialLimit, maxLimit, minLimit int,
	smoothing, probeSize float64, shortSize, longSize int) (*Gradient2Limiter, error) {
	// Set defaults
	if initialLimit <= 0 {
		initialLimit = 20
	}
	if maxLimit <= 0 {
		maxLimit = 200
	}
	if minLimit <= 0 {
		minLimit = 20
	}
	if minLimit > maxLimit {
		return nil, fmt.Errorf("minLimit must be <= maxLimit")
	}
	if smoothing > 1.0 || smoothing < 0 {
		smoothing = 0.2
	}
	if probeSize <= 0 {
		probeSize = 4.0
	}
	if shortSize < 0 {
		shortSize = 60
	}
	if longSize < 0 {
		longSize = 600
	}
	if shortSize >= longSize {
		return nil, fmt.Errorf("shortSize must be < longSize")
	}

	return &Gradient2Limiter{
		currentLimit: float64(initialLimit),
		maxLimit:     float64(maxLimit),
		minLimit:     float64(minLimit),
		smoothing:    smoothing,
		probeSize:    probeSize,
		shortAvg:     mwnd.Exponential[int64](mwnd.ExponentialAlphaForApproximatingFixed(shortSize)),
		longAvg:      mwnd.Exponential[int64](mwnd.ExponentialAlphaForApproximatingFixed(longSize)),
	}, nil
}

// Limit returns the current estimated limit.
func (l *Gradient2Limiter) Limit() int64 {
	l.mux.RLock()
	defer l.mux.RUnlock()
	return int64(l.currentLimit)
}

// OnSample adjusts the concurrency limit for the provided latency sample.
func (l *Gradient2Limiter) OnSample(rtt int64, inFlight int64) {
	l.mux.Lock()
	defer l.mux.Unlock()

	l.shortAvg.Put(rtt)
	l.longAvg.Put(rtt)

	if inFlight < int64(l.currentLimit)/2 {
		// Don't adjust if we are not near the limit
		MetricAdaptiveConcurrency.Record(context.Background(), int64(l.currentLimit))
		return
	}

	shortRTT := l.shortAvg.Mean()
	longRTT := l.longAvg.Mean()

	// Clamp the gradient to avoid excessive swings
	gradient := max(0.5, min(1.0, longRTT/shortRTT))
	l.updateLocked(gradient)
}

func (l *Gradient2Limiter) OnDrop() {
	l.mux.Lock()
	defer l.mux.Unlock()
	// Back off on drops
	l.updateLocked(0.5)
}

func (l *Gradient2Limiter) updateLocked(gradient float64) {
	newLimit := l.currentLimit*gradient + l.probeSize
	newLimit = l.currentLimit*(1-l.smoothing) + newLimit*l.smoothing
	newLimit = max(l.minLimit, min(l.maxLimit, newLimit))
	l.currentLimit = newLimit
	MetricAdaptiveConcurrency.Record(context.Background(), int64(l.currentLimit))
}

type AdaptiveTransport struct {
	base    http.RoundTripper
	limiter *Gradient2Limiter
	sem     *Weighted
}

func NewAdaptiveTransport(base http.RoundTripper, limiter *Gradient2Limiter) *AdaptiveTransport {
	//notify := make(chan struct{})
	t := &AdaptiveTransport{
		base:    base,
		limiter: limiter,
		sem:     NewWeighted(limiter.Limit()),
	}
	return t
}

func (t *AdaptiveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	t.sem.SetSize(t.limiter.Limit())
	err := t.sem.Acquire(ctx, 1)
	if err != nil {
		return nil, err
	}
	defer t.sem.Release(1)

	semCount := t.sem.Count()
	MetricAdaptiveTransport.Record(ctx, semCount)

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	rtt := time.Since(start)
	if err != nil {
		t.limiter.OnDrop()
		return nil, err
	}
	t.limiter.OnSample(int64(rtt), semCount)
	return resp, nil
}
