package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	expirable "github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"
)

// urlQueryEscape is a tiny wrapper so this package doesn't import "net/url"
// in two places. Keeps the search URL builder readable.
func urlQueryEscape(s string) string { return url.QueryEscape(s) }

const (
	apiBase       = "https://hacker-news.firebaseio.com/v0"
	algoliaBase   = "https://hn.algolia.com/api/v1"
	topStoriesTTL = 60 * time.Second
	itemTTL       = 5 * time.Minute
	threadTTL     = 3 * time.Minute
	httpTimeout   = 10 * time.Second

	itemCacheCap   = 2048
	threadCacheCap = 256

	userAgent = "yavchn/0.1 (+https://github.com/paulmooreparks/yavchn)"

	// HN-specific tab slugs. Internal — exposed externally via HN.Tabs().
	hnTabTop  = "top"
	hnTabShow = "show"
	hnTabAsk  = "ask"
	hnTabNew  = "new"
	hnTabBest = "best"
	hnTabJobs = "jobs"
)

// hnTabEndpoints maps an HN tab slug to its firebaseio.com path.
var hnTabEndpoints = map[string]string{
	hnTabTop:  "/topstories.json",
	hnTabShow: "/showstories.json",
	hnTabAsk:  "/askstories.json",
	hnTabNew:  "/newstories.json",
	hnTabBest: "/beststories.json",
	hnTabJobs: "/jobstories.json",
}

// hnTabs is the tab set rendered in the source-tabs row when HN is active.
var hnTabs = []TabDef{
	{hnTabTop, "Top"},
	{hnTabShow, "Show HN"},
	{hnTabAsk, "Ask HN"},
	{hnTabNew, "New"},
	{hnTabBest, "Best"},
	{hnTabJobs, "Jobs"},
}

// hnFirebaseItem is the raw shape firebaseio.com returns. Decoded internally
// then normalized into the shared *Item before leaving the source.
type hnFirebaseItem struct {
	ID          int64   `json:"id"`
	By          string  `json:"by"`
	Time        int64   `json:"time"`
	Text        string  `json:"text"`
	Dead        bool    `json:"dead"`
	Deleted     bool    `json:"deleted"`
	Parent      int64   `json:"parent"`
	Kids        []int64 `json:"kids"`
	URL         string  `json:"url"`
	Score       int     `json:"score"`
	Title       string  `json:"title"`
	Type        string  `json:"type"`
	Descendants int     `json:"descendants"`
}

// fromFirebase converts the raw HN shape into the shared *Item.
func (r *hnFirebaseItem) toItem() *Item {
	kids := make([]string, 0, len(r.Kids))
	for _, k := range r.Kids {
		kids = append(kids, strconv.FormatInt(k, 10))
	}
	return &Item{
		ID:          strconv.FormatInt(r.ID, 10),
		By:          r.By,
		Time:        r.Time,
		Text:        r.Text,
		Dead:        r.Dead,
		Deleted:     r.Deleted,
		Kids:        kids,
		URL:         r.URL,
		Score:       r.Score,
		Title:       r.Title,
		Type:        r.Type,
		Descendants: r.Descendants,
	}
}

type algoliaItem struct {
	ID         int64         `json:"id"`
	Author     string        `json:"author"`
	Text       string        `json:"text"`
	CreatedAtI int64         `json:"created_at_i"`
	Children   []algoliaItem `json:"children"`
}

// SearchHit represents one story matched by the HN Algolia search API.
type SearchHit struct {
	ID          string
	Title       string
	URL         string
	Author      string
	Points      int
	NumComments int
	CreatedAt   int64
}

type algoliaSearchResponse struct {
	Hits []struct {
		ObjectID    string `json:"objectID"`
		Title       string `json:"title"`
		StoryText   string `json:"story_text"`
		URL         string `json:"url"`
		Author      string `json:"author"`
		Points      int    `json:"points"`
		NumComments int    `json:"num_comments"`
		CreatedAtI  int64  `json:"created_at_i"`
	} `json:"hits"`
	NbPages int `json:"nbPages"`
}

