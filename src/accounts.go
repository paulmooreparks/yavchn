package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"
)

const accountSchema = `
CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS account_identities (
 provider TEXT NOT NULL, subject TEXT NOT NULL,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 username TEXT NOT NULL, PRIMARY KEY(provider, subject)
);
CREATE TABLE IF NOT EXISTS account_sessions (
 token_hash TEXT PRIMARY KEY,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 csrf TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL,
 agent TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS account_sessions_owner ON account_sessions(account_id);
CREATE TABLE IF NOT EXISTS account_login_flows (
 state_hash TEXT PRIMARY KEY, verifier TEXT NOT NULL,
 return_to TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS account_data (
 account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL DEFAULT 0,
 document TEXT NOT NULL DEFAULT '{"pins":{},"domains":[],"progress":{}}'
);
CREATE TABLE IF NOT EXISTS account_avatars (
 account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 content_type TEXT NOT NULL, image BLOB NOT NULL, version TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS account_email_links (
 token_hash TEXT PRIMARY KEY, email TEXT NOT NULL, link_account TEXT NOT NULL,
 return_to TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS account_email_links_address ON account_email_links(email, link_account);
CREATE TABLE IF NOT EXISTS account_email_budget (
 bucket TEXT PRIMARY KEY, count INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS account_passkeys (
 credential_id TEXT PRIMARY KEY,
 account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 name TEXT NOT NULL, credential TEXT NOT NULL, created_at INTEGER NOT NULL, used_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS account_passkeys_owner ON account_passkeys(account_id);
CREATE TABLE IF NOT EXISTS account_ceremonies (
 token_hash TEXT PRIMARY KEY, kind TEXT NOT NULL, account_id TEXT NOT NULL,
 session TEXT NOT NULL, return_to TEXT NOT NULL, expires_at INTEGER NOT NULL
);`

// accountColumns are columns added after their tables first shipped, which
// CREATE TABLE IF NOT EXISTS does not add to a database that has the table.
var accountColumns = [][3]string{
	// The picture an account shows: "github" mirrors its GitHub picture,
	// "upload" is one the reader chose, and "none" is the placeholder.
	{"accounts", "picture", `TEXT NOT NULL DEFAULT 'github'`},
	{"account_identities", "avatar_url", `TEXT NOT NULL DEFAULT ''`},
	// A GitHub sign-in started from inside an account links to it.
	{"account_login_flows", "link_account", `TEXT NOT NULL DEFAULT ''`},
}

func migrateAccounts(db *sql.DB) error {
	if _, err := db.Exec(accountSchema); err != nil {
		return err
	}
	for _, c := range accountColumns {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, c[0], c[1]).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + c[0] + ` ADD COLUMN ` + c[1] + ` ` + c[2]); err != nil {
				return err
			}
		}
	}
	// Sessions once recorded the browser's user agent, which nothing read.
	_, err := db.Exec(`UPDATE account_sessions SET agent='' WHERE agent<>''`)
	return err
}

// sweepInterval is how often expired sign-in records are deleted. The
// privacy policy says they go within an hour of expiring.
const sweepInterval = 15 * time.Minute

// StartSweep deletes expired sessions, sign-in flows, email links, passkey
// ceremonies and email budgets now and every sweepInterval, so none
// outlives its purpose, including one left by an account since deleted.
func (a *accountService) StartSweep(ctx context.Context) {
	go func() {
		a.sweep(ctx)
		t := time.NewTicker(sweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.sweep(ctx)
			}
		}
	}()
}

