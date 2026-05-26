package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const pageSize = 30

type Server struct {
	hn  *HN
	tpl *template.Template
}

func NewServer(hn *HN, tpl *template.Template) *Server {
	return &Server{hn: hn, tpl: tpl}
}

type listVM struct {
	Stories  []storyVM
	Page     int
	PrevPage int
	NextPage int
	HasPrev  bool
	HasNext  bool
}

type storyVM struct {
	Rank     int
	ID       int64
	Title    string
	URL      string
	Host     string
	Score    int
	By       string
	Age      string
	Comments int
	HNURL    string
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
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

	items := s.hn.ItemsParallel(ctx, pageIDs)

	vm := listVM{
		Page:     page,
		PrevPage: page - 1,
		NextPage: page + 1,
		HasPrev:  page > 1,
		HasNext:  end < len(ids),
	}
	for i, item := range items {
		if item == nil || item.Dead || item.Deleted {
			continue
		}
		host := ""
		displayURL := item.URL
		if displayURL == "" {
			displayURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID)
			host = "news.ycombinator.com"
		} else if u, err := url.Parse(displayURL); err == nil {
			host = strings.TrimPrefix(u.Host, "www.")
		}
		vm.Stories = append(vm.Stories, storyVM{
			Rank:     start + i + 1,
			ID:       item.ID,
			Title:    item.Title,
			URL:      displayURL,
			Host:     host,
			Score:    item.Score,
			By:       item.By,
			Age:      relTime(item.Time),
			Comments: item.Descendants,
			HNURL:    fmt.Sprintf("https://news.ycombinator.com/item?id=%d", item.ID),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, "index.html.tmpl", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
