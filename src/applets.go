package main

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// The applets in the menu bar's Applets menu, each a window like a
// story's and each a page of its own:
//
//	replies               /applets/replies  replies to an HN user's recent comments and stories
//	hiring                /applets/hiring   the posts of HN's monthly "Who is hiring?" threads, filtered
//	user                  /user             a form that looks up a user
//	user-{source}-{name}  /user/{source}/{name}  that user's profile and recent activity
//
// The server renders each from the sites' public read APIs, as it renders
// stories, so every page works without script. The replies watcher and
// the hiring filter are PUDL applets that add live checking and filtering.

var (
	errNoSuchUser = errors.New("no such user")
	userNameRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
)

const whoIsHiringUser = "whoishiring"

// appVM is the body of an applet's window or page.
type appVM struct {
	AccountPanel      *accountPanelVM
	SettingsView      string
	SettingsPlacement string
	Kind              string    // "replies", "hiring", "lookup" or "profile"
	Lookup            *lookupVM // the lookup bar, above a profile and alone in the lookup
	Profile           *profileVM
	Replies           *repliesVM
	Hiring            *hiringVM
}

// lookupVM is the lookup bar: a user name and a site. In a window,
// submitting it shows the new profile in the window's place.
type lookupVM struct {
	Key, Name, Source string
}

type profileVM struct {
	Source, SourceLabel, Name string
	Since                     string
	Karma                     int
	HasKarma                  bool
	Roles                     []string
	About                     template.HTML
	SiteURL                   string
	SiteLabel                 string
	Submissions               []activityVM // the user's stories, newest first
	SubmissionsNote           string       // why there are none, when the site could not say
	Comments                  []activityVM // the user's recent comments
	CommentsNote              string       // why there are none to show
	Error                     string
}

type activityVM struct {
	Key, PageURL, Title string
	Comment             bool
	Excerpt             string
	Age                 string
	Score, Comments     int
}

type repliesVM struct {
	User    string
	Checked bool // the server looked; without a user it renders the prompt
	Replies []replyVM
	Error   string
}

type replyVM struct {
	Key, PageURL         string
	Author               string
	AuthorKey, AuthorURL string
	Age                  string
	Time                 int64
	HTML                 template.HTML
	ParentKey, ParentURL string
	ParentLabel          string
	ReplyURL             string
}

type hiringVM struct {
	Threads  []hiringThreadVM
	ThreadID string
	Query    string
	Loaded   bool // the posts are in the page, as on the applet's own page
	Jobs     []jobVM
	Total    int
	Error    string
}

type hiringThreadVM struct {
	ID, Title string
	Selected  bool
}

type jobVM struct {
	Author               string
	AuthorKey, AuthorURL string
	Age                  string
	HTML                 template.HTML
	URL                  string
	Hidden               bool
}

// profilePlace floats a profile at the right, beside the story it was
// opened from.
var profilePlace = floatingAt(0.5, 0.05, 0.46, 0.86)

func profileKey(source, name string) string { return "user-" + source + "-" + name }
func profileURL(source, name string) string { return "/user/" + source + "/" + name }

// authorLink is the profile window a commenter's name opens, or empty for
// a name that cannot be one, such as HN's "[deleted]".
func authorLink(source, name string) (key, href string) {
	if !userNameRE.MatchString(name) {
		return "", ""
	}
	return profileKey(source, name), profileURL(source, name)
}

