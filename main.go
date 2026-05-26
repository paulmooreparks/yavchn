package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed templates/*.tmpl static/*
var assets embed.FS

func main() {
	tpl, err := template.ParseFS(assets, "templates/*.tmpl")
	if err != nil {
		log.Fatal(err)
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dbPath := os.Getenv("YAVCHN_DB_PATH")
	if dbPath == "" {
		dbPath = "./yavchn.db"
	}
	db, err := OpenDB(ctx, dbPath)
	if err != nil {
		log.Fatalf("open db (%s): %v", dbPath, err)
	}
	defer db.Close()

	hn := NewHN()
	hn.StartBackgroundRefresh(ctx)
	extract := NewExtractor(db)

	srv := NewServer(hn, tpl, extract)
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /api/article", srv.ArticleAPI)
	mux.HandleFunc("GET /api/discussion", srv.DiscussionAPI)
	mux.HandleFunc("GET /{$}", srv.Index)
	mux.HandleFunc("GET /s/{id}", srv.Index)

	httpSrv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("yavchn listening on %s (db=%s)", httpSrv.Addr, dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)
}
