// Pieces both todo pages need: the API call and the IME guard (from the shared
// common.js), and how a pasted link keeps its URL. The related-URL rows are
// links.js. Loaded before the page script, which reads them from
// window.todoShared.
(function () {
  'use strict';

  // The IME guard and the API call are shared with the note screens
  // (/shared/js/common.js, loaded first).
  var composing = window.workbench.composing;
  var request = window.workbench.request;

  // Reading a pasted link and inserting plain text are shared with the note
  // screens (/shared/js/paste.js, loaded first).
  var pastedLink = window.workbench.paste.pastedLink;
  var insertText = window.workbench.paste.insertText;

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

  // writeLinkedText puts text into el with each [N] that names one of links as
  // a link to its URL (/shared/js/paste.js), styled as the name's links.
  function writeLinkedText(el, text, links) {
    window.workbench.paste.writeLinkedText(el, text, links, 'ticket ref');
  }

  window.todoShared = {
    composing: composing,
    request: request,
    pastedLink: pastedLink,
    insertText: insertText,
    wirePaste: wirePaste,
    writeLinkedText: writeLinkedText,
  };
})();
