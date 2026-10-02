package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeedRevalidation(t *testing.T) {
	for _, name := range []string{"hn", "lobsters"} {
		t.Run(name, func(t *testing.T) {
			version := 1
			rt := &countingRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
				if name == "hn" {
					if version == 1 {
						return jsonResp(200, `[1]`), nil
					}
					return jsonResp(200, `[2]`), nil
				}
				if version == 1 {
					return jsonResp(200, `[{"short_id":"a","title":"Old","score":1}]`), nil
				}
				return jsonResp(200, `[{"short_id":"b","title":"Updated","score":2}]`), nil
			}}
			var source Source
			var tab string
			if name == "hn" {
				h := NewHN()
				h.http = &http.Client{Transport: rt}
				h.items.Add("2", &Item{ID: "2", Title: "Cached"})
				source, tab = h, hnTabTop
			} else {
				l := NewLobsters()
				l.http = &http.Client{Transport: rt}
				source, tab = l, lobstersTabHottest
			}
			old, _, err := source.StoryIDs(context.Background(), tab, 1, false)
			if err != nil {
				t.Fatal(err)
			}
			version = 2
			cached, _, err := source.StoryIDs(context.Background(), tab, 1, false)
			if err != nil || cached[0] != old[0] || rt.Calls() != 1 {
				t.Fatalf("ordinary read did not use cache: %v, %v", cached, err)
			}
			fresh, _, err := source.StoryIDs(context.Background(), tab, 1, true)
			if err != nil || len(fresh) != 1 || fresh[0] == old[0] || rt.Calls() != 2 {
				t.Fatalf("revalidation did not fetch new feed: %v, %v", fresh, err)
			}
			if h, ok := source.(*HN); ok {
				if _, cached := h.items.Get("2"); cached {
					t.Error("HN summary remained stale")
				}
			}
			if l, ok := source.(*Lobsters); ok {
				item, err := l.Item(context.Background(), "b")
				if err != nil || item.Title != "Updated" || rt.Calls() != 2 {
					t.Errorf("Lobsters summary was not refreshed: %v, %v", item, err)
				}
			}
		})
	}
}

type refreshObservingSource struct {
	*fakeSource
	revalidated bool
}

func (f *refreshObservingSource) StoryIDs(ctx context.Context, tab string, page int, revalidate bool) ([]string, bool, error) {
	f.revalidated = revalidate
	return f.fakeSource.StoryIDs(ctx, tab, page, revalidate)
}
func TestFeedRefreshRequest(t *testing.T) {
	s, _ := testServer(t)
	source := &refreshObservingSource{fakeSource: s.sources["hn"].(*fakeSource)}
	req := httptest.NewRequest("GET", "/hn/", nil)
	req.Header.Set("Cache-Control", "max-age=0, no-cache")
	rec := httptest.NewRecorder()
	s.SourceIndex(source, "top")(rec, req)
	if rec.Code != 200 || !source.revalidated {
		t.Fatalf("refresh request not honored: status %d", rec.Code)
	}
}
