package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

func NewDefaultClient() (*http.Client, string) {
	return http.DefaultClient, "default"
}

func NewHttp11KeepAlive() (*http.Client, string) {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http11_keepalive"
}

func NewHttp11DisableKeepAlive() (*http.Client, string) {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: -1,
		KeepAliveConfig: net.KeepAliveConfig{
			Enable: false,
		},
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http11_nokeepalive"
}

func NewHttp2KeepAlive() (*http.Client, string) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http2_keepalive"
}

func NewHttp2KeepAliveLimited() (*http.Client, string) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxConnsPerHost:       2,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http2_keepalive_limited"
}

func NewHttp2Special1() (*http.Client, string) {
	transport := &http2.Transport{
		IdleConnTimeout:            90 * time.Second,
		StrictMaxConcurrentStreams: true,
		TLSClientConfig:            &tls.Config{InsecureSkipVerify: true},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
	}, "http2_special1"
}
