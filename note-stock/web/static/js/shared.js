// Pieces the pages share: the API call and the IME guard.
(function () {
  'use strict';

  // A key pressed while the IME is converting belongs to the IME, not to us.
  // In Japanese input, Enter confirms the conversion and Escape cancels it; if
  // we act on those, the first Enter saves a half-converted memo. `isComposing`
  // covers current browsers, keyCode 229 covers the ones that omit it.
  function composing(e) {
    return e.isComposing === true || e.keyCode === 229;
  }

  function request(method, path, body) {
    var opts = { method: method, headers: {} };
    if (body) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(data.error || ('サーバが ' + res.status + ' を返しました'));
        return data;
      });
    });
  }

  // fetch rejects with a TypeError when the server cannot be reached at all.
  function unreachable(err) {
    return err instanceof TypeError;
  }

  window.noteShared = { composing: composing, request: request, unreachable: unreachable };
})();
