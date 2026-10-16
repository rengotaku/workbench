// Pieces both todo pages need: the API call and the IME guard (from the shared
// common.js), and how a pasted link keeps its URL. Loaded before the page
// script, which reads them from window.todoShared.
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

  // ---- related URLs ----

  // The first [N] (N >= k) in text, or "". A removal of row k would move every
  // later row up by one, so a [k] or later in the name would quietly point at
  // a different URL; the editor refuses it instead (the note screens do too).
  function referencedFrom(text, k) {
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var m;
    while ((m = re.exec(text)) !== null) {
      if (Number(m[1]) >= k) return m[0];
    }
    return '';
  }

  // linkEditor builds the rows of related URLs a name points at with [N]: one
  // row per URL, numbered by position ("URL 一言" in each), and a button to add
  // one. The server numbers what is sent the same way, in order.
  //   opts.text()        the name the rows belong to (for the removal check)
  //   opts.onError(msg)  a row cannot be removed or is empty
  //   opts.onChange()    a row was added (with a value), edited or removed
  //   opts.onEnter()     Enter in a row
  function linkEditor(opts) {
    var wrap = document.createElement('div');
    wrap.className = 'link-editor';
    var rows = document.createElement('div');
    rows.className = 'link-rows';
    rows.setAttribute('role', 'group');
    rows.setAttribute('aria-label', '関連 URL（任意）');
    var addBtn = document.createElement('button');
    addBtn.type = 'button';
    addBtn.className = 'link-add';
    addBtn.textContent = '＋ 関連 URL';
    wrap.appendChild(rows);
    wrap.appendChild(addBtn);

    function all() { return Array.prototype.slice.call(rows.querySelectorAll('.link-row')); }

    // quiet is true while set() lays out rows the task already has: they are
    // not a change, and saving each one as it lands would send a partial list.
    var quiet = false;
    function changed() { if (!quiet && opts.onChange) opts.onChange(); }

    function renumber() {
      all().forEach(function (row, i) {
        var n = '[' + (i + 1) + ']';
        row.querySelector('.ln').textContent = n;
        row.querySelector('input').setAttribute('aria-label', '関連 URL ' + n + '（URL と一言）');
        row.querySelector('.link-remove').setAttribute('aria-label', '関連 URL ' + n + ' を外す');
        row.querySelector('.link-edit').setAttribute('aria-label', '関連 URL ' + n + ' を直す');
      });
    }

    // add appends a row, prefilled with value when given, and returns its
    // number. It focuses the row unless the caret stays where it is (a paste
    // into the name).
    function add(value, keepFocus) {
      var row = document.createElement('div');
      row.className = 'link-row';
      var n = document.createElement('span');
      n.className = 'ln';
      var input = document.createElement('input');
      input.type = 'text';
      input.className = 'link-input';
      input.autocomplete = 'off';
      input.placeholder = 'URL と一言（一言は省略可）';
      if (value) input.value = value;
      input.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); if (opts.onEnter) opts.onEnter(); }
      });
      input.addEventListener('change', changed);
      var remove = document.createElement('button');
      remove.type = 'button';
      remove.className = 'linklike link-remove';
      remove.textContent = '外す';
      remove.addEventListener('click', function () {
        var k = all().indexOf(row) + 1;
        var from = referencedFrom(opts.text(), k);
        if (from) {
          if (opts.onError) opts.onError('名前が ' + from + ' を指しているので [' + k + '] は外せません。名前の番号を直してから外してください。');
          return;
        }
        row.remove();
        renumber();
        // Always a change to look at: a saved URL emptied before its removal
        // was never saved empty, so the removal is what takes it off.
        changed();
      });
      row.appendChild(n);
      row.appendChild(input);
      row.appendChild(remove);
      // A row with a URL shows as a link; 直す turns it back into the input.
      window.workbench.paste.linkRowView(row, input, remove);
      rows.appendChild(row);
      renumber();
      if (!keepFocus) input.focus();
      if (value) changed();
      return all().length;
    }

    addBtn.addEventListener('click', function () { add('', false); });

    // read returns the rows as [{url, label}], or null when a row is empty: an
    // empty row in the middle would shift every number after it. quiet skips
    // the message (a save on the fly that will run again).
    function read(quiet) {
      var out = [];
      var list = all();
      for (var i = 0; i < list.length; i++) {
        var v = list[i].querySelector('input').value.trim();
        if (v === '') {
          if (!quiet && opts.onError) opts.onError('関連 URL [' + (i + 1) + '] が空です。URL を入れるか「外す」を押してください。');
          return null;
        }
        var m = /^(\S+)(?:[\s\u3000]+([\s\S]*))?$/.exec(v);
        out.push({ url: m[1], label: (m[2] || '').trim() });
      }
      return out;
    }

    // set replaces the rows with links ([{url, label}]).
    function set(links) {
      quiet = true;
      rows.textContent = '';
      (links || []).forEach(function (l) { add(l.url + (l.label ? ' ' + l.label : ''), true); });
      quiet = false;
    }

    function lock(locked) {
      addBtn.disabled = locked;
      all().forEach(function (row) {
        row.querySelector('input').disabled = locked;
        row.querySelector('.link-edit').disabled = locked;
        row.querySelector('.link-remove').disabled = locked;
      });
    }

    return { el: wrap, add: add, read: read, set: set, lock: lock };
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
    linkEditor: linkEditor,
    writeLinkedText: writeLinkedText,
  };
})();
