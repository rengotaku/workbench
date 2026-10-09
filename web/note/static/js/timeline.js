// "＋ メモを追加" and N open the add screen in a dialog over the timeline,
// which reloads after it closes if a memo went in.
(function () {
  'use strict';

  var wb = window.workbench;

  function openAdd() {
    wb.openAddDialog('/notes', 'メモを追加', function (changed) {
      if (changed) location.reload();
    });
  }

  document.getElementById('to-add').addEventListener('click', function (e) {
    // A modified click (new tab, new window) still opens the page itself.
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    openAdd();
  });

  document.addEventListener('keydown', function (e) {
    if (window.noteShared.composing(e) || e.metaKey || e.ctrlKey || e.altKey) return;
    if (document.querySelector('dialog[open]')) return;
    var t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
    if (e.key === 'n' || e.key === 'N') { e.preventDefault(); openAdd(); }
  });
})();
