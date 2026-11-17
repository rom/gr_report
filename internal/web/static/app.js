// gr_report progressive enhancement. Every page works without this file;
// it only adds conveniences. No inline script is used anywhere, so the
// Content-Security-Policy can stay at script-src 'self'.
(function () {
  'use strict';

  function each(sel, fn, root) {
    Array.prototype.forEach.call((root || document).querySelectorAll(sel), fn);
  }

  // Controls that only make sense with scripting are rendered hidden.
  each('[data-js-show]', function (el) { el.hidden = false; });

  // Settings: preview the theme while choosing.
  var theme = document.getElementById('theme');
  if (theme) {
    theme.addEventListener('change', function () {
      document.documentElement.setAttribute('data-theme', theme.value);
    });
  }

  // Queue: select or clear all reports on the page.
  var all = document.getElementById('select-all');
  if (all) {
    all.addEventListener('change', function () {
      each('input[type=checkbox][name=id]', function (box) { box.checked = all.checked; });
    });
  }

  // Template editor: add another empty field row without saving first.
  var add = document.getElementById('add-field');
  var rows = document.querySelector('input[name=rows]');
  if (add && rows) {
    add.addEventListener('click', function () {
      var list = document.querySelectorAll('.fieldrow[data-row]');
      var max = parseInt(add.getAttribute('data-max'), 10);
      var n = parseInt(rows.value, 10);
      if (!list.length || n >= max) { add.disabled = true; return; }
      var row = list[list.length - 1].cloneNode(true);
      row.setAttribute('data-row', n);
      each('input, select', function (el) {
        el.name = el.name.replace(/^f\d+_/, 'f' + n + '_');
        var label = el.getAttribute('aria-label');
        if (label) { el.setAttribute('aria-label', label.replace(/\d+$/, n + 1)); }
        if (el.type === 'checkbox') { el.checked = false; }
        else if (el.tagName === 'SELECT') { el.selectedIndex = 0; }
        else { el.value = ''; }
      }, row);
      var legend = row.querySelector('legend');
      if (legend) { legend.textContent = legend.textContent.replace(/\d+$/, n + 1); }
      list[list.length - 1].parentNode.insertBefore(row, list[list.length - 1].nextSibling);
      rows.value = n + 1;
      var first = row.querySelector('input[type=text]');
      if (first) { first.focus(); }
    });
  }

  // Printing (report page, list of new accounts).
  var print = document.getElementById('print');
  if (print) { print.addEventListener('click', function () { window.print(); }); }

  // Preferences that only make sense per browser live in localStorage.
  function pref(key, value) {
    try {
      if (value === undefined) { return window.localStorage.getItem('gr_' + key) === '1'; }
      window.localStorage.setItem('gr_' + key, value ? '1' : '0');
    } catch (e) { /* storage disabled */ }
    return false;
  }

  // A short two-tone beep made with Web Audio, so no sound file is needed.
  function beep() {
    try {
      var Ctx = window.AudioContext || window.webkitAudioContext;
      if (!Ctx) { return; }
      var ctx = new Ctx();
      [880, 1175].forEach(function (freq, i) {
        var osc = ctx.createOscillator();
        var gain = ctx.createGain();
        osc.frequency.value = freq;
        gain.gain.value = 0.08;
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.start(ctx.currentTime + i * 0.18);
        osc.stop(ctx.currentTime + i * 0.18 + 0.15);
      });
      window.setTimeout(function () { ctx.close(); }, 800);
    } catch (e) { /* audio not available or not yet allowed */ }
  }

  function notify(text) {
    if (pref('sound')) { beep(); }
    if (pref('desktop') && window.Notification && Notification.permission === 'granted') {
      try { new Notification(text, { tag: 'gr_report' }); } catch (e) { /* not allowed here */ }
    }
  }

  // Settings: switches for notifications and a test button.
  var nDesktop = document.getElementById('notify-desktop');
  var nSound = document.getElementById('notify-sound');
  var nStatus = document.getElementById('notify-status');
  if (nDesktop && nSound) {
    nDesktop.checked = pref('desktop') && window.Notification && Notification.permission === 'granted';
    nSound.checked = pref('sound');
    nSound.addEventListener('change', function () { pref('sound', nSound.checked); });
    nDesktop.addEventListener('change', function () {
      nStatus.textContent = '';
      if (!nDesktop.checked) { pref('desktop', false); return; }
      if (!window.Notification) {
        nDesktop.checked = false;
        nStatus.textContent = nStatus.getAttribute('data-unsupported');
        return;
      }
      Notification.requestPermission().then(function (result) {
        var ok = result === 'granted';
        nDesktop.checked = ok;
        pref('desktop', ok);
        if (!ok) { nStatus.textContent = nStatus.getAttribute('data-denied'); }
      });
    });
    document.getElementById('notify-test').addEventListener('click', function () {
      notify(document.title);
    });
  }

  // Settings: fill in the time zone of this device.
  var tzDetect = document.getElementById('tz-detect');
  if (tzDetect && window.Intl && Intl.DateTimeFormat) {
    tzDetect.addEventListener('click', function () {
      var zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
      if (zone) { document.getElementById('tz').value = zone; }
    });
  }

  // Report form: save a draft in the background while the user writes, so
  // an expired session or a closed tab does not lose the text.
  var form = document.querySelector('form[data-autosave]');
  var saveStatus = document.getElementById('autosave-status');
  if (form && saveStatus && window.fetch && window.FormData && window.URLSearchParams) {
    var dirty = false;
    var saving = false;
    form.addEventListener('input', function () { dirty = true; });
    form.addEventListener('submit', function () { dirty = false; });
    window.setInterval(function () {
      if (!dirty || saving) { return; }
      dirty = false;
      saving = true;
      var body = new URLSearchParams(new FormData(form));
      body.set('action', 'draft');
      // Not form.action: the form has controls named "action", which
      // shadow that property.
      fetch(form.getAttribute('action'), { method: 'POST', credentials: 'same-origin', body: body, headers: { 'X-Autosave': '1' } })
        .then(function (r) {
          saving = false;
          saveStatus.textContent = saveStatus.getAttribute(r.status === 204 ? 'data-saved' : 'data-failed');
        })
        .catch(function () {
          saving = false;
          dirty = true;
          saveStatus.textContent = saveStatus.getAttribute('data-failed');
        });
    }, 15000);
  }

  // Queue: keep the list current. The page itself is fetched again and
  // parsed into an inert document; the fresh rows, counters and queue list
  // replace the old ones. Ticked boxes and the keyboard focus are kept.
  var liveTable = document.querySelector('table[data-live]');
  if (liveTable && window.fetch && window.DOMParser) {
    var swap = function (fresh, id) {
      var from = fresh.getElementById(id);
      var to = document.getElementById(id);
      if (!from || !to) { return; }
      var kept = {};
      each('input[type=checkbox][name=id]:checked', function (box) { kept[box.value] = true; }, to);
      // Remember which control has the focus, to put it back afterwards.
      var active = document.activeElement;
      var focusSel = null;
      if (active && to.contains(active)) {
        if (active.name === 'id') { focusSel = 'input[name=id][value="' + active.value + '"]'; }
        else if (active.getAttribute('href')) { focusSel = 'a[href="' + active.getAttribute('href') + '"]'; }
      }
      var copy = document.importNode(from, true);
      each('input[type=checkbox][name=id]', function (box) { box.checked = !!kept[box.value]; }, copy);
      to.parentNode.replaceChild(copy, to);
      if (focusSel) {
        var again = copy.querySelector(focusSel);
        if (again) { again.focus(); }
      }
    };
    window.setInterval(function () {
      if (document.hidden) { return; }
      fetch(window.location.href, { credentials: 'same-origin', headers: { 'Accept': 'text/html' } })
        .then(function (r) { return r.ok && r.url.indexOf('/login') === -1 ? r.text() : null; })
        .then(function (text) {
          if (!text) { return; }
          var fresh = new DOMParser().parseFromString(text, 'text/html');
          ['queue-body', 'queue-list', 'queue-status'].forEach(function (id) { swap(fresh, id); });
        })
        .catch(function () { /* offline: try again next time */ });
    }, 10000);
  }

  // Tell the user when new reports arrive, without reloading the page and
  // losing what they are typing. The notice is a polite live region.
  var live = document.getElementById('live');
  if (live && window.fetch) {
    var seen = parseInt(live.getAttribute('data-count'), 10) || 0;
    var seenChat = parseInt(live.getAttribute('data-chat'), 10) || 0;
    var baseTitle = document.title;
    var poll = function () {
      fetch(live.getAttribute('data-url'), { credentials: 'same-origin', headers: { 'Accept': 'application/json' } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (data) {
          if (!data || typeof data.new !== 'number') { return; }
          each('.js-new-count', function (el) {
            el.textContent = data.new;
            el.hidden = data.new === 0;
          });
          if (typeof data.chat === 'number') {
            each('.js-chat-count', function (el) {
              el.textContent = data.chat;
              el.hidden = data.chat === 0;
            });
          }
          document.title = (data.new ? '(' + data.new + ') ' : '') + baseTitle;
          if (data.new > seen) {
            // On the queue itself the list updates; no notice is needed.
            if (!document.querySelector('table[data-live]')) { live.hidden = false; }
            notify(live.getAttribute('data-notify-report'));
          }
          seen = data.new;
          if (typeof data.chat === 'number') {
            if (data.chat > seenChat) { notify(live.getAttribute('data-notify-chat')); }
            seenChat = data.chat;
          }
        })
        .catch(function () { /* offline: try again next time */ });
    };
    window.setInterval(poll, 15000);
  }

  // Chat: fetch new messages every few seconds and append them. Lines are
  // built from a <template> with textContent, never from HTML strings.
  var log = document.getElementById('chat-log');
  var lineTpl = document.getElementById('chat-line');
  if (log && lineTpl && window.fetch && log.getAttribute('data-url')) {
    var after = parseInt(log.getAttribute('data-after'), 10) || 0;
    var box = log.parentNode;
    var atBottom = function () { return box.scrollHeight - box.scrollTop - box.clientHeight < 40; };
    box.scrollTop = box.scrollHeight;
    var busy = false;
    var pollChat = function () {
      if (busy) { return; }
      busy = true;
      fetch(log.getAttribute('data-url') + '?after=' + after, { credentials: 'same-origin', headers: { 'Accept': 'application/json' } })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (data) {
          busy = false;
          if (!data) { return; }
          // Oversight may be switched on or off while the page is open:
          // show or hide the notice at once, and record in the form which
          // state the writer has been shown.
          var seen = document.getElementById('oversight-seen');
          if (seen && typeof data.oversight === 'boolean') {
            each('[data-oversight]', function (el) {
              el.hidden = (el.getAttribute('data-oversight') === 'on') !== data.oversight;
            });
            seen.value = data.oversight ? '1' : '0';
          }
          if (!data.messages || !data.messages.length) { return; }
          var stick = atBottom();
          data.messages.forEach(function (m) {
            if (m.seq <= after) { return; }
            var li = lineTpl.content.firstElementChild.cloneNode(true);
            if (m.mine) { li.className += ' mine'; }
            li.querySelector('.chat-sender').textContent = m.sender;
            li.querySelector('time').textContent = m.time;
            li.querySelector('.body').textContent = m.body;
            var tag = li.querySelector('.chat-overseen');
            if (m.overseen && tag) { tag.hidden = false; }
            log.appendChild(li);
            after = m.seq;
          });
          var empty = document.getElementById('chat-empty');
          if (empty) { empty.hidden = true; }
          if (stick) { box.scrollTop = box.scrollHeight; }
        })
        .catch(function () { busy = false; });
    };
    window.setInterval(pollChat, 4000);
  }
})();
