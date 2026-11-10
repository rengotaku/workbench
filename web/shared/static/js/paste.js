// Pasting into the text fields of both tools. A paste always lands as plain
// text; a link copied with its text (from a tracker, a chat, a document) keeps
// its URL as a related URL: the text goes in followed by [N], and the URL is
// added as related URL N, the way the note screens number them (ADR note-0003).
(function () {
  'use strict';

  function parse(cb) {
    var html = cb.getData('text/html');
    if (!html) return null;
    try { return new DOMParser().parseFromString(html, 'text/html'); } catch (e) { return null; }
  }

  //   is the non-breaking space a copied fragment separates words with.
  function norm(v) { return (v || '').replace(/[\s ]+/g, ' ').trim(); }

  // openable lists the anchors a browser can open (http/https), in order. The
  // URL is kept exactly as copied — the app never inspects it, so a tracker, a
  // wiki or a document are all the same thing to it.
  function openable(doc) {
    return Array.prototype.slice.call(doc.querySelectorAll('a[href]')).filter(function (a) {
      return /^https?:\/\//i.test(a.getAttribute('href') || '');
    });
  }

  // pastedLink reads the first openable link out of what was copied: the text
  // it showed and the URL it pointed at, or null when there is none.
  function pastedLink(cb) {
    var doc = parse(cb);
    if (!doc) return null;
    var a = openable(doc)[0];
    if (!a) return null;
    var href = a.getAttribute('href');
    // The label can sit outside the anchor — a tracker that links the key alone
    // leaves the title beside it — so the text comes from the whole fragment.
    var text = norm(doc.body ? doc.body.textContent : '');
    var label = norm(a.textContent);
    if (!text) { text = label || href; }
    return { url: href, text: text, linkText: label || text };
  }

  // pastedLinks reads every openable link out of what was copied. With one
  // link the text is the whole fragment and its mark goes at the end (see
  // pastedLink); with several, each link's mark goes right after its own text.
  // parts is the text cut at the marks: links[i] belongs after parts[i]. null
  // when nothing copied carries a link.
  function pastedLinks(cb) {
    var doc = parse(cb);
    if (!doc || !doc.body) return null;
    var anchors = openable(doc);
    if (anchors.length === 0) return null;
    if (anchors.length === 1) {
      var one = pastedLink(cb);
      return { parts: [one.text, ''], links: [{ url: one.url, label: one.linkText }] };
    }
    var links = anchors.map(function (a) {
      return { url: a.getAttribute('href'), label: norm(a.textContent) };
    });
    var parts = [''];
    (function walk(node) {
      node.childNodes.forEach(function (c) {
        if (c.nodeType === 3) { parts[parts.length - 1] += c.nodeValue; return; }
        if (c.nodeType !== 1) return;
        if (anchors.indexOf(c) !== -1) {
          parts[parts.length - 1] += c.textContent;
          parts.push('');
          return;
        }
        walk(c);
      });
    })(doc.body);
    parts = parts.map(function (p) { return p.replace(/[\s ]+/g, ' '); });
    parts[0] = parts[0].replace(/^ /, '');
    parts[parts.length - 1] = parts[parts.length - 1].replace(/ $/, '');
    return { parts: parts, links: links };
  }

  // insertText puts text at the caret (replacing a selection) and tells the
  // field's input listeners, as typing would. It goes through the editing
  // command first, so Cmd+Z takes the paste back (and restores what it
  // replaced); setting the value directly would not enter the undo history.
  function insertText(node, text) {
    if (document.activeElement === node && document.execCommand && document.execCommand('insertText', false, text)) return;
    if (node.tagName === 'INPUT' || node.tagName === 'TEXTAREA') {
      var start = node.selectionStart === null ? node.value.length : node.selectionStart;
      var end = node.selectionEnd === null ? node.value.length : node.selectionEnd;
      node.setRangeText(text, start, end, 'end');
      node.dispatchEvent(new Event('input', { bubbles: true }));
      return;
    }
    node.textContent = node.textContent + text; // last resort: keep the paste
    node.dispatchEvent(new Event('input', { bubbles: true }));
  }

  // wireLinkPaste makes a paste into node land as plain text. For each link it
  // carries, addLink({url, label}) adds the related URL and returns its number
  // (0 when it cannot), and the link's mark becomes " [N]". single folds the
  // text onto one line.
  function wireLinkPaste(node, addLink, single) {
    node.addEventListener('paste', function (e) {
      var cb = e.clipboardData || window.clipboardData;
      if (!cb) return;
      var got = pastedLinks(cb);
      var text;
      if (got) {
        text = got.parts[0];
        got.links.forEach(function (l, i) {
          var n = addLink(l);
          text += (n ? ' [' + n + ']' : '') + got.parts[i + 1];
        });
      } else {
        text = cb.getData('text/plain') || '';
      }
      if (!text) return;
      if (single) text = text.replace(/\s+/g, ' ').trim();
      e.preventDefault();
      insertText(node, text);
    });
  }

  // ---- related URLs on screen ----

  // splitLinkInput reads a related-URL row ("URL 一言") into {url, label}.
  function splitLinkInput(v) {
    var m = /^(\S+)(?:[\s　]+([\s\S]*))?$/.exec((v || '').trim());
    return m ? { url: m[1], label: (m[2] || '').trim() } : null;
  }

  // writeLinkedText puts text into el with each [N] that names one of links
  // ({n, url, label}) as a link to its URL, and the rest as plain text nodes
  // (never HTML). cls is the links' class.
  function writeLinkedText(el, text, links, cls) {
    var byN = {};
    (links || []).forEach(function (l) { byN[l.n] = l; });
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var at = 0;
    var m;
    while ((m = re.exec(text)) !== null) {
      var l = byN[Number(m[1])];
      if (!l) continue;
      if (m.index > at) el.appendChild(document.createTextNode(text.slice(at, m.index)));
      var a = document.createElement('a');
      a.className = cls || 'ref';
      a.href = l.url;
      a.textContent = m[0];
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.title = l.label ? l.label + ' — ' + l.url : l.url;
      a.draggable = false; // dragging a row must not drag the link instead
      el.appendChild(a);
      at = m.index + m[0].length;
    }
    if (at < text.length) el.appendChild(document.createTextNode(text.slice(at)));
  }

  window.workbench.paste = {
    pastedLink: pastedLink,
    pastedLinks: pastedLinks,
    insertText: insertText,
    wireLinkPaste: wireLinkPaste,
    splitLinkInput: splitLinkInput,
    writeLinkedText: writeLinkedText,
  };
})();
