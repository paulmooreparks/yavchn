package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pageSize = 30

type Server struct {
	sources       map[string]Source    // "hn", "lobsters"
	finders       []DiscussionProvider // ordered: HN, Lobsters — used by the discussion-finder
	defaultSource string               // "hn" — used when / is hit with no stored choice
	hn            *HN                  // direct ref for search (HN-only)
	tpl           *template.Template
	extract       *Extractor
	db            *sql.DB
	accounts      *accountService
	appletRate    *rateLimiter // caps the replies watcher's checks per visitor
}

func NewServer(sources map[string]Source, finders []DiscussionProvider, defaultSource string, hn *HN, tpl *template.Template, extract *Extractor, db *sql.DB) *Server {
	return &Server{sources: sources, finders: finders, defaultSource: defaultSource, hn: hn, tpl: tpl, extract: extract, db: db,
		appletRate: newRateLimiter(20, 60*time.Second)}
}

func (s *Server) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.db.PingContext(ctx); err != nil {
		slog.Warn("healthz db ping failed", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"db_unavailable"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// listVM is the desktop page: a list of stories in the sidebar and the
// story windows the address opens over the detail pane. Every list view
// (a source's tab, search, Pinned and Find) renders it, so moving between
// them swaps only the list and leaves the windows alone.
type listVM struct {
	menuContext
	NewReaderHref string
	Title         string        // the page's <title>
	Source        string        // active view: "hn" / "lobsters" / "pinned" / "collections" / "notes" / "find"
	SourceLabel   string        // active source label: "Hacker News" / "Lobsters"
	Tab           string        // active tab slug within the source
	AllSources    []sourceOptVM // the topbar's source switcher
	Query         string
	Tabs          []tabVM
	Stories       []storyVM
	Page          int
	HasPrev       bool
	HasNext       bool
	PrevURL       string
	NextURL       string
	ListError     string
	RetryURL      string
	ShowSearch    bool // false on Lobsters (no JSON search API) and Pinned views
	Finder        bool // the /find view: the toolbar takes a URL, the list its submissions
	FindURL       string
	FindHost      string
	FindNote      string       // why the finder's list is empty, when it is
	Pin           *pinFilterVM // a stored list's filters, from its address
	Win           windowsVM
}

// pinFilterVM is how the Pinned view is filtered and ordered. The pins
// live in the reader's browser, so pinned.js applies these; the server
// reads them from the address and renders the controls and chips, so a
// filtered view is an address like any other.
type pinFilterVM struct {
	Q           string
	Sources     []string // the sites chosen, "hn" and "lobsters"; none means every site
	Unread      bool     // only the stories the reader has not opened
	Sort        string   // "" (newest first), "oldest", "points" or "comments"
	Collections []string // the collections chosen in the Collections view; none means all

	Sites     []checkVM  // the Show menu's site checkboxes
	Sorts     []choiceVM // the Order menu's choices
	SortLabel string     // the current order, which labels the Order menu's button
	Chips     []chipVM
	ClearURL  string
	Base      string // the view's own address, such as /pinned/
	Noun      string // what the words filter narrows, such as "pinned stories"
}

// One collection chosen alone is the collection on display, which the
// Manage menu renames or deletes and whose rows offer removal.
func (f *pinFilterVM) OneCollection() string {
	if len(f.Collections) == 1 {
		return f.Collections[0]
	}
	return ""
}

// storedList names one browser-stored list: Pinned, Collections or Notes.
type storedList struct {
	Base, Noun     string
	Newest, Oldest string // the labels of the two time orders
}

var pinnedView = storedList{"/pinned/", "pinned stories", "Newest pin", "Oldest pin"}
var notesView = storedList{"/notes/", "notes", "Recently edited", "Oldest edit"}
var collectedView = storedList{"/collections/", "collected stories", "Recently added", "Oldest added"}

type choiceVM struct {
	Label, URL string
	Current    bool
}

type checkVM struct {
	Value, Label string
	Checked      bool
}

// A chip's ID names the collection a Collection chip removes, whose name
// only the browser knows.
type chipVM struct {
	Kind, Label, RemoveURL, ID string
}

// params is the filters as the address writes them, from their checked
// values, so a value the server refused is not carried into any link.
func (f *pinFilterVM) params() url.Values {
	v := url.Values{}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	for _, s := range f.Sources {
		v.Add("source", s)
	}
	if f.Unread {
		v.Set("show", "unread")
	}
	if f.Sort != "" {
		v.Set("sort", f.Sort)
	}
	for _, c := range f.Collections {
		v.Add("c", c)
	}
	return v
}

func pinFilter(r *http.Request, view storedList) *pinFilterVM {
	q := r.URL.Query()
	f := &pinFilterVM{Q: strings.TrimSpace(q.Get("q")), Base: view.Base, Noun: view.Noun}
	if len([]rune(f.Q)) > 100 {
		f.Q = string([]rune(f.Q)[:100])
	}
	sites := [][2]string{{"hn", "Hacker News"}, {"lobsters", "Lobsters"}}
	for _, s := range sites {
		if slices.Contains(q["source"], s[0]) {
			f.Sources = append(f.Sources, s[0])
		}
		f.Sites = append(f.Sites, checkVM{s[0], s[1], slices.Contains(q["source"], s[0])})
	}
	f.Unread = q.Get("show") == "unread"
	if s := q.Get("sort"); s == "oldest" || s == "points" || s == "comments" {
		f.Sort = s
	}
	if view.Base == collectedView.Base {
		for _, c := range q["c"] {
			if collectionID.MatchString(c) && !slices.Contains(f.Collections, c) && len(f.Collections) < 100 {
				f.Collections = append(f.Collections, c)
			}
		}
	}
	// The view's address with one value of a filter removed, keeping the
	// others, the order and the windows.
	wins := parseWinState(q)
	without := func(name, value string) string {
		v := f.params()
		kept := slices.DeleteFunc(slices.Clone(v[name]), func(s string) bool { return value == "" || s == value })
		v.Del(name)
		for _, s := range kept {
			v.Add(name, s)
		}
		return winURL(view.Base, v, wins)
	}
	for _, o := range [][2]string{{"", view.Newest}, {"oldest", view.Oldest}, {"points", "Points"}, {"comments", "Comments"}} {
		v := f.params()
		v.Del("sort")
		if o[0] != "" {
			v.Set("sort", o[0])
		}
		f.Sorts = append(f.Sorts, choiceVM{Label: o[1], URL: winURL(view.Base, v, wins), Current: o[0] == f.Sort})
		if o[0] == f.Sort {
			f.SortLabel = o[1]
		}
	}

	if f.Q != "" {
		f.Chips = append(f.Chips, chipVM{Kind: "Words", Label: f.Q, RemoveURL: without("q", "")})
	}
	for _, c := range f.Collections {
		f.Chips = append(f.Chips, chipVM{Kind: "Collection", Label: "a collection", RemoveURL: without("c", c), ID: c})
	}
	for _, s := range f.Sites {
		if s.Checked {
			f.Chips = append(f.Chips, chipVM{Kind: "Site", Label: s.Label, RemoveURL: without("source", s.Value)})
		}
	}
	if f.Unread {
		f.Chips = append(f.Chips, chipVM{Kind: "Show", Label: "Unread", RemoveURL: without("show", "")})
	}
	// Clearing the filters keeps the order, which is not one.
	rest := url.Values{}
	if f.Sort != "" {
		rest.Set("sort", f.Sort)
	}
	f.ClearURL = winURL(view.Base, rest, wins)
	return f
}

type sourceOptVM struct {
	Name   string
	Label  string
	URL    string
	Active bool
}

type tabVM struct {
	Label  string
	URL    string
	Active bool
}

// storyVM is one row of the list. Its link is the story's own page, and
// with script it opens the story's window instead.
type storyVM struct {
	NewReaderHref string
	Rank          int
	Key           string // window key, "<source>-<id>"
	ID            string
	Source        string // "hn" or "lobsters", emitted as data-source for the pin/dismiss/visited stores
	Title         string
	URL           string
	Host          string
	Score         int
	By            string
	Age           string
	Comments      int
	Where         string // the finder's subreddit and the like; "" for HN and Lobsters
	PageURL       string
	Section       string // set on the first row of a labelled section, as the finder's per-source groups
}

func newStoryVM(rank int, src Source, item *Item) storyVM {
	host, displayURL := storyURLs(src, item)
	return storyVM{
		Rank: rank, Key: src.Name() + "-" + item.ID, ID: item.ID, Source: src.Name(),
		Title: item.Title, URL: displayURL, Host: host, Score: item.Score, By: item.By,
		Age: relTime(item.Time), Comments: item.Descendants, PageURL: storyPageURL(src.Name(), item.ID),
	}
}

type commentVM struct {
	ID          string
	Author      string
	AuthorKey   string // the window of the author's profile, when the name can have one
	AuthorURL   string
	Age         string
	HTML        template.HTML
	HNURL       string
	Children    []*commentVM
	Descendants int
	CreatedAt   int64
}

type threadVM struct {
	Comments []*commentVM
}

// renderList renders the desktop page, once the windows the address opens
// are ready. status 0 means 200.
func (s *Server) renderList(w http.ResponseWriter, r *http.Request, vm *listVM, wins func() windowsVM, status int) {
	vm.menuContext = listMenu(r, vm.Source == "hn" || vm.Source == "lobsters")
	w.Header().Add("Vary", "Cookie")
	vm.Win = wins()
	if vm.WindowView {
		for _, win := range vm.Win.List {
			if win.Active && !win.Hidden && win.PageURL != "" && win.PageURL != "/" {
				vm.ClassicURL = win.PageURL
				break
			}
		}
	}
	if vm.View == "classic" || r.URL.Query().Has("view") {
		for i := range vm.AllSources {
			vm.AllSources[i].URL = viewURL(vm.AllSources[i].URL, vm.View)
		}
		for i := range vm.Tabs {
			vm.Tabs[i].URL = viewURL(vm.Tabs[i].URL, vm.View)
		}
		if vm.PrevURL != "" {
			vm.PrevURL = viewURL(vm.PrevURL, vm.View)
		}
		if vm.NextURL != "" {
			vm.NextURL = viewURL(vm.NextURL, vm.View)
		}
		if vm.RetryURL != "" {
			vm.RetryURL = viewURL(vm.RetryURL, vm.View)
		}
		if vm.Pin != nil {
			for i := range vm.Pin.Sorts {
				vm.Pin.Sorts[i].URL = viewURL(vm.Pin.Sorts[i].URL, vm.View)
			}
			for i := range vm.Pin.Chips {
				vm.Pin.Chips[i].RemoveURL = viewURL(vm.Pin.Chips[i].RemoveURL, vm.View)
			}
			vm.Pin.ClearURL = viewURL(vm.Pin.ClearURL, vm.View)
		}
	}
	vm.NewReaderHref = vm.Win.NewReaderHref
	for i := range vm.Stories {
		u, _ := url.Parse(vm.NewReaderHref)
		q := u.Query()
		q.Set("r."+q.Get("top"), vm.Stories[i].Key)
		u.RawQuery = q.Encode()
		vm.Stories[i].NewReaderHref = u.String()
	}
	if vm.Title == "" {
		vm.Title = "YAVCHN"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != 0 {
		w.WriteHeader(status)
	}
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		slog.Error("render list", "source", vm.Source, "err", err)
	}
}

func pageParam(r *http.Request) int {
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		return p
	}
	return 1
}

// pageHref is the request's own address with its page number changed and
// its windows kept, for the pager.
func pageHref(r *http.Request, page int) string {
	q := r.URL.Query()
	rest := restParams(q)
	if page > 1 {
		rest.Set("page", strconv.Itoa(page))
	} else {
		rest.Del("page")
	}
	return winURL(r.URL.Path, rest, parseWinState(q))
}

// revalidateFeed honors an explicit request to revalidate cached feed data.
func revalidateFeed(r *http.Request) bool {
	for _, directive := range strings.Split(r.Header.Get("Cache-Control"), ",") {
		if strings.EqualFold(strings.TrimSpace(directive), "no-cache") {
			return true
		}
	}
	return false
}

// SourceIndex serves a source's list page, such as /hn/ or /lobsters/newest/.
func (s *Server) SourceIndex(source Source, tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := pageParam(r)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		wins := s.startWindows(ctx, r)

		vm := listVM{
			Title:       source.Label() + " · " + tabLabel(source, tab) + " · YAVCHN",
			Source:      source.Name(),
			SourceLabel: source.Label(),
			Tab:         tab,
			AllSources:  s.buildSourceOpts(source.Name()),
			Tabs:        buildTabs(source, tab),
			Page:        page,
			RetryURL:    r.URL.RequestURI(),
			ShowSearch:  source.Name() == "hn", // /lobsters has no JSON search; /pinned doesn't search
		}

		pageIDs, hasNext, idsErr := source.StoryIDs(ctx, tab, page, revalidateFeed(r))
		if idsErr != nil {
			slog.Warn("storyids unavailable", "source", source.Name(), "tab", tab, "err", idsErr, "path", r.URL.Path)
			vm.ListError = "The " + source.Label() + " / " + tabLabel(source, tab) + " feed couldn't be loaded right now."
			s.renderList(w, r, &vm, wins, http.StatusServiceUnavailable)
			return
		}
		if len(pageIDs) == 0 {
			http.NotFound(w, r)
			return
		}

		vm.HasPrev, vm.HasNext = page > 1, hasNext
		vm.PrevURL, vm.NextURL = pageHref(r, page-1), pageHref(r, page+1)
		rankBase := (page - 1) * pageSize
		for i, item := range source.ItemsParallel(ctx, pageIDs) {
			if item == nil || item.Dead || item.Deleted {
				continue
			}
			vm.Stories = append(vm.Stories, newStoryVM(rankBase+i+1, source, item))
		}
		s.renderList(w, r, &vm, wins, 0)
	}
}

