package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHN_Tabs(t *testing.T) {
	h := NewHN()
	tabs := h.Tabs()
	want := []string{"top", "show", "ask", "new", "best", "jobs"}
	if len(tabs) != len(want) {
		t.Fatalf("expected %d tabs, got %d", len(want), len(tabs))
	}
	for i, slug := range want {
		if tabs[i].Slug != slug {
			t.Errorf("Tabs()[%d].Slug = %q, want %q", i, tabs[i].Slug, slug)
		}
		if !h.ValidTab(slug) {
			t.Errorf("ValidTab(%q) = false, want true", slug)
		}
	}
	bad := []string{"", "random", "TOP", "Top", "foo"}
	for _, s := range bad {
		if h.ValidTab(s) {
			t.Errorf("ValidTab(%q) = true, want false", s)
		}
	}
	if h.DefaultTab() != "top" {
		t.Errorf("DefaultTab() = %q, want \"top\"", h.DefaultTab())
	}
	if h.Name() != "hn" {
		t.Errorf("Name() = %q, want \"hn\"", h.Name())
	}
}

func TestHN_Item_CachesAfterFirstFetch(t *testing.T) {
	rt := &countingRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `{"id": 42, "by": "alice", "title": "hi", "type": "story"}`), nil
	}}
	h := NewHN()
	h.http = &http.Client{Transport: rt}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		it, err := h.Item(ctx, "42")
		if err != nil {
			t.Fatalf("Item %d: %v", i, err)
		}
		if it.ID != "42" {
			t.Fatalf("Item %d: ID = %q, want \"42\"", i, it.ID)
		}
	}
	if got := rt.Calls(); got != 1 {
		t.Fatalf("expected 1 upstream call (subsequent served from cache), got %d", got)
	}
}

func TestHN_Item_NullBodyReturnsNotFound(t *testing.T) {
	// firebaseio returns 200 + literal `null` for unknown IDs.
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, "null"), nil
	})
	h := NewHN()
	h.http = &http.Client{Transport: rt}

	_, err := h.Item(context.Background(), "99999999")
	if err == nil {
		t.Fatal("expected not-found error for null body")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got %v", err)
	}
}

func TestHN_StoryIDs_RejectsUnknownTab(t *testing.T) {
	h := NewHN()
	if _, _, err := h.StoryIDs(context.Background(), "made-up-tab", 1); err == nil {
		t.Fatal("expected error for unknown tab")
	}
}

func TestHN_StoryIDs_CachesIDList(t *testing.T) {
	rt := &countingRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		return jsonResp(200, `[1, 2, 3]`), nil
	}}
	h := NewHN()
	h.http = &http.Client{Transport: rt}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ids, _, err := h.StoryIDs(ctx, hnTabTop, 1)
		if err != nil {
			t.Fatalf("StoryIDs %d: %v", i, err)
		}
		if len(ids) != 3 {
			t.Fatalf("StoryIDs %d: got %d ids, want 3", i, len(ids))
		}
		if ids[0] != "1" {
			t.Fatalf("StoryIDs %d: ids[0] = %q, want \"1\" (stringified)", i, ids[0])
		}
	}
	if got := rt.Calls(); got != 1 {
		t.Fatalf("expected 1 upstream call (subsequent served from per-tab cache), got %d", got)
	}
}

func TestConvertHNComment_DeletedAuthorPlaceholder(t *testing.T) {
	c := convertHNComment(algoliaItem{ID: 1, Author: "", Text: "x"})
	if c.Author != "[deleted]" {
		t.Fatalf("empty author should become [deleted], got %q", c.Author)
	}
	if c.ID != "1" {
		t.Fatalf("expected stringified ID \"1\", got %q", c.ID)
	}
}

func TestConvertHNComment_PreservesAuthorAndChildren(t *testing.T) {
	raw := algoliaItem{
		ID: 1, Author: "alice", Text: "parent",
		Children: []algoliaItem{
			{ID: 2, Author: "bob", Text: "child-a"},
			{ID: 3, Author: "", Text: "child-b"},
		},
	}
	c := convertHNComment(raw)
	if c.Author != "alice" {
		t.Fatalf("expected alice, got %q", c.Author)
	}
	if len(c.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(c.Children))
	}
	if c.Children[0].Author != "bob" {
		t.Fatalf("expected bob, got %q", c.Children[0].Author)
	}
	if c.Children[1].Author != "[deleted]" {
		t.Fatalf("expected [deleted] on empty-author child, got %q", c.Children[1].Author)
	}
}
