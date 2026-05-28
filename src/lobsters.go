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
	lobstersBase = "https://lobste.rs"

	lobstersTabHottest = "hottest"
	lobstersTabActive  = "active"
	lobstersTabNewest  = "newest"
)

// lobstersTabPaths maps a Lobsters tab slug to its JSON endpoint root.
// The "" key is the default (Hottest, served at /hottest.json).
var lobstersTabPaths = map[string]string{
	lobstersTabHottest: "/hottest.json",
	lobstersTabActive:  "/active.json",
	lobstersTabNewest:  "/newest.json",
}

var lobstersTabs = []TabDef{
	{lobstersTabHottest, "Hottest"},
	{lobstersTabActive, "Active"},
	{lobstersTabNewest, "Newest"},
}

// lobstersStory is the raw shape /hottest.json (and peers) return per entry.
// submitter_user is captured as RawMessage because Lobsters returns it as
// EITHER a plain string (v1-ish API) OR an object {username:...} (newer
// endpoints). submitter() parses both shapes lazily.
type lobstersStory struct {
	ShortID       string          `json:"short_id"`
	CreatedAt     string          `json:"created_at"`
	Title         string          `json:"title"`
	URL           string          `json:"url"`
	Score         int             `json:"score"`
	Flags         int             `json:"flags"`
	CommentCount  int             `json:"comment_count"`
	Description   string          `json:"description"`
	SubmitterRaw  json.RawMessage `json:"submitter_user"`
	Tags          []string        `json:"tags"`
	CommentsCount int             `json:"-"` // alias for CommentCount kept for clarity in derived code
}

// lobstersStoryDetail extends lobstersStory with the comments list returned
// only by /s/{id}.json.
type lobstersStoryDetail struct {
	lobstersStory
	Comments []lobstersComment `json:"comments"`
}

type lobstersComment struct {
	ShortID        string `json:"short_id"`
	CreatedAt      string `json:"created_at"`
	IsDeleted      bool   `json:"is_deleted"`
	IsModerated    bool   `json:"is_moderated"`
	Score          int    `json:"score"`
	ParentComment  string `json:"parent_comment"` // empty / null for top-level
	Comment        string `json:"comment"`        // sanitized HTML
	Depth          int    `json:"depth"`
	CommentingUser string `json:"commenting_user"`
}

func (s *lobstersStory) submitter() string {
	if len(s.SubmitterRaw) == 0 {
		return ""
	}
	// Try the plain-string shape first (v1 API).
	var asStr string
	if err := json.Unmarshal(s.SubmitterRaw, &asStr); err == nil {
		return asStr
	}
	// Fall back to the object shape ({username: "..."}).
	var asObj struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(s.SubmitterRaw, &asObj); err == nil {
		return asObj.Username
	}
	return ""
}

func (s *lobstersStory) timeUnix() int64 {
	// Lobsters' created_at is RFC3339 with a numeric offset, e.g.:
	// "2026-05-27T10:36:02.000-05:00"
	t, err := time.Parse("2006-01-02T15:04:05.000-07:00", s.CreatedAt)
	if err != nil {
		// Fall back to a looser RFC3339 parse.
		t, err = time.Parse(time.RFC3339, s.CreatedAt)
		if err != nil {
			return 0
		}
	}
	return t.Unix()
}

func (s *lobstersStory) toItem() *Item {
	return &Item{
		ID:          s.ShortID,
		By:          s.submitter(),
		Time:        s.timeUnix(),
		Text:        s.Description,
		URL:         s.URL,
		Score:       s.Score,
		Title:       s.Title,
		Type:        "story",
		Descendants: s.CommentCount,
	}
}

// Lobsters implements Source for Lobsters (lobste.rs).
type Lobsters struct {
	http *http.Client
	sf   singleflight.Group

	mu          sync.RWMutex
	listIDs     map[string][]string
	listFetched map[string]time.Time
	listCache   map[string]map[string]*Item // per-tab id → item (filled during list fetch so Item() is a cache hit)

	items   *expirable.LRU[string, *Item]
	threads *expirable.LRU[string, *StoryThread]

	threadRate *rateLimiter
}

