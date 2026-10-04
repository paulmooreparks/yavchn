package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type accountFixture struct {
	a   *accountService
	h   http.Handler
	t   *testing.T
	mux *http.ServeMux
	// The fake GitHub's picture for the next login: its address in the
	// identity, and the bytes it serves there.
	avatarURL string
	avatar    []byte
}

func accountsForTest(t *testing.T) *accountFixture {
	t.Helper()
	s, mux := testServer(t)
	db, err := OpenDB(context.Background(), filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s.db = db
	a, err := newAccountService(s, accountConfig{Origin: "https://beta.yavchn.com", ClientID: "client", ClientSecret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	s.accounts = a
	a.register(mux)
	return &accountFixture{a: a, h: a.middleware(mux), t: t, mux: mux}
}
func (f *accountFixture) request(method, path, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, "https://beta.yavchn.com"+path, strings.NewReader(body))
	for _, c := range cookies {
		r.AddCookie(c)
	}
	if method == "POST" || method == "PUT" {
		r.Header.Set("Origin", "https://beta.yavchn.com")
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func cookieNamed(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing cookie %s: %s", name, w.Body)
	return nil
}
func (f *accountFixture) flow() (string, *http.Cookie, string) {
	f.t.Helper()
	w := f.request("POST", "/auth/github", "", nil, nil)
	if w.Code != 303 {
		f.t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		f.t.Fatal(err)
	}
	if u.Query().Get("scope") != "" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("redirect_uri") != "https://beta.yavchn.com/auth/github/callback" {
		f.t.Fatal("unexpected authorization parameters")
	}
	return u.Query().Get("state"), cookieNamed(f.t, w, flowCookie), u.Query().Get("code_challenge")
}
func (f *accountFixture) login(id int64) *http.Cookie {
	f.t.Helper()
	state, flow, challenge := f.flow()
	f.a.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "github.com" {
			if err := r.ParseForm(); err != nil {
				f.t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(hash[:]) != challenge || r.PostForm.Get("client_secret") != "test-secret" {
				f.t.Fatal("PKCE exchange was not bound to the login flow")
			}
			return jsonResp(200, `{"access_token":"transient-token","token_type":"bearer"}`), nil
		}
		if r.URL.Host == "avatars.githubusercontent.com" {
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("s") != "96" {
				f.t.Fatal("avatar request carried a token or asked for the wrong size")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(f.avatar))), Header: http.Header{}}, nil
		}
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer transient-token" {
			f.t.Fatal("unexpected provider request")
		}
		avatar, _ := json.Marshal(f.avatarURL)
		return jsonResp(200, fmt.Sprintf(`{"id":%d,"login":"user%d","avatar_url":%s}`, id, id, avatar)), nil
	})
	w := f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", []*http.Cookie{flow}, nil)
	if w.Code != 303 || w.Header().Get("Location") != "/account" {
		f.t.Fatalf("callback: %d %s", w.Code, w.Body)
	}
	if replay := f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", []*http.Cookie{flow}, nil); replay.Code != 400 {
		f.t.Fatal("callback could be replayed")
	}
	c := cookieNamed(f.t, w, sessionCookie)
	if !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		f.t.Fatalf("insecure session cookie: %#v", c)
	}
	return c
}
func (f *accountFixture) session(cookie *http.Cookie) *accountSession {
	f.t.Helper()
	w := f.request("GET", "/account/data", "", []*http.Cookie{cookie}, nil)
	if w.Code != 200 {
		f.t.Fatalf("session: %d %s", w.Code, w.Body)
	}
	u := &accountSession{}
	if err := json.Unmarshal(w.Body.Bytes(), u); err != nil {
		f.t.Fatal(err)
	}
	if err := f.a.server.db.QueryRow(`SELECT csrf FROM account_sessions WHERE token_hash=?`, tokenHash(cookie.Value)).Scan(&u.CSRF); err != nil {
		f.t.Fatal(err)
	}
	return u
}
func (f *accountFixture) put(cookie *http.Cookie, u *accountSession, d accountDocument) *httptest.ResponseRecorder {
	b, err := json.Marshal(d)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.request("PUT", "/account/data", string(b), []*http.Cookie{cookie}, map[string]string{"Content-Type": "application/json", "If-Match": accountETag(u.Revision), "X-CSRF-Token": u.CSRF})
}
func TestAccountLoginAndIdentity(t *testing.T) {
	f := accountsForTest(t)
	first := f.login(7)
	u := f.session(first)
	w := f.request("GET", "/account", "", []*http.Cookie{first}, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "user7") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || !strings.Contains(w.Body.String(), `id="yavchn-account-data"`) {
		t.Fatalf("account page: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "transient-token") || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatal("provider secret exposed")
	}
	second := f.login(7)
	if f.session(second).ID != u.ID {
		t.Fatal("returning provider identity created another account")
	}
	var stored string
	if err := f.a.server.db.QueryRow(`SELECT token_hash FROM account_sessions WHERE token_hash=?`, tokenHash(first.Value)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == first.Value {
		t.Fatal("session bearer token persisted without hashing")
	}
}
func TestAccountFlowBindingExpiryAndProviderFailure(t *testing.T) {
	f := accountsForTest(t)
	state, c, _ := f.flow()
	if w := f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", nil, nil); w.Code != 400 {
		t.Fatal("accepted callback without browser binding")
	}
	if w := f.request("GET", "/auth/github/callback?state="+strings.Repeat("x", 43)+"&code=code", "", []*http.Cookie{c}, nil); w.Code != 400 {
		t.Fatal("accepted wrong state")
	}
	if _, err := f.a.server.db.Exec(`UPDATE account_login_flows SET expires_at=0`); err != nil {
		t.Fatal(err)
	}
	if w := f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", []*http.Cookie{c}, nil); w.Code != 400 {
		t.Fatal("accepted expired flow")
	}
	state, c, _ = f.flow()
	f.a.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResp(200, `{"error":"bad_verification_code","error_description":"provider-secret"}`), nil
	})
	w := f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", []*http.Cookie{c}, nil)
	if w.Code != 502 || strings.Contains(w.Body.String(), "provider-secret") {
		t.Fatal("provider failure mishandled")
	}
	if w = f.request("GET", "/auth/github/callback?state="+state+"&code=code", "", []*http.Cookie{c}, nil); w.Code != 400 {
		t.Fatal("failed flow reused")
	}
}
func TestAccountDataIsolationConflictAndValidation(t *testing.T) {
	f := accountsForTest(t)
	first := f.login(1)
	second := f.login(2)
	u := f.session(first)
	other := f.session(second)
	d := emptyAccountDocument()
	d.Pins["hn-42"] = pinEntry{Source: "hn", Title: "<script>private</script>", URL: "https://example.com", PinnedAt: 123}
	w := f.put(first, u, d)
	if w.Code != 200 || w.Header().Get("ETag") != `"1"` {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w = f.put(first, u, emptyAccountDocument()); w.Code != 412 {
		t.Fatal("stale snapshot replaced newer data")
	}
	if len(f.session(second).Data.Pins) != 0 || u.ID == other.ID {
		t.Fatal("account data crossed accounts")
	}
	w = f.request("GET", "/hn/", "", []*http.Cookie{first}, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>private</script>") || !strings.Contains(w.Body.String(), `\u003cscript\u003e`) {
		t.Fatalf("unsafe embedded state: %d", w.Code)
	}
	u = f.session(first)
	d.Pins["hn-42"] = pinEntry{Source: "hn", URL: "javascript:alert(1)"}
	if f.put(first, u, d).Code != 400 {
		t.Fatal("accepted active-content pin URL")
	}
	if w = f.request("PUT", "/account/data", `{"pins":{},"domains":[],"progress":{},"unknown":1}`, []*http.Cookie{first}, map[string]string{"Content-Type": "application/json", "If-Match": `"1"`, "X-CSRF-Token": u.CSRF}); w.Code != 400 {
		t.Fatal("unknown data accepted")
	}
}
func TestAccountCollectionsAndNotes(t *testing.T) {
	f := accountsForTest(t)
	c := f.login(5)
	u := f.session(c)
	if u.Data.Collections == nil || u.Data.Collected == nil || u.Data.Notes == nil {
		t.Fatal("stored document lacks empty collections and notes")
	}
	d := emptyAccountDocument()
	d.Collections["k3x9q2"] = collectionEntry{Name: "Read later", CreatedAt: 1}
	d.Collected["k3x9q2:hn-42"] = collectedEntry{Source: "hn", ID: "42", Title: "Story", URL: "https://example.com/", AddedAt: 2}
	d.Notes["lobsters-ab12cd"] = noteEntry{Source: "lobsters", ID: "ab12cd", Title: "Thread", Text: "Worth rereading.", UpdatedAt: 3}
	if w := f.put(c, u, d); w.Code != 200 {
		t.Fatalf("save collections and notes: %d %s", w.Code, w.Body)
	}
	// A script from before these features sends only the original members.
	u = f.session(c)
	w := f.request("PUT", "/account/data", `{"pins":{},"domains":["example.com"],"progress":{}}`, []*http.Cookie{c}, map[string]string{"Content-Type": "application/json", "If-Match": accountETag(u.Revision), "X-CSRF-Token": u.CSRF})
	if w.Code != 200 {
		t.Fatalf("older client write: %d %s", w.Code, w.Body)
	}
	u = f.session(c)
	if u.Data.Collections["k3x9q2"].Name != "Read later" || len(u.Data.Collected) != 1 || u.Data.Notes["lobsters-ab12cd"].Text != "Worth rereading." || len(u.Data.Domains) != 1 {
		t.Fatalf("older client erased newer members: %+v", u.Data)
	}
	invalid := map[string]func(*accountDocument){
		"orphan entry":     func(d *accountDocument) { d.Collected["zzzzzz:hn-1"] = collectedEntry{Source: "hn", ID: "1"} },
		"mismatched entry": func(d *accountDocument) { d.Collected["k3x9q2:hn-1"] = collectedEntry{Source: "hn", ID: "2"} },
		"unsafe entry URL": func(d *accountDocument) {
			d.Collected["k3x9q2:hn-1"] = collectedEntry{Source: "hn", ID: "1", URL: "javascript:alert(1)"}
		},
		"bad collection id": func(d *accountDocument) { d.Collections["Bad-ID"] = collectionEntry{Name: "x"} },
		"blank collection":  func(d *accountDocument) { d.Collections["abcdef"] = collectionEntry{Name: "  "} },
		"untrimmed name":    func(d *accountDocument) { d.Collections["abcdef"] = collectionEntry{Name: " x"} },
		"mismatched note":   func(d *accountDocument) { d.Notes["hn-1"] = noteEntry{Source: "hn", ID: "2", Text: "x"} },
		"empty note":        func(d *accountDocument) { d.Notes["hn-1"] = noteEntry{Source: "hn", ID: "1", Text: " "} },
		"oversized note": func(d *accountDocument) {
			d.Notes["hn-1"] = noteEntry{Source: "hn", ID: "1", Text: strings.Repeat("é", 10001)}
		},
		"unknown note source": func(d *accountDocument) { d.Notes["x-1"] = noteEntry{Source: "x", ID: "1", Text: "x"} },
	}
	for name, change := range invalid {
		u = f.session(c)
		bad := u.Data
		bad.Collections, bad.Collected, bad.Notes = maps.Clone(bad.Collections), maps.Clone(bad.Collected), maps.Clone(bad.Notes)
		change(&bad)
		if f.put(c, u, bad).Code != 400 {
			t.Fatalf("accepted %s", name)
		}
	}
	long := u.Data
	long.Notes = map[string]noteEntry{"hn-1": {Source: "hn", ID: "1", Text: strings.Repeat("é", 10000)}}
	if w := f.put(c, f.session(c), long); w.Code != 200 {
		t.Fatalf("rejected a note at the limit: %d", w.Code)
	}
}
func TestAccountTopBarAndAvatar(t *testing.T) {
	f := accountsForTest(t)
	if w := f.request("GET", "/hn/", "", nil, nil); !strings.Contains(w.Body.String(), `account-pill account-signin" href="/account?view=window" data-win-open="account">Sign in</a>`) {
		t.Fatal("signed-out top bar lacks Sign in")
	}
	plain := f.login(6)
	if w := f.request("GET", "/hn/", "", []*http.Cookie{plain}, nil); !strings.Contains(w.Body.String(), `account-avatar-placeholder`) || strings.Contains(w.Body.String(), ">Sign in</a>") {
		t.Fatal("an account without a picture lacks the placeholder")
	}
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	f.avatarURL, f.avatar = "https://avatars.githubusercontent.com/u/7?v=4", []byte(png)
	c := f.login(7)
	page := f.request("GET", "/hn/", "", []*http.Cookie{c}, nil).Body.String()
	at := strings.Index(page, `<img class="account-avatar" src="/account/avatar?v=`)
	if at < 0 || !strings.Contains(page, `aria-label="Your account (user7)"`) {
		t.Fatal("top bar lacks the account picture")
	}
	src := page[at+len(`<img class="account-avatar" src="`):]
	src = src[:strings.Index(src, `"`)]
	img := f.request("GET", src, "", []*http.Cookie{c}, nil)
	if img.Code != 200 || img.Body.String() != png || img.Header().Get("Content-Type") != "image/png" || img.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatalf("avatar: %d %q %q", img.Code, img.Header().Get("Content-Type"), img.Header().Get("Cache-Control"))
	}
	if f.request("GET", src, "", nil, nil).Code != 404 || f.request("GET", "/account/avatar?v=stale", "", []*http.Cookie{c}, nil).Code != 404 {
		t.Fatal("avatar served without its owner or at a stale version")
	}
	if f.request("GET", src, "", []*http.Cookie{plain}, nil).Code != 404 {
		t.Fatal("avatar served to another account")
	}
	// Neither a picture from another host nor one that is not an image replaces the copy.
	for _, next := range []struct{ url, body string }{{"https://evil.example/a.png", png}, {"https://avatars.githubusercontent.com/u/7", "<html>not an image</html>"}} {
		f.avatarURL, f.avatar = next.url, []byte(next.body)
		again := f.login(7)
		if w := f.request("GET", src, "", []*http.Cookie{again}, nil); w.Code != 200 || w.Body.String() != png {
			t.Fatalf("%s replaced the picture", next.url)
		}
	}
}
func TestAccountWriteProtectionAndSessionRevocation(t *testing.T) {
	f := accountsForTest(t)
	first := f.login(4)
	second := f.login(4)
	u := f.session(first)
	body := url.Values{"csrf": {u.CSRF}}.Encode()
	if w := f.request("POST", "/account/session", body, []*http.Cookie{first}, map[string]string{"Origin": "https://evil.example"}); w.Code != 403 {
		t.Fatal("cross-origin logout accepted")
	}
	if w := f.request("POST", "/account/session", "csrf=wrong", []*http.Cookie{first}, nil); w.Code != 303 || w.Header().Get("Location") != "/account?message=session-changed" || f.session(first).ID != u.ID {
		t.Fatal("wrong CSRF accepted")
	}
	if w := f.request("POST", "/account/sessions", body, []*http.Cookie{first}, nil); w.Code != 303 {
		t.Fatal("other session revocation failed")
	}
	if w := f.request("GET", "/account/data", "", []*http.Cookie{second}, nil); w.Code != 401 {
		t.Fatal("revoked session remained usable")
	}
	if w := f.request("GET", "/account/data", "", []*http.Cookie{first}, nil); w.Code != 200 {
		t.Fatal("current session revoked too")
	}
	if w := f.request("POST", "/account/session", body, []*http.Cookie{first}, nil); w.Code != 303 {
		t.Fatal("logout failed")
	}
	if w := f.request("GET", "/account/data", "", []*http.Cookie{first}, nil); w.Code != 401 {
		t.Fatal("logged-out session usable")
	}
}
func TestAccountDeletionAndExport(t *testing.T) {
	f := accountsForTest(t)
	first := f.login(5)
	u := f.session(first)
	w := f.request("GET", "/account/export", "", []*http.Cookie{first}, nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("export unavailable")
	}
	body := url.Values{"csrf": {u.CSRF}, "confirm": {"delete"}}.Encode()
	if _, err := f.a.server.db.Exec(`UPDATE account_sessions SET created_at=?`, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if w = f.request("POST", "/account", body, []*http.Cookie{first}, nil); w.Code != 403 {
		t.Fatal("deletion without recent login accepted")
	}
	if _, err := f.a.server.db.Exec(`UPDATE account_sessions SET created_at=?`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if w = f.request("POST", "/account", body, []*http.Cookie{first}, nil); w.Code != 303 {
		t.Fatalf("deletion failed: %s", w.Body)
	}
	for _, table := range []string{"accounts", "account_sessions", "account_identities", "account_data"} {
		var n int
		if err := f.a.server.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s retained deleted account", table)
		}
	}
}
func TestAccountUnavailableAndConfiguredOrigin(t *testing.T) {
	f := accountsForTest(t)
	f.a.config.ClientID = ""
	w := f.request("GET", "/account", "", nil, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `action="/auth/github"`) {
		t.Fatal("unconfigured provider offered login")
	}
	if w = f.request("POST", "/auth/github", "", nil, nil); w.Code != 503 {
		t.Fatal("unconfigured provider accepted login")
	}
	r := httptest.NewRequest("GET", "https://attacker.example/account", nil)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != 421 {
		t.Fatal("request host trusted for account URL")
	}
}

func TestAccountConfigurationValidation(t *testing.T) {
	for _, key := range []string{"YAVCHN_PUBLIC_ORIGIN", "YAVCHN_GITHUB_CLIENT_ID", "YAVCHN_GITHUB_CLIENT_SECRET", "YAVCHN_GITHUB_CLIENT_SECRET_FILE"} {
		t.Setenv(key, "")
	}
	if _, err := loadAccountConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAVCHN_GITHUB_CLIENT_ID", "client")
	if _, err := loadAccountConfig(); err == nil {
		t.Fatal("partial credentials accepted")
	}
	t.Setenv("YAVCHN_GITHUB_CLIENT_SECRET", "secret")
	for _, origin := range []string{"", "http://beta.yavchn.com", "https://user:password@beta.yavchn.com", "https://beta.yavchn.com/account", "https://beta.yavchn.com/?next=evil"} {
		t.Setenv("YAVCHN_PUBLIC_ORIGIN", origin)
		if _, err := loadAccountConfig(); err == nil {
			t.Fatalf("invalid public origin accepted: %s", origin)
		}
	}
	t.Setenv("YAVCHN_PUBLIC_ORIGIN", "https://beta.yavchn.com/")
	if config, err := loadAccountConfig(); err != nil || config.Origin != "https://beta.yavchn.com" {
		t.Fatal("valid origin rejected")
	}
}
func TestAccountProviderBoundedResponses(t *testing.T) {
	f := accountsForTest(t)
	f.a.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", 65537) + `{}`)), Header: http.Header{}}, nil
	})
	r, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	var result any
	if f.a.providerJSON(r, &result) == nil {
		t.Fatal("accepted oversized provider response")
	}
}

