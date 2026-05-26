# yavchn

Yet Another Vibe-Coded Hacker News.

A three-pane web reader for Hacker News:

- **Left:** paged list of stories
- **Right-top:** the linked article, reader-mode extracted
- **Right-bottom:** the HN discussion thread

## Running

```
go run ./src
```

Serves on `http://localhost:8080`.

## Stack

- Go 1.25, standard `net/http` + `html/template`.
- `golang.org/x/sync/singleflight` &mdash; coalesce concurrent upstream fetches.
- `github.com/hashicorp/golang-lru/v2/expirable` &mdash; bounded LRU + TTL for HN item and thread caches.
- `github.com/go-shiori/go-readability` &mdash; server-side article extraction.
- `github.com/microcosm-cc/bluemonday` &mdash; HTML sanitisation for extracted articles and comment bodies.
- `modernc.org/sqlite` &mdash; pure-Go SQLite for the article-extraction cache.
- Vanilla JS frontend, CSS grid layout. No SPA framework.

## Design notes

- **URL-is-king.** Every state is addressable: `/`, `/?page=N`, `/s/{hn-id}`, `/s/{hn-id}?page=N`.
- **Zero auth in this app.** HN has no OAuth. Comments are fetched server-side from the HN Algolia API and rendered into the discussion pane. For vote / save / hide / reply, click the per-comment "&uarr;" or the pane-header "Open on HN &uarr;" &mdash; opens the item on news.ycombinator.com in a new tab where your existing HN session handles the action. yavchn never sees your credentials or cookies.
- **Cache hierarchy.** HN top-list, items, and comment threads: in-memory LRU + TTL. Extracted articles: SQLite (expensive to recompute, stable per URL).
- **Progressive enhancement.** Pages render server-side without JavaScript. Article reader-mode and discussion rendering are lazy-loaded after first paint via `GET /api/article?url=...` and `GET /api/discussion?id=...`; no-JS visitors get prominent "Open article" / "Open on HN" fallback links.
- **Persisted UI state.** Splitter sizes and the light/dark theme preference live in `localStorage`; an inline script in `<head>` applies them before first paint to avoid flash.

## Docker

```
docker build -t yavchn .
docker run --rm -p 8080:8080 yavchn
```

Single-binary distroless image. SQLite article cache lives at `/home/nonroot/yavchn.db` inside the container; rebuilds from scratch after a container replace.