func (a *accountService) sweep(ctx context.Context) {
	now := time.Now().Unix()
	for _, table := range []string{"account_sessions", "account_login_flows", "account_email_links", "account_ceremonies", "account_email_budget"} {
		if _, err := a.server.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE expires_at<=?`, now); err != nil && ctx.Err() == nil {
			slog.Warn("account sweep", "table", table, "err", err)
		}
	}
}

// avatarLimit bounds the copy of a GitHub profile picture, requested at 96 pixels.
const avatarLimit = 256 * 1024

const sessionCookie = "__Host-yavchn-session"
const flowCookie = "__Host-yavchn-flow"
const accountBodyLimit = 4 * 1024 * 1024

type accountConfig struct {
	Origin, ClientID, ClientSecret string
	// Email sign-in links go through Resend; both are needed to offer them.
	ResendKey, EmailFrom string
	EmailReplyTo         string // optional; where replies to sign-in mail go
}

// GitHub, Email and Passkeys say which ways of signing in this deployment offers.
func (c accountConfig) GitHub() bool   { return c.ClientID != "" }
func (c accountConfig) Email() bool    { return c.ResendKey != "" && c.EmailFrom != "" && c.Origin != "" }
func (c accountConfig) Passkeys() bool { return c.Origin != "" && (c.GitHub() || c.Email()) }
func (c accountConfig) Enabled() bool  { return c.GitHub() || c.Email() }

// secretFrom reads a secret from its variable or from the file another names, not both.
func secretFrom(name string) (string, error) {
	v := os.Getenv(name)
	if path := os.Getenv(name + "_FILE"); path != "" {
		if v != "" {
			return "", fmt.Errorf("configure %s or %s_FILE, not both", name, name)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", name, err)
		}
		v = strings.TrimSpace(string(b))
	}
	return v, nil
}

func loadAccountConfig() (accountConfig, error) {
	c := accountConfig{Origin: os.Getenv("YAVCHN_PUBLIC_ORIGIN"), ClientID: os.Getenv("YAVCHN_GITHUB_CLIENT_ID"), EmailFrom: strings.TrimSpace(os.Getenv("YAVCHN_EMAIL_FROM")), EmailReplyTo: strings.TrimSpace(os.Getenv("YAVCHN_EMAIL_REPLY_TO"))}
	var err error
	if c.ClientSecret, err = secretFrom("YAVCHN_GITHUB_CLIENT_SECRET"); err != nil {
		return c, err
	}
	if c.ResendKey, err = secretFrom("YAVCHN_RESEND_API_KEY"); err != nil {
		return c, err
	}
	if (c.ClientID == "") != (c.ClientSecret == "") {
		return c, errors.New("GitHub client ID and secret must both be configured")
	}
	if (c.ResendKey == "") != (c.EmailFrom == "") {
		return c, errors.New("YAVCHN_RESEND_API_KEY and YAVCHN_EMAIL_FROM must both be configured")
	}
	if c.EmailFrom != "" {
		if _, err := mail.ParseAddress(c.EmailFrom); err != nil {
			return c, errors.New("YAVCHN_EMAIL_FROM must be an email address")
		}
	}
	if c.EmailReplyTo != "" {
		if _, err := mail.ParseAddress(c.EmailReplyTo); err != nil {
			return c, errors.New("YAVCHN_EMAIL_REPLY_TO must be an email address")
		}
	}
	if c.Origin == "" && (c.ClientID != "" || c.ResendKey != "") {
		return c, errors.New("YAVCHN_PUBLIC_ORIGIN is required for sign-in")
	}
	if c.Origin != "" {
		u, err := url.Parse(c.Origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return c, errors.New("public origin must be an HTTPS origin")
		}
		c.Origin = strings.TrimSuffix(c.Origin, "/")
	}
	return c, nil
}

type pinEntry struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	By       string `json:"by"`
	Score    int    `json:"score"`
	Comments int    `json:"comments"`
	PinnedAt int64  `json:"pinned_at"`
}
type collectionEntry struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

// collectedEntry is one story in one collection, keyed "{collection}:{source}-{id}".
type collectedEntry struct {
	Source   string `json:"source"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Host     string `json:"host"`
	By       string `json:"by"`
	Score    int    `json:"score"`
	Comments int    `json:"comments"`
	AddedAt  int64  `json:"added_at"`
}

// noteEntry is the reader's note on one story, keyed "{source}-{id}".
type noteEntry struct {
	Source    string `json:"source"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Host      string `json:"host"`
	By        string `json:"by"`
	Score     int    `json:"score"`
	Comments  int    `json:"comments"`
	Text      string `json:"text"`
	UpdatedAt int64  `json:"updated_at"`
}

type accountDocument struct {
	Pins        map[string]pinEntry        `json:"pins"`
	Domains     []string                   `json:"domains"`
	Progress    map[string]json.RawMessage `json:"progress"`
	Collections map[string]collectionEntry `json:"collections"`
	Collected   map[string]collectedEntry  `json:"collected"`
	Notes       map[string]noteEntry       `json:"notes"`
}

func emptyAccountDocument() accountDocument {
	d := accountDocument{Pins: map[string]pinEntry{}, Domains: []string{}, Progress: map[string]json.RawMessage{}}
	d.normalize()
	return d
}

// normalize gives documents stored before collections and notes their empty members.
func (d *accountDocument) normalize() {
	if d.Collections == nil {
		d.Collections = map[string]collectionEntry{}
	}
	if d.Collected == nil {
		d.Collected = map[string]collectedEntry{}
	}
	if d.Notes == nil {
		d.Notes = map[string]noteEntry{}
	}
}

var accountEntryKey = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,100}$`)
var accountDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?\.[a-z]{2,63}$`)
var collectionID = regexp.MustCompile(`^[a-z0-9]{6,24}$`)
var storyID = regexp.MustCompile(`^[a-zA-Z0-9]{1,40}$`)

func validAccountEntryKey(key string) bool {
	return accountEntryKey.MatchString(key) && key != "__proto__" && key != "constructor" && key != "prototype"
}

// validStory checks the copy of a story's listing that saved entries keep.
func validStory(source, title, link, host, by string, score, comments int) bool {
	if (source != "hn" && source != "lobsters") || len(title) > 4096 || len(link) > 8192 || len(host) > 253 || len(by) > 100 || score < 0 || comments < 0 {
		return false
	}
	if link != "" {
		u, err := url.Parse(link)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return false
		}
	}
	return true
}

func (d accountDocument) validate() error {
	if d.Pins == nil || d.Domains == nil || d.Progress == nil || len(d.Pins) > 500 || len(d.Domains) > 500 || len(d.Progress) > 100 {
		return errors.New("invalid account data size")
	}
	if d.Collections == nil || d.Collected == nil || d.Notes == nil || len(d.Collections) > 100 || len(d.Collected) > 2000 || len(d.Notes) > 1000 {
		return errors.New("invalid account data size")
	}
	for k, p := range d.Pins {
		if !validAccountEntryKey(k) || !validStory(p.Source, p.Title, p.URL, p.Host, p.By, p.Score, p.Comments) || p.PinnedAt < 0 {
			return errors.New("invalid pinned story")
		}
	}
	for k, c := range d.Collections {
		name := strings.TrimSpace(c.Name)
		if !collectionID.MatchString(k) || name == "" || name != c.Name || utf8.RuneCountInString(name) > 100 || c.CreatedAt < 0 {
			return errors.New("invalid collection")
		}
	}
	for k, e := range d.Collected {
		collection, story, ok := strings.Cut(k, ":")
		if _, exists := d.Collections[collection]; !ok || !exists || !storyID.MatchString(e.ID) || story != e.Source+"-"+e.ID ||
			!validStory(e.Source, e.Title, e.URL, e.Host, e.By, e.Score, e.Comments) || e.AddedAt < 0 {
			return errors.New("invalid collection entry")
		}
	}
	for k, n := range d.Notes {
		if !storyID.MatchString(n.ID) || k != n.Source+"-"+n.ID || !validStory(n.Source, n.Title, n.URL, n.Host, n.By, n.Score, n.Comments) ||
			strings.TrimSpace(n.Text) == "" || !utf8.ValidString(n.Text) || utf8.RuneCountInString(n.Text) > 10000 || n.UpdatedAt < 0 {
			return errors.New("invalid note")
		}
	}
	seen := map[string]bool{}
	for _, v := range d.Domains {
		if !accountDomain.MatchString(v) || strings.Contains(v, "..") || seen[v] {
			return errors.New("invalid blocked domain")
		}
		seen[v] = true
	}
	for k, v := range d.Progress {
		if !validAccountEntryKey(k) || len(v) > 65536 || !json.Valid(v) || len(v) == 0 || v[0] != '{' {
			return errors.New("invalid reading state")
		}
	}
	return nil
}

type accountSession struct {
	ID        string                 `json:"id"`
	Username  string                 `json:"username"`
	CSRF      string                 `json:"-"`
	TokenHash string                 `json:"-"`
	CreatedAt int64                  `json:"-"`
	AvatarURL string                 `json:"-"` // this site's copy of the GitHub picture, or empty
	Revision  int64                  `json:"revision"`
	Data      accountDocument        `json:"data"`
	Links     map[string]accountLink `json:"links"`
}

type accountLink struct {
	Href   string `json:"href"`
	Method string `json:"method"`
}

func (u *accountSession) setLinks() {
	u.Links = map[string]accountLink{
		"self":    {Href: "/account/data", Method: "GET"},
		"replace": {Href: "/account/data", Method: "PUT"},
		"account": {Href: "/account", Method: "GET"},
		"export":  {Href: "/account/export", Method: "GET"},
		"signout": {Href: "/account/session", Method: "POST"},
	}
	if time.Now().Unix()-u.CreatedAt < 600 {
		u.Links["delete"] = accountLink{Href: "/account", Method: "POST"}
	}
}

type accountContextKey struct{}
type accountReturnContextKey struct{}
type accountSignInContextKey struct{}

type accountElsewhereContextKey struct{}

// signInAvailable reports whether this deployment offers sign-in on the
// hostname the request came to.
func signInAvailable(r *http.Request) bool {
	on, _ := r.Context().Value(accountSignInContextKey{}).(bool)
	return on
}

// accountElsewhere is the account page on the public origin, for a request
// that came to another hostname the site answers to; otherwise it is empty.
func accountElsewhere(r *http.Request) string {
	u, _ := r.Context().Value(accountElsewhereContextKey{}).(string)
	return u
}

// onOrigin reports whether a request came to the public origin's hostname.
func (a *accountService) onOrigin(r *http.Request) bool {
	origin, err := url.Parse(a.config.Origin)
	return a.config.Origin == "" || (err == nil && strings.EqualFold(r.Host, origin.Host))
}

func currentAccount(r *http.Request) *accountSession {
	a, _ := r.Context().Value(accountContextKey{}).(*accountSession)
	return a
}

type accountService struct {
	server *Server
	config accountConfig
	client *http.Client
	rate   *rateLimiter
	writes *rateLimiter
	// webauthn runs passkey ceremonies when the deployment offers them.
	webauthn *webauthn.WebAuthn
}

func newAccountService(s *Server, cfg accountConfig) (*accountService, error) {
	if err := migrateAccounts(s.db); err != nil {
		return nil, err
	}
	a := &accountService{server: s, config: cfg, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, rate: newRateLimiter(10, 5*time.Minute), writes: newRateLimiter(120, time.Minute)}
	if cfg.Passkeys() {
		if err := a.configurePasskeys(); err != nil {
			return nil, err
		}
	}
	return a, nil
}
func accountToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func tokenHash(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func sameToken(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func accountCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func accountError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, code)
}

func (a *accountService) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /account", a.page)
	mux.HandleFunc("POST /auth/github", a.start)
	mux.HandleFunc("GET /auth/github/callback", a.callback)
	mux.HandleFunc("POST /account/session", a.logout)
	mux.HandleFunc("POST /account/sessions", a.revokeOthers)
	mux.HandleFunc("POST /account", a.remove)
	mux.HandleFunc("GET /account/export", a.export)
	mux.HandleFunc("GET /account/data", a.getData)
	mux.HandleFunc("PUT /account/data", a.putData)
	mux.HandleFunc("GET /account/avatar", a.avatar)
	mux.HandleFunc("POST /account/avatar", a.uploadPicture)
	mux.HandleFunc("POST /account/avatar/remove", a.removePicture)
	mux.HandleFunc("POST /account/avatar/github", a.githubPicture)
	mux.HandleFunc("POST /auth/email", a.requestEmail)
	mux.HandleFunc("GET /auth/email/link", a.emailLink)
	mux.HandleFunc("POST /auth/email/link", a.redeemEmail)
	mux.HandleFunc("POST /auth/passkey/options", a.passkeyOptions)
	mux.HandleFunc("POST /auth/passkey", a.passkeySignIn)
	mux.HandleFunc("POST /account/passkeys/options", a.passkeyCreationOptions)
	mux.HandleFunc("POST /account/passkeys", a.passkeyRegister)
	mux.HandleFunc("POST /account/passkeys/remove", a.removePasskey)
	mux.HandleFunc("POST /account/identities/remove", a.removeIdentity)
}

// avatar serves the signed-in reader their own picture. Its address
// carries the picture's version, so a private cache may keep it.
func (a *accountService) avatar(w http.ResponseWriter, r *http.Request) {
	u := currentAccount(r)
	if u == nil {
		http.NotFound(w, r)
		return
	}
	var kind, version string
	var image []byte
	err := a.server.db.QueryRowContext(r.Context(), `SELECT content_type,image,version FROM account_avatars WHERE account_id=?`, u.ID).Scan(&kind, &image, &version)
	if err != nil || r.URL.Query().Get("v") != version {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(image)
}

// refreshAvatar copies the reader's GitHub picture at sign-in, so pages
// show it from this site and never send the reader's browser to GitHub.
// A failure keeps the previous copy, or the placeholder, and never stops
// the sign-in.
func (a *accountService) refreshAvatar(ctx context.Context, accountID, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "avatars.githubusercontent.com" || u.User != nil {
		return
	}
	q := u.Query()
	q.Set("s", "96")
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "YAVCHN")
	res, err := a.client.Do(req)
	if err != nil {
		slog.Warn("GitHub avatar fetch failed")
		return
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, avatarLimit+1))
	if res.StatusCode != http.StatusOK || err != nil || len(b) == 0 || len(b) > avatarLimit {
		slog.Warn("GitHub avatar unusable", "status", res.StatusCode, "bytes", len(b))
		return
	}
	kind := http.DetectContentType(b)
	if kind != "image/png" && kind != "image/jpeg" && kind != "image/gif" && kind != "image/webp" {
		slog.Warn("GitHub avatar has an unexpected type", "type", kind)
		return
	}
	sum := sha256.Sum256(b)
	_, err = a.server.db.ExecContext(ctx, `INSERT INTO account_avatars(account_id,content_type,image,version)VALUES(?,?,?,?) ON CONFLICT(account_id) DO UPDATE SET content_type=excluded.content_type,image=excluded.image,version=excluded.version`,
		accountID, kind, b, hex.EncodeToString(sum[:8]))
	if err != nil {
		slog.Warn("GitHub avatar could not be stored")
	}
}
func (a *accountService) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		accountUI := r.URL.Path == "/account" || r.URL.Path == "/window/account" || slices.Contains(strings.Split(r.URL.Query().Get("open"), ","), "account")
		private := accountUI || strings.HasPrefix(r.URL.Path, "/account") || strings.HasPrefix(r.URL.Path, "/auth/")
		if private {
			w.Header().Set("Cache-Control", "no-store")
			// A no-referrer policy can make a form POST's Origin null.
			// Suppress callback URL leakage without breaking account form checks.
			// The email link's page keeps the default policy: its form must post
			// with a real Origin, and other sites see only the origin anyway.
			if r.URL.Path == "/auth/github/callback" {
				w.Header().Set("Referrer-Policy", "no-referrer")
			} else {
				w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			}
			if a.config.Origin != "" {
				origin, _ := url.Parse(a.config.Origin)
				if !strings.EqualFold(r.Host, origin.Host) {
					// Sessions and passkeys belong to the public origin alone, so
					// another hostname the site answers to, such as www or the old
					// domain, sends a reader looking at their account there.
					if r.Method == "GET" || r.Method == "HEAD" {
						http.Redirect(w, r, a.config.Origin+r.URL.RequestURI(), http.StatusFound)
						return
					}
					accountError(w, 421, "Use "+a.config.Origin+"/account for your account.")
					return
				}
			}
		}
		if c, err := r.Cookie(sessionCookie); err == nil && len(c.Value) == 43 {
			user := &accountSession{}
			var avatar string
			err = a.server.db.QueryRowContext(r.Context(), `SELECT a.id,COALESCE((SELECT username FROM account_identities WHERE account_id=a.id ORDER BY provider<>'github',username LIMIT 1),'your account'),s.csrf,s.token_hash,s.created_at,d.revision,d.document,COALESCE(v.version,'') FROM account_sessions s JOIN accounts a ON a.id=s.account_id JOIN account_data d ON d.account_id=a.id LEFT JOIN account_avatars v ON v.account_id=a.id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(c.Value), time.Now().Unix()).Scan(&user.ID, &user.Username, &user.CSRF, &user.TokenHash, &user.CreatedAt, &user.Revision, newAccountScan(&user.Data), &avatar)
			if err == nil {
				if avatar != "" {
					user.AvatarURL = "/account/avatar?v=" + avatar
				}
				user.setLinks()
				r = r.WithContext(context.WithValue(r.Context(), accountContextKey{}, user))
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Add("Vary", "Cookie")
			} else if err != sql.ErrNoRows {
				slog.Error("account session lookup failed")
				accountError(w, 503, "Account storage is unavailable. Please try again.")
				return
			}
		}
		if a.config.Enabled() {
			if a.onOrigin(r) {
				r = r.WithContext(context.WithValue(r.Context(), accountSignInContextKey{}, true))
			} else {
				r = r.WithContext(context.WithValue(r.Context(), accountElsewhereContextKey{}, a.config.Origin+"/account"))
			}
		}
		next.ServeHTTP(w, r)
	})
}

