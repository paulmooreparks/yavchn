package main

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestArticleAPIArchiveFallback(t *testing.T) {
	for _, code := range []int{401, 403, 404, 410, 451, 429, 500, 503} {
		for _, refresh := range []string{"", "&refresh=1"} {
			t.Run(fmt.Sprintf("%d%s", code, refresh), func(t *testing.T) {
				e := newTestExtractor(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
					return htmlResp(code, ""), nil
				}))
				s := &Server{extract: e}
				w := httptest.NewRecorder()
				rawURL := "https://example.com/story?a=1&b=2"
				s.ArticleAPI(w, httptest.NewRequest("GET", "/api/article?url="+url.QueryEscape(rawURL)+refresh, nil))
				body := html.UnescapeString(w.Body.String())
				wantArchive := code == 401 || code == 403 || code == 404 || code == 410 || code == 451
				if got := strings.Contains(body, "https://web.archive.org/web/*/"+rawURL); got != wantArchive {
					t.Fatalf("archive link = %v, want %v: %s", got, wantArchive, body)
				}
				if w.Code != http.StatusOK || !strings.Contains(body, fmt.Sprintf("HTTP %d", code)) || !strings.Contains(body, "Open the article") {
					t.Fatalf("unexpected fallback: %d %s", w.Code, body)
				}
			})
		}
	}
}

func TestArticleArchiveURLSafety(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &upstreamHTTPError{StatusCode: 403})
	body := articleErrorHTML("https://user:secret@example.com/story?a=1&b=2#section", err)
	if !strings.Contains(body, `href="https://web.archive.org/web/*/https://example.com/story?a=1&amp;b=2"`) {
		t.Fatalf("archive URL should omit credentials and fragment: %s", body)
	}
	for _, rawURL := range []string{"javascript:alert(1)", "https:///missing-host", ""} {
		if strings.Contains(articleErrorHTML(rawURL, err), "web.archive.org") {
			t.Errorf("archive link for invalid URL %q", rawURL)
		}
	}
	body = articleErrorHTML(`https://example.com/" onclick="alert(1)`, err)
	if strings.Contains(body, `" onclick="`) {
		t.Fatalf("unescaped attribute: %s", body)
	}
	if strings.Contains(articleErrorHTML("https://example.com/", errors.New("not html")), "web.archive.org") {
		t.Fatal("parse failures should not offer an archive lookup")
	}
}
