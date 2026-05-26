package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
	"golang.org/x/sync/singleflight"
)

const (
	extractTimeout           = 8 * time.Second
	maxConcurrentExtractions = 20
)

type Extractor struct {
	db     *sql.DB
	sf     singleflight.Group
	client *http.Client
	sem    chan struct{}
}

type Article struct {
	URL     string
	Title   string
	Byline  string
	Content template.HTML
}

func NewExtractor(db *sql.DB) *Extractor {
	return &Extractor{
		db: db,
		client: &http.Client{
			Timeout:   extractTimeout,
			Transport: newPoliteTransport(8),
		},
		sem: make(chan struct{}, maxConcurrentExtractions),
	}
}

func (e *Extractor) Get(ctx context.Context, rawURL string) (*Article, error) {
	if !isAllowedURL(rawURL) {
		return nil, errors.New("url scheme not allowed")
	}
	hash := urlHash(rawURL)

	if a, err := e.fromCache(ctx, hash); err == nil {
		return a, nil
	}

	v, err, _ := e.sf.Do(hash, func() (interface{}, error) {
		if a, err := e.fromCache(ctx, hash); err == nil {
			return a, nil
		}
		return e.fetchAndStore(ctx, hash, rawURL)
	})
	if err != nil {
		return nil, err
	}
	return v.(*Article), nil
}

func (e *Extractor) fromCache(ctx context.Context, hash string) (*Article, error) {
	row := e.db.QueryRowContext(ctx,
		`SELECT url, title, byline, content FROM articles WHERE url_hash = ?`, hash)
	var a Article
	var content string
	if err := row.Scan(&a.URL, &a.Title, &a.Byline, &content); err != nil {
		return nil, err
	}
	a.Content = template.HTML(content)
	return &a, nil
}

func (e *Extractor) fetchAndStore(ctx context.Context, hash, rawURL string) (*Article, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	// Bound concurrent outbound article fetches across all hosts.
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	subCtx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(subCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; yavchn-reader/0.1; +https://github.com/paulmooreparks/yavchn)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fetch %s: %s", rawURL, resp.Status)
	}
	ctype := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ctype, "text/html") && !strings.HasPrefix(ctype, "application/xhtml") {
		return nil, fmt.Errorf("not html: %s", ctype)
	}

	parsed, err := readability.FromReader(resp.Body, parsedURL)
	if err != nil {
		return nil, err
	}
	sanitized := sanitizeHTML(parsed.Content)
	if strings.TrimSpace(sanitized) == "" {
		return nil, errors.New("no extractable content")
	}
	a := &Article{
		URL:     rawURL,
		Title:   firstNonEmpty(parsed.Title, parsedURL.Host),
		Byline:  parsed.Byline,
		Content: template.HTML(sanitized),
	}
	if _, err := e.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO articles
		   (url_hash, url, fetched_at, title, byline, content)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hash, rawURL, time.Now().Unix(), a.Title, a.Byline, sanitized); err != nil {
		return nil, fmt.Errorf("cache store: %w", err)
	}
	return a, nil
}

func urlHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func isAllowedURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
