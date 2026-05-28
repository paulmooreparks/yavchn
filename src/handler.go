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
	"strconv"
	"strings"
	"sync"
	"time"
)

const pageSize = 30

type Server struct {
	sources       map[string]Source // "hn", "lobsters"
	defaultSource string            // "hn" — used when / is hit with no stored choice
	hn            *HN               // direct ref for search (HN-only)
	tpl           *template.Template
	extract       *Extractor
	db            *sql.DB
}

func NewServer(sources map[string]Source, defaultSource string, hn *HN, tpl *template.Template, extract *Extractor, db *sql.DB) *Server {
	return &Server{sources: sources, defaultSource: defaultSource, hn: hn, tpl: tpl, extract: extract, db: db}
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

type listVM struct {
	Source       string  // active source name: "hn" / "lobsters" / "pinned"
	SourceLabel  string  // active source label: "Hacker News" / "Lobsters"
	Tab          string  // active tab slug within the source
	AllSources   []sourceOptVM // for the header source-selector dropdown
	Query        string
	Tabs         []tabVM
	Stories      []storyVM
	Page         int
	HasPrev      bool
	HasNext      bool
	PrevURL      string
	NextURL      string
	Selected     *selectedVM
	ListError    string
	SelectError  string
	RetryURL     string
	ShowSearch   bool // false on Lobsters (no JSON search API) and Pinned views
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

type storyVM struct {
	Rank      int
	ID        string
	Source    string // "hn" or "lobsters" — emitted as data-source for the pin/dismiss/visited stores
	Title     string
	URL       string
	Host      string
	Score     int
	By        string
	Age       string
	Comments  int
	HNURL     string // discussion URL on the source's own site
	Selected  bool
	SelectURL string
}

type selectedVM struct {
	ID            string
	Source        string
	Title         string
	URL           string
	Host          string
	HNURL         string // discussion URL on the source's own site (kept name for template back-compat)
	ExternalLabel string // "Open on HN ↑" / "Open on lobste.rs ↑"
	HasArticle    bool
}

type commentVM struct {
	ID          string
	Author      string
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

// SourceIndex serves a list page for a specific source + tab. Routed by
// main.go with paths like /hn/{tab}/, /hn/{tab}/s/{id}, /lobsters/{tab}/, etc.
// The source and tab come from the URL path.
func (s *Server) SourceIndex(source Source, tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
			page = p
		}

		var selectedID string
		if idStr := r.PathValue("id"); idStr != "" {
			selectedID = idStr
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		pageIDs, hasNext, idsErr := source.StoryIDs(ctx, tab, page)
		if idsErr != nil {
			slog.Warn("storyids unavailable", "source", source.Name(), "tab", tab, "err", idsErr, "path", r.URL.Path)
			s.renderShellWithListError(w, r, source.Name(), tab, page, selectedID,
				"The "+source.Label()+" / "+tabLabel(source, tab)+" feed couldn't be loaded right now.")
			return
		}
		if len(pageIDs) == 0 {
			http.NotFound(w, r)
			return
		}

		var (
			wg      sync.WaitGroup
			items   []*Item
			selItem *Item
			selErr  error
		)
		wg.Add(1)
		go func() { defer wg.Done(); items = source.ItemsParallel(ctx, pageIDs) }()
		if selectedID != "" {
			wg.Add(1)
			go func() { defer wg.Done(); selItem, selErr = source.Item(ctx, selectedID) }()
		}
		wg.Wait()

		rankBase := (page - 1) * pageSize

		vm := listVM{
			Source:      source.Name(),
			SourceLabel: source.Label(),
			Tab:         tab,
			AllSources:  s.buildSourceOpts(source.Name()),
			Tabs:        buildTabs(source, tab),
			Page:        page,
			HasPrev:     page > 1,
			HasNext:     hasNext,
			PrevURL:     buildPagerURL(source.Name(), tab, selectedID, page-1),
			NextURL:     buildPagerURL(source.Name(), tab, selectedID, page+1),
			RetryURL:    r.URL.RequestURI(),
			ShowSearch:  source.Name() == "hn", // /lobsters has no JSON search; /pinned doesn't search
		}

		for i, item := range items {
			if item == nil || item.Dead || item.Deleted {
				continue
			}
			host, displayURL := storyURLs(source, item)
			vm.Stories = append(vm.Stories, storyVM{
				Rank:      rankBase + i + 1,
				ID:        item.ID,
				Source:    source.Name(),
				Title:     item.Title,
				URL:       displayURL,
				Host:      host,
				Score:     item.Score,
				By:        item.By,
				Age:       relTime(item.Time),
				Comments:  item.Descendants,
				HNURL:     source.StoryDiscussionURL(item.ID),
				Selected:  item.ID == selectedID,
				SelectURL: buildSelectURL(source.Name(), tab, item.ID),
			})
		}

		if selectedID != "" {
			s.fillSelected(&vm, source, selItem, selErr, selectedID)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
			slog.Error("render index", "err", err)
		}
	}
}

// fillSelected populates the Selected field on the listVM. Shared between
// the source index path and the pinned-shell path.
func (s *Server) fillSelected(vm *listVM, source Source, selItem *Item, selErr error, selectedID string) {
	switch {
	case selErr != nil || selItem == nil:
		slog.Warn("item fetch failed", "id", selectedID, "source", source.Name(), "err", selErr)
		vm.SelectError = "Couldn't load this story. It may have been removed, or the upstream is having a moment."
	case selItem.Dead || selItem.Deleted:
		vm.SelectError = "This story has been removed."
	default:
		host, displayURL := storyURLs(source, selItem)
		vm.Selected = &selectedVM{
			ID:            selItem.ID,
			Source:        source.Name(),
			Title:         selItem.Title,
			URL:           displayURL,
			Host:          host,
			HNURL:         source.StoryDiscussionURL(selItem.ID),
			ExternalLabel: externalLabelForSource(source.Name()),
			HasArticle:    selItem.URL != "",
		}
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

// Pinned serves the global Pinned tab. The story list is empty in the
// server-rendered shell; pinned.js populates it from localStorage on the
// client. The selected-story handling still runs server-side so the
// article + discussion panes work for /pinned/s/{source}/{id}.
func (s *Server) Pinned(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Selected story on /pinned/s/{source}/{id}. The source segment lets the
	// pin entry render its article from whichever source it originally came
	// from (HN or Lobsters).
	var selectedID, selectedSource string
	if idStr := r.PathValue("id"); idStr != "" {
		selectedID = idStr
	}
	if src := r.PathValue("source"); src != "" {
		selectedSource = src
	}
	// Backwards compat: /pinned/s/{id} without an explicit source defaults
	// to HN (matches the pre-multi-source pin entries).
	if selectedSource == "" && selectedID != "" {
		selectedSource = "hn"
	}

	var selItem *Item
	var selErr error
	var selSrc Source
	if selectedID != "" {
		var ok bool
		selSrc, ok = s.sources[selectedSource]
		if !ok {
			http.NotFound(w, r)
			return
		}
		selItem, selErr = selSrc.Item(ctx, selectedID)
	}

	vm := listVM{
		Source:      "pinned",
		SourceLabel: "Pinned",
		Tab:         "pinned",
		AllSources:  s.buildSourceOpts("pinned"),
		Page:        page,
		RetryURL:    r.URL.RequestURI(),
		ShowSearch:  false,
	}

	if selectedID != "" {
		s.fillSelected(&vm, selSrc, selItem, selErr, selectedID)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		slog.Error("render pinned shell", "err", err)
	}
}

// renderShellWithListError renders the page shell with a list-pane error placeholder.
func (s *Server) renderShellWithListError(w http.ResponseWriter, r *http.Request, sourceName, tab string, page int, selectedID, msg string) {
	src, ok := s.sources[sourceName]
	if !ok {
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}
	vm := listVM{
		Source:      sourceName,
		SourceLabel: src.Label(),
		Tab:         tab,
		AllSources:  s.buildSourceOpts(sourceName),
		Tabs:        buildTabs(src, tab),
		Page:        page,
		ListError:   msg,
		RetryURL:    r.URL.RequestURI(),
		ShowSearch:  sourceName == "hn",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		slog.Error("render shell", "err", err)
	}
}

// articleErrorTmpl renders the article-pane fallback when reader-mode
// extraction fails. The CTA is rendered through html/template so the
// href is auto-escaped and javascript: schemes are neutralised.
var articleErrorTmpl = template.Must(template.New("articleError").Parse(
	`<div class="article-stub">
  <h2>Reader-mode couldn't load this page</h2>
  {{ if . }}<a class="cta" href="{{ . }}" target="_blank" rel="noopener">Open article &uarr;</a>{{ end }}
  <p class="note">The source server didn't return readable HTML.</p>
</div>`))

var rateLimitedTmpl = template.Must(template.New("rateLimited").Parse(
	`<div class="article-stub">
  <h2>Too many article requests</h2>
  {{ if . }}<a class="cta" href="{{ . }}" target="_blank" rel="noopener">Open article &uarr;</a>{{ end }}
  <p class="note">You've hit the per-visitor rate limit. Wait a minute, or open the source page directly.</p>
</div>`))

const discussionErrorFragment = `<div class="empty-note"><p>Couldn't load the discussion right now. Use the "Open on HN &uarr;" link above to read it directly.</p></div>`

const discussionRateLimitedFragment = `<div class="empty-note"><p>You've hit the per-visitor rate limit for discussions. Wait a minute and try again, or use the "Open on HN &uarr;" link above to read on news.ycombinator.com.</p></div>`

func articleErrorHTML(rawURL string) string {
	var buf bytes.Buffer
	_ = articleErrorTmpl.Execute(&buf, rawURL)
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

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	var selectedID string
	if idStr := r.PathValue("id"); idStr != "" {
		selectedID = idStr
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	hits, hasMore, searchErr := s.hn.Search(ctx, q, page)
	if searchErr != nil {
		slog.Warn("search failed", "q", q, "err", searchErr)
		vm := listVM{
			Source:      "hn",
			SourceLabel: "Hacker News",
			Tab:         "search",
			AllSources:  s.buildSourceOpts("hn"),
			Query:       q,
			Tabs:        buildTabs(s.sources["hn"], ""),
			Page:        page,
			ListError:   "Search couldn't be run right now. The HN search service may be having a moment.",
			RetryURL:    r.URL.RequestURI(),
			ShowSearch:  true,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm)
		return
	}

	var (
		wg      sync.WaitGroup
		selItem *Item
		selErr  error
	)
	if selectedID != "" {
		wg.Add(1)
		go func() { defer wg.Done(); selItem, selErr = s.hn.Item(ctx, selectedID) }()
	}
	wg.Wait()

	vm := listVM{
		Source:      "hn",
		SourceLabel: "Hacker News",
		Tab:         "search",
		AllSources:  s.buildSourceOpts("hn"),
		Query:       q,
		Tabs:        buildTabs(s.sources["hn"], ""),
		Page:        page,
		HasPrev:     page > 1,
		HasNext:     hasMore,
		PrevURL:     buildSearchPagerURL(q, selectedID, page-1),
		NextURL:     buildSearchPagerURL(q, selectedID, page+1),
		RetryURL:    r.URL.RequestURI(),
		ShowSearch:  true,
	}

	for i, h := range hits {
		host, displayURL := searchHitURLs(h)
		vm.Stories = append(vm.Stories, storyVM{
			Rank:      (page-1)*pageSize + i + 1,
			ID:        h.ID,
			Source:    "hn",
			Title:     h.Title,
			URL:       displayURL,
			Host:      host,
			Score:     h.Points,
			By:        h.Author,
			Age:       relTime(h.CreatedAt),
			Comments:  h.NumComments,
			HNURL:     fmt.Sprintf("https://news.ycombinator.com/item?id=%s", h.ID),
			Selected:  h.ID == selectedID,
			SelectURL: buildSearchSelectURL(q, h.ID),
		})
	}

	if selectedID != "" {
		s.fillSelected(&vm, s.sources["hn"], selItem, selErr, selectedID)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		slog.Error("render search", "err", err)
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

func buildSearchSelectURL(q, id string) string {
	qs := url.Values{}
	qs.Set("q", q)
	return fmt.Sprintf("/hn/search/s/%s?%s", id, qs.Encode())
}

func buildSearchPagerURL(q, selectedID string, page int) string {
	base := "/hn/search"
	if selectedID != "" {
		base = fmt.Sprintf("/hn/search/s/%s", selectedID)
	}
	qs := url.Values{}
	qs.Set("q", q)
	if page > 1 {
		qs.Set("page", strconv.Itoa(page))
	}
	return base + "?" + qs.Encode()
}

func (s *Server) ArticleAPI(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeFragment(w, http.StatusBadRequest, `<div class="article-stub"><p class="note">Missing URL.</p></div>`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), extractTimeout+2*time.Second)
	defer cancel()

	article, err := s.extract.Get(ctx, rawURL, clientIP(r))
	if err != nil {
		if errors.Is(err, errRateLimited) {
			slog.Info("article extract rate-limited", "url", rawURL, "ip", clientIP(r))
			w.Header().Set("Retry-After", "60")
			writeFragment(w, http.StatusTooManyRequests, rateLimitedHTML(rawURL))
			return
		}
		slog.Warn("article extract failed", "url", rawURL, "err", err)
		writeFragment(w, http.StatusOK, articleErrorHTML(rawURL))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "article.html.tmpl", article); err != nil {
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
			slog.Info("discussion rate-limited", "id", idStr, "source", sourceName, "ip", clientIP(r))
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
		HTML:      template.HTML(sanitizeHTML(c.Text)),
		HNURL:     commentExternalURL(src, c.ID),
		CreatedAt: c.CreatedAt,
	}
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
	out = append(out, sourceOptVM{
		Name:   "pinned",
		Label:  "Pinned",
		URL:    "/pinned/",
		Active: activeName == "pinned",
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

// buildSelectURL builds the URL for clicking a story in the list. Tab "" or
// default-tab → /{source}/s/{id}. Non-default tab → /{source}/{tab}/s/{id}.
func buildSelectURL(source, tab, id string) string {
	if tab == "" || isDefaultTab(source, tab) {
		return fmt.Sprintf("/%s/s/%s", source, id)
	}
	return fmt.Sprintf("/%s/%s/s/%s", source, tab, id)
}

func buildPagerURL(source, tab, selectedID string, page int) string {
	q := ""
	if page > 1 {
		q = fmt.Sprintf("?page=%d", page)
	}
	base := "/" + source + "/"
	if tab != "" && !isDefaultTab(source, tab) {
		base = "/" + source + "/" + tab + "/"
	}
	if selectedID != "" {
		base = strings.TrimRight(base, "/") + "/s/" + selectedID
	}
	return base + q
}

// isDefaultTab tells whether a tab slug is the source's default (rendered at
// the bare /{source}/ URL). Hardcoded to avoid passing a Source into URL
// builders.
func isDefaultTab(source, tab string) bool {
	switch source {
	case "hn":
		return tab == "top"
	case "lobsters":
		return tab == "hottest"
	}
	return false
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
