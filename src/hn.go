package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	expirable "github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/sync/singleflight"
)

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

	SourceTop  = "top"
	SourceShow = "show"
	SourceAsk  = "ask"
	SourceNew  = "new"
)

// sourceEndpoints maps a source name to the firebaseio.com path. Sources
// not in the map are unknown and rejected by validSource.
var sourceEndpoints = map[string]string{
	SourceTop:  "/topstories.json",
	SourceShow: "/showstories.json",
	SourceAsk:  "/askstories.json",
	SourceNew:  "/newstories.json",
}

func validSource(s string) bool {
	_, ok := sourceEndpoints[s]
	return ok
}

type Item struct {
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

type Comment struct {
	ID        int64
	Author    string
	Text      string
	CreatedAt int64
	Children  []*Comment
}

type StoryThread struct {
	StoryID  int64
	Comments []*Comment
}

type algoliaItem struct {
	ID         int64         `json:"id"`
	Author     string        `json:"author"`
	Text       string        `json:"text"`
	CreatedAtI int64         `json:"created_at_i"`
	Children   []algoliaItem `json:"children"`
}

type HN struct {
	http *http.Client
	sf   singleflight.Group

	mu          sync.RWMutex
	listIDs     map[string][]int64
	listFetched map[string]time.Time

	items   *expirable.LRU[int64, *Item]
	threads *expirable.LRU[int64, *StoryThread]

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
		listIDs:     make(map[string][]int64),
		listFetched: make(map[string]time.Time),
		items:       expirable.NewLRU[int64, *Item](itemCacheCap, nil, itemTTL),
		threads:     expirable.NewLRU[int64, *StoryThread](threadCacheCap, nil, threadTTL),
		threadRate:  newRateLimiter(30, 60*time.Second),
	}
}

// newPoliteTransport bounds per-host connections so we don't flood
// firebaseio / algolia under a burst, and tags every request with our
// User-Agent so HN ops folks can identify us if needed.
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

// StoryIDs returns the cached id list for source (top/show/ask/new), fetching
// upstream if absent or stale. Sources are cached independently.
func (h *HN) StoryIDs(ctx context.Context, source string) ([]int64, error) {
	if !validSource(source) {
		return nil, fmt.Errorf("unknown source %q", source)
	}
	h.mu.RLock()
	fetched := h.listFetched[source]
	ids := h.listIDs[source]
	h.mu.RUnlock()
	if !fetched.IsZero() && time.Since(fetched) < topStoriesTTL {
		return ids, nil
	}
	return h.refreshStoryIDs(ctx, source)
}

func (h *HN) refreshStoryIDs(ctx context.Context, source string) ([]int64, error) {
	v, err, _ := h.sf.Do("storyids:"+source, func() (interface{}, error) {
		return h.fetchStoryIDs(ctx, source)
	})
	if err != nil {
		return nil, err
	}
	ids := v.([]int64)
	h.mu.Lock()
	h.listIDs[source] = ids
	h.listFetched[source] = time.Now()
	h.mu.Unlock()
	return ids, nil
}

func (h *HN) fetchStoryIDs(ctx context.Context, source string) ([]int64, error) {
	endpoint, ok := sourceEndpoints[source]
	if !ok {
		return nil, fmt.Errorf("unknown source %q", source)
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
		return nil, fmt.Errorf("%s: %s", source, resp.Status)
	}
	var ids []int64
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (h *HN) Item(ctx context.Context, id int64) (*Item, error) {
	if item, ok := h.items.Get(id); ok {
		return item, nil
	}

	v, err, _ := h.sf.Do(fmt.Sprintf("item:%d", id), func() (interface{}, error) {
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

func (h *HN) fetchItem(ctx context.Context, id int64) (*Item, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/item/%d.json", apiBase, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("item %d: %s", id, resp.Status)
	}
	var item Item
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	// firebaseio returns 200 + body "null" for unknown IDs; that decodes
	// into a zero-value Item. Treat as not-found.
	if item.ID == 0 {
		return nil, fmt.Errorf("item %d: not found", id)
	}
	return &item, nil
}

func (h *HN) ItemsParallel(ctx context.Context, ids []int64) []*Item {
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

// StoryThread returns the full comment tree for a story, fetched from the HN
// Algolia API (one HTTP call returns the entire tree). Cached for threadTTL.
// requesterIP is rate-limited on cache miss only; cache hits and singleflight
// piggy-backers don't consume the requester's budget.
func (h *HN) StoryThread(ctx context.Context, id int64, requesterIP string) (*StoryThread, error) {
	if thread, ok := h.threads.Get(id); ok {
		return thread, nil
	}

	v, err, _ := h.sf.Do(fmt.Sprintf("thread:%d", id), func() (interface{}, error) {
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

func (h *HN) fetchThread(ctx context.Context, id int64) (*StoryThread, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/items/%d", algoliaBase, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("algolia %d: %s", id, resp.Status)
	}
	var raw algoliaItem
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	st := &StoryThread{StoryID: id}
	for _, child := range raw.Children {
		st.Comments = append(st.Comments, convertComment(child))
	}
	return st, nil
}

func convertComment(a algoliaItem) *Comment {
	author := a.Author
	if author == "" {
		author = "[deleted]"
	}
	c := &Comment{
		ID:        a.ID,
		Author:    author,
		Text:      a.Text,
		CreatedAt: a.CreatedAtI,
	}
	for _, child := range a.Children {
		c.Children = append(c.Children, convertComment(child))
	}
	return c
}

// StartBackgroundRefresh keeps the "top" source warm. Other sources fill
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
				_, _ = h.refreshStoryIDs(bgCtx, SourceTop)
				cancel()
			}
		}
	}()
}
