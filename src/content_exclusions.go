package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const contentExclusionSchema = `
CREATE TABLE IF NOT EXISTS content_exclusions (
 source TEXT NOT NULL, kind TEXT NOT NULL, value TEXT NOT NULL,
 added_at INTEGER NOT NULL, PRIMARY KEY(source,kind,value)
);`

var errContentRemoved = errors.New("content unavailable at the contributor's request")
var errSourceDocument = errors.New("source pages must use the source reader")

func checkSourceDocument(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")) {
	case "news.ycombinator.com", "hacker-news.firebaseio.com", "hn.algolia.com",
		"yavchn.com", "www.yavchn.com", "beta.yavchn.com", "yavchn.parkscomputing.com":
		return errSourceDocument
	}
	return nil
}

// Exclusions are an immutable startup snapshot. Operator changes take effect
// after a restart, which also discards the process's upstream content caches.
type contentExclusions struct {
	authors map[string]bool
	items   map[string]bool
}

func loadContentExclusions(ctx context.Context, db *sql.DB) (*contentExclusions, error) {
	p := &contentExclusions{authors: map[string]bool{}, items: map[string]bool{}}
	rows, err := db.QueryContext(ctx, `SELECT kind,value FROM content_exclusions WHERE source='hn'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, err
		}
		if kind == "author" {
			p.authors[value] = true
		} else if kind == "item" {
			p.items[value] = true
		}
	}
	return p, rows.Err()
}

func (p *contentExclusions) author(name string) bool       { return p != nil && p.authors[name] }
func (p *contentExclusions) item(id string) bool           { return p != nil && p.items[id] }
func (p *contentExclusions) blocks(id, author string) bool { return p.item(id) || p.author(author) }

// Copy retained nodes so filtering never mutates a cached thread. Replies by
// other contributors survive in their original order when a parent is omitted.
func (p *contentExclusions) comments(comments []*Comment) []*Comment {
	var out []*Comment
	for _, c := range comments {
		children := p.comments(c.Children)
		if p.blocks(c.ID, c.Author) {
			out = append(out, children...)
			continue
		}
		shown := *c
		shown.Children = children
		out = append(out, &shown)
	}
	return out
}

func contentGone(w http.ResponseWriter) {
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "This content is no longer available.", http.StatusGone)
}

// Only HN is supported until another source implements these same gates.
func runContentTakedown(ctx context.Context, db *sql.DB, args []string, out io.Writer) error {
	usage := errors.New("usage: yavchn takedown author|item add|remove hn <value>, or author|item list; restart the service after changes")
	if len(args) == 4 && args[0] == "author" && args[1] == "audit" && args[2] == "hn" && userNameRE.MatchString(args[3]) {
		var articles, accounts int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM articles WHERE instr(lower(title || ' ' || byline || ' ' || content),lower(?))>0`, args[3]).Scan(&articles); err != nil {
			return err
		}
		// Account tables are optional when only the standalone cache is used.
		var present int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='account_data'`).Scan(&present); err != nil {
			return err
		}
		if present > 0 {
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM account_data WHERE instr(lower(document),lower(?))>0`, args[3]).Scan(&accounts); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "Article cache rows mentioning identifier: %d\nPrivate account documents mentioning identifier: %d\nThis text audit does not detect unattributed quotations or offline browser copies.\n", articles, accounts)
		return nil
	}
	if len(args) == 2 && args[1] == "list" {
		rows, err := db.QueryContext(ctx, `SELECT source,value FROM content_exclusions WHERE kind=? ORDER BY source,value`, args[0])
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var source, value string
			if err := rows.Scan(&source, &value); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s %s\n", source, value)
		}
		return rows.Err()
	}
	if len(args) != 4 || args[2] != "hn" || (args[1] != "add" && args[1] != "remove") {
		return usage
	}
	value := args[3]
	if args[0] == "author" && !userNameRE.MatchString(value) {
		return usage
	}
	if args[0] == "item" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
			return usage
		}
	}
	var err error
	if args[1] == "add" {
		_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO content_exclusions(source,kind,value,added_at) VALUES(?,?,?,?)`, args[2], args[0], value, time.Now().Unix())
	} else {
		_, err = db.ExecContext(ctx, `DELETE FROM content_exclusions WHERE source=? AND kind=? AND value=?`, args[2], args[0], value)
	}
	if err == nil {
		fmt.Fprintln(out, "Exclusion updated. Restart the service to apply it and clear upstream memory caches.")
	}
	return err
}
