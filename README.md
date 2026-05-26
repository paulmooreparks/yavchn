# yavchn

Yet Another Vibe-Coded Hacker News.

A three-pane web reader for Hacker News:

- **Left:** infinite-scroll list of front-page stories.
- **Right-top:** the linked article, reader-mode extracted.
- **Right-bottom:** the HN discussion thread (iframed from news.ycombinator.com so your existing HN session handles vote/save/hide/comment natively).

## Running

```
go run ./src
```

Serves on `http://localhost:8080`.

The Go module lives at the repo root (`go.mod`); all source is under `src/`.

## Stack

- Go 1.25, standard `net/http` + `html/template`.
- `golang.org/x/sync/singleflight` — coalesce concurrent upstream fetches.
- `github.com/go-shiori/go-readability` — server-side article extraction.
- `modernc.org/sqlite` — pure-Go SQLite for the article-extraction cache.
- Vanilla JS frontend, CSS grid layout. No SPA framework.

## Design notes

- **URL-is-king.** Every state is addressable: `/`, `/?page=N`, `/s/{hn-id}`.
- **Zero auth in this app.** HN has no OAuth. The discussion pane iframes news.ycombinator.com, so your HN session in the browser handles all logged-in actions. We never see your credentials or cookies.
- **Cache hierarchy.** HN top-list and items: in-memory. Extracted articles: SQLite (expensive to recompute, stable per URL).
