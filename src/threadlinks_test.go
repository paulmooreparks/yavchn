package main

import (
	"strings"
	"testing"
)

func TestThreadKey(t *testing.T) {
	for href, want := range map[string]string{
		"https://news.ycombinator.com/item?id=41234567":       "hn-41234567",
		"http://www.news.ycombinator.com/item?id=1&p=2":       "hn-1",
		"https://news.ycombinator.com/item?id=12x":            "",
		"https://news.ycombinator.com/user?id=pg":             "",
		"https://lobste.rs/s/abc123/some_slug":                "lobsters-abc123",
		"https://lobste.rs/s/abc123":                          "lobsters-abc123",
		"https://lobste.rs/c/abc123":                          "",
		"https://example.com/item?id=1":                       "",
		"javascript:alert(1)//news.ycombinator.com/item?id=1": "",
	} {
		if got := threadKey(href); got != want {
			t.Errorf("threadKey(%q) = %q, want %q", href, got, want)
		}
	}
}

func TestLinkThreads(t *testing.T) {
	in := sanitizeHTML(`<p>See <a href="https://news.ycombinator.com/item?id=42">this</a> and <a href="https://example.com/">that</a>.</p>`)
	out := linkThreads(in)
	if !strings.Contains(out, `<a href="/story/hn/42" rel="nofollow noreferrer noopener" data-win-open="hn-42">this</a>`) {
		t.Errorf("thread link not rewritten: %s", out)
	}
	if !strings.Contains(out, `href="https://example.com/"`) || !strings.Contains(out, `target="_blank"`) {
		t.Errorf("other links should be left alone: %s", out)
	}
	if plain := "<p>No threads here.</p>"; linkThreads(plain) != plain {
		t.Error("a fragment with no thread links should come back unchanged")
	}
}
