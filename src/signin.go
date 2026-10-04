package main

// Ways of signing in beyond GitHub, after parkscomputing.com: email links,
// passkeys, and the profile picture a reader uploads. The design and its
// reasons are in docs/USER-ACCOUNTS.md under "Email links, passkeys, and
// recovery" and "Profile pictures".

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const ceremonyCookie = "__Host-yavchn-ceremony"

// uploadLimit bounds an account request's multipart body; pictureLimit
// bounds the picture inside it, generous for a 256-pixel square.
const uploadLimit = 300 * 1024
const pictureLimit = 200 * 1024

var errIdentityTaken = errors.New("identity belongs to another account")

// accountMessages are the outcomes an account action reports by name in
// its return address, so the page that shows them is an address like any other.
var accountMessages = map[string]string{
	"signed-out":       "You are signed out. Your anonymous browser data is available again.",
	"deleted":          "Your account and synchronized data have been deleted.",
	"email-sent":       "If that address can receive mail, a sign-in link is on its way. It works once, within 15 minutes.",
	"email-link-sent":  "If that address can receive mail, a confirmation link is on its way. Open it in this browser to add the address to your account.",
	"email-refused":    "Enter a valid email address.",
	"email-linked":     "That email address is now a way to sign in to your account.",
	"github-linked":    "Your GitHub account is now a way to sign in to your account.",
	"identity-taken":   "That sign-in already belongs to another YAVCHN account, so it was not added. Sign in with it to use that account.",
	"link-expired":     "That link has expired or was already used. Ask for a new one.",
	"link-elsewhere":   "Open the confirmation link in the browser where you are signed in to the account that asked for it.",
	"identity-removed": "That way of signing in was removed.",
	"last-identity":    "An account keeps at least one email address or GitHub account, so you can always get back in. Add another before removing this one.",
	"passkey-added":    "Your passkey was added.",
	"passkey-removed":  "The passkey was removed.",
	"picture-saved":    "Your picture was saved.",
	"picture-removed":  "Your picture was removed.",
	"picture-github":   "Your GitHub picture is shown again.",
	"picture-refused":  "That picture could not be used. Choose a PNG, JPEG, or WebP picture of at most 200 KB.",
}

// withMessage adds an outcome to a local return address.
func withMessage(target, message string) string {
	u, err := url.Parse(target)
	if err != nil {
		return "/account?message=" + url.QueryEscape(message)
	}
	q := u.Query()
	q.Set("message", message)
	u.RawQuery = q.Encode()
	return u.String()
}

// signInToken is the token a sign-in form must carry: the session's own
// when someone is signed in, and otherwise the one the account page issued.
func signInToken(r *http.Request) string {
	if user := currentAccount(r); user != nil {
		return user.CSRF
	}
	if c, err := r.Cookie(loginCookie); err == nil {
		return c.Value
	}
	return ""
}

// startSession replaces this browser's session with a new one for the account.
func (a *accountService) startSession(ctx context.Context, tx *sql.Tx, r *http.Request, accountID string) (string, error) {
	session, err := accountToken()
	if err != nil {
		return "", err
	}
	csrf, err := accountToken()
	if err != nil {
		return "", err
	}
	if c, e := r.Cookie(sessionCookie); e == nil {
		if _, err = tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE token_hash=?`, tokenHash(c.Value)); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_sessions(token_hash,account_id,csrf,created_at,expires_at,agent)VALUES(?,?,?,?,?,?)`,
		tokenHash(session), accountID, csrf, time.Now().Unix(), time.Now().Add(30*24*time.Hour).Unix(), limitedString(r.UserAgent(), 512))
	return session, err
}

func (a *accountService) setSession(w http.ResponseWriter, session string) {
	accountCookie(w, sessionCookie, session, 30*24*3600)
	accountCookie(w, loginCookie, "", -1)
}

