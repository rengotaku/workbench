// 着手順 — the page reads and writes through the same API the CLI uses, so
// neither can drift from the other or skip an event.
(function () {
  'use strict';

  var SCOPE_KEY = 'todo-order.scope';
  var state = { scope: '7', showOut: false, board: null, open: {} };

  // A ticket key at the start of a name becomes a link when a base URL is
  // configured. The app itself knows nothing about any tracker; this is only
  // presentation, and with no base URL the name stays plain text.
  var TICKET_KEY = /\b([A-Z][A-Z0-9]*-\d+)\b/g;
  var ticketBase = (document.querySelector('.wrap') || {}).dataset
    ? document.querySelector('.wrap').dataset.ticketUrlBase || ''
    : '';

  var el = {
    queue: document.getElementById('queue'),
    queueEmpty: document.getElementById('queue-empty'),
    waitSec: document.getElementById('waiting-sec'),
    waitList: document.getElementById('waitlist'),
    doneSec: document.getElementById('done-sec'),
    doneList: document.getElementById('donelist'),
    more: document.getElementById('more'),
    over: document.getElementById('c-over'),
    near: document.getElementById('c-near'),
    rest: document.getElementById('c-rest'),
    untilWrap: document.getElementById('scope-until-wrap'),
    until: document.getElementById('scope-until'),
    err: document.getElementById('err'),
  };

  var scopeButtons = Array.prototype.slice.call(document.querySelectorAll('.scope-set button'));

  // ---- server ----

  function request(method, path, body) {
    var opts = { method: method, headers: {} };
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

  function load() {
    return request('GET', '/api/tasks?scope=' + encodeURIComponent(state.scope))
      .then(function (board) {
        state.board = board;
        render();
        showError(null);
      })
      .catch(showError);
  }

  function showError(err) {
    if (!err) { el.err.hidden = true; return; }
    el.err.textContent = err.message || String(err);
    el.err.hidden = false;
  }

  // ---- rendering ----

  function render() {
    var b = state.board;
    if (!b) return;

    el.over.textContent = b.counts.over;
    el.near.textContent = b.counts.near;
    el.rest.textContent = b.counts.rest;

    var queue = b.queue || [];
    el.queue.textContent = '';
    queue.forEach(function (t, i) {
      var inScope = t.in_scope;
      if (!inScope && !state.showOut) return;
      el.queue.appendChild(queueRow(t, i, inScope));
    });
    el.queueEmpty.hidden = queue.length > 0;

    var out = queue.filter(function (t) { return !t.in_scope; }).length;
    el.more.hidden = out === 0;
    el.more.textContent = (state.showOut ? '− ' : '＋ ') + 'スコープ外 ' + out + '件';
    el.more.setAttribute('aria-expanded', String(state.showOut));

    var waiting = (b.waiting || []).slice().sort(function (a, c) {
      return (c.stalled_for || 0) - (a.stalled_for || 0);
    });
    el.waitSec.hidden = waiting.length === 0;
    el.waitList.textContent = '';
    waiting.forEach(function (t) { el.waitList.appendChild(waitRow(t)); });

    var done = b.done || [];
    el.doneSec.hidden = done.length === 0;
    el.doneList.textContent = '';
    done.forEach(function (t) { el.doneList.appendChild(doneRow(t)); });
  }

  // row builds the shared 5-column shape: every section lines up on the same
  // left baseline, so the grip column is present even where it is unused.
  function row(opts) {
    var li = document.createElement('li');
    li.className = 'task' + (opts.extraClass ? ' ' + opts.extraClass : '');
    li.dataset.id = opts.task.id;

    var grip = document.createElement('span');
    grip.className = 'grip' + (opts.draggable ? '' : ' empty');
    grip.setAttribute('aria-hidden', 'true');
    grip.textContent = opts.draggable ? '⠿' : '';
    li.appendChild(grip);

    var num = document.createElement('span');
    num.className = 'num' + (opts.markClass ? ' ' + opts.markClass : '');
    num.textContent = opts.mark;
    if (opts.markClass) num.setAttribute('aria-hidden', 'true');
    li.appendChild(num);

    var check = document.createElement('input');
    check.type = 'checkbox';
    check.className = 'check';
    check.checked = !!opts.checked;
    check.setAttribute('aria-label', opts.checked ? '完了済み' : '完了にする');
    check.disabled = !!opts.checked;
    li.appendChild(check);

    var body = document.createElement('div');
    body.className = 'body';

    var name = document.createElement('div');
    name.className = 'name';
    var txt = document.createElement('span');
    txt.className = 'txt';
    txt.title = opts.task.name;
    linkify(txt, opts.task.name);
    name.appendChild(txt);

    if (opts.hasPanel) {
      var toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'detail-toggle';
      toggle.setAttribute('aria-label', '詳細');
      var open = !!state.open[opts.task.id];
      toggle.textContent = open ? '−' : '+';
      toggle.setAttribute('aria-expanded', String(open));
      name.appendChild(toggle);
    }
    body.appendChild(name);

    if (opts.hasPanel && state.open[opts.task.id]) {
      body.appendChild(panelFor(opts.task));
    }
    li.appendChild(body);

    // The due chip doubles as the way to set a due date: clicking it swaps in
    // a date field. Keeping it in the chip avoids adding a column to a row
    // that is meant to stay one line.
    var due;
    if (opts.editableDue) {
      due = document.createElement('button');
      due.type = 'button';
      due.className = 'due due-edit';
      due.title = '期限を設定する';
      due.dataset.due = opts.task.due || '';
    } else {
      due = document.createElement('span');
      due.className = 'due';
    }
    if (opts.dueClass) due.classList.add(opts.dueClass);
    due.textContent = opts.dueLabel;
    li.appendChild(due);

    return li;
  }

  function queueRow(t, i, inScope) {
    var cls = t.overdue ? 'overdue' : (inScope ? 'in-window' : 'out');
    var li = row({
      task: t,
      draggable: true,
      mark: String(i + 1),
      dueLabel: t.due_label,
      dueClass: t.due_class === 'plain' || t.due_class === 'none' ? '' : t.due_class,
      hasPanel: true,
      editableDue: true,
      extraClass: cls,
    });
    li.draggable = true;
    return li;
  }

  function waitRow(t) {
    var days = t.stalled_for || 0;
    return row({
      task: t,
      mark: '◷',
      markClass: 'mark-wait',
      dueLabel: days + '日',
      dueClass: days >= 10 ? 'over' : (days >= 5 ? 'near' : ''),
      hasPanel: true,
      extraClass: 'waiting',
    });
  }

  function doneRow(t) {
    var label = '';
    if (t.completed_at) {
      var d = new Date(t.completed_at);
      label = (d.getMonth() + 1) + '/' + d.getDate();
    }
    return row({
      task: t,
      mark: '✓',
      markClass: 'mark-done',
      checked: true,
      dueLabel: label,
      hasPanel: false,
      extraClass: 'done',
    });
  }

  // panelFor renders what I wrote above what happened. The trail is fetched
  // per task, so opening one row does not pull every task's history.
  function panelFor(t) {
    var panel = document.createElement('div');
    panel.className = 'panel';

    if (t.detail) {
      var detail = document.createElement('div');
      detail.className = 'detail';
      linkify(detail, t.detail);
      panel.appendChild(detail);
    }
    if (t.state === 'waiting' && t.handed_to) {
      var handoff = document.createElement('div');
      handoff.className = 'detail';
      handoff.textContent = t.handed_on + ' に ' + t.handed_to + ' へ渡した'
        + (t.wait_note ? ' · ' + t.wait_note : '');
      panel.appendChild(handoff);
    }

    var title = document.createElement('div');
    title.className = 'trail-title';
    title.textContent = '経緯';
    panel.appendChild(title);

    var trail = document.createElement('div');
    trail.className = 'trail';
    trail.dataset.for = t.id;
    trail.textContent = '読み込み中…';
    panel.appendChild(trail);

    request('GET', '/api/tasks/' + encodeURIComponent(t.id) + '/context')
      .then(function (data) { fillTrail(trail, data.events || []); })
      .catch(function (err) { trail.textContent = err.message; });

    return panel;
  }

  // linkify writes text into el, turning ticket keys into links. Text is added
  // as text nodes, never as HTML, so a task name cannot inject markup.
  function linkify(el, text) {
    el.textContent = '';
    if (!ticketBase) {
      el.textContent = text;
      return;
    }
    var last = 0;
    var m;
    TICKET_KEY.lastIndex = 0;
    while ((m = TICKET_KEY.exec(text)) !== null) {
      if (m.index > last) {
        el.appendChild(document.createTextNode(text.slice(last, m.index)));
      }
      var a = document.createElement('a');
      a.className = 'ticket';
      a.href = ticketBase + encodeURIComponent(m[1]);
      a.textContent = m[1];
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.title = m[1] + ' を開く';
      a.draggable = false; // dragging a row must not drag the link instead
      el.appendChild(a);
      last = m.index + m[1].length;
    }
    if (last < text.length) {
      el.appendChild(document.createTextNode(text.slice(last)));
    }
  }

  var KIND_LABEL = {
    created: '追加',
    reordered: '順序変更',
    due_set: '期限',
    waiting: '待ちへ',
    resumed: '復帰',
    done: '完了',
    reopened: '再開',
    detail_edited: '詳細を編集',
  };

  function fillTrail(trail, events) {
    trail.textContent = '';
    if (events.length === 0) {
      trail.textContent = '記録なし';
      return;
    }
    events.slice().reverse().forEach(function (e) {
      var line = document.createElement('div');
      line.className = 'trail-row';

      var at = document.createElement('time');
      var d = new Date(e.at);
      at.dateTime = e.at;
      at.textContent = (d.getMonth() + 1) + '/' + d.getDate();
      line.appendChild(at);

      var what = document.createElement('span');
      what.textContent = (KIND_LABEL[e.kind] || e.kind) + changeOf(e)
        + (e.actor === 'ai' ? '（AI）' : '');
      line.appendChild(what);

      var why = document.createElement('span');
      why.className = 'why';
      if (e.reason) {
        why.textContent = '— ' + e.reason;
      } else if (needsReason(e)) {
        why.textContent = '— 理由なし';
        line.classList.add('unexplained');
      }
      line.appendChild(why);

      trail.appendChild(line);
    });
  }

  function changeOf(e) {
    switch (e.kind) {
      case 'reordered':
        return ' ' + ((e.from_order || 0) + 1) + ' → ' + ((e.to_order || 0) + 1) + ' 番目';
      case 'due_set':
        return ' ' + (e.from_value || 'なし') + ' → ' + (e.to_value || 'なし');
      case 'waiting':
        return ' ' + (e.to_value || '');
      default:
        return '';
    }
  }

  // needsReason mirrors the server's four conditions closely enough to mark a
  // row; the server stays the authority for what lands in `todo ask`.
  function needsReason(e) {
    if (e.reason) return false;
    if (e.kind !== 'reordered') return false;
    return e.to_order === 0 || (e.jumped && e.jumped.length > 0);
  }

  // ---- interaction ----

  document.addEventListener('click', function (ev) {
    if (ev.target.closest('a.ticket')) return; // let the link do its job

    var chip = ev.target.closest('.due-edit');
    if (chip) {
      openDueEditor(chip);
      return;
    }

    var toggle = ev.target.closest('.detail-toggle');
    if (toggle) {
      var li = toggle.closest('.task');
      var id = li.dataset.id;
      state.open[id] = !state.open[id];
      render();
      return;
    }
    if (ev.target === el.more) {
      state.showOut = !state.showOut;
      render();
    }
  });

  document.addEventListener('change', function (ev) {
    var box = ev.target;
    if (!box.classList || !box.classList.contains('check') || !box.checked) return;
    var li = box.closest('.task');
    box.disabled = true;
    request('POST', '/api/tasks/' + encodeURIComponent(li.dataset.id) + '/done')
      .then(load)
      .catch(function (err) { box.checked = false; box.disabled = false; showError(err); });
  });

  // openDueEditor swaps the chip for a date field. Leaving it empty clears the
  // due date, which is why the field is not marked required.
  function openDueEditor(chip) {
    var li = chip.closest('.task');
    var input = document.createElement('input');
    input.type = 'date';
    input.className = 'due-input';
    input.value = chip.dataset.due || '';
    input.setAttribute('aria-label', '期限');

    var done = false;
    function finish(save) {
      if (done) return;
      done = true;
      if (!save || input.value === (chip.dataset.due || '')) {
        load(); // put the row back the way the server has it
        return;
      }
      request('PATCH', '/api/tasks/' + encodeURIComponent(li.dataset.id), { due: input.value })
        .then(load)
        .catch(function (err) { showError(err); load(); });
    }

    input.addEventListener('change', function () { finish(true); });
    input.addEventListener('blur', function () { finish(true); });
    input.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') { finish(false); }
      if (e.key === 'Enter') { finish(true); }
    });

    chip.replaceWith(input);
    input.focus();
    if (input.showPicker) {
      try { input.showPicker(); } catch (e) { /* not allowed without a gesture */ }
    }
  }

  scopeButtons.forEach(function (b) {
    b.addEventListener('click', function () {
      scopeButtons.forEach(function (o) { o.setAttribute('aria-pressed', String(o === b)); });
      var v = b.dataset.scope;
      el.untilWrap.hidden = v !== 'custom';
      if (v === 'custom') {
        state.scope = String(daysUntil(el.until.value));
      } else {
        state.scope = v;
        try { localStorage.setItem(SCOPE_KEY, v); } catch (e) { /* private window */ }
      }
      load();
    });
  });

  el.until.addEventListener('change', function () {
    state.scope = String(daysUntil(el.until.value));
    load();
  });

  function daysUntil(value) {
    if (!value) return 7;
    var target = new Date(value + 'T00:00:00');
    var now = new Date();
    var today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    var days = Math.round((target - today) / 86400000);
    return days < 0 ? 0 : days;
  }

  // ---- drag to reorder ----

  var dragged = null;

  el.queue.addEventListener('dragstart', function (ev) {
    var li = ev.target.closest('.task');
    if (!li) return;
    dragged = li;
    li.classList.add('dragging');
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData('text/plain', li.dataset.id);
  });

  el.queue.addEventListener('dragover', function (ev) {
    if (!dragged) return;
    ev.preventDefault();
    var over = ev.target.closest('.task');
    if (!over || over === dragged) return;
    var box = over.getBoundingClientRect();
    var after = (ev.clientY - box.top) > box.height / 2;
    el.queue.insertBefore(dragged, after ? over.nextSibling : over);
  });

  el.queue.addEventListener('drop', function (ev) { ev.preventDefault(); });

  el.queue.addEventListener('dragend', function () {
    if (!dragged) return;
    var li = dragged;
    li.classList.remove('dragging');
    dragged = null;

    // Position is read off the rows on screen. With "スコープ外" collapsed the
    // hidden rows are not shown, so the whole queue is sent from the board
    // rather than from the DOM.
    var ids = Array.prototype.map.call(el.queue.children, function (n) { return n.dataset.id; });
    var to = ids.indexOf(li.dataset.id);
    var visibleBefore = (state.board.queue || [])
      .filter(function (t) { return state.showOut || t.in_scope; })
      .map(function (t) { return t.id; });
    var target = fullIndexOf(li.dataset.id, to, visibleBefore);

    request('POST', '/api/tasks/' + encodeURIComponent(li.dataset.id) + '/order', {
      to: target,
      scope_days: Number(state.scope) || 7,
    }).then(load).catch(function (err) { showError(err); load(); });
  });

  // fullIndexOf converts a position among visible rows into a position in the
  // whole queue, so collapsing scope-out rows cannot move a task somewhere
  // other than where it was dropped.
  function fullIndexOf(id, visibleIndex, visibleBefore) {
    var all = (state.board.queue || []).map(function (t) { return t.id; });
    if (visibleBefore.length === all.length) return visibleIndex;

    var neighbourId = null;
    if (visibleIndex > 0) {
      var seen = 0;
      for (var i = 0; i < visibleBefore.length; i++) {
        if (visibleBefore[i] === id) continue;
        if (seen === visibleIndex - 1) { neighbourId = visibleBefore[i]; break; }
        seen++;
      }
    }
    if (neighbourId === null) return 0;
    var at = all.indexOf(neighbourId);
    return at < 0 ? visibleIndex : at + 1;
  }

  // ---- boot ----

  try {
    var saved = localStorage.getItem(SCOPE_KEY);
    if (saved) {
      state.scope = saved;
      scopeButtons.forEach(function (b) {
        b.setAttribute('aria-pressed', String(b.dataset.scope === saved));
      });
    }
  } catch (e) { /* storage unavailable; the default scope still applies */ }

  load();
})();
