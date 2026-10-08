package main

import (
	"bytes"
	"strings"
	"testing"
)

// Analytics load only where the deployment sets a Plausible script, and no
// other address is loaded in its place.
func TestAnalyticsScript(t *testing.T) {
	const src = "https://plausible.io/js/pa-6eOh4zvn9DGx7SVfxXCnu.js"
	for value, want := range map[string]string{
		"":                                  "",
		src:                                 src,
		"https://evil.example/js/pa-x.js":   "",
		"http://plausible.io/js/pa-x.js":    "",
		"https://plausible.io/js/pa-x.js?a": "",
	} {
		t.Setenv("YAVCHN_PLAUSIBLE_SCRIPT", value)
		if got := analyticsScript(); got != want {
			t.Errorf("%q: %q, want %q", value, got, want)
		}
	}

	head := func(value string) string {
		t.Setenv("YAVCHN_PLAUSIBLE_SCRIPT", value)
		tpl, err := parseTemplates()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := tpl.ExecuteTemplate(&b, "head", struct{ Title, Account any }{Title: "T"}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if on := head(src); !strings.Contains(on, `<script async src="`+src+`"></script>`) || !strings.Contains(on, `/static/analytics.js?v=`) {
		t.Errorf("production head lacks the analytics: %s", on)
	}
	if off := head(""); strings.Contains(off, "plausible") || strings.Contains(off, "analytics.js") {
		t.Errorf("a head without analytics loads them: %s", off)
	}
}
