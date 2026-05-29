(function () {
  var pane = document.querySelector('.pane-discussion.finder-disc');
  if (!pane) return;
  var thread = pane.querySelector('.finder-thread');
  if (!thread) return;

  function groups() { return Array.prototype.slice.call(pane.querySelectorAll('.finder-group')); }
  function tabs() { return Array.prototype.slice.call(pane.querySelectorAll('.finder-tab')); }

  // Show one source's submission group, hide the rest; mark its tab active.
  function showSource(source) {
    tabs().forEach(function (t) { t.classList.toggle('active', t.dataset.source === source); });
    groups().forEach(function (g) { g.style.display = g.dataset.source === source ? '' : 'none'; });
  }

  // Load a submission's thread into the pane via the existing source-aware
  // discussion API, then dispatch yavchn:loaded so collapse / sort / n-N /
  // highlight-new (discussion.js, sort.js) wire up exactly as on the
  // browse pages.
  function loadThread(source, id) {
    pane.querySelectorAll('.finder-sub').forEach(function (s) {
      s.classList.toggle('active', s.dataset.source === source && s.dataset.id === id);
    });
    // Set the dataset the downstream listeners read (collapse persistence
    // keys off discussionId; sort keys off the pane).
    pane.dataset.discussionId = id;
    pane.dataset.discussionSource = source;
    thread.innerHTML = '<p class="empty-note">Loading discussion&hellip;</p>';
    fetch('/api/discussion?id=' + encodeURIComponent(id) + '&source=' + encodeURIComponent(source), { credentials: 'omit' })
      .then(function (r) { return r.text(); })
      .then(function (html) {
        thread.innerHTML = html;
        thread.scrollTop = 0;
        thread.dispatchEvent(new CustomEvent('yavchn:loaded', { bubbles: true }));
      })
      .catch(function () {
        thread.innerHTML = '<p class="empty-note">Couldn\'t load this discussion. Use the "open &uarr;" link on the submission.</p>';
      });
  }

  // Tab click: switch source, select its first submission, load it.
  pane.addEventListener('click', function (e) {
    var tab = e.target.closest('.finder-tab');
    if (tab) {
      showSource(tab.dataset.source);
      var firstSub = pane.querySelector('.finder-group[data-source="' + tab.dataset.source + '"] .finder-sub');
      if (firstSub) loadThread(firstSub.dataset.source, firstSub.dataset.id);
      return;
    }
    var sub = e.target.closest('.finder-sub');
    if (sub && !e.target.closest('.finder-sub-open')) {
      loadThread(sub.dataset.source, sub.dataset.id);
    }
  });

  // Bootstrap: activate the first submission of the first source on load.
  var first = pane.querySelector('.finder-sub.active') || pane.querySelector('.finder-sub');
  if (first) {
    showSource(first.dataset.source);
    loadThread(first.dataset.source, first.dataset.id);
  }
})();
