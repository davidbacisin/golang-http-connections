package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/time/rate"
)

type Stage struct {
	VUs      int
	Duration string
}

type Scenario struct {
	Name      string
	Stages    []Stage
	NewClient func() *http.Client
}

var (
	Scenarios = map[string]Scenario{
		"1.1": Example1_1,
		"1.2": Example1_2,
		"1.3": Example1_3,
		"2.1": Example2_1,
		"2.2": Example2_2,
		"2.3": Example2_3,
		"3.1": Example3_1,
		"3.2": Example3_2,
		"3.3": Example3_3,
	}

	Example1_1 = Scenario{
		Name: "Example 1.1: default HTTP/1.1 client",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 100, Duration: "20s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func() *http.Client {
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
		NewClient: func() *http.Client {
			client := Example1_1.NewClient()
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
		NewClient: func() *http.Client {
			client := Example1_1.NewClient()
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
		NewClient: func() *http.Client {
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
		NewClient: func() *http.Client {
			return Example2_1.NewClient()
		},
	}

	Example2_3 = Scenario{
		Name:   "Example 2.3: HTTP/2 client with strict max concurrent streams",
		Stages: Example2_2.Stages,
		NewClient: func() *http.Client {
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
		Name: "Example 3.1: HTTP/1.1 client with load greatly exceeding idle pool",
		Stages: []Stage{
			{VUs: 2, Duration: "20s"},
			{VUs: 1000, Duration: "60s"},
			{VUs: 2, Duration: "20s"},
		},
		NewClient: func() *http.Client {
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
			return &http.Client{
				Transport: transport,
				Timeout:   3 * time.Second,
			}
		},
	}

	Example3_2 = Scenario{
		Name:   "Example 3.2: HTTP/1.1 client with rate-limited dialer",
		Stages: Example3_1.Stages,
		NewClient: func() *http.Client {
			client := Example3_1.NewClient()
			transport := client.Transport.(*http.Transport)
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

	Example3_3 = Scenario{
		Name:   "Example 3.3: HTTP/1.1 client with adaptive concurrency limiter",
		Stages: Example3_1.Stages,
		NewClient: func() *http.Client {
			client := Example3_1.NewClient()
			transport := client.Transport.(*http.Transport)

			lim, err := NewGradient2Limiter(2, transport.MaxConnsPerHost, 1, 0.01, 4.0, 10, 10_000)
			if err != nil {
				log.Fatalf("failed to create Gradient2Limiter: %v", err)
			}

			client.Transport = NewAdaptiveTransport(transport, lim)
			return client
		},
	}
)
