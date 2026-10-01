/* The story applet: a story's article above its discussion, in a window or
   on the story's own page. PUDL's applet runtime starts an instance for
   each story mount as its window opens (or with the page), and destroys it
   as the window closes, so each window has a life of its own.

   An instance:
   - fetches the reader view from /api/article and the thread from
     /api/discussion into its panes, and dispatches yavchn:loaded on each
     pane as its content arrives, for the discussion's collapse, sort and
     new-comment marks (discussion.js, sort.js);
   - gives the menu bar a Story and a Discussion menu while its window is
     in front (menus(), asked afresh as each menu opens);
   - keeps its place, the two panes' scroll positions and the comment the
     keyboard reached, as its state, which the host below remembers per
     story, so a reload or a story opened again comes back where it was.

   The article's share of a story is the reader's, kept as a percentage
   under yavchn-article-h for every story, and applied to the page before
   its first paint. */
(function () {
  'use strict';

  var SPLIT_KEY = 'yavchn-article-h';
  var STATE_KEY = 'yavchn-story-state';
  var STATE_CAP = 100;

  var failed = '<div class="empty-state story-note"><p class="empty-state-title">This could not be loaded</p>' +
    '<p class="empty-state-body">The link above opens it on its own site.</p></div>';

  function fill(body, src, signal) {
    return fetch(src, { credentials: 'omit', signal: signal })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        if (!html) throw new Error('empty');
        body.innerHTML = html;
        body.scrollTop = 0;
        body.dispatchEvent(new CustomEvent('yavchn:loaded', { bubbles: true }));
      })
      .catch(function (err) { if (!err || err.name !== 'AbortError') body.innerHTML = failed; });
  }

  /* State is a query string, a=article scroll, d=discussion scroll and
     c=the id of the top-level comment the keyboard reached. */
  function parseState(s) {
    var q = new URLSearchParams(s || '');
    return { a: parseInt(q.get('a') || '0', 10) || 0, d: parseInt(q.get('d') || '0', 10) || 0, c: q.get('c') || '' };
  }

  function init(root, opts) {
    var ctl = new AbortController();
    var signal = ctl.signal;
    var article = root.querySelector('.story-article-body');
    var discussion = root.querySelector('.story-discussion-body');
    var refreshBtn = root.querySelector('.story-refresh');
    var wanted = parseState(opts.state);
    var timer = 0;
    var d = root.dataset;
    var lib = window.yavchn;

    function state() {
      var q = new URLSearchParams();
      if (article && article.scrollTop) q.set('a', String(Math.round(article.scrollTop)));
      if (discussion && discussion.scrollTop) q.set('d', String(Math.round(discussion.scrollTop)));
      var c = root.querySelector('.discussion-content > .thread > .comment.focused');
      if (c && c.dataset.id) q.set('c', c.dataset.id);
      return q.toString();
    }

    function changedSoon() {
      clearTimeout(timer);
      timer = setTimeout(function () { opts.changed(state()); }, 400);
    }

    /* A pane's place comes back once its content is in, and only the
       first time, so a refresh starts at the top as it always has. */
    function restore(body, top) {
      if (top) body.scrollTop = top;
    }

    function articleSrc(refresh) {
      return '/api/article?' + (refresh ? 'refresh=1&' : '') + 'url=' + encodeURIComponent(d.readerUrl);
    }

    function refresh() {
      if (!refreshBtn || !article || !d.readerUrl || refreshBtn.disabled) return;
      refreshBtn.disabled = true;
      refreshBtn.setAttribute('aria-busy', 'true');
      fill(article, articleSrc(true), signal).then(function () {
        refreshBtn.disabled = false;
        refreshBtn.removeAttribute('aria-busy');
      });
    }

    if (article && d.readerUrl) {
      fill(article, articleSrc(false), signal).then(function () { restore(article, wanted.a); });
    } else if (article) {
      restore(article, wanted.a);
    }
    if (discussion) {
      fill(discussion, '/api/discussion?id=' + encodeURIComponent(d.storyId) +
        '&source=' + encodeURIComponent(d.storySource || 'hn'), signal).then(function () {
        if (wanted.c && lib.discussion) lib.discussion.focusId(root, wanted.c, false);
        restore(discussion, wanted.d);
      });
    }

    [article, discussion].forEach(function (el) {
      if (el) el.addEventListener('scroll', changedSoon, { passive: true, signal: signal });
    });
    root.addEventListener('yavchn:comment-focus', changedSoon, { signal: signal });
    if (refreshBtn) refreshBtn.addEventListener('click', refresh, { signal: signal });

    function win() { return root.closest('.win'); }

    function menus() {
      var pins = lib.pins, disc = lib.discussion, sort = lib.sort;
      var next = win() && lib.nextStory ? lib.nextStory(win()) : null;
      var page = new URL(opts.pageUrl, location.href).href;
      var original = d.readerUrl || '';
      var source = root.querySelector('.story-discussion .story-bar a[target="_blank"]');
      var story = [
        { label: 'Pin this story', checked: !!(pins && pins.isPinned(d.storyId)), run: function () { pins.toggleStory(root); } },
        { label: 'Next story', disabled: !next, run: function () { if (next) next.click(); } },
        '-',
        { label: 'Open the original', disabled: !original, run: function () { window.open(original, '_blank', 'noopener'); } },
        { label: source ? source.textContent.trim() : 'Open on the source site', disabled: !source,
          run: function () { window.open(source.href, '_blank', 'noopener'); } },
        { label: 'Copy the link to this story', run: function () { if (navigator.clipboard) navigator.clipboard.writeText(page).catch(function () {}); } },
        { label: 'Fetch the article again', disabled: !original, run: refresh }
      ];
      if (lib.hiding) {
        story.push('-', { label: 'Hide this story', run: function () { lib.hiding.hide(d.storyId); } });
        // A text post's host is the source itself, which is no site to block.
        if (original && d.host) {
          story.push({ label: 'Hide stories from ' + d.host, run: function () { lib.hiding.blockDomain(d.host); } });
        }
      }
      if (win()) {
        story.push('-', { label: 'Close', run: function () { window.pudlWindows.close(win().getAttribute('data-win')); } });
      }

      var mode = sort ? sort.get() : 'best';
      var hasNew = !!root.querySelector('.comment-new');
      var discussionMenu = [
        { label: 'Best first', radio: 'sort', checked: mode === 'best', run: function () { sort.set('best'); } },
        { label: 'Newest first', radio: 'sort', checked: mode === 'newest', run: function () { sort.set('newest'); } },
        { label: 'Oldest first', radio: 'sort', checked: mode === 'oldest', run: function () { sort.set('oldest'); } },
        '-',
        { label: 'Next comment', run: function () { disc.step(root, 1); } },
        { label: 'Previous comment', run: function () { disc.step(root, -1); } },
        { label: 'First new comment', disabled: !hasNew, run: function () { disc.firstNew(root); } },
        '-',
        { label: 'Collapse every thread', run: function () { disc.collapseAll(root, true); } },
        { label: 'Expand every thread', run: function () { disc.collapseAll(root, false); } }
      ];
      return { titles: [{ label: 'Story', items: story }, { label: 'Discussion', items: discussionMenu }] };
    }

    return {
      state: state,
      menus: menus,
      destroy: function () {
        clearTimeout(timer);
        ctl.abort();
      }
    };
  }

  window.pudlApplets.register('story', { init: init });

  /* === The host's side of the state ======================================
     PUDL keeps no applet state; it asks the host before an instance starts
     and says when the state changes. YAVCHN keeps the last hundred stories'
     places in this browser, by story, and forgets a story's place when the
     reader closes its window, but not when Next replaced it or Back moved
     past it, since then the reader may well return. */
  function kept() {
    try { return JSON.parse(localStorage.getItem(STATE_KEY) || '{}') || {}; } catch (e) { return {}; }
  }

  function keep(all) {
    var keys = Object.keys(all);
    if (keys.length > STATE_CAP) {
      keys.sort(function (a, b) { return (all[a].t || 0) - (all[b].t || 0); });
      keys.slice(0, keys.length - STATE_CAP).forEach(function (k) { delete all[k]; });
    }
    try { localStorage.setItem(STATE_KEY, JSON.stringify(all)); } catch (e) { /* not kept, then */ }
  }

  function storyKeyOf(mount) { return mount.getAttribute('data-story-key'); }

  document.addEventListener('pudl:applet-state', function (e) {
    if (e.detail.name !== 'story') return;
    var s = kept()[storyKeyOf(e.target)];
    if (s && s.s != null) e.detail.state = s.s;
  });

  document.addEventListener('pudl:applet-change', function (e) {
    var k = e.target.matches && e.target.matches('[data-applet="story"]') && storyKeyOf(e.target);
    if (!k) return;
    var all = kept();
    if (e.detail.state) all[k] = { s: e.detail.state, t: Date.now() };
    else delete all[k];
    keep(all);
  });

  document.addEventListener('pudl:window-close', function (e) {
    var why = e.detail && e.detail.reason;
    if (why !== 'button' && why !== 'key' && why !== 'script') return;
    var mount = e.target.querySelector && e.target.querySelector('[data-applet="story"]');
    var k = mount && storyKeyOf(mount);
    if (!k) return;
    var all = kept();
    if (all[k]) { delete all[k]; keep(all); }
  });

  /* === The split ======================================================== */
  function applySplit(pct) {
    document.querySelectorAll('.story-split').forEach(function (s) {
      if (pct) s.style.setProperty('--split-a', pct);
      else s.style.removeProperty('--split-a');
    });
  }

  document.addEventListener('pudl:split', function (e) {
    var split = e.target.closest('.story-split');
    if (!split) return;
    var pct = null;
    if (e.detail.size != null) {
      var whole = split.getBoundingClientRect().height;
      if (whole > 0) pct = Math.max(10, Math.min(90, e.detail.size / whole * 100)).toFixed(2) + '%';
    }
    try {
      if (pct) localStorage.setItem(SPLIT_KEY, pct);
      else localStorage.removeItem(SPLIT_KEY);
    } catch (err) { /* storage blocked */ }
    applySplit(pct);
  });
})();