// HN implements Source for Hacker News (firebaseio.com + Algolia).
type HN struct {
	http *http.Client
	sf   singleflight.Group

	mu          sync.RWMutex
	listIDs     map[string][]string
	listFetched map[string]time.Time

	items   *expirable.LRU[string, *Item]
	threads *expirable.LRU[string, *StoryThread]

	// threadRate caps outbound Algolia thread fetches per requester IP.
	// Cache hits never reach this gate (checked inside the singleflight
	// callback after a second cache peek).
	threadRate *rateLimiter
}

func NewHN() *HN {
	return &HN{
		http: &http.Client{
			Timeout:   httpTimeout,
			Transport: newPoliteTransport(30),
		},
		listIDs:     make(map[string][]string),
		listFetched: make(map[string]time.Time),
		items:       expirable.NewLRU[string, *Item](itemCacheCap, nil, itemTTL),
		threads:     expirable.NewLRU[string, *StoryThread](threadCacheCap, nil, threadTTL),
		threadRate:  newRateLimiter(30, 60*time.Second),
	}
}

// --- Source interface ---

func (h *HN) Name() string  { return "hn" }
func (h *HN) Label() string { return "Hacker News" }
func (h *HN) Tabs() []TabDef {
	return hnTabs
}
func (h *HN) ValidTab(slug string) bool { _, ok := hnTabEndpoints[slug]; return ok }
func (h *HN) DefaultTab() string        { return hnTabTop }

func (h *HN) StoryDiscussionURL(id string) string {
	return fmt.Sprintf("https://news.ycombinator.com/item?id=%s", id)
}

// --- newPoliteTransport (shared by Lobsters too) ---

func newPoliteTransport(maxPerHost int) http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxConnsPerHost = maxPerHost
	t.MaxIdleConnsPerHost = maxPerHost / 3
	t.IdleConnTimeout = 90 * time.Second
	return &uaTransport{rt: t, ua: userAgent}
}

type uaTransport struct {
	rt http.RoundTripper
	ua string
}

func (u *uaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", u.ua)
	}
	return u.rt.RoundTrip(r)
}

// --- list fetching ---

// StoryIDs returns the page-th slice of the cached id list for an HN tab.
// HN's firebaseio endpoints return the full ranked list in one call, so we
// keep the full list cached and just slice for pagination here.
func (h *HN) StoryIDs(ctx context.Context, tab string, page int) ([]string, bool, error) {
	if !h.ValidTab(tab) {
		return nil, false, fmt.Errorf("unknown HN tab %q", tab)
	}
	if page < 1 {
		page = 1
	}
	h.mu.RLock()
	fetched := h.listFetched[tab]
	ids := h.listIDs[tab]
	h.mu.RUnlock()
	if fetched.IsZero() || time.Since(fetched) >= topStoriesTTL {
		var err error
		ids, err = h.refreshStoryIDs(ctx, tab)
		if err != nil {
			return nil, false, err
		}
	}
	start := (page - 1) * pageSize
	if start >= len(ids) {
		return nil, false, nil
	}
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	return ids[start:end], end < len(ids), nil
}

func (h *HN) refreshStoryIDs(ctx context.Context, tab string) ([]string, error) {
	v, err, _ := h.sf.Do("storyids:"+tab, func() (interface{}, error) {
		return h.fetchStoryIDs(ctx, tab)
	})
	if err != nil {
		return nil, err
	}
	ids := v.([]string)
	h.mu.Lock()
	h.listIDs[tab] = ids
	h.listFetched[tab] = time.Now()
	h.mu.Unlock()
	return ids, nil
}

