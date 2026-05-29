package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Submission is one place a URL was discussed: a single HN story (and, in
// later cards, a single Reddit post). The finder groups submissions by
// source for the two-level selector.
type Submission struct {
	Source      string // "hn"
	ID          string // story id, used with /api/discussion?id=&source=
	Title       string
	Score       int
	NumComments int
	CreatedAt   int64
	Where       string // subreddit etc.; "" for HN
}

// DiscussionProvider finds every submission of a URL on one source. The
// finder fans out across all registered providers. HN implements it in v1;
// Reddit and others slot in via follow-on cards without touching the finder.
type DiscussionProvider interface {
	ProviderName() string
	FindByURL(ctx context.Context, rawURL string) ([]Submission, error)
}

// normalizeURL applies light canonicalization so we don't miss submissions
// over trivial URL differences: lowercase host, drop a leading "www.",
// strip the fragment and common tracking params, drop a trailing slash on
// the path. Returns the normalized URL; on parse failure returns the input
// unchanged so the caller still queries with something.
func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimPrefix(u.Host, "www.")
	u.Fragment = ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "ref" {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	if len(u.Path) > 1 {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

// --- HN DiscussionProvider ---

func (h *HN) ProviderName() string { return "hn" }

// FindByURL queries the HN Algolia index for stories whose url field matches
// rawURL. Algolia's restrictSearchableAttributes=url scopes the match to the
// submission URL. We query with the normalized form and, when it differs,
// also the as-given form, then union + dedupe by story ID.
func (h *HN) FindByURL(ctx context.Context, rawURL string) ([]Submission, error) {
	norm := normalizeURL(rawURL)
	queries := []string{norm}
	if raw := strings.TrimSpace(rawURL); raw != "" && raw != norm {
		queries = append(queries, raw)
	}

	seen := make(map[string]bool)
	var out []Submission
	var firstErr error
	for _, q := range queries {
		hits, err := h.algoliaByURL(ctx, q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, hit := range hits {
			if seen[hit.ID] {
				continue
			}
			seen[hit.ID] = true
			out = append(out, hit)
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	// Most-recent submission first; the picker shows them newest-to-oldest.
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// --- Lobsters DiscussionProvider ---

func (l *Lobsters) ProviderName() string { return "lobsters" }

// FindByURL lists the URL's host on Lobsters via /domains/{host}.json and
// keeps the stories whose URL matches. Lobsters has no JSON search endpoint,
// but the per-domain listing is JSON, so we filter that instead. v1 reads
// only the first page of the domain listing; domains with many submissions
// past page 1 could miss older ones (acceptable for now).
func (l *Lobsters) FindByURL(ctx context.Context, rawURL string) ([]Submission, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return nil, nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	endpoint := fmt.Sprintf("%s/domains/%s.json", lobstersBase, host)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil // domain never submitted to Lobsters
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lobsters domains: %s", resp.Status)
	}
	var raw []lobstersStory
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	target := normalizeURL(rawURL)
	var out []Submission
	for _, s := range raw {
		if normalizeURL(s.URL) != target {
			continue
		}
		out = append(out, Submission{
			Source:      "lobsters",
			ID:          s.ShortID,
			Title:       s.Title,
			Score:       s.Score,
			NumComments: s.CommentCount,
			CreatedAt:   s.timeUnix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (h *HN) algoliaByURL(ctx context.Context, query string) ([]Submission, error) {
	u := fmt.Sprintf("%s/search?query=%s&restrictSearchableAttributes=url&tags=story&hitsPerPage=50",
		algoliaBase, urlQueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("algolia url-search: %s", resp.Status)
	}
	var raw algoliaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Submission, 0, len(raw.Hits))
	for _, hit := range raw.Hits {
		// Algolia's url-restricted search still fuzzy-matches; keep only hits
		// whose stored URL actually equals our target (normalized both sides)
		// so we don't surface unrelated stories.
		if normalizeURL(hit.URL) != normalizeURL(query) {
			continue
		}
		out = append(out, Submission{
			Source:      "hn",
			ID:          hit.ObjectID,
			Title:       hit.Title,
			Score:       hit.Points,
			NumComments: hit.NumComments,
			CreatedAt:   hit.CreatedAtI,
		})
	}
	return out, nil
}