type accountScanner struct{ data *accountDocument }

func newAccountScan(d *accountDocument) *accountScanner { return &accountScanner{data: d} }
func (s *accountScanner) Scan(v any) error {
	var err error
	switch x := v.(type) {
	case string:
		err = json.Unmarshal([]byte(x), s.data)
	case []byte:
		err = json.Unmarshal(x, s.data)
	default:
		return errors.New("invalid account document")
	}
	s.data.normalize()
	return err
}

type accountPanelVM struct {
	Account   *accountSession
	Message   accountMessage // what the last action reported, if anything
	ReturnURL string
	// The ways of signing in this deployment offers.
	LoginAvailable, EmailAvailable, PasskeysAvailable bool
	OtherSessions                                     int
	RecentLogin                                       bool
	// The signed-in account's ways in and picture (signin.go).
	Identities    []panelIdentity
	Passkeys      []panelPasskey
	HasGitHub     bool
	GitHubPicture bool   // a GitHub picture the reader can return to
	Email         string // an address of the account's, for signing in again
	Picture       string // "github", "upload" or "none"
}

// panel supplies the same account content to the Classic page and PUDL window.
func (a *accountService) panel(r *http.Request) (*accountPanelVM, error) {
	vm := &accountPanelVM{Account: currentAccount(r), LoginAvailable: a.config.GitHub(), EmailAvailable: a.config.Email(), PasskeysAvailable: a.webauthn != nil, ReturnURL: "/account"}
	if r.URL.Path != "/account" {
		vm.ReturnURL = "/hn/?view=window&open=account&top=account"
		if target, ok := r.Context().Value(accountReturnContextKey{}).(string); ok {
			vm.ReturnURL = target
		}
	}
	vm.Message = accountMessages[r.URL.Query().Get("message")]
	if user := currentAccount(r); user != nil {
		if err := a.server.db.QueryRowContext(r.Context(), `SELECT count(*) FROM account_sessions WHERE account_id=? AND token_hash<>? AND expires_at>?`, user.ID, user.TokenHash, time.Now().Unix()).Scan(&vm.OtherSessions); err != nil {
			return nil, err
		}
		if err := a.signInMethods(r.Context(), vm); err != nil {
			return nil, err
		}
		vm.RecentLogin = time.Now().Unix()-user.CreatedAt < 600
	}
	return vm, nil
}

