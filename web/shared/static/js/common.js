// Pieces both screens' scripts build on: the IME guard and the API call. Each
// screen's shared.js reads them from window.workbench.
(function () {
  'use strict';

  // A key pressed while the IME is converting belongs to the IME, not to us.
  // In Japanese input, Enter confirms the conversion and Escape cancels it; if
  // we act on those, the first Enter submits a half-converted text.
  // `isComposing` covers current browsers, keyCode 229 covers the ones that
  // omit it.
  function composing(e) {
    return e.isComposing === true || e.keyCode === 229;
  }

  // request calls the JSON API. statusMessage turns a failing status into the
  // text of the error; a screen that shows it in its own language passes one.
  function request(method, path, body, statusMessage) {
    var opts = { method: method, headers: {} };
    // The server refuses a POST/PUT/PATCH that is not JSON, even with nothing
    // to say, so those methods always send a body.
    if (!body && /^(POST|PUT|PATCH)$/i.test(method)) body = {};
    if (body) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) {
          throw new Error(data.error || (statusMessage ? statusMessage(res.status) : 'server returned ' + res.status));
        }
        return data;
      });
    });
  }

  window.workbench = { composing: composing, request: request };
})();
