(function () {
  var KEY = 'yavchn-visited';
  var CAP = 500;

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

  // LIFO: if id is already present, move it to the end so eviction prefers
  // genuinely-cold stories on overflow.
  function add(id) {
    if (!id) return;
    var arr = load();
    var idx = arr.indexOf(id);
    if (idx >= 0) arr.splice(idx, 1);
    arr.push(id);
    save(arr);
  }

  function apply() {
    var arr = load();
    if (!arr.length) return;
    var set = {};
    for (var i = 0; i < arr.length; i++) set[arr[i]] = true;
    var rows = document.querySelectorAll('.pane-list .story[data-id]');
    for (var j = 0; j < rows.length; j++) {
      if (set[rows[j].dataset.id]) rows[j].classList.add('visited');
    }
  }

  function currentURLStoryID() {
    var m = window.location.pathname.match(/\/s\/(\d+)/);
    return m ? m[1] : null;
  }

  // Mark the URL's story (if any) visited on load, then apply classes.
  add(currentURLStoryID());
  apply();

  // Mark clicks immediately so the fade appears right away (before pane-swap
  // navigates away). swap.js handles the navigation; this just tracks state.
  var list = document.querySelector('.pane-list');
  if (list) {
    list.addEventListener('click', function (e) {
      if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
      if (e.button !== 0) return;
      var a = e.target.closest('.story .title a');
      if (!a) return;
      var row = a.closest('.story');
      if (!row || !row.dataset.id) return;
      add(row.dataset.id);
      row.classList.add('visited');
    });
  }
})();
