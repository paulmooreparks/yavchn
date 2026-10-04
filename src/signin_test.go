package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// signinFixture is an account fixture whose fake network answers for
// GitHub, GitHub's pictures and Resend, recording every message sent.
type signinFixture struct {
	*accountFixture
	githubID   int64
	mails      []map[string]any
	mailStatus int
}

func signinForTest(t *testing.T) *signinFixture {
	f := &signinFixture{accountFixture: accountsForTest(t), mailStatus: 200}
	f.a.config.ResendKey, f.a.config.EmailFrom, f.a.config.EmailReplyTo = "re_test", "YAVCHN <signin@example.com>", "help@example.com"
	f.a.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "api.resend.com":
			if r.Header.Get("Authorization") != "Bearer re_test" {
				t.Fatal("mail sent without the Resend key")
			}
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mails = append(f.mails, m)
			return jsonResp(f.mailStatus, `{"id":"1"}`), nil
		case "github.com":
			return jsonResp(200, `{"access_token":"transient-token","token_type":"bearer"}`), nil
		case "api.github.com":
			return jsonResp(200, fmt.Sprintf(`{"id":%d,"login":"user%d","avatar_url":"https://avatars.githubusercontent.com/u/%d"}`, f.githubID, f.githubID, f.githubID)), nil
		case "avatars.githubusercontent.com":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\ngithub")), Header: http.Header{}}, nil
		}
		t.Fatalf("unexpected request to %s", r.URL.Host)
		return nil, nil
	})
	return f
}

var linkPattern = regexp.MustCompile(`https://beta\.yavchn\.com/auth/email/link\?token=([A-Za-z0-9_-]{43})`)

