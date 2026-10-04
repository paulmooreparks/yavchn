package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/*.tmpl static/*
var assets embed.FS

// withSecurityHeaders applies baseline response headers to every reply: no
// clickjacking, no MIME sniffing, no referrer leakage to third-party sites.
// Sits at the mux boundary so all routes -- including /static/ -- get it.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	tpl, err := parseTemplates()
	if err != nil {
		slog.Error("parse templates", "err", err)
		os.Exit(1)
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		slog.Error("static fs sub", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dbPath := os.Getenv("YAVCHN_DB_PATH")
	if dbPath == "" {
		dbPath = "./yavchn.db"
	}
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		slog.Error("open db", "path", dbPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()

	hn := NewHN()
	hn.StartBackgroundRefresh(ctx)
	lobsters := NewLobsters()
	lobsters.StartBackgroundRefresh(ctx)

	sources := map[string]Source{
		"hn":       hn,
		"lobsters": lobsters,
	}
	// Ordered provider list for the discussion-finder (HN first, then Lobsters).
	finders := []DiscussionProvider{hn, lobsters}

	extract := NewExtractor(db)
	extract.StartGC(ctx)

	srv := NewServer(sources, finders, "hn", hn, tpl, extract, db)
	accountCfg, err := loadAccountConfig()
	if err != nil {
		slog.Error("account configuration", "err", err)
		os.Exit(1)
	}
	srv.accounts, err = newAccountService(srv, accountCfg)
	if err != nil {
		slog.Error("account storage initialization failed")
		os.Exit(1)
	}
	mux := http.NewServeMux()
	srv.accounts.register(mux)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /api/article", srv.ArticleAPI)
	mux.HandleFunc("GET /api/discussion", srv.DiscussionAPI)
	mux.HandleFunc("GET /healthz", srv.Healthz)
	mux.HandleFunc("POST /settings/view", srv.ViewSetting)
	mux.HandleFunc("POST /settings/reader-placement", srv.ReaderPlacementSetting)
	mux.HandleFunc("GET /settings", srv.appletPage("settings"))

	// Discussion-finder: /find (empty) and /find?url=<encoded> (results).
	mux.HandleFunc("GET /find", srv.Finder)

	// A story window's markup, fetched by pudl-windows.js, and a story's
	// own page, where its link goes without script.
	mux.HandleFunc("GET /window/{key}", srv.Window)
	mux.HandleFunc("GET /story/{source}/{id}", srv.StoryPage)

	// The applets, each a page of its own as well as a window, and the
	// fragments their scripts fetch.
	mux.HandleFunc("GET /applets/replies", srv.appletPage("replies"))
	mux.HandleFunc("GET /applets/hiring", srv.appletPage("hiring"))
	mux.HandleFunc("GET /user", srv.UserLookup)
	mux.HandleFunc("GET /user/{source}/{name}", srv.UserPage)
	mux.HandleFunc("GET /api/replies", srv.RepliesAPI)
	mux.HandleFunc("GET /api/hiring", srv.HiringAPI)

	// Root → redirect to the default source. The client-side dropdown can
	// override this on subsequent visits by navigating to /{stored-source}/
	// directly; we just need a sensible landing page for first-touch.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+srv.defaultSource+"/", http.StatusFound)
	})

	// Per-source routes for HN and Lobsters.
	for name, src := range sources {
		registerSourceRoutes(mux, name, src, srv)
	}

	// Search: HN-only. /lobsters/search is registered as an explicit 404
	// so a stale link or accidental nav gets a clear response (rather than
	// falling into the catch-all source-tab route which would 404 anyway,
	// but with a less helpful message).
	mux.HandleFunc("GET /hn/search", srv.Search)
	mux.HandleFunc("GET /hn/search/s/{id}", windowRedirect("/hn/search", "hn"))
	mux.HandleFunc("GET /lobsters/search", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Search isn't available for Lobsters (no JSON search API). Use /hn/search instead.", http.StatusNotFound)
	})

	// Pinned: global, cross-source. The two older shapes that named a
	// selected story, /pinned/s/{id} (HN) and /pinned/s/{source}/{id},
	// redirect to the story's window.
	mux.HandleFunc("GET /pinned", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pinned/", http.StatusFound)
	})
	mux.HandleFunc("GET /pinned/{$}", srv.Pinned)
	mux.HandleFunc("GET /pinned/s/{id}", windowRedirect("/pinned/", "hn"))
	mux.HandleFunc("GET /pinned/s/{source}/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sources[r.PathValue("source")]; !ok {
			http.NotFound(w, r)
			return
		}
		windowRedirect("/pinned/", r.PathValue("source"))(w, r)
	})

	// Collections and Notes: browser-stored lists like Pinned.
	mux.HandleFunc("GET /collections", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/collections/", http.StatusFound)
	})
	mux.HandleFunc("GET /collections/{$}", srv.Collections)
	mux.HandleFunc("GET /collections/{id}/{$}", srv.CollectionRedirect)
	mux.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/notes/", http.StatusFound)
	})
	mux.HandleFunc("GET /notes/{$}", srv.Notes)

	// Backwards-compat 301 redirects from the pre-multi-source flat URLs.
	registerLegacyRedirects(mux)

	httpSrv := &http.Server{
		Addr:              ":8080",
		Handler:           withSecurityHeaders(srv.accounts.middleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("yavchn listening", "addr", httpSrv.Addr, "db", dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)
}

// windowRedirect sends an address from before story windows, which named
// one selected story as .../s/{id}, to the list it was on with that story's
// window open, keeping its other parameters (a page, a search).
func windowRedirect(listPath, source string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !storyIDRE.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		key := source + "-" + id
		st := winState{named: true, open: []string{key}, top: key}
		http.Redirect(w, r, winURL(listPath, restParams(r.URL.Query()), st), http.StatusMovedPermanently)
	}
}

// registerSourceRoutes wires the URL shapes a source needs:
//
//	GET /{source}/                      default tab
//	GET /{source}/{tab}/                explicit tab
//	GET /{source}/s/{id}                older address of a selected story, redirected to its window
//	GET /{source}/{tab}/s/{id}          the same, on a tab
func registerSourceRoutes(mux *http.ServeMux, name string, src Source, srv *Server) {
	def := src.DefaultTab()

	// Default tab: bare /{source}/, and the older /{source}/s/{id}.
	mux.HandleFunc("GET /"+name+"/{$}", srv.SourceIndex(src, def))
	mux.HandleFunc("GET /"+name+"/s/{id}", windowRedirect("/"+name+"/", name))
	// Tolerate the trailing-slash-less variant by redirecting (matches the
	// browser's natural URL shape from typed addresses).
	mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+name+"/", http.StatusFound)
	})

	// Explicit tabs. Iterate over the source's tab definitions so each
	// gets its own dedicated handler (lets net/http's mux do precise
	// matching rather than us pattern-matching tab slugs in a generic
	// handler). Skip the default tab — already wired above.
	for _, t := range src.Tabs() {
		if t.Slug == def {
			continue
		}
		mux.HandleFunc("GET /"+name+"/"+t.Slug+"/{$}", srv.SourceIndex(src, t.Slug))
		mux.HandleFunc("GET /"+name+"/"+t.Slug+"/s/{id}", windowRedirect("/"+name+"/"+t.Slug+"/", name))
		mux.HandleFunc("GET /"+name+"/"+t.Slug, func(slug string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/"+name+"/"+slug+"/", http.StatusFound)
			}
		}(t.Slug))
	}
}

// registerLegacyRedirects 301-redirects the pre-multi-source flat URLs to
// their /hn/* equivalents so bookmarks and external links still work.
func registerLegacyRedirects(mux *http.ServeMux) {
	// Legacy /s/{id} → the HN front page with the story's window open.
	mux.HandleFunc("GET /s/{id}", windowRedirect("/hn/", "hn"))

	// Legacy /search and /search/s/{id} → /hn/search…
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/hn/search"+queryString(r), http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /search/s/{id}", windowRedirect("/hn/search", "hn"))

	// Legacy HN tab roots → /hn/{tab}/ (top has no flat variant; the legacy
	// /show, /ask, /new, /best, /jobs all map to their HN tab equivalents).
	for _, tab := range []string{"show", "ask", "new", "best", "jobs"} {
		tab := tab
		mux.HandleFunc("GET /"+tab, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/hn/"+tab+"/"+queryString(r), http.StatusMovedPermanently)
		})
		mux.HandleFunc("GET /"+tab+"/{$}", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/hn/"+tab+"/"+queryString(r), http.StatusMovedPermanently)
		})
		mux.HandleFunc("GET /"+tab+"/s/{id}", windowRedirect("/hn/"+tab+"/", "hn"))
	}
}

// queryString returns "?<raw>" or "" for use when building redirect targets.
func queryString(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

// (unused now, but kept for symmetry — url.QueryEscape is still imported via hn.go)
var _ = url.QueryEscape
var _ = strings.TrimRight
