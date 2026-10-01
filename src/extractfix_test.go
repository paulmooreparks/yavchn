package main

import (
	"net/url"
	"strings"
	"testing"

	readability "github.com/go-shiori/go-readability"
	"golang.org/x/net/html"
)

func parseDoc(t *testing.T, s string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDocumentBase(t *testing.T) {
	page, _ := url.Parse("https://example.org/posts/one/")
	for markup, want := range map[string]string{
		`<head><base href="/"></head>`:                        "https://example.org/",
		`<head><base href="../assets/"></head>`:               "https://example.org/posts/assets/",
		`<head><base href="https://cdn.example.net/"></head>`: "https://cdn.example.net/",
		`<head><base target="_blank"></head>`:                 "https://example.org/posts/one/",
		`<head><base href="javascript:alert(1)"></head>`:      "https://example.org/posts/one/",
		`<head></head>`: "https://example.org/posts/one/",
	} {
		if got := documentBase(parseDoc(t, markup), page).String(); got != want {
			t.Errorf("documentBase(%s) = %q, want %q", markup, got, want)
		}
	}
}

func TestFixLazyImages(t *testing.T) {
	doc := parseDoc(t, `<body>
<img id="a" src="data:image/svg+xml;base64,PHN2Zz4=" data-src="https://x.example/real.jpg">
<img id="b" data-srcset="https://x.example/small.jpg 300w, https://x.example/big.jpg 900w">
<img id="c" src="https://x.example/kept.jpg" data-src="https://x.example/other.jpg">
<img id="d" src="data:image/gif;base64,R0lGOD">
</body>`)
	fixLazyImages(doc)
	got := map[string]string{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			got[attr(n, "id")] = attr(n, "src")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	for id, want := range map[string]string{
		"a": "https://x.example/real.jpg",
		"b": "https://x.example/small.jpg",
		"c": "https://x.example/kept.jpg",
		"d": "data:image/gif;base64,R0lGOD", // nothing better to put there
	} {
		if got[id] != want {
			t.Errorf("img %s src = %q, want %q", id, got[id], want)
		}
	}
}

// End to end, the way unsung.aresluna.org writes its images: relative to a
// <base href="/">, in a <picture>. Before the fix every one 404ed.
func TestExtraction_HonoursBase(t *testing.T) {
	page, _ := url.Parse("https://unsung.example/before-pixels/")
	para := strings.Repeat("Industrial dashboards were built from modules, and each one had a job to do on the panel. ", 12)
	doc := parseDoc(t, `<html><head><base href="/"><title>Before pixels</title></head><body><article>
<h1>Before pixels</h1><p>`+para+`</p>
<picture><source srcset="_media/before-pixels/1.avif" type="image/avif"><img src="_media/before-pixels/1.jpg" alt="A panel"></picture>
<p>`+para+`</p></article></body></html>`)
	fixLazyImages(doc)
	a, err := readability.FromDocument(doc, documentBase(doc, page))
	if err != nil {
		t.Fatal(err)
	}
	out := sanitizeHTML(a.Content)
	if !strings.Contains(out, `src="https://unsung.example/_media/before-pixels/1.jpg"`) {
		t.Errorf("the image should resolve against the base, got: %s", out)
	}
	if strings.Contains(out, "/before-pixels/_media/") {
		t.Errorf("the image resolved against the page's own path: %s", out)
	}
}
