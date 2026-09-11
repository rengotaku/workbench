// 着手順 — the page reads and writes through the same API the CLI uses, so
// neither can drift from the other or skip an event.
(function () {
  'use strict';

  var SCOPE_KEY = 'todo-order.scope';
  var DONE_KEY = 'todo-order.done-open';
  var POLL_MS = 4000;
  var state = {
    scope: '7',
    showOut: false,
    board: null,
    openId: null, // only one row is open at a time; another row closes it
    doneOpen: false, // the completed list is a record, not a queue
    busy: false, // a drag or a date field is in progress; do not redraw under it
  };

  // A ticket key at the start of a name becomes a link when a base URL is
  // configured. The app itself knows nothing about any tracker; this is only
  // presentation, and with no base URL the name stays plain text.
  var TICKET_KEY = /\b([A-Z][A-Z0-9]*-\d+)\b/g;
  var ticketBase = (document.querySelector('.wrap') || {}).dataset
    ? document.querySelector('.wrap').dataset.ticketUrlBase || ''
    : '';

  // A key pressed while the IME is converting belongs to the IME, not to us.
  // In Japanese input, Enter confirms the conversion and Escape cancels it; if
  // we act on those, the first Enter submits a half-converted name. `isComposing`
  // covers current browsers, keyCode 229 covers the ones that omit it.
  function composing(e) {
    return e.isComposing === true || e.keyCode === 229;
  }

  var TRASH_SVG = '<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">'
    + '<path d="M2.5 4.5h11M6 4.5V2.8h4v1.7M4.2 4.5l.7 8.7h6.2l.7-8.7M6.6 6.8v4.6M9.4 6.8v4.6"'
    + ' fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>';

  var el = {
    queue: document.getElementById('queue'),
    waitSec: document.getElementById('waiting-sec'),
    waitList: document.getElementById('waitlist'),
    doneSec: document.getElementById('done-sec'),
    doneList: document.getElementById('donelist'),
    more: document.getElementById('more'),
    over: document.getElementById('c-over'),
    near: document.getElementById('c-near'),
    rest: document.getElementById('c-rest'),
    focusBand: document.getElementById('focus-band'),
    focusList: document.getElementById('focus-list'),
    focusCount: document.getElementById('focus-count'),
    addSlot: document.getElementById('add-slot'),
    addTrigger: document.getElementById('add-trigger'),
    doneFold: document.getElementById('done-fold'),
    doneCount: document.getElementById('done-count'),
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
            load().then(function () { flash('外で変更があったので読み直した', false); });
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

  // flash says what just happened without looking like a failure, and offers
  // to take it back. Undo is where a mis-click gets fixed, so it belongs next
  // to the message about the click.
  function flash(text, undoable) {
    el.err.classList.add('notice');
    el.err.textContent = text;
    if (undoable !== false) {
      var link = document.createElement('button');
      link.type = 'button';
      link.className = 'undo-link';
      link.textContent = '元に戻す';
      link.addEventListener('click', undo);
      el.err.appendChild(document.createTextNode('  '));
      el.err.appendChild(link);
    }
    el.err.hidden = false;
    window.setTimeout(function () {
      if (el.err.classList.contains('notice')) el.err.hidden = true;
    }, 6000);
  }

  // undo reverses the last change, whoever made it — the page, the terminal or
  // the assistant. Cmd/Ctrl+Z reaches the same call.
  function undo() {
    request('POST', '/api/undo')
      .then(function (data) {
        var done = data.undone || {};
        state.openId = null;
        return load().then(function () { flash(done.summary || '取り消した', false); });
      })
      .catch(showError);
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

    // The band is hidden when empty: an empty band would only take space.
    var focus = b.focus || [];
    el.focusBand.hidden = focus.length === 0;
    el.focusCount.textContent = focus.length;
    el.focusList.textContent = '';
    focus.forEach(function (t) { el.focusList.appendChild(focusItem(t)); });

    var queue = b.queue || [];
    el.queue.textContent = '';
    queue.forEach(function (t) {
      var inScope = t.in_scope;
      if (!inScope && !state.showOut) return;
      el.queue.appendChild(queueRow(t, inScope));
    });
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
    el.doneCount.textContent = done.length;
    el.doneFold.setAttribute('aria-expanded', String(state.doneOpen));
    el.doneFold.querySelector('.caret').textContent = state.doneOpen ? '▼' : '▶';
    el.doneList.hidden = !state.doneOpen;
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
      li.setAttribute('aria-expanded', String(isOpen(opts.task.id)));
      if (isOpen(opts.task.id)) li.classList.add('row-open');
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

    if (opts.hasPanel && isOpen(opts.task.id)) {
      // An open row is the editable one. The name is the same text in the same
      // place, so there is no second copy of it inside the panel. Ticket keys
      // stop being links while editing: a link inside an editable line fights
      // the caret.
      txt.classList.add('live');
      txt.contentEditable = 'true';
      txt.spellcheck = false;
      txt.textContent = opts.task.name;
      wireLiveField(txt, opts.task, 'name', { single: true });
    } else {
      txt.title = opts.task.name;
      linkify(txt, opts.task.name);
    }
    name.appendChild(txt);

    body.appendChild(name);

    if (opts.hasPanel && isOpen(opts.task.id)) {
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

    // The delete slot sits next to the due chip and only appears on hover, so
    // the row stays quiet while reading. The column is always there (even for
    // rows that cannot be deleted) to keep the right edge from shifting.
    li.appendChild(opts.removable === false ? emptyDelSlot() : removeIcon(opts.task));

    return li;
  }

  function emptyDelSlot() {
    var span = document.createElement('span');
    span.className = 'del-slot';
    span.setAttribute('aria-hidden', 'true');
    return span;
  }

  // removeIcon deletes on one click. There is no confirmation: the delete is
  // recorded as an event with the whole task in it, so the notice that follows
  // offers 元に戻す and `Cmd/Ctrl+Z` works too. Asking first would cost a click
  // on every real deletion to guard against a rare stray one.
  function removeIcon(t) {
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'del del-slot';
    btn.title = '削除する';
    btn.setAttribute('aria-label', t.name + ' を削除する');
    btn.innerHTML = TRASH_SVG;

    btn.addEventListener('click', function (ev) {
      // The row opens on click; deleting must not also open or close it.
      ev.stopPropagation();
      btn.disabled = true;
      request('DELETE', '/api/tasks/' + encodeURIComponent(t.id))
        .then(function () {
          if (state.openId === t.id) state.openId = null;
          return load().then(function () { flash('削除した: ' + t.name); });
        })
        .catch(function (err) { btn.disabled = false; showError(err); });
    });
    return btn;
  }

  // focusItem is one line in the band: finish it, open it, or drop it out.
  function focusItem(t) {
    var li = document.createElement('li');
    li.className = 'focus-item';

    var check = document.createElement('input');
    check.type = 'checkbox';
    check.className = 'check';
    check.setAttribute('aria-label', '完了にする');
    check.addEventListener('change', function () {
      check.disabled = true;
      request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/done')
        .then(function () { return load().then(function () { flash('完了にした: ' + t.name); }); })
        .catch(function (err) { check.checked = false; check.disabled = false; showError(err); });
    });

    var name = document.createElement('span');
    name.className = 'fname';
    name.title = t.name + '（押すと着手順の行が開く）';
    name.textContent = t.name;
    name.addEventListener('click', function () {
      state.openId = t.id;
      render();
      var row = document.querySelector('.task[data-id="' + t.id + '"]');
      if (row) row.scrollIntoView({ block: 'center' });
    });

    var due = document.createElement('span');
    due.className = 'due' + (t.due_class === 'over' || t.due_class === 'near' ? ' ' + t.due_class : '');
    due.textContent = t.due_label;

    var drop = document.createElement('button');
    drop.type = 'button';
    drop.className = 'drop';
    drop.textContent = '✕';
    drop.title = 'フォーカスから外す';
    drop.addEventListener('click', function () {
      drop.disabled = true;
      request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/focus', { on: false })
        .then(function () { return load().then(function () { flash('フォーカスから外した'); }); })
        .catch(function (err) { drop.disabled = false; showError(err); });
    });

    li.appendChild(check);
    li.appendChild(name);
    li.appendChild(due);
    li.appendChild(drop);
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
      // `todo order` uses it. The column carries ◎ when the task is focused.
      mark: t.focused ? '◎' : '',
      markClass: t.focused ? 'mark-focus' : '',
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

    // The detail is edited where it is read: no label, no box, no save button.
    var detail = document.createElement('div');
    detail.className = 'live detail';
    detail.contentEditable = 'true';
    detail.spellcheck = false;
    detail.dataset.placeholder = '詳細を書く（任意）';
    detail.textContent = t.detail || '';
    wireLiveField(detail, t, 'detail', { single: false });
    panel.appendChild(detail);

    var actions = document.createElement('div');
    actions.className = 'panel-actions';

    if (t.state !== 'done') {
      var focusBtn = document.createElement('button');
      focusBtn.type = 'button';
      focusBtn.className = t.focused ? 'focus-on' : '';
      focusBtn.textContent = t.focused ? 'フォーカスを外す' : 'フォーカス';
      focusBtn.title = t.focused ? '上部の一覧から外す' : '上部の一覧に入れる（着手順の位置は動かない）';
      focusBtn.addEventListener('click', function () {
        focusBtn.disabled = true;
        var was = t.focused;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/focus', { on: !was })
          .then(function () {
            return load().then(function () {
              flash(was ? 'フォーカスから外した' : 'フォーカスに入れた');
            });
          })
          .catch(function (err) { focusBtn.disabled = false; showError(err); });
      });
      actions.appendChild(focusBtn);
    }

    var saved = document.createElement('span');
    saved.className = 'save-state';
    saved.dataset.save = '1';
    actions.appendChild(saved);

    var spacer = document.createElement('span');
    spacer.className = 'spacer';
    actions.appendChild(spacer);

    if (t.state === 'waiting') {
      var resume = document.createElement('button');
      resume.type = 'button';
      resume.textContent = '着手順へ戻す';
      resume.title = (t.handed_on || '?') + ' に ' + (t.handed_to || '?') + ' へ渡した分を戻す';
      resume.addEventListener('click', function () {
        resume.disabled = true;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/resume', { note: '' })
          .then(function () { return load().then(function () { flash('着手順へ戻した'); }); })
          .catch(function (err) { resume.disabled = false; showError(err); });
      });
      actions.appendChild(resume);
    } else if (t.state === 'open') {
      actions.appendChild(waitControl(t));
    }

    // The trail is one line: a link whose tooltip holds the whole history.
    // Listing it would push the row open by as many lines as the task has
    // events, and it is read rarely.
    var trailLink = document.createElement('span');
    trailLink.className = 'trail-link';
    trailLink.textContent = '経緯';
    actions.appendChild(trailLink);

    var reasonLink = document.createElement('span');
    reasonLink.className = 'reason-link';
    reasonLink.hidden = true;
    actions.appendChild(reasonLink);

    if (t.state === 'waiting' && t.handed_to) {
      var handoff = document.createElement('span');
      handoff.className = 'handoff-note';
      handoff.textContent = t.handed_on + ' に ' + t.handed_to + ' へ渡した'
        + (t.wait_note ? ' · ' + t.wait_note : '');
      actions.appendChild(handoff);
    }

    panel.appendChild(actions);

    request('GET', '/api/tasks/' + encodeURIComponent(t.id) + '/context')
      .then(function (data) { fillTrail(trailLink, reasonLink, data.events || []); })
      .catch(function (err) {
        trailLink.textContent = '経緯を読めない';
        trailLink.title = err.message;
      });

    return panel;
  }

  function isOpen(id) { return state.openId === id; }

  // wireLiveField makes a piece of text save itself: a pause in typing writes
  // it, and so does leaving the field. There is no save button to forget.
  function wireLiveField(node, t, key, opts) {
    var last = t[key] || '';
    var timer = null;

    function save(reload) {
      var value = node.textContent;
      if (opts.single) value = value.replace(/\s+/g, ' ').trim();
      if (value === last) return;
      if (opts.single && !value) { node.textContent = last; return; } // a name cannot be empty

      var body = {};
      body[key] = value;
      mark(node, '保存中…');
      request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), body)
        .then(function () {
          last = value;
          t[key] = value;
          mark(node, '保存した', true);
          if (reload) load();
        })
        .catch(function (err) { showError(err); node.textContent = last; });
    }

    node.addEventListener('focus', function () { state.busy = true; });
    node.addEventListener('input', function () {
      window.clearTimeout(timer);
      timer = window.setTimeout(function () { save(false); }, 900);
    });
    node.addEventListener('blur', function () {
      window.clearTimeout(timer);
      state.busy = false;
      save(true);
    });
    node.addEventListener('keydown', function (e) {
      if (composing(e)) return;
      if (e.key === 'Enter' && opts.single) { e.preventDefault(); node.blur(); }
      if (e.key === 'Escape') { e.stopPropagation(); node.blur(); }
    });
  }

  // mark shows what happened to the save, next to the task it happened to.
  function mark(node, text, done) {
    var panel = node.closest('.panel') || (node.closest('.task') || document).querySelector('.panel');
    var slot = panel && panel.querySelector('[data-save]');
    if (!slot) return;
    slot.textContent = text;
    slot.className = 'save-state on' + (done ? ' saved' : '');
    if (done) {
      window.setTimeout(function () { slot.className = 'save-state saved'; }, 1600);
    }
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

  function field(label, kind, value) {
    var wrap = document.createElement('div');
    wrap.className = 'field';
    var id = 'f-' + Math.random().toString(36).slice(2, 9);

    var lab = document.createElement('label');
    lab.textContent = label;
    lab.htmlFor = id;

    var input = document.createElement(kind === 'textarea' ? 'textarea' : 'input');
    input.id = id;
    input.value = value;
    if (kind !== 'textarea') input.type = 'text';

    wrap.appendChild(lab);
    wrap.appendChild(input);
    return { wrap: wrap, input: input };
  }

  // waitControl asks who holds the ball before moving a task out of the queue:
  // the elapsed count has no meaning without it.
  function waitControl(t) {
    var span = document.createElement('span');

    var ask = document.createElement('button');
    ask.type = 'button';
    ask.textContent = '待ちにする';
    ask.title = '相手に渡す。経過日数が数え始められる';
    ask.addEventListener('click', function () {
      span.textContent = '';
      var who = document.createElement('input');
      who.type = 'text';
      who.placeholder = '渡した相手';
      who.setAttribute('aria-label', '渡した相手');

      var go = document.createElement('button');
      go.type = 'button';
      go.className = 'primary';
      go.textContent = '渡した';
      function submit() {
        if (!who.value.trim()) { who.focus(); return; }
        go.disabled = true;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/wait',
          { to: who.value.trim(), note: '' })
          .then(function () { return load().then(function () { flash('待ちへ移した'); }); })
          .catch(function (err) { go.disabled = false; showError(err); });
      }
      go.addEventListener('click', submit);
      who.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); submit(); }
        if (e.key === 'Escape') { render(); }
      });

      span.appendChild(who);
      span.appendChild(go);
      who.focus();
    });

    span.appendChild(ask);
    return span;
  }

  var KIND_LABEL = {
    created: '追加',
    renamed: '名前を変更',
    focused: 'フォーカスに入れた',
    unfocused: 'フォーカスから外した',
    reordered: '順序変更',
    due_set: '期限',
    waiting: '待ちへ',
    resumed: '復帰',
    done: '完了',
    reopened: '再開',
    detail_edited: '詳細を編集',
  };

  // fillTrail puts the history into the link's tooltip and, when something is
  // still unexplained, offers to write the reason for the latest one.
  function fillTrail(trailLink, reasonLink, events) {
    if (events.length === 0) {
      trailLink.textContent = '経緯なし';
      return;
    }

    trailLink.textContent = '経緯 ' + events.length + '件';
    trailLink.title = events.slice().reverse().map(function (e) {
      var d = new Date(e.at);
      var when = (d.getMonth() + 1) + '/' + d.getDate() + ' ' +
        ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
      var what = (KIND_LABEL[e.kind] || e.kind) + changeOf(e) + (e.actor === 'ai' ? '（AI）' : '');
      var why = e.reason ? '\n    理由: ' + e.reason : (needsReason(e) ? '\n    理由なし' : '');
      return when + '  ' + what + why;
    }).join('\n');

    var unexplained = events.filter(needsReason);
    if (unexplained.length === 0) return;

    var target = unexplained[unexplained.length - 1];
    reasonLink.hidden = false;
    reasonLink.textContent = '理由を書く' + (unexplained.length > 1 ? '（' + unexplained.length + '件）' : '');
    reasonLink.title = (KIND_LABEL[target.kind] || target.kind) + changeOf(target) + ' の理由';
    reasonLink.addEventListener('click', function () { openReasonEditor(reasonLink, target); });
  }

  // openReasonEditor is the human side of `todo ask`: the question can be
  // answered where it is asked, instead of only from the terminal.
  function openReasonEditor(anchor, e) {
    var input = document.createElement('input');
    input.type = 'text';
    input.placeholder = 'なぜそうしたか';
    input.setAttribute('aria-label', '理由');

    var done = false;
    function finish(save) {
      if (done) return;
      done = true;
      state.busy = false;
      if (!save || !input.value.trim()) { render(); return; }
      request('POST', '/api/events/' + encodeURIComponent(e.id) + '/why',
        { reason: input.value.trim() })
        .then(function () { return load().then(function () { flash('理由を記録した'); }); })
        .catch(function (err) { showError(err); render(); });
    }

    input.addEventListener('keydown', function (ev) {
      if (composing(ev)) return;
      if (ev.key === 'Enter') { ev.preventDefault(); finish(true); }
      if (ev.key === 'Escape') { finish(false); }
    });
    input.addEventListener('blur', function () { finish(true); });

    state.busy = true;
    anchor.replaceWith(input);
    input.focus();
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

    // How opening and editing live together:
    //   closed row, anywhere        → open it (the name is not editable yet)
    //   open row, the name          → put the caret in it; do not close
    //   open row, panel / checkbox  → that action only
    //   open row, anywhere else     → close it
    //   another row                 → that one opens, this one closes
    var row = ev.target.closest('.task.openable');
    if (!row) return;
    if (ev.target.closest('.check') || ev.target.closest('.due-input') ||
        ev.target.closest('.panel')) {
      return;
    }

    var id = row.dataset.id;
    if (!isOpen(id)) {
      state.openId = id; // opening does not place a caret: opening is reading
      render();
      return;
    }
    if (ev.target.closest('.txt')) return; // the name is being edited

    state.openId = null;
    render();
  });

  // Enter / Space open a row from the keyboard.
  document.addEventListener('keydown', function (ev) {
    if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === 'z' && !state.busy) {
      ev.preventDefault();
      undo();
      return;
    }
    if (ev.key === 'Escape') {
      // First Escape leaves the field (which saves it); the second closes the
      // row. The field's own handler stops the event while it has focus.
      if (state.openId) { state.openId = null; render(); }
      return;
    }
    if (ev.key !== 'Enter' && ev.key !== ' ') return;
    var row = ev.target.closest && ev.target.closest('.task.openable');
    if (!row || row !== ev.target) return;
    ev.preventDefault();
    state.openId = isOpen(row.dataset.id) ? null : row.dataset.id;
    render();
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

  el.addTrigger.addEventListener('click', openAddEditor);

  // openAddEditor turns the button into a field. It stays open after a task is
  // added so several can go in one after another, and closes on Escape or on
  // leaving it empty.
  function openAddEditor() {
    var slot = el.addSlot;
    slot.classList.add('editing');
    slot.textContent = '';

    var name = document.createElement('input');
    name.type = 'text';
    name.id = 'add-name';
    name.placeholder = 'タスク名（Enter で追加）';
    name.setAttribute('aria-label', 'タスク名');
    name.autocomplete = 'off';

    var due = document.createElement('input');
    due.type = 'date';
    due.id = 'add-due';
    due.setAttribute('aria-label', '期限（任意）');

    function close() {
      state.busy = false;
      slot.classList.remove('editing');
      slot.textContent = '';
      ['', ''].forEach(function () { slot.appendChild(document.createElement('span')); });
      var plus = document.createElement('span');
      plus.className = 'plus';
      plus.setAttribute('aria-hidden', 'true');
      plus.textContent = '＋';
      var trigger = document.createElement('button');
      trigger.type = 'button';
      trigger.className = 'as-button';
      trigger.id = 'add-trigger';
      trigger.textContent = 'タスクを追加';
      trigger.addEventListener('click', openAddEditor);
      slot.appendChild(plus);
      slot.appendChild(trigger);
      slot.appendChild(document.createElement('span'));
      el.addTrigger = trigger;
    }

    function submit() {
      var value = name.value.trim();
      if (!value) { close(); return; }

      var body = { name: value };
      if (due.value) body.due = due.value;

      name.disabled = true;
      request('POST', '/api/tasks', body)
        .then(function () {
          return load().then(function () {
            // The list was redrawn; open a fresh field at the end again.
            openAddEditor();
            flash('追加した: ' + value);
          });
        })
        .catch(function (err) { name.disabled = false; showError(err); });
    }

    name.addEventListener('keydown', function (e) {
      if (composing(e)) return;
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
      if (e.key === 'Escape') { close(); }
    });
    due.addEventListener('keydown', function (e) {
      if (composing(e)) return;
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
      if (e.key === 'Escape') { close(); }
    });
    name.addEventListener('blur', function () {
      // Leaving an empty field puts the button back; a half-typed one stays.
      window.setTimeout(function () {
        if (!name.value.trim() && document.activeElement !== due) close();
      }, 120);
    });

    var plusMark = document.createElement('span');
    plusMark.className = 'plus';
    plusMark.setAttribute('aria-hidden', 'true');
    plusMark.textContent = '＋';

    slot.appendChild(document.createElement('span'));
    slot.appendChild(document.createElement('span'));
    slot.appendChild(plusMark);
    slot.appendChild(name);
    slot.appendChild(due);

    state.busy = true; // typing must not be interrupted by the poll
    name.focus();
  }

  el.doneFold.addEventListener('click', function () {
    state.doneOpen = !state.doneOpen;
    try { localStorage.setItem(DONE_KEY, state.doneOpen ? '1' : '0'); } catch (e) { /* private window */ }
    render();
  });

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

  // A link can name the task to open: http://127.0.0.1:8085/#T007
  var fromHash = (location.hash || '').replace('#', '');
  if (/^T\d+$/.test(fromHash)) state.openId = fromHash;

  try {
    state.doneOpen = localStorage.getItem(DONE_KEY) === '1';
  } catch (e) { /* storage unavailable; closed is the default */ }

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
