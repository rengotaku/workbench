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

  // An add screen also shows inside the add dialog (modal.js), which borrows
  // it from the router. dialog() is that dialog while it is open.
  function dialog() {
    return window.workbench.addDialog ? window.workbench.addDialog() : null;
  }

  // tell sends the dialog a message: 'busy' / 'idle' around a request that
  // writes, 'changed' after something was added or undone, 'close' when the
  // screen is left. Outside a dialog it does nothing.
  function tell(type) {
    var d = dialog();
    if (d) d.message(type);
  }

  // leave goes to href, or closes the dialog the screen is in. The router
  // (router.js) shows a registered screen in place and loads anything else.
  function leave(href) {
    if (dialog()) { tell('close'); return; }
    if (window.workbench.router) { window.workbench.router.navigate(href); return; }
    location.href = href;
  }

  // A link marked data-leave (the add screens' way back) closes the dialog
  // when the screen is in one.
  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('a[data-leave]');
    if (!a || !dialog()) return;
    e.preventDefault();
    tell('close');
  });

  window.workbench = { composing: composing, request: request, tell: tell, leave: leave };
})();
