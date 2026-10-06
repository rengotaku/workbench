// The add screen: one field. Enter saves the memo and empties the field for
// the next one. A topic chip, when picked, stays picked between saves.
(function () {
  'use strict';

  var shared = window.noteShared;
  var el = {
    memo: document.getElementById('memo'),
    topics: document.getElementById('topics'),
    addTopic: document.getElementById('topic-add'),
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
    t.textContent = '#' + n.id + '  ' + n.text + (n.topic ? '  [' + n.topic + ']' : '');
    var undo = document.createElement('button');
    undo.type = 'button';
    undo.className = 'linklike';
    undo.textContent = '元に戻す';
    undo.addEventListener('click', function () {
      shared.request('DELETE', '/api/notes/' + n.id)
        .then(function () {
          li.classList.add('is-undone');
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
    var body = { text: text };
    if (selected) body.topic = selected;

    busy = true;
    el.memo.disabled = true;
    shared.request('POST', '/api/notes', body)
      .then(function (n) {
        remember(n);
        show('✓ 保存しました（補足待ち）', false);
        el.memo.value = '';
      })
      .catch(function (err) {
        // Keep what was typed so nothing is lost.
        show(failure('保存できませんでした', err, 'もう一度 Enter を押してください'), true);
      })
      .then(function () {
        busy = false;
        el.memo.disabled = false;
        el.memo.focus();
      });
  }

  el.memo.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    if (e.key === 'Enter') { e.preventDefault(); save(); }
  });

  document.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    if (e.key === 'Escape') { location.href = '/timeline'; }
  });

  el.memo.focus();
})();