func NewLobsters() *Lobsters {
	return &Lobsters{
		http: &http.Client{
			Timeout:   httpTimeout,
			Transport: newPoliteTransport(15),
		},
		listIDs:     make(map[string][]string),
		listFetched: make(map[string]time.Time),
		listCache:   make(map[string]map[string]*Item),
		items:       expirable.NewLRU[string, *Item](itemCacheCap, nil, itemTTL),
		threads:     expirable.NewLRU[string, *StoryThread](threadCacheCap, nil, threadTTL),
		threadRate:  newRateLimiter(30, 60*time.Second),
	}
}

// --- Source interface ---

func (l *Lobsters) Name() string             { return "lobsters" }
func (l *Lobsters) Label() string            { return "Lobsters" }
func (l *Lobsters) Tabs() []TabDef           { return lobstersTabs }
func (l *Lobsters) ValidTab(slug string) bool { _, ok := lobstersTabPaths[slug]; return ok }
func (l *Lobsters) DefaultTab() string       { return lobstersTabHottest }
func (l *Lobsters) StoryDiscussionURL(id string) string {
	return fmt.Sprintf("https://lobste.rs/s/%s", id)
}

// --- list fetching ---

// lobstersPageSize is what Lobsters returns per /{tab}.json or /page/N.json
// call (empirically 25). Used as the hasNext heuristic: if a page returned
// a full 25, there's almost certainly more.
const lobstersPageSize = 25

// StoryIDs fetches the requested page from Lobsters. Page 1 uses the tab's
// base endpoint (/hottest.json etc); page > 1 uses /{tab-prefix}/page/N.json
// (with hottest's prefix being the empty string -- /page/N.json).
func (l *Lobsters) StoryIDs(ctx context.Context, tab string, page int) ([]string, bool, error) {
	if !l.ValidTab(tab) {
		return nil, false, fmt.Errorf("unknown Lobsters tab %q", tab)
	}
	if page < 1 {
		page = 1
	}
	cacheKey := fmt.Sprintf("%s:%d", tab, page)

	l.mu.RLock()
	fetched := l.listFetched[cacheKey]
	ids := l.listIDs[cacheKey]
	l.mu.RUnlock()
	if !fetched.IsZero() && time.Since(fetched) < topStoriesTTL {
		return ids, len(ids) >= lobstersPageSize, nil
	}
	got, err := l.refreshPage(ctx, tab, page)
	if err != nil {
		return nil, false, err
	}
	return got, len(got) >= lobstersPageSize, nil
}

