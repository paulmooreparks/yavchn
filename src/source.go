package main

import "context"

// Source is the polymorphic interface for a news-aggregation site (HN,
// Lobsters, future additions). Each source knows its own tabs, its own
// upstream endpoints, and its own ID format. Item / Comment / StoryThread
// are the shared shapes that source-specific decoders normalize into.
//
// IDs are strings. HN's int64 IDs are stringified at the source boundary;
// Lobsters uses base36 short IDs natively. This lets the rest of the
// codebase work with one ID type without per-source generics.
type Source interface {
	Name() string                                                                       // "hn" / "lobsters"
	Label() string                                                                      // "Hacker News" / "Lobsters"
	Tabs() []TabDef                                                                     // per-source tab set for the source-tabs row
	ValidTab(slug string) bool                                                          // does this source have a tab with this slug?
	DefaultTab() string                                                                 // the slug rendered when the visitor lands on /{source}/
	StoryIDs(ctx context.Context, tab string, page int) (ids []string, hasNext bool, err error) // 1-based page; hasNext signals "more pages exist upstream"
	Item(ctx context.Context, id string) (*Item, error)                                 // a single story
	ItemsParallel(ctx context.Context, ids []string) []*Item                            // bulk fetch for a list page
	StoryThread(ctx context.Context, id, requesterIP string) (*StoryThread, error)      // the comment tree for one story
	StoryDiscussionURL(id string) string                                                // the canonical discussion URL on the source's own site
	StartBackgroundRefresh(ctx context.Context)                                         // keep the default tab warm
}

// TabDef defines one entry in the source-tabs row.
type TabDef struct {
	Slug  string // URL fragment, e.g. "top" or "hottest"
	Label string // display text, e.g. "Top" or "Hottest"
}

// Item is the shared shape both HN and Lobsters decoders normalize into.
// Source-specific JSON shapes (HN's firebaseio.com payload, Lobsters'
// /s/{id}.json payload) decode into private types and copy into Item.
type Item struct {
	ID          string
	By          string
	Time        int64
	Text        string
	Dead        bool
	Deleted     bool
	Kids        []string
	URL         string
	Score       int
	Title       string
	Type        string
	Descendants int
}

// Comment is one node in a discussion tree.
type Comment struct {
	ID        string
	Author    string
	Text      string
	CreatedAt int64
	Children  []*Comment
}

// StoryThread is the comment tree for one story.
type StoryThread struct {
	StoryID  string
	Comments []*Comment
}