// externalLabelForSource returns the text for the "Open on X" link in
// the discussion pane header. Source-aware so HN reads "Open on HN" and
// Lobsters reads "Open on Lobsters".
func externalLabelForSource(sourceName string) string {
	switch sourceName {
	case "lobsters":
		return "Open on Lobsters"
	default:
		return "Open on HN"
	}
}

// Pinned serves the global Pinned view. The story list is empty in the
// server-rendered page; pinned.js fills it from localStorage, so the
// server keeps no per-reader state.
func (s *Server) Pinned(w http.ResponseWriter, r *http.Request) {
	s.storedList(w, r, "pinned", "Pinned", pinnedView)
}

// Collections serves the collected stories, of every collection or of
// those its c parameters choose. Collection names live in the browser, as
// pins do, so library.js names the collections and fills the list.
func (s *Server) Collections(w http.ResponseWriter, r *http.Request) {
	s.storedList(w, r, "collections", "Collections", collectedView)
}

// CollectionRedirect sends the first form of a collection's address,
// /collections/{id}/, to the Collections view with that collection chosen.
func (s *Server) CollectionRedirect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !collectionID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	q.Add("c", id)
	http.Redirect(w, r, "/collections/?"+q.Encode(), http.StatusMovedPermanently)
}

// Notes serves the stories that carry the reader's notes.
func (s *Server) Notes(w http.ResponseWriter, r *http.Request) {
	s.storedList(w, r, "notes", "Notes", notesView)
}

