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

func TestReaderPlacementPreference(t *testing.T) {
	_, mux := testServer(t)
	for _, value := range []string{"floating", "maximized", "invalid"} {
		r := httptest.NewRequest("POST", "/settings/reader-placement", strings.NewReader("placement="+value))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if value == "invalid" {
			if w.Code != 400 || len(w.Result().Cookies()) != 0 {
				t.Fatal("invalid preference accepted")
			}
		} else if w.Code != 204 || w.Result().Cookies()[0].Value != value {
			t.Fatal("preference not saved")
		}
	}
	for _, tc := range []struct{ path, preference, want string }{
		{"/window/reader-1?r.reader-1=hn-1", "", "floating"},
		{"/window/reader-1?r.reader-1=hn-1", "maximized", "maximized"},
		{"/window/reader-1", "maximized", "maximized"},
		{"/window/settings", "maximized", "floating"},
		{"/hn/?open=reader-1&r.reader-1=hn-1&p.reader-1=floating:0.1,0.1,0.5,0.7", "maximized", "floating"},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "yavchn-reader-placement", Value: tc.preference})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `data-win-mode="`+tc.want+`"`) {
			t.Fatalf("%s: expected %s", tc.path, tc.want)
		}
	}
}
