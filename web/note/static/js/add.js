// The add screen: one field, plus optional related-URL rows numbered [1], [2]…
// that the memo text can point at. Enter saves the memo and empties the
// fields for the next one. A topic chip, when picked, stays picked between
// saves.
(function () {
  'use strict';

  var shared = window.noteShared;
  var el = {
    memo: document.getElementById('memo'),
    topics: document.getElementById('topics'),
    addTopic: document.getElementById('topic-add'),
    links: document.getElementById('links'),
    addLink: document.getElementById('link-add'),
    status: document.getElementById('status'),
    recent: document.getElementById('recent'),
  };
  var selected = '';
  var busy = false;

  // ---- topic chips (pick one; pick it again to drop it) ----

  function chips() { return Array.prototype.slice.call(el.topics.querySelectorAll('.chip[data-topic]')); }

  // selectionVersion counts selection changes, so a late reply can tell that
  // the person chose something else while it was in flight.
  var selectionVersion = 0;
  function select(name) {
    selectionVersion++;
    selected = name;
    chips().forEach(function (b) { b.setAttribute('aria-pressed', String(b.dataset.topic === name)); });
  }

  function wireChip(b) {
    b.addEventListener('click', function () {
      select(selected === b.dataset.topic ? '' : b.dataset.topic);
      el.memo.focus();
    });
  }
  chips().forEach(wireChip);

  function addChip(name) {
    var existing = chips().filter(function (b) { return b.dataset.topic === name; })[0];
    if (existing) return existing;
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'chip';
    b.dataset.topic = name;
    b.setAttribute('aria-pressed', 'false');
    b.textContent = name;
    wireChip(b);
    el.topics.insertBefore(b, el.addTopic);
    return b;
  }

  // ---- "＋ 話題を追加": type the name in place ----

  el.addTopic.addEventListener('click', function () {
    var input = document.createElement('input');
    input.type = 'text';
    input.className = 'topic-input';
    input.setAttribute('aria-label', '新しい話題の名前');
    input.placeholder = '話題の名前';
    el.addTopic.hidden = true;
    el.topics.insertBefore(input, el.addTopic);
    input.focus();

    // done: the input is gone (saved or cancelled). sending: a POST is in
    // flight, so another Enter must not send it again. A reply that arrives
    // after done is stale: the person has moved on, so it must not touch the
    // current selection.
    var done = false;
    var sending = false;
    function close() {
      if (done) return;
      done = true;
      input.remove();
      el.addTopic.hidden = false;
      el.memo.focus();
    }
    input.addEventListener('keydown', function (e) {
      if (shared.composing(e)) return;
      if (e.key === 'Escape') { e.stopPropagation(); close(); return; }
      if (e.key !== 'Enter') return;
      e.preventDefault();
      if (sending) return;
      var name = input.value.trim();
      if (!name) { close(); return; }
      sending = true;
      var sentVersion = selectionVersion;
      shared.request('POST', '/api/topics', { name: name })
        .then(function () {
          sending = false;
          // The topic exists on the server either way; show its chip, but only
          // select it when the input is still open and nothing else was picked.
          var chip = addChip(name);
          if (done) return;
          if (selectionVersion === sentVersion) select(chip.dataset.topic);
          close();
        })
        .catch(function (err) {
          sending = false;
          if (done) return;
          // A name that already exists is not a failure for the person: pick it.
          if (chips().some(function (b) { return b.dataset.topic === name; })) { select(name); close(); return; }
          show(failure('話題を追加できませんでした', err, 'もう一度 Enter を押してください'), true);
        });
    });
    input.addEventListener('blur', function () { if (!input.value.trim()) close(); });
  });

  // ---- related URLs: one row each, numbered by position ----

  function linkRows() { return Array.prototype.slice.call(el.links.querySelectorAll('.link-row')); }

  // A row's number is its position, so removing one renumbers the rest. The
  // server numbers them the same way, in the order sent.
  function renumber() {
    linkRows().forEach(function (row, i) {
      var n = '[' + (i + 1) + ']';
      row.querySelector('.ln').textContent = n;
      row.querySelector('input').setAttribute('aria-label', '関連 URL ' + n + '（URL と一言）');
      row.querySelector('button').setAttribute('aria-label', '関連 URL ' + n + ' を外す');
    });
  }

  function addLinkRow() {
    var row = document.createElement('div');
    row.className = 'link-row';
    var n = document.createElement('span');
    n.className = 'ln';
    var input = document.createElement('input');
    input.type = 'text';
    input.className = 'link-input';
    input.autocomplete = 'off';
    input.placeholder = 'URL と一言（一言は省略可）';
    input.addEventListener('keydown', function (e) {
      if (shared.composing(e)) return;
      if (e.key === 'Enter') { e.preventDefault(); save(); }
    });
    var remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'linklike';
    remove.textContent = '外す';
    remove.addEventListener('click', function () {
      if (busy) return;
      // Removing row k moves every later row up by one, so a [k] or later in
      // the memo would quietly point at a different URL. Refuse instead.
      var k = linkRows().indexOf(row) + 1;
      var from = referencedFrom(el.memo.value, k);
      if (from) {
        show('本文が ' + from + ' を指しているので [' + k + '] は外せません。本文の番号を直してから外してください。', true);
        return;
      }
      row.remove();
      renumber();
      el.status.hidden = true; // a message about a row number may no longer hold
      el.memo.focus();
    });
    row.appendChild(n);
    row.appendChild(input);
    row.appendChild(remove);
    el.links.appendChild(row);
    renumber();
    input.focus();
  }

  el.addLink.addEventListener('click', function () { if (!busy) addLinkRow(); });

  // The first [N] (N >= k) in text, or "". A [N] inside a URL counts too: that
  // only refuses a removal, it never saves a wrong reference.
  function referencedFrom(text, k) {
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var m;
    while ((m = re.exec(text)) !== null) {
      if (Number(m[1]) >= k) return m[0];
    }
    return '';
  }

  // While a save is in flight the rows must not change: the reply clears
  // them, which would drop an edit made after the request was sent.
  function lockLinks(locked) {
    el.addLink.disabled = locked;
    linkRows().forEach(function (row) {
      row.querySelector('input').disabled = locked;
      row.querySelector('button').disabled = locked;
    });
  }

  // "URL 一言" → {url, label}. The server numbers what is sent in order, so
  // an empty row in the middle would shift every number after it away from
  // what the screen shows. Such a row stops the save instead (returns null).
  function readLinks() {
    var out = [];
    var rows = linkRows();
    for (var i = 0; i < rows.length; i++) {
      var v = rows[i].querySelector('input').value.trim();
      if (v === '') {
        show('関連 URL [' + (i + 1) + '] が空です。URL を入れるか「外す」を押してください。', true);
        rows[i].querySelector('input').focus();
        return null;
      }
      var m = /^(\S+)(?:[\s\u3000]+([\s\S]*))?$/.exec(v);
      out.push({ url: m[1], label: (m[2] || '').trim() });
    }
    return out;
  }

  function clearLinks() { linkRows().forEach(function (row) { row.remove(); }); }

  // ---- status line ----

  function show(parts, isError) {
    el.status.textContent = '';
    el.status.classList.toggle('is-error', !!isError);
    [].concat(parts).forEach(function (p) {
      el.status.appendChild(typeof p === 'string' ? document.createTextNode(p) : p);
    });
    el.status.hidden = false;
  }

  // What happened, then what to do next.
  function failure(what, err, next) {
    if (shared.unreachable(err)) {
      return what + '。サーバに接続できません。note-stock のサーバが起動しているか確認してから、' + next + '。';
    }
    return what + '。' + err.message + '。' + next + '。';
  }

  // ---- save / undo ----

  function remember(n) {
    var li = document.createElement('li');
    var t = document.createElement('span');
    t.className = 'rtext';
    var links = (n.links || []).length;
    t.textContent = '#' + n.id + '  ' + n.text + (n.topic ? '  [' + n.topic + ']' : '') + (links ? '  ＋関連 ' + links : '');
    var undo = document.createElement('button');
    undo.type = 'button';
    undo.className = 'linklike';
    undo.textContent = '元に戻す';
    undo.addEventListener('click', function () {
      shared.request('DELETE', '/api/notes/' + n.id)
        .then(function () {
          li.classList.add('is-undone');
          window.workbench.tell('changed');
          undo.remove();
          show('取り消しました: ' + n.text, false);
          el.memo.focus();
        })
        .catch(function (err) { show(failure('取り消せませんでした', err, 'もう一度「元に戻す」を押してください'), true); });
    });
    li.appendChild(t);
    li.appendChild(undo);
    el.recent.prepend(li);
  }

  function save() {
    var text = el.memo.value.trim();
    if (!text || busy) { el.memo.focus(); return; }
    var links = readLinks();
    if (!links) return;
    var body = { text: text };
    if (selected) body.topic = selected;
    if (links.length) body.links = links;

    busy = true;
    el.memo.disabled = true;
    lockLinks(true);
    shared.request('POST', '/api/notes', body)
      .then(function (n) {
        remember(n);
        window.workbench.tell('changed');
        show('✓ 保存しました（補足待ち）', false);
        el.memo.value = '';
        clearLinks();
      })
      .catch(function (err) {
        // Keep what was typed so nothing is lost.
        show(failure('保存できませんでした', err, 'もう一度 Enter を押してください'), true);
      })
      .then(function () {
        busy = false;
        el.memo.disabled = false;
        lockLinks(false);
        el.memo.focus();
      });
  }

  el.memo.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    if (e.key === 'Enter') { e.preventDefault(); save(); }
  });

  document.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    if (e.key === 'Escape') { window.workbench.leave('/notes/timeline'); }
  });

  el.memo.focus();
})();
