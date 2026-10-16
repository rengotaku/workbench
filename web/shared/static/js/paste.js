// Pasting into the text fields of both tools. A paste always lands as plain
// text; a link copied with its text (from a tracker, a chat, a document) keeps
// its URL as a related URL: the text goes in followed by [N], and the URL is
// added as related URL N, the way the note screens number them (ADR note-0003).
(function () {
  'use strict';

  // pastedLink reads a link out of what was copied: the text it showed and the
  // URL it pointed at, or null when there is no link a browser can open. The
  // URL is kept exactly as copied — the app never inspects it, so a tracker, a
  // wiki or a document are all the same thing to it.
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
    //   is the non-breaking space such a paste separates them with.
    var norm = function (v) { return (v || '').replace(/[\s ]+/g, ' ').trim(); };
    var text = norm(doc.body ? doc.body.textContent : '');
    var label = norm(a.textContent);
    if (!text) { text = label || href; }
    return { url: href, text: text, linkText: label || text };
  }

  // insertText puts text at the caret (replacing a selection) and tells the
  // field's input listeners, as typing would.
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

  // wireLinkPaste makes a paste into node land as plain text. When it carries
  // a link, addLink({url, label}) adds the related URL and returns its number,
  // and the text goes in as "text [N]". single folds the text onto one line.
  function wireLinkPaste(node, addLink, single) {
    node.addEventListener('paste', function (e) {
      var cb = e.clipboardData || window.clipboardData;
      if (!cb) return;
      var link = pastedLink(cb);
      var text = link ? link.text : (cb.getData('text/plain') || '');
      if (!text) return;
      if (single) text = text.replace(/\s+/g, ' ').trim();
      e.preventDefault();
      if (link) {
        var n = addLink({ url: link.url, label: link.linkText });
        if (n) text += ' [' + n + ']';
      }
      insertText(node, text);
    });
  }

  window.workbench.paste = {
    pastedLink: pastedLink,
    insertText: insertText,
    wireLinkPaste: wireLinkPaste,
  };
})();