func (h *HN) fetchStoryIDs(ctx context.Context, tab string) ([]string, error) {
	endpoint, ok := hnTabEndpoints[tab]
	if !ok {
		return nil, fmt.Errorf("unknown HN tab %q", tab)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", tab, resp.Status)
	}
	var raw []int64
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	ids := make([]string, len(raw))
	for i, id := range raw {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return ids, nil
}

// --- single item fetching ---

func (h *HN) Item(ctx context.Context, id string) (*Item, error) {
	if item, ok := h.items.Get(id); ok {
		return item, nil
	}

	v, err, _ := h.sf.Do("item:"+id, func() (interface{}, error) {
		if item, ok := h.items.Get(id); ok {
			return item, nil
		}
		return h.fetchItem(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	item := v.(*Item)
	h.items.Add(id, item)
	return item, nil
}

func (h *HN) fetchItem(ctx context.Context, id string) (*Item, error) {
	intID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("hn item id %q: %w", id, err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/item/%d.json", apiBase, intID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("item %s: %s", id, resp.Status)
	}
	var raw hnFirebaseItem
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	// firebaseio returns 200 + body "null" for unknown IDs; that decodes
	// into a zero-value Item. Treat as not-found.
	if raw.ID == 0 {
		return nil, fmt.Errorf("item %s: not found", id)
	}
	return raw.toItem(), nil
}

func (h *HN) ItemsParallel(ctx context.Context, ids []string) []*Item {
	out := make([]*Item, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		i, id := i, id
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := h.Item(ctx, id)
			if err != nil {
				return
			}
			out[i] = item
		}()
	}
	wg.Wait()
	return out
}

// --- thread / discussion fetching ---

// StoryThread returns the full comment tree for a story, fetched from the HN
// Algolia API (one HTTP call returns the entire tree). Cached for threadTTL.
// requesterIP is rate-limited on cache miss only; cache hits and singleflight
// piggy-backers don't consume the requester's budget.
func (h *HN) StoryThread(ctx context.Context, id, requesterIP string) (*StoryThread, error) {
	if thread, ok := h.threads.Get(id); ok {
		return thread, nil
	}

	v, err, _ := h.sf.Do("thread:"+id, func() (interface{}, error) {
		if thread, ok := h.threads.Get(id); ok {
			return thread, nil
		}
		if !h.threadRate.Allow(requesterIP) {
			return nil, errRateLimited
		}
		return h.fetchThread(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	thread := v.(*StoryThread)
	h.threads.Add(id, thread)
	return thread, nil
}

func (h *HN) fetchThread(ctx context.Context, id string) (*StoryThread, error) {
	intID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("hn thread id %q: %w", id, err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/items/%d", algoliaBase, intID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("algolia %s: %s", id, resp.Status)
	}
	var raw algoliaItem
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	st := &StoryThread{StoryID: id}
	for _, child := range raw.Children {
		st.Comments = append(st.Comments, convertHNComment(child))
	}
	return st, nil
}

// --- HN-only: search (not part of Source interface) ---

// Search runs an HN Algolia search and returns the page's hits plus a hasMore
// flag. Page is 1-based; Algolia is 0-based, translated here.
func (h *HN) Search(ctx context.Context, query string, page int) ([]*SearchHit, bool, error) {
	if page < 1 {
		page = 1
	}
	apiPage := page - 1
	u := fmt.Sprintf("%s/search?query=%s&tags=story&hitsPerPage=30&page=%d",
		algoliaBase, urlQueryEscape(query), apiPage)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("algolia search: %s", resp.Status)
	}
	var raw algoliaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, false, err
	}
	hits := make([]*SearchHit, 0, len(raw.Hits))
	for _, r := range raw.Hits {
		hits = append(hits, &SearchHit{
			ID:          r.ObjectID,
			Title:       r.Title,
			URL:         r.URL,
			Author:      r.Author,
			Points:      r.Points,
			NumComments: r.NumComments,
			CreatedAt:   r.CreatedAtI,
		})
	}
	return hits, apiPage+1 < raw.NbPages, nil
}

func convertHNComment(a algoliaItem) *Comment {
	author := a.Author
	if author == "" {
		author = "[deleted]"
	}
	c := &Comment{
		ID:        strconv.FormatInt(a.ID, 10),
		Author:    author,
		Text:      a.Text,
		CreatedAt: a.CreatedAtI,
	}
	for _, child := range a.Children {
		c.Children = append(c.Children, convertHNComment(child))
	}
	return c
}

// StartBackgroundRefresh keeps the default tab warm. Other tabs fill
// lazily on first hit -- ok for niche tabs that don't need sub-minute freshness.
func (h *HN) StartBackgroundRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(topStoriesTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bgCtx, cancel := context.WithTimeout(ctx, httpTimeout)
				_, _ = h.refreshStoryIDs(bgCtx, hnTabTop)
				cancel()
			}
		}
	}()
}
