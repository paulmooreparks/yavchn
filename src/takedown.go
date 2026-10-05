package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
)

// The reader's exclusions: articles whose publishers asked YAVCHN not to
// show a readable copy, as the terms of use promise. An exclusion is one
// article's address or a whole site, a host that also covers its
// subdomains. The reader neither fetches nor serves an excluded article,
// and adding an exclusion deletes the copies already cached. The story and
// its discussion stay, with a link to the article on its own site.
//
// Exclusions are managed with the server's own binary, which in the
// container is run with docker exec:
//
//	docker exec yavchn /yavchn takedown add https://example.com/post
//	docker exec yavchn /yavchn takedown add example.com
//	docker exec yavchn /yavchn takedown remove example.com
//	docker exec yavchn /yavchn takedown list
//
// The list keeps only what is excluded and when, so it holds nothing about
// the person who asked.

const takedownSchema = `
CREATE TABLE IF NOT EXISTS reader_exclusions (
  pattern  TEXT PRIMARY KEY,
  added_at INTEGER NOT NULL
);
`

var errExcluded = errors.New("excluded at the publisher's request")

// exclusionPattern turns an article's address or a site's host into the
// key it is stored under: "url:" and the address without its scheme or
// fragment, or "host:" and the host without a leading www.
func exclusionPattern(target string) (string, error) {
	target = strings.TrimSpace(target)
	if !strings.Contains(target, "://") {
		host := strings.ToLower(strings.TrimSuffix(target, "."))
		host = strings.TrimPrefix(host, "www.")
		if host == "" || strings.ContainsAny(host, "/?#@: ") {
			return "", fmt.Errorf("%q is neither an address nor a host", target)
		}
		return "host:" + host, nil
	}
	key, err := articleAddress(target)
	if err != nil {
		return "", err
	}
	return "url:" + key, nil
}

// articleAddress is an address as exclusions compare it: http and https
// alike, the host in lower case and without www., no fragment, and no
// trailing slash on the path.
func articleAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not an http or https address", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	key := host + strings.TrimSuffix(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key, nil
}

// exclusionKeys are the patterns that would exclude an article: its own
// address, and its host and every domain above it.
func exclusionKeys(raw string) []string {
	key, err := articleAddress(raw)
	if err != nil {
		return nil
	}
	u, _ := url.Parse(raw)
	keys := []string{"url:" + key}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for host != "" {
		keys = append(keys, "host:"+host)
		_, rest, found := strings.Cut(host, ".")
		if !found {
			break
		}
		host = rest
	}
	return keys
}

func excluded(ctx context.Context, db *sql.DB, raw string) (bool, error) {
	keys := exclusionKeys(raw)
	if len(keys) == 0 {
		return false, nil
	}
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM reader_exclusions WHERE pattern IN (?`+strings.Repeat(",?", len(keys)-1)+`)`, args...).Scan(&n)
	return n > 0, err
}

// addExclusion records the pattern and deletes the cached copies it
// covers, returning how many it deleted.
func addExclusion(ctx context.Context, db *sql.DB, pattern string) (int, error) {
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO reader_exclusions(pattern,added_at)VALUES(?,?)`, pattern, time.Now().Unix()); err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT url_hash,url FROM articles`)
	if err != nil {
		return 0, err
	}
	var doomed []string
	for rows.Next() {
		var hash, raw string
		if err := rows.Scan(&hash, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		for _, k := range exclusionKeys(raw) {
			if k == pattern {
				doomed = append(doomed, hash)
				break
			}
		}
	}
	if err := closeRows(rows); err != nil {
		return 0, err
	}
	for _, hash := range doomed {
		if _, err := db.ExecContext(ctx, `DELETE FROM articles WHERE url_hash=?`, hash); err != nil {
			return 0, err
		}
	}
	return len(doomed), nil
}

// runTakedown is the takedown command, given the arguments after its name.
func runTakedown(ctx context.Context, db *sql.DB, args []string, out io.Writer) error {
	usage := errors.New("usage: yavchn takedown add|remove <address or host>, or yavchn takedown list")
	if len(args) == 0 {
		return usage
	}
	switch {
	case args[0] == "list" && len(args) == 1:
		rows, err := db.QueryContext(ctx, `SELECT pattern,added_at FROM reader_exclusions ORDER BY added_at,pattern`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var pattern string
			var added int64
			if err := rows.Scan(&pattern, &added); err != nil {
				rows.Close()
				return err
			}
			kind, value, _ := strings.Cut(pattern, ":")
			if kind == "host" {
				value += " and its subdomains"
			}
			fmt.Fprintf(out, "%s  %s\n", time.Unix(added, 0).UTC().Format("2006-01-02"), value)
		}
		return closeRows(rows)
	case (args[0] == "add" || args[0] == "remove") && len(args) == 2:
		pattern, err := exclusionPattern(args[1])
		if err != nil {
			return err
		}
		_, value, _ := strings.Cut(pattern, ":")
		if args[0] == "remove" {
			res, err := db.ExecContext(ctx, `DELETE FROM reader_exclusions WHERE pattern=?`, pattern)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("%s is not excluded", value)
			}
			fmt.Fprintf(out, "The reader shows %s again.\n", value)
			return nil
		}
		n, err := addExclusion(ctx, db, pattern)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "The reader no longer shows %s. Deleted %d cached %s.\n", value, n, plural(n, "copy", "copies"))
		return nil
	}
	return usage
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// takedownMain runs the takedown command against the server's database
// and exits.
func takedownMain(args []string) {
	ctx := context.Background()
	path := os.Getenv("YAVCHN_DB_PATH")
	if path == "" {
		path = "./yavchn.db"
	}
	db, err := OpenDB(ctx, path)
	if err == nil {
		err = runTakedown(ctx, db, args, os.Stdout)
		db.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