func (s *Server) storedList(w http.ResponseWriter, r *http.Request, source, label string, view storedList) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vm := listVM{
		Title:       label + " · YAVCHN",
		Source:      source,
		SourceLabel: "Saved",
		Tab:         source,
		AllSources:  s.buildSourceOpts(source),
		Page:        pageParam(r),
		RetryURL:    r.URL.RequestURI(),
		Pin:         pinFilter(r, view),
	}
	for _, t := range [][3]string{{"pinned", "Pinned", "/pinned/"}, {"collections", "Collections", "/collections/"}, {"notes", "Notes", "/notes/"}} {
		vm.Tabs = append(vm.Tabs, tabVM{Label: t[1], URL: t[2], Active: t[0] == source})
	}
	s.renderList(w, r, &vm, s.startWindows(ctx, r), 0)
}

// Window serves /window/{key}, the markup of one window, a story's or an
// applet's, which pudl-windows.js fetches when it opens without a page load.
func (s *Server) Window(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Cookie")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vm, ok := s.window(ctx, r, r.PathValue("key"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "window", vm); err != nil {
		slog.Error("render window", "key", vm.Key, "err", err)
	}
}

// StoryPage serves /story/{source}/{id}, a story's own page: its article
// and discussion without the list. It is where a story's link goes without
// script, and where a window's "Open as a page" button goes.
func (s *Server) StoryPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vm, ok := s.storyWindow(ctx, r.PathValue("source")+"-"+r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	page := struct {
		menuContext
		NewReaderHref string
		Title         string
		AllSources    []sourceOptVM
		Story         windowVM
	}{pageMenu(r, vm.Key, vm.Source), "/hn/?view=window&open=reader-1&top=reader-1", vm.Title + " · YAVCHN", s.buildSourceOpts(""), vm}
	page.StoriesURL = "/" + vm.Source + "/?view=classic"
	for i := range page.AllSources {
		page.AllSources[i].URL = viewURL(page.AllSources[i].URL, "classic")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "story.html.tmpl", page); err != nil {
		slog.Error("render story page", "key", vm.Key, "err", err)
	}
}

