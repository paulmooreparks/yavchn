package main

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestParseWinState_NoOpenMeansUnnamed(t *testing.T) {
	st := parseWinState(mustQuery(t, "page=2"))
	if st.named || len(st.open) != 0 {
		t.Fatalf("want an unnamed, empty state, got %+v", st)
	}
	if got := winURL("/hn/", url.Values{}, st); got != "/hn/" {
		t.Fatalf("unnamed state should add nothing to the address, got %q", got)
	}
}

func TestParseWinState_DropsWhatItCannotRead(t *testing.T) {
	st := parseWinState(mustQuery(t,
		"open=hn-1,bad key,hn-1,lobsters-abc&top=nope&min=hn-1,hn-9&p.hn-1=floating:0.1,0.2,0.5,0.6&p.lobsters-abc=floating:2,0,0.5,0.5"))
	if strings.Join(st.open, ",") != "hn-1,lobsters-abc" {
		t.Fatalf("open = %v", st.open)
	}
	if st.top != "" {
		t.Fatalf("top naming no open window should be dropped, got %q", st.top)
	}
	if !st.min["hn-1"] || st.min["hn-9"] {
		t.Fatalf("min = %v", st.min)
	}
	if _, ok := st.place["lobsters-abc"]; ok {
		t.Fatal("a placement outside 0..1 should be dropped")
	}
	if p := st.place["hn-1"]; p.String() != "floating:0.1,0.2,0.5,0.6" {
		t.Fatalf("placement = %q", p.String())
	}
}

func TestParsePlacement_CountsNumbersPerMode(t *testing.T) {
	for raw, ok := range map[string]bool{
		"maximized:0.06,0.05,0.55,0.75":          true,
		"zone:0.06,0.05,0.55,0.75,0,0,0.5,0.5":   true,
		"zone:0.06,0.05,0.55,0.75":               false,
		"dock-bottom:0.06,0.05,0.55,0.75,0.22":   true,
		"dock-bottom:0.06,0.05,0.55,0.75":        false,
		"sideways:0.06,0.05,0.55,0.75":           false,
		"floating:0.06,0.05,0,0.75":              false,
		"floating:0.06,0.05,0.55,0.75;color:red": false,
	} {
		if _, got := parsePlacement(raw); got != ok {
			t.Errorf("parsePlacement(%q) ok = %v, want %v", raw, got, ok)
		}
	}
}

func TestWinState_FrontAndStack(t *testing.T) {
	st := parseWinState(mustQuery(t, "open=a,b,c&top=a"))
	if st.front() != "a" || strings.Join(st.stack(), ",") != "b,c,a" {
		t.Fatalf("front %q stack %v", st.front(), st.stack())
	}
	// Minimise-all leaves top naming a minimised window: nothing in front.
	st = parseWinState(mustQuery(t, "open=a,b&top=b&min=a,b"))
	if st.front() != "" || st.shown() {
		t.Fatalf("front %q shown %v", st.front(), st.shown())
	}
	// No top: the last window not minimised.
	st = parseWinState(mustQuery(t, "open=a,b,c&min=c"))
	if st.front() != "b" {
		t.Fatalf("front %q", st.front())
	}
}

func TestWinState_ButtonAddresses(t *testing.T) {
	q := mustQuery(t, "page=2&open=hn-1,hn-2&top=hn-2&p.hn-2=floating:0.1,0.1,0.5,0.5")
	st, rest := parseWinState(q), restParams(q)

	if got := winURL("/hn/", rest, st.closed("hn-2")); got != "/hn/?page=2&open=hn-1" {
		t.Errorf("close = %q", got)
	}
	if got := winURL("/hn/", rest, st.closed("hn-1").closed("hn-2")); got != "/hn/?page=2&open=" {
		t.Errorf("closing the last window should keep open=, got %q", got)
	}
	if got := winURL("/hn/", rest, st.minimizeToggled("hn-2")); got != "/hn/?page=2&open=hn-1,hn-2&min=hn-2&p.hn-2=floating:0.1,0.1,0.5,0.5" {
		t.Errorf("minimize = %q", got)
	}
	if got := winURL("/hn/", rest, st.maximizeToggled("hn-2", storyDef)); got != "/hn/?page=2&open=hn-1,hn-2&top=hn-2&p.hn-2=maximized:0.1,0.1,0.5,0.5" {
		t.Errorf("maximize a floating window = %q", got)
	}
	// A window with no placement opens maximised, so its button restores it.
	if got := winURL("/hn/", rest, st.maximizeToggled("hn-1", storyDef)); !strings.Contains(got, "p.hn-1=floating:0.06,0.05,0.55,0.75") {
		t.Errorf("restore a maximised window = %q", got)
	}
}

func TestWinState_Attrs(t *testing.T) {
	st := parseWinState(mustQuery(t, "open=a,b,c&p.a=zone:0.06,0.05,0.55,0.75,0,0,0.5,0.5&p.b=dock-bottom:0.06,0.05,0.55,0.75,0.22"))
	if a := st.attrs("a", storyDef); a.Mode != "zone" || !strings.Contains(string(a.Style), "--zone-w:0.5") {
		t.Errorf("zone attrs = %+v", a)
	}
	if a := st.attrs("b", storyDef); a.Mode != "dock-bottom" || a.Edge != "bottom" || !strings.Contains(string(a.Style), "--win-dock-size:0.22") {
		t.Errorf("dock attrs = %+v", a)
	}
	if a := st.attrs("c", storyDef); a.Mode != storyWinMode || a.Style != "" {
		t.Errorf("default attrs = %+v", a)
	}
	attrs := map[string]winAttrs{"a": st.attrs("a", storyDef), "b": st.attrs("b", storyDef), "c": st.attrs("c", storyDef)}
	if got := string(st.layerStyle(attrs)); got != "--dock-bottom:22%" {
		t.Errorf("layer style = %q", got)
	}
	// A window whose markup docks it takes its strip with no placement in
	// the address, as the replies watcher does.
	st = parseWinState(mustQuery(t, "open=replies"))
	attrs = map[string]winAttrs{"replies": st.attrs("replies", dockedAt("right", 0.3))}
	if got := string(st.layerStyle(attrs)); got != "--dock-right:30%" {
		t.Errorf("default dock layer style = %q", got)
	}
}