// signInIdentity signs the browser in to the account an identity belongs
// to, creating the account for an identity nobody has. With link, an
// identity nobody has joins that account instead, and one another account
// has is refused with errIdentityTaken. Any other failure has been reported
// to the reader when it returns.
func (a *accountService) signInIdentity(w http.ResponseWriter, r *http.Request, provider, subject, label, link string, also func(*sql.Tx, string) error) (string, error) {
	ctx := r.Context()
	fail := func(err error) (string, error) {
		slog.Error("account sign-in storage failed", "provider", provider)
		accountError(w, 503, "Cannot save your sign-in. Please try again.")
		return "", err
	}
	tx, err := a.server.db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM account_identities WHERE provider=? AND subject=?`, provider, subject).Scan(&id)
	switch {
	case err == nil && link != "" && id != link:
		return "", errIdentityTaken
	case err == nil:
		_, err = tx.ExecContext(ctx, `UPDATE account_identities SET username=? WHERE provider=? AND subject=?`, label, provider, subject)
	case err == sql.ErrNoRows && link != "":
		id = link
		_, err = tx.ExecContext(ctx, `INSERT INTO account_identities(provider,subject,account_id,username)VALUES(?,?,?,?)`, provider, subject, id, label)
	case err == sql.ErrNoRows:
		if id, err = accountToken(); err != nil {
			return fail(err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,created_at)VALUES(?,?)`, id, time.Now().Unix())
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO account_identities(provider,subject,account_id,username)VALUES(?,?,?,?)`, provider, subject, id, label)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO account_data(account_id)VALUES(?)`, id)
		}
	}
	if err == nil && also != nil {
		err = also(tx, id)
	}
	var session string
	if err == nil {
		session, err = a.startSession(ctx, tx, r, id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return fail(err)
	}
	a.setSession(w, session)
	return id, nil
}

/* === Email links ========================================================= */

// normalEmail is an address as YAVCHN keeps it: one plain address, lower case.
func normalEmail(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" || len(v) > 254 || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", false
	}
	parsed, err := mail.ParseAddress(v)
	if err != nil || parsed.Name != "" || parsed.Address != v || !strings.Contains(v, "@") {
		return "", false
	}
	return v, true
}

// emailBudget counts one message against the hourly limits for the address
// and the network address and the daily limit for everyone, and reports
// whether all of them still had room.
func (a *accountService) emailBudget(ctx context.Context, address, ip string) bool {
	now := time.Now().UTC()
	buckets := []struct {
		key   string
		limit int
		until time.Time
	}{
		{"all:" + now.Format("20060102"), 500, now.Truncate(24 * time.Hour).Add(24 * time.Hour)},
		{"address:" + now.Format("2006010215") + ":" + tokenHash(address), 5, now.Truncate(time.Hour).Add(time.Hour)},
		{"network:" + now.Format("2006010215") + ":" + tokenHash(ip), 30, now.Truncate(time.Hour).Add(time.Hour)},
	}
	tx, err := a.server.db.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_email_budget WHERE expires_at<=?`, now.Unix()); err != nil {
		return false
	}
	for _, b := range buckets {
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count FROM account_email_budget WHERE bucket=?`, b.key).Scan(&count)
		if err != nil && err != sql.ErrNoRows {
			return false
		}
		if count >= b.limit {
			return false
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_email_budget(bucket,count,expires_at)VALUES(?,1,?) ON CONFLICT(bucket) DO UPDATE SET count=count+1`, b.key, b.until.Unix()); err != nil {
			return false
		}
	}
	return tx.Commit() == nil
}

// sendMail sends plain text through Resend's documented HTTP API.
func (a *accountService) sendMail(ctx context.Context, to, subject, text string) bool {
	message := map[string]any{"from": a.config.EmailFrom, "to": []string{to}, "subject": subject, "text": text}
	if a.config.EmailReplyTo != "" {
		message["reply_to"] = a.config.EmailReplyTo
	}
	body, err := json.Marshal(message)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+a.config.ResendKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "YAVCHN")
	res, err := a.client.Do(req)
	if err != nil {
		slog.Error("mail request failed")
		return false
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		slog.Error("mail refused", "status", res.StatusCode)
		return false
	}
	return true
}

// requestEmail sends a sign-in link, or with link=1 from inside an account,
// a link that adds the address to it. Every request gets the same answer.
func (a *accountService) requestEmail(w http.ResponseWriter, r *http.Request) {
	if !a.config.Email() {
		accountError(w, 503, "Email sign-in is not available.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !a.sameOrigin(r) || r.ParseForm() != nil {
		accountError(w, 403, "Invalid sign-in request.")
		return
	}
	if !sameToken(signInToken(r), r.PostForm.Get("csrf")) {
		accountError(w, 403, "Reload the account page before signing in.")
		return
	}
	link, message := "", "email-sent"
	if user := currentAccount(r); user != nil && r.PostForm.Get("link") == "1" {
		link, message = user.ID, "email-link-sent"
	}
	target := accountReturn(r, "")
	address, ok := normalEmail(r.PostForm.Get("email"))
	if !ok {
		http.Redirect(w, r, withMessage(target, "email-refused"), http.StatusSeeOther)
		return
	}
	if !a.rate.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "300")
		accountError(w, 429, "Too many sign-in attempts. Please try again later.")
		return
	}
	if a.emailBudget(r.Context(), address, clientIP(r)) {
		a.issueEmail(r.Context(), address, link, target)
	}
	http.Redirect(w, r, withMessage(target, message), http.StatusSeeOther)
}

func (a *accountService) issueEmail(ctx context.Context, address, link, target string) {
	token, err := accountToken()
	if err != nil {
		return
	}
	now := time.Now()
	tx, err := a.server.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	// A newer link for the same address and purpose replaces the older one.
	_, err = tx.ExecContext(ctx, `DELETE FROM account_email_links WHERE expires_at<=? OR (email=? AND link_account=?)`, now.Unix(), address, link)
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO account_email_links(token_hash,email,link_account,return_to,expires_at)VALUES(?,?,?,?,?)`,
			tokenHash(token), address, link, target, now.Add(15*time.Minute).Unix())
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		slog.Error("email link could not be stored")
		return
	}
	page := a.config.Origin + "/auth/email/link?token=" + token
	subject, text := "Sign in to YAVCHN", "Open this link within 15 minutes, then press the button on the page it opens to sign in to YAVCHN:\n\n"+page+"\n\nIf you did not ask to sign in, ignore this message. Nobody can sign in with it unless they open it."
	if link != "" {
		subject, text = "Add this address to your YAVCHN account", "Someone signed in to YAVCHN asked to add this address to their account. To add it, open this link within 15 minutes, in the browser where you are signed in, and press the button on the page it opens:\n\n"+page+"\n\nIf that was not you, ignore this message and the address will not be added."
	}
	if !a.sendMail(ctx, address, subject, text) {
		_, _ = a.server.db.ExecContext(ctx, `DELETE FROM account_email_links WHERE token_hash=?`, tokenHash(token))
	}
}

