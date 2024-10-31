package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

func NewDefaultClient() (*http.Client, string) {
	return http.DefaultClient, "default"
}

func NewHttp11KeepAlive() (*http.Client, string) {
	dialer := NewTracingDialer(&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
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
	}, "http11_keepalive"
}

func NewHttp11DisableKeepAlive() (*http.Client, string) {
	dialer := NewTracingDialer(&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: -1,
		KeepAliveConfig: net.KeepAliveConfig{
			Enable: false,
		},
	})
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
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
	}, "http11_nokeepalive"
}

func NewHttp2KeepAlive() (*http.Client, string) {
	dialer := NewTracingDialer(&net.Dialer{})
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h2"},
		},
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http2_keepalive"
}