var storyDef = winAttrs{Mode: storyWinMode}

// --- rendering ---

type fakeSource struct {
	name  string
	items map[string]*Item
}

func (f *fakeSource) Name() string  { return f.name }
func (f *fakeSource) Label() string { return "Fake " + f.name }
func (f *fakeSource) Tabs() []TabDef {
	return []TabDef{{Slug: "top", Label: "Top"}, {Slug: "new", Label: "New"}}
}
func (f *fakeSource) ValidTab(s string) bool                 { return s == "top" || s == "new" }
func (f *fakeSource) DefaultTab() string                     { return "top" }
func (f *fakeSource) StartBackgroundRefresh(context.Context) {}
func (f *fakeSource) StoryDiscussionURL(id string) string    { return "https://example.org/item?id=" + id }
func (f *fakeSource) StoryIDs(context.Context, string, int, bool) ([]string, bool, error) {
	return []string{"1", "2"}, false, nil
}
func (f *fakeSource) Item(_ context.Context, id string) (*Item, error) {
	if it, ok := f.items[id]; ok {
		return it, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeSource) ItemsParallel(ctx context.Context, ids []string) []*Item {
	out := make([]*Item, len(ids))
	for i, id := range ids {
		out[i], _ = f.Item(ctx, id)
	}
	return out
}
func (f *fakeSource) StoryThread(context.Context, string, string) (*StoryThread, error) {
	return &StoryThread{}, nil
}

func testServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	tpl, err := template.ParseFS(assets, "templates/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{name: "hn", items: map[string]*Item{
		"1": {ID: "1", Title: "First <story>", URL: "https://a.example/one", By: "ann", Score: 10, Descendants: 3},
		"2": {ID: "2", Title: "Ask: second", Text: "<p>Body <script>x</script></p>", By: "bob"},
	}}
	s := NewServer(map[string]Source{"hn": src}, nil, "hn", nil, tpl, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /settings/view", s.ViewSetting)
	mux.HandleFunc("GET /user", s.UserLookup)
	mux.HandleFunc("GET /settings", s.appletPage("settings"))
	mux.HandleFunc("GET /hn/{$}", s.SourceIndex(src, "top"))
	mux.HandleFunc("GET /hn/s/{id}", windowRedirect("/hn/", "hn"))
	mux.HandleFunc("GET /window/{key}", s.Window)
	mux.HandleFunc("GET /story/{source}/{id}", s.StoryPage)
	mux.HandleFunc("GET /pinned/{$}", s.Pinned)
	return s, mux
}

func get(t *testing.T, mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func TestRender_ListWithWindows(t *testing.T) {
	_, mux := testServer(t)
	rec := get(t, mux, "/hn/?open=hn-1,hn-2,hn-404,zz-1&top=hn-1&p.hn-2=floating:0.1,0.1,0.5,0.5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-win-open="hn-1"`,
		`href="/story/hn/1"`,
		`First &lt;story&gt;`,
		`data-md-pane="detail"`,
		`data-win="hn-2" data-win-mode="floating"`,
		`style="--win-x:0.1; --win-y:0.1; --win-w:0.5; --win-h:0.5"`,
		`class="win story-win active" data-win="hn-1" data-win-mode="maximized"`,
		`data-win="hn-404"`, // an unknown story still gets a window, saying so
		`Story unavailable`,
		`<a class="story-poster" href="/user/hn/ann" data-win-open="user-hn-ann">ann</a>`,
		`Body </p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, `data-win="zz-1"`) || strings.Contains(body, "<script>x") {
		t.Error("an unknown source's window, or an unsanitised text post, was rendered")
	}
	// The active window is drawn last.
	if strings.Index(body, `data-win="hn-1"`) < strings.Index(body, `data-win="hn-2"`) {
		t.Error("the window in front should come last")
	}
}

func TestRender_WindowFragmentAndStoryPage(t *testing.T) {
	_, mux := testServer(t)
	rec := get(t, mux, "/window/hn-1")
	if rec.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(rec.Body.String()), `<section class="win story-win"`) {
		t.Fatalf("fragment %d: %.200s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("the fragment should be the window alone")
	}
	if rec := get(t, mux, "/window/bad%20key"); rec.Code != http.StatusNotFound {
		t.Errorf("bad key: %d", rec.Code)
	}
	if rec := get(t, mux, "/window/story"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-applet="story" data-state-key="story"`) {
		t.Errorf("reader bootstrap: %d: %.200s", rec.Code, rec.Body)
	}
	if rec := get(t, mux, "/story/hn/1"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `class="story"`) {
		t.Errorf("story page: %d", rec.Code)
	}
	if rec := get(t, mux, "/pinned/"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-source="pinned"`) {
		t.Errorf("pinned: %d", rec.Code)
	}
}

func TestRender_LegacySelectionRedirects(t *testing.T) {
	_, mux := testServer(t)
	rec := get(t, mux, "/hn/s/123?page=2")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/hn/?page=2&open=hn-123&top=hn-123" {
		t.Fatalf("Location %q", loc)
	}
	if rec := get(t, mux, "/hn/s/12%2F3"); rec.Code != http.StatusNotFound {
		t.Errorf("a malformed id should 404, got %d", rec.Code)
	}
}
