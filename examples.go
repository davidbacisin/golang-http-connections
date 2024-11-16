package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
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

var Example1_1 = Scenario{
	Name: "Example 1.1: default HTTP/1.1 client with spike",
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