// articleErrorTmpl renders the article pane's fallback when reader-mode
// extraction fails. The link is rendered through html/template so the
// href is auto-escaped and javascript: schemes are neutralised.
var articleErrorTmpl = template.Must(template.New("articleError").Parse(
	`<div class="empty-state story-note">
  <p class="empty-state-title">Reader-mode couldn't load this page</p>
  <p class="empty-state-body">{{ .Message }}</p>
  <div class="empty-state-actions">
    {{ if .OriginalURL }}<a class="btn btn-sm" href="{{ .OriginalURL }}" target="_blank" rel="noopener">Open the article</a>{{ end }}
    {{ if .ArchiveURL }}<a class="btn btn-sm" href="{{ .ArchiveURL }}" target="_blank" rel="noopener noreferrer">Look for an archived copy</a>{{ end }}
  </div>
</div>`))

var rateLimitedTmpl = template.Must(template.New("rateLimited").Parse(
	`<div class="empty-state story-note">
  <p class="empty-state-title">Too many article requests</p>
  <p class="empty-state-body">You've hit the per-visitor rate limit. Wait a minute, or open the source page directly.</p>
  {{ if . }}<div class="empty-state-actions"><a class="btn btn-sm" href="{{ . }}" target="_blank" rel="noopener">Open the article</a></div>{{ end }}
</div>`))

