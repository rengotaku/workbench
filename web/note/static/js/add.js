// The add screen: one field, plus optional related-URL rows numbered [1], [2]…
// that the memo text can point at. Enter saves the memo and empties the
// fields for the next one. A topic chip, when picked, stays picked between
// saves.
(function () {
  'use strict';

  // The markup of the screen. It used to be templates/add.html; the router
  // shows every screen in one page, so the screen draws itself when first shown.
  var MARKUP = [
    '<div class="page">',
    '  <div class="frame">',
    '  <header class="hd">',
    '    <h1 class="page-title">メモを残す</h1>',
    '    <a href="/notes/timeline" data-leave>タイムラインへ <kbd>Esc</kbd></a>',
    '  </header>',
    '',
    '  <label class="sr-only" for="memo">メモ</label>',
    '  <input class="big" id="memo" type="text" autofocus autocomplete="off" placeholder="リンクと一言を貼って Enter">',
    '',
    '  <div class="link-rows" id="links" role="group" aria-label="関連 URL（任意）"></div>',
    '  <button type="button" class="chip dashed link-add" id="link-add">＋ 関連 URL</button>',
    '',
    '  <div class="filters one" id="topics" role="group" aria-label="話題（任意）">',
    '    <span>話題（任意）:</span>',
    '    <button type="button" class="chip more" id="topic-more" hidden>他 0 ▾</button>',
    '    <button type="button" class="chip dashed" id="topic-add">＋ 話題を追加</button>',
    '  </div>',
    '',
    '  <div class="hint">',
    '    <span>リンクと一言をそのまま貼る。関連 URL は本文で [1] [2] と書いて指せる。補足は後で AI が付ける</span>',
    '    <span><kbd>Enter</kbd> で保存</span>',
    '  </div>',
    '',
    '  <div class="status is-error" id="status" role="alert" hidden></div>',
    '',
    '  <div class="sec"><b id="pending-title">補足待ち 0 件</b><span>AI が拾うと一覧から消える</span></div>',
    '  <ul class="recent" id="recent" aria-labelledby="pending-title"></ul>',
    '  </div>',
    '</div>',
  ].join('\n');

  // How many topic chips the row shows before folding the rest into "他 N"
  // (visibleTopics in internal/notepages).
  var VISIBLE_TOPICS = 6;

  // byRecent orders topic details by their newest memo, newest first; topics
  // with no memo keep their order, after the rest (note.RankByRecent).
  function byRecent(details) {
    return details
      .map(function (d, i) { return { name: d.name, at: d.last_used_at ? Date.parse(d.last_used_at) : null, i: i }; })
      .sort(function (a, b) {
        if (a.at === null && b.at === null) return a.i - b.i;
        if (a.at === null) return 1;
        if (b.at === null) return -1;
        return b.at - a.at || a.i - b.i;
      });
  }

  // active is true while the screen is on show: the Escape listener on
  // document stays for the life of the page and must not act from a hidden
  // screen.
  var active = false;
  var hooks = {};
  var built = false;

  function mount(root) {
    root.innerHTML = MARKUP;

    var shared = window.noteShared;
    var el = {
      memo: document.getElementById('memo'),
      topics: document.getElementById('topics'),
      addTopic: document.getElementById('topic-add'),
      more: document.getElementById('topic-more'),
      links: document.getElementById('links'),
      addLink: document.getElementById('link-add'),
      status: document.getElementById('status'),
      recent: document.getElementById('recent'),
      pendingTitle: document.getElementById('pending-title'),
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
      refit();
    }

    function wireChip(b) {
      b.addEventListener('click', function () {
        select(selected === b.dataset.topic ? '' : b.dataset.topic);
        el.memo.focus();
      });
    }

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
      el.topics.insertBefore(b, el.more);
      return b;
    }

    // ---- "他 N" and "＋ 話題を追加": a suggesting input in place of the button ----

    // Every topic, so the input can suggest the ones folded into "他 N" too.
    // Filled from /api/topics once the screen is built.
    var allTopics = [];

    // refit hides the chips that do not fit on the line and counts them, with
    // the topics that have no chip, on "他 N". It returns all of those names.
    function refit() {
      var extra = allTopics.filter(function (t) {
        return !chips().some(function (b) { return b.dataset.topic === t; });
      });
      return extra.concat(shared.fitChips(el.topics, el.more, extra));
    }
    // The chips come from the server: the most recently used VISIBLE_TOPICS,
    // the rest folded into "他 N" (the same split the timeline uses,
    // splitTopics in internal/notepages).
    // It runs on every show: a topic renamed, merged or deleted elsewhere must
    // not keep its chip, or saving with it would bring the topic back. The
    // picked topic stays picked while it still exists.
    var watching = false;
    // topicSeq numbers the reads: a reply that is not the newest is dropped, so
    // an old list cannot bring back a topic renamed or removed since. Creating
    // a topic bumps it too, so a list read before the topic existed is dropped.
    var topicSeq = 0;
    function loadTopics() {
      var mine = ++topicSeq;
      return shared.request('GET', '/api/topics')
        .then(function (res) {
          if (mine !== topicSeq) return;
          allTopics = (res.topics || []).slice();
          chips().forEach(function (b) { b.remove(); });
          byRecent(res.details || []).slice(0, VISIBLE_TOPICS).forEach(function (t) { addChip(t.name); });
          if (selected && allTopics.indexOf(selected) !== -1) addChip(selected);
          select(allTopics.indexOf(selected) !== -1 ? selected : '');
          if (!watching) { watching = true; shared.watchChips(el.topics, refit); }
        })
        .catch(function (err) {
          if (mine !== topicSeq) return;
          // The picked topic may have been renamed or removed meanwhile; saving
          // with it would bring it back.
          select('');
          show(failure('話題を読み込めませんでした', err, 'ページを再読み込みしてください'));
        });
    }

    // openPicker shows the input where the button was. A pick of an existing
    // topic selects it; a new name is created on the server first.
    function openPicker(button, suggest, initial) {
      var picker = window.noteShared.createPicker({
        topics: suggest,
        allowCreate: true,
        initial: initial,
        label: '話題を探す・新しい話題の名前',
        onPick: onPick,
        onClose: close,
      });
      button.hidden = true;
      el.topics.insertBefore(picker.el, el.addTopic);
      picker.input.focus();

      // done: the input is gone. A reply that arrives after that is stale: the
      // person has moved on, so it must not touch the current selection.
      var done = false;
      function close() {
        if (done) return;
        done = true;
        picker.el.remove();
        button.hidden = false;
        refit();
        el.memo.focus();
      }
      function onPick(name, isNew) {
        if (!isNew) { select(addChip(name).dataset.topic); close(); return; }
        var sentVersion = selectionVersion;
        shared.request('POST', '/api/topics', { name: name })
          .then(function () {
            topicSeq++; // a list read before this topic existed must not land
            // The topic exists on the server either way; show its chip, but only
            // select it when the input is still open and nothing else was picked.
            if (allTopics.indexOf(name) === -1) allTopics.push(name);
            var chip = addChip(name);
            if (done) return;
            if (selectionVersion === sentVersion) select(chip.dataset.topic);
            close();
          })
          .catch(function (err) {
            if (done) return;
            // A name that already exists is not a failure for the person: pick it.
            if (allTopics.indexOf(name) !== -1) { select(addChip(name).dataset.topic); close(); return; }
            // Reopen with the typed name so it is not lost.
            close();
            show(failure('話題を追加できませんでした', err, 'もう一度名前を入れて Enter を押してください'));
            openPicker(button, suggest, name);
          });
      }
    }

    el.addTopic.addEventListener('click', function () { openPicker(el.addTopic, allTopics, ''); });
    el.more.addEventListener('click', function () { openPicker(el.more, refit(), ''); });

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
          show('本文が ' + from + ' を指しているので [' + k + '] は外せません。本文の番号を直してから外してください。');
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
          show('関連 URL [' + (i + 1) + '] が空です。URL を入れるか「外す」を押してください。');
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

    // The band appears for failures only and stays until the next success.
    function show(parts) {
      el.status.textContent = '';
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

    // ---- the waiting list: memos the AI has not picked up yet ----

    function two(n) { return (n < 10 ? '0' : '') + n; }
    function stamp(iso) {
      var d = new Date(iso);
      return two(d.getMonth() + 1) + '/' + two(d.getDate()) + ' ' + two(d.getHours()) + ':' + two(d.getMinutes());
    }

    // The title counts the rows still waiting; a deleted row stays, struck
    // through, but no longer counts.
    function updateTitle() {
      var n = el.recent.querySelectorAll('li:not(.is-undone)').length;
      el.pendingTitle.textContent = '補足待ち ' + n + ' 件';
    }

    function row(n, isNew) {
      var li = document.createElement('li');
      li.dataset.id = String(n.id);
      if (isNew) li.className = 'new';
      var t = document.createElement('span');
      t.className = 'rtext';
      var when = document.createElement('span');
      when.className = 'when';
      when.textContent = stamp(n.created_at);
      var links = (n.links || []).length;
      t.appendChild(when);
      t.appendChild(document.createTextNode('#' + n.id + '  ' + n.text + (n.topic ? '  [' + n.topic + ']' : '') + (links ? '  ＋関連 ' + links : '')));
      var del = document.createElement('button');
      del.type = 'button';
      del.className = 'linklike danger';
      del.textContent = '削除';
      del.setAttribute('aria-label', 'メモ #' + n.id + ' を削除');
      del.addEventListener('click', function () {
        var told = window.workbench.tell('busy');
        shared.request('DELETE', '/api/notes/' + n.id)
          .then(function () {
            li.classList.add('is-undone');
            li.classList.remove('new');
            window.workbench.tell('changed', told);
            el.status.hidden = true;
            var gone = document.createElement('span');
            gone.className = 'gone';
            gone.textContent = '削除しました';
            del.replaceWith(gone);
            updateTitle();
            el.memo.focus();
          })
          .catch(function (err) { show(failure('削除できませんでした', err, 'もう一度「削除」を押してください')); })
          .then(function () { window.workbench.tell('idle', told); });
      });
      li.appendChild(t);
      li.appendChild(del);
      return li;
    }

    // saved counts the memos saved from this screen. A row remembers the
    // count it was saved at, so a list read before that save cannot drop it.
    var saved = 0;
    function remember(n) {
      var li = row(n, true);
      li.dataset.saved = String(++saved);
      el.recent.prepend(li);
      updateTitle();
    }

    // On a later show (fresh) the list is made to match the server: a memo the
    // AI picked up meanwhile leaves it, so its 削除 cannot remove a handled
    // memo. Rows for memos still waiting stay as they are.
    // pendingSeq numbers the reads: only the newest reply is used, so an old
    // list cannot bring back a memo the AI has picked up since (its 削除 would
    // then remove a handled memo).
    var pendingSeq = 0;
    function loadPending(fresh) {
      var mine = ++pendingSeq;
      var savedAtRead = saved;
      shared.request('GET', '/api/notes?pending=1')
        .then(function (res) {
          if (mine !== pendingSeq) return;
          var notes = res.notes || [];
          if (fresh) {
            var waiting = {};
            notes.forEach(function (n) { waiting[String(n.id)] = true; });
            Array.prototype.slice.call(el.recent.querySelectorAll('li')).forEach(function (li) {
              // A memo saved after the read started is not in it, yet waiting.
              if (Number(li.dataset.saved || 0) > savedAtRead) return;
              if (!waiting[li.dataset.id]) li.remove();
            });
          }
          // A memo saved while this was in flight is already on the list.
          notes.forEach(function (n) {
            if (!el.recent.querySelector('li[data-id="' + n.id + '"]')) el.recent.appendChild(row(n, false));
          });
          updateTitle();
        })
        .catch(function (err) {
          if (mine !== pendingSeq) return;
          show(failure('補足待ちのメモを読み込めませんでした', err, 'ページを再読み込みしてください'));
        });
    }

    // ---- save ----

    // A save waits for the topic read in flight: until it lands, the picked
    // topic may be one renamed or removed while another screen was on show.
    function save() {
      topicsReady.then(saveNow);
    }

    function saveNow() {
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
      var told = window.workbench.tell('busy');
      shared.request('POST', '/api/notes', body)
        .then(function (n) {
          remember(n);
          window.workbench.tell('changed', told);
          el.status.hidden = true; // the band is for failures; a success clears an old one
          el.memo.value = '';
          clearLinks();
        })
        .catch(function (err) {
          // Keep what was typed so nothing is lost.
          show(failure('保存できませんでした', err, 'もう一度 Enter を押してください'));
        })
        .then(function () {
          busy = false;
          el.memo.disabled = false;
          lockLinks(false);
          el.memo.focus();
          window.workbench.tell('idle', told);
        });
    }

    el.memo.addEventListener('keydown', function (e) {
      if (shared.composing(e)) return;
      if (e.key === 'Enter') { e.preventDefault(); save(); }
    });

    document.addEventListener('keydown', function (e) {
      if (!active) return;
      if (shared.composing(e)) return;
      if (e.key === 'Escape') { window.workbench.leave('/notes/timeline'); }
    });

    var topicsReady = loadTopics();
    loadPending();

    hooks.show = function (again) {
      // While another screen was on show, memos may have been saved or picked
      // up and topics renamed or removed: read both again.
      if (again) { loadPending(true); topicsReady = loadTopics(); }
      // autofocus only works once per page; the screen is shown many times.
      // (The attribute stays: the add dialog (modal.js) looks for it.)
      el.memo.focus();
    };
  }

  window.workbench.router.register('note-add', {
    title: 'note',
    mount: function (root) {
      active = true;
      mount(root);
    },
    show: function () {
      active = true;
      if (hooks.show) hooks.show(built);
      built = true;
    },
    hide: function () {
      active = false;
    },
  });
})();
