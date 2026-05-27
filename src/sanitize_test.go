package main

import (
	"strings"
	"testing"
)

func TestSanitizeHTML_StripsScript(t *testing.T) {
	out := sanitizeHTML(`<p>hi</p><script>alert(1)</script>`)
	if strings.Contains(out, "<script") {
		t.Fatalf("expected script tag stripped, got %q", out)
	}
	if !strings.Contains(out, "<p>hi</p>") {
		t.Fatalf("expected <p> preserved, got %q", out)
	}
}

func TestSanitizeHTML_StripsJavascriptURL(t *testing.T) {
	out := sanitizeHTML(`<a href="javascript:alert(1)">x</a>`)
	if strings.Contains(strings.ToLower(out), "javascript:") {
		t.Fatalf("javascript: URL not stripped: %q", out)
	}
}

func TestSanitizeHTML_AddsLinkSafetyAttrs(t *testing.T) {
	out := sanitizeHTML(`<a href="https://example.com/x">x</a>`)
	low := strings.ToLower(out)
	if !strings.Contains(low, `rel=`) || !strings.Contains(low, "noreferrer") {
		t.Fatalf("expected rel=noreferrer on fully-qualified link, got %q", out)
	}
	if !strings.Contains(low, `target="_blank"`) && !strings.Contains(low, `target='_blank'`) {
		t.Fatalf("expected target=_blank on fully-qualified link, got %q", out)
	}
}

func TestSanitizeHTML_AllowsImgLoadingAttr(t *testing.T) {
	out := sanitizeHTML(`<img src="https://example.com/x.png" loading="lazy" alt="x">`)
	if !strings.Contains(out, `loading="lazy"`) {
		t.Fatalf("expected loading attr preserved, got %q", out)
	}
}

func TestSanitizeHTML_StripsEventHandler(t *testing.T) {
	out := sanitizeHTML(`<p onclick="alert(1)">hi</p>`)
	if strings.Contains(strings.ToLower(out), "onclick") {
		t.Fatalf("onclick attr not stripped: %q", out)
	}
	if !strings.Contains(out, "<p>hi</p>") && !strings.Contains(out, "<p >hi</p>") {
		t.Fatalf("expected <p>hi</p> after onclick strip, got %q", out)
	}
}

func TestSanitizeHTML_EmptyStringNoop(t *testing.T) {
	if got := sanitizeHTML(""); got != "" {
		t.Fatalf("expected empty output for empty input, got %q", got)
	}
}