// appletWindow builds the window for an applet's key, or reports false
// for a key that is not one.
func (s *Server) appletWindow(ctx context.Context, r *http.Request, key string) (windowVM, bool) {
	w := windowVM{Key: key, MinHref: "?", MaxHref: "?", CloseHref: "?"}
	q := r.URL.Query()
	switch {
	case key == "account":
		panel, err := s.accounts.panel(r)
		if err != nil {
			return windowVM{}, false
		}
		w.ContentSized = true
		w.Title, w.PageURL, w.Def = "Your YAVCHN account", "/account?view=classic", floatingAt(0.12, 0.05, 0.48, 0.86)
		w.App = &appVM{Kind: "account", AccountPanel: panel}
	case key == "settings":
		w.IconGlyph = "gear"
		w.ContentSized = true
		w.Title, w.PageURL, w.Def = "Settings", "/settings", floatingAt(0.12, 0.05, 0.48, 0.86)
		w.App = &appVM{Kind: "settings", SettingsView: savedView(r), SettingsPlacement: savedReaderPlacement(r)}
	case key == "replies":
		// A watcher stands beside the reading, so it docks at the right.
		w.Title, w.PageURL, w.Def = "Replies to me", "/applets/replies", dockedAt("right", 0.3)
		w.App = &appVM{Kind: "replies", Replies: &repliesVM{}}
	case key == "hiring":
		w.Title, w.PageURL, w.Def = "Who is hiring?", "/applets/hiring", floatingAt(0.04, 0.03, 0.72, 0.92)
		h := s.hiring(ctx, q.Get("t"), q.Get("q"))
		w.App = &appVM{Kind: "hiring", Hiring: h}
	case key == "user":
		// The lookup opens where a profile does, since the profile it
		// looks up takes its place.
		w.Title, w.PageURL, w.Def = "Look up a user", "/user", profilePlace
		w.App = &appVM{Kind: "lookup", Lookup: &lookupVM{Key: key, Source: "hn"}}
	case strings.HasPrefix(key, "user-"):
		source, name, ok := strings.Cut(strings.TrimPrefix(key, "user-"), "-")
		if _, known := s.sources[source]; !ok || !known || !userNameRE.MatchString(name) {
			return windowVM{}, false
		}
		p := s.profile(ctx, source, name)
		w.Title, w.PageURL, w.Def = p.Name+" on "+p.SourceLabel, profileURL(source, name), profilePlace
		w.App = &appVM{Kind: "profile", Profile: p, Lookup: &lookupVM{Key: key, Name: name, Source: source}}
	default:
		return windowVM{}, false
	}
	w.Mode, w.Style, w.Edge = w.Def.Mode, w.Def.Style, w.Def.Edge
	return w, true
}

// --- profiles ---

func (s *Server) profile(ctx context.Context, source, name string) *profileVM {
	p := &profileVM{Source: source, Name: name, SourceLabel: s.sources[source].Label()}
	switch source {
	case "hn":
		p.SiteURL, p.SiteLabel = "https://news.ycombinator.com/user?id="+url.QueryEscape(name), "Open on HN"
		// The submissions come from Algolia's search, which finds a user's
		// stories however many comments they have posted since; the
		// profile's own list of items is mostly comments for most users.
		var stories []*SearchHit
		var storiesErr error
		done := make(chan struct{})
		go func() { defer close(done); stories, storiesErr = s.hn.AuthorStories(ctx, name) }()
		u, err := s.hn.User(ctx, name)
		<-done
		if err != nil {
			p.Error = userError(err, name, p.SourceLabel)
			return p
		}
		p.Name = u.ID
		p.Since = time.Unix(u.Created, 0).UTC().Format("2 January 2006")
		p.Karma, p.HasKarma = u.Karma, true
		p.About = template.HTML(linkThreads(sanitizeHTML(u.About)))
		if storiesErr != nil {
			slog.Warn("author stories unavailable", "user", name, "err", storiesErr)
			p.SubmissionsNote = "The submissions couldn't be loaded right now."
		}
		for _, h := range stories {
			p.Submissions = append(p.Submissions, activityVM{Key: "hn-" + h.ID, PageURL: storyPageURL("hn", h.ID),
				Title: h.Title, Age: relTime(h.CreatedAt), Score: h.Points, Comments: h.NumComments})
		}
		ids := make([]string, 0, 30)
		for i, id := range u.Submitted {
			if i == 30 {
				break
			}
			ids = append(ids, strconv.FormatInt(id, 10))
		}
		for _, it := range s.hn.ItemsParallel(ctx, ids) {
			if it == nil || it.Dead || it.Deleted || it.Type != "comment" || len(p.Comments) == 15 {
				continue
			}
			p.Comments = append(p.Comments, activityVM{Key: "hn-" + it.ID, PageURL: storyPageURL("hn", it.ID),
				Comment: true, Excerpt: excerpt(it.Text, 200), Age: relTime(it.Time)})
		}
	case "lobsters":
		lob, _ := s.sources["lobsters"].(*Lobsters)
		p.SiteURL, p.SiteLabel = "https://lobste.rs/~"+url.PathEscape(name), "Open on Lobsters"
		if lob == nil {
			p.Error = "Lobsters profiles are unavailable."
			return p
		}
		u, err := lob.User(ctx, name)
		if err != nil {
			p.Error = userError(err, name, p.SourceLabel)
			return p
		}
		p.Name = u.Username
		if t := (&lobstersStory{CreatedAt: u.CreatedAt}).timeUnix(); t > 0 {
			p.Since = time.Unix(t, 0).UTC().Format("2 January 2006")
		}
		if u.IsAdmin {
			p.Roles = append(p.Roles, "Administrator")
		}
		if u.IsModerator {
			p.Roles = append(p.Roles, "Moderator")
		}
		p.About = template.HTML(linkThreads(sanitizeHTML(u.About)))
		for _, it := range u.Stories {
			p.Submissions = append(p.Submissions, activityVM{Key: "lobsters-" + it.ID, PageURL: storyPageURL("lobsters", it.ID),
				Title: it.Title, Age: relTime(it.Time), Score: it.Score, Comments: it.Descendants})
		}
		p.CommentsNote = "Lobsters publishes a user's stories, but not their comments."
	}
	return p
}

