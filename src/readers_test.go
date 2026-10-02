package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestReaderInstancesRenderAndLink(t *testing.T) {
	_, mux := testServer(t)
	target := "/hn/?open=reader-1,reader-2&top=reader-2&r.reader-1=hn-1&r.reader-2=hn-1&p.reader-1=dock-left:0.1,0.1,0.5,0.5,0.3"
	rec := get(t, mux, target)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{`data-win="reader-1" data-win-mode="dock-left"`, `data-win="reader-2" data-win-mode="floating"`, `data-state-key="reader-1:hn-1"`, `data-state-key="reader-2:hn-1"`, `data-reader-new="hn-1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(body, `data-story-key="hn-1"`) != 2 {
		t.Error("duplicate article should render in two independent readers")
	}
	previous := -1
	for _, id := range []string{"site", "go", "applets", "feed", "view", "window", "help"} {
		index := strings.Index(body, `data-menubar-id="`+id+`"`)
		if index <= previous {
			t.Fatalf("menu %s missing or out of order", id)
		}
		previous = index
	}
	rec = get(t, mux, "/hn/?open=reader-1")
	if !strings.Contains(rec.Body.String(), "Select an article from the sidebar.") {
		t.Error("empty reader has no prompt")
	}
	st := parseWinState(mustQuery(t, strings.SplitN(target, "?", 2)[1]))
	created, _ := url.Parse(newReaderURL("/hn/", nil, st, "hn-2"))
	q := created.Query()
	if q.Get("top") != "reader-3" || q.Get("r.reader-3") != "hn-2" || q.Get("r.reader-1") != "hn-1" {
		t.Fatalf("creation lost state: %s", created)
	}
	closed, _ := url.Parse(winURL("/hn/", nil, st.closed("reader-1")))
	if closed.Query().Has("r.reader-1") || closed.Query().Get("r.reader-2") != "hn-1" {
		t.Fatalf("close lost sibling or retained closed assignment: %s", closed)
	}
	if rec := get(t, mux, "/window/reader-2?r.reader-2=hn-2"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-story-key="hn-2"`) {
		t.Error("fragment did not render its assignment")
	}
}

func TestReaderBrowser(t *testing.T) {
	if os.Getenv("YAVCHN_BROWSER_TEST") == "" {
		t.Skip("set YAVCHN_BROWSER_TEST=1 and install playwright to run browser tests")
	}
	s, mux := testServer(t)
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	lobsters := &fakeSource{name: "lobsters", items: map[string]*Item{
		"1": {ID: "1", Title: "Lobsters first", Text: "<p>Lobsters article</p>"},
		"2": {ID: "2", Title: "Lobsters second", Text: "<p>Lobsters second article</p>"},
	}}
	s.sources["lobsters"] = lobsters
	mux.HandleFunc("GET /lobsters/{$}", s.SourceIndex(lobsters, "top"))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../tests/readers.browser.cjs", srv.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
