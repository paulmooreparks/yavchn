package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	apiBase       = "https://hacker-news.firebaseio.com/v0"
	topStoriesTTL = 60 * time.Second
	itemTTL       = 5 * time.Minute
	httpTimeout   = 10 * time.Second
)

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

type HN struct {
	http *http.Client
	sf   singleflight.Group

	mu         sync.RWMutex
	topIDs     []int64
	topFetched time.Time
	items      map[int64]itemEntry
}

type itemEntry struct {
	item    *Item
	fetched time.Time
}

func NewHN() *HN {
	return &HN{
		http:  &http.Client{Timeout: httpTimeout},
		items: make(map[int64]itemEntry),
	}
}

func (h *HN) TopIDs(ctx context.Context) ([]int64, error) {
	h.mu.RLock()
	fresh := !h.topFetched.IsZero() && time.Since(h.topFetched) < topStoriesTTL
	ids := h.topIDs
	h.mu.RUnlock()
	if fresh {
		return ids, nil
	}
	return h.refreshTopIDs(ctx)
}

func (h *HN) refreshTopIDs(ctx context.Context) ([]int64, error) {
	v, err, _ := h.sf.Do("topstories", func() (interface{}, error) {
		return h.fetchTopIDs(ctx)
	})
	if err != nil {
		return nil, err
	}
	ids := v.([]int64)
	h.mu.Lock()
	h.topIDs = ids
	h.topFetched = time.Now()
	h.mu.Unlock()
	return ids, nil
}

func (h *HN) fetchTopIDs(ctx context.Context) ([]int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/topstories.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("topstories: %s", resp.Status)
	}
	var ids []int64
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (h *HN) Item(ctx context.Context, id int64) (*Item, error) {
	h.mu.RLock()
	entry, ok := h.items[id]
	h.mu.RUnlock()
	if ok && time.Since(entry.fetched) < itemTTL {
		return entry.item, nil
	}

	v, err, _ := h.sf.Do(fmt.Sprintf("item:%d", id), func() (interface{}, error) {
		return h.fetchItem(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	item := v.(*Item)
	h.mu.Lock()
	h.items[id] = itemEntry{item: item, fetched: time.Now()}
	h.mu.Unlock()
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
	return &item, nil
}

// ItemsParallel fetches a batch concurrently, preserving order. Failed items are nil.
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

// StartBackgroundRefresh refreshes the top-list on a fixed cadence until ctx is done.
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
				_, _ = h.refreshTopIDs(bgCtx)
				cancel()
			}
		}
	}()
}
