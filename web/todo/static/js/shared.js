// Pieces both pages need: the API call, the IME guard, and how a pasted link
// keeps its URL. Loaded before the page script, which reads them from
// window.todoShared.
(function () {
  'use strict';

  // A key pressed while the IME is converting belongs to the IME, not to us.
  // In Japanese input, Enter confirms the conversion and Escape cancels it; if
  // we act on those, the first Enter submits a half-converted name. `isComposing`
  // covers current browsers, keyCode 229 covers the ones that omit it.
  function composing(e) {
    return e.isComposing === true || e.keyCode === 229;
  }

  function request(method, path, body) {
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
        if (!res.ok) throw new Error(data.error || ('server returned ' + res.status));
        return data;
      });
    });
  }

  // A link pasted into a name keeps two things: the text it showed, and the URL
  // it pointed at. The URL is stored with the task exactly as pasted — the app
  // never inspects it, so a link to a tracker, a wiki or a document are all the
  // same thing to it.
  function pastedLink(cb) {
    var html = cb.getData('text/html');
    if (!html) return null;
    var doc;
    try { doc = new DOMParser().parseFromString(html, 'text/html'); } catch (e) { return null; }
    var a = doc.querySelector('a[href]');
    if (!a) return null;
    var href = a.getAttribute('href') || '';
    if (!/^https?:\/\//i.test(href)) return null; // only links a browser can open
    // The label can sit outside the anchor — a tracker that links the key alone
    // leaves the title beside it — so the text comes from the whole fragment.
    // \u00a0 is the non-breaking space such a paste separates them with.
    var norm = function (v) { return (v || '').replace(/[\s\u00a0]+/g, ' ').trim(); };
    var text = norm(doc.body ? doc.body.textContent : '');
    var label = norm(a.textContent);
    if (!text) { text = label || href; }
    return { url: href, text: text, linkText: label || text };
  }

  function insertText(node, text) {
    if (node.tagName === 'INPUT' || node.tagName === 'TEXTAREA') {
      var start = node.selectionStart === null ? node.value.length : node.selectionStart;
      var end = node.selectionEnd === null ? node.value.length : node.selectionEnd;
      node.setRangeText(text, start, end, 'end');
      node.dispatchEvent(new Event('input', { bubbles: true }));
      return;
    }
    if (document.execCommand && document.execCommand('insertText', false, text)) return;
    node.textContent = node.textContent + text; // last resort: keep the paste
    node.dispatchEvent(new Event('input', { bubbles: true }));
  }

  // wirePaste makes a paste land as plain text — a link pasted into an editable
  // name would otherwise bring its markup with it — and remembers the URL on the
  // node so whoever saves the field can send it along.
  function wirePaste(node, single) {
    node.addEventListener('paste', function (e) {
      var cb = e.clipboardData || window.clipboardData;
      if (!cb) return;
      var link = pastedLink(cb);
      var text = link ? link.text : (cb.getData('text/plain') || '');
      if (!text) return;
      if (single) text = text.replace(/\s+/g, ' ').trim();
      e.preventDefault();
      if (link) {
        node.pastedURL = link.url;
        node.pastedLinkText = link.linkText;
      }
      insertText(node, text);
    });
  }

  window.todoShared = {
    composing: composing,
    request: request,
    pastedLink: pastedLink,
    insertText: insertText,
    wirePaste: wirePaste,
  };
})();
