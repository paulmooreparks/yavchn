package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_EmptyKeyAlwaysAllows(t *testing.T) {
	r := newRateLimiter(1, time.Second)
	for i := 0; i < 5; i++ {
		if !r.Allow("") {
			t.Fatalf("empty key should always allow, denied on iteration %d", i)
		}
	}
}

func TestRateLimiter_AllowsUpToLimit(t *testing.T) {
	r := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !r.Allow("ip-a") {
			t.Fatalf("under-limit call %d should allow", i)
		}
	}
	if r.Allow("ip-a") {
		t.Fatalf("over-limit call should deny")
	}
}

func TestRateLimiter_PerKeyIndependent(t *testing.T) {
	r := newRateLimiter(1, time.Minute)
	if !r.Allow("ip-a") {
		t.Fatalf("first call for ip-a should allow")
	}
	if r.Allow("ip-a") {
		t.Fatalf("second call for ip-a should deny")
	}
	if !r.Allow("ip-b") {
		t.Fatalf("first call for ip-b should allow (independent key)")
	}
}

func TestRateLimiter_SlidingWindow_Forgets(t *testing.T) {
	r := newRateLimiter(2, 50*time.Millisecond)
	r.Allow("ip")
	r.Allow("ip")
	if r.Allow("ip") {
		t.Fatalf("third call inside window should deny")
	}
	time.Sleep(80 * time.Millisecond)
	if !r.Allow("ip") {
		t.Fatalf("after window expiry, allow should reset")
	}
}

func TestClientIP_PrefersCFConnectingIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:443"
	req.Header.Set("X-Forwarded-For", "2.2.2.2, 3.3.3.3")
	req.Header.Set("CF-Connecting-IP", "1.1.1.1")
	if got := clientIP(req); got != "1.1.1.1" {
		t.Fatalf("CF-Connecting-IP should win, got %q", got)
	}
}

func TestClientIP_FallsBackToXFFLeftmost(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:443"
	req.Header.Set("X-Forwarded-For", "2.2.2.2, 3.3.3.3")
	if got := clientIP(req); got != "2.2.2.2" {
		t.Fatalf("XFF leftmost should win when CF header absent, got %q", got)
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.4:55555"
	if got := clientIP(req); got != "203.0.113.4" {
		t.Fatalf("RemoteAddr host should win when no proxy headers, got %q", got)
	}
}

func TestClientIP_TrimsWhitespace(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:443"
	req.Header.Set("X-Forwarded-For", "  4.4.4.4  ,5.5.5.5")
	if got := clientIP(req); got != "4.4.4.4" {
		t.Fatalf("XFF leftmost should be trimmed, got %q", got)
	}
}