func userError(err error, name, site string) string {
	if errors.Is(err, errNoSuchUser) {
		return "There is no user called " + name + " on " + site + "."
	}
	slog.Warn("profile fetch failed", "user", name, "site", site, "err", err)
	return "The profile couldn't be loaded right now."
}

// excerpt is the first n characters of an HTML fragment's text.
func excerpt(fragment string, n int) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(fragment))
	for b.Len() < n*2 {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.TextToken:
			b.Write(z.Text())
		case html.StartTagToken:
			if name, _ := z.TagName(); string(name) == "p" && b.Len() > 0 {
				b.WriteByte(' ')
			}
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(text); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "…"
	}
	return text
}

// --- replies ---

// hnReplies lists the replies to an HN user's most recent items, newest
// first. HN offers no list of replies, so this reads the user's last 30
// items and the replies to each; a reply to anything older is not seen.
func (s *Server) hnReplies(ctx context.Context, name string) *repliesVM {
	vm := &repliesVM{User: name, Checked: true}
	u, err := s.hn.User(ctx, name)
	if err != nil {
		vm.Error = userError(err, name, "Hacker News")
		return vm
	}
	vm.User = u.ID
	ids := make([]string, 0, 30)
	for i, id := range u.Submitted {
		if i == 30 {
			break
		}
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	parents := map[string]*Item{}
	var kids []string
	for _, it := range s.hn.ItemsParallel(ctx, ids) {
		if it == nil || it.Dead || it.Deleted {
			continue
		}
		parents[it.ID] = it
		for _, k := range it.Kids {
			if len(kids) < 100 {
				kids = append(kids, k)
			}
		}
	}
	for _, it := range s.hn.ItemsParallel(ctx, kids) {
		if it == nil || it.Dead || it.Deleted || it.By == u.ID {
			continue
		}
		parent := parents[it.Parent]
		if parent == nil {
			continue
		}
		rp := replyVM{
			Key: "hn-" + it.ID, PageURL: storyPageURL("hn", it.ID),
			Author: it.By, Age: relTime(it.Time), Time: it.Time,
			HTML:      template.HTML(linkThreads(sanitizeHTML(it.Text))),
			ParentKey: "hn-" + parent.ID, ParentURL: storyPageURL("hn", parent.ID),
			ReplyURL: "https://news.ycombinator.com/reply?id=" + it.ID,
		}
		rp.AuthorKey, rp.AuthorURL = authorLink("hn", it.By)
		if parent.Type == "comment" {
			rp.ParentLabel = "your comment: " + excerpt(parent.Text, 80)
		} else {
			rp.ParentLabel = parent.Title
		}
		vm.Replies = append(vm.Replies, rp)
	}
	sort.Slice(vm.Replies, func(i, j int) bool { return vm.Replies[i].Time > vm.Replies[j].Time })
	return vm
}

// --- who is hiring ---

// hiringThreads is HN's monthly threads, newest first, from the account
// that posts them.
func (s *Server) hiringThreads(ctx context.Context) ([]hiringThreadVM, error) {
	u, err := s.hn.User(ctx, whoIsHiringUser)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, 12)
	for i, id := range u.Submitted {
		if i == 12 {
			break
		}
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	var out []hiringThreadVM
	for _, it := range s.hn.ItemsParallel(ctx, ids) {
		if it != nil && !it.Dead && !it.Deleted && strings.HasPrefix(it.Title, "Ask HN:") {
			out = append(out, hiringThreadVM{ID: it.ID, Title: strings.TrimPrefix(it.Title, "Ask HN: ")})
		}
	}
	return out, nil
}

// hiring is the applet's body: the threads to choose from, with the
// chosen one (by default the newest "Who is hiring?") marked.
func (s *Server) hiring(ctx context.Context, thread, query string) *hiringVM {
	vm := &hiringVM{Query: strings.TrimSpace(query)}
	threads, err := s.hiringThreads(ctx)
	if err != nil || len(threads) == 0 {
		slog.Warn("hiring threads unavailable", "err", err)
		vm.Error = "HN's hiring threads couldn't be loaded right now."
		return vm
	}
	for _, t := range threads {
		if t.ID == thread {
			vm.ThreadID = t.ID
		}
	}
	if vm.ThreadID == "" {
		vm.ThreadID = threads[0].ID
		for _, t := range threads {
			if strings.HasPrefix(t.Title, "Who is hiring") {
				vm.ThreadID = t.ID
				break
			}
		}
	}
	for i := range threads {
		threads[i].Selected = threads[i].ID == vm.ThreadID
	}
	vm.Threads = threads
	return vm
}

// loadJobs puts the chosen thread's posts into vm, each top-level comment
// one post, marking those the query leaves out as hidden.
func (s *Server) loadJobs(ctx context.Context, vm *hiringVM, ip string) {
	vm.Loaded = true
	thread, err := s.sources["hn"].StoryThread(ctx, vm.ThreadID, ip)
	if err != nil {
		if errors.Is(err, errRateLimited) {
			vm.Error = "You've hit the per-visitor rate limit. Wait a minute and try again."
		} else {
			slog.Warn("hiring thread fetch failed", "id", vm.ThreadID, "err", err)
			vm.Error = "This thread couldn't be loaded right now."
		}
		return
	}
	terms := parseTerms(vm.Query)
	for _, c := range thread.Comments {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		j := jobVM{Author: c.Author, Age: relTime(c.CreatedAt),
			HTML: template.HTML(linkThreads(sanitizeHTML(c.Text))),
			URL:  "https://news.ycombinator.com/item?id=" + c.ID}
		j.AuthorKey, j.AuthorURL = authorLink("hn", c.Author)
		j.Hidden = !terms.match(excerpt(c.Text, 1<<20) + " " + c.Author)
		if !j.Hidden {
			vm.Total++
		}
		vm.Jobs = append(vm.Jobs, j)
	}
}

// terms is a filter: words every post must contain, "quoted phrases"
// taken whole, and -words no post may contain. hiring.js reads the same
// grammar as the reader types.
type terms struct{ want, not []string }

var termRE = regexp.MustCompile(`-?"[^"]*"|\S+`)

func parseTerms(q string) terms {
	var t terms
	for _, tok := range termRE.FindAllString(strings.ToLower(q), -1) {
		neg := strings.HasPrefix(tok, "-") && len(tok) > 1
		tok = strings.Trim(strings.TrimPrefix(tok, "-"), `"`)
		if tok == "" {
			continue
		}
		if neg {
			t.not = append(t.not, tok)
		} else {
			t.want = append(t.want, tok)
		}
	}
	return t
}

func (t terms) match(text string) bool {
	text = strings.ToLower(text)
	for _, w := range t.want {
		if !strings.Contains(text, w) {
			return false
		}
	}
	for _, n := range t.not {
		if strings.Contains(text, n) {
			return false
		}
	}
	return true
}

// --- handlers ---

type appPageVM struct {
	menuContext
	NewReaderHref string
	Title         string
	AllSources    []sourceOptVM
	Win           windowVM
}

func (s *Server) renderAppPage(w http.ResponseWriter, r *http.Request, win windowVM) {
	w.Header().Add("Vary", "Cookie")
	vm := appPageVM{menuContext: pageMenu(r, win.Key, "hn"), NewReaderHref: "/hn/?open=reader-1&top=reader-1", Title: win.Title + " · YAVCHN", AllSources: s.buildSourceOpts(""), Win: win}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for i := range vm.AllSources {
		vm.AllSources[i].URL = viewURL(vm.AllSources[i].URL, "classic")
	}
	if err := s.tpl.ExecuteTemplate(w, "applet.html.tmpl", vm); err != nil {
		slog.Error("render applet page", "key", win.Key, "err", err)
	}
}

func (s *Server) appletPage(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		win, _ := s.appletWindow(ctx, r, key)
		q := r.URL.Query()
		switch key {
		case "replies":
			if u := strings.TrimSpace(q.Get("u")); userNameRE.MatchString(u) {
				if !s.appletRate.Allow(clientIP(r)) {
					win.App.Replies = &repliesVM{User: u, Checked: true, Error: "You've hit the per-visitor rate limit. Wait a minute and try again."}
				} else {
					win.App.Replies = s.hnReplies(ctx, u)
				}
			}
		case "hiring":
			if win.App.Hiring.Error == "" {
				s.loadJobs(ctx, win.App.Hiring, clientIP(r))
			}
		}
		s.renderAppPage(w, r, win)
	}
}