// requestLink asks for an email link with the given cookies and token, and
// returns the answer's message and the link's token, if a message was sent.
func (f *signinFixture) requestLink(address string, cookies []*http.Cookie, csrf string, link bool) (string, string) {
	f.t.Helper()
	form := url.Values{"csrf": {csrf}, "email": {address}, "return_to": {"/account"}}
	if link {
		form.Set("link", "1")
	}
	before := len(f.mails)
	w := f.request("POST", "/auth/email", form.Encode(), cookies, nil)
	if w.Code != 303 {
		f.t.Fatalf("email request: %d %s", w.Code, w.Body)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	token := ""
	if len(f.mails) > before {
		m := linkPattern.FindStringSubmatch(f.mails[len(f.mails)-1]["text"].(string))
		if m == nil {
			f.t.Fatalf("message lacks a link: %v", f.mails[len(f.mails)-1])
		}
		token = m[1]
	}
	return loc.Query().Get("message"), token
}

func (f *signinFixture) loginToken() *http.Cookie {
	f.t.Helper()
	return cookieNamed(f.t, f.request("GET", "/account", "", nil, nil), loginCookie)
}

func (f *signinFixture) redeem(token string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	return f.request("POST", "/auth/email/link", url.Values{"token": {token}}.Encode(), cookies, nil)
}

// github signs in with a GitHub user through this fixture's fake network.
func (f *signinFixture) github(id int64) *http.Cookie {
	f.t.Helper()
	f.githubID = id
	login := f.loginToken()
	w := f.request("POST", "/auth/github", url.Values{"csrf": {login.Value}, "return_to": {"/account"}}.Encode(), []*http.Cookie{login}, nil)
	loc, _ := url.Parse(w.Header().Get("Location"))
	w = f.request("GET", "/auth/github/callback?state="+loc.Query().Get("state")+"&code=code", "", []*http.Cookie{cookieNamed(f.t, w, flowCookie)}, nil)
	return cookieNamed(f.t, w, sessionCookie)
}

// githubLink runs the GitHub flow from inside the signed-in account.
func (f *signinFixture) githubLink(id int64, session *http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	f.githubID = id
	u := f.session(session)
	w := f.request("POST", "/auth/github", url.Values{"csrf": {u.CSRF}, "link": {"1"}, "return_to": {"/account"}}.Encode(), []*http.Cookie{session}, nil)
	if w.Code != 303 {
		f.t.Fatalf("link start: %d %s", w.Code, w.Body)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	return f.request("GET", "/auth/github/callback?state="+loc.Query().Get("state")+"&code=code", "", []*http.Cookie{cookieNamed(f.t, w, flowCookie), session}, nil)
}

func identitiesOf(t *testing.T, f *signinFixture, accountID string) []string {
	t.Helper()
	rows, err := f.a.server.db.Query(`SELECT provider||':'||subject FROM account_identities WHERE account_id=? ORDER BY 1`, accountID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestEmailLinkSignIn(t *testing.T) {
	f := signinForTest(t)
	login := f.loginToken()
	if msg, _ := f.requestLink("not an address", []*http.Cookie{login}, login.Value, false); msg != "email-refused" {
		t.Fatalf("invalid address: %s", msg)
	}
	if w := f.request("POST", "/auth/email", url.Values{"csrf": {"forged"}, "email": {"a@example.com"}}.Encode(), []*http.Cookie{login}, nil); w.Code != 403 {
		t.Fatal("email request accepted without the page's token")
	}
	msg, first := f.requestLink(" Reader@Example.com ", []*http.Cookie{login}, login.Value, false)
	if msg != "email-sent" || first == "" || f.mails[0]["to"].([]any)[0] != "reader@example.com" || f.mails[0]["reply_to"] != "help@example.com" {
		t.Fatalf("sign-in link: %s %v", msg, f.mails)
	}
	_, second := f.requestLink("reader@example.com", []*http.Cookie{login}, login.Value, false)
	if f.redeem(first, nil).Header().Get("Location") != "/account?message=link-expired" {
		t.Fatal("a newer link did not replace the older one")
	}
	// Opening the link only shows its confirmation; the scanner's fetch spends nothing.
	for range 2 {
		page := f.request("GET", "/auth/email/link?token="+second, "", nil, nil)
		if !strings.Contains(page.Body.String(), "reader@example.com") || !strings.Contains(page.Body.String(), `name="token" value="`+second+`"`) || page.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("confirmation page: %d", page.Code)
		}
	}
	w := f.redeem(second, nil)
	session := cookieNamed(t, w, sessionCookie)
	if w.Code != 303 || w.Header().Get("Location") != "/account" {
		t.Fatalf("redeem: %d %s", w.Code, w.Header().Get("Location"))
	}
	u := f.session(session)
	if u.Username != "reader@example.com" || strings.Join(identitiesOf(t, f, u.ID), ",") != "email:reader@example.com" {
		t.Fatalf("new account: %s %v", u.Username, identitiesOf(t, f, u.ID))
	}
	if f.redeem(second, nil).Header().Get("Location") != "/account?message=link-expired" {
		t.Fatal("a link was used twice")
	}
	if page := f.request("GET", "/auth/email/link?token="+second, "", nil, nil); !strings.Contains(page.Body.String(), "This link has expired") {
		t.Fatal("a spent link still offered to sign in")
	}
	// A second sign-in with the address reaches the same account.
	_, again := f.requestLink("reader@example.com", []*http.Cookie{login}, login.Value, false)
	if f.session(cookieNamed(t, f.redeem(again, nil), sessionCookie)).ID != u.ID {
		t.Fatal("the address signed in to a different account")
	}
	// A message Resend refuses cancels its link.
	f.mailStatus = 500
	_, refused := f.requestLink("other@example.com", []*http.Cookie{login}, login.Value, false)
	if f.redeem(refused, nil).Header().Get("Location") != "/account?message=link-expired" {
		t.Fatal("an undelivered link still worked")
	}
}

func TestEmailBudget(t *testing.T) {
	f := signinForTest(t)
	login := f.loginToken()
	for i := range 6 {
		msg, token := f.requestLink("busy@example.com", []*http.Cookie{login}, login.Value, false)
		if msg != "email-sent" || (i < 5) != (token != "") {
			t.Fatalf("request %d: %s sent=%v", i, msg, token != "")
		}
	}
}

func TestLinkingIdentities(t *testing.T) {
	f := signinForTest(t)
	f.githubID = 31
	gh := f.github(31)
	owner := f.session(gh)
	// An address added from inside the account must be confirmed in the same browser.
	msg, token := f.requestLink("mine@example.com", []*http.Cookie{gh}, owner.CSRF, true)
	if msg != "email-link-sent" || !strings.Contains(f.mails[len(f.mails)-1]["text"].(string), "add this address") {
		t.Fatalf("link request: %s", msg)
	}
	if loc := f.redeem(token, nil).Header().Get("Location"); loc != "/account?message=link-elsewhere" {
		t.Fatalf("another browser confirmed the link: %s", loc)
	}
	w := f.redeem(token, []*http.Cookie{gh})
	if loc := w.Header().Get("Location"); loc != "/account?message=email-linked" {
		t.Fatalf("link: %s", loc)
	}
	gh = cookieNamed(t, w, sessionCookie)
	if got := strings.Join(identitiesOf(t, f, owner.ID), ","); got != "email:mine@example.com,github:31" {
		t.Fatalf("identities: %s", got)
	}
	// An address another account holds is refused, not moved.
	login := f.loginToken()
	_, other := f.requestLink("theirs@example.com", []*http.Cookie{login}, login.Value, false)
	theirs := f.session(cookieNamed(t, f.redeem(other, nil), sessionCookie))
	_, token = f.requestLink("theirs@example.com", []*http.Cookie{gh}, f.session(gh).CSRF, true)
	if loc := f.redeem(token, []*http.Cookie{gh}).Header().Get("Location"); loc != "/account?message=identity-taken" {
		t.Fatalf("taken address: %s", loc)
	}
	if got := strings.Join(identitiesOf(t, f, theirs.ID), ","); got != "email:theirs@example.com" {
		t.Fatalf("the other account lost its address: %s", got)
	}
	// GitHub links the same way from inside an email account.
	_, token = f.requestLink("solo@example.com", []*http.Cookie{login}, login.Value, false)
	solo := cookieNamed(t, f.redeem(token, nil), sessionCookie)
	soloID := f.session(solo).ID
	if loc := f.githubLink(31, solo).Header().Get("Location"); loc != "/account?message=identity-taken" {
		t.Fatalf("GitHub user of another account: %s", loc)
	}
	w = f.githubLink(32, solo)
	if loc := w.Header().Get("Location"); loc != "/account?message=github-linked" {
		t.Fatalf("GitHub link: %s", loc)
	}
	solo = cookieNamed(t, w, sessionCookie)
	if got := strings.Join(identitiesOf(t, f, soloID), ","); got != "email:solo@example.com,github:32" {
		t.Fatalf("identities: %s", got)
	}
	// Removing a way in keeps at least one.
	remove := func(provider, subject string) string {
		u := f.session(solo)
		return f.request("POST", "/account/identities/remove", url.Values{"csrf": {u.CSRF}, "provider": {provider}, "subject": {subject}}.Encode(), []*http.Cookie{solo}, nil).Header().Get("Location")
	}
	if loc := remove("github", "32"); loc != "/account?message=identity-removed" {
		t.Fatalf("remove: %s", loc)
	}
	if loc := remove("email", "solo@example.com"); loc != "/account?message=last-identity" {
		t.Fatalf("removed the last identity: %s", loc)
	}
	if got := strings.Join(identitiesOf(t, f, soloID), ","); got != "email:solo@example.com" {
		t.Fatalf("identities after removal: %s", got)
	}
}

func upload(t *testing.T, f *signinFixture, session *http.Cookie, image string) string {
	t.Helper()
	u := f.session(session)
	var b strings.Builder
	m := multipart.NewWriter(&b)
	_ = m.WriteField("csrf", u.CSRF)
	_ = m.WriteField("return_to", "/account")
	part, _ := m.CreateFormFile("picture", "picture.webp")
	_, _ = part.Write([]byte(image))
	_ = m.Close()
	w := f.request("POST", "/account/avatar", b.String(), []*http.Cookie{session}, map[string]string{"Content-Type": m.FormDataContentType()})
	return w.Header().Get("Location")
}

func TestUploadedPictures(t *testing.T) {
	f := signinForTest(t)
	f.githubID = 41
	c := f.github(41)
	picture := func() string {
		var kind string
		var image []byte
		if err := f.a.server.db.QueryRow(`SELECT content_type,image FROM account_avatars v JOIN account_sessions s ON s.account_id=v.account_id WHERE s.token_hash=?`, tokenHash(c.Value)).Scan(&kind, &image); err != nil {
			return ""
		}
		return kind + ":" + string(image)
	}
	if got := picture(); got != "image/png:\x89PNG\r\n\x1a\ngithub" {
		t.Fatalf("GitHub copy: %q", got)
	}
	webp := "RIFF\x00\x00\x00\x00WEBPVP8 uploaded"
	for _, refused := range []string{"GIF89a", "<svg/>", strings.Repeat("\xff\xd8\xff", pictureLimit/3+1)} {
		if loc := upload(t, f, c, refused); loc != "/account?message=picture-refused" {
			t.Fatalf("accepted a refused picture: %s", loc)
		}
	}
	if loc := upload(t, f, c, webp); loc != "/account?message=picture-saved" || picture() != "image/webp:"+webp {
		t.Fatalf("upload: %s %q", loc, picture())
	}
	// A GitHub sign-in leaves an uploaded picture alone.
	c = f.github(41)
	if picture() != "image/webp:"+webp {
		t.Fatal("GitHub sign-in replaced the uploaded picture")
	}
	page := f.request("GET", "/account", "", []*http.Cookie{c}, nil).Body.String()
	if !strings.Contains(page, "Use my GitHub picture") || !strings.Contains(page, "Remove picture") {
		t.Fatal("picture choices missing")
	}
	post := func(path string) string {
		u := f.session(c)
		return f.request("POST", path, url.Values{"csrf": {u.CSRF}, "return_to": {"/account"}}.Encode(), []*http.Cookie{c}, nil).Header().Get("Location")
	}
	if loc := post("/account/avatar/remove"); loc != "/account?message=picture-removed" || picture() != "" {
		t.Fatalf("remove: %s", loc)
	}
	c = f.github(41)
	if picture() != "" {
		t.Fatal("GitHub sign-in restored a removed picture")
	}
	if loc := post("/account/avatar/github"); loc != "/account?message=picture-github" || picture() != "image/png:\x89PNG\r\n\x1a\ngithub" {
		t.Fatalf("GitHub picture: %s %q", loc, picture())
	}
}

func TestSigninBrowser(t *testing.T) {
	if os.Getenv("YAVCHN_BROWSER_TEST") == "" {
		t.Skip("set YAVCHN_BROWSER_TEST=1 to run browser verification")
	}
	f := signinForTest(t)
	session := f.github(61)
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		t.Fatal(err)
	}
	f.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	srv := httptest.NewTLSServer(f.h)
	defer srv.Close()
	// WebAuthn accepts localhost as a relying party, though not an IP address.
	f.a.config.Origin = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if err := f.a.configurePasskeys(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../tests/signin.browser.cjs", f.a.config.Origin, session.Value)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sign-in browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}

func TestPasskeyEndpointsRefuseForgedRequests(t *testing.T) {
	f := signinForTest(t)
	login := f.loginToken()
	headers := map[string]string{"Content-Type": "application/json", "X-CSRF-Token": "forged"}
	if w := f.request("POST", "/auth/passkey/options", `{}`, []*http.Cookie{login}, headers); w.Code != 403 {
		t.Fatalf("forged options: %d", w.Code)
	}
	headers["X-CSRF-Token"] = login.Value
	w := f.request("POST", "/auth/passkey/options", `{"return_to":"https://evil.example/"}`, []*http.Cookie{login}, headers)
	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil || options.PublicKey.Challenge == "" || options.PublicKey.RPID != "beta.yavchn.com" {
		t.Fatalf("options: %d %s", w.Code, w.Body)
	}
	ceremony := cookieNamed(t, w, ceremonyCookie)
	var target string
	_ = f.a.server.db.QueryRow(`SELECT return_to FROM account_ceremonies WHERE token_hash=?`, tokenHash(ceremony.Value)).Scan(&target)
	if target != "/account" {
		t.Fatalf("a passkey ceremony kept a foreign destination: %s", target)
	}
	if w := f.request("POST", "/auth/passkey", `{"credential":{}}`, []*http.Cookie{login, ceremony}, headers); w.Code != 401 {
		t.Fatalf("a malformed assertion: %d", w.Code)
	}
	if w := f.request("POST", "/auth/passkey", `{"credential":{}}`, []*http.Cookie{login, ceremony}, headers); w.Code != 400 {
		t.Fatalf("a ceremony was used twice: %d", w.Code)
	}
	if w := f.request("POST", "/account/passkeys/options", `{}`, []*http.Cookie{login}, headers); w.Code != 401 {
		t.Fatalf("passkey added without an account: %d", w.Code)
	}
}