func (l *Lobsters) refreshPage(ctx context.Context, tab string, page int) ([]string, error) {
	cacheKey := fmt.Sprintf("%s:%d", tab, page)
	v, err, _ := l.sf.Do("lobsters-page:"+cacheKey, func() (interface{}, error) {
		return l.fetchPage(ctx, tab, page)
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

// lobstersPageURL returns the JSON URL for a (tab, page) pair. Lobsters'
// hottest tab is the bare root (/hottest.json or /page/N.json); other tabs
// prepend the tab name (/newest.json, /newest/page/N.json, etc.).
func lobstersPageURL(tab string, page int) string {
	prefix := ""
	if tab != lobstersTabHottest {
		prefix = "/" + tab
	}
	if page <= 1 {
		// /hottest.json, /newest.json, /active.json
		base := tab
		if prefix == "" {
			base = lobstersTabHottest
		}
		return lobstersBase + "/" + base + ".json"
	}
	// /page/N.json (hottest) or /newest/page/N.json
	return fmt.Sprintf("%s%s/page/%d.json", lobstersBase, prefix, page)
}

func (l *Lobsters) fetchPage(ctx context.Context, tab string, page int) ([]string, error) {
	url := lobstersPageURL(tab, page)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lobsters %s page %d: %s", tab, page, resp.Status)
	}
	var raw []lobstersStory
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	ids := make([]string, len(raw))
	byID := make(map[string]*Item, len(raw))
	for i, s := range raw {
		ids[i] = s.ShortID
		byID[s.ShortID] = s.toItem()
	}
	cacheKey := fmt.Sprintf("%s:%d", tab, page)
	l.mu.Lock()
	l.listIDs[cacheKey] = ids
	l.listFetched[cacheKey] = time.Now()
	l.listCache[cacheKey] = byID
	l.mu.Unlock()
	// Warm the per-item LRU too so cross-tab navigation hits cache.
	for id, item := range byID {
		l.items.Add(id, item)
	}
	return ids, nil
}

// --- single item fetching ---

func (l *Lobsters) Item(ctx context.Context, id string) (*Item, error) {
	if item, ok := l.items.Get(id); ok {
		return item, nil
	}
	v, err, _ := l.sf.Do("lobsters-item:"+id, func() (interface{}, error) {
		if item, ok := l.items.Get(id); ok {
			return item, nil
		}
		return l.fetchItem(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	item := v.(*Item)
	l.items.Add(id, item)
	return item, nil
}

func (l *Lobsters) fetchItem(ctx context.Context, id string) (*Item, error) {
	// /s/{short_id}.json returns story + comments. We only need the story
	// fields here; the comment tree is fetched separately via StoryThread
	// (and cached separately) so cross-tab navigation that lands here
	// doesn't unnecessarily download the full thread.
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/s/%s.json", lobstersBase, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("lobsters item %s: not found", id)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lobsters item %s: %s", id, resp.Status)
	}
	var raw lobstersStoryDetail
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.ShortID == "" {
		return nil, fmt.Errorf("lobsters item %s: empty body", id)
	}
	return raw.toItem(), nil
}

func (l *Lobsters) ItemsParallel(ctx context.Context, ids []string) []*Item {
	out := make([]*Item, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		i, id := i, id
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := l.Item(ctx, id)
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

func (l *Lobsters) StoryThread(ctx context.Context, id, requesterIP string) (*StoryThread, error) {
	if thread, ok := l.threads.Get(id); ok {
		return thread, nil
	}
	v, err, _ := l.sf.Do("lobsters-thread:"+id, func() (interface{}, error) {
		if thread, ok := l.threads.Get(id); ok {
			return thread, nil
		}
		if !l.threadRate.Allow(requesterIP) {
			return nil, errRateLimited
		}
		return l.fetchThread(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	thread := v.(*StoryThread)
	l.threads.Add(id, thread)
	return thread, nil
}

func (l *Lobsters) fetchThread(ctx context.Context, id string) (*StoryThread, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/s/%s.json", lobstersBase, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lobsters thread %s: %s", id, resp.Status)
	}
	var raw lobstersStoryDetail
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	// Also warm the per-item cache so a subsequent Item() lookup hits.
	l.items.Add(id, raw.toItem())

	return assembleLobstersThread(id, raw.Comments), nil
}

// assembleLobstersThread builds the comment tree from Lobsters' flat
// comment list (parent_comment refs). Lobsters delivers comments in
// pre-order traversal: a parent always precedes its children in the
// array, so a single pass suffices.
func assembleLobstersThread(storyID string, raw []lobstersComment) *StoryThread {
	st := &StoryThread{StoryID: storyID}
	byID := make(map[string]*Comment, len(raw))
	for _, rc := range raw {
		author := rc.CommentingUser
		if rc.IsDeleted || rc.IsModerated || author == "" {
			author = "[deleted]"
		}
		c := &Comment{
			ID:        rc.ShortID,
			Author:    author,
			Text:      rc.Comment,
			CreatedAt: lobstersTimeUnix(rc.CreatedAt),
		}
		byID[rc.ShortID] = c
		if rc.ParentComment == "" {
			st.Comments = append(st.Comments, c)
			continue
		}
		parent, ok := byID[rc.ParentComment]
		if !ok {
			// Out-of-order or stale ref — fall back to top-level rather
			// than dropping the comment entirely.
			st.Comments = append(st.Comments, c)
			continue
		}
		parent.Children = append(parent.Children, c)
	}
	return st
}

func lobstersTimeUnix(s string) int64 {
	t, err := time.Parse("2006-01-02T15:04:05.000-07:00", s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return 0
		}
	}
	return t.Unix()
}

// StartBackgroundRefresh keeps the Hottest tab warm.
func (l *Lobsters) StartBackgroundRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(topStoriesTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bgCtx, cancel := context.WithTimeout(ctx, httpTimeout)
				_, _ = l.refreshPage(bgCtx, lobstersTabHottest, 1)
				cancel()
			}
		}
	}()
}
