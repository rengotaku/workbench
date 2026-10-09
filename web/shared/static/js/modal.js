// The add dialog: the screen's own add page (/add, /notes) opened in a frame
// over the list, so it is the same form with the same keys. The page says
// when it added something and when it is left (common.js: tell / leave); the
// dialog closes on the latter and lets the screen refresh once it is gone.
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

    function onMessage(e) {
      if (e.origin !== location.origin || e.source !== frame.contentWindow) return;
      var type = e.data && e.data.workbench;
      if (type === 'changed') changed = true;
      if (type === 'close') dialog.close();
    }

    frame.addEventListener('load', function () {
      frame.style.visibility = '';
      frame.focus(); // the page focuses its own field; the frame must hold focus for that to show
    });
    close.addEventListener('click', function () { dialog.close(); });
    // A click on the backdrop (outside the box) closes it too.
    dialog.addEventListener('click', function (e) { if (e.target === dialog) dialog.close(); });
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