const discussionErrorFragment = `<div class="empty-state story-note"><p class="empty-state-title">Couldn't load the discussion</p><p class="empty-state-body">The upstream didn't answer. The link above reads it on the source's own site.</p></div>`

const discussionRateLimitedFragment = `<div class="empty-state story-note"><p class="empty-state-title">Too many discussion requests</p><p class="empty-state-body">You've hit the per-visitor rate limit for discussions. Wait a minute and try again, or use the link above to read it on the source's own site.</p></div>`

func articleErrorHTML(rawURL string, err error) string {
	data := struct {
		OriginalURL string
		ArchiveURL  string
		Message     string
	}{OriginalURL: rawURL, Message: "The source server didn't return readable HTML."}
	var upstream *upstreamHTTPError
	if errors.As(err, &upstream) {
		data.Message = fmt.Sprintf("The source server returned HTTP %d (%s).", upstream.StatusCode, http.StatusText(upstream.StatusCode))
		switch upstream.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusUnavailableForLegalReasons:
			if u, parseErr := url.Parse(rawURL); parseErr == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" {
				u.User = nil
				u.Fragment = ""
				// Wayback's calendar lists captures without promising one is available.
				data.ArchiveURL = "https://web.archive.org/web/*/" + u.String()
			}
		}
	}
	var buf bytes.Buffer
	_ = articleErrorTmpl.Execute(&buf, data)
	return buf.String()
}

// excludedTmpl stands in for an article whose publisher asked YAVCHN not to
// show a copy (takedown.go). It offers no archived copy, which would defeat
// the request.
var excludedTmpl = template.Must(template.New("excluded").Parse(
	`<div class="empty-state story-note">
  <p class="empty-state-title">Read this article on its own site</p>
  <p class="empty-state-body">Its publisher asked YAVCHN not to show a copy of it here.</p>
  {{ if . }}<div class="empty-state-actions"><a class="btn btn-sm" href="{{ . }}" target="_blank" rel="noopener">Open the article</a></div>{{ end }}
</div>`))

