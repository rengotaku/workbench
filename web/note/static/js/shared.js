// Pieces the note pages share: the API call and the IME guard come from the
// common script loaded before this one; the error text here is Japanese.
(function () {
  'use strict';

  function request(method, path, body) {
    return window.workbench.request(method, path, body, function (status) {
      return 'サーバが ' + status + ' を返しました';
    });
  }

  // fetch rejects with a TypeError when the server cannot be reached at all.
  function unreachable(err) {
    return err instanceof TypeError;
  }

  window.noteShared = { composing: window.workbench.composing, request: request, unreachable: unreachable };
})();
