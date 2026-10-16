// The timeline: memos by day, newest first, drawn from GET /api/notes/timeline.
// The filter chips change the query (?topic= / ?no_topic=1) through the router,
// so filtering reads the timeline again without reloading the page.
// "＋ メモを追加" and N open the add screen in a dialog over the timeline;
// "他 N" picks a topic from the ones folded out of the chips; "話題を整理" opens
// the topic dialog (rename / merge / delete). Either dialog reads the timeline
// again after it closes if something changed.
(function () {
  'use strict';

  var MARKUP = [
    '<div class="page">',
    '  <div class="frame">',
    '  <header class="hd">',
    '    <h1 class="page-title">タイムライン</h1>',
    '    <span class="hd-acts">',
    '      <button type="button" class="organize" id="organize"><svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2 4h12M2 8h8M2 12h5"/></svg>話題を整理</button>',
    '      <a href="/notes" id="to-add">＋ メモを追加 <kbd>N</kbd></a>',
    '    </span>',
    '  </header>',
    '',
    '  <nav class="filters one" id="filters" aria-label="話題で絞り込む">',
    '    <span>話題:</span>',
    '    <button type="button" class="chip more" id="chip-more" hidden>他 0 ▾</button>',
    '    <span class="push" id="tl-pending">補足待ち 0</span>',
    '  </nav>',
    '',
    '  <div id="tl-days"></div>',
    '  <div class="status is-error" id="tl-error" role="alert" hidden></div>',
    '  </div>',
    '</div>',
    '',
    '<dialog class="organize-dialog" id="organize-dialog" aria-labelledby="organize-title">',
    '  <div class="dlg-hd"><h2 id="organize-title">話題を整理</h2><button type="button" class="x" id="organize-close" aria-label="閉じる">×</button></div>',
    '  <p class="sub">件数はその話題が付いているメモの数。変更はすべて履歴に残る</p>',
    '  <div class="status is-error" id="organize-error" role="alert" hidden></div>',
    '  <ul class="rows" id="organize-rows"></ul>',
    '  <p class="foot">メモが付いている話題を削除すると、そのメモは「話題なし」になる。Esc で閉じる</p>',
    '</dialog>',
  ].join('\n');

  var TIMELINE_PATH = '/notes/timeline';

  // active is true while the timeline is on show: the N listener on document
  // stays for the life of the page and must not act from a hidden screen.
  var active = false;
  var hooks = {};

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  }

  // An external link, opened in a new tab (as the page did).
  function outLink(url, text, cls) {
    var a = el('a', cls, text);
    a.href = url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    return a;
  }

  function mount(root) {
    root.innerHTML = MARKUP;

    var wb = window.workbench;
    var shared = window.noteShared;
    var filters = root.querySelector('#filters');
    var more = root.querySelector('#chip-more');
    var pendingEl = root.querySelector('#tl-pending');
    var daysEl = root.querySelector('#tl-days');
    var errorEl = root.querySelector('#tl-error');

    // ---- drawing ----

    // extra: the topics the server folded into "他 N".
    var extra = [];
    // seq numbers the reads, so a slow reply for an old filter cannot draw
    // over a newer one.
    var seq = 0;

    function noteView(n) {
      var art = el('article', 'ev');
      art.id = 'note-' + n.id;

      var meta = el('div', 'meta');
      var parts = [document.createTextNode(n.time)];
      if (n.source) parts.push(el('span', 'src', n.source));
      if (n.topic) parts.push(el('span', 'topic', n.topic));
      parts.push(el('span', 'nid', '#' + n.id));
      if (n.route) {
        if (n.route.href) {
          var r = el('a', 'route', n.route.label);
          r.href = n.route.href;
          parts.push(r);
        } else {
          parts.push(el('span', 'route', n.route.label));
        }
      }
      parts.forEach(function (p, i) {
        if (i > 0) meta.appendChild(document.createTextNode(' · '));
        meta.appendChild(p);
      });
      art.appendChild(meta);

      var memo = el('p', 'memo');
      n.segments.forEach(function (s) {
        if (s.ref) {
          var a = outLink(s.url, s.text, 'ref');
          a.title = s.url;
          memo.appendChild(a);
        } else if (s.url) {
          memo.appendChild(outLink(s.url, s.text));
        } else {
          memo.appendChild(document.createTextNode(s.text));
        }
      });
      art.appendChild(memo);

      if (n.links.length) {
        var ol = el('ol', 'links');
        ol.setAttribute('aria-label', '関連 URL');
        n.links.forEach(function (l) {
          var li = el('li');
          li.appendChild(el('span', 'ln', '[' + l.n + ']'));
          li.appendChild(document.createTextNode(' '));
          li.appendChild(el('span', 'src', l.source));
          li.appendChild(document.createTextNode(' '));
          li.appendChild(outLink(l.url, l.label || l.url));
          ol.appendChild(li);
        });
        art.appendChild(ol);
      }

      n.annotations.forEach(function (a) {
        var box = el('div', 'add k-' + a.kind);
        box.appendChild(el('div', 'who', a.label + ' · ' + a.author + ' · ' + a.when + (a.source ? ' · ' + a.source : '')));
        box.appendChild(el(a.kind === 'quote' ? 'blockquote' : 'p', 'body', a.body));
        art.appendChild(box);
      });

      if (n.pending) art.appendChild(el('div', 'pending', '補足待ち'));
      return art;
    }

    function draw(data) {
      // Chips: すべて, the shown topics, 話題なし (links the router follows).
      Array.prototype.slice.call(filters.querySelectorAll('a.chip')).forEach(function (c) { c.remove(); });
      data.chips.forEach(function (c) {
        var a = el('a', 'chip' + (c.on ? ' on' : ''), c.label);
        a.href = c.href;
        if (c.topic) a.dataset.topic = c.topic;
        if (c.on) a.setAttribute('aria-current', 'page');
        filters.insertBefore(a, more);
      });
      extra = data.other.slice();
      pendingEl.textContent = '補足待ち ' + data.pending;

      daysEl.textContent = '';
      data.days.forEach(function (d) {
        var sec = el('section');
        sec.appendChild(el('h2', 'day', d.heading));
        d.notes.forEach(function (n) { sec.appendChild(noteView(n)); });
        daysEl.appendChild(sec);
      });
      if (data.days.length === 0) daysEl.appendChild(el('p', 'empty', data.empty));
      refit();
    }

    // Chips that do not fit on the line fold into "他 N" with the ones the
    // server already left out.
    function refit() { return extra.concat(shared.fitChips(filters, more, extra)); }
    shared.watchChips(filters, refit);

    // load reads the timeline for url (a URL object) and draws it. A hash
    // (#note-12, a link from a todo's source memo) scrolls to that memo.
    function load(url) {
      var mine = ++seq;
      return shared.request('GET', '/api/notes/timeline' + url.search)
        .then(function (data) {
          if (mine !== seq) return;
          errorEl.hidden = true;
          draw(data);
          var target = url.hash && root.querySelector(url.hash.replace(/[^#\w-]/g, ''));
          if (target) target.scrollIntoView();
        })
        .catch(function (err) {
          if (mine !== seq) return;
          errorEl.textContent = shared.unreachable(err)
            ? 'タイムラインを読み込めませんでした。サーバに接続できません。サーバが起動しているか確認してから、ページを再読み込みしてください。'
            : 'タイムラインを読み込めませんでした。' + err.message + '。ページを再読み込みしてください。';
          errorEl.hidden = false;
        });
    }

    // reload reads the timeline again with the filter on screen.
    function reload() { return load(new URL(location.href)); }

    // ---- add dialog ----

    function openAdd() {
      wb.openAddDialog('/notes', 'メモを追加', function (changed) {
        if (changed) reload();
      });
    }

    root.querySelector('#to-add').addEventListener('click', function (e) {
      // A modified click (new tab, new window) still opens the page itself.
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
      e.preventDefault();
      openAdd();
    });

    document.addEventListener('keydown', function (e) {
      if (!active) return;
      if (shared.composing(e) || e.metaKey || e.ctrlKey || e.altKey) return;
      if (document.querySelector('dialog[open]')) return;
      var t = e.target;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      if (e.key === 'n' || e.key === 'N') { e.preventDefault(); openAdd(); }
    });

    // ---- "他 N": pick a topic to filter by ----

    more.addEventListener('click', function () {
      var picker = shared.createPicker({
        topics: refit(),
        allowCreate: false,
        label: '絞り込む話題を探す',
        onPick: function (name) {
          picker.el.remove();
          wb.router.navigate(TIMELINE_PATH + '?topic=' + encodeURIComponent(name));
        },
        onClose: function () { picker.el.remove(); refit(); },
      });
      more.hidden = true;
      filters.insertBefore(picker.el, more);
      picker.input.focus();
    });

    // ---- 話題を整理 ----

    var dlg = root.querySelector('#organize-dialog');
    var rowsEl = root.querySelector('#organize-rows');
    var errEl = root.querySelector('#organize-error');
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
          renderTopics();
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

    function renderTopics() {
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

    root.querySelector('#organize').addEventListener('click', function () {
      changed = false;
      showError('');
      rowsEl.textContent = '';
      dlg.showModal();
      shared.request('GET', '/api/topics')
        .then(function (res) { topics = res.details || []; renderTopics(); })
        .catch(function (err) { showError(failure('話題を読み込めませんでした', err)); });
    });
    root.querySelector('#organize-close').addEventListener('click', function () { dlg.close(); });
    // A click on the backdrop (outside the box) closes it too.
    dlg.addEventListener('click', function (e) { if (e.target === dlg) dlg.close(); });
    // A renamed or removed topic changes the chips and the memos' topics.
    dlg.addEventListener('close', function () { if (changed) reload(); });

    // ---- shown by the router ----

    // Every show reads the timeline: the first one, a new filter (the query
    // changed) and a return from another screen, where memos may have been
    // added or picked up meanwhile.
    hooks.show = load;
  }

  window.workbench.router.register('timeline', {
    title: 'note タイムライン',
    mount: function (root) {
      active = true;
      mount(root);
    },
    show: function (url) {
      active = true;
      hooks.show(url);
    },
    hide: function () {
      active = false;
    },
  });
})();