// NoticeAt is the notice the panel shows at a place, or nil.
func (vm *accountPanelVM) NoticeAt(place string) *accountMessage {
	if vm.Message.Text == "" || vm.Message.Toast || vm.Message.Place != place {
		return nil
	}
	return &vm.Message
}

// Toast is the passing confirmation the panel raises, or nil.
func (vm *accountPanelVM) Toast() *accountMessage {
	if !vm.Message.Toast {
		return nil
	}
	return &vm.Message
}
func (a *accountService) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "window" {
		target := url.Values{"view": {"window"}, "open": {"account"}, "top": {"account"}}
		if message := r.URL.Query().Get("message"); accountMessages[message].Text != "" {
			target.Set("message", message)
		}
		http.Redirect(w, r, "/hn/?"+target.Encode(), http.StatusSeeOther)
		return
	}
	win, ok := a.server.appletWindow(r.Context(), r, "account")
	if !ok {
		accountError(w, 503, "Account storage is unavailable.")
		return
	}
	a.server.renderAppPage(w, r, win)
}

// Account returns are limited to the account page or a known Windowed workspace.
func accountReturn(r *http.Request, message string) string {
	target := "/account"
	raw := r.PostForm.Get("return_to")
	u, err := url.Parse(raw)
	if err == nil && len(raw) <= 4096 && strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.Contains(raw, "\\") && u.Host == "" && u.Scheme == "" && u.User == nil {
		q := u.Query()
		workspace := u.Path == "/hn/" || u.Path == "/lobsters/" || u.Path == "/pinned/" || u.Path == "/collections/" || u.Path == "/notes/" || u.Path == "/find"
		// A Windowed workspace's address often names no view, which then comes
		// from the reader's saved preference; one with the Account window open
		// is Windowed by definition, so the return says so.
		windowed := workspace && q.Get("view") != "classic" && slices.Contains(strings.Split(q.Get("open"), ","), "account")
		if u.Path == "/account" || windowed {
			if windowed {
				q.Set("view", "window")
			}
			u.Fragment = ""
			q.Del("message")
			u.RawQuery = q.Encode()
			target = u.String()
		}
	}
	if message != "" {
		target = withMessage(target, message)
	}
	return target
}
func (a *accountService) sameOrigin(r *http.Request) bool {
	return a.config.Origin != "" && r.Header.Get("Origin") == a.config.Origin
}
func (a *accountService) start(w http.ResponseWriter, r *http.Request) {
	if a.config.ClientID == "" {
		accountError(w, 503, "GitHub sign-in is not configured yet.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !a.sameOrigin(r) || r.ParseForm() != nil {
		accountError(w, 403, "Invalid sign-in request.")
		return
	}
	// Linking attaches the GitHub user to the account that asked for it.
	link, stale := linkTarget(r)
	if stale != "" {
		http.Redirect(w, r, accountReturn(r, stale), http.StatusSeeOther)
		return
	}
	if !a.rate.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "300")
		accountError(w, 429, "Too many sign-in attempts. Please try again later.")
		return
	}
	state, err := accountToken()
	if err != nil {
		accountError(w, 500, "Cannot start sign-in.")
		return
	}
	verifier, err := accountToken()
	if err != nil {
		accountError(w, 500, "Cannot start sign-in.")
		return
	}
	// The return destination is limited to account views on this site.
	_, err = a.server.db.ExecContext(r.Context(), `DELETE FROM account_login_flows WHERE expires_at<=?`, time.Now().Unix())
	if err == nil {
		_, err = a.server.db.ExecContext(r.Context(), `INSERT INTO account_login_flows(state_hash,verifier,return_to,expires_at,link_account)VALUES(?,?,?,?,?)`, tokenHash(state), verifier, accountReturn(r, ""), time.Now().Add(10*time.Minute).Unix(), link)
	}
	if err != nil {
		accountError(w, 503, "Cannot start sign-in right now.")
		return
	}
	accountCookie(w, flowCookie, state, 600)
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {a.config.ClientID}, "redirect_uri": {a.config.Origin + "/auth/github/callback"}, "scope": {""}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "prompt": {"select_account"}}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusSeeOther)
}
func (a *accountService) callback(w http.ResponseWriter, r *http.Request) {
	if a.config.ClientID == "" {
		accountError(w, 503, "GitHub sign-in is not configured yet.")
		return
	}
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(flowCookie)
	if err != nil || len(state) != 43 || !sameToken(cookie.Value, state) {
		accountError(w, 400, "Sign-in did not match this browser. Please start again.")
		return
	}
	var verifier, target, linkAccount string
	err = a.server.db.QueryRowContext(r.Context(), `DELETE FROM account_login_flows WHERE state_hash=? AND expires_at>? RETURNING verifier,return_to,link_account`, tokenHash(state), time.Now().Unix()).Scan(&verifier, &target, &linkAccount)
	accountCookie(w, flowCookie, "", -1)
	if err != nil {
		accountError(w, 400, "Sign-in expired or was already used. Please start again.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 1024 {
		accountError(w, 400, "GitHub sign-in was cancelled. Return to /account to try again.")
		return
	}
	form := url.Values{"client_id": {a.config.ClientID}, "client_secret": {a.config.ClientSecret}, "code": {code}, "redirect_uri": {a.config.Origin + "/auth/github/callback"}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		accountError(w, 500, "Cannot complete sign-in.")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err = a.providerJSON(req, &token); err != nil || token.Error != "" || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "bearer") {
		accountError(w, 502, "GitHub could not complete sign-in. Please try again.")
		return
	}
	req, err = http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com/user", nil)
	if err != nil {
		accountError(w, 500, "Cannot complete sign-in.")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "YAVCHN")
	var identity struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err = a.providerJSON(req, &identity); err != nil || identity.ID <= 0 || identity.Login == "" || len(identity.Login) > 100 {
		accountError(w, 502, "GitHub identity could not be verified. Please try again.")
		return
	}
	// Provider tokens are used for this request only and are never persisted.
	subject := strconv.FormatInt(identity.ID, 10)
	id, err := a.signInIdentity(w, r, "github", subject, identity.Login, linkAccount, func(tx *sql.Tx, id string) error {
		_, err := tx.ExecContext(r.Context(), `UPDATE account_identities SET avatar_url=? WHERE provider='github' AND subject=?`, limitedString(identity.AvatarURL, 2048), subject)
		return err
	})
	if errors.Is(err, errIdentityTaken) {
		http.Redirect(w, r, withMessage(target, "identity-taken"), http.StatusSeeOther)
		return
	}
	if err != nil {
		return
	}
	var picture string
	if err := a.server.db.QueryRowContext(r.Context(), `SELECT picture FROM accounts WHERE id=?`, id).Scan(&picture); err == nil && picture == "github" {
		a.refreshAvatar(r.Context(), id, identity.AvatarURL)
	}
	if linkAccount != "" {
		target = withMessage(target, "github-linked")
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
func limitedString(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func (a *accountService) providerJSON(req *http.Request, out any) error {
	res, err := a.client.Do(req)
	if err != nil {
		return errors.New("provider unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("provider rejected request")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(b) > 65536 {
		return errors.New("invalid provider response")
	}
	return json.Unmarshal(b, out)
}
func (a *accountService) authorizedWrite(w http.ResponseWriter, r *http.Request) *accountSession {
	if !a.sameOrigin(r) {
		accountError(w, 403, "This request must come from your account's site.")
		return nil
	}
	csrf, form := r.Header.Get("X-CSRF-Token"), false
	if r.Method == "POST" {
		// A script's JSON request carries its token in a header and leaves
		// its body to the handler; a form, or an upload, carries it in a field.
		switch kind := strings.Split(r.Header.Get("Content-Type"), ";")[0]; kind {
		case "application/json":
			r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		case "multipart/form-data":
			r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)
			if r.ParseMultipartForm(uploadLimit) != nil {
				accountError(w, 400, "Invalid account request.")
				return nil
			}
			csrf, form = r.PostFormValue("csrf"), true
		default:
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			if r.ParseForm() != nil {
				accountError(w, 400, "Invalid account request.")
				return nil
			}
			csrf, form = r.PostForm.Get("csrf"), true
		}
	}
	// A form from a page left open across a sign-in or sign-out changes
	// nothing, and the reader lands on their account as it is now. A
	// script's request gets the status accounts.js reloads on.
	u := currentAccount(r)
	stale := ""
	switch {
	case u == nil:
		stale = "session-ended"
	case !sameToken(u.CSRF, csrf):
		stale = "session-changed"
	}
	if stale != "" && form {
		http.Redirect(w, r, accountReturn(r, stale), http.StatusSeeOther)
		return nil
	}
	if u == nil {
		accountError(w, 401, "Sign in to your account first.")
		return nil
	}
	if stale != "" {
		accountError(w, 403, "Your session changed. Reload this page before trying again.")
		return nil
	}
	if !a.writes.Allow(u.ID) {
		accountError(w, 429, "Too many account changes. Please try again shortly.")
		return nil
	}
	return u
}
func (a *accountService) logout(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	if _, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_sessions WHERE token_hash=?`, u.TokenHash); err != nil {
		accountError(w, 503, "Cannot sign out right now.")
		return
	}
	accountCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, accountReturn(r, "signed-out"), http.StatusSeeOther)
}
func (a *accountService) revokeOthers(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	if _, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_sessions WHERE account_id=? AND token_hash<>?`, u.ID, u.TokenHash); err != nil {
		accountError(w, 503, "Cannot revoke other sessions right now.")
		return
	}
	http.Redirect(w, r, accountReturn(r, ""), http.StatusSeeOther)
}
func (a *accountService) remove(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	if r.PostForm.Get("confirm") != "delete" {
		accountError(w, 400, "Confirm account deletion first.")
		return
	}
	if time.Now().Unix()-u.CreatedAt >= 600 {
		accountError(w, 403, "Sign in again before deleting your account.")
		return
	}
	if _, err := a.server.db.ExecContext(r.Context(), `DELETE FROM accounts WHERE id=?`, u.ID); err != nil {
		accountError(w, 503, "Cannot delete your account right now.")
		return
	}
	accountCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, accountReturn(r, "deleted"), http.StatusSeeOther)
}
func (a *accountService) export(w http.ResponseWriter, r *http.Request) {
	u := currentAccount(r)
	if u == nil {
		accountError(w, 401, "Sign in to export your data.")
		return
	}
	x, err := a.exportOf(r.Context(), u)
	if err != nil {
		slog.Error("account export", "err", err)
		accountError(w, 500, "Your data could not be exported.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="yavchn-account.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(x)
}

// accountExport is everything the server keeps about an account, which
// the privacy policy promises the export holds. Secrets that would let
// someone sign in, the session tokens and the passkeys' keys, are left
// out; a session is described by its dates.
type accountExport struct {
	ID         string                 `json:"id"`
	CreatedAt  int64                  `json:"created_at"`
	Identities []exportIdentity       `json:"sign_in_identities"`
	Passkeys   []exportPasskey        `json:"passkeys"`
	Sessions   []exportSession        `json:"sessions"`
	Picture    string                 `json:"picture"`                 // "github", "upload" or "none"
	PictureURI string                 `json:"picture_image,omitempty"` // the stored image as a data URI
	Revision   int64                  `json:"revision"`
	Data       accountDocument        `json:"data"`
	Links      map[string]accountLink `json:"links"`
}

type exportIdentity struct {
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type exportPasskey struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	UsedAt    int64  `json:"used_at,omitempty"`
}

type exportSession struct {
	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
	Current   bool  `json:"current,omitempty"`
}

func (a *accountService) exportOf(ctx context.Context, u *accountSession) (*accountExport, error) {
	db := a.server.db
	x := &accountExport{ID: u.ID, Revision: u.Revision, Data: u.Data, Links: u.Links,
		Identities: []exportIdentity{}, Passkeys: []exportPasskey{}, Sessions: []exportSession{}}
	if err := db.QueryRowContext(ctx, `SELECT created_at,picture FROM accounts WHERE id=?`, u.ID).Scan(&x.CreatedAt, &x.Picture); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT provider,subject,username,avatar_url FROM account_identities WHERE account_id=? ORDER BY provider,username`, u.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id exportIdentity
		if err := rows.Scan(&id.Provider, &id.Subject, &id.Username, &id.AvatarURL); err != nil {
			rows.Close()
			return nil, err
		}
		x.Identities = append(x.Identities, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	if rows, err = db.QueryContext(ctx, `SELECT name,created_at,used_at FROM account_passkeys WHERE account_id=? ORDER BY created_at`, u.ID); err != nil {
		return nil, err
	}
	for rows.Next() {
		var k exportPasskey
		if err := rows.Scan(&k.Name, &k.CreatedAt, &k.UsedAt); err != nil {
			rows.Close()
			return nil, err
		}
		x.Passkeys = append(x.Passkeys, k)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	if rows, err = db.QueryContext(ctx, `SELECT token_hash,created_at,expires_at FROM account_sessions WHERE account_id=? AND expires_at>? ORDER BY created_at`, u.ID, time.Now().Unix()); err != nil {
		return nil, err
	}
	for rows.Next() {
		var s exportSession
		var hash string
		if err := rows.Scan(&hash, &s.CreatedAt, &s.ExpiresAt); err != nil {
			rows.Close()
			return nil, err
		}
		s.Current = hash == u.TokenHash
		x.Sessions = append(x.Sessions, s)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	var kind string
	var image []byte
	err = db.QueryRowContext(ctx, `SELECT content_type,image FROM account_avatars WHERE account_id=?`, u.ID).Scan(&kind, &image)
	if err == nil {
		x.PictureURI = "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(image)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return x, nil
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}
func accountETag(revision int64) string { return fmt.Sprintf(`"%d"`, revision) }
func (a *accountService) writeData(w http.ResponseWriter, u *accountSession, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", accountETag(u.Revision))
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(u)
}
func (a *accountService) getData(w http.ResponseWriter, r *http.Request) {
	u := currentAccount(r)
	if u == nil {
		accountError(w, 401, "Sign in to synchronize your data.")
		return
	}
	a.writeData(w, u, 200)
}
func (a *accountService) putData(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	if r.Header.Get("If-Match") == "" {
		accountError(w, 428, "The current data revision is required.")
		return
	}
	if r.Header.Get("If-Match") != accountETag(u.Revision) {
		a.writeData(w, u, http.StatusPreconditionFailed)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		accountError(w, 415, "Send account data as JSON.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, accountBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var d accountDocument
	if err := decoder.Decode(&d); err != nil {
		accountError(w, 400, "Invalid account data.")
		return
	}
	// A client from before collections and notes omits them; it must not erase them.
	if d.Collections == nil {
		d.Collections = u.Data.Collections
	}
	if d.Collected == nil {
		d.Collected = u.Data.Collected
	}
	if d.Notes == nil {
		d.Notes = u.Data.Notes
	}
	if err := decoder.Decode(new(any)); err != io.EOF || d.validate() != nil {
		accountError(w, 400, "Invalid account data.")
		return
	}
	b, err := json.Marshal(d)
	if err != nil {
		accountError(w, 400, "Invalid account data.")
		return
	}
	result, err := a.server.db.ExecContext(r.Context(), `UPDATE account_data SET document=?,revision=revision+1 WHERE account_id=? AND revision=? AND EXISTS(SELECT 1 FROM account_sessions WHERE token_hash=? AND account_id=? AND expires_at>?)`, string(b), u.ID, u.Revision, u.TokenHash, u.ID, time.Now().Unix())
	if err != nil {
		accountError(w, 503, "Cannot synchronize right now.")
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		accountError(w, 503, "Cannot synchronize right now.")
		return
	}
	if n == 0 {
		accountError(w, http.StatusPreconditionFailed, "Your account data or session changed. Refresh before saving.")
		return
	}
	u.Data = d
	u.Revision++
	a.writeData(w, u, 200)
}
