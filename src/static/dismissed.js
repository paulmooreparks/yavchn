(function () {
  var KEY = 'yavchn-dismissed';
  var CAP = 1000;

  function load() {
    try {
      var raw = localStorage.getItem(KEY);
      if (!raw) return [];
      var arr = JSON.parse(raw);
      return Array.isArray(arr) ? arr : [];
    } catch (e) { return []; }
  }

  function save(arr) {
    if (arr.length > CAP) arr = arr.slice(-CAP);
    try { localStorage.setItem(KEY, JSON.stringify(arr)); } catch (e) {}
  }

  // LIFO: refresh recency on re-dismiss so eviction prefers genuinely cold IDs.
  function add(id) {
    if (!id) return;
    var arr = load();
    var idx = arr.indexOf(id);
    if (idx >= 0) arr.splice(idx, 1);
    arr.push(id);
    save(arr);
  }

  function clear() {
    try { localStorage.removeItem(KEY); } catch (e) {}
  }

  function applyAndBanner() {
    var arr = load();
    var rows = document.querySelectorAll('.pane-list .story[data-id]');
    if (arr.length) {
      var set = {};
      for (var i = 0; i < arr.length; i++) set[arr[i]] = true;
      for (var j = 0; j < rows.length; j++) {
        rows[j].classList.toggle('dismissed', !!set[rows[j].dataset.id]);
      }
    } else {
      for (var k = 0; k < rows.length; k++) rows[k].classList.remove('dismissed');
    }
    updateBanner();
  }

  function updateBanner() {
    var banner = document.querySelector('.pane-list .hidden-banner');
    if (!banner) return;
    var hiddenRows = document.querySelectorAll('.pane-list .story.dismissed').length;
    if (hiddenRows === 0) {
      banner.hidden = true;
      return;
    }
    var btn = banner.querySelector('.hidden-banner-show');
    if (btn) {
      var noun = hiddenRows === 1 ? 'story' : 'stories';
      btn.textContent = hiddenRows + ' hidden ' + noun + ' — show all';
    }
    banner.hidden = false;
  }

  applyAndBanner();

  // Re-apply when infinite-scroll appends fresh rows from page 2/3/etc.
  document.addEventListener('yavchn:rows-appended', function () { applyAndBanner(); });

  // Delegated click handler -- survives pane-swap and lazy-loaded list rows.
  document.addEventListener('click', function (e) {
    var dismissBtn = e.target.closest('.pane-list .dismiss-btn');
    if (dismissBtn) {
      e.preventDefault();
      e.stopPropagation();
      var row = dismissBtn.closest('.story');
      if (!row || !row.dataset.id) return;
      add(row.dataset.id);
      row.classList.add('dismissed');
      updateBanner();
      return;
    }
    var showAll = e.target.closest('.pane-list .hidden-banner-show');
    if (showAll) {
      e.preventDefault();
      clear();
      var rows = document.querySelectorAll('.pane-list .story.dismissed');
      for (var i = 0; i < rows.length; i++) rows[i].classList.remove('dismissed');
      updateBanner();
    }
  });
})();
