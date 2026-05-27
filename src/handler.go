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
	hn      *HN
	tpl     *template.Template
	extract *Extractor
	db      *sql.DB
}

func NewServer(hn *HN, tpl *template.Template, extract *Extractor, db *sql.DB) *Server {
	return &Server{hn: hn, tpl: tpl, extract: extract, db: db}
}

func (s *Server) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.db.PingContext(ctx); err != nil {
		// Log the detail privately; don't leak SQLite version / paths / lock
		// state to the public endpoint.
		slog.Warn("healthz db ping failed", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"db_unavailable"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type listVM struct {
	Source      string
	Tabs        []tabVM
	Stories     []storyVM
	Page        int
	HasPrev     bool
	HasNext     bool
	PrevURL     string
	NextURL     string
	Selected    *selectedVM
	ListError   string
	SelectError string
	RetryURL    string
}

type tabVM struct {
	Label  string
	URL    string
	Active bool
}

type storyVM struct {
	Rank      int
	ID        int64
	Title     string
	URL       string
	Host      string
	Score     int
	By        string
	Age       string
	Comments  int
	HNURL     string
	Selected  bool
	SelectURL string
}

type selectedVM struct {
	ID         int64
	Title      string
	URL        string
	Host       string
	HNURL      string
	HasArticle bool
}

type commentVM struct {
	Author      string
	Age         string
	HTML        template.HTML
	HNURL       string
	Children    []*commentVM
	Descendants int   // total nested comment count, for the "[N replies]" indicator when collapsed
	CreatedAt   int64 // unix seconds; emitted as data-ts for the "new since last visit" highlight
}

type threadVM struct {
	Comments []*commentVM
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	source := sourceFromPath(r.URL.Path)

	var selectedID int64
	if idStr := r.PathValue("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			selectedID = id
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	ids, idsErr := s.hn.StoryIDs(ctx, source)
	if idsErr != nil {
		slog.Warn("storyids unavailable", "source", source, "err", idsErr, "path", r.URL.Path)
		s.renderShellWithListError(w, r, source, page, selectedID,
			"The "+sourceLabel(source)+" couldn't be loaded right now. The HN API may be having a moment.")
		return
	}

	start := (page - 1) * pageSize
	if start >= len(ids) {
		http.NotFound(w, r)
		return
	}
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	pageIDs := ids[start:end]

	var (
		wg      sync.WaitGroup
		items   []*Item
		selItem *Item
		selErr  error
	)
	wg.Add(1)
	go func() { defer wg.Done(); items = s.hn.ItemsParallel(ctx, pageIDs) }()
	if selectedID != 0 {
		wg.Add(1)
		go func() { defer wg.Done(); selItem, selErr = s.hn.Item(ctx, selectedID) }()
	}
	wg.Wait()

	vm := listVM{
		Source:   source,
		Tabs:     buildTabs(source),
		Page:     page,
		HasPrev:  page > 1,
		HasNext:  end < len(ids),
		PrevURL:  buildPagerURL(source, selectedID, page-1),
		NextURL:  buildPagerURL(source, selectedID, page+1),
		RetryURL: r.URL.RequestURI(),
	}

	for i, item := range items {
		if item == nil || item.Dead || item.Deleted {
			continue
		}
		host, displayURL := storyURLs(item)
		vm.Stories = append(vm.Stories, storyVM{
			Rank:      start + i + 1,
			ID:        item.ID,
			Title:     item.Title,
			URL:       displayURL,
			Host:      host,
			Score:     item.Score,
			By:        item.By,
			Age:       relTime(item.Time),
			Comments:  item.Descendants,
			HNURL:     fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID),
			Selected:  item.ID == selectedID,
			SelectURL: buildSelectURL(source, item.ID),
		})
	}

	if selectedID != 0 {
		switch {
		case selErr != nil || selItem == nil:
			slog.Warn("item fetch failed", "id", selectedID, "err", selErr)
			vm.SelectError = "Couldn't load this story. It may have been removed, or the HN API is having a moment."
		case selItem.Dead || selItem.Deleted:
			vm.SelectError = "This story has been removed."
		default:
			host, displayURL := storyURLs(selItem)
			vm.Selected = &selectedVM{
				ID:         selItem.ID,
				Title:      selItem.Title,
				URL:        displayURL,
				Host:       host,
				HNURL:      fmt.Sprintf("https://news.ycombinator.com/item?id=%d", selItem.ID),
				HasArticle: selItem.URL != "",
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		slog.Error("render index", "err", err)
	}
}

// renderShellWithListError renders the page shell with a list-pane error placeholder.
// Used when StoryIDs itself fails -- we can't show the list, but the rest of the
// layout still gives the visitor something to look at.
func (s *Server) renderShellWithListError(w http.ResponseWriter, r *http.Request, source string, page int, selectedID int64, msg string) {
	vm := listVM{
		Source:    source,
		Tabs:      buildTabs(source),
		Page:      page,
		ListError: msg,
		RetryURL:  r.URL.RequestURI(),
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
		// 200 (not 5xx) so Cloudflare and friends don't replace our fragment
		// with their own branded error page. Inability to extract is a content
		// failure, not a server failure.
		writeFragment(w, http.StatusOK, articleErrorHTML(rawURL))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "article.html.tmpl", article); err != nil {
		slog.Error("render article", "err", err)
	}
}

func (s *Server) DiscussionAPI(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeFragment(w, http.StatusBadRequest, `<div class="empty-note"><p>Bad story id.</p></div>`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	thread, err := s.hn.StoryThread(ctx, id, clientIP(r))
	if err != nil {
		if errors.Is(err, errRateLimited) {
			slog.Info("discussion rate-limited", "id", id, "ip", clientIP(r))
			w.Header().Set("Retry-After", "60")
			writeFragment(w, http.StatusTooManyRequests, discussionRateLimitedFragment)
			return
		}
		slog.Warn("thread fetch failed", "id", id, "err", err)
		// 200 for the same reason as ArticleAPI -- Cloudflare swaps 5xx
		// origin responses for its own branded error page.
		writeFragment(w, http.StatusOK, discussionErrorFragment)
		return
	}

	vm := buildThreadVM(thread)
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

func buildThreadVM(t *StoryThread) *threadVM {
	vm := &threadVM{}
	for _, c := range t.Comments {
		vm.Comments = append(vm.Comments, commentToVM(c))
	}
	return vm
}

func commentToVM(c *Comment) *commentVM {
	cv := &commentVM{
		Author:    c.Author,
		Age:       relTime(c.CreatedAt),
		HTML:      template.HTML(sanitizeHTML(c.Text)),
		HNURL:     fmt.Sprintf("https://news.ycombinator.com/item?id=%d", c.ID),
		CreatedAt: c.CreatedAt,
	}
	for _, child := range c.Children {
		ccv := commentToVM(child)
		cv.Children = append(cv.Children, ccv)
		cv.Descendants += 1 + ccv.Descendants
	}
	return cv
}

func storyURLs(item *Item) (host, displayURL string) {
	displayURL = item.URL
	if displayURL == "" {
		displayURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
		host = "news.ycombinator.com"
		return
	}
	if u, err := url.Parse(displayURL); err == nil {
		host = strings.TrimPrefix(u.Host, "www.")
	}
	return
}

func sourceFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/show"):
		return SourceShow
	case strings.HasPrefix(path, "/ask"):
		return SourceAsk
	case strings.HasPrefix(path, "/new"):
		return SourceNew
	default:
		return SourceTop
	}
}

func sourceLabel(source string) string {
	switch source {
	case SourceShow:
		return "Show HN"
	case SourceAsk:
		return "Ask HN"
	case SourceNew:
		return "New"
	default:
		return "HN front page"
	}
}

func sourceBase(source string) string {
	if source == SourceTop {
		return ""
	}
	return "/" + source
}

func buildTabs(active string) []tabVM {
	defs := []struct{ src, label string }{
		{SourceTop, "Top"},
		{SourceShow, "Show HN"},
		{SourceAsk, "Ask HN"},
		{SourceNew, "New"},
	}
	out := make([]tabVM, 0, len(defs))
	for _, d := range defs {
		url := "/"
		if d.src != SourceTop {
			url = "/" + d.src + "/"
		}
		out = append(out, tabVM{Label: d.label, URL: url, Active: d.src == active})
	}
	return out
}

func buildSelectURL(source string, id int64) string {
	return fmt.Sprintf("%s/s/%d", sourceBase(source), id)
}

func buildPagerURL(source string, selectedID int64, page int) string {
	q := ""
	if page > 1 {
		q = fmt.Sprintf("?page=%d", page)
	}
	base := sourceBase(source)
	if selectedID != 0 {
		return fmt.Sprintf("%s/s/%d%s", base, selectedID, q)
	}
	if base == "" {
		if q == "" {
			return "/"
		}
		return "/" + q
	}
	return base + "/" + q
}

func relTime(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
