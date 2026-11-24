// Console behaviour that needs script and nothing else: keyboard shortcuts for the inbox list and logout.
// Every action also works without this file (FR-038): links and forms are plain HTML.
(function () {
  'use strict';
  function rowsNow() { return Array.prototype.slice.call(document.querySelectorAll('[data-kbd-row]')); }
  document.addEventListener('keydown', function (e) {
    var t = e.target && e.target.tagName;
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT' || e.ctrlKey || e.metaKey || e.altKey) return;
    var rows = rowsNow(), i = rows.indexOf(document.activeElement);
    if (e.key === 'j' && rows.length) { rows[Math.min(i + 1, rows.length - 1)].focus(); e.preventDefault(); }
    if (e.key === 'k' && rows.length) { rows[Math.max(i - 1, 0)].focus(); e.preventDefault(); }
    if (e.key === 'g') { location.assign('/'); }
    if (e.key === '/') { var f = document.getElementById('f-state'); if (f) { f.focus(); e.preventDefault(); } }
  });
  var lo = document.querySelector('.js-logout');
  if (lo) lo.addEventListener('submit', function (e) {
    e.preventDefault();
    fetch('/logout', { method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': lo.querySelector('[name=_csrf]').value } })
      .then(function () { location.assign('/login'); });
  });
})();
