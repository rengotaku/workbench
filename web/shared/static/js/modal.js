// The add dialog: the add screen itself (todo /add, note /notes) shown over
// the screen on show, so it is the same form with the same keys. The router
// lends the screen's root to the dialog and takes it back when the dialog
// closes, so what was typed there stays with the screen. The screen says when
// a save is in flight, when it added something and when it is left
// (common.js: tell / leave); the dialog closes on the latter and lets the
// screen below refresh once it is gone. A close asked for while a save is in
// flight waits for it, so the screen below does not miss a save that went in.
(function () {
  'use strict';

  var open = null; // the dialog on show: { message(type) }

  // openAddDialog shows the screen for path in the dialog. onClosed(changed)
  // runs after it closes; changed is true when something was added (or undone)
  // while it was open.
  function openAddDialog(path, label, onClosed) {
    var router = window.workbench.router;
    var dialog = document.createElement('dialog');
    dialog.className = 'add-dialog';
    dialog.setAttribute('aria-label', label);

    var close = document.createElement('button');
    close.type = 'button';
    close.className = 'add-dialog-close';
    close.setAttribute('aria-label', '閉じる');
    close.textContent = '×';
    dialog.appendChild(close);
    document.body.appendChild(dialog);
    dialog.showModal();

    var release = router.lend(router.viewFor(path), dialog, path);
    if (!release) {
      // The screen cannot be shown here: open it as a page instead.
      dialog.close();
      dialog.remove();
      router.navigate(path);
      return null;
    }

    var changed = false;
    // Saves in flight. An undo and a save can overlap, so this counts: the
    // dialog closes only once every one of them has answered.
    var pending = 0;
    var closeWanted = false;
    var done = false;

    // finish gives the screen back at once, before anything else runs: the
    // router closes dialogs when it switches screens and may show this very
    // screen next, while the close event only comes later.
    function finish() {
      if (done) return;
      done = true;
      open = null;
      release();
    }

    function requestClose() {
      if (pending > 0) { closeWanted = true; return; }
      dialog.close();
    }

    var nativeClose = dialog.close.bind(dialog);
    dialog.close = function () {
      finish();
      nativeClose();
    };

    open = {
      message: function (type) {
        if (type === 'busy') pending++;
        if (type === 'changed') changed = true;
        if (type === 'idle') {
          pending = Math.max(0, pending - 1);
          if (closeWanted && pending === 0) dialog.close();
        }
        if (type === 'close') requestClose();
      },
    };

    close.addEventListener('click', requestClose);
    // A click on the backdrop (outside the box) closes it too.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) requestClose(); });
    // Esc: the screen's own Esc asks to leave (common.js leave); this catches
    // it when focus is elsewhere in the dialog.
    dialog.addEventListener('cancel', function (e) { e.preventDefault(); requestClose(); });
    dialog.addEventListener('close', function () {
      finish();
      dialog.remove();
      if (onClosed) onClosed(changed);
    });
    return dialog;
  }

  window.workbench.openAddDialog = openAddDialog;
  // The screen in the dialog reports through this (common.js tell / leave).
  window.workbench.addDialog = function () { return open; };
})();
