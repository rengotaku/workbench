// 着手順 — the page reads and writes through the same API the CLI uses, so
// neither can drift from the other or skip an event.
(function () {
  'use strict';

  var SCOPE_KEY = 'todo-order.scope';
  var POLL_MS = 4000;
  var state = {
    scope: '7',
    showOut: false,
    board: null,
    open: {},
    busy: false, // a drag or a date field is in progress; do not redraw under it
  };

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

  // The page is not the only writer: the CLI (and whatever the assistant runs
  // through it) changes the same list. Polling the revision is cheap and keeps
  // the page from showing a list that has moved on.
  function watch() {
    setInterval(function () {
      if (state.busy || !state.board || document.hidden) return;
      request('GET', '/api/revision')
        .then(function (r) {
          if (r.revision !== state.board.revision) {
            load().then(function () { flash('外で変更があったので読み直した'); });
          }
        })
        .catch(function () { /* a blip: the next tick tries again */ });
    }, POLL_MS);

    // Coming back to the window is the moment a stale list is most visible.
    document.addEventListener('visibilitychange', function () {
      if (!document.hidden && !state.busy) load();
    });
  }

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
    el.err.classList.remove('notice');
    el.err.textContent = err.message || String(err);
    el.err.hidden = false;
  }

  // flash says what just happened without looking like a failure.
  function flash(text) {
    el.err.classList.add('notice');
    el.err.textContent = text;
    el.err.hidden = false;
    window.setTimeout(function () {
      if (el.err.classList.contains('notice')) el.err.hidden = true;
    }, 4000);
  }

  // ---- rendering ----

  function render() {
    var b = state.board;
    if (!b) return;

    el.over.textContent = b.counts.over;
    el.near.textContent = b.counts.near;
    el.rest.textContent = b.counts.rest;

    // The colour is the whole legend, so each swatch says what it means and
    // what the current scope is — the meaning of amber moves with the scope.
    var within = b.scope_days > 365 ? 'いつでも' : b.scope_days + '日以内';
    setTitle('.count.over', '赤: 期限を過ぎている（今日を含む）— ' + b.counts.over + '件');
    setTitle('.count.near', '黄: ' + within + 'に期限がある（いまのスコープ）— ' + b.counts.near + '件');
    setTitle('.count.rest', '灰: 期限なし、または ' + within + 'より先 — ' + b.counts.rest + '件。'
      + '期限が先のものは末尾に畳まれ、期限なしは常に出る');

    var queue = b.queue || [];
    el.queue.textContent = '';
    queue.forEach(function (t) {
      var inScope = t.in_scope;
      if (!inScope && !state.showOut) return;
      el.queue.appendChild(queueRow(t, inScope));
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

  function setTitle(sel, text) {
    var node = document.querySelector(sel);
    if (node) node.title = text;
  }

  // dueTitle explains the row's colour in words. The stripe on the left edge
  // is a border and cannot be hovered on its own, so the chip — which carries
  // the same colour — is where the explanation lives.
  function dueTitle(t) {
    var scope = Number(state.scope) || 7;
    var within = state.scope === 'all' ? 'いつでも' : scope + '日以内';
    switch (t.due_class) {
      case 'over':
        return '赤: 期限 ' + t.due + ' を過ぎている（' + t.due_label + '）\nクリックで変更';
      case 'near':
        return '黄: 期限 ' + t.due + ' が ' + within + '（' + t.due_label + '）\nクリックで変更';
      case 'plain':
        return '灰: 期限 ' + t.due + ' は ' + within + 'より先。行は末尾に畳まれる\nクリックで変更';
      default:
        return '灰: 期限なし。スコープでは畳まれず常に出る\nクリックで設定';
    }
  }

  // row builds the shared 5-column shape: every section lines up on the same
  // left baseline, so the grip column is present even where it is unused.
  function row(opts) {
    var li = document.createElement('li');
    li.className = 'task' + (opts.extraClass ? ' ' + opts.extraClass : '');
    li.dataset.id = opts.task.id;

    // The row itself opens the detail — there is no separate control. Rows
    // that have a panel are focusable so the keyboard can reach them.
    if (opts.hasPanel) {
      li.classList.add('openable');
      li.tabIndex = 0;
      li.setAttribute('aria-expanded', String(!!state.open[opts.task.id]));
    }

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
    check.setAttribute('aria-label', opts.checked ? '完了を取り消す' : '完了にする');
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
      due.title = opts.dueTitle || '期限を設定する';
      due.dataset.due = opts.task.due || '';
    } else {
      due = document.createElement('span');
      due.className = 'due';
      if (opts.dueTitle) due.title = opts.dueTitle;
    }
    if (opts.dueClass) due.classList.add(opts.dueClass);
    due.textContent = opts.dueLabel;
    li.appendChild(due);

    return li;
  }

  function queueRow(t, inScope) {
    var cls = t.overdue ? 'overdue' : (inScope ? 'in-window' : 'out');
    var li = row({
      task: t,
      draggable: true,
      // The position is not shown. The order is what the list is for, and the
      // row above/below already says it; a number only adds a value that goes
      // stale the moment anything moves. It is still kept server-side, and
      // `todo order` uses it.
      mark: '',
      dueLabel: t.due_label,
      dueClass: t.due_class === 'plain' || t.due_class === 'none' ? '' : t.due_class,
      hasPanel: true,
      editableDue: true,
      dueTitle: dueTitle(t),
      extraClass: cls,
    });
    li.draggable = true;
    return li;
  }

  function waitRow(t) {
    var days = t.stalled_for || 0;
    var level = days >= 10 ? '赤: 10日以上' : (days >= 5 ? '黄: 5日以上' : '灰: 5日未満');
    return row({
      task: t,
      mark: '◷',
      markClass: 'mark-wait',
      dueLabel: days + '日',
      dueClass: days >= 10 ? 'over' : (days >= 5 ? 'near' : ''),
      dueTitle: level + '経過\n' + (t.handed_on || '?') + ' に ' + (t.handed_to || '?')
        + ' へ渡してから ' + days + '日。14日で理由を聞かれる',
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
      mark: '', // the strike-through and the ticked box already say it is done
      markClass: 'mark-done',
      checked: true,
      dueLabel: label,
      dueTitle: label + ' に完了。直近の月曜以降の分だけ残る\nチェックを外すと着手順の末尾へ戻る',
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

    panel.appendChild(removeControl(t));
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

  // removeControl lives inside the opened detail, not on the row: deleting is
  // the one thing that cannot be undone, so it takes opening the task and then
  // a second click. Anything reachable in one click from the list would be too
  // easy to hit by accident.
  function removeControl(t) {
    var wrap = document.createElement('div');
    wrap.className = 'panel-actions';

    var ask = document.createElement('button');
    ask.type = 'button';
    ask.className = 'danger';
    ask.textContent = '削除';
    ask.title = 'このタスクを取り除く（戻せない）';

    ask.addEventListener('click', function () {
      wrap.textContent = '';

      var label = document.createElement('span');
      label.className = 'confirm-label';
      label.textContent = '戻せません。削除しますか';

      var yes = document.createElement('button');
      yes.type = 'button';
      yes.className = 'danger strong';
      yes.textContent = '削除する';
      yes.addEventListener('click', function () {
        yes.disabled = true;
        request('DELETE', '/api/tasks/' + encodeURIComponent(t.id))
          .then(function () {
            delete state.open[t.id];
            return load().then(function () { flash('削除した: ' + t.name); });
          })
          .catch(function (err) { yes.disabled = false; showError(err); });
      });

      var no = document.createElement('button');
      no.type = 'button';
      no.textContent = 'やめる';
      no.addEventListener('click', function () { render(); });

      wrap.appendChild(label);
      wrap.appendChild(yes);
      wrap.appendChild(no);
      yes.focus();
    });

    wrap.appendChild(ask);
    return wrap;
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
    if (ev.target === el.more) {
      state.showOut = !state.showOut;
      render();
      return;
    }

    // Anything else inside an openable row toggles its detail. The checkbox,
    // the due chip and the panel itself are excluded: those are their own
    // actions, and a click inside an open panel (selecting text) must not
    // close it.
    var row = ev.target.closest('.task.openable');
    if (!row) return;
    if (ev.target.closest('.check') || ev.target.closest('.due-input') ||
        ev.target.closest('.panel')) {
      return;
    }
    toggleRow(row);
  });

  function toggleRow(row) {
    var id = row.dataset.id;
    state.open[id] = !state.open[id];
    render();
  }

  // Enter / Space open a row from the keyboard.
  document.addEventListener('keydown', function (ev) {
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    var row = ev.target.closest && ev.target.closest('.task.openable');
    if (!row || row !== ev.target) return;
    ev.preventDefault();
    toggleRow(row);
  });

  document.addEventListener('change', function (ev) {
    var box = ev.target;
    if (!box.classList || !box.classList.contains('check')) return;
    var li = box.closest('.task');
    var was = box.checked;
    // Unticking a completed row puts it back: a mis-click has a way out.
    var path = was ? '/done' : '/reopen';
    box.disabled = true;
    request('POST', '/api/tasks/' + encodeURIComponent(li.dataset.id) + path)
      .then(load)
      .catch(function (err) {
        box.checked = !was;
        box.disabled = false;
        showError(err);
      });
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
      state.busy = false;
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

    state.busy = true;
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
    state.busy = true;
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

    // The revision the positions were read at travels with the move. If the
    // list changed in between, the server refuses instead of dropping the row
    // somewhere else, and the page re-reads.
    request('POST', '/api/tasks/' + encodeURIComponent(li.dataset.id) + '/order', {
      to: target,
      scope_days: Number(state.scope) || 7,
      revision: state.board.revision,
    })
      .then(function () { state.busy = false; return load(); })
      .catch(function (err) {
        state.busy = false;
        showError(err);
        load();
      });
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

  load().then(watch);
})();