type emailLinkVM struct {
	menuContext
	Title, Token, Email string
	Valid, Adding       bool
	NewReaderHref       string
	AllSources          []sourceOptVM
}

// emailLink shows a link's confirmation page. Showing it spends nothing,
// so a mail scanner that fetches the link cannot use it up.
func (a *accountService) emailLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	vm := emailLinkVM{menuContext: menuContext{Account: currentAccount(r), View: "classic", WindowURL: "/account?view=window", ClassicURL: "/account?view=classic"},
		Title: "Confirm sign-in · YAVCHN", NewReaderHref: "/hn/?open=reader-1&top=reader-1", AllSources: a.server.buildSourceOpts("")}
	var link string
	if len(token) == 43 && a.server.db.QueryRowContext(r.Context(), `SELECT email,link_account FROM account_email_links WHERE token_hash=? AND expires_at>?`, tokenHash(token), time.Now().Unix()).Scan(&vm.Email, &link) == nil {
		vm.Valid, vm.Token, vm.Adding = true, token, link != ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.server.tpl.ExecuteTemplate(w, "emaillink.html.tmpl", vm); err != nil {
		slog.Error("render email link page failed")
	}
}

// redeemEmail spends a link and signs in, or adds the address to the
// account that asked for it, which must be the one signed in here.
func (a *accountService) redeemEmail(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !a.sameOrigin(r) || r.ParseForm() != nil {
		accountError(w, 403, "Invalid sign-in request.")
		return
	}
	token := r.PostForm.Get("token")
	var address, link, target string
	if len(token) != 43 || a.server.db.QueryRowContext(r.Context(), `SELECT email,link_account,return_to FROM account_email_links WHERE token_hash=? AND expires_at>?`, tokenHash(token), time.Now().Unix()).Scan(&address, &link, &target) != nil {
		http.Redirect(w, r, "/account?message=link-expired", http.StatusSeeOther)
		return
	}
	if user := currentAccount(r); link != "" && (user == nil || user.ID != link) {
		http.Redirect(w, r, "/account?message=link-elsewhere", http.StatusSeeOther)
		return
	}
	// Only the request that removes the link may use it.
	if res, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_email_links WHERE token_hash=? AND expires_at>?`, tokenHash(token), time.Now().Unix()); err != nil {
		accountError(w, 503, "Cannot complete sign-in right now.")
		return
	} else if n, _ := res.RowsAffected(); n != 1 {
		http.Redirect(w, r, "/account?message=link-expired", http.StatusSeeOther)
		return
	}
	_, err := a.signInIdentity(w, r, "email", address, address, link, nil)
	switch {
	case errors.Is(err, errIdentityTaken):
		http.Redirect(w, r, withMessage(target, "identity-taken"), http.StatusSeeOther)
	case err != nil:
	case link != "":
		http.Redirect(w, r, withMessage(target, "email-linked"), http.StatusSeeOther)
	default:
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

/* === Passkeys ============================================================ */

func (a *accountService) configurePasskeys() error {
	u, err := url.Parse(a.config.Origin)
	if err != nil {
		return err
	}
	a.webauthn, err = webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "YAVCHN", RPOrigins: []string{a.config.Origin}})
	return err
}

// passkeyUser is an account as WebAuthn sees it. Its user handle is the
// account's internal identifier, which is random and never shown.
type passkeyUser struct {
	id, name string
	creds    []webauthn.Credential
}

func (p *passkeyUser) WebAuthnID() []byte                         { return []byte(p.id) }
func (p *passkeyUser) WebAuthnName() string                       { return p.name }
func (p *passkeyUser) WebAuthnDisplayName() string                { return p.name }
func (p *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

func (a *accountService) passkeyUser(ctx context.Context, id string) (*passkeyUser, error) {
	p := &passkeyUser{id: id}
	err := a.server.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT username FROM account_identities WHERE account_id=a.id ORDER BY provider<>'github',username LIMIT 1),'YAVCHN account') FROM accounts a WHERE a.id=?`, id).Scan(&p.name)
	if err != nil {
		return nil, err
	}
	rows, err := a.server.db.QueryContext(ctx, `SELECT credential FROM account_passkeys WHERE account_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var c webauthn.Credential
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		p.creds = append(p.creds, c)
	}
	return p, rows.Err()
}

// saveCeremony keeps a ceremony's challenge for five minutes, bound to this
// browser by a cookie; takeCeremony uses it once.
func (a *accountService) saveCeremony(ctx context.Context, w http.ResponseWriter, kind, accountID, target string, s *webauthn.SessionData) error {
	token, err := accountToken()
	if err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err = a.server.db.ExecContext(ctx, `DELETE FROM account_ceremonies WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return err
	}
	if _, err = a.server.db.ExecContext(ctx, `INSERT INTO account_ceremonies(token_hash,kind,account_id,session,return_to,expires_at)VALUES(?,?,?,?,?,?)`,
		tokenHash(token), kind, accountID, string(b), target, time.Now().Add(5*time.Minute).Unix()); err != nil {
		return err
	}
	accountCookie(w, ceremonyCookie, token, 300)
	return nil
}

