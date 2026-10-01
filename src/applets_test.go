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

func TestRender_PinnedFilters(t *testing.T) {
	_, mux := testServer(t)
	body := get(t, mux, "/pinned/?q=rust&source=hn&show=unread&sort=points&source=bogus&open=hn-1").Body.String()
	for _, want := range []string{
		`data-pin-q="rust" data-pin-source="hn" data-pin-sort="points" data-pin-unread`,
		`<span class="filter-chip-kind">Words</span> rust`,
		// Removing one filter keeps the others, the order and the windows.
		`href="/pinned/?show=unread&amp;sort=points&amp;source=hn&amp;open=hn-1"`,
		// Clearing them keeps only the order and the windows.
		`<a class="md-chips-clear" href="/pinned/?sort=points&amp;open=hn-1">`,
		`<a href="/pinned/?q=rust&amp;show=unread&amp;sort=points&amp;open=hn-1">All</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Pinned lacks %q", want)
		}
	}
	if !strings.Contains(get(t, mux, "/hn/").Body.String(), `<div class="md-chips" data-region="chips">`) {
		t.Error("every list needs the chips region, empty or not, for region swaps")
	}
}

func TestRender_LookupWindow(t *testing.T) {
	_, mux := testServer(t)
	rec := get(t, mux, "/window/user")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `class="win app-win app-win-lookup"`) || !strings.Contains(body, `class="user-lookup lookup-bar"`) {
		t.Fatalf("lookup window %d: %.300s", rec.Code, body)
	}
	if !strings.Contains(body, `style="--win-x:0.5; --win-y:0.05; --win-w:0.46; --win-h:0.86"`) {
		t.Error("the lookup window should open where a profile does")
	}
	if rec := get(t, mux, "/window/user-zz-pg"); rec.Code != 404 {
		t.Errorf("a profile on an unknown site should 404, got %d", rec.Code)
	}
}
