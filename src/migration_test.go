package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestMigrationBrowser(t *testing.T) {
	if os.Getenv("YAVCHN_BROWSER_TEST") == "" {
		t.Skip("set YAVCHN_BROWSER_TEST=1 and install playwright to run browser tests")
	}
	_, mux := testServer(t)
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../tests/migration.browser.cjs", srv.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
