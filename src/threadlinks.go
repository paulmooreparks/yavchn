package main

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// threadKey is the story window key a link to a Hacker News item or a
// Lobsters story names, or "" for any other link. An HN item may be a
// comment, whose window shows the comment above its replies.
func threadKey(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	switch strings.TrimPrefix(strings.ToLower(u.Host), "www.") {
	case "news.ycombinator.com":
		if id := u.Query().Get("id"); u.Path == "/item" && id != "" && strings.Trim(id, "0123456789") == "" {
			return "hn-" + id
		}
	case "lobste.rs":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "s" && storyIDRE.MatchString(parts[1]) {
			return "lobsters-" + parts[1]
		}
	}
	return ""
}

// linkThreads makes the links in a sanitised fragment that point at
// another thread open that thread as a story window, as a row in the list
// does: the link goes to the story's own page, and with script
// pudl-windows.js opens its window instead. Following a conversation from
// thread to thread then stays in YAVCHN, and Back retraces it.
func linkThreads(fragment string) string {
	if !strings.Contains(fragment, "news.ycombinator.com") && !strings.Contains(fragment, "lobste.rs") {
		return fragment
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return fragment
	}
	changed := false
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			changed = rewriteThreadLink(n) || changed
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	if !changed {
		return fragment
	}
	var b strings.Builder
	for _, n := range nodes {
		if err := html.Render(&b, n); err != nil {
			return fragment
		}
	}
	return b.String()
}

func rewriteThreadLink(a *html.Node) bool {
	key := ""
	for _, attr := range a.Attr {
		if attr.Key == "href" {
			key = threadKey(attr.Val)
		}
	}
	if key == "" {
		return false
	}
	source, id, _ := strings.Cut(key, "-")
	attrs := a.Attr[:0]
	for _, attr := range a.Attr {
		switch attr.Key {
		case "target", "data-win-open":
			// The story opens here, in a window, rather than in a new tab.
		case "href":
			attrs = append(attrs, html.Attribute{Key: "href", Val: storyPageURL(source, id)})
		default:
			attrs = append(attrs, attr)
		}
	}
	a.Attr = append(attrs, html.Attribute{Key: "data-win-open", Val: key})
	return true
}
