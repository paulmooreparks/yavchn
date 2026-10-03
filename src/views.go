package main

import (
	"net/http"
	"net/url"
	"strings"
)

// menuContext describes the rendered page, independently of screen width.
type menuContext struct {
	Account    *accountSession
	View       string
	WindowView bool
	HasList    bool
	HasFeed    bool
	WindowURL  string
	ClassicURL string
	StoriesURL string
}

func viewURL(raw, view string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/hn/?view=" + view
	}
	q := u.Query()
	q.Set("view", view)
	if view == "classic" {
		for k := range q {
			if k == "open" || k == "top" || k == "min" || strings.HasPrefix(k, "p.") || strings.HasPrefix(k, "r.") {
				q.Del(k)
			}
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func listView(r *http.Request) string {
	if v := r.URL.Query().Get("view"); v == "classic" || v == "window" {
		return v
	}
	if r.URL.Query().Has("open") {
		return "window"
	}
	return savedView(r)
}

func savedView(r *http.Request) string {
	if c, err := r.Cookie("yavchn-view"); err == nil && c.Value == "classic" {
		return "classic"
	}
	return "window"
}

func listMenu(r *http.Request, feed bool) menuContext {
	v := listView(r)
	return menuContext{View: v, WindowView: v == "window", HasList: true, HasFeed: feed,
		Account:   currentAccount(r),
		WindowURL: viewURL(r.URL.RequestURI(), "window"), ClassicURL: viewURL(r.URL.RequestURI(), "classic")}
}

func pageMenu(r *http.Request, key, source string) menuContext {
	if source == "" {
		source = "hn"
	}
	q := r.URL.Query()
	q.Set("view", "window")
	q.Set("open", key)
	q.Set("top", key)
	return menuContext{Account: currentAccount(r), View: "classic", WindowURL: "/" + source + "/?" + q.Encode(), ClassicURL: r.URL.RequestURI()}
}

// ViewSetting changes only this browser's preferred browsing view.
func (s *Server) ViewSetting(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid settings", 400)
		return
	}
	v := r.PostForm.Get("view")
	if v != "window" && v != "classic" {
		http.Error(w, "Invalid view", 400)
		return
	}
	target := r.PostForm.Get("target")
	u, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.Contains(target, "\\") || u.Host != "" || u.Scheme != "" {
		http.Error(w, "Invalid destination", 400)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "yavchn-view", Value: v, Path: "/", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Accept") == "application/json" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// savedReaderPlacement applies only when the URL supplies no placement.
func savedReaderPlacement(r *http.Request) string {
	if c, err := r.Cookie("yavchn-reader-placement"); err == nil && c.Value == "maximized" {
		return "maximized"
	}
	return "floating"
}

func (s *Server) ReaderPlacementSetting(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid settings", http.StatusBadRequest)
		return
	}
	value := r.PostForm.Get("placement")
	if value != "floating" && value != "maximized" {
		http.Error(w, "Invalid placement", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "yavchn-reader-placement", Value: value, Path: "/", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Accept") == "application/json" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
