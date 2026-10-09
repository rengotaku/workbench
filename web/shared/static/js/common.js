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

  // An add screen also runs inside the add dialog (modal.js) as a framed page.
  // There it has no site header, and leaving it closes the dialog instead of
  // navigating the frame.
  var embedded = window.parent !== window;
  if (embedded) document.documentElement.classList.add('embedded');

  // tell sends the dialog a message: 'changed' after something was added or
  // undone, 'close' when the screen is left. Outside a dialog it does nothing.
  function tell(type) {
    if (embedded) window.parent.postMessage({ workbench: type }, location.origin);
  }

  // leave goes to href, or closes the dialog the screen is in.
  function leave(href) {
    if (embedded) { tell('close'); return; }
    location.href = href;
  }

  // A link marked data-leave closes the dialog rather than open in the frame.
  document.addEventListener('click', function (e) {
    var a = e.target.closest && e.target.closest('a[data-leave]');
    if (!a || !embedded) return;
    e.preventDefault();
    tell('close');
  });

  window.workbench = { composing: composing, request: request, embedded: embedded, tell: tell, leave: leave };
})();
