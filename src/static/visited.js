/* Stories the reader has opened, kept in this browser under
   yavchn-visited, fade in the list so the eye passes over them on the next
   scan. A story counts as visited once its window opens, however it was
   opened: a row, the keyboard, the dock or an address. */
(function () {
  'use strict';
  var KEY = 'yavchn-visited';
  var CAP = 500;

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

  // An id already present moves to the end, so eviction prefers
  // genuinely cold stories.
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
      row.classList.toggle('visited', !!set[row.dataset.id]);
    });
  }

  function visit(story) {
    if (story && story.dataset.storyId) add(story.dataset.storyId);
  }

  document.querySelectorAll('.story').forEach(visit);
  document.addEventListener('pudl:window-open', function (e) {
    e.target.querySelectorAll('.story').forEach(visit);
    apply();
  });

  window.yavchn.onList(apply);
  document.addEventListener('yavchn:rows-appended', apply);
})();