func (a *accountService) takeCeremony(w http.ResponseWriter, r *http.Request, kind, accountID string) (*webauthn.SessionData, string, error) {
	c, err := r.Cookie(ceremonyCookie)
	if err != nil {
		return nil, "", err
	}
	accountCookie(w, ceremonyCookie, "", -1)
	var raw, target string
	if err = a.server.db.QueryRowContext(r.Context(), `DELETE FROM account_ceremonies WHERE token_hash=? AND kind=? AND account_id=? AND expires_at>? RETURNING session,return_to`,
		tokenHash(c.Value), kind, accountID, time.Now().Unix()).Scan(&raw, &target); err != nil {
		return nil, "", err
	}
	s := &webauthn.SessionData{}
	return s, target, json.Unmarshal([]byte(raw), s)
}

func passkeyJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func passkeyRefused(w http.ResponseWriter, status int, message string) {
	passkeyJSON(w, status, map[string]string{"error": message})
}

// passkeyRequest reads a script's JSON request for a passkey ceremony,
// which carries its token in X-CSRF-Token.
func (a *accountService) passkeyRequest(w http.ResponseWriter, r *http.Request, token string, into any) bool {
	if a.webauthn == nil {
		passkeyRefused(w, 503, "Passkeys are not available here.")
		return false
	}
	if !a.sameOrigin(r) || !sameToken(token, r.Header.Get("X-CSRF-Token")) || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		passkeyRefused(w, 403, "Reload the page, then try again.")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(into); err != nil {
		passkeyRefused(w, 400, "That did not work. Try again.")
		return false
	}
	return true
}

