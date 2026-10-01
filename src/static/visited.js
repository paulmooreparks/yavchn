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

  /* A read row is marked three ways (style.css): its title is muted and
     lighter, a tick stands before it, and assistive technology hears
     "read" after the row's words, so the state never rests on colour
     alone. The words go last because other scripts read a row's title
     from the link's first child. */
  function apply() {
    var set = {};
    load().forEach(function (id) { set[id] = true; });
    document.querySelectorAll('.story-list .story-row[data-id]').forEach(function (row) {
      var read = !!set[row.dataset.id];
      row.classList.toggle('visited', read);
      var link = row.querySelector('.md-item');
      var label = link && link.querySelector(':scope > .story-read-label');
      if (read && link && !label) {
        label = document.createElement('span');
        label.className = 'visually-hidden story-read-label';
        label.textContent = ', read';
        link.appendChild(label);
      } else if (!read && label) {
        label.remove();
      }
    });
  }

  // For Pinned's Unread filter.
  window.yavchn.visited = { has: function (id) { return load().indexOf(id) >= 0; } };

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
