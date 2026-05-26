package main

import (
	"context"
	"database/sql"
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
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"db_unavailable","error":%q}`, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

type listVM struct {
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
	Author   string
	Age      string
	HTML     template.HTML
	HNURL    string
	Children []*commentVM
}

type threadVM struct {
	Comments []*commentVM
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	var selectedID int64
	if idStr := r.PathValue("id"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			selectedID = id
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	ids, idsErr := s.hn.TopIDs(ctx)
	if idsErr != nil {
		slog.Warn("topstories unavailable", "err", idsErr, "path", r.URL.Path)
		s.renderShellWithListError(w, r, page, selectedID,
			"The HN front page couldn't be loaded right now. The HN API may be having a moment.")
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
		Page:     page,
		HasPrev:  page > 1,
		HasNext:  end < len(ids),
		PrevURL:  buildPagerURL(selectedID, page-1),
		NextURL:  buildPagerURL(selectedID, page+1),
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
			SelectURL: fmt.Sprintf("/s/%d", item.ID),
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
// Used when TopIDs itself fails -- we can't show the list, but the rest of the layout
// still gives the visitor something to look at.
func (s *Server) renderShellWithListError(w http.ResponseWriter, r *http.Request, page int, selectedID int64, msg string) {
	vm := listVM{
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

const articleErrorFragment = `<div class="article-stub"><h2>Reader-mode couldn't load this page</h2><p class="note">The source server didn't return readable HTML. Use the "Open article" link above to read on the original site.</p></div>`

const discussionErrorFragment = `<div class="empty-note"><p>Couldn't load the discussion right now. Use the "Open on HN &uarr;" link above to read it directly.</p></div>`

func (s *Server) ArticleAPI(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeFragment(w, http.StatusBadRequest, `<div class="article-stub"><p class="note">Missing URL.</p></div>`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), extractTimeout+2*time.Second)
	defer cancel()

	article, err := s.extract.Get(ctx, rawURL)
	if err != nil {
		slog.Warn("article extract failed", "url", rawURL, "err", err)
		writeFragment(w, http.StatusBadGateway, articleErrorFragment)
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

	thread, err := s.hn.StoryThread(ctx, id)
	if err != nil {
		slog.Warn("thread fetch failed", "id", id, "err", err)
		writeFragment(w, http.StatusBadGateway, discussionErrorFragment)
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
		Author: c.Author,
		Age:    relTime(c.CreatedAt),
		HTML:   template.HTML(sanitizeHTML(c.Text)),
		HNURL:  fmt.Sprintf("https://news.ycombinator.com/item?id=%d", c.ID),
	}
	for _, child := range c.Children {
		cv.Children = append(cv.Children, commentToVM(child))
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

func buildPagerURL(selectedID int64, page int) string {
	q := ""
	if page > 1 {
		q = fmt.Sprintf("?page=%d", page)
	}
	if selectedID != 0 {
		return fmt.Sprintf("/s/%d%s", selectedID, q)
	}
	if q == "" {
		return "/"
	}
	return "/" + q
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
