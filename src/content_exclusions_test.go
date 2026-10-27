package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentExclusionsPersistAndReverse(t *testing.T) {
	e := takedownDB(t)
	ctx := context.Background()
	for _, args := range [][]string{{"author", "add", "hn", "excluded-user"}, {"item", "add", "hn", "42"}} {
		if err := runTakedown(ctx, e.db, args, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	var audit bytes.Buffer
	if err := runTakedown(ctx, e.db, []string{"author", "audit", "hn", "excluded-user"}, &audit); err != nil || !strings.Contains(audit.String(), "Article cache rows mentioning identifier: 0") {
		t.Fatalf("audit: %s, %v", audit.String(), err)
	}
	p, err := loadContentExclusions(ctx, e.db)
	if err != nil || !p.author("excluded-user") || !p.item("42") || p.author("unrelated") {
		t.Fatalf("snapshot: %v, %v", p, err)
	}
	if err := runTakedown(ctx, e.db, []string{"author", "remove", "hn", "excluded-user"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	next, err := loadContentExclusions(ctx, e.db)
	if err != nil || next.author("excluded-user") || !p.author("excluded-user") {
		t.Fatal("restart snapshot must be independent")
	}
	for _, args := range [][]string{{"author", "add", "lobsters", "someone"}, {"item", "add", "hn", "-1"}, {"item", "add", "hn", "042"}} {
		if err := runTakedown(ctx, e.db, args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestContentCacheAuditAndExclusion(t *testing.T) {
	e := takedownDB(t)
	ctx := context.Background()
	raw := "https://example.com/post?token=secret"
	cacheArticle(t, e, raw)
	if _, err := e.db.Exec(`UPDATE articles SET content='<p>excluded-user</p>'`); err != nil {
		t.Fatal(err)
	}
	var audit bytes.Buffer
	if err := runTakedown(ctx, e.db, []string{"author", "audit", "hn", "excluded-user"}, &audit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit.String(), "cache_key="+articleKey(raw)) || strings.Contains(audit.String(), "secret") {
		t.Fatalf("audit disclosure: %s", audit.String())
	}
	if err := runTakedown(ctx, e.db, []string{"cache", "exclude", articleKey(raw)}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if no, err := excluded(ctx, e.db, raw); err != nil || !no {
		t.Fatalf("exclusion: %v %v", no, err)
	}
	var count int
	if err := e.db.QueryRow(`SELECT count(*) FROM articles`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cache retained: %d %v", count, err)
	}
	if _, err := e.Get(ctx, raw, "test"); !errors.Is(err, errExcluded) {
		t.Fatalf("reimport allowed: %v", err)
	}
}

func TestHNContentExclusionsAcrossAdapters(t *testing.T) {
	h := NewHN()
	h.exclusions = &contentExclusions{authors: map[string]bool{"excluded-user": true}, items: map[string]bool{"42": true}}
	rt := &countingRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "/item/"):
			return jsonResp(200, `{"id":43,"by":"excluded-user","text":"private words","type":"comment"}`), nil
		case strings.Contains(r.URL.Path, "/items/"):
			return jsonResp(200, `{"id":1,"author":"someone","children":[{"id":2,"author":"excluded-user","text":"private words","children":[{"id":3,"author":"someone","text":"retained reply"}]},{"id":4,"author":"someone","text":"retained sibling"}]}`), nil
		default:
			return jsonResp(200, `{"hits":[{"objectID":"2","author":"excluded-user","title":"private words","url":"https://example.com/"},{"objectID":"4","author":"someone","title":"retained","url":"https://example.com/"}],"nbPages":1}`), nil
		}
	}}
	h.http = &http.Client{Transport: rt}
	ctx := context.Background()
	for _, check := range []func() error{
		func() error { _, e := h.Item(ctx, "42"); return e },
		func() error { _, e := h.User(ctx, "excluded-user"); return e },
		func() error { _, e := h.AuthorStories(ctx, "excluded-user"); return e },
		func() error { _, e := h.StoryThread(ctx, "42", "test"); return e },
	} {
		if err := check(); !errors.Is(err, errContentRemoved) {
			t.Fatalf("gate: %v", err)
		}
	}
	if rt.Calls() != 0 {
		t.Fatal("known exclusions fetched upstream")
	}
	if _, err := h.Item(ctx, "43"); !errors.Is(err, errContentRemoved) {
		t.Fatalf("newly discovered author: %v", err)
	}
	if _, ok := h.items.Get("43"); ok {
		t.Fatal("excluded item entered cache")
	}
	for i := 0; i < 2; i++ {
		thread, err := h.StoryThread(ctx, "1", "test")
		if err != nil || len(thread.Comments) != 2 || thread.Comments[0].ID != "3" || thread.Comments[1].ID != "4" {
			t.Fatalf("filtered thread: %#v, %v", thread, err)
		}
	}
	hits, _, err := h.Search(ctx, "query", 1)
	if err != nil || len(hits) != 1 || hits[0].ID != "4" {
		t.Fatalf("search: %v, %v", hits, err)
	}
	subs, err := h.FindByURL(ctx, "https://example.com/")
	if err != nil || len(subs) != 1 || subs[0].ID != "4" {
		t.Fatalf("finder: %v, %v", subs, err)
	}
}

func TestContentExclusionHTTPAndExtraction(t *testing.T) {
	s, _ := testServer(t)
	h := NewHN()
	h.exclusions = &contentExclusions{authors: map[string]bool{"excluded-user": true}, items: map[string]bool{"42": true}}
	s.hn, s.sources["hn"] = h, h
	mux := http.NewServeMux()
	mux.HandleFunc("GET /story/{source}/{id}", s.StoryPage)
	mux.HandleFunc("GET /window/{key}", s.Window)
	mux.HandleFunc("GET /user/{source}/{name}", s.UserPage)
	mux.HandleFunc("GET /api/discussion", s.DiscussionAPI)
	mux.HandleFunc("GET /api/replies", s.RepliesAPI)
	for _, path := range []string{"/story/hn/42", "/window/hn-42", "/window/reader-1?r.reader-1=hn-42", "/user/hn/excluded-user", "/api/discussion?id=42", "/api/replies?u=excluded-user"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 410 || w.Header().Get("X-Robots-Tag") != "noindex" || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "excluded-user") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	e := takedownDB(t)
	for _, raw := range []string{"https://news.ycombinator.com/item?id=42", "https://news.ycombinator.com/user?id=someone", "https://hn.algolia.com/api/v1/items/42", "https://hacker-news.firebaseio.com/v0/item/42.json"} {
		cacheArticle(t, e, raw)
		if _, err := e.Get(context.Background(), raw, "test"); !errors.Is(err, errSourceDocument) {
			t.Fatalf("cached source %s: %v", raw, err)
		}
		if _, err := e.ForceGet(context.Background(), raw, "test"); !errors.Is(err, errSourceDocument) {
			t.Fatalf("forced source %s: %v", raw, err)
		}
	}
	if err := e.client.CheckRedirect(httptest.NewRequest("GET", "https://news.ycombinator.com/item?id=42", nil), nil); !errors.Is(err, errSourceDocument) {
		t.Fatal("source redirect bypass")
	}
	if _, err := addExclusion(context.Background(), e.db, "host:example.com"); err != nil {
		t.Fatal(err)
	}
	if err := e.client.CheckRedirect(httptest.NewRequest("GET", "https://example.com/post", nil), nil); !errors.Is(err, errExcluded) {
		t.Fatal("publisher redirect bypass")
	}
}
