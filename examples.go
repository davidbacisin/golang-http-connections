package main

import (
	"context"
	"crypto/tls"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/time/rate"
)

type Stage struct {
	VUs      int
	Duration string

	InjectLatency func(time.Duration, int64) time.Duration
}

type Scenario struct {
	Name      string
	Stages    []Stage
	NewClient func(*LatencyInjector) *http.Client
}

var (
	Scenarios = map[string]Scenario{
		// Part 1
		"1.1": Example1_1,
		"1.2": Example1_2,
		"1.3": Example1_3,
		"2.1": Example2_1,
		"2.2": Example2_2,
		"2.3": Example2_3,
		// Part 2
		"3.1": Example3_1,
		"3.2": Example3_2,
		"4.1": Example4_1,
		"4.2": Example4_2,
		"5.1": Example5_1,
		"5.2": Example5_2,
		"6.1": Example6_1,
		"6.2": Example6_2,
	}

	InjectedLatencySimulation = []Stage{
		{VUs: 2, Duration: "20s"},
		{VUs: 1000, Duration: "20s"},
		{VUs: 2, Duration: "20s"},
		{
			VUs:      1000,
			Duration: "60s",
			InjectLatency: func(_ time.Duration, active int64) time.Duration {
				delay := float64(100*time.Millisecond) * rand.Float64()

				// Add "queuing" time to simulate access to a finite resource.
				queueDepth := max(0, active-50)
				if queueDepth > 0 {
					// A deeper queue leads to a longer delay.
					// While a linear relationship might not be the most realistic,
					// it's a simple implementation that is effective for the examples.
					delay += float64(queueDepth) * float64(10*time.Millisecond)
				}

				return time.Duration(delay)
			},
		},
	}

	Example1_1 = Scenario{
		Name: "Example 1.1: default HTTP/1.1 client",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 100, Duration: "20s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func(_ *LatencyInjector) *http.Client {
			// This closely follows http.DefaultTransport, except it sets
			// ForceAttemptHTTP2 to false so that we use HTTP/1.1
			dialer := &TracingDialer{
				Dialer: net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				},
			}
			transport := &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           dialer.DialContext,
				DisableKeepAlives:     false,
				ForceAttemptHTTP2:     false,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
			return &http.Client{
				Transport: transport,
				Timeout:   3 * time.Second,
			}
		},
	}

	Example1_2 = Scenario{
		Name:   "Example 1.2: HTTP/1.1 client with larger idle pool",
		Stages: Example1_1.Stages,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example1_1.NewClient(li)
			transport := client.Transport.(*http.Transport)
			transport.MaxIdleConnsPerHost = 100
			return client
		},
	}

	Example1_3 = Scenario{
		Name: "Example 1.3: HTTP/1.1 client with MaxConnsPerHost",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 100, Duration: "20s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example1_1.NewClient(li)
			transport := client.Transport.(*http.Transport)
			transport.MaxIdleConnsPerHost = 10
			transport.MaxConnsPerHost = 20
			return client
		},
	}

	Example2_1 = Scenario{
		Name: "Example 2.1: default HTTP/2 client",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 100, Duration: "20s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func(_ *LatencyInjector) *http.Client {
			transport := &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DisableKeepAlives:     false,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
			return &http.Client{
				Transport: transport,
				Timeout:   3 * time.Second,
			}
		},
	}

	Example2_2 = Scenario{
		Name: "Example 2.2: default HTTP/2 client with extended stages",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 100, Duration: "20s"},
			{VUs: 200, Duration: "20s"},
			{VUs: 300, Duration: "20s"},
			{VUs: 400, Duration: "20s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func(li *LatencyInjector) *http.Client {
			return Example2_1.NewClient(li)
		},
	}

	Example2_3 = Scenario{
		Name:   "Example 2.3: HTTP/2 client with strict max concurrent streams",
		Stages: Example2_2.Stages,
		NewClient: func(_ *LatencyInjector) *http.Client {
			transport := &http2.Transport{
				StrictMaxConcurrentStreams: true,
				IdleConnTimeout:            90 * time.Second,
				TLSClientConfig:            &tls.Config{InsecureSkipVerify: true},
			}
			return &http.Client{
				Transport: transport,
				Timeout:   3 * time.Second,
			}
		},
	}

	Example3_1 = Scenario{
		Name:   "Example 3.1: HTTP/1.1 client with load greatly exceeding idle pool",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			// This closely follows http.DefaultTransport,
			// with exceptions called out in comments below
			dialer := &TracingDialer{
				Dialer: net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				},
			}
			transport := &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           dialer.DialContext,
				DisableKeepAlives:     false,
				ForceAttemptHTTP2:     false,  // ensure HTTP/1.1
				MaxIdleConns:          100,    // keep default max idle conns
				MaxIdleConnsPerHost:   100,    // use a larger idle pool per host
				MaxConnsPerHost:       15_000, // prevent port exhaustion on the test machine
				IdleConnTimeout:       90 * time.Second,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
			li.Transport = transport
			return &http.Client{
				Transport: li,
				Timeout:   3 * time.Second,
			}
		},
	}

	Example3_2 = Scenario{
		Name:   "Example 3.2: HTTP/2 client with load greatly exceeding idle pool",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example3_1.NewClient(li)
			transport := client.Transport.(*LatencyInjector).Transport.(*http.Transport)
			transport.ForceAttemptHTTP2 = true
			return client
		},
	}

	Example4_1 = Scenario{
		Name:   "Example 4.1: HTTP/1.1 client with rate-limited dialer",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example3_1.NewClient(li)
			transport := client.Transport.(*LatencyInjector).Transport.(*http.Transport)
			baseDial := transport.DialContext

			// Allow bursts up to the size of the idle connection pool
			// so that we can initialize the pool quickly, but then limit the
			// creation of new connections.
			burstSize := max(transport.MaxIdleConns, transport.MaxIdleConnsPerHost)
			lim := rate.NewLimiter(rate.Limit(2), burstSize)
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				err := lim.Wait(ctx)
				if err != nil {
					return nil, err
				}

				return baseDial(ctx, network, addr)
			}

			return client
		},
	}

	Example4_2 = Scenario{
		Name:   "Example 4.2: HTTP/2 client with rate-limited dialer",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example4_1.NewClient(li)
			base := client.Transport.(*LatencyInjector).Transport.(*http.Transport)
			base.ForceAttemptHTTP2 = true
			return client
		},
	}

	Example5_1 = Scenario{
		Name:   "Example 5.1: HTTP/1.1 client with latency-informed dialer",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example3_1.NewClient(li)
			base := client.Transport.(*LatencyInjector).Transport.(*http.Transport)
			baseDial := base.DialContext

			const alpha = 250 * float64(time.Nanosecond)
			measure := NewMeasuringTransport(li, 2<<4, 2<<14)
			client.Transport = measure

			base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Only allow new connections if the short-term mean latency is below the long-term mean
				// latency by more than alpha.
				if diff := measure.Long.Mean() - measure.Short.Mean(); measure.Active() > 2 && diff < alpha {
					// Block until the context is canceled
					<-ctx.Done()
					return nil, ctx.Err()
				}

				return baseDial(ctx, network, addr)
			}

			return client
		},
	}

	Example5_2 = Scenario{
		Name:   "Example 5.2: HTTP/2 client with latency-informed dialer",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example5_1.NewClient(li)
			base := client.Transport.(*MeasuringTransport).
				Transport.(*LatencyInjector).
				Transport.(*http.Transport)
			base.ForceAttemptHTTP2 = true
			return client
		},
	}

	Example6_1 = Scenario{
		Name:   "Example 6.1: HTTP/1.1 client with adaptive concurrency limiter",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example3_1.NewClient(li)

			lim, err := NewGradient2Limiter(2, 100, 1, 0.001, 4.0, 2<<4, 2<<14)
			if err != nil {
				log.Fatalf("failed to create Gradient2Limiter: %v", err)
			}

			// The adaptive transport needs to wrap the latency injector
			client.Transport = NewAdaptiveTransport(li, lim)
			return client
		},
	}

	Example6_2 = Scenario{
		Name:   "Example 6.2: HTTP/2 client with adaptive concurrency limiter",
		Stages: InjectedLatencySimulation,
		NewClient: func(li *LatencyInjector) *http.Client {
			client := Example6_1.NewClient(li)
			// Not pretty, but get the base transport
			base := client.Transport.(*AdaptiveTransport).
				Transport.(*LatencyInjector).
				Transport.(*http.Transport)
			base.ForceAttemptHTTP2 = true
			return client
		},
	}
)