func excludedHTML(rawURL string) string {
	var buf bytes.Buffer
	_ = excludedTmpl.Execute(&buf, rawURL)
	return buf.String()
}

func rateLimitedHTML(rawURL string) string {
	var buf bytes.Buffer
	_ = rateLimitedTmpl.Execute(&buf, rawURL)
	return buf.String()
}

// Search handles /hn/search. Lobsters search isn't supported (no JSON API);
// /lobsters/search returns 404. The plain /search route 301-redirects to
// /hn/search for backwards compatibility.
func (s *Server) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Redirect(w, r, "/hn/", http.StatusSeeOther)
		return
	}

	page := pageParam(r)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	wins := s.startWindows(ctx, r)

	hn := s.sources["hn"]
	vm := listVM{
		Title:       "Search: " + q + " · YAVCHN",
		Source:      "hn",
		SourceLabel: "Hacker News",
		Tab:         "search",
		AllSources:  s.buildSourceOpts("hn"),
		Query:       q,
		Tabs:        buildTabs(hn, ""),
		Page:        page,
		RetryURL:    r.URL.RequestURI(),
		ShowSearch:  true,
	}

	hits, hasMore, searchErr := s.hn.Search(ctx, q, page)
	if searchErr != nil {
		slog.Warn("search failed", "q", q, "err", searchErr)
		vm.ListError = "Search couldn't be run right now. The HN search service may be having a moment."
		s.renderList(w, r, &vm, wins, http.StatusServiceUnavailable)
		return
	}

	vm.HasPrev, vm.HasNext = page > 1, hasMore
	vm.PrevURL, vm.NextURL = pageHref(r, page-1), pageHref(r, page+1)
	for i, h := range hits {
		host, displayURL := searchHitURLs(h)
		vm.Stories = append(vm.Stories, storyVM{
			Rank:     (page-1)*pageSize + i + 1,
			Key:      "hn-" + h.ID,
			ID:       h.ID,
			Source:   "hn",
			Title:    h.Title,
			URL:      displayURL,
			Host:     host,
			Score:    h.Points,
			By:       h.Author,
			Age:      relTime(h.CreatedAt),
			Comments: h.NumComments,
			PageURL:  storyPageURL("hn", h.ID),
		})
	}
	s.renderList(w, r, &vm, wins, 0)
}

// Finder handles /find (empty state) and /find?url=<encoded> (results). It
// fans out across the registered DiscussionProviders and lists every
// submission of the URL, grouped by source, in the sidebar. Each one opens
// as a story window, like any other list's stories.
func (s *Server) Finder(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	wins := s.startWindows(ctx, r)

	vm := listVM{
		Title:      "Find discussions · YAVCHN",
		Source:     "find",
		AllSources: s.buildSourceOpts("find"),
		Finder:     true,
		FindURL:    rawURL,
		RetryURL:   r.URL.RequestURI(),
	}
	if rawURL == "" {
		s.renderList(w, r, &vm, wins, 0)
		return
	}
	if u, err := url.Parse(rawURL); err == nil {
		vm.FindHost = strings.TrimPrefix(u.Host, "www.")
	}
	vm.Title = "Discussions of " + vm.FindHost + " · YAVCHN"

	// Fan out across providers concurrently; one slow/erroring source
	// shouldn't sink the page.
	type result struct {
		idx  int
		subs []Submission
		err  error
	}
	results := make([]result, len(s.finders))
	var wg sync.WaitGroup
	for i, p := range s.finders {
		i, p := i, p
		wg.Add(1)
		go func() {
			defer wg.Done()
			subs, err := p.FindByURL(ctx, rawURL)
			results[i] = result{idx: i, subs: subs, err: err}
		}()
	}
	wg.Wait()

	errCount := 0
	for _, res := range results {
		name := s.finders[res.idx].ProviderName()
		if res.err != nil {
			slog.Warn("finder provider failed", "provider", name, "url", rawURL, "err", res.err)
			errCount++
			continue
		}
		section := finderSourceLabel(name)
		for j, sub := range res.subs {
			// A submission opens as a story window, so it needs a source
			// that can show one.
			if _, ok := s.sources[sub.Source]; !ok || !storyIDRE.MatchString(sub.ID) {
				continue
			}
			st := storyVM{
				Rank:     len(vm.Stories) + 1,
				Key:      sub.Source + "-" + sub.ID,
				ID:       sub.ID,
				Source:   sub.Source,
				Title:    sub.Title,
				URL:      rawURL,
				Host:     vm.FindHost,
				Score:    sub.Score,
				Age:      relTime(sub.CreatedAt),
				Comments: sub.NumComments,
				Where:    sub.Where,
				PageURL:  storyPageURL(sub.Source, sub.ID),
			}
			if j == 0 {
				st.Section = fmt.Sprintf("%s · %d", section, len(res.subs))
			}
			vm.Stories = append(vm.Stories, st)
		}
	}

	switch {
	case len(vm.Stories) == 0 && errCount == len(s.finders) && len(s.finders) > 0:
		vm.ListError = "Couldn't reach the discussion sources right now. Try again in a moment."
	case len(vm.Stories) == 0:
		vm.FindNote = "No discussions of this URL on Hacker News or Lobsters."
	}
	s.renderList(w, r, &vm, wins, 0)
}