func TestAccountBrowser(t *testing.T) {
	if os.Getenv("YAVCHN_BROWSER_TEST") == "" {
		t.Skip("set YAVCHN_BROWSER_TEST=1 to run browser verification")
	}
	f := accountsForTest(t)
	first, second := f.login(51), f.login(52)
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		t.Fatal(err)
	}
	f.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	srv := httptest.NewTLSServer(f.h)
	defer srv.Close()
	f.a.config.Origin = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../tests/accounts.browser.cjs", srv.URL, first.Value, second.Value)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("account browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}

func TestAccountAppletViews(t *testing.T) {
	f := accountsForTest(t)
	page := f.request("GET", "/account?view=classic", "", nil, nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `data-applet="account"`) || !strings.Contains(page.Body.String(), `data-view="classic"`) {
		t.Fatalf("Classic account: %d", page.Code)
	}
	win := f.request("GET", "/window/account", "", nil, nil)
	if win.Code != 200 || !strings.Contains(win.Body.String(), `data-win="account"`) || !strings.Contains(win.Body.String(), `data-win-size="content"`) {
		t.Fatalf("Account window: %d %s", win.Code, win.Body)
	}
	if win.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("window private cache policy missing")
	}
	target := "/lobsters/?view=window&open=reader-1,account&top=account"
	started := f.request("POST", "/auth/github", url.Values{"return_to": {target}}.Encode(), nil, nil)
	if started.Code != 303 {
		t.Fatalf("window login start: %d", started.Code)
	}
	u, _ := url.Parse(started.Header().Get("Location"))
	var stored string
	if err := f.a.server.db.QueryRow(`SELECT return_to FROM account_login_flows WHERE state_hash=?`, tokenHash(u.Query().Get("state"))).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	expected, _ := url.Parse(target)
	expected.RawQuery = expected.Query().Encode()
	if stored != expected.String() {
		t.Fatalf("workspace return: %s", stored)
	}
	workspace := f.request("GET", "/hn/?view=window&open=reader-1,account&top=account", "", nil, nil)
	if workspace.Code != 200 || !strings.Contains(workspace.Body.String(), `name="return_to" value="/hn/?open=reader-1%2Caccount&amp;top=account&amp;view=window"`) {
		t.Fatal("server-rendered workspace return missing")
	}
	windowed := f.request("GET", "/account?view=window", "", nil, nil)
	if windowed.Code != 303 || !strings.Contains(windowed.Header().Get("Location"), "open=account") {
		t.Fatal("Windowed account link does not launch its applet")
	}
}
func TestAccountReturnDestinations(t *testing.T) {
	for _, raw := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example/", "/auth/github/callback", "/hn/?view=classic", "/hn/?view=window&open=reader-1", "/unknown?view=window&open=account"} {
		r := httptest.NewRequest("POST", "/auth/github", nil)
		r.PostForm = url.Values{"return_to": {raw}}
		if got := accountReturn(r, ""); got != "/account" {
			t.Fatalf("accepted return %q: %s", raw, got)
		}
	}
}