type passkeyBody struct {
	ReturnTo   string          `json:"return_to"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// returnFrom gives a script's requested destination the account return rules.
func returnFrom(r *http.Request, raw string) string {
	r.PostForm = url.Values{"return_to": {raw}}
	return accountReturn(r, "")
}

// passkeyOptions starts a sign-in with a passkey, which needs no address.
func (a *accountService) passkeyOptions(w http.ResponseWriter, r *http.Request) {
	var body passkeyBody
	if !a.passkeyRequest(w, r, signInToken(r), &body) {
		return
	}
	if !a.rate.Allow(clientIP(r)) {
		passkeyRefused(w, 429, "Too many sign-in attempts. Please try again later.")
		return
	}
	assertion, session, err := a.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err == nil {
		err = a.saveCeremony(r.Context(), w, "signin", "", returnFrom(r, body.ReturnTo), session)
	}
	if err != nil {
		passkeyRefused(w, 503, "Cannot start a passkey sign-in right now.")
		return
	}
	passkeyJSON(w, 200, assertion)
}

// passkeySignIn finishes a sign-in with a passkey.
func (a *accountService) passkeySignIn(w http.ResponseWriter, r *http.Request) {
	var body passkeyBody
	if !a.passkeyRequest(w, r, signInToken(r), &body) {
		return
	}
	refused := "That passkey could not sign you in. Try again, or use another way to sign in."
	session, target, err := a.takeCeremony(w, r, "signin", "")
	if err != nil {
		passkeyRefused(w, 400, "The passkey request expired. Try again.")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
	if err != nil {
		passkeyRefused(w, 401, refused)
		return
	}
	user, cred, err := a.webauthn.ValidatePasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
		return a.passkeyUser(r.Context(), string(handle))
	}, *session, parsed)
	if err != nil || cred.Authenticator.CloneWarning {
		passkeyRefused(w, 401, refused)
		return
	}
	id := string(user.WebAuthnID())
	stored, err := json.Marshal(cred)
	if err != nil {
		passkeyRefused(w, 500, refused)
		return
	}
	tx, err := a.server.db.BeginTx(r.Context(), nil)
	if err != nil {
		passkeyRefused(w, 503, "Cannot save your sign-in right now.")
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE account_passkeys SET credential=?,used_at=? WHERE credential_id=? AND account_id=?`, string(stored), time.Now().Unix(), base64.RawURLEncoding.EncodeToString(cred.ID), id)
	var changed int64
	if err == nil {
		changed, err = res.RowsAffected()
	}
	var token string
	if err == nil && changed == 1 {
		token, err = a.startSession(r.Context(), tx, r, id)
	}
	if err == nil && changed == 1 {
		err = tx.Commit()
	}
	if err != nil || changed != 1 {
		passkeyRefused(w, 503, "Cannot save your sign-in right now.")
		return
	}
	a.setSession(w, token)
	passkeyJSON(w, 200, map[string]string{"redirect": target})
}

// passkeyCreationOptions starts adding a passkey to the signed-in account.
func (a *accountService) passkeyCreationOptions(w http.ResponseWriter, r *http.Request) {
	u := currentAccount(r)
	if u == nil {
		passkeyRefused(w, 401, "Sign in to your account first.")
		return
	}
	var body passkeyBody
	if !a.passkeyRequest(w, r, u.CSRF, &body) {
		return
	}
	user, err := a.passkeyUser(r.Context(), u.ID)
	if err != nil {
		passkeyRefused(w, 503, "Cannot add a passkey right now.")
		return
	}
	if len(user.creds) >= 20 {
		passkeyRefused(w, 409, "This account has 20 passkeys, which is the limit. Remove one to add another.")
		return
	}
	creation, session, err := a.webauthn.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(user.creds).CredentialDescriptors()))
	if err == nil {
		err = a.saveCeremony(r.Context(), w, "register", u.ID, returnFrom(r, body.ReturnTo), session)
	}
	if err != nil {
		passkeyRefused(w, 503, "Cannot add a passkey right now.")
		return
	}
	passkeyJSON(w, 200, creation)
}

