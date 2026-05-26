package main

import (
	"context"
	"fmt"
	"html/template"
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
}

func NewServer(hn *HN, tpl *template.Template, extract *Extractor) *Server {
	return &Server{hn: hn, tpl: tpl, extract: extract}
}

type listVM struct {
	Stories  []storyVM
	Page     int
	HasPrev  bool
	HasNext  bool
	PrevURL  string
	NextURL  string
	Selected *selectedVM
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

	ids, err := s.hn.TopIDs(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream: %v", err), http.StatusBadGateway)
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
	)
	wg.Add(1)
	go func() { defer wg.Done(); items = s.hn.ItemsParallel(ctx, pageIDs) }()
	if selectedID != 0 {
		wg.Add(1)
		go func() { defer wg.Done(); selItem, _ = s.hn.Item(ctx, selectedID) }()
	}
	wg.Wait()

	vm := listVM{
		Page:    page,
		HasPrev: page > 1,
		HasNext: end < len(ids),
		PrevURL: buildPagerURL(selectedID, page-1),
		NextURL: buildPagerURL(selectedID, page+1),
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

	if selItem != nil && !selItem.Dead && !selItem.Deleted {
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) ArticleAPI(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), extractTimeout+2*time.Second)
	defer cancel()

	article, err := s.extract.Get(ctx, rawURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "article.html.tmpl", article); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) DiscussionAPI(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	thread, err := s.hn.StoryThread(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	vm := buildThreadVM(thread)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "discussion.html.tmpl", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