// UserLookup serves /user: the form, or with a name, the profile it names.
func (s *Server) UserLookup(w http.ResponseWriter, r *http.Request) {
	name, source := strings.TrimSpace(r.URL.Query().Get("name")), r.URL.Query().Get("source")
	if _, ok := s.sources[source]; ok && userNameRE.MatchString(name) {
		http.Redirect(w, r, profileURL(source, name), http.StatusSeeOther)
		return
	}
	s.appletPage("user")(w, r)
}

// UserPage serves /user/{source}/{name}, a profile as a page of its own.
func (s *Server) UserPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	win, ok := s.appletWindow(ctx, r, profileKey(r.PathValue("source"), r.PathValue("name")))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderAppPage(w, r, win)
}

// RepliesAPI serves /api/replies?u=, the replies list the watcher checks.
func (s *Server) RepliesAPI(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSpace(r.URL.Query().Get("u"))
	vm := &repliesVM{User: u, Checked: true}
	switch {
	case !userNameRE.MatchString(u):
		vm.Error = "That isn't a Hacker News user name."
	case !s.appletRate.Allow(clientIP(r)):
		w.Header().Set("Retry-After", "60")
		vm.Error = "You've hit the per-visitor rate limit. Wait a minute and try again."
	default:
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		vm = s.hnReplies(ctx, u)
	}
	s.renderFragment(w, "replieslist", vm)
}

