package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestViewsAndPreference(t *testing.T) {
	_, mux := testServer(t)
	for _, tc := range []struct {
		path, cookie, view string
		layer              bool
	}{
		{"/hn/", "", "window", true},
		{"/hn/", "classic", "classic", false},
		{"/hn/?view=window", "classic", "window", true},
		{"/hn/?view=classic&open=hn-1", "window", "classic", false},
		{"/hn/?open=hn-1", "classic", "window", true},
		{"/story/hn/1", "window", "classic", false},
		{"/user", "window", "classic", false},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "yavchn-view", Value: tc.cookie})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `data-view="`+tc.view+`"`) || strings.Contains(body, `data-win-layer`) != tc.layer {
			t.Fatalf("%s (%s): unexpected view", tc.path, tc.cookie)
		}
		hasList := strings.HasPrefix(tc.path, "/hn/")
		for _, marker := range []string{`class="source-switch"`, `href="/hn/">Hacker News</a>`, `href="/find">Find the discussions of a link</a>`} {
			if strings.Contains(body, marker) != hasList {
				t.Errorf("%s: feed navigation visibility incorrect for %s", tc.path, marker)
			}
		}
		if !tc.layer && (strings.Contains(body, `data-focus-toggle`) || strings.Contains(body, `data-close-all`)) {
			t.Fatalf("desktop commands on %s", tc.path)
		}
	}
	data := url.Values{"view": {"classic"}, "target": {"/hn/?view=classic"}}
	r := httptest.NewRequest("POST", "/settings/view", strings.NewReader(data.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/hn/?view=classic" || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].Value != "classic" {
		t.Fatalf("settings: %d %v", w.Code, w.Header())
	}
	data.Set("target", "//example.com/")
	r = httptest.NewRequest("POST", "/settings/view", strings.NewReader(data.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("external redirect accepted")
	}
}
