/* The desktop's own wiring on top of PUDL's scripts, and the helpers the
   other YAVCHN scripts share as window.yavchn.

   - The menu bar's theme choices and its "Hide the story list" toggle,
     which the f key also flips.
   - The ? key, which shows the help.
   - Close-all, in the window bar and the Window menu, which PUDL has no
     attribute for.
   - The finder opening its first submission by itself.
   - Clearing the search box returning to the front page. */
(function () {
  'use strict';

  /* Keep the reading shell inside the visible viewport on navigation and
     restoration as well as resize. CSS viewport units remain the fallback. */
  function syncViewportHeight() {
    var viewport = window.visualViewport;
    // Pinch zoom magnifies the existing layout without changing its height.
    if (viewport && viewport.scale !== 1) return;
    var height = viewport ? viewport.height : window.innerHeight;
    // An inactive document can report a zero-sized visual viewport.
    if (height > 0) document.documentElement.style.setProperty('--yv-viewport-height', height + 'px');
  }
  syncViewportHeight();
  window.addEventListener('pageshow', syncViewportHeight);
  window.addEventListener('resize', syncViewportHeight);
  if (window.visualViewport) window.visualViewport.addEventListener('resize', syncViewportHeight);

  function inEditable(t) {
    if (!t || !t.closest) return false;
    return !!t.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]');
  }

  /* A bare key press meant for the page, not for a field or a shortcut. */
  function plainKey(e) {
    return !e.ctrlKey && !e.altKey && !e.metaKey && !inEditable(e.target);
  }

  function plainClick(e) {
    return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
  }

  /* Runs fn now, and again whenever pudl-regions.js swaps in a new list. */
  function onList(fn) {
    fn();
    document.addEventListener('pudl:regions-swap', function () { fn(); });
  }

  /* The story the keyboard acts on: the one in the window in front, or the
     story of a story's own page. */
  function currentStory() {
    return document.querySelector('.win.active:not([hidden]) .story') ||
      document.querySelector('.story-page-main .story');
  }

  window.yavchn = {
    inEditable: inEditable,
    plainKey: plainKey,
    plainClick: plainClick,
    onList: onList,
    currentStory: currentStory
  };

  /* === Theme ============================================================ */
  function syncTheme() {
    var pref = window.pudlThemePreference ? window.pudlThemePreference() : 'system';
    document.querySelectorAll('[data-theme-choice]').forEach(function (b) {
      b.setAttribute('aria-checked', b.getAttribute('data-theme-choice') === pref ? 'true' : 'false');
    });
  }
  document.addEventListener('pudl:theme-change', syncTheme);
  syncTheme();

  /* === Hiding the story list =========================================== */
  var layout = document.querySelector('.md-layout');
  var mobileWindows = null;
  function narrowLayout() { return layout && layout.getBoundingClientRect().width <= 640; }
  function listHidden() {
    return narrowLayout() ? layout.dataset.mdPane === 'detail' : layout && layout.hasAttribute('data-md-collapsed');
  }
  function syncFocus() {
    var hidden = listHidden();
    document.querySelectorAll('[data-focus-toggle]').forEach(function (b) {
      b.setAttribute('aria-pressed', hidden ? 'true' : 'false');
      if (b.classList.contains('sidebar-toggle')) {
        b.hidden = false;
        b.setAttribute('aria-expanded', hidden ? 'false' : 'true');
        var label = hidden ? 'Show sidebar' : narrowLayout() ? 'Show article windows' : 'Hide sidebar';
        b.setAttribute('aria-label', label);
        b.title = label;
        b.disabled = !!(narrowLayout() && !hidden && (!window.pudlWindows || !window.pudlWindows.state().open.length));
      }
    });
  }
  function setListHidden(on) {
    if (narrowLayout()) {
      if (!window.pudlWindows) return;
      var state = window.pudlWindows.state();
      if (on) {
        var keys = mobileWindows ? mobileWindows.keys.filter(function (key) { return state.open.indexOf(key) >= 0; }) : [];
        var key = mobileWindows && keys.indexOf(mobileWindows.top) >= 0 ? mobileWindows.top : state.top || state.open[state.open.length - 1];
        if (!key) return;
        keys.filter(function (k) { return k !== key; }).forEach(function (k) { window.pudlWindows.raise(k); });
        window.pudlWindows.raise(key);
      } else {
        mobileWindows = { keys: state.open.filter(function (key) { return !state.min[key]; }), top: state.top };
        window.pudlWindows.minimizeAll();
      }
    } else {
      if (window.pudlMd) window.pudlMd.command(layout, on ? 'collapse' : 'expand');
    }
    syncFocus();
  }
  if (layout) {
    new MutationObserver(syncFocus).observe(layout, { attributes: true, attributeFilter: ['data-md-pane', 'data-md-collapsed'] });
    new ResizeObserver(syncFocus).observe(layout);
  }
  if (layout) {
    layout.addEventListener('pudl:md-request', function (e) {
      // The window manager owns narrow pane selection and its URL state.
      setListHidden(e.detail.pane === 'detail');
    });
    layout.addEventListener('pudl:md-change', syncFocus);
  }
  document.addEventListener('pudl:windows-change', syncFocus);
  syncFocus();

  /* On a phone, choosing a source while reading an article is a request
     for that source's list, so the list comes back once it has loaded.
     Waiting for the swap leaves the article in the history entry it was
     read in. Choosing the source already shown brings the list back now,
     since no swap follows. */
  var listWanted = false;
  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('.source-switch a[href]');
    if (!a || !plainClick(e) || !narrowLayout() || !listHidden()) return;
    if (a.getAttribute('aria-current') === 'page') setListHidden(false);
    else listWanted = true;
  });

  document.addEventListener('click', function (e) {
    var t = e.target.closest && e.target.closest('[data-theme-choice], [data-focus-toggle], [data-close-all]');
    if (!t) return;
    if (t.hasAttribute('data-theme-choice')) {
      if (window.pudlSetTheme) window.pudlSetTheme(t.getAttribute('data-theme-choice'));
      syncTheme();
    } else if (t.hasAttribute('data-focus-toggle')) {
      setListHidden(!listHidden());
    } else if (plainClick(e) && window.pudlWindows) {
      /* Close-all closes every top-level window in place; a click that
         isn't plain follows the link to the windowless address. */
      e.preventDefault();
      var layer = document.querySelector('[data-win-layer]');
      window.pudlWindows.state().open.forEach(function (key) {
        var el = layer && layer.querySelector('.win[data-win="' + key + '"]');
        if (el && !el.hasAttribute('data-win-parent')) window.pudlWindows.close(key);
      });
    }
  });

  function syncCloseAll() {
    var none = !window.pudlWindows || window.pudlWindows.state().open.length === 0;
    document.querySelectorAll('[data-close-all]').forEach(function (a) {
      if (none) { a.setAttribute('aria-disabled', 'true'); a.tabIndex = -1; }
      else { a.removeAttribute('aria-disabled'); a.removeAttribute('tabindex'); }
    });
  }
  document.addEventListener('pudl:windows-change', syncCloseAll);
  syncCloseAll();

  /* === Keys ============================================================= */
  document.addEventListener('keydown', function (e) {
    if (!plainKey(e)) return;
    if (e.key === 'f' && !e.shiftKey && document.querySelector('[data-win-layer]') && document.querySelector('.story-list')) {
      e.preventDefault();
      setListHidden(!listHidden());
    } else if (e.key === '?') {
      var help = document.getElementById('help-dialog');
      if (!help || typeof help.showModal !== 'function') return;
      e.preventDefault();
      if (help.open) { help.close(); return; }
      document.querySelectorAll('dialog[open]').forEach(function (d) { d.close(); });
      help.showModal();
    }
  });

  /* === The finder's first submission ====================================
     A finder list that arrives with none of its submissions open opens the
     first, as the finder always has. Arriving by Back or Forward is left
     alone, since the address then says which windows were open. */
  var fromHistory = false;
  window.addEventListener('popstate', function () { fromHistory = true; });
  document.addEventListener('click', function () { fromHistory = false; }, true);
  document.addEventListener('submit', function () { fromHistory = false; }, true);

  function autoOpen() {
    var list = document.querySelector('.story-list[data-auto-open]');
    if (!list || !window.pudlWindows) return;
    var open = Array.prototype.map.call(document.querySelectorAll('.win .story[data-story-key]'), function (story) {
      return story.dataset.storyKey;
    });
    var links = Array.prototype.slice.call(list.querySelectorAll('a[data-win-open]'));
    if (!links.length || links.some(function (a) { return open.indexOf(a.getAttribute('data-win-open')) >= 0; })) return;
    window.yavchn.readers.read(links[0].getAttribute('data-win-open'), links[0]);
  }
  document.addEventListener('pudl:regions-swap', function () {
    // A list asked for on a phone stays in view, so the finder opens nothing.
    if (listWanted) { listWanted = false; setListHidden(false); }
    else if (!fromHistory) autoOpen();
    fromHistory = false;
  });

  /* === Where windows open ===============================================
     PUDL opens a window from a link in another window in that window's
     state, which suits a story opened from a story. An applet is a tool
     beside the reading rather than more of it, so it opens where its
     markup places it however it is opened, and a story opened from an
     applet's window opens as a story does from the list, maximised. The
     server's markup (applets.go, windows.go) is where those places are
     written; this only stops the opener from overriding them. */
  var STORY_PLACE = { mode: 'maximized', x: 0.06, y: 0.05, w: 0.55, h: 0.75 }; // windows.go: storyWinMode, storyWinFloat

  function winEl(layer, key) { return key ? layer.querySelector(':scope > .win[data-win="' + CSS.escape(key) + '"]') : null; }

  function markupPlace(el) {
    var s = el.style;
    var n = ['--win-x', '--win-y', '--win-w', '--win-h'].map(function (p) { return parseFloat(s.getPropertyValue(p)); });
    if (n.some(isNaN)) return null;
    return { mode: el.getAttribute('data-win-mode') || 'floating', x: n[0], y: n[1], w: n[2], h: n[3] };
  }

  function placeWindows(layer) {
    layer.addEventListener('pudl:window-place', function (e) {
      if (e.detail.placement || !e.detail.opener) return;
      var el = winEl(layer, e.detail.key), opener = winEl(layer, e.detail.opener);
      if (!el) return;
      if (el.classList.contains('app-win')) e.detail.placement = markupPlace(el);
      else if (opener && opener.classList.contains('app-win')) e.detail.placement = Object.assign({}, STORY_PLACE);
    });
  }

  /* The lookup bar, alone in the lookup or above a profile, shows the
     profile it names in its window's place, so one window looks up user
     after user, and Back returns to the one before. On a page of its own
     it is an ordinary form, which the server redirects. */
  document.addEventListener('submit', function (e) {
    var form = e.target.closest && e.target.closest('.user-lookup');
    var win = form && form.closest('.win[data-win]');
    if (!win || !window.pudlWindows) return;
    var name = form.elements.name.value.trim();
    var source = form.elements.source.value;
    if (!/^[A-Za-z0-9_-]{1,32}$/.test(name)) return;
    e.preventDefault();
    window.pudlWindows.replace(win.getAttribute('data-win'), 'user-' + source + '-' + name);
  });

  /* === Search ===========================================================
     Clearing the search box on a results page returns to the front page,
     through the list's own Top tab, so the windows stay. */
  document.addEventListener('input', function (e) {
    var input = e.target;
    if (!input.matches || !input.matches('.site-listbar input[name="q"]') || input.value !== '') return;
    if (location.pathname !== '/hn/search') return;
    var top = document.querySelector('.list-tabs a');
    if (top) top.click();
  });

  function showFeedRefresh() {
    var button = document.querySelector('#feed-refresh');
    var available = !!(button && window.pudlRegions && window.pudlRegions.reload);
    if (available) button.hidden = false;
    document.querySelectorAll('[data-feed-refresh]').forEach(function (command) {
      command.disabled = !available || button.disabled;
    });
  }
  onList(showFeedRefresh);
  document.addEventListener('click', function (e) {
    if (!e.target.closest || !e.target.closest('#feed-refresh, [data-feed-refresh]')) return;
    var button = document.querySelector('#feed-refresh');
    if (!button || button.disabled) return;
    var status = button.parentNode.querySelector('.feed-refresh-status');
    var restoreButtonFocus = document.activeElement === button;
    button.disabled = true;
    showFeedRefresh();
    button.setAttribute('aria-busy', 'true');
    status.hidden = false;
    status.classList.add('visually-hidden');
    status.textContent = 'Refreshing feed...';
    window.pudlRegions.reload().then(function (changed) {
      if (!changed) return;
      var freshStatus = document.querySelector('.feed-refresh-status');
      if (freshStatus) { freshStatus.hidden = false; freshStatus.textContent = 'Feed refreshed.'; }
      if (restoreButtonFocus && document.activeElement === document.body) document.querySelector('#feed-refresh').focus();
    }).catch(function () {
      if (!status.isConnected) return;
      status.hidden = false;
      status.classList.remove('visually-hidden');
      status.textContent = 'The feed could not be refreshed. Try again.';
    }).finally(function () {
      if (button.isConnected) {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
      showFeedRefresh();
    });
  });

  /* Before pudl-windows.js lays out the windows the address names, which
     it does a task after it starts, so a profile in the address with no
     placement floats where the listener says. */
  var layerNow = document.querySelector('[data-win-layer]');
  if (layerNow) placeWindows(layerNow);

  function init() {
    syncCloseAll();
    /* After pudl-windows.js has brought the windows in line with the
       address, which settles a task after it starts. */
    setTimeout(autoOpen, 0);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
