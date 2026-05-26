package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
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
	rateLimitPerWindow       = 20
	rateLimitWindow          = 60 * time.Second
	maxResponseBytes         = 5 * 1024 * 1024 // 5 MiB upper bound on the source-page body we'll parse
)

// errRateLimited is returned by Extractor.Get when the caller's IP has
// exceeded the per-window outbound-fetch budget. Cache hits never reach
// this path -- the rate check fires only after cache miss + singleflight.
var errRateLimited = errors.New("rate limited")

type Extractor struct {
	db     *sql.DB
	sf     singleflight.Group
	client *http.Client
	sem    chan struct{}
	rate   *rateLimiter
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
			Transport: newExtractorTransport(),
		},
		sem:  make(chan struct{}, maxConcurrentExtractions),
		rate: newRateLimiter(rateLimitPerWindow, rateLimitWindow),
	}
}

// newExtractorTransport wires the politeness wrapper around an http.Transport
// whose DialContext refuses to connect to internal addresses. Used only for
// the article-extraction client (HN / Algolia calls go via newPoliteTransport
// which trusts the fixed upstream hosts).
func newExtractorTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxConnsPerHost = 8
	t.MaxIdleConnsPerHost = 3
	t.IdleConnTimeout = 90 * time.Second
	t.TLSHandshakeTimeout = 5 * time.Second
	t.DialContext = newSafeDialContext()
	return &uaTransport{rt: t, ua: userAgent}
}

func (e *Extractor) Get(ctx context.Context, rawURL, requesterIP string) (*Article, error) {
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
		// Rate-check inside singleflight so cache hits and piggy-backers
		// on an in-flight fetch never consume the requester's budget.
		if !e.rate.Allow(requesterIP) {
			return nil, errRateLimited
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

	parsed, err := readability.FromReader(io.LimitReader(resp.Body, maxResponseBytes), parsedURL)
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
