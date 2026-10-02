/* The hiring filter: the posts of one of HN's monthly "Who is hiring?"
   threads, filtered as the reader types. Every word must appear in a
   post, a "quoted phrase" is taken whole, and a -word leaves out the
   posts that have it, the grammar the server's own filter reads
   (applets.go, parseTerms), so the page without script finds the same
   posts.

   Its state is the thread and the words, as t=41234567&q=remote+rust. On
   its own page they are the address, so a filtered list can be shared. */
(function () {
  'use strict';

  function parseTerms(q) {
    var want = [], not = [];
    (q.toLowerCase().match(/-?"[^"]*"|\S+/g) || []).forEach(function (tok) {
      var neg = tok.charAt(0) === '-' && tok.length > 1;
      tok = tok.replace(/^-/, '').replace(/^"|"$/g, '');
      if (tok) (neg ? not : want).push(tok);
    });
    return { want: want, not: not };
  }

  function matches(text, t) {
    return t.want.every(function (w) { return text.indexOf(w) >= 0; }) &&
      !t.not.some(function (n) { return text.indexOf(n) >= 0; });
  }

  function init(root, opts) {
    var ctl = new AbortController();
    var form = root.querySelector('.hiring-bar');
    if (!form) return { destroy: function () {} };
    var select = form.querySelector('select[name="t"]');
    var input = form.querySelector('input[name="q"]');
    var count = root.querySelector('.hiring-count');
    var posts = root.querySelector('.hiring-posts');
    var texts = new WeakMap();
    var timer = 0;

    var st = new URLSearchParams(opts.state || '');
    if (!opts.ownsUrl) {
      if (st.get('t') && select.querySelector('option[value="' + CSS.escape(st.get('t')) + '"]')) select.value = st.get('t');
      if (st.has('q') && !input.value) input.value = st.get('q');
    }

    function state() {
      var s = new URLSearchParams();
      s.set('t', select.value);
      if (input.value.trim()) s.set('q', input.value.trim());
      return s.toString();
    }

    function save() {
      opts.changed(state());
      if (opts.ownsUrl) history.replaceState(history.state, '', location.pathname + '?' + state());
    }

    function textOf(li) {
      var t = texts.get(li);
      if (t == null) {
        // The post's words and its author, as the server's filter reads.
        var body = li.querySelector('.comment-text'), who = li.querySelector('.comment-author');
        t = ((body ? body.textContent : '') + ' ' + (who ? who.textContent : '')).toLowerCase();
        texts.set(li, t);
      }
      return t;
    }

    function filter() {
      var jobs = posts.querySelectorAll('.job');
      var t = parseTerms(input.value);
      var shown = 0;
      jobs.forEach(function (li) {
        var ok = matches(textOf(li), t);
        li.hidden = !ok;
        if (ok) shown++;
      });
      if (jobs.length) count.textContent = shown + ' of ' + jobs.length + ' posts';
    }

    function load() {
      count.textContent = '';
      posts.innerHTML = '<div class="loading" role="status"><span class="spinner" aria-hidden="true"></span> Loading the posts…</div>';
      fetch('/api/hiring?id=' + encodeURIComponent(select.value), { credentials: 'omit', signal: ctl.signal })
        .then(function (r) { return r.text(); })
        .then(function (html) {
          posts.innerHTML = html;
          posts.scrollTop = 0;
          filter();
        })
        .catch(function (err) {
          if (err && err.name === 'AbortError') return;
          posts.innerHTML = '<div class="empty-state story-note"><p class="empty-state-body">The posts couldn\'t be loaded. Try again in a moment.</p></div>';
        });
    }

    form.addEventListener('submit', function (e) { e.preventDefault(); filter(); save(); }, { signal: ctl.signal });
    input.addEventListener('input', function () {
      clearTimeout(timer);
      timer = setTimeout(function () { filter(); save(); }, 150);
    }, { signal: ctl.signal });
    select.addEventListener('change', function () { load(); save(); }, { signal: ctl.signal });

    // The server renders the posts on the applet's own page; anywhere else
    // the first load fetches them.
    var have = posts.querySelector('.jobs');
    if (have && have.dataset.thread === select.value) filter();
    else load();

    return {
      state: state,
      setState: function (s) {
        var q = new URLSearchParams(s || '');
        input.value = q.get('q') || '';
        if (q.get('t') && q.get('t') !== select.value) { select.value = q.get('t'); load(); }
        else filter();
      },
      menus: function () {
        var threads = Array.prototype.map.call(select.options, function (o) {
          return { label: o.textContent, radio: 'thread', checked: o.selected,
            run: function () { select.value = o.value; load(); save(); } };
        });
        return { titles: [{ label: 'Hiring', items: window.yavchn.identityMenu(root, 'Copy hiring search link') }, { label: 'Search', items: threads.concat(['-',
          { label: 'Clear the filter', disabled: !input.value, run: function () { input.value = ''; filter(); save(); } }]) }] };
      },
      destroy: function () {
        clearTimeout(timer);
        ctl.abort();
      }
    };
  }

  window.pudlApplets.register('hiring', { init: init });
})();