// passkeyName is a passkey's name as the reader gave it, or "Passkey".
func passkeyName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" || strings.IndexFunc(name, unicode.IsControl) >= 0 || !utf8.ValidString(name) {
		return "Passkey"
	}
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	return name
}

// passkeyRegister finishes adding a passkey to the signed-in account.
func (a *accountService) passkeyRegister(w http.ResponseWriter, r *http.Request) {
	u := currentAccount(r)
	if u == nil {
		passkeyRefused(w, 401, "Sign in to your account first.")
		return
	}
	var body passkeyBody
	if !a.passkeyRequest(w, r, u.CSRF, &body) {
		return
	}
	session, target, err := a.takeCeremony(w, r, "register", u.ID)
	if err != nil {
		passkeyRefused(w, 400, "The passkey request expired. Try again.")
		return
	}
	user, err := a.passkeyUser(r.Context(), u.ID)
	if err != nil {
		passkeyRefused(w, 503, "Cannot add a passkey right now.")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
	var cred *webauthn.Credential
	if err == nil {
		cred, err = a.webauthn.CreateCredential(user, *session, parsed)
	}
	if err != nil {
		passkeyRefused(w, 400, "That passkey could not be added. Try again.")
		return
	}
	stored, err := json.Marshal(cred)
	if err == nil {
		_, err = a.server.db.ExecContext(r.Context(), `INSERT INTO account_passkeys(credential_id,account_id,name,credential,created_at,used_at)VALUES(?,?,?,?,?,0)`,
			base64.RawURLEncoding.EncodeToString(cred.ID), u.ID, passkeyName(body.Name), string(stored), time.Now().Unix())
	}
	if err != nil {
		passkeyRefused(w, 409, "That passkey could not be saved. It may already belong to an account.")
		return
	}
	passkeyJSON(w, 200, map[string]string{"redirect": withMessage(target, "passkey-added")})
}

func (a *accountService) removePasskey(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	if _, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_passkeys WHERE credential_id=? AND account_id=?`, r.PostForm.Get("id"), u.ID); err != nil {
		accountError(w, 503, "Cannot remove the passkey right now.")
		return
	}
	http.Redirect(w, r, accountReturn(r, "passkey-removed"), http.StatusSeeOther)
}

// removeIdentity removes a way of signing in, keeping at least one, since
// an identity is how a reader gets back in when a device is lost.
func (a *accountService) removeIdentity(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	res, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_identities WHERE provider=? AND subject=? AND account_id=? AND (SELECT count(*) FROM account_identities WHERE account_id=?)>1`,
		r.PostForm.Get("provider"), r.PostForm.Get("subject"), u.ID, u.ID)
	var n int64
	if err == nil {
		n, err = res.RowsAffected()
	}
	if err != nil {
		accountError(w, 503, "Cannot change your sign-in methods right now.")
		return
	}
	message := "identity-removed"
	if n == 0 {
		message = "last-identity"
	}
	http.Redirect(w, r, accountReturn(r, message), http.StatusSeeOther)
}

/* === Profile pictures ==================================================== */

// pictureType is the type an image's own leading bytes say it is, or "".
func pictureType(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func (a *accountService) storePicture(ctx context.Context, accountID, kind string, image []byte, picture string) error {
	sum := sha256.Sum256(image)
	tx, err := a.server.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO account_avatars(account_id,content_type,image,version)VALUES(?,?,?,?) ON CONFLICT(account_id) DO UPDATE SET content_type=excluded.content_type,image=excluded.image,version=excluded.version`,
		accountID, kind, image, hex.EncodeToString(sum[:8]))
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET picture=? WHERE id=?`, picture, accountID)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}

