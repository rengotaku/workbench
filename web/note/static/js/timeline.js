// "＋ メモを追加" and N open the add screen in a dialog over the timeline,
// which reloads after it closes if a memo went in. "他 N" picks a topic from
// the ones folded out of the filter chips. "話題を整理" opens the topic dialog
// (rename / merge / delete), which reloads the page after it closes if a topic
// changed.
(function () {
  'use strict';

  var wb = window.workbench;
  var shared = window.noteShared;

  function openAdd() {
    wb.openAddDialog('/notes', 'メモを追加', function (changed) {
      if (changed) location.reload();
    });
  }

  document.getElementById('to-add').addEventListener('click', function (e) {
    // A modified click (new tab, new window) still opens the page itself.
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    openAdd();
  });

  document.addEventListener('keydown', function (e) {
    if (shared.composing(e) || e.metaKey || e.ctrlKey || e.altKey) return;
    if (document.querySelector('dialog[open]')) return;
    var t = e.target;
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
    if (e.key === 'n' || e.key === 'N') { e.preventDefault(); openAdd(); }
  });

  // ---- "他 N": pick a topic to filter by ----

  var filters = document.getElementById('filters');
  var more = document.getElementById('chip-more');
  var extra = JSON.parse(filters.dataset.others || '[]');
  // Chips that do not fit on the line fold into "他 N" with the ones the
  // server already left out.
  function refit() { return extra.concat(shared.fitChips(filters, more, extra)); }
  shared.watchChips(filters, refit);
  more.addEventListener('click', function () {
    var picker = shared.createPicker({
      topics: refit(),
      allowCreate: false,
      label: '絞り込む話題を探す',
      onPick: function (name) { location.href = '/notes/timeline?topic=' + encodeURIComponent(name); },
      onClose: function () { picker.el.remove(); refit(); },
    });
    more.hidden = true;
    filters.insertBefore(picker.el, more);
    picker.input.focus();
  });

  // ---- 話題を整理 ----

  var dlg = document.getElementById('organize-dialog');
  var rowsEl = document.getElementById('organize-rows');
  var errEl = document.getElementById('organize-error');
  var changed = false;
  var topics = []; // [{name, count}] from GET /api/topics
  var busy = false;

  function showError(msg) {
    errEl.textContent = msg;
    errEl.hidden = msg === '';
  }

  // What happened, then what to do next.
  function failure(what, err) {
    if (shared.unreachable(err)) return what + '。サーバに接続できません。note-stock のサーバが起動しているか確認してから、もう一度押してください。';
    return what + '。' + err.message + '。内容を確認して、もう一度押してください。';
  }

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }
  function button(label, cls, onClick) {
    var b = el('button', cls, label);
    b.type = 'button';
    b.addEventListener('click', onClick);
    return b;
  }

  // call runs a write. On success the dialog shows the topics the server
  // answered with; a failure stays in the edit area it came from.
  function call(method, name, body, onError) {
    if (busy) return;
    busy = true;
    showError('');
    shared.request(method, '/api/topics/' + encodeURIComponent(name), body)
      .then(function (res) {
        changed = true;
        topics = res.details || [];
        render();
      })
      .catch(function (err) { onError(err); })
      .then(function () { busy = false; });
  }

  function closeEdits() {
    rowsEl.querySelectorAll('.edit').forEach(function (e) { e.remove(); });
  }

  function edit(li, build) {
    closeEdits();
    var box = el('div', 'edit');
    var warn = el('span', 'warn');
    build(box, warn);
    box.appendChild(button('やめる', 'btn plain', function () { box.remove(); }));
    if (!box.classList.contains('del')) box.appendChild(warn);
    li.appendChild(box);
    var focus = box.querySelector('input, select');
    if (focus) focus.focus();
  }

  function renameEdit(li, t) {
    edit(li, function (box, warn) {
      var input = el('input');
      input.type = 'text';
      input.value = t.name;
      input.setAttribute('aria-label', '新しい名前');
      function explain() {
        warn.className = 'warn muted';
        var v = input.value.trim();
        var other = topics.filter(function (o) { return o.name === v && o.name !== t.name; })[0];
        warn.textContent = other
          ? '「' + v + '」は既にある話題なので統合になる。「' + t.name + '」の ' + t.count + ' 件を「' + v + '」に付け替え、「' + t.name + '」を一覧から消す'
          : t.count + ' 件のメモも新しい名前になる。既にある話題名を入れると統合として扱う';
      }
      function submit() {
        var v = input.value.trim();
        if (v === '' || v === t.name) { warn.className = 'warn'; warn.textContent = '今と違う名前を入れてください'; return; }
        call('PUT', t.name, { name: v }, function (err) { warn.className = 'warn'; warn.textContent = failure('名前を変えられませんでした', err); });
      }
      input.addEventListener('input', explain);
      input.addEventListener('keydown', function (e) {
        if (shared.composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); submit(); }
      });
      box.appendChild(el('span', '', '新しい名前'));
      box.appendChild(input);
      box.appendChild(button('変える', 'btn', submit));
      explain();
    });
  }

  function mergeEdit(li, t) {
    edit(li, function (box, warn) {
      var others = topics.filter(function (o) { return o.name !== t.name; });
      box.appendChild(el('span', '', '統合先'));
      if (others.length === 0) { warn.textContent = '統合先になる話題がありません'; return; }
      var select = el('select');
      select.setAttribute('aria-label', '統合先');
      others.forEach(function (o) { var op = el('option', '', o.name); op.value = o.name; select.appendChild(op); });
      function explain() {
        warn.className = 'warn';
        warn.textContent = '「' + t.name + '」の ' + t.count + ' 件を「' + select.value + '」に付け替え、「' + t.name + '」を一覧から消す';
      }
      select.addEventListener('change', explain);
      box.appendChild(select);
      box.appendChild(button('統合する', 'btn', function () {
        call('PUT', t.name, { merge_into: select.value }, function (err) { warn.textContent = failure('統合できませんでした', err); });
      }));
      explain();
    });
  }

  function deleteEdit(li, t) {
    edit(li, function (box) {
      box.className = 'edit del';
      box.appendChild(el('span', '', t.count > 0
        ? '「' + t.name + '」を一覧から消す。付いている ' + t.count + ' 件のメモは話題なしになる'
        : '「' + t.name + '」を一覧から消す。付いているメモは無い'));
      box.appendChild(button('削除する', 'btn red', function () {
        call('DELETE', t.name, null, function (err) { showError(failure('削除できませんでした', err)); });
      }));
    });
  }

  function render() {
    rowsEl.textContent = '';
    topics.forEach(function (t) {
      var li = el('li', 'row');
      li.appendChild(el('span', 'name', t.name));
      li.appendChild(el('span', 'n', t.count + ' 件'));
      var acts = el('span', 'acts');
      acts.appendChild(button('名前を変える', 'linklike', function () { renameEdit(li, t); }));
      acts.appendChild(button('統合', 'linklike', function () { mergeEdit(li, t); }));
      acts.appendChild(button('削除', 'linklike danger', function () { deleteEdit(li, t); }));
      li.appendChild(acts);
      rowsEl.appendChild(li);
    });
    if (topics.length === 0) rowsEl.appendChild(el('li', 'empty', '話題はまだありません'));
  }

  document.getElementById('organize').addEventListener('click', function () {
    changed = false;
    showError('');
    rowsEl.textContent = '';
    dlg.showModal();
    shared.request('GET', '/api/topics')
      .then(function (res) { topics = res.details || []; render(); })
      .catch(function (err) { showError(failure('話題を読み込めませんでした', err)); });
  });
  document.getElementById('organize-close').addEventListener('click', function () { dlg.close(); });
  // A click on the backdrop (outside the box) closes it too.
  dlg.addEventListener('click', function (e) { if (e.target === dlg) dlg.close(); });
  dlg.addEventListener('close', function () { if (changed) location.reload(); });
})();
