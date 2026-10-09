// N opens the add screen, the same key the header shows.
(function () {
  'use strict';
  document.addEventListener('keydown', function (e) {
    if (window.noteShared.composing(e) || e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
    if (e.key === 'n' || e.key === 'N') { e.preventDefault(); location.href = '/notes'; }
  });
})();
