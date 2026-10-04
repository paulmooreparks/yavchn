/* The story applet: a story's article above its discussion, in a window or
   on the story's own page. A window's instance stays alive as the reader
   chooses other stories from the list. The instance replaces its mount's
   story markup and restarts the two story-specific fetches itself.

   An instance:
   - fetches the reader view from /api/article and the thread from
     /api/discussion into its panes, and dispatches yavchn:loaded on each
     pane as its content arrives, for the discussion's collapse, sort and
     new-comment marks (discussion.js, sort.js);
   - gives the menu bar Reader, Story, and Discussion menus while its window is
     in front (menus(), asked afresh as each menu opens);
   - keeps its place, the two panes' scroll positions and the comment the
     keyboard reached, as its state, which continuity.js remembers per
     story, so a reload or a story opened again comes back where it was.

   The article's share of a story is the reader's, kept as a percentage
   under yavchn-article-h for every story, and applied to the page before
   its first paint. */
(function () {
  'use strict';


  var failed = '<div class="empty-state story-note"><p class="empty-state-title">This could not be loaded</p>' +
    '<p class="empty-state-body">The link above opens it on its own site.</p></div>';

  function fill(body, src, signal) {
    return fetch(src, { credentials: 'omit', signal: signal })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        if (!html) throw new Error('empty');
        body.innerHTML = html;
        body.scrollTop = 0;
        (body.closest('.story-scroll') || body).dispatchEvent(new CustomEvent('yavchn:loaded', { bubbles: true }));
      })
      .catch(function (err) { if (!err || err.name !== 'AbortError') body.innerHTML = failed; });
  }

  /* State is a query string, a=article scroll, d=discussion scroll,
     c=the id of the top-level comment the keyboard reached, and note=open
     or closed once the reader has opened or closed the note panel. */
  function parseState(s) {
    var q = new URLSearchParams(s || '');
    var note = q.get('note');
    return { a: parseInt(q.get('a') || '0', 10) || 0, d: parseInt(q.get('d') || '0', 10) || 0, c: q.get('c') || '', pane: q.get('pane') || '', ratio: q.get('ratio') || '',
      note: note === 'open' || note === 'closed' ? note : '' };
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
    var paneMode = '';
    var splitRatio = '';
    var positions = { a: 0, d: 0 };
    var d = root.dataset;
    var lib = window.yavchn;

    function state() {
      var q = new URLSearchParams();
      if (paneMode) q.set('pane', paneMode);
      if (splitRatio) q.set('ratio', splitRatio);
      if (article && article.offsetHeight) positions.a = article.scrollTop;
      if (positions.a) q.set('a', String(Math.round(positions.a)));
      if (discussion && discussion.offsetHeight) positions.d = discussion.scrollTop;
      if (positions.d) q.set('d', String(Math.round(positions.d)));
      var c = root.querySelector('.discussion-content > .thread > .comment.focused');
      if (c && c.dataset.id) q.set('c', c.dataset.id);
      var note = root.querySelector('[data-note-panel]');
      if (note && note.dataset.noteChosen) q.set('note', note.hidden ? 'closed' : 'open');
      return q.toString();
    }


    function validPane(value) { return ['article', 'discussion', 'split'].indexOf(value) >= 0; }
    function sharedPage() {
      var page = new URL(root.getAttribute('data-applet-page') || opts.pageUrl, location.href);
      if (paneMode) page.searchParams.set('pane', paneMode);
      if (splitRatio) page.searchParams.set('ratio', splitRatio);
      return page;
    }
    function savePresentation(persist) {
      if (persist !== false) opts.changed(state());
      var page = sharedPage();
      var host = root.closest('.win');
      if (host) {
        host.setAttribute('data-win-href', page.href);
        var pageLink = host.querySelector('[data-win-action="page"]');
        if (pageLink) pageLink.href = page.href;
      } else if (opts.host === 'page' && persist !== false) {
        var url = new URL(location.href);
        url.searchParams.set('pane', paneMode);
        if (splitRatio) url.searchParams.set('ratio', splitRatio);
        else url.searchParams.delete('ratio');
        history.replaceState(history.state, '', url);
      }
    }
    function showPane(mode, save) {
      var split = root.querySelector('.story-split');
      if (!split || !validPane(mode)) return;
      if (save && article && article.offsetHeight) positions.a = article.scrollTop;
      if (save && discussion && discussion.offsetHeight) positions.d = discussion.scrollTop;
      paneMode = mode;
      root.dataset.readerPane = mode;
      var hiddenPane = split.querySelector(mode === 'article' ? '.story-discussion' : '.story-article');
      if (mode !== 'split' && hiddenPane.contains(document.activeElement)) {
        root.querySelector('[data-reader-pane="' + mode + '"]').focus();
      }
      if (mode === 'split') delete split.dataset.splitPane;
      else split.dataset.splitPane = mode === 'article' ? 'first' : 'second';
      if (splitRatio) split.style.setProperty('--split-a', splitRatio + '%');
      root.querySelectorAll('[data-reader-pane]').forEach(function (b) { b.setAttribute('aria-pressed', String(b.dataset.readerPane === mode)); });
      if (article && mode !== 'discussion') article.scrollTop = positions.a;
      if (discussion && mode !== 'article') discussion.scrollTop = positions.d;
      if (mode === 'split' && window.pudlSplit) window.pudlSplit.refresh();
      if (save) savePresentation();
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
      var body = root.querySelector('.story-article-content');
      positions.a = 0;
      article.scrollTop = 0;
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
      // library.js opens or closes the note panel as the reader left it.
      if (wanted.note) root.dataset.noteWanted = wanted.note;
      else delete root.dataset.noteWanted;
      if (lib.library) lib.library.bindNotes();
      positions = { a: wanted.a, d: wanted.d };
      var urlState = opts.host === 'page' ? parseState(location.search.slice(1)) : null;
      if (urlState && validPane(urlState.pane)) paneMode = urlState.pane;
      else if (!paneMode && validPane(wanted.pane)) paneMode = wanted.pane;
      if (!paneMode && d.storyKey) {
        var width = root.getBoundingClientRect().width || window.innerWidth;
        paneMode = !d.readerUrl && !root.querySelector('.story-text') ? 'discussion' : width <= 600 ? 'article' : 'split';
      }
      var ratio = (urlState && urlState.ratio) || splitRatio || wanted.ratio;
      if (/^\d+(\.\d+)?$/.test(ratio) && +ratio >= 10 && +ratio <= 90) splitRatio = ratio;
      var toolbar = root.querySelector('.reader-toolbar');
      if (toolbar) toolbar.hidden = false;
      showPane(paneMode, false);
      root.querySelectorAll('[data-reader-pane]').forEach(function (b) {
        b.addEventListener('click', function () { showPane(b.dataset.readerPane, true); }, { signal: signal });
      });
      if (d.storyKey) savePresentation(false);

      if (article && d.readerUrl) {
        var articleBody = article;
        fill(root.querySelector('.story-article-content'), articleSrc(false), signal).then(function () {
          if (!signal.aborted) restore(articleBody, wantedState.a);
        });
      } else if (article) {
        restore(article, wantedState.a);
      }
      if (discussion) {
        var discussionBody = discussion;
        fill(root.querySelector('.story-discussion-content'), '/api/discussion?id=' + encodeURIComponent(d.storyId) +
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

    // The same reader instance can change its menu inventory as articles load.
    root.addEventListener('yavchn:story-change', function () {
      if (window.pudlMenubar) window.pudlMenubar.refresh();
    }, { signal: ctl.signal });
    root.addEventListener('pudl:split', function (e) {
      var split = e.target.closest('.story-split');
      if (!split || paneMode !== 'split') return;
      splitRatio = e.detail.size == null ? '' : String(Math.max(10, Math.min(90, e.detail.size / split.getBoundingClientRect().height * 100)).toFixed(2));
      if (splitRatio) split.style.setProperty('--split-a', splitRatio + '%');
      savePresentation();
    }, { signal: ctl.signal });
    root.addEventListener('yavchn:show-discussion', function () {
      if (paneMode === 'article') showPane('discussion', true);
    }, { signal: ctl.signal });
    root.addEventListener('yavchn:comment-focus', changedSoon, { signal: ctl.signal });
    root.addEventListener('yavchn:note-toggle', changedSoon, { signal: ctl.signal });
    wire(opts.state);
    var unmountReader = lib.readers.mount(root, loadStory);

    function win() { return root.closest('.win'); }

    function menus() {
      if (!d.storyKey) return { titles: [] };
      var pins = lib.pins, disc = lib.discussion, sort = lib.sort;
      var next = win() && lib.nextStory ? lib.nextStory(win()) : null;
      var page = sharedPage().href;
      var original = d.readerUrl || '';
      var source = root.querySelector('.story-discussion .story-bar a[target="_blank"]');
      var story = [
        { label: 'Pin this story', checked: !!(pins && pins.isPinned(d.storyId)), run: function () { pins.toggleStory(root); } },
        ...(lib.library ? lib.library.storyMenu(root) : []),
        '-',
        ...(win() ? [{ label: 'Open in new reader window', run: function () { lib.readers.create(d.storyKey); } }] : []),
        { label: 'Open the original', disabled: !original, run: function () { window.open(original, '_blank', 'noopener'); } },
        { label: source ? source.textContent.trim() : 'Open on the source site', disabled: !source,
          run: function () { window.open(source.href, '_blank', 'noopener'); } },
        { label: 'Fetch the article again', disabled: !original || !!(refreshBtn && refreshBtn.disabled), run: refresh }
      ];
      if (lib.hiding) {
        story.push('-', { label: 'Hide this story', run: function () { lib.hiding.hide(d.storyId); } });
        // A text post's host is the source itself, which is no site to block.
        if (original && d.host) {
          story.push({ label: 'Block ' + d.host + ' across all feeds', run: function () { lib.hiding.blockDomain(d.host); } });
        }
      }
      var mode = sort ? sort.get() : 'best';
      var hasNew = !!root.querySelector('.comment-new');
      var hasComments = !!root.querySelector('.discussion-content .comment');
      var discussionMenu = [
        { label: 'Best first', disabled: !hasComments, radio: 'sort', checked: mode === 'best', run: function () { sort.set('best'); } },
        { label: 'Newest first', disabled: !hasComments, radio: 'sort', checked: mode === 'newest', run: function () { sort.set('newest'); } },
        { label: 'Oldest first', disabled: !hasComments, radio: 'sort', checked: mode === 'oldest', run: function () { sort.set('oldest'); } },
        '-',
        { label: 'Collapse every thread', disabled: !hasComments, run: function () { disc.collapseAll(root, true); } },
        { label: 'Expand every thread', disabled: !hasComments, run: function () { disc.collapseAll(root, false); } }
      ];
      var navigation = [
        ...(win() ? [{ label: 'Next story', disabled: !next, run: function () { if (next) next.click(); } }, '-'] : []),
        { label: 'Next comment', disabled: !hasComments, run: function () { disc.step(root, 1); } },
        { label: 'Previous comment', disabled: !hasComments, run: function () { disc.step(root, -1); } },
        { label: 'First new comment', disabled: !hasNew, run: function () { disc.firstNew(root); } },
      ];
      return {
        titles: [
          { label: 'Reader', items: lib.identityMenu(root, 'Copy story link', page) },
          { label: 'Story', items: story },
          { label: 'Discussion', items: discussionMenu }
        ],
        into: { go: navigation, view: ['article', 'discussion', 'split'].map(function (mode) {
          return { label: mode === 'article' ? 'Article only' : mode === 'discussion' ? 'Discussion only' : 'Article and discussion', radio: 'reader-pane', checked: paneMode === mode, run: function () { showPane(mode, true); } };
        }) }
      };
    }

    return {
      state: state,
      setState: function (s) {
        var key = new URLSearchParams(s || '').get('story');
        var incoming = parseState(s);
        if (validPane(incoming.pane)) showPane(incoming.pane, true);
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

})();