func finderSourceLabel(name string) string {
	switch name {
	case "lobsters":
		return "Lobsters"
	default:
		return "Hacker News"
	}
}

func searchHitURLs(h *SearchHit) (host, displayURL string) {
	displayURL = h.URL
	if displayURL == "" {
		displayURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%s", h.ID)
		host = "news.ycombinator.com"
		return
	}
	if u, err := url.Parse(displayURL); err == nil {
		host = strings.TrimPrefix(u.Host, "www.")
	}
	return
}

func (s *Server) ArticleAPI(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeFragment(w, http.StatusBadRequest, `<div class="article-stub"><p class="note">Missing URL.</p></div>`)
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), extractTimeout+2*time.Second)
	defer cancel()

	var article *Article
	var err error
	if refresh {
		article, err = s.extract.ForceGet(ctx, rawURL, clientIP(r))
	} else {
		article, err = s.extract.Get(ctx, rawURL, clientIP(r))
	}
	if err != nil {
		if errors.Is(err, errRateLimited) {
			slog.Info("article extract rate-limited", "url", rawURL)
			w.Header().Set("Retry-After", "60")
			writeFragment(w, http.StatusTooManyRequests, rateLimitedHTML(rawURL))
			return
		}
		if errors.Is(err, errExcluded) {
			writeFragment(w, http.StatusOK, excludedHTML(rawURL))
			return
		}
		slog.Warn("article extract failed", "url", rawURL, "err", err)
		writeFragment(w, http.StatusOK, articleErrorHTML(rawURL, err))
		return
	}

	// The extraction is cached and shared, so the links are rewritten on a
	// copy, as the article is sent.
	shown := *article
	shown.Content = template.HTML(linkThreads(string(article.Content)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "article.html.tmpl", &shown); err != nil {
		slog.Error("render article", "err", err)
	}
}

// DiscussionAPI fetches the comment thread for a story. ?source=hn|lobsters
// selects the source; defaults to HN for backwards compat with existing
// pinned entries / cached URLs that don't carry the source.
func (s *Server) DiscussionAPI(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		writeFragment(w, http.StatusBadRequest, `<div class="empty-note"><p>Missing story id.</p></div>`)
		return
	}
	sourceName := r.URL.Query().Get("source")
	if sourceName == "" {
		sourceName = "hn"
	}
	src, ok := s.sources[sourceName]
	if !ok {
		writeFragment(w, http.StatusBadRequest, `<div class="empty-note"><p>Unknown source.</p></div>`)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	thread, err := src.StoryThread(ctx, idStr, clientIP(r))
	if err != nil {
		if errors.Is(err, errRateLimited) {
			slog.Info("discussion rate-limited", "id", idStr, "source", sourceName)
			w.Header().Set("Retry-After", "60")
			writeFragment(w, http.StatusTooManyRequests, discussionRateLimitedFragment)
			return
		}
		slog.Warn("thread fetch failed", "id", idStr, "source", sourceName, "err", err)
		writeFragment(w, http.StatusOK, discussionErrorFragment)
		return
	}

	vm := buildThreadVM(thread, src)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "discussion.html.tmpl", vm); err != nil {
		slog.Error("render discussion", "err", err)
	}
}

