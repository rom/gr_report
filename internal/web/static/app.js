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

  // Tell the user when new reports arrive, without reloading the page and
  // losing what they are typing. The notice is a polite live region.
  var live = document.getElementById('live');
  if (live && window.fetch) {
    var seen = parseInt(live.getAttribute('data-count'), 10) || 0;
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
          document.title = (data.new ? '(' + data.new + ') ' : '') + baseTitle;
          if (data.new > seen) { live.hidden = false; }
          seen = Math.max(seen, data.new);
        })
        .catch(function () { /* offline: try again next time */ });
    };
    window.setInterval(poll, 30000);
  }
})();
