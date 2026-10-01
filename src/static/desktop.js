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
  var FOCUS_KEY = 'yavchn-focus';
  function listHidden() { return document.documentElement.classList.contains('focus-mode'); }
  function syncFocus() {
    document.querySelectorAll('[data-focus-toggle]').forEach(function (b) {
      b.setAttribute('aria-pressed', listHidden() ? 'true' : 'false');
    });
  }
  function setListHidden(on) {
    document.documentElement.classList.toggle('focus-mode', !!on);
    try { localStorage.setItem(FOCUS_KEY, on ? '1' : '0'); } catch (e) { /* storage blocked */ }
    syncFocus();
  }
  syncFocus();

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
    document.querySelectorAll('.win-bar [data-close-all]').forEach(function (a) {
      if (none) { a.setAttribute('aria-disabled', 'true'); a.tabIndex = -1; }
      else { a.removeAttribute('aria-disabled'); a.removeAttribute('tabindex'); }
    });
  }
  document.addEventListener('pudl:windows-change', syncCloseAll);

  /* === Keys ============================================================= */
  document.addEventListener('keydown', function (e) {
    if (!plainKey(e)) return;
    if (e.key === 'f' && !e.shiftKey) {
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
    var open = window.pudlWindows.state().open;
    var links = Array.prototype.slice.call(list.querySelectorAll('a[data-win-open]'));
    if (!links.length || links.some(function (a) { return open.indexOf(a.getAttribute('data-win-open')) >= 0; })) return;
    window.pudlWindows.open(links[0].getAttribute('data-win-open'), links[0]);
  }
  document.addEventListener('pudl:regions-swap', function () {
    if (!fromHistory) autoOpen();
    fromHistory = false;
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

  function init() {
    syncCloseAll();
    /* After pudl-windows.js has brought the windows in line with the
       address, which settles a task after it starts. */
    setTimeout(autoOpen, 0);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