// HiringAPI serves /api/hiring?id=, one thread's posts, all shown, for
// the applet to filter as the reader types.
func (s *Server) HiringAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	vm := s.hiring(ctx, r.URL.Query().Get("id"), "")
	if vm.Error == "" {
		s.loadJobs(ctx, vm, clientIP(r))
	}
	s.renderFragment(w, "hiringposts", vm)
}

func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render fragment", "name", name, "err", err)
	}
}

// --- shared ---

// window is the window for any key: a story's or an applet's.
func (s *Server) window(ctx context.Context, r *http.Request, key string) (result windowVM, found bool) {
	defer func() {
		if found && result.App == nil && savedReaderPlacement(r) == "maximized" {
			result.Def.Mode = "maximized"
			result.Mode = "maximized"
		}
	}()
	if readerKeyRE.MatchString(key) || r.URL.Query().Has("r."+key) {
		article := r.URL.Query().Get("r." + key)
		if article == "" {
			w := readerWindow()
			w.Key = key
			return w, true
		}
		w, ok := s.storyWindow(ctx, article)
		if !ok {
			return windowVM{}, false
		}
		w.Key = key
		w.Def = floatingAt(0.06, 0.05, 0.55, 0.75)
		w.Mode, w.Style = w.Def.Mode, w.Def.Style
		return w, true
	}
	if key == "story" {
		return readerWindow(), true
	}
	if w, ok := s.appletWindow(ctx, r, key); ok {
		return w, true
	}
	return s.storyWindow(ctx, key)
}

// windowsParallel builds the windows for keys concurrently.
func (s *Server) windowsParallel(ctx context.Context, r *http.Request, keys []string) map[string]windowVM {
	got := make(map[string]windowVM, len(keys))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			if w, ok := s.window(ctx, r, k); ok {
				mu.Lock()
				got[k] = w
				mu.Unlock()
			}
		}(k)
	}
	wg.Wait()
	return got
}
