package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Stories open as PUDL floating windows over the list. The window grammar
// is PUDL's own (its README, "Floating windows"): the address carries
// open, top, min and p.<key>. The server reads it to render the windows an
// address names, so they show before any script runs, and to write the
// link each window button stands for. Once the page is up, pudl-windows.js
// owns the grammar and keeps every link current.
//
// A story's window key is "<source>-<id>", as hn-41234567 or lobsters-abc123.

var (
	winKeyRE    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	storyIDRE   = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	placementRE = regexp.MustCompile(`^([a-z-]+):([0-9.]+(?:,[0-9.]+)*)$`)
)

// winModes maps each placement mode to the count of numbers it carries:
// the floating geometry, then a zone's rectangle or a dock's strip size.
var winModes = map[string]int{
	"floating": 4, "maximized": 4, "left": 4, "right": 4, "zone": 8,
	"dock-top": 5, "dock-bottom": 5, "dock-left": 5, "dock-right": 5,
}

// storyWinMode is the mode a story window opens in when the address gives
// it no placement. A story is something to read, so it opens maximised,
// as PUDL's article reader does, and Restore floats it at storyWinFloat.
const storyWinMode = "maximized"

var storyWinFloat = []float64{0.06, 0.05, 0.55, 0.75}

var readerKeyRE = regexp.MustCompile(`^reader-[1-9][0-9]*$`)

func newReaderURL(path string, rest url.Values, st winState, article string) string {
	c := st.clone()
	c.named = true
	var key string
	for n := 1; ; n++ {
		key = "reader-" + strconv.Itoa(n)
		if !c.has(key) {
			break
		}
	}
	c.open = append(c.open, key)
	c.top = key
	if article != "" {
		c.reader[key] = article
	}
	return winURL(path, rest, c)
}

type placement struct {
	Mode string
	N    []float64
}

