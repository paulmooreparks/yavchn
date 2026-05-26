package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

	tpl, err := template.ParseFS(assets, "templates/*.tmpl")
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
	extract := NewExtractor(db)

	srv := NewServer(hn, tpl, extract, db)
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /api/article", srv.ArticleAPI)
	mux.HandleFunc("GET /api/discussion", srv.DiscussionAPI)
	mux.HandleFunc("GET /healthz", srv.Healthz)
	mux.HandleFunc("GET /{$}", srv.Index)
	mux.HandleFunc("GET /s/{id}", srv.Index)
	for _, src := range []string{"show", "ask", "new"} {
		mux.HandleFunc("GET /"+src, srv.Index)
		mux.HandleFunc("GET /"+src+"/{$}", srv.Index)
		mux.HandleFunc("GET /"+src+"/s/{id}", srv.Index)
	}

	httpSrv := &http.Server{
		Addr:              ":8080",
		Handler:           withSecurityHeaders(mux),
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
