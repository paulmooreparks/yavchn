package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestUrlHash_Deterministic(t *testing.T) {
	a := urlHash("https://example.com/x")
	b := urlHash("https://example.com/x")
	if a != b {
		t.Fatalf("urlHash should be deterministic, got %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("urlHash should be 64-char hex, got len %d", len(a))
	}
	c := urlHash("https://example.com/y")
	if a == c {
		t.Fatalf("different inputs should hash differently")
	}
}

func TestIsAllowedURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"http://example.com/x", true},
		{"https://example.com/x", true},
		{"ftp://example.com/x", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"data:text/html,<x>", false},
		{"", false},
		{"://no-scheme", false},
	}
	for _, tc := range cases {
		if got := isAllowedURL(tc.url); got != tc.ok {
			t.Errorf("isAllowedURL(%q) = %v, want %v", tc.url, got, tc.ok)
		}
	}
}

func TestExtractor_Get_RejectsBadScheme(t *testing.T) {
	e := newTestExtractor(t, nil)
	if _, err := e.Get(context.Background(), "ftp://example.com/x", "ip"); err == nil {
		t.Fatal("expected error for non-http(s) URL")
	}
}

func TestExtractor_Get_CachesAfterFirstFetch(t *testing.T) {
	rt := &countingRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		return htmlResp(200, sampleArticleHTML), nil
	}}
	e := newTestExtractor(t, rt)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		a, err := e.Get(ctx, "https://example.com/test", "ip-1")
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if a == nil || a.Title == "" {
			t.Fatalf("Get %d: empty article", i)
		}
	}
	if got := rt.Calls(); got != 1 {
		t.Fatalf("expected 1 upstream call (subsequent served from cache), got %d", got)
	}
}

func TestExtractor_Get_PropagatesNon200(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return htmlResp(500, ""), nil
	})
	e := newTestExtractor(t, rt)
	_, err := e.Get(context.Background(), "https://example.com/oops", "ip-1")
	if err == nil {
		t.Fatal("expected error on upstream 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention upstream status, got %v", err)
	}
}

func TestExtractor_Get_RejectsNonHTMLContentType(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return resp(200, "image/png", "fake png bytes"), nil
	})
	e := newTestExtractor(t, rt)
	_, err := e.Get(context.Background(), "https://example.com/img", "ip-1")
	if err == nil {
		t.Fatal("expected error on non-html content-type")
	}
	if !strings.Contains(err.Error(), "not html") {
		t.Fatalf("expected 'not html' in error, got %v", err)
	}
}

func TestExtractor_Get_FetchTransportError(t *testing.T) {
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial fake")
	})
	e := newTestExtractor(t, rt)
	_, err := e.Get(context.Background(), "https://example.com/oops", "ip-1")
	if err == nil {
		t.Fatal("expected transport error to surface")
	}
}

// newTestExtractor returns an Extractor backed by a temp-dir sqlite DB
// and an http.Client whose Transport is the given fake. Skips the
// SSRF-safe DialContext (the fake never dials).
func newTestExtractor(t *testing.T, rt http.RoundTripper) *Extractor {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := NewExtractor(db)
	if rt != nil {
		e.client = &http.Client{Transport: rt}
	}
	return e
}

// sampleArticleHTML is a minimal page go-readability will happily extract.
// Used by the cache-hit test to drive a successful extraction path.
const sampleArticleHTML = `<!doctype html>
<html><head><title>Cache Test Article</title></head>
<body>
<article>
<h1>Cache Test Article</h1>
<p>This is a paragraph of text that is sufficiently long for the readability
algorithm to treat it as the main content of the page. Lorem ipsum dolor sit
amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore
et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation
ullamco laboris nisi ut aliquip ex ea commodo consequat.</p>
<p>Another paragraph. Duis aute irure dolor in reprehenderit in voluptate
velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat
cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id
est laborum.</p>
</article>
</body></html>`