// uploadPicture takes the picture the browser cropped and re-encoded, or
// the file itself without script. It is never decoded here; it is served
// back with the type its bytes showed, nosniff and a sandbox.
func (a *accountService) uploadPicture(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	file, _, err := r.FormFile("picture")
	var image []byte
	if err == nil {
		defer file.Close()
		image, err = io.ReadAll(io.LimitReader(file, pictureLimit+1))
	}
	kind := pictureType(image)
	if err != nil || len(image) == 0 || len(image) > pictureLimit || kind == "" {
		http.Redirect(w, r, accountReturn(r, "picture-refused"), http.StatusSeeOther)
		return
	}
	if err := a.storePicture(r.Context(), u.ID, kind, image, "upload"); err != nil {
		accountError(w, 503, "Cannot save your picture right now.")
		return
	}
	http.Redirect(w, r, accountReturn(r, "picture-saved"), http.StatusSeeOther)
}

func (a *accountService) removePicture(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	_, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_avatars WHERE account_id=?`, u.ID)
	if err == nil {
		_, err = a.server.db.ExecContext(r.Context(), `UPDATE accounts SET picture='none' WHERE id=?`, u.ID)
	}
	if err != nil {
		accountError(w, 503, "Cannot remove your picture right now.")
		return
	}
	http.Redirect(w, r, accountReturn(r, "picture-removed"), http.StatusSeeOther)
}

// githubPicture shows the account's GitHub picture again, copied afresh.
func (a *accountService) githubPicture(w http.ResponseWriter, r *http.Request) {
	u := a.authorizedWrite(w, r)
	if u == nil {
		return
	}
	var avatar string
	if err := a.server.db.QueryRowContext(r.Context(), `SELECT avatar_url FROM account_identities WHERE account_id=? AND provider='github' AND avatar_url<>'' LIMIT 1`, u.ID).Scan(&avatar); err != nil {
		http.Redirect(w, r, accountReturn(r, "picture-refused"), http.StatusSeeOther)
		return
	}
	_, err := a.server.db.ExecContext(r.Context(), `DELETE FROM account_avatars WHERE account_id=?`, u.ID)
	if err == nil {
		_, err = a.server.db.ExecContext(r.Context(), `UPDATE accounts SET picture='github' WHERE id=?`, u.ID)
	}
	if err != nil {
		accountError(w, 503, "Cannot change your picture right now.")
		return
	}
	a.refreshAvatar(r.Context(), u.ID, avatar)
	http.Redirect(w, r, accountReturn(r, "picture-github"), http.StatusSeeOther)
}

/* === The account panel's sign-in methods ================================= */

type panelIdentity struct {
	Provider, Subject, Label string
}

type panelPasskey struct {
	ID, Name, Added, Used string
}

// signInMethods fills the panel's identities, passkeys and picture choices.
func (a *accountService) signInMethods(ctx context.Context, vm *accountPanelVM) error {
	u := vm.Account
	rows, err := a.server.db.QueryContext(ctx, `SELECT provider,subject,username,avatar_url FROM account_identities WHERE account_id=? ORDER BY provider<>'github',username`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id panelIdentity
		var avatar string
		if err := rows.Scan(&id.Provider, &id.Subject, &id.Label, &avatar); err != nil {
			return err
		}
		vm.Identities = append(vm.Identities, id)
		if id.Provider == "github" {
			vm.HasGitHub, vm.GitHubPicture = true, avatar != ""
		} else if vm.Email == "" {
			vm.Email = id.Subject
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	keys, err := a.server.db.QueryContext(ctx, `SELECT credential_id,name,created_at,used_at FROM account_passkeys WHERE account_id=? ORDER BY created_at`, u.ID)
	if err != nil {
		return err
	}
	defer keys.Close()
	for keys.Next() {
		var k panelPasskey
		var added, used int64
		if err := keys.Scan(&k.ID, &k.Name, &added, &used); err != nil {
			return err
		}
		k.Added = time.Unix(added, 0).UTC().Format("2 January 2006")
		if used > 0 {
			k.Used = time.Unix(used, 0).UTC().Format("2 January 2006")
		}
		vm.Passkeys = append(vm.Passkeys, k)
	}
	if err := keys.Err(); err != nil {
		return err
	}
	return a.server.db.QueryRowContext(ctx, `SELECT picture FROM accounts WHERE id=?`, u.ID).Scan(&vm.Picture)
}
