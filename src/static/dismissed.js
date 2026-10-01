/* Hidden stories, kept in this browser under yavchn-dismissed. A row's
   cross hides its story from every list, and the button below the list
   brings them all back. */
(function () {
  'use strict';
  var KEY = 'yavchn-dismissed';
  var CAP = 1000;

  function load() {
    try {
      var arr = JSON.parse(localStorage.getItem(KEY) || '[]');
      return Array.isArray(arr) ? arr : [];
    } catch (e) { return []; }
  }

  function save(arr) {
    if (arr.length > CAP) arr = arr.slice(-CAP);
    try { localStorage.setItem(KEY, JSON.stringify(arr)); } catch (e) {}
  }

  // Refresh recency on re-dismiss so eviction prefers genuinely cold IDs.
  function add(id) {
    if (!id) return;
    var arr = load();
    var idx = arr.indexOf(id);
    if (idx >= 0) arr.splice(idx, 1);
    arr.push(id);
    save(arr);
  }

  function apply() {
    var set = {};
    load().forEach(function (id) { set[id] = true; });
    document.querySelectorAll('.story-list .story-row[data-id]').forEach(function (row) {
      row.classList.toggle('dismissed', !!set[row.dataset.id]);
    });
    updateBanner();
  }

  function updateBanner() {
    var banner = document.querySelector('.story-list .story-hidden');
    if (!banner) return;
    var n = document.querySelectorAll('.story-list .story-row.dismissed').length;
    banner.hidden = n === 0;
    var btn = banner.querySelector('.story-show-hidden');
    if (btn && n) btn.textContent = 'Show ' + n + ' hidden ' + (n === 1 ? 'story' : 'stories');
  }

  document.addEventListener('click', function (e) {
    var hide = e.target.closest && e.target.closest('.story-row .story-hide');
    if (hide) {
      e.preventDefault();
      var row = hide.closest('.story-row');
      if (!row || !row.dataset.id) return;
      // The keyboard's mark moves on to the next row before this one goes.
      if (row.classList.contains('focused')) {
        var next = row.nextElementSibling;
        while (next && (!next.classList.contains('story-row') || next.offsetParent === null)) next = next.nextElementSibling;
        row.classList.remove('focused');
        if (next) next.classList.add('focused');
      }
      add(row.dataset.id);
      row.classList.add('dismissed');
      updateBanner();
      return;
    }
    if (e.target.closest && e.target.closest('.story-list .story-show-hidden')) {
      e.preventDefault();
      try { localStorage.removeItem(KEY); } catch (err) {}
      apply();
    }
  });

  // For the story applet's menu, which hides the story in front.
  window.yavchn.hiding = window.yavchn.hiding || {};
  window.yavchn.hiding.hide = function (id) {
    add(id);
    apply();
    document.dispatchEvent(new CustomEvent('yavchn:list-change'));
  };

  window.yavchn.onList(apply);
  document.addEventListener('yavchn:rows-appended', apply);
})();
