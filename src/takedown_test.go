package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func takedownDB(t *testing.T) *Extractor {
	t.Helper()
	db, err := OpenDB(context.Background(), filepath.Join(t.TempDir(), "takedown.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewExtractor(db)
}

func cacheArticle(t *testing.T, e *Extractor, raw string) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO articles(url_hash,url,fetched_at,title,byline,content)VALUES(?,?,?,'T','','<p>Text</p>')`, articleKey(raw), raw, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
}

func TestExclusionPatterns(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.Example.com/post/#comments": "url:example.com/post",
		"http://example.com/post?id=7":           "url:example.com/post?id=7",
		"www.Example.com":                        "host:example.com",
		"blog.example.com.":                      "host:blog.example.com",
	} {
		if got, err := exclusionPattern(in); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://example.com/x", "example.com/post", "user@example.com", "https://"} {
		if _, err := exclusionPattern(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// One article's exclusion covers its address however it is written, and a
// site's covers its subdomains, but neither reaches past what it says.
func TestExclusionsCoverWhatTheySay(t *testing.T) {
	e := takedownDB(t)
	ctx := context.Background()
	for _, target := range []string{"https://example.com/post", "news.example.org"} {
		if err := runTakedown(ctx, e.db, []string{"add", target}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	for raw, want := range map[string]bool{
		"https://example.com/post":         true,
		"http://www.example.com/post/#top": true,
		"https://example.com/post?page=2":  false,
		"https://example.com/other":        false,
		"https://news.example.org/a":       true,
		"https://eu.news.example.org/b":    true,
		"https://example.org/c":            false,
		"https://othernews.example.org/d":  false,
	} {
		if got, err := excluded(ctx, e.db, raw); err != nil || got != want {
			t.Errorf("%s: excluded %v, %v; want %v", raw, got, err, want)
		}
	}
}

// An exclusion deletes the cached copies it covers at once, and the reader
// refuses the article without fetching it, even when asked to refresh.
func TestExclusionStopsTheReader(t *testing.T) {
	e := takedownDB(t)
	ctx := context.Background()
	cacheArticle(t, e, "https://blog.example.com/one")
	cacheArticle(t, e, "https://www.blog.example.com/two")
	cacheArticle(t, e, "https://example.net/three")
	var out bytes.Buffer
	if err := runTakedown(ctx, e.db, []string{"add", "blog.example.com"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Deleted 2 cached copies") {
		t.Errorf("add said %q", out.String())
	}
	var left int
	if err := e.db.QueryRow(`SELECT count(*) FROM articles`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("%d cached articles left, %v", left, err)
	}
	if _, err := e.Get(ctx, "https://blog.example.com/one", "203.0.113.1"); !errors.Is(err, errExcluded) {
		t.Errorf("Get: %v", err)
	}
	if _, err := e.ForceGet(ctx, "https://blog.example.com/new", "203.0.113.1"); !errors.Is(err, errExcluded) {
		t.Errorf("ForceGet: %v", err)
	}
	if a, err := e.Get(ctx, "https://example.net/three", "203.0.113.1"); err != nil || a.Title != "T" {
		t.Errorf("an article outside the exclusion: %v", err)
	}

	s := &Server{extract: e}
	rec := httptest.NewRecorder()
	s.ArticleAPI(rec, httptest.NewRequest("GET", "/api/article?url=https://blog.example.com/one", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "asked YAVCHN not to show a copy") || strings.Contains(rec.Body.String(), "archive.org") {
		t.Errorf("reader got %d: %s", rec.Code, rec.Body)
	}

	out.Reset()
	if err := runTakedown(ctx, e.db, []string{"list"}, &out); err != nil || !strings.Contains(out.String(), "blog.example.com and its subdomains") {
		t.Errorf("list: %q, %v", out.String(), err)
	}
	if err := runTakedown(ctx, e.db, []string{"remove", "www.blog.example.com"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if no, _ := excluded(ctx, e.db, "https://blog.example.com/one"); no {
		t.Error("removing the exclusion left it in force")
	}
	if err := runTakedown(ctx, e.db, []string{"remove", "blog.example.com"}, &bytes.Buffer{}); err == nil {
		t.Error("removing an exclusion twice succeeded")
	}
	if err := runTakedown(ctx, e.db, []string{"add"}, &bytes.Buffer{}); err == nil {
		t.Error("add without a target succeeded")
	}
}
