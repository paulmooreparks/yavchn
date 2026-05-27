package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// roundTripFunc lets a test inject a synthetic HTTP response without
// dialing a real socket. Stubs out the upstream HN / extraction calls so
// the cache + failure-path tests stay hermetic.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// countingRoundTripper tracks how many times the wrapped fn fires.
// Used to assert that cache hits don't re-issue outbound requests.
type countingRoundTripper struct {
	calls atomic.Int64
	fn    roundTripFunc
}

func (c *countingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.fn(r)
}

func (c *countingRoundTripper) Calls() int64 { return c.calls.Load() }

func jsonResp(status int, body string) *http.Response {
	return resp(status, "application/json", body)
}

func htmlResp(status int, body string) *http.Response {
	return resp(status, "text/html; charset=utf-8", body)
}

func resp(status int, ctype, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{ctype}},
	}
}
