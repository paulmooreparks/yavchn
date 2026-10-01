package main

import (
	"strings"
	"testing"
)

func TestParseTerms_Match(t *testing.T) {
	text := "Acme Corp | Senior Rust Engineer | REMOTE (US) | New York optional"
	for q, want := range map[string]bool{
		"":                      true,
		"rust remote":           true,
		"Rust REMOTE":           true,
		"rust -remote":          false,
		`"new york"`:            true,
		`"york new"`:            false,
		`rust -"san francisco"`: true,
		"golang":                false,
		"-":                     true, // a lone minus names nothing, and is ignored
	} {
		if got := parseTerms(q).match(text); got != want {
			t.Errorf("parseTerms(%q).match = %v, want %v", q, got, want)
		}
	}
}

func TestExcerpt(t *testing.T) {
	got := excerpt("<p>First paragraph.<p>Second <a href=\"x\">link</a> here.", 200)
	if got != "First paragraph. Second link here." {
		t.Errorf("excerpt = %q", got)
	}
	if got := excerpt("<p>"+strings.Repeat("word ", 100)+"</p>", 20); len([]rune(got)) != 20 || !strings.HasSuffix(got, "…") {
		t.Errorf("a long excerpt should be cut to n with an ellipsis, got %q", got)
	}
}

func TestAuthorLink(t *testing.T) {
	if k, u := authorLink("hn", "pg"); k != "user-hn-pg" || u != "/user/hn/pg" {
		t.Errorf("authorLink = %q %q", k, u)
	}
	if k, _ := authorLink("hn", "[deleted]"); k != "" {
		t.Errorf("a name that cannot be a user should get no link, got %q", k)
	}
}

func TestRender_LookupWindow(t *testing.T) {
	_, mux := testServer(t)
	rec := get(t, mux, "/window/user")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `class="win app-win app-win-lookup"`) || !strings.Contains(body, `class="app user-lookup"`) {
		t.Fatalf("lookup window %d: %.300s", rec.Code, body)
	}
	if !strings.Contains(body, `style="--win-x:0.3; --win-y:0.08; --win-w:0.4; --win-h:0.55"`) {
		t.Error("the lookup window should carry its own floating place")
	}
	if rec := get(t, mux, "/window/user-zz-pg"); rec.Code != 404 {
		t.Errorf("a profile on an unknown site should 404, got %d", rec.Code)
	}
}
