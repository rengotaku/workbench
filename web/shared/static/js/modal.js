// The add dialog: the screen's own add page (/add, /notes) opened in a frame
// over the list, so it is the same form with the same keys. The page says
// when a save is in flight, when it added something and when it is left
// (common.js: tell / leave); the dialog closes on the latter and lets the
// screen refresh once it is gone. A close asked for while a save is in flight
// waits for it: dropping the frame then would lose whether the save went in,
// and the list would not show a memo that did.
(function () {
  'use strict';

  // open shows src in the dialog. onClosed(changed) runs after it closes;
  // changed is true when something was added (or undone) while it was open.
  function openAddDialog(src, label, onClosed) {
    var dialog = document.createElement('dialog');
    dialog.className = 'add-dialog';
    dialog.setAttribute('aria-label', label);

    var close = document.createElement('button');
    close.type = 'button';
    close.className = 'add-dialog-close';
    close.setAttribute('aria-label', '閉じる');
    close.textContent = '×';

    var frame = document.createElement('iframe');
    frame.title = label;
    frame.src = src;
    // Shown once its scripts ran, so the site header it hides never flashes.
    frame.style.visibility = 'hidden';

    var changed = false;
    var busy = false;
    var closeWanted = false;

    function requestClose() {
      if (busy) { closeWanted = true; return; }
      dialog.close();
    }

    function onMessage(e) {
      if (e.origin !== location.origin || e.source !== frame.contentWindow) return;
      var type = e.data && e.data.workbench;
      if (type === 'busy') busy = true;
      if (type === 'changed') changed = true;
      if (type === 'idle') { busy = false; if (closeWanted) dialog.close(); }
      if (type === 'close') requestClose();
    }

    frame.addEventListener('load', function () {
      frame.style.visibility = '';
      // The page focused its field while the frame was hidden, which does not
      // take; focus it again now that it shows.
      frame.focus();
      var field = frame.contentDocument && frame.contentDocument.querySelector('[autofocus]');
      if (field) field.focus();
    });
    close.addEventListener('click', requestClose);
    // A click on the backdrop (outside the box) closes it too.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) requestClose(); });
    // Esc while focus is outside the frame.
    dialog.addEventListener('cancel', function (e) { e.preventDefault(); requestClose(); });
    dialog.addEventListener('close', function () {
      window.removeEventListener('message', onMessage);
      dialog.remove();
      if (onClosed) onClosed(changed);
    });
    window.addEventListener('message', onMessage);

    dialog.appendChild(close);
    dialog.appendChild(frame);
    document.body.appendChild(dialog);
    dialog.showModal();
    return dialog;
  }

  window.workbench.openAddDialog = openAddDialog;
})();