func parsePlacement(s string) (placement, bool) {
	m := placementRE.FindStringSubmatch(s)
	if m == nil {
		return placement{}, false
	}
	parts := strings.Split(m[2], ",")
	if want, ok := winModes[m[1]]; !ok || len(parts) != want {
		return placement{}, false
	}
	p := placement{Mode: m[1], N: make([]float64, len(parts))}
	for i, part := range parts {
		f, err := strconv.ParseFloat(part, 64)
		if err != nil || f < 0 || f > 1 {
			return placement{}, false
		}
		p.N[i] = f
	}
	if p.N[2] <= 0 || p.N[3] <= 0 {
		return placement{}, false
	}
	return p, true
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (p placement) String() string {
	s := make([]string, len(p.N))
	for i, f := range p.N {
		s[i] = num(f)
	}
	return p.Mode + ":" + strings.Join(s, ",")
}

// winState is the arrangement an address names. named is false when the
// address carries no open parameter at all, which PUDL reads as "the
// page's default windows"; YAVCHN has none, so that means no windows.
type winState struct {
	named  bool
	open   []string
	top    string
	min    map[string]bool
	place  map[string]placement
	reader map[string]string
}

func keyList(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range strings.Split(v, ",") {
		if winKeyRE.MatchString(k) && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func isWinParam(name string) bool {
	return name == "open" || name == "top" || name == "min" || strings.HasPrefix(name, "p.") || strings.HasPrefix(name, "r.")
}

func parseWinState(q url.Values) winState {
	st := winState{min: map[string]bool{}, place: map[string]placement{}, reader: map[string]string{}}
	if _, ok := q["open"]; !ok {
		return st
	}
	st.named = true
	st.open = keyList(q.Get("open"))
	for _, k := range keyList(q.Get("min")) {
		if st.has(k) {
			st.min[k] = true
		}
	}
	if t := q.Get("top"); st.has(t) {
		st.top = t
	}
	for name, v := range q {
		if k, ok := strings.CutPrefix(name, "r."); ok && st.has(k) {
			if _, _, valid := splitWinKey(q.Get(name)); valid {
				st.reader[k] = q.Get(name)
			}
		}
		if k, ok := strings.CutPrefix(name, "p."); ok && st.has(k) && len(v) > 0 {
			if p, ok := parsePlacement(v[0]); ok {
				st.place[k] = p
			}
		}
	}
	return st
}

// restParams is the query without the window parameters: what the list
// shows rather than what floats over it.
func restParams(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		if !isWinParam(k) {
			out[k] = v
		}
	}
	return out
}

func (st winState) has(k string) bool {
	for _, o := range st.open {
		if o == k {
			return true
		}
	}
	return false
}

func (st winState) clone() winState {
	c := winState{named: st.named, open: append([]string(nil), st.open...), top: st.top,
		min: map[string]bool{}, place: map[string]placement{}, reader: map[string]string{}}
	for k, v := range st.reader {
		c.reader[k] = v
	}
	for k, v := range st.min {
		c.min[k] = v
	}
	for k, v := range st.place {
		c.place[k] = placement{Mode: v.Mode, N: append([]float64(nil), v.N...)}
	}
	return c
}

// front is the window drawn in front. A top naming a minimised window,
// which minimise-all leaves, means no window is in front.
func (st winState) front() string {
	if st.top != "" {
		if st.min[st.top] {
			return ""
		}
		return st.top
	}
	for i := len(st.open) - 1; i >= 0; i-- {
		if !st.min[st.open[i]] {
			return st.open[i]
		}
	}
	return ""
}

// stack is the open windows in drawing order, the one in front last.
func (st winState) stack() []string {
	f := st.front()
	out := make([]string, 0, len(st.open))
	for _, k := range st.open {
		if k != f {
			out = append(out, k)
		}
	}
	if f != "" {
		out = append(out, f)
	}
	return out
}

func (st winState) shown() bool {
	for _, k := range st.open {
		if !st.min[k] {
			return true
		}
	}
	return false
}

func (st winState) closed(k string) winState {
	c := st.clone()
	c.named = true
	c.open = c.open[:0]
	for _, o := range st.open {
		if o != k {
			c.open = append(c.open, o)
		}
	}
	delete(c.min, k)
	delete(c.place, k)
	delete(c.reader, k)
	if c.top == k {
		c.top = ""
	}
	return c
}

func (st winState) minimizeToggled(k string) winState {
	c := st.clone()
	if c.min[k] {
		delete(c.min, k)
		c.top = k
	} else {
		c.min[k] = true
		if c.top == k {
			c.top = ""
		}
	}
	return c
}

// maximizeToggled is the state the maximise button produces. def is the
// placement the window's markup gives it, which applies while the address
// gives it none.
func (st winState) maximizeToggled(k string, def winAttrs) winState {
	c := st.clone()
	p, ok := c.place[k]
	if !ok {
		float := def.Float
		if float == nil {
			float = storyWinFloat
		}
		p = placement{Mode: def.Mode, N: append([]float64(nil), float...)}
	}
	if p.Mode == "floating" {
		p.Mode = "maximized"
	} else {
		p = placement{Mode: "floating", N: p.N[:4]}
	}
	c.place[k] = p
	c.top = k
	delete(c.min, k)
	return c
}

func (st winState) allMinimized() winState {
	c := st.clone()
	for _, k := range c.open {
		c.min[k] = true
	}
	return c
}

func (st winState) allRestored() winState {
	c := st.clone()
	c.min = map[string]bool{}
	return c
}

func (st winState) params() []string {
	if !st.named {
		return nil
	}
	out := []string{"open=" + strings.Join(st.open, ",")}
	if st.top != "" {
		out = append(out, "top="+st.top)
	}
	var mins []string
	for _, k := range st.open {
		if st.min[k] {
			mins = append(mins, k)
		}
	}
	if len(mins) > 0 {
		out = append(out, "min="+strings.Join(mins, ","))
	}
	for _, k := range st.open {
		if article := st.reader[k]; article != "" {
			out = append(out, "r."+k+"="+article)
		}
		if p, ok := st.place[k]; ok {
			out = append(out, "p."+k+"="+p.String())
		}
	}
	return out
}

// winURL is the address of path showing rest's list with st's windows.
// The window parameters are written unescaped, as pudl-windows.js writes
// them; their keys and placements hold nothing that needs escaping.
func winURL(path string, rest url.Values, st winState) string {
	var parts []string
	if enc := rest.Encode(); enc != "" {
		parts = append(parts, enc)
	}
	parts = append(parts, st.params()...)
	if len(parts) == 0 {
		return path
	}
	return path + "?" + strings.Join(parts, "&")
}

type winAttrs struct {
	Mode     string
	Style    template.CSS
	Edge     string
	DockSize float64   // a docked window's strip, as a fraction of the layer
	Float    []float64 // the floating geometry its markup gives it, if any
}

// floatingAt is the attributes of a window floating at x, y with size w, h,
// fractions of the layer.
func floatingAt(x, y, w, h float64) winAttrs {
	return winAttrs{Mode: "floating", Float: []float64{x, y, w, h},
		Style: template.CSS("--win-x:" + num(x) + "; --win-y:" + num(y) + "; --win-w:" + num(w) + "; --win-h:" + num(h))}
}

// dockedAt is the attributes of a window docked at edge, taking size of
// the layer.
func dockedAt(edge string, size float64) winAttrs {
	return winAttrs{Mode: "dock-" + edge, Edge: edge, DockSize: size, Style: template.CSS("--win-dock-size:" + num(size))}
}

// attrs is how the window markup states k's placement: data-win-mode, the
// --win- properties, and for a zone or a dock the properties that place
// it. def is what the window's markup gives it while the address gives it
// no placement.
func (st winState) attrs(k string, def winAttrs) winAttrs {
	p, ok := st.place[k]
	if !ok {
		return def
	}
	style := []string{"--win-x:" + num(p.N[0]), "--win-y:" + num(p.N[1]), "--win-w:" + num(p.N[2]), "--win-h:" + num(p.N[3])}
	var edge string
	switch {
	case p.Mode == "zone":
		style = append(style, "--zone-x:"+num(p.N[4]), "--zone-y:"+num(p.N[5]), "--zone-w:"+num(p.N[6]), "--zone-h:"+num(p.N[7]))
	case strings.HasPrefix(p.Mode, "dock-"):
		edge = strings.TrimPrefix(p.Mode, "dock-")
		style = append(style, "--win-dock-size:"+num(p.N[4]))
	}
	a := winAttrs{Mode: p.Mode, Style: template.CSS(strings.Join(style, "; ")), Edge: edge}
	if edge != "" {
		a.DockSize = p.N[4]
	}
	return a
}

// layerStyle gives the layer the strip each edge's dock takes, from each
// window's attrs. An edge shows its most recently opened dock; the script
// refines this at once.
func (st winState) layerStyle(attrs map[string]winAttrs) template.CSS {
	strips := map[string]float64{}
	for _, k := range st.open {
		if a, ok := attrs[k]; ok && a.Edge != "" && !st.min[k] {
			strips[a.Edge] = a.DockSize
		}
	}
	var out []string
	for _, edge := range []string{"top", "bottom", "left", "right"} {
		if s, ok := strips[edge]; ok {
			out = append(out, fmt.Sprintf("--dock-%s:%s%%", edge, num(s*100)))
		}
	}
	return template.CSS(strings.Join(out, "; "))
}

// --- view models ---

// windowVM is one window, a story's or an applet's: the same markup
// whether the page renders it or /window/{key} returns it. App is set for
// a tool applet's window, Reader for the article reader's bootstrap, and
// the story fields for a loaded story.
type windowVM struct {
	ContentSized     bool
	StoryKey         string
	App              *appVM
	Reader           bool
	Def              winAttrs // the placement the window's markup gives it
	Key, Source, ID  string
	Title, URL, Host string
	By, Age          string
	ByKey, ByURL     string // the poster's profile window and page
	Score, Comments  int
	SourceURL        string // the discussion on the source's own site
	SourceLabel      string // "Open on HN" / "Open on Lobsters"
	HasArticle       bool
	IsComment        bool          // an HN comment, opened from a link to it
	Text             template.HTML // a text post's own words, sanitised
	Error            string
	PageURL          string // the story's own page, /story/{source}/{id}
	Mode, Edge       string
	Style            template.CSS
	Hidden, Active   bool
	MinHref, MaxHref string
	CloseHref        string
}

func readerWindow() windowVM {
	return windowVM{
		Reader: true,
		Key:    "story", Title: "Article reader", PageURL: "/",
		Def: floatingAt(0.06, 0.05, 0.55, 0.75), Mode: "floating",
		Style:   floatingAt(0.06, 0.05, 0.55, 0.75).Style,
		MinHref: "?", MaxHref: "?", CloseHref: "?",
	}
}

// windowsVM is everything the page shell renders for the windows.
type windowsVM struct {
	NewReaderHref  string
	List           []windowVM
	LayerStyle     template.CSS
	Shown          bool // some window is not minimised, so a narrow layout shows them
	Open           bool
	MinAllHref     string
	RestoreAllHref string
	CloseAllHref   string
	AnyMin         bool
}

func storyPageURL(source, id string) string { return "/story/" + source + "/" + id }

func splitWinKey(key string) (source, id string, ok bool) {
	source, id, ok = strings.Cut(key, "-")
	return source, id, ok && storyIDRE.MatchString(id)
}

// storyWindow builds the window for key, or reports false for a key that
// names no story YAVCHN can show, which the page then leaves out.
func (s *Server) storyWindow(ctx context.Context, key string) (windowVM, bool) {
	srcName, id, ok := splitWinKey(key)
	src, known := s.sources[srcName]
	if !ok || !known {
		return windowVM{}, false
	}
	w := windowVM{
		Key: key, StoryKey: key, Source: srcName, ID: id,
		PageURL:     storyPageURL(srcName, id),
		SourceURL:   src.StoryDiscussionURL(id),
		SourceLabel: externalLabelForSource(srcName),
		Def:         winAttrs{Mode: storyWinMode},
		Mode:        storyWinMode,
		MinHref:     "?", MaxHref: "?", CloseHref: "?",
	}
	item, err := src.Item(ctx, id)
	switch {
	case err != nil || item == nil:
		w.Title = "Story unavailable"
		w.Error = "Couldn't load this story. It may have been removed, or the upstream is having a moment."
	case item.Dead || item.Deleted:
		w.Title = "Story removed"
		w.Error = "This story has been removed."
	default:
		w.Host, w.URL = storyURLs(src, item)
		w.Title = item.Title
		// A link to an HN comment opens the comment above its replies.
		w.IsComment = item.Type == "comment"
		if w.IsComment {
			w.Title = "Comment by " + item.By
		}
		w.By = item.By
		w.ByKey, w.ByURL = authorLink(srcName, item.By)
		w.Age = relTime(item.Time)
		w.Score = item.Score
		w.Comments = item.Descendants
		w.HasArticle = item.URL != ""
		if !w.HasArticle && item.Text != "" {
			w.Text = template.HTML(linkThreads(sanitizeHTML(item.Text)))
		}
	}
	return w, true
}

// startWindows fetches the windows the request's address opens while the
// handler fetches its list, and returns a function that waits for them and
// lays them out.
func (s *Server) startWindows(ctx context.Context, r *http.Request) func() windowsVM {
	q := r.URL.Query()
	st := parseWinState(q)
	rest := restParams(q)
	path := r.URL.Path
	done := make(chan map[string]windowVM, 1)
	// A window built for a list page reads its own parameters, not the
	// list's, so an applet such as the hiring filter starts as its markup
	// says rather than from the list's ?q=.
	bare := r.Clone(context.WithValue(ctx, accountReturnContextKey{}, viewURL(r.URL.RequestURI(), "window")))
	readerQuery := url.Values{}
	if message := q.Get("message"); message == "signed-out" || message == "deleted" {
		readerQuery.Set("message", message)
	}
	for k, article := range st.reader {
		readerQuery.Set("r."+k, article)
	}
	bare.URL = &url.URL{Path: path, RawQuery: readerQuery.Encode()}
	go func() { done <- s.windowsParallel(ctx, bare, st.open) }()
	return func() windowsVM {
		got := <-done
		// A key the server does not recognise is ignored, as PUDL asks.
		for _, k := range st.open {
			if _, ok := got[k]; !ok {
				st = st.closed(k)
			}
		}
		front := st.front()
		attrs := make(map[string]winAttrs, len(st.open))
		for _, k := range st.open {
			attrs[k] = st.attrs(k, got[k].Def)
		}
		vm := windowsVM{
			NewReaderHref:  newReaderURL(path, rest, st, ""),
			LayerStyle:     st.layerStyle(attrs),
			Shown:          st.shown(),
			Open:           len(st.open) > 0,
			AnyMin:         len(st.min) > 0,
			MinAllHref:     winURL(path, rest, st.allMinimized()),
			RestoreAllHref: winURL(path, rest, st.allRestored()),
			CloseAllHref:   winURL(path, rest, winState{named: true}),
		}
		for _, k := range st.stack() {
			w := got[k]
			a := attrs[k]
			w.Mode, w.Style, w.Edge = a.Mode, a.Style, a.Edge
			w.Hidden = st.min[k]
			w.Active = k == front
			w.MinHref = winURL(path, rest, st.minimizeToggled(k))
			w.MaxHref = winURL(path, rest, st.maximizeToggled(k, w.Def))
			w.CloseHref = winURL(path, rest, st.closed(k))
			vm.List = append(vm.List, w)
		}
		return vm
	}
}
