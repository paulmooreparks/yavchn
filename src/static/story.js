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
    /* The pane the reader last chose opens every story that has no place
       of its own. It stays in this browser, since a phone and a desktop
       want different panes. */
    var PANE_KEY = 'yavchn-reader-pane';
    function preferredPane() {
      try { var p = localStorage.getItem(PANE_KEY); return validPane(p) ? p : ''; } catch (e) { return ''; }
    }
    function preferPane(mode) {
      try { localStorage.setItem(PANE_KEY, mode); } catch (e) { /* not kept, then */ }
    }
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
    function showPane(mode, save, chosen) {
      var split = root.querySelector('.story-split');
      if (!split || !validPane(mode)) return;
      if (chosen) preferPane(mode);
      if (save && article && article.offsetHeight) positions.a = article.scrollTop;
      if (save && discussion && discussion.offsetHeight) positions.d = discussion.scrollTop;
      paneMode = mode;
      root.dataset.readerPane = mode;
      var hiddenPane = split.querySelector(mode === 'article' ? '.story-discussion' : '.story-article');
      if (mode !== 'split' && hiddenPane.contains(document.activeElement)) {
        var chosenButton = root.querySelector('[data-command="pane-' + mode + '"]');
        if (chosenButton) chosenButton.focus();
      }
      if (mode === 'split') delete split.dataset.splitPane;
      else split.dataset.splitPane = mode === 'article' ? 'first' : 'second';
      if (splitRatio) split.style.setProperty('--split-a', splitRatio + '%');
      if (opts.commandsChanged) opts.commandsChanged();
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

    /* The refresh command is disabled while an article is fetched again, and
       its button turns its glyph meanwhile. */
    var refreshing = false;
    function refresh() {
      if (!article || !d.readerUrl || refreshing) return;
      var body = root.querySelector('.story-article-content');
      positions.a = 0;
      article.scrollTop = 0;
      var signal = contentCtl.signal;
      refreshing = true;
      if (refreshBtn) refreshBtn.setAttribute('aria-busy', 'true');
      if (opts.commandsChanged) opts.commandsChanged();
      return fill(body, articleSrc(true), signal).then(function () {
        refreshing = false;
        if (refreshBtn) refreshBtn.removeAttribute('aria-busy');
        if (opts.commandsChanged) opts.commandsChanged();
      });
    }

    function setRunning(on) {
      var host = root.closest('.win[data-win]');
      if (host) host.toggleAttribute('data-win-running', on);
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
      // A story with no article of its own shows its discussion whatever was chosen.
      var noArticle = !d.readerUrl && !root.querySelector('.story-text');
      if (urlState && validPane(urlState.pane)) paneMode = urlState.pane;
      else if (validPane(wanted.pane)) paneMode = wanted.pane;
      else if (d.storyKey && noArticle) paneMode = 'discussion';
      else if (d.storyKey) {
        var width = root.getBoundingClientRect().width || window.innerWidth;
        paneMode = preferredPane() || (width <= 600 ? 'article' : 'split');
      }
      var ratio = (urlState && urlState.ratio) || splitRatio || wanted.ratio;
      if (/^\d+(\.\d+)?$/.test(ratio) && +ratio >= 10 && +ratio <= 90) splitRatio = ratio;
      var toolbar = root.querySelector('.reader-toolbar');
      if (toolbar) toolbar.hidden = false;
      refreshing = false;
      showPane(paneMode, false);
      if (d.storyKey) savePresentation(false);

      var loading = [];
      if (article && d.readerUrl) {
        var articleBody = article;
        loading.push(fill(root.querySelector('.story-article-content'), articleSrc(false), signal).then(function () {
          if (!signal.aborted) restore(articleBody, wantedState.a);
        }));
      } else if (article) {
        restore(article, wantedState.a);
      }
      if (discussion) {
        var discussionBody = discussion;
        loading.push(fill(root.querySelector('.story-discussion-content'), '/api/discussion?id=' + encodeURIComponent(d.storyId) +
          '&source=' + encodeURIComponent(d.storySource || 'hn'), signal).then(function () {
          if (signal.aborted) return;
          if (wantedState.c && lib.discussion) lib.discussion.focusId(root, wantedState.c, false);
          restore(discussionBody, wantedState.d);
        }));
      }
      // The window's running mark, PUDL's, shows in the taskbar until the
      // article and the discussion are in.
      setRunning(loading.length > 0);
      if (loading.length) Promise.allSettled(loading).then(function () { if (!signal.aborted) setRunning(false); });

      [article, discussion].forEach(function (el) {
        if (el) el.addEventListener('scroll', changedSoon, { passive: true, signal: signal });
      });
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
        setRunning(false);
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

    /* The reader's commands, declared once with PUDL's command table: the
       toolbar's pane selector, pin and refresh are drawn from them, and the
       menus below list them by id, so a command's words, state and reason
       are the same wherever it appears, and an agent can list and run them
       (pudlCommands.list and run). Every field is read when it is drawn. */
    var noStory = function () { return !d.storyKey; };
    var noComments = function () { return !root.querySelector('.discussion-content .comment'); };
    var original = function () { return d.readerUrl || ''; };
    var sourceLink = function () { return root.querySelector('.story-discussion .story-bar a[target="_blank"]'); };
    var nextLink = function () { return win() && lib.nextStory ? lib.nextStory(win()) : null; };
    var previousLink = function () { return win() && lib.previousStory ? lib.previousStory(win()) : null; };
    function pane(mode, label) {
      return { id: 'pane-' + mode, label: label, choice: 'pane', choiceLabel: 'Reader panes', hidden: noStory,
        checked: function () { return paneMode === mode; }, run: function () { showPane(mode, true, true); } };
    }
    function sortBy(mode, label) {
      return { id: 'sort-' + mode, label: label, choice: 'sort', choiceLabel: 'Sort comments', hidden: noStory,
        disabled: noComments, reason: 'The discussion has no comments yet.',
        checked: function () { return !!lib.sort && lib.sort.get() === mode; }, run: function () { lib.sort.set(mode); } };
    }
    function commandTable() {
      var pins = lib.pins, disc = lib.discussion;
      return [
        pane('article', 'Article'), pane('discussion', 'Discussion'), pane('split', 'Split'),
        { id: 'pin', glyph: 'pin', hidden: function () { return !d.storyId || !pins; },
          label: function () { return pins && pins.isPinned(d.storyId) ? 'Unpin this story' : 'Pin this story'; },
          checked: function () { return !!(pins && pins.isPinned(d.storyId)); }, run: function () { pins.toggleStory(root); } },
        { id: 'new-reader', label: 'Open in new reader window', glyph: 'new-window', hidden: function () { return noStory() || !win(); },
          run: function () { lib.readers.create(d.storyKey); } },
        { id: 'open-original', label: 'Open the original', glyph: 'open', hidden: function () { return noStory() || !original(); },
          run: function () { window.open(original(), '_blank', 'noopener'); } },
        { id: 'open-source', glyph: 'link', hidden: function () { return noStory() || !sourceLink(); },
          label: function () { var s = sourceLink(); return s ? s.textContent.trim() : 'Open on the source site'; },
          run: function () { window.open(sourceLink().href, '_blank', 'noopener'); } },
        { id: 'refresh', label: 'Refresh the article', glyph: 'reload', hidden: function () { return noStory() || !original(); },
          disabled: function () { return refreshing; }, reason: 'The article is being fetched again.', run: refresh },
        { id: 'hide-story', label: 'Hide this story', hidden: function () { return noStory() || !lib.hiding; },
          run: function () { lib.hiding.hide(d.storyId); } },
        // A text post's host is the source itself, which is no site to block.
        { id: 'block-site', hidden: function () { return noStory() || !lib.hiding || !original() || !d.host; },
          label: function () { return 'Block ' + d.host + ' across all feeds'; }, danger: true, run: function () { lib.hiding.blockDomain(d.host); } },
        sortBy('best', 'Best first'), sortBy('newest', 'Newest first'), sortBy('oldest', 'Oldest first'),
        { id: 'collapse-all', label: 'Collapse every thread', hidden: noStory, disabled: noComments,
          reason: 'The discussion has no comments yet.', run: function () { disc.collapseAll(root, true); } },
        { id: 'expand-all', label: 'Expand every thread', hidden: noStory, disabled: noComments,
          reason: 'The discussion has no comments yet.', run: function () { disc.collapseAll(root, false); } },
        { id: 'previous-story', label: 'Previous story', hidden: function () { return noStory() || !win(); },
          disabled: function () { return !previousLink(); }, reason: 'This is the first story in the list.',
          run: function () { var to = previousLink(); if (to) to.click(); } },
        { id: 'next-story', label: 'Next story', hidden: function () { return noStory() || !win(); },
          disabled: function () { return !nextLink(); }, reason: 'This is the last story in the list.',
          run: function () { var next = nextLink(); if (next) next.click(); } },
        { id: 'next-comment', label: 'Next comment', glyph: 'arrow-down', hidden: noStory, disabled: noComments,
          reason: 'The discussion has no comments yet.', run: function () { disc.step(root, 1); } },
        { id: 'previous-comment', label: 'Previous comment', glyph: 'arrow-up', hidden: noStory, disabled: noComments,
          reason: 'The discussion has no comments yet.', run: function () { disc.step(root, -1); } },
        { id: 'first-new-comment', label: 'First new comment', hidden: noStory,
          disabled: function () { return !root.querySelector('.comment-new'); }, reason: 'No comment is new since your last visit.',
          run: function () { disc.firstNew(root); } }
      ];
    }

    function menus() {
      if (!d.storyKey) return { titles: [] };
      return {
        titles: [
          { label: 'Reader', items: lib.identityMenu(root, 'Copy story link', sharedPage().href) },
          { label: 'Story', items: ['pin', ...(lib.library ? lib.library.storyMenu(root) : []), '-',
            'new-reader', 'open-original', 'open-source', 'refresh', '-', 'hide-story', 'block-site'] },
          { label: 'Discussion', items: ['sort-best', 'sort-newest', 'sort-oldest', '-', 'collapse-all', 'expand-all'] }
        ],
        into: { go: ['previous-story', 'next-story', '-', 'next-comment', 'previous-comment', 'first-new-comment'],
          view: ['pane-article', 'pane-discussion', 'pane-split'] }
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
      commandTable: commandTable,
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
