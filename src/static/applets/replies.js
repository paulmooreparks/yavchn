/* The replies watcher: the replies to a Hacker News user's recent comments
   and stories, from /api/replies, checked again every few minutes while
   its window or page is open. HN has no notifications, and this needs no
   login, since everything it reads is public.

   Its state is the user it watches and the time of the newest reply the
   reader has marked read, as u=pg&seen=1790000000. A reply newer than that
   carries the word "new", and the window's title counts them. The state
   is kept by continuity.js even when the window closes, because the user
   is a setting rather than a place in a page. */
(function () {
  'use strict';
  var EVERY = 3 * 60 * 1000;

  function init(root, opts) {
    var ctl = new AbortController();
    var form = root.querySelector('.replies-user');
    var input = form.querySelector('input[name="u"]');
    var list = root.querySelector('.replies-list');
    var status = root.querySelector('.replies-status');
    var q = new URLSearchParams(opts.state || '');
    var user = (input.value || q.get('u') || '').trim();
    var seen = parseInt(q.get('seen') || '0', 10) || 0;
    var timer = 0;
    var win = root.closest('.win');
    var key = win && win.getAttribute('data-win');
    input.value = user;

    function state() {
      var s = new URLSearchParams();
      if (user) s.set('u', user);
      if (seen) s.set('seen', String(seen));
      return s.toString();
    }

    function times() {
      return Array.prototype.map.call(list.querySelectorAll('.reply[data-time]'), function (li) {
        return parseInt(li.dataset.time, 10) || 0;
      });
    }

    function unread() {
      return list.querySelectorAll('.reply.reply-new').length;
    }

    /* Marks what is newer than seen, and says how many in the title, so
       the dock tab shows it with the window minimised. */
    function mark() {
      list.querySelectorAll('.reply[data-time]').forEach(function (li) {
        li.classList.toggle('reply-new', (parseInt(li.dataset.time, 10) || 0) > seen);
      });
      var n = unread();
      var title = 'Replies to ' + (user || 'me') + (n ? ' (' + n + ' new)' : '');
      if (key && window.pudlWindows) window.pudlWindows.retitle(key, title);
      else document.title = title + ' · YAVCHN';
    }

    function markRead() {
      seen = Math.max.apply(null, [seen].concat(times()));
      mark();
      opts.changed(state());
    }

    function check() {
      if (!user) return;
      status.textContent = 'Checking for replies to ' + user + '…';
      fetch('/api/replies?u=' + encodeURIComponent(user), { credentials: 'omit', signal: ctl.signal })
        .then(function (r) { return r.text(); })
        .then(function (html) {
          list.innerHTML = html;
          // Watching a new user starts with what is there already read.
          if (!seen) { seen = Math.max.apply(null, [1].concat(times())); opts.changed(state()); }
          mark();
          status.textContent = 'Checked at ' + new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) +
            '. Checking again every three minutes.';
        })
        .catch(function (err) {
          if (err && err.name === 'AbortError') return;
          status.textContent = 'The check failed; it will try again in three minutes.';
        });
    }

    function watch(name) {
      name = (name || '').trim();
      if (!/^[A-Za-z0-9_-]{1,32}$/.test(name)) {
        input.setCustomValidity('Enter a Hacker News user name.');
        input.reportValidity();
        return;
      }
      input.setCustomValidity('');
      if (name !== user) { user = name; seen = 0; }
      opts.changed(state());
      if (opts.ownsUrl) history.replaceState(history.state, '', location.pathname + '?u=' + encodeURIComponent(user));
      check();
    }

    form.addEventListener('submit', function (e) {
      e.preventDefault();
      watch(input.value);
    }, { signal: ctl.signal });
    input.addEventListener('input', function () { input.setCustomValidity(''); }, { signal: ctl.signal });

    // The server renders the list on the applet's own page; anywhere else
    // the first check fills it.
    if (list.querySelector('.replies')) {
      if (!seen) { seen = Math.max.apply(null, [1].concat(times())); opts.changed(state()); }
      mark();
    } else {
      check();
    }
    timer = setInterval(check, EVERY);

    return {
      state: state,
      menus: function () {
        var n = unread();
        return { titles: [{ label: 'Replies', items: [
          { label: 'Check now', disabled: !user, run: check },
          { label: 'Mark all as read', disabled: !n, run: markRead },
          '-',
          { label: 'Watch another user…', run: function () { input.focus(); input.select(); } }
        ] }] };
      },
      destroy: function () {
        clearInterval(timer);
        ctl.abort();
      }
    };
  }

  window.pudlApplets.register('replies', { init: init });
})();
