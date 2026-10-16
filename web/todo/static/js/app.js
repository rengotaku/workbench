// 着手順 — the page reads and writes through the same API the CLI uses, so
// neither can drift from the other or skip an event.
(function () {
  'use strict';

  // The markup of the screen. It used to be templates/index.html; the router
  // shows every screen in one page, so the screen draws itself when first shown.
  // The detail box only reads: it follows the row under the pointer. On a
  // narrow screen it is gone and opening a row is the way in (opening works on
  // a wide screen too).
  var MARKUP = [
    '<div class="wrap">',
    '  <div class="panes">',
    '  <div class="app">',
    '    <div class="app-head">',
    '      <div class="head-left">',
    '        <h1 class="app-name">着手順</h1>',
    '        <span class="app-date"></span>',
    '      </div>',
    '      <div class="head-right">',
    '        <div class="scope-set" role="group" aria-label="スコープ">',
    '          <button type="button" data-scope="7" aria-pressed="true">1週間</button>',
    '          <button type="button" data-scope="14" aria-pressed="false">2週間</button>',
    '          <button type="button" data-scope="all" aria-pressed="false">すべて</button>',
    '          <button type="button" data-scope="custom" aria-pressed="false">指定</button>',
    '        </div>',
    '        <label class="scope-until" id="scope-until-wrap" hidden>～<input type="date" id="scope-until" aria-label="スコープの締め日"></label>',
    '        <div class="counts">',
    '          <span class="count over" title="期限超過"><i aria-hidden="true"></i><b id="c-over">0</b><span class="sr">件 期限超過</span></span>',
    '          <span class="count near" title="期限が今日"><i aria-hidden="true"></i><b id="c-near">0</b><span class="sr">件 期限が今日</span></span>',
    '          <span class="count rest" title="期限がまだ先・期限なし"><i aria-hidden="true"></i><b id="c-rest">0</b><span class="sr">件 期限がまだ先・期限なし</span></span>',
    '        </div>',
    '      </div>',
    '    </div>',
    '',
    '    <section class="focus-band" id="focus-band">',
    '      <div class="focus-head">フォーカス<span class="n" id="focus-count">0</span></div>',
    '      <ul class="list focus-list" id="focus-list"></ul>',
    '      <p class="focus-empty" id="focus-empty" hidden>ここへドラッグするとフォーカスになる</p>',
    '    </section>',
    '',
    '    <section class="sec">',
    '      <ul class="list" id="queue"></ul>',
    '      <button type="button" class="more" id="more" hidden></button>',
    '      <div class="add" id="add-slot">',
    '        <span></span><span></span>',
    '        <span class="plus" aria-hidden="true">＋</span>',
    '        <button type="button" class="as-button" id="add-trigger">タスクを追加</button>',
    '        <kbd aria-hidden="true">N</kbd>',
    '      </div>',
    '    </section>',
    '',
    '    <section class="sec" id="waiting-sec" hidden>',
    '      <div class="sec-head">',
    '        <h2 class="sec-title">待ち</h2>',
    '        <span class="sec-hint">ボールを渡してからの経過日数で並ぶ</span>',
    '      </div>',
    '      <ul class="list" id="waitlist"></ul>',
    '    </section>',
    '',
    '    <section class="sec" id="someday-sec" hidden>',
    '      <button type="button" class="fold-head" id="someday-fold" aria-expanded="false" aria-controls="somedaylist">',
    '        <span class="caret" aria-hidden="true">▶</span>いつか<span class="n" id="someday-count">0</span>',
    '      </button>',
    '      <ul class="list" id="somedaylist" hidden></ul>',
    '    </section>',
    '',
    '    <section class="sec" id="done-sec" hidden>',
    '      <button type="button" class="fold-head" id="done-fold" aria-expanded="false" aria-controls="donelist">',
    '        <span class="caret" aria-hidden="true">▶</span>今週完了<span class="n" id="done-count">0</span>',
    '      </button>',
    '      <ul class="list" id="donelist" hidden></ul>',
    '    </section>',
    '',
    '    <section class="sec" id="drop-sec" hidden>',
    '      <button type="button" class="fold-head" id="drop-fold" aria-expanded="false" aria-controls="droplist">',
    '        <span class="caret" aria-hidden="true">▶</span>今週やめた<span class="n" id="drop-count">0</span>',
    '      </button>',
    '      <ul class="list" id="droplist" hidden></ul>',
    '    </section>',
    '',
    '    <div class="add-row">',
    '      <code>todo "タスク名" --due YYYY-MM-DD</code>',
    '      <span>→ 端末からも同じ口に入る</span>',
    '    </div>',
    '  </div>',
    '  <aside class="detail" id="detail" aria-live="polite"></aside>',
    '  </div>',
    '',
    '  <div class="legend">',
    '    <span class="l-over"><i></i>期限超過</span>',
    '    <span class="l-near"><i></i>期限が今日</span>',
    '    <span class="l-rest"><i></i>期限がまだ先・期限なし</span>',
    '  </div>',
    '  <p class="err" id="err" hidden></p>',
    '</div>',
  ].join('\n');

  // active is true while the todo screen is on show. Listeners on document and
  // window stay attached for the life of the page, so each one checks it: a
  // hidden screen must not take N, Escape or Cmd+Z, nor poll.
  var active = false;
  var hooks = {}; // show / hide, set once the screen is built
  var built = false;

  // dateLabel turns the board's today (YYYY-MM-DD, the server's date, which
  // the due colours are judged against) into the heading's 1/2 (曜). The
  // weekday comes from the date itself, not the browser's time zone.
  function dateLabel(today) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(today || '');
    if (!m) return '';
    var day = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])).getUTCDay();
    return (+m[2]) + '/' + (+m[3]) + ' (' + '日月火水木金土'.charAt(day) + ')';
  }

  function mount(root) {
    root.innerHTML = MARKUP;
    var dateEl = root.querySelector('.app-date');

    var shared = window.todoShared;
    var composing = shared.composing;
    var request = shared.request;
    var wirePaste = shared.wirePaste;

    var SCOPE_KEY = 'todo-order.scope';
    var DONE_KEY = 'todo-order.done-open';
    var DROP_KEY = 'todo-order.drop-open';
    var SOMEDAY_KEY = 'todo-order.someday-open';
    var POLL_MS = 4000;
    var state = {
      scope: '7',
      showOut: false,
      board: null,
      openId: null, // only one row is open at a time; another row closes it
      doneOpen: false, // the completed list is a record, not a queue
      dropOpen: false, // so is the list of what was given up on
      somedayOpen: false, // and so is the shelf: closed by default, like done
      busy: false, // a drag or a date field is in progress; do not redraw under it
      shownId: null, // the task the detail box is showing; the pointer moves it
      revealId: null, // the task a link named; its section is unfolded on first load
    };

    // The detail box keeps the last task it was given. Letting it empty when the
    // pointer leaves the list would blank it mid-read, which is when it is most
    // in use.
    var trailCache = {};

    var TRASH_SVG = '<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">'
      + '<path d="M2.5 4.5h11M6 4.5V2.8h4v1.7M4.2 4.5l.7 8.7h6.2l.7-8.7M6.6 6.8v4.6M9.4 6.8v4.6"'
      + ' fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>';

    var el = {
      queue: document.getElementById('queue'),
      waitSec: document.getElementById('waiting-sec'),
      waitList: document.getElementById('waitlist'),
      somedaySec: document.getElementById('someday-sec'),
      somedayList: document.getElementById('somedaylist'),
      somedayFold: document.getElementById('someday-fold'),
      somedayCount: document.getElementById('someday-count'),
      doneSec: document.getElementById('done-sec'),
      doneList: document.getElementById('donelist'),
      dropSec: document.getElementById('drop-sec'),
      dropList: document.getElementById('droplist'),
      more: document.getElementById('more'),
      over: document.getElementById('c-over'),
      near: document.getElementById('c-near'),
      rest: document.getElementById('c-rest'),
      focusBand: document.getElementById('focus-band'),
      focusList: document.getElementById('focus-list'),
      focusCount: document.getElementById('focus-count'),
      focusEmpty: document.getElementById('focus-empty'),
      addTrigger: document.getElementById('add-trigger'),
      detail: document.getElementById('detail'),
      doneFold: document.getElementById('done-fold'),
      doneCount: document.getElementById('done-count'),
      dropFold: document.getElementById('drop-fold'),
      dropCount: document.getElementById('drop-count'),
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
        if (!active || state.busy || !state.board || document.hidden) return;
        request('GET', '/api/revision')
          .then(function (r) {
            if (r.revision !== state.board.revision) {
              load().then(function () { flash('外で変更があったので読み直した', false); });
            } else if (r.today && r.today !== state.board.today) {
              // The date moved with no write: the due colours are for yesterday.
              load();
            }
          })
          .catch(function () { /* a blip: the next tick tries again */ });
      }, POLL_MS);

      // Coming back to the window is the moment a stale list is most visible.
      document.addEventListener('visibilitychange', function () {
        if (!active) return;
        if (!document.hidden && !state.busy) load();
      });
    }

    // reveal unfolds whatever hides the task a link named, once, on the first
    // load: a URL that opens a row is useless if the row sits in a closed
    // section or outside the scope. It does not save the unfolding — that is
    // the link's doing, not a choice the viewer made.
    function reveal(board) {
      var id = state.revealId;
      if (!id) return;
      state.revealId = null;
      function has(list, extra) {
        return (list || []).some(function (t) { return t.id === id && (!extra || extra(t)); });
      }
      if (has(board.someday)) state.somedayOpen = true;
      if (has(board.done)) state.doneOpen = true;
      if (has(board.queue, function (t) { return !t.in_scope; })) state.showOut = true;
    }

    function load() {
      return request('GET', '/api/tasks?scope=' + encodeURIComponent(state.scope))
        .then(function (board) {
          state.board = board;
          reveal(board);
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

      // The date follows the board, so it moves when the day does.
      dateEl.textContent = dateLabel(b.today);

      el.over.textContent = b.counts.over;
      el.near.textContent = b.counts.near;
      el.rest.textContent = b.counts.rest;

      // The colour is the whole legend, so each swatch says what it means. The
      // counts split the queue the same way as the row stripes.
      setTitle('.count.over', '赤: 期限を過ぎている — ' + b.counts.over + '件');
      setTitle('.count.near', '黄: 期限が今日 — ' + b.counts.near + '件');
      setTitle('.count.rest', '灰: 期限がまだ先、または期限なし — ' + b.counts.rest + '件');

      // Focused tasks are the top of the queue (ADR todo-0012) and are shown in
      // the band only, so each task appears once. The band stays when empty: it
      // is where a row is dropped to focus it.
      var focus = b.focus || [];
      el.focusCount.textContent = focus.length;
      el.focusEmpty.hidden = focus.length > 0;
      el.focusList.textContent = '';
      focus.forEach(function (t) { el.focusList.appendChild(queueRow(t, true)); });

      var queue = unfocusedQueue();
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

      // Server-sorted already: soonest until first, no-until last. Arrived rows
      // sit ahead of the rest of that order because an earlier until means
      // arrived, not because the client re-sorts anything.
      var someday = b.someday || [];
      el.somedaySec.hidden = someday.length === 0;
      var reached = someday.filter(function (t) { return t.until_reached; }).length;
      el.somedayCount.textContent = someday.length + (reached > 0 ? '（到来 ' + reached + '）' : '');
      el.somedayFold.setAttribute('aria-expanded', String(state.somedayOpen));
      el.somedayFold.querySelector('.caret').textContent = state.somedayOpen ? '▼' : '▶';
      el.somedayList.hidden = !state.somedayOpen;
      el.somedayList.textContent = '';
      someday.forEach(function (t) { el.somedayList.appendChild(somedayRow(t)); });

      refreshDetail();

      var done = b.done || [];
      el.doneSec.hidden = done.length === 0;
      el.doneCount.textContent = done.length;
      el.doneFold.setAttribute('aria-expanded', String(state.doneOpen));
      el.doneFold.querySelector('.caret').textContent = state.doneOpen ? '▼' : '▶';
      el.doneList.hidden = !state.doneOpen;
      el.doneList.textContent = '';
      done.forEach(function (t) { el.doneList.appendChild(doneRow(t)); });

      var dropped = b.dropped || [];
      el.dropSec.hidden = dropped.length === 0;
      el.dropCount.textContent = dropped.length;
      el.dropFold.setAttribute('aria-expanded', String(state.dropOpen));
      el.dropFold.querySelector('.caret').textContent = state.dropOpen ? '\u25bc' : '\u25b6';
      el.dropList.hidden = !state.dropOpen;
      el.dropList.textContent = '';
      dropped.forEach(function (d) { el.dropList.appendChild(dropRow(d)); });
    }

    // After a reload the box holds an older copy of the task. Redraw it from the
    // fresh board, or fall back to the first task when it has none yet (the box
    // should not open empty).
    function refreshDetail() {
      if (!el.detail) return;
      var b = state.board;
      if (!b) return;
      var all = (b.queue || []).concat(b.waiting || [], b.someday || [], b.done || []);
      if (all.length === 0) return;
      var still = null;
      all.forEach(function (t) { if (t.id === state.shownId) still = t; });
      if (still) { trailCache[still.id] = null; delete trailCache[still.id]; showDetail(still); return; }
      if (state.shownId === null) showDetail(all[0]);
    }

    function setTitle(sel, text) {
      var node = document.querySelector(sel);
      if (node) node.title = text;
    }

    // The due date is when to start, and the order already says what comes
    // first. Colour is only for a start date that has arrived while the row sits
    // where it is: red once it has passed, amber on the day. A date still ahead
    // and no date at all stay uncoloured, so the few that need moving stand out.
    // dueChip is the chip's colour class; dueStripe is the row's.
    function dueChip(t) {
      if (t.due_class === 'over') return 'over';
      if (t.due_class === 'today') return 'near';
      return '';
    }
    function dueStripe(t) {
      if (t.due_class === 'over') return 'overdue';
      if (t.due_class === 'today') return 'in-window';
      return '';
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
        case 'today':
          return '黄: 期限 ' + t.due + ' が今日\nクリックで変更';
        case 'near':
          return '灰: 期限 ' + t.due + ' が ' + within + '（' + t.due_label + '）\nクリックで変更';
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
        writeName(txt, opts.task);
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
        due.setAttribute('aria-haspopup', 'menu');
        due.setAttribute('aria-expanded', 'false');
        due.dataset.due = opts.task.due || '';
      } else {
        due = document.createElement('span');
        due.className = 'due';
        if (opts.dueTitle) due.title = opts.dueTitle;
      }
      if (opts.dueClass) due.classList.add(opts.dueClass);
      due.textContent = opts.dueLabel;
      li.appendChild(due);

      // Moving over a row hands it to the detail box. The box is read-only, so
      // this cannot interrupt anything being typed; editing stays in the row.
      li.addEventListener('mouseenter', function () { showDetail(opts.task); });

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

    // unfocusedQueue is the queue below the band. Focused tasks lead the queue
    // and are shown in the band instead, so the list starts after them.
    function unfocusedQueue() {
      return ((state.board && state.board.queue) || []).filter(function (t) { return !t.focused; });
    }

    // queueRow draws a queue task, in the band or in the list below it: the same
    // row either way, so a focused task opens where it is.
    function queueRow(t, inScope) {
      var cls = inScope ? dueStripe(t) : 'out';
      var li = row({
        task: t,
        draggable: true,
        // The position is not shown. The order is what the list is for, and the
        // row above/below already says it; a number only adds a value that goes
        // stale the moment anything moves. It is still kept server-side, and
        // `todo order` uses it.
        mark: '',
        markClass: '',
        dueLabel: t.due_label,
        dueClass: dueChip(t),
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
        // No mark: ◷ sat in the same column as the focus ◎ and read alike.
        // The section heading already says these are waiting.
        mark: '',
        markClass: '',
        dueLabel: days + '日',
        dueClass: days >= 10 ? 'over' : (days >= 5 ? 'near' : ''),
        dueTitle: level + '経過\n' + (t.handed_on || '?') + ' に ' + (t.handed_to || '?')
          + ' へ渡してから ' + days + '日。14日で理由を聞かれる',
        hasPanel: true,
        extraClass: 'waiting',
      });
    }

    // somedayRow shows a shelved task. Arriving is not overdue — nothing was
    // promised — so it borrows the same amber stripe waiting uses for "worth a
    // look" rather than the red one, and the tooltip says why in words. The
    // chip is not editable here: Until is not Due, and there is no editor for
    // it in the row (only `todo someday --until` and the resume button).
    function somedayRow(t) {
      return row({
        task: t,
        mark: '◇',
        markClass: 'mark-someday',
        dueLabel: t.until ? t.until + ' まで' : '',
        dueClass: t.until_reached ? 'near' : '',
        dueTitle: t.until_reached
          ? ('見る番が来た（' + t.until + ' までにしていた）')
          : (t.until ? (t.until + ' まで見なくていい') : ''),
        hasPanel: true,
        extraClass: 'someday',
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

    // dropRow shows a task that is gone. It cannot reuse row(): there is no task
    // left to tick, open, or drag — only the name it had and why it was dropped.
    function dropRow(d) {
      var li = document.createElement('li');
      li.className = 'task drop';

      ['grip empty', 'num', 'check empty'].forEach(function (cls) {
        var span = document.createElement('span');
        span.className = cls;
        span.setAttribute('aria-hidden', 'true');
        li.appendChild(span);
      });

      var body = document.createElement('div');
      body.className = 'body';

      var name = document.createElement('div');
      name.className = 'name';
      var txt = document.createElement('span');
      txt.className = 'txt';
      txt.title = d.name;
      writeName(txt, d);
      name.appendChild(txt);
      body.appendChild(name);

      var why = document.createElement('div');
      why.className = 'drop-why' + (d.reason ? '' : ' none');
      why.textContent = d.reason || '理由なし';
      body.appendChild(why);
      li.appendChild(body);

      var when = document.createElement('span');
      when.className = 'due';
      var at = new Date(d.at);
      when.textContent = (at.getMonth() + 1) + '/' + at.getDate();
      when.title = when.textContent + ' にやめた。直近の月曜以降の分だけ残る';
      li.appendChild(when);

      return li;
    }

    // showDetail fills the box beside the list. It reads; every change is still
    // made in the row, so the box can disappear at narrow widths without taking
    // any operation with it.
    function showDetail(t) {
      if (!el.detail) return;
      state.shownId = t.id;
      el.detail.textContent = '';

      var eyebrow = document.createElement('div');
      eyebrow.className = 'd-eyebrow';
      var id = document.createElement('span');
      id.textContent = t.id;
      var where = document.createElement('span');
      where.textContent = detailState(t);
      eyebrow.appendChild(id);
      eyebrow.appendChild(where);
      el.detail.appendChild(eyebrow);

      var name = document.createElement('p');
      name.className = 'd-name';
      writeName(name, t);
      el.detail.appendChild(name);

      var body = document.createElement('div');
      body.className = 'd-body' + (t.detail ? '' : ' none');
      body.textContent = t.detail || '詳細なし';
      el.detail.appendChild(body);

      var related = relatedList(t);
      if (related) el.detail.appendChild(related);

      var supplements = supplementsBlock(t);
      if (supplements) el.detail.appendChild(supplements);

      var sec = document.createElement('div');
      sec.className = 'd-sec';
      var h = document.createElement('h2');
      h.textContent = '経緯';
      sec.appendChild(h);
      var list = document.createElement('ul');
      list.className = 'd-trail';
      sec.appendChild(list);
      el.detail.appendChild(sec);

      if (trailCache[t.id]) {
        fillDetailTrail(list, trailCache[t.id]);
        return;
      }
      var waiting = document.createElement('li');
      waiting.textContent = '読み込み中';
      list.appendChild(waiting);
      var asked = t.id;
      request('GET', '/api/tasks/' + encodeURIComponent(t.id) + '/context')
        .then(function (data) {
          trailCache[asked] = data.events || [];
          if (state.shownId !== asked) return; // the pointer moved on
          fillDetailTrail(list, trailCache[asked]);
        })
        .catch(function () {
          if (state.shownId !== asked) return;
          list.textContent = '';
          var oops = document.createElement('li');
          oops.textContent = '経緯を読めない';
          list.appendChild(oops);
        });
    }

    var ANNOTATION_LABEL = { quote: '引用', analysis: '考察', memo: 'メモ' };

    // supplementsBlock shows what was appended to a task (newest first, like the
    // trail) and the link back to the memo it was made from. Every value goes in
    // through textContent, so a body is shown as text and never read as HTML.
    // It returns null when there is nothing to show.
    // relatedList shows a task's related URLs ([N] 一言 / URL) in the detail
    // box, or null when it has none. A task from before related URLs keeps its
    // one link in the name and has no list.
    function relatedList(t) {
      if (!t.links || t.links.length === 0) return null;
      var sec = document.createElement('div');
      sec.className = 'd-sec';
      var h = document.createElement('h2');
      h.textContent = '関連 URL';
      sec.appendChild(h);
      var ul = document.createElement('ul');
      ul.className = 'd-links';
      t.links.forEach(function (l) {
        var li = document.createElement('li');
        var n = document.createElement('span');
        n.className = 'ln';
        n.textContent = '[' + l.n + ']';
        var a = document.createElement('a');
        a.href = l.url;
        a.target = '_blank';
        a.rel = 'noopener noreferrer';
        a.textContent = l.label || l.url;
        a.title = l.url;
        li.appendChild(n);
        li.appendChild(a);
        ul.appendChild(li);
      });
      sec.appendChild(ul);
      return sec;
    }

    // linkEditors holds the open row's related-URL rows, so a paste into its
    // name can add one.
    var linkEditors = {};

    // linksBlock is the open row's related URLs, edited in place like the name:
    // adding (or pasting) a URL, editing a row and removing one save at once.
    // Numbers follow the rows' order, and a row the name still points at (or
    // one before it) cannot be removed, so a [N] never moves to another URL.
    function linksBlock(t) {
      function nameNow() {
        var n = document.querySelector('.task[data-id="' + t.id + '"] .txt.live');
        return n ? n.textContent : t.name;
      }
      var last = JSON.stringify((t.links || []).map(function (l) { return { url: l.url, label: l.label || '' }; }));
      var ed = shared.linkEditor({
        text: nameNow,
        onError: function (msg) { showError(new Error(msg)); },
        onChange: save,
        onEnter: function () { if (document.activeElement) document.activeElement.blur(); },
      });
      // One save at a time: each PATCH replaces the whole list, so two in
      // flight could land in either order and the older list would win. A save
      // asked for meanwhile runs once the one in flight has answered, and
      // compares with what the server last confirmed (last), so changing a row
      // and changing it back while a save is out still reaches the server.
      var inFlight = false;
      var again = false;
      function save() {
        if (inFlight) { again = true; return; }
        var links = ed.read(true);
        if (!links) return; // an empty row: saved once it has a URL
        var now = JSON.stringify(links);
        if (now === last) return;
        inFlight = true;
        mark(ed.el, '保存中…');
        request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), { links: links })
          .then(function (res) {
            last = now;
            t.links = (res && res.task && res.task.links) || links.map(function (l, i) { return { n: i + 1, url: l.url, label: l.label }; });
            mark(ed.el, '保存した', true);
          })
          .catch(function (err) { showError(err); })
          .then(function () {
            inFlight = false;
            if (again) { again = false; save(); }
          });
      }
      ed.set(t.links || []);
      // Typing in a row holds the poll, as the name and detail do.
      ed.el.addEventListener('focusin', function () { state.busy = true; });
      ed.el.addEventListener('focusout', function () { state.busy = false; });
      linkEditors = {};
      linkEditors[t.id] = ed;
      return ed.el;
    }

    function supplementsBlock(t) {
      var anns = t.annotations || [];
      if (anns.length === 0 && !t.source_note_id) return null;

      var sec = document.createElement('div');
      sec.className = 'd-sec d-ann';
      if (t.source_note_id) {
        var from = document.createElement('a');
        from.className = 'd-from';
        from.href = '/notes/timeline#note-' + encodeURIComponent(t.source_note_id);
        from.textContent = '元のメモ #' + t.source_note_id;
        sec.appendChild(from);
      }
      if (anns.length > 0) {
        var h = document.createElement('h2');
        h.textContent = '補足';
        sec.appendChild(h);
      }
      anns.slice().reverse().forEach(function (a) {
        var item = document.createElement('div');
        item.className = 'd-ann-item k-' + String(a.kind).replace(/[^a-z]/g, '');
        var who = document.createElement('div');
        who.className = 'd-ann-who';
        var d = new Date(a.created_at);
        who.textContent = (ANNOTATION_LABEL[a.kind] || a.kind)
          + ' · ' + (a.author === 'ai' ? 'AI' : '人')
          + ' · ' + (d.getMonth() + 1) + '/' + d.getDate() + ' '
          + ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2)
          + (a.source ? ' · ' + a.source : '');
        var body = document.createElement('div');
        body.className = 'd-ann-body';
        body.textContent = a.body;
        item.appendChild(who);
        item.appendChild(body);
        sec.appendChild(item);
      });
      return sec;
    }

    function detailState(t) {
      if (t.state === 'waiting') {
        return (t.handed_to ? t.handed_to + ' 待ち' : '待ち') + ' ' + (t.stalled_for || 0) + '日';
      }
      if (t.state === 'someday') {
        return 'いつか' + (t.until ? '（' + t.until + ' まで）' : '');
      }
      if (t.state === 'done') return '完了';
      return t.focused ? '着手順 ◎ フォーカス' : '着手順';
    }

    function fillDetailTrail(list, events) {
      list.textContent = '';
      if (events.length === 0) {
        var none = document.createElement('li');
        none.textContent = '経緯なし';
        list.appendChild(none);
        return;
      }
      events.slice().reverse().forEach(function (e) {
        var li = document.createElement('li');
        var d = new Date(e.at);
        var when = document.createElement('time');
        when.textContent = (d.getMonth() + 1) + '/' + d.getDate() + ' '
          + ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
        li.appendChild(when);
        var what = (KIND_LABEL[e.kind] || e.kind) + changeOf(e) + (e.actor === 'ai' ? '（AI）' : '');
        li.appendChild(document.createTextNode(what + (e.reason ? ' — ' + e.reason : '')));
        list.appendChild(li);
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

      panel.appendChild(linksBlock(t));

      var supplements = supplementsBlock(t);
      if (supplements) panel.appendChild(supplements);

      var actions = document.createElement('div');
      actions.className = 'panel-actions';

      // Only queue rows offer focus: the band shows nothing else, so the button
      // would claim a move that does not happen for waiting or someday rows.
      if (t.state === 'open') {
        var focusBtn = document.createElement('button');
        focusBtn.type = 'button';
        focusBtn.className = t.focused ? 'focus-on' : '';
        focusBtn.textContent = t.focused ? 'フォーカスを外す' : 'フォーカス';
        // The button is the keyboard's way to do what dragging between the band
        // and the list does.
        focusBtn.title = t.focused
          ? 'フォーカスから外す（一覧の先頭へ移る）'
          : 'フォーカスに入れる（フォーカスの末尾へ移る）';
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

      actions.appendChild(copyUrlButton(t));

      var saved = document.createElement('span');
      saved.className = 'save-state';
      saved.dataset.save = '1';
      actions.appendChild(saved);

      var spacer = document.createElement('span');
      spacer.className = 'spacer';
      actions.appendChild(spacer);

      if (t.state === 'waiting' || t.state === 'someday') {
        var resume = document.createElement('button');
        resume.type = 'button';
        resume.textContent = '着手順へ戻す';
        resume.title = t.state === 'waiting'
          ? ((t.handed_on || '?') + ' に ' + (t.handed_to || '?') + ' へ渡した分を戻す')
          : ((t.until || '?') + ' まで棚上げしていた分を戻す');
        resume.addEventListener('click', function () {
          resume.disabled = true;
          request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/resume', { note: '' })
            .then(function () { return load().then(function () { flash('着手順へ戻した'); }); })
            .catch(function (err) { resume.disabled = false; showError(err); });
        });
        actions.appendChild(resume);
        if (t.state === 'waiting') actions.appendChild(somedayButton(t));
      } else if (t.state === 'open') {
        // いつかにする lives in the due chip's menu for open rows.
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
            // Not while another field of the row is being typed in (its related
            // URL rows): redrawing would drop what is typed there.
            if (reload && !state.busy) load();
          })
          .catch(function (err) { showError(err); node.textContent = last; });
      }

      if (key === 'name') {
        // A link pasted into the name goes in as "text [N]" with its URL as
        // related URL N, in the open row's rows below (/shared/js/paste.js).
        window.workbench.paste.wireLinkPaste(node, function (link) {
          var ed = linkEditors[t.id];
          return ed ? ed.add(link.url + (link.label ? ' ' + link.label : ''), true) : 0;
        }, true);
      } else {
        wirePaste(node, !!opts.single);
      }
      if (opts.single) paintLink(node, t);
      node.addEventListener('focus', function () { state.busy = true; });
      node.addEventListener('input', function () {
        if (opts.single) paintLink(node, t);
        window.clearTimeout(timer);
        timer = window.setTimeout(function () { save(false); }, 900);
      });
      node.addEventListener('blur', function () {
        window.clearTimeout(timer);
        state.busy = false;
        if (opts.single && window.CSS && CSS.highlights) CSS.highlights.delete(HIGHLIGHT_NAME);
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

    // linkRange finds the part of a name that should carry the link: the words
    // that were the link where it was copied from. A task with no link, or whose
    // name no longer contains those words, has no range.
    function linkRange(t) {
      if (!t.url) return null;
      var label = t.link_text || t.name;
      var at = t.name.indexOf(label);
      if (at === -1 || !label) return null;
      return { start: at, end: at + label.length };
    }

    // writeName puts a task's name into el, with the link range as a link and the
    // rest as plain text. Text goes in as text nodes, never as HTML.
    function writeName(el, t) {
      el.textContent = '';
      // Each [N] naming one of the task's related URLs is a link to it (ADR
      // todo-0014). A task from before them also keeps its one link over the
      // words it covered (ADR todo-0008), related URLs added later or not.
      var links = t.links || [];
      var r = linkRange(t);
      if (!r) {
        shared.writeLinkedText(el, t.name, links);
        return;
      }
      if (r.start > 0) shared.writeLinkedText(el, t.name.slice(0, r.start), links);
      var a = document.createElement('a');
      a.className = 'ticket';
      a.href = t.url;
      a.textContent = t.name.slice(r.start, r.end);
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.title = t.url;
      a.draggable = false; // dragging a row must not drag the link instead
      el.appendChild(a);
      if (r.end < t.name.length) shared.writeLinkedText(el, t.name.slice(r.end), links);
    }

    // paintLink marks the link range inside an editable name. It uses the
    // highlight API rather than wrapping the text in an element: the node keeps
    // one text node, so repainting never moves the caret, and there is no anchor
    // under the caret to fight with. Where the API is missing the name simply
    // shows unmarked.
    var HIGHLIGHT_NAME = 'link-part';
    function paintLink(node, t) {
      if (typeof Highlight !== 'function' || !window.CSS || !CSS.highlights) return;
      CSS.highlights.delete(HIGHLIGHT_NAME);
      var text = node.firstChild;
      if (!text || text.nodeType !== 3) return;
      var r = linkRange({ url: t.url, name: text.data, link_text: t.link_text });
      if (!r) return;
      var range = document.createRange();
      range.setStart(text, r.start);
      range.setEnd(text, r.end);
      CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(range));
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

    // taskUrl is the address that opens this row: the page reads #Txxx on load.
    // It is what gets pasted to an AI to say which task is meant.
    function taskUrl(t) {
      return location.origin + location.pathname + '#' + t.id;
    }

    // copyUrlButton puts taskUrl on the clipboard. Success is said on the button
    // itself, not in the shared notice: a copy often follows a blur that saves,
    // and the notice is where a failed save is reported. The clipboard API needs
    // a secure context; 127.0.0.1 counts as one, but if it is refused anyway the
    // URL is shown so it can still be copied by hand.
    function copyUrlButton(t) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.textContent = 'URLをコピー';
      btn.title = taskUrl(t) + '\nAI にこのタスクを伝えるときに貼る';
      btn.addEventListener('click', function () {
        var url = taskUrl(t);
        if (!navigator.clipboard || !navigator.clipboard.writeText) {
          showError(new Error('クリップボードに書けない: ' + url));
          return;
        }
        navigator.clipboard.writeText(url)
          .then(function () {
            btn.textContent = 'コピーした';
            window.setTimeout(function () { btn.textContent = 'URLをコピー'; }, 1500);
          })
          .catch(function (err) {
            showError(new Error('クリップボードに書けない（' + err.message + '）: ' + url));
          });
      });
      return btn;
    }

    // somedayButton shelves a task with one click: unlike waitControl, nobody
    // holds it, so there is nothing to ask before sending the request. The
    // until date is left to the server, which sets it a month out.
    function somedayButton(t) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.textContent = 'いつかにする';
      btn.title = 'いまは着手順に置かず、いつか再開する（1ヶ月後に見る番が来る）';
      btn.addEventListener('click', function () {
        btn.disabled = true;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/someday', {})
          .then(function () { return load().then(function () { flash('いつかへ棚上げした'); }); })
          .catch(function (err) { btn.disabled = false; showError(err); });
      });
      return btn;
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
      linked: 'リンクを設定',
      links_set: '関連 URL を変更',
      focused: 'フォーカスに入れた',
      unfocused: 'フォーカスから外した',
      reordered: '順序変更',
      due_set: '期限',
      waiting: '待ちへ',
      resumed: '復帰',
      shelved: '棚上げ',
      done: '完了',
      reopened: '再開',
      detail_edited: '詳細を編集',
      annotated: '補足を追加',
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
      if (!active) return;
      if (ev.target.closest('a.ticket')) return; // let the link do its job
      if (ev.target.closest('.due-menu')) return; // the menu handles its own clicks

      var chip = ev.target.closest('.due-edit');
      if (chip) {
        var same = dueMenu && dueMenu.chip === chip;
        closeDueMenu();
        if (!same) openDueMenu(chip);
        return;
      }
      // A click outside an open menu only closes it: it should not also open
      // or close the row underneath.
      if (dueMenu) {
        closeDueMenu();
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
      if (!active) return;
      // While the add dialog is open the list's keys are off: Cmd+Z here would
      // undo the list's history behind it.
      if (document.querySelector('dialog[open]')) return;
      if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === 'z' && !state.busy) {
        ev.preventDefault();
        undo();
        return;
      }
      if (ev.key === 'Escape' && dueMenu) {
        closeDueMenu();
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
      if (!active) return;
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

    // ---- due menu ----
    //
    // The due chip opens one menu for everything that moves a task later: a
    // date, a week or a month on, no date, or off the queue to someday. They
    // used to live in three places (the chip, an emptied date field, the panel).

    var dueMenu = null; // { chip, node }

    function ymd(d) {
      return d.getFullYear() + '-' + ('0' + (d.getMonth() + 1)).slice(-2) + '-' + ('0' + d.getDate()).slice(-2);
    }

    function shortDate(d) { return (d.getMonth() + 1) + '/' + d.getDate(); }

    // shiftBase is where "+1週間" counts from: the due date if it is still ahead,
    // otherwise today. Counting from an overdue date could land a week on and
    // still be overdue.
    function shiftBase(due) {
      var now = new Date();
      var today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      if (!due) return today;
      var d = new Date(due + 'T00:00:00');
      return isNaN(d) || d < today ? today : d;
    }

    function addDays(d, n) { var x = new Date(d); x.setDate(x.getDate() + n); return x; }
    // A month on follows Go's AddDate, which the server uses for someday: 1/31
    // goes to 3/3, not 2/28. Keeping one rule means the two never disagree.
    function addMonths(d, n) { var x = new Date(d); x.setMonth(x.getMonth() + n); return x; }

    function dueMenuItem(label, hint, cls, run) {
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'dm-item' + (cls ? ' ' + cls : '');
      b.setAttribute('role', 'menuitem');
      var l = document.createElement('span');
      l.textContent = label;
      b.appendChild(l);
      if (hint) {
        var h = document.createElement('span');
        h.className = 'dm-hint';
        h.textContent = hint;
        b.appendChild(h);
      }
      b.addEventListener('click', function () {
        b.disabled = true;
        run();
      });
      return b;
    }

    function dueMenuLabel(text) {
      var d = document.createElement('div');
      d.className = 'dm-label';
      d.textContent = text;
      return d;
    }

    function openDueMenu(chip) {
      var id = chip.closest('.task').dataset.id;
      var current = chip.dataset.due || '';
      var path = '/api/tasks/' + encodeURIComponent(id);

      function setDue(value, said) {
        closeDueMenu();
        request('PATCH', path, { due: value })
          .then(function () { return load().then(function () { flash(said); }); })
          .catch(function (err) { showError(err); load(); });
      }

      var menu = document.createElement('div');
      menu.className = 'due-menu';
      menu.setAttribute('role', 'menu');
      menu.setAttribute('aria-label', '期限');

      var sec = document.createElement('div');
      sec.className = 'dm-sec';
      sec.appendChild(dueMenuLabel('期限'));
      var input = document.createElement('input');
      input.type = 'date';
      input.className = 'due-input';
      input.value = current;
      input.setAttribute('aria-label', '期限の日付');
      input.addEventListener('change', function () {
        if (!input.value || input.value === current) return;
        setDue(input.value, '期限を ' + shortDate(new Date(input.value + 'T00:00:00')) + ' にした');
      });
      sec.appendChild(input);
      menu.appendChild(sec);

      var base = shiftBase(current);
      sec = document.createElement('div');
      sec.className = 'dm-sec';
      sec.appendChild(dueMenuLabel('後ろにずらす（' + shortDate(base) + ' から）'));
      [['+1週間', addDays(base, 7)], ['+2週間', addDays(base, 14)], ['+1ヶ月', addMonths(base, 1)]]
        .forEach(function (o) {
          sec.appendChild(dueMenuItem(o[0], shortDate(o[1]), '', function () {
            setDue(ymd(o[1]), '期限を ' + shortDate(o[1]) + ' にずらした');
          }));
        });
      menu.appendChild(sec);

      sec = document.createElement('div');
      sec.className = 'dm-sec';
      if (current) {
        sec.appendChild(dueMenuItem('期限なし', '', 'dm-clear', function () {
          setDue('', '期限を外した');
        }));
      }
      sec.appendChild(dueMenuItem('いつかにする', shortDate(addMonths(shiftBase(''), 1)) + ' に見直す', 'dm-someday',
        function () {
          closeDueMenu();
          request('POST', path + '/someday', {})
            .then(function () { return load().then(function () { flash('いつかへ棚上げした'); }); })
            .catch(function (err) { showError(err); load(); });
        }));
      menu.appendChild(sec);

      menu.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        var items = Array.prototype.slice.call(menu.querySelectorAll('input, button:not([disabled])'));
        var at = items.indexOf(document.activeElement);
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          closeDueMenu();
          chip.focus();
        } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
          // Inside the date field the arrows step the date; leave them alone.
          if (document.activeElement === input) return;
          e.preventDefault();
          e.stopPropagation();
          var step = e.key === 'ArrowDown' ? 1 : -1;
          items[(at + step + items.length) % items.length].focus();
        } else if (e.key === 'Enter' || e.key === ' ') {
          e.stopPropagation(); // the button's own click runs; the row must not toggle
        }
      });

      // The menu hangs off the screen, not the row: the row redraws on every
      // load and would take the menu with it. Inside the screen's root so its
      // styles (scoped to .view-todo) reach it.
      root.appendChild(menu);
      var r = chip.getBoundingClientRect();
      var left = Math.max(8, Math.min(r.right - menu.offsetWidth, window.innerWidth - menu.offsetWidth - 8));
      menu.style.left = (left + window.scrollX) + 'px';
      menu.style.top = (r.bottom + 4 + window.scrollY) + 'px';

      chip.setAttribute('aria-expanded', 'true');
      state.busy = true; // hold the poll: a redraw would replace the chip under the menu
      dueMenu = { chip: chip, node: menu };
      var first = menu.querySelector('.dm-item');
      if (first) first.focus();
    }

    function closeDueMenu() {
      if (!dueMenu) return;
      dueMenu.node.remove();
      dueMenu.chip.setAttribute('aria-expanded', 'false');
      dueMenu = null;
      state.busy = false;
    }

    window.addEventListener('resize', closeDueMenu);

    // "タスクを追加" and N open the add page (/add) in a dialog over the list:
    // the same form, due chips and detail as a bare `todo` opens. The list is
    // reloaded after it closes if a task went in.
    function openAdd() {
      window.workbench.openAddDialog('/add', 'タスクを追加', function (changed) {
        if (changed) load();
      });
    }
    el.addTrigger.addEventListener('click', openAdd);

    // N, the same key as on the timeline. Not while typing (a name, a due date)
    // or while a dialog is already open.
    document.addEventListener('keydown', function (ev) {
      if (!active) return;
      if (composing(ev) || ev.metaKey || ev.ctrlKey || ev.altKey) return;
      if (ev.key !== 'n' && ev.key !== 'N') return;
      if (document.querySelector('dialog[open]')) return;
      var t = ev.target;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return;
      ev.preventDefault();
      openAdd();
    });

    el.doneFold.addEventListener('click', function () {
      state.doneOpen = !state.doneOpen;
      try { localStorage.setItem(DONE_KEY, state.doneOpen ? '1' : '0'); } catch (e) { /* private window */ }
      render();
    });

    el.dropFold.addEventListener('click', function () {
      state.dropOpen = !state.dropOpen;
      try { localStorage.setItem(DROP_KEY, state.dropOpen ? '1' : '0'); } catch (e) { /* private window */ }
      render();
    });

    el.somedayFold.addEventListener('click', function () {
      state.somedayOpen = !state.somedayOpen;
      try { localStorage.setItem(SOMEDAY_KEY, state.somedayOpen ? '1' : '0'); } catch (e) { /* private window */ }
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

    // ---- drag to reorder, and to focus ----

    // Rows move between two lists: the band (focused tasks, the top of the
    // queue) and the list below it. Dropping across them changes focus; dropping
    // within one reorders (ADR todo-0012).
    var dragged = null;
    var dropped = false; // a drag ended by Escape or outside both lists saves nothing

    function onDragStart(ev) {
      var li = ev.target.closest('.task');
      if (!li) return;
      state.busy = true;
      dragged = li;
      dropped = false;
      li.classList.add('dragging');
      el.focusBand.classList.add('drop-ready');
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', li.dataset.id);
    }

    function onDragOver(list) {
      return function (ev) {
        if (!dragged) return;
        ev.preventDefault();
        var over = ev.target.closest('.task');
        if (over && over !== dragged && over.parentNode === list) {
          var box = over.getBoundingClientRect();
          var after = (ev.clientY - box.top) > box.height / 2;
          list.insertBefore(dragged, after ? over.nextSibling : over);
          return;
        }
        // Over the area but not over a row (the band's heading or empty-band
        // hint, the space below the last row, the add button): the row goes to
        // the end of that list. This is also how a row reaches an empty list.
        if (!over && dragged.parentNode !== list) {
          list.appendChild(dragged);
        }
      };
    }

    function onDrop(ev) {
      ev.preventDefault();
      dropped = true;
    }

    // The list's drop area is its whole section, so an empty list (every task
    // focused, or every one folded under スコープ外) still takes a row.
    var queueArea = el.queue.closest('.sec') || el.queue;
    el.focusBand.addEventListener('dragstart', onDragStart);
    el.queue.addEventListener('dragstart', onDragStart);
    el.focusBand.addEventListener('dragover', onDragOver(el.focusList));
    queueArea.addEventListener('dragover', onDragOver(el.queue));
    el.focusBand.addEventListener('drop', onDrop);
    queueArea.addEventListener('drop', onDrop);
    el.focusBand.addEventListener('dragend', onDragEnd);
    el.queue.addEventListener('dragend', onDragEnd);

    function onDragEnd() {
      if (!dragged) return;
      var li = dragged;
      li.classList.remove('dragging');
      el.focusBand.classList.remove('drop-ready');
      dragged = null;
      if (!dropped) {
        // Cancelled: the rows were only moved for the preview. Put them back.
        state.busy = false;
        render();
        return;
      }

      var id = li.dataset.id;
      var inBand = li.parentNode === el.focusList;
      var task = (state.board.queue || []).filter(function (t) { return t.id === id; })[0];
      if (!task) { state.busy = false; load(); return; }
      // focused is omitted from the JSON when false, so compare a boolean:
      // undefined !== false would send every reorder to /focus as a no-op.
      var crossed = inBand !== Boolean(task.focused);

      // The place is "just after the row above it", read against the whole
      // queue without the moved task. Rows hidden under スコープ外 are not on
      // screen, so counting rows on screen would land somewhere else.
      var rest = (state.board.queue || []).filter(function (t) { return t.id !== id; });
      var prev = li.previousElementSibling;
      var to;
      if (prev && prev.dataset.id) {
        to = rest.map(function (t) { return t.id; }).indexOf(prev.dataset.id) + 1;
      } else {
        to = inBand ? 0 : rest.filter(function (t) { return t.focused; }).length;
      }

      // Crossing between the band and the list is a change of focus; the
      // server clamps the place to the block the task now belongs to. Within
      // one list it is a reorder. Both carry the revision the positions were
      // read at: if the list changed in between, the server refuses instead of
      // dropping the row somewhere else, and the page re-reads.
      var req = crossed
        ? request('POST', '/api/tasks/' + encodeURIComponent(id) + '/focus', {
          on: inBand,
          to: to,
          revision: state.board.revision,
        })
        : request('POST', '/api/tasks/' + encodeURIComponent(id) + '/order', {
          to: to,
          scope_days: Number(state.scope) || 7,
          revision: state.board.revision,
        });
      req
        .then(function () {
          state.busy = false;
          return load().then(function () {
            if (crossed) flash(inBand ? 'フォーカスに入れた' : 'フォーカスから外した');
          });
        })
        .catch(function (err) {
          state.busy = false;
          showError(err);
          load();
        });
    }

    // ---- boot ----

    // A link can name the task to open: http://127.0.0.1:8085/#T007
    openFromHash(location.hash);

    try {
      state.doneOpen = localStorage.getItem(DONE_KEY) === '1';
      state.dropOpen = localStorage.getItem(DROP_KEY) === '1';
      state.somedayOpen = localStorage.getItem(SOMEDAY_KEY) === '1';
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

    // ---- shown and hidden by the router ----

    // openFromHash opens the task a #T007 hash names, unfolding whatever hides
    // it on the next load. It reports whether the hash named one.
    function openFromHash(hash) {
      var id = (hash || '').replace('#', '');
      if (!/^T\d+$/.test(id)) return false;
      state.openId = id;
      state.revealId = id;
      return true;
    }

    hooks.show = function (url) {
      // A link from another screen can name a task (/#T007). The list may also
      // have moved while another screen was on show, so it is read again.
      openFromHash(url.hash);
      load();
    };
    hooks.hide = function () {
      closeDueMenu();
    };

  }

  window.workbench.router.register('todo', {
    title: '着手順',
    mount: function (root) {
      active = true;
      mount(root);
      built = true;
    },
    show: function (url) {
      active = true;
      // The first show is the mount, which has just loaded the list.
      if (built && hooks.firstShown) hooks.show(url);
      hooks.firstShown = true;
    },
    hide: function () {
      active = false;
      if (hooks.hide) hooks.hide();
    },
  });
})();