func writeFragment(w http.ResponseWriter, status int, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
}

func buildThreadVM(t *StoryThread, src Source) *threadVM {
	vm := &threadVM{}
	for _, c := range t.Comments {
		vm.Comments = append(vm.Comments, commentToVM(c, src))
	}
	return vm
}

func commentToVM(c *Comment, src Source) *commentVM {
	cv := &commentVM{
		ID:        c.ID,
		Author:    c.Author,
		Age:       relTime(c.CreatedAt),
		HTML:      template.HTML(linkThreads(sanitizeHTML(c.Text))),
		HNURL:     commentExternalURL(src, c.ID),
		CreatedAt: c.CreatedAt,
	}
	cv.AuthorKey, cv.AuthorURL = authorLink(src.Name(), c.Author)
	for _, child := range c.Children {
		ccv := commentToVM(child, src)
		cv.Children = append(cv.Children, ccv)
		cv.Descendants += 1 + ccv.Descendants
	}
	return cv
}

// commentExternalURL returns the URL of a single comment on the source's own
// site. HN uses /item?id=N; Lobsters uses /c/{short_id}.
func commentExternalURL(src Source, commentID string) string {
	switch src.Name() {
	case "lobsters":
		return fmt.Sprintf("https://lobste.rs/c/%s", commentID)
	default: // hn and fallback
		return fmt.Sprintf("https://news.ycombinator.com/item?id=%s", commentID)
	}
}

func storyURLs(src Source, item *Item) (host, displayURL string) {
	displayURL = item.URL
	if displayURL == "" {
		displayURL = src.StoryDiscussionURL(item.ID)
		switch src.Name() {
		case "lobsters":
			host = "lobste.rs"
		default:
			host = "news.ycombinator.com"
		}
		return
	}
	if u, err := url.Parse(displayURL); err == nil {
		host = strings.TrimPrefix(u.Host, "www.")
	}
	return
}

// tabLabel returns the display label for a tab slug on the given source.
func tabLabel(src Source, tabSlug string) string {
	for _, t := range src.Tabs() {
		if t.Slug == tabSlug {
			return t.Label
		}
	}
	return tabSlug
}

func (s *Server) buildSourceOpts(activeName string) []sourceOptVM {
	// Stable display order: HN first, then Lobsters, then Pinned (peer of
	// the sources since it's a top-level view, not a sub-tab of either).
	order := []string{"hn", "lobsters"}
	out := make([]sourceOptVM, 0, len(order)+1)
	for _, name := range order {
		src, ok := s.sources[name]
		if !ok {
			continue
		}
		out = append(out, sourceOptVM{
			Name:   name,
			Label:  src.Label(),
			URL:    "/" + name + "/",
			Active: name == activeName,
		})
	}
	// Saved holds the reader's own lists: Pinned, Collections and Notes,
	// which its list bar shows as tabs.
	out = append(out, sourceOptVM{
		Name:   "pinned",
		Label:  "Saved",
		URL:    "/pinned/",
		Active: activeName == "pinned" || activeName == "collections" || activeName == "notes",
	})
	out = append(out, sourceOptVM{
		Name:   "find",
		Label:  "Find",
		URL:    "/find",
		Active: activeName == "find",
	})
	return out
}

func buildTabs(src Source, activeTab string) []tabVM {
	// Pinned is no longer a tab in this row -- it's a peer of HN/Lobsters
	// in the source-picker (see buildSourceOpts).
	defs := src.Tabs()
	out := make([]tabVM, 0, len(defs))
	for _, d := range defs {
		path := "/" + src.Name() + "/"
		if d.Slug != src.DefaultTab() {
			path = "/" + src.Name() + "/" + d.Slug + "/"
		}
		out = append(out, tabVM{Label: d.Label, URL: path, Active: d.Slug == activeTab})
	}
	return out
}

// relTime renders relative-time for recent items and switches to a short
// absolute date past 30 days. "12d" is easy to grok; "1638d" is not.
func relTime(unix int64) string {
	t := time.Unix(unix, 0)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}
