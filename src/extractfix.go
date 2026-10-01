package main

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Repairs made to a fetched page before readability reads it, for the two
// ways it otherwise loses a page's images.

// documentBase is the URL a page's relative links resolve against: its
// first <base href>, resolved against the page's own URL, as the HTML
// standard says, or the page's URL when it has none. Readability resolves
// relative URLs against the URL it is given and never reads <base>, so a
// page whose images are written relative to a <base href="/"> otherwise
// loses every one of them to a doubled path.
func documentBase(doc *html.Node, pageURL *url.URL) *url.URL {
	var href string
	var find func(n *html.Node) bool
	find = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Base {
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = strings.TrimSpace(a.Val)
					return true
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if find(c) {
				return true
			}
		}
		return false
	}
	find(doc)
	if href == "" {
		return pageURL
	}
	base, err := pageURL.Parse(href)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return pageURL
	}
	return base
}

// lazyImageAttrs are where lazy-loading scripts keep an image's real
// address, in the order they are trusted.
var lazyImageAttrs = []string{"data-src", "data-lazy-src", "data-original", "data-url", "data-srcset", "data-lazy-srcset"}

// fixLazyImages gives each lazily loaded image its real address. Such a
// page leaves src empty or holds a placeholder there, often a data: SVG,
// and a script swaps in the address from an attribute such as data-src
// once the image scrolls into view. Readability keeps an SVG placeholder,
// and the sanitiser then drops the data: URL, so the image is left with
// no address at all. Here src takes the real one before either sees it.
func fixLazyImages(doc *html.Node) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			if src := attr(n, "src"); src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
				for _, name := range lazyImageAttrs {
					if real := firstCandidate(attr(n, name)); real != "" && !strings.HasPrefix(strings.ToLower(real), "data:") {
						setAttr(n, "src", real)
						break
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

// firstCandidate is the first URL of a srcset, or the value itself when it
// is a plain URL.
func firstCandidate(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, ","); i >= 0 && strings.Contains(v, " ") {
		v = v[:i]
	}
	if f := strings.Fields(v); len(f) > 0 {
		return f[0]
	}
	return ""
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, val string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
