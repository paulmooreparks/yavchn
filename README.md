# YAVCHN - Yet Another Vibe-Coded Hacker News (Wrapper)

<img src="src/static/logo.png" align="right" width="200" alt="YAVCHN logo">

A three-pane web reader for [Hacker News](https://news.ycombinator.com/) and [Lobsters](https://lobste.rs):

- **Left:** The list of articles from the active source
- **Right-top:** The linked article, reader-mode extracted
- **Right-bottom:** The discussion thread, server-rendered

Why?

Because I could!

## No, Really... Why?

It seems that everybody has their own personalized Hacker-News reader these days. After all, why not? It's so easy to whip one up in an evening when you have agentic coding at your disposal.

Whenever I browse HN, I find myself opening the discussion in a new tab, then clicking through in that tab to view the article, then popping back to the discussion. It's all very annoying, when what I really want to do is get a quick overview of the article and see if there is any interesting discussion going on before I dive into either the article or the discussion.

YAVCHN lets me quickly browse an article in scaled-down reader mode in one pane and see the discussion in the pane below. If I find either one compelling, I can click "Open original" to see the original article or "Open on HN" / "Open on Lobsters" to join the discussion on the source's own site.

The same three-pane treatment works on [Lobsters](https://lobste.rs) (a smaller, computing-focused link aggregator) thanks to a tiny `Source` abstraction in the Go backend; pick the source from the segmented control at the top of the header.

![YAVCHN, three-pane reader: story list, reader-mode article, and threaded discussion](screenshot.png)

## Live Site

The site is live at https://yavchn.parkscomputing.com/ if you'd like to try it out.

## Running Locally

```
go run ./src
```

Serves on `http://localhost:8080`.

## Stack

- Go 1.25, standard `net/http` + `html/template`.
- `golang.org/x/sync/singleflight`: coalesce concurrent upstream fetches.
- `github.com/hashicorp/golang-lru/v2/expirable`: bounded LRU + TTL for HN item and thread caches.
- `github.com/go-shiori/go-readability`: server-side article extraction.
- `github.com/microcosm-cc/bluemonday`: HTML sanitisation for extracted articles and comment bodies.
- `modernc.org/sqlite`: pure-Go SQLite for the article-extraction cache.
- Vanilla JS frontend, CSS grid layout. No SPA framework.

## Design notes

- **The URL is king.** Every page in YAVCHN has its own URL. You can bookmark `/hn/show/`, `/lobsters/`, `/pinned/`, or a specific story like `/hn/s/12345678`, and reopening that URL takes you straight back to what you were reading. The source, the tab, the page number, and the selected story all live in the URL. Nothing important is hidden behind ephemeral client state.

- **No accounts, no per-user server state.** YAVCHN never sees your HN or Lobsters credentials. Comments are fetched from each site's public JSON API and rendered into the discussion pane. When you want to vote, reply, save, or hide a comment, click the upward arrow next to it (or the "Open on HN" / "Open on Lobsters" link at the top of the pane). That opens the item on the source's own site in a new tab, where your existing session does the work.

- **Multi-source by design.** YAVCHN is built around a small Go interface called `Source` (see `src/source.go`). HN and Lobsters each have their own implementation that knows how to talk to their respective JSON APIs. Adding a third site means writing one more implementation. The rest of YAVCHN doesn't care which site a story came from.

- **Caching.** Story lists, individual items, and comment threads are cached in memory with a short time-to-live (a minute or two), so the front page doesn't hammer HN or Lobsters when many people are reading. Article extraction is much more expensive (fetch the source page, run readability, sanitize the HTML), so extracted articles are cached durably in SQLite and only re-fetched after 30 days.

- **Progressive enhancement.** The page renders fully server-side, so it works without JavaScript. The article reader-mode pane and the comment thread are fetched separately after the page loads, via `GET /api/article` and `GET /api/discussion`. That keeps the first paint fast, and it lets the heavier requests fail without breaking the page. Visitors with JavaScript disabled see plainly-labeled "Open article" and "Open on HN" / "Open on Lobsters" fallback links instead.

- **Browser-local state.** Splitter positions, theme choice, focus mode, pinned stories, dismissed stories, visited stories, collapsed comment threads, the comment-sort preference, and the domain block-list all live in your browser's `localStorage`. Nothing is sent to the server. A small inline `<script>` in the page `<head>` applies your theme, splitter sizes, and focus mode before the first paint, so reloading doesn't flash the default layout for a moment.

- **Search.** Search runs against HN only, at `/hn/search`. Lobsters has a search page, but it returns HTML rather than JSON, so instead of scraping that HTML and watching it break every time Lobsters tweaks its markup, YAVCHN returns 404 for `/lobsters/search`.

## Docker

```
docker build -t yavchn .
docker run --rm -p 8080:8080 yavchn
```

YAVCHN is a single-binary distroless image. The SQLite article cache is in `/home/nonroot/yavchn.db` inside the container and rebuilds from scratch after a container replace.

## License

[MIT](LICENSE). Feel free to use it, fork it, embed it, learn from it, whatever. Just keep the copyright notice intact.
