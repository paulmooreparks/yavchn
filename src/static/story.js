/* The story applet: a story's article above its discussion, in a window or
   on the story's own page. A window's instance stays alive as the reader
   chooses other stories from the list. The instance replaces its mount's
   story markup and restarts the two story-specific fetches itself.

   An instance:
   - fetches the reader view from /api/article and the thread from
     /api/discussion into its panes, and dispatches yavchn:loaded on each
     pane as its content arrives, for the discussion's collapse, sort and
     new-comment marks (discussion.js, sort.js);
   - gives the menu bar a Story and a Discussion menu while its window is
     in front (menus(), asked afresh as each menu opens);
   - keeps its place, the two panes' scroll positions and the comment the
     keyboard reached, as its state, which continuity.js remembers per
     story, so a reload or a story opened again comes back where it was.

   The article's share of a story is the reader's, kept as a percentage
   under yavchn-article-h for every story, and applied to the page before
   its first paint. */
(function () {
  'use strict';

  var SPLIT_KEY = 'yavchn-article-h';

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
    var contentCtl = null;
    var storyCtl = null;
    var article = null;
    var discussion = null;
    var refreshBtn = null;
    var wanted = null;
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
      var button = refreshBtn;
      var body = article;
      var signal = contentCtl.signal;
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      fill(body, articleSrc(true), signal).then(function () {
        if (signal.aborted) return;
        button.disabled = false;
        button.removeAttribute('aria-busy');
      });
    }

    function wire(stateText) {
      if (contentCtl) contentCtl.abort();
      contentCtl = new AbortController();
      var signal = contentCtl.signal;
      article = root.querySelector('.story-article-body');
      discussion = root.querySelector('.story-discussion-body');
      refreshBtn = root.querySelector('.story-refresh');
      wanted = parseState(stateText);
      var wantedState = wanted;

      if (article && d.readerUrl) {
        var articleBody = article;
        fill(articleBody, articleSrc(false), signal).then(function () {
          if (!signal.aborted) restore(articleBody, wantedState.a);
        });
      } else if (article) {
        restore(article, wantedState.a);
      }
      if (discussion) {
        var discussionBody = discussion;
        fill(discussionBody, '/api/discussion?id=' + encodeURIComponent(d.storyId) +
          '&source=' + encodeURIComponent(d.storySource || 'hn'), signal).then(function () {
          if (signal.aborted) return;
          if (wantedState.c && lib.discussion) lib.discussion.focusId(root, wantedState.c, false);
          restore(discussionBody, wantedState.d);
        });
      }

      [article, discussion].forEach(function (el) {
        if (el) el.addEventListener('scroll', changedSoon, { passive: true, signal: signal });
      });
      if (refreshBtn) refreshBtn.addEventListener('click', refresh, { signal: signal });
    }

    function rememberedState() {
      var detail = { name: 'story', instance: d.storyKey, host: opts.host, fit: opts.fit, param: null, state: null };
      root.dispatchEvent(new CustomEvent('pudl:applet-state', { bubbles: true, detail: detail }));
      return detail.state;
    }

    function replaceMount(next) {
      var runtime = {};
      ['data-applet-state', 'data-applet-fit'].forEach(function (name) {
        if (root.hasAttribute(name)) runtime[name] = root.getAttribute(name);
      });
      Array.prototype.slice.call(root.attributes).forEach(function (attr) { root.removeAttribute(attr.name); });
      Array.prototype.slice.call(next.attributes).forEach(function (attr) { root.setAttribute(attr.name, attr.value); });
      Object.keys(runtime).forEach(function (name) { root.setAttribute(name, runtime[name]); });
      root.innerHTML = next.innerHTML;
    }

    function loadStory(key, push) {
      var win = root.closest('.win[data-win]');
      if (!win || !window.pudlWindows) return;
      if (storyCtl) storyCtl.abort();
      if (key === (d.storyKey || '')) {
        root.removeAttribute('aria-busy');
        if (push) window.pudlWindows.raise(win.getAttribute('data-win'));
        return;
      }
      if (!key) {
        clearTimeout(timer);
        opts.changed(state());
        if (contentCtl) contentCtl.abort();
        root.removeAttribute('aria-busy');
        Array.from(root.attributes).forEach(function (attr) {
          if (attr.name.indexOf('data-story-') === 0 || attr.name === 'data-reader-url' || attr.name === 'data-applet-page') root.removeAttribute(attr.name);
        });
        root.setAttribute('data-state-key', win.getAttribute('data-win'));
        root.innerHTML = '<div class="empty-state story-note"><p class="empty-state-title">Article reader</p><p class="empty-state-body">Select an article from the sidebar.</p></div>';
        article = discussion = refreshBtn = null;
        window.pudlWindows.retitle(win.getAttribute('data-win'), 'Article reader');
        root.dispatchEvent(new CustomEvent('yavchn:story-change', { bubbles: true }));
        return;
      }
      storyCtl = new AbortController();
      var signal = storyCtl.signal;
      root.setAttribute('aria-busy', 'true');

      fetch('/window/' + encodeURIComponent(key), { credentials: 'same-origin', signal: signal })
        .then(function (r) { if (!r.ok) throw new Error('story ' + r.status); return r.text(); })
        .then(function (html) {
          if (signal.aborted || !root.isConnected) return;
          var doc = new DOMParser().parseFromString(html, 'text/html');
          var nextWin = doc.querySelector('.win[data-win]');
          var next = nextWin && nextWin.querySelector('.story[data-applet="story"]');
          if (!next) throw new Error('story markup missing');

          clearTimeout(timer);
          opts.changed(state());
          var oldKey = win.getAttribute('data-win');
          window.yavchn.readers.assign(oldKey, key, push);

          var title = nextWin.querySelector('.win-title');
          var currentTitle = win.querySelector('.win-title');
          if (title && currentTitle) window.pudlWindows.retitle(oldKey, title.textContent);
          var page = nextWin.querySelector('[data-win-action="page"]');
          var currentPage = win.querySelector('[data-win-action="page"]');
          if (page && currentPage) currentPage.href = page.href;
          replaceMount(next);
          root.setAttribute('data-state-key', oldKey + ':' + key);
          wire(rememberedState());
          if (push) window.pudlWindows.raise(oldKey);
          root.dispatchEvent(new CustomEvent('yavchn:story-change', { bubbles: true, detail: { key: key } }));
        })
        .catch(function (err) {
          if (!err || err.name !== 'AbortError') {
            root.removeAttribute('aria-busy');
            if (window.console) console.warn('story:', err.message);
          }
        });
    }

    root.addEventListener('yavchn:comment-focus', changedSoon, { signal: ctl.signal });
    wire(opts.state);
    var unmountReader = lib.readers.mount(root, loadStory);

    function win() { return root.closest('.win'); }

    function menus() {
      if (!d.storyKey) return { titles: [] };
      var pins = lib.pins, disc = lib.discussion, sort = lib.sort;
      var next = win() && lib.nextStory ? lib.nextStory(win()) : null;
      var page = new URL(root.getAttribute('data-applet-page') || opts.pageUrl, location.href).href;
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
      setState: function (s) {
        var key = new URLSearchParams(s || '').get('story');
        if (key) loadStory(key, true);
      },
      menus: menus,
      destroy: function () {
        unmountReader();
        clearTimeout(timer);
        if (storyCtl) storyCtl.abort();
        if (contentCtl) contentCtl.abort();
        ctl.abort();
      }
    };
  }

  window.pudlApplets.register('story', { init: init });

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
