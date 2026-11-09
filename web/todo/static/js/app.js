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
    '  <div class="app"></div>',
    '  <aside class="detail" id="detail" aria-live="polite"></aside>',
    '  <datalist id="todo-topics"></datalist>',
    '  </div>',
    '',
    '  <div class="legend">',
    '    <span class="l-over"><i></i>期限超過</span>',
    '    <span class="l-near"><i></i>期限が今日・3日以内</span>',
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

  function mount(root) {
    root.innerHTML = MARKUP;
    var appEl = root.querySelector('.app');

    var shared = window.todoShared;
    var composing = shared.composing;
    var request = shared.request;
    // The list's drawing and its date helpers are list.js's (loaded before this).
    var list = window.workbench.todoList;
    var detail = window.workbench.todoDetail;
    var monthDay = list.monthDay;
    var shiftDay = list.shiftDay;

    var SCOPE_KEY = 'todo-order.scope';
    var DONE_KEY = 'todo-order.done-open';
    var DROP_KEY = 'todo-order.drop-open';
    var SOMEDAY_KEY = 'todo-order.someday-open';
    var OUT_KEY = 'todo-order.out-open';
    var WAIT_KEY = 'todo-order.wait-open';
    var LONG_KEY = 'todo-order.long-open';
    var GROUP_KEY = 'todo-order.group-by-topic';
    var POLL_MS = 4000;
    var state = {
      scope: 'week',
      scopeKey: 'week', // which scope button is pressed; 'custom' has its own date field
      menuId: null, // the task whose due menu is open
      // Only the band and the in-scope queue are always open. What lies past
      // the scope and what waits on someone else are opened when wanted.
      outOpen: false,
      waitOpen: false,
      // The long-run band is a reminder, not today's work: closed by default
      // (ADR todo-0019).
      longOpen: false,
      // Group the queue under a heading per topic. Display only: the order is
      // still one list (ADR todo-0024).
      groupByTopic: false,
      board: null,
      openId: null, // only one row is open at a time; another row closes it
      doneOpen: false, // the completed list is a record, not a queue
      dropOpen: false, // so is the list of what was given up on
      somedayOpen: false, // and so is the shelf: closed by default, like done
      statusDrafts: {}, // unsent status text per task id; survives redraws
      reviewing: {}, // long-run task ids whose 見直した is in flight
      shownId: null, // the task the detail box is showing; the pointer moves it
      revealId: null, // the task a link named; its section is unfolded on first load
      landedId: null, // the task just resumed; its row is tinted for a moment
    };
    // busy: a drag, a menu or a field is in progress; do not redraw under it.
    // list.js keeps the open row's name and panel while it holds. When it is
    // released, draw once more (after the focus has settled, so moving between
    // two fields does not count) so what was held back catches up with the board.
    var busy = false;
    Object.defineProperty(state, 'busy', {
      enumerable: true,
      get: function () { return busy; },
      set: function (v) {
        var released = busy && !v;
        busy = !!v;
        if (released) window.setTimeout(function () { if (!busy) render(); }, 0);
      },
    });
    var landedTimer = null;
    var LANDED_MS = 3000;

    // The frame's own elements. What is inside .app is drawn by list.js (the
    // header, the bands, every section), and a draw may make a new element, so
    // those are looked up when used instead of kept.
    var el = {
      detail: document.getElementById('detail'),
      topicList: document.getElementById('todo-topics'),
      err: document.getElementById('err'),
    };
    var LIVE = {
      queue: 'queue',
      focusList: 'focus-list',
      outList: 'outlist',
      longList: 'long-list',
      focusBand: 'focus-band',
      until: 'scope-until',
    };
    Object.keys(LIVE).forEach(function (k) {
      Object.defineProperty(el, k, { get: function () { return document.getElementById(LIVE[k]); } });
    });


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
      if (has(board.queue, function (t) { return !t.in_scope && !t.focused; })) state.outOpen = true;
      if (has(board.waiting)) state.waitOpen = true;
      if (has(board.long)) state.longOpen = true;
    }

    // loadSeq numbers the reads, so a slow reply for an old moment (or an old
    // scope) cannot draw over a newer one: only the latest read is taken up.
    // The timeline's seq does the same.
    var loadSeq = 0;
    function load() {
      var mine = ++loadSeq;
      return request('GET', '/api/tasks?scope=' + encodeURIComponent(state.scope))
        .then(function (board) {
          if (mine !== loadSeq) return;
          state.board = board;
          reveal(board);
          render();
          showError(null);
        })
        .catch(function (err) { if (mine === loadSeq) showError(err); });
    }

    // reloadShowing draws the list again and then shows the error. load() clears
    // the message when it succeeds, so showing it first would lose it at once.
    function reloadShowing(err) {
      return load().then(function () { showError(err); });
    }

    function showError(err) {
      if (!err) { el.err.hidden = true; return; }
      el.err.classList.remove('notice');
      el.err.textContent = err.message || String(err);
      el.err.hidden = false;
    }

    // flash says what just happened without looking like a failure, and offers
    // to take it back. Undo is where a mis-click gets fixed, so it belongs next
    // to the message about the click. go, when given, is one more button placed
    // before undo: { label, run }.
    function flash(text, undoable, go) {
      el.err.classList.add('notice');
      el.err.textContent = text;
      if (go) {
        var goLink = document.createElement('button');
        goLink.type = 'button';
        goLink.className = 'undo-link';
        goLink.textContent = go.label;
        goLink.addEventListener('click', go.run);
        el.err.appendChild(document.createTextNode('  '));
        el.err.appendChild(goLink);
      }
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
      // A newer notice gets its own 6 seconds; the older one's timer would
      // otherwise hide it early.
      window.clearTimeout(flashTimer);
      flashTimer = window.setTimeout(function () {
        if (el.err.classList.contains('notice')) el.err.hidden = true;
      }, 6000);
    }
    var flashTimer = null;

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

    // render draws the list from state.board. The drawing is list.js's (Preact):
    // it keeps the rows that did not change, so an open row, a field being typed
    // in and the focus survive a draw. Before the first reply it draws the empty
    // frame. The detail box and the topic suggestions are outside .app and stay
    // here.
    function render() {
      // A drag previews by moving the dragged row's own element. Put it back
      // where the last draw left it, so the next draw finds the DOM it expects.
      if (dragRestore) { dragRestore(); dragRestore = null; }
      list.render(appEl, { board: state.board, state: state, ctx: ctx });
      var b = state.board;
      if (!b) return;
      fillTopicList(b.topics || []);
      refreshDetail();
    }

    // fillTopicList refreshes the suggestions every topic field offers.
    function fillTopicList(topics) {
      if (!el.topicList) return;
      el.topicList.textContent = '';
      topics.forEach(function (name) {
        var o = document.createElement('option');
        o.value = name;
        el.topicList.appendChild(o);
      });
    }

    // After a reload the box holds an older copy of the task. Redraw it from the
    // fresh board, or fall back to the first task when it has none yet (the box
    // should not open empty).
    function refreshDetail() {
      if (!el.detail) return;
      var b = state.board;
      if (!b) return;
      var all = (b.queue || []).concat(b.long || [], b.waiting || [], b.someday || [], b.done || []);
      if (all.length === 0) return;
      var still = null;
      all.forEach(function (t) { if (t.id === state.shownId) still = t; });
      if (still) { showDetail(still); return; }
      if (state.shownId === null) showDetail(all[0]);
    }

    // openLongTask opens the long-run fold and the task's row in it.
    function openLongTask(id) {
      state.longOpen = true;
      state.openId = id;
      render();
      var node = el.longList.querySelector('[data-id="' + id + '"]');
      if (!node) return;
      // Move the focus to the row: the next Tab then goes on from there, as
      // showRow does.
      node.focus({ preventScroll: true });
      if (node.scrollIntoView) node.scrollIntoView({ block: 'center' });
    }

    // reviewDone marks a long-run task looked at: its next look is a week on.
    function reviewDone(t) {
      state.reviewing[t.id] = true; // list.js draws the button disabled from this
      render();
      request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/review', {})
        .then(function (data) {
          var on = data.task && data.task.review_on;
          delete state.reviewing[t.id];
          return load().then(function () { flash('見直した。次に見る日は ' + monthDay(on)); });
        })
        .catch(function (err) { delete state.reviewing[t.id]; render(); reloadShowing(err); });
    }

    function removeTask(t, reason) {
      return request('DELETE', '/api/tasks/' + encodeURIComponent(t.id), reason ? { reason: reason } : null)
        .then(function () {
          if (state.openId === t.id) state.openId = null;
          return load().then(function () { flash('削除した: ' + t.name); });
        });
    }

    // openDropAsk hangs a reason field off the trash icon, the way the due menu
    // hangs off its chip. Enter deletes once something is written; Escape or a
    // click elsewhere leaves the task where it is.
    function openDropAsk(btn, t) {
      closeDueMenu();
      closeDropAsk();

      var box = document.createElement('div');
      box.className = 'drop-ask';
      box.setAttribute('role', 'dialog');
      box.setAttribute('aria-label', t.name + ' をやめる理由');

      var label = document.createElement('div');
      label.className = 'dm-label';
      label.textContent = 'やめる理由（作ってから1時間以上たっている）';
      box.appendChild(label);

      var input = document.createElement('input');
      input.type = 'text';
      input.className = 'drop-ask-input';
      input.placeholder = 'なぜやめるか';
      input.setAttribute('aria-label', 'やめる理由');
      box.appendChild(input);

      var go = document.createElement('button');
      go.type = 'button';
      go.className = 'drop-ask-go';
      go.textContent = '削除する';
      box.appendChild(go);

      // While the DELETE is out the field stays: closing it would let the
      // trash be pressed again and send a second DELETE behind an undo.
      var sending = false;
      function mine() { return dropAsk && dropAsk.node === box; }
      function update() { go.disabled = sending || !input.value.trim(); }
      function submit() {
        var reason = input.value.trim();
        if (!reason || sending) { input.focus(); return; }
        sending = true;
        input.disabled = true;
        update();
        removeTask(t, reason)
          .then(function () { if (mine()) closeDropAsk(); })
          .catch(function (err) {
            sending = false;
            if (mine()) { input.disabled = false; update(); input.focus(); }
            showError(err);
          });
      }

      input.addEventListener('input', update);
      input.addEventListener('keydown', function (ev) {
        if (composing(ev)) return;
        if (ev.key === 'Enter') { ev.preventDefault(); submit(); }
      });
      // On the box, not the input, so Escape also works from the button.
      box.addEventListener('keydown', function (ev) {
        if (composing(ev) || ev.key !== 'Escape' || sending) return;
        ev.preventDefault();
        ev.stopPropagation();
        closeDropAsk();
        btn.focus();
      });
      go.addEventListener('click', submit);
      update();

      function outside(ev) {
        if (!sending && !box.contains(ev.target)) closeDropAsk();
      }

      root.appendChild(box);
      var r = btn.getBoundingClientRect();
      var left = Math.max(8, Math.min(r.right - box.offsetWidth, window.innerWidth - box.offsetWidth - 8));
      box.style.left = (left + window.scrollX) + 'px';
      box.style.top = (r.bottom + 4 + window.scrollY) + 'px';

      // Hold the poll for the same reason as the due menu: a redraw would
      // replace the row, and the icon, under the field.
      state.busy = true;
      document.addEventListener('mousedown', outside, true);
      dropAsk = { node: box, outside: outside };
      input.focus();
    }

    function closeDropAsk() {
      if (!dropAsk) return;
      document.removeEventListener('mousedown', dropAsk.outside, true);
      dropAsk.node.remove();
      dropAsk = null;
      state.busy = false;
    }

    // addChild opens the add dialog with the parent filled in.
    function addChild(t) {
      window.workbench.openAddDialog('/add?parent=' + encodeURIComponent(t.id), 'タスクを追加', function (changed) {
        if (changed) load();
      });
    }

    // showDetail fills the box beside the list (detail.js draws it). It reads;
    // every change is still made in the row, so the box can disappear at narrow
    // widths without taking any operation with it. The box keeps the last task
    // it was given: letting it empty when the pointer leaves the list would
    // blank it mid-read, which is when it is most in use.
    function showDetail(t) {
      if (!el.detail) return;
      // Typing in the box's topic field: another row under the pointer must not
      // swap the box (and the field) away mid-edit.
      if (el.detail.contains(document.activeElement) && document.activeElement.tagName === 'INPUT') return;
      state.shownId = t.id;
      detail.renderSide(el.detail, t, state.board ? state.board.revision : 0, ctx);
    }

    function isOpen(id) { return state.openId === id; }

    // closeRow folds a row whose task changed state. The task moves to another
    // section, often a folded one, where an open panel would wait unseen.
    function closeRow(id) { if (state.openId === id) state.openId = null; }

    // landed tells where a resumed task went. It goes to the end of the queue,
    // which may be below the fold or in the closed スコープ外 section, so the
    // row seemed to vanish. The notice names the place, and 見る opens the
    // section and scrolls to the row. Nothing moves until 見る is pressed: the
    // waiting list is often worked through top to bottom.
    function landed(t) {
      var now = ((state.board && state.board.queue) || []).filter(function (q) { return q.id === t.id; })[0];
      // A task focused when it went to 待ち keeps the mark and returns to the
      // end of the band, whatever its due date.
      var where = !now ? '着手順'
        : now.focused ? 'フォーカスの末尾'
          : now.in_scope ? '着手順の末尾'
          : 'スコープ外（期限 ' + monthDay(now.due) + '）';
      flash('「' + t.name + '」を' + where + 'へ戻した', true, now && { label: '見る', run: function () { showRow(t.id); } });
      if (now) markLanded(t.id);
    }

    function showRow(id) {
      state.revealId = id;
      reveal(state.board);
      render();
      var node = document.querySelector('.task[data-id="' + id + '"]');
      if (!node) return;
      // Focus follows the eye, so the keyboard can act on the row right away.
      if (node.tabIndex >= 0) node.focus({ preventScroll: true });
      var still = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      node.scrollIntoView({ block: 'center', behavior: still ? 'auto' : 'smooth' });
      markLanded(id);
    }

    // markLanded tints the row for a moment. The id lives in state and the row
    // is drawn from it, so a redraw in the meantime (the poll) keeps the tint.
    function markLanded(id) {
      state.landedId = id;
      render();
      window.clearTimeout(landedTimer);
      landedTimer = window.setTimeout(function () {
        state.landedId = null;
        render();
      }, LANDED_MS);
    }

    // ---- interaction ----

    document.addEventListener('click', function (ev) {
      if (!active) return;
      if (ev.target.closest('a.ticket')) return; // let the link do its job
      if (ev.target.closest('.due-menu')) return; // the menu handles its own clicks
      if (ev.target.closest('.drop-ask')) return; // so does the reason field

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

    // onCheck ticks a task done, or puts a done one back when it is unticked: a
    // mis-click has a way out. The box belongs to its row; it is only held shut
    // while the request is out.
    function onCheck(box, t) {
      var was = box.checked;
      var path = was ? '/done' : '/reopen';
      box.disabled = true;
      request('POST', '/api/tasks/' + encodeURIComponent(t.id) + path)
        .then(function () { closeRow(t.id); return load(); })
        .catch(function (err) {
          box.checked = !was;
          box.disabled = false;
          showError(err);
        });
    }

    // ---- due menu ----
    //
    // The due chip opens one menu for everything that moves a task later: a
    // date, a week or a month on, no date, or off the queue to someday. They
    // used to live in three places (the chip, an emptied date field, the panel).

    var dueMenu = null; // { chip, node }
    var dropAsk = null; // { node, outside }

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
      if (chip.classList.contains('review-chip')) { openReviewMenu(chip); return; }
      var id = chip.closest('.task').dataset.id;
      var current = chip.dataset.due || '';
      var path = '/api/tasks/' + encodeURIComponent(id);

      function setDue(value, said) {
        closeDueMenu();
        request('PATCH', path, { due: value })
          .then(function () { return load().then(function () { flash(said); }); })
          .catch(reloadShowing);
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
      // A long-run task is not shelved: it is already out of the queue.
      if (!chip.closest('.long-list')) {
        sec.appendChild(dueMenuItem('いつかにする', shortDate(addMonths(shiftBase(''), 1)) + ' に見直す', 'dm-someday',
          function () {
            closeDueMenu();
            request('POST', path + '/someday', {})
              .then(function () { closeRow(id); return load().then(function () { flash('いつかへ棚上げした'); }); })
              .catch(reloadShowing);
          }));
      }
      if (sec.childNodes.length) menu.appendChild(sec);

      showMenu(chip, menu, input);
    }

    // openReviewMenu is the due menu for a long-run row's review day: a date, or
    // a step ahead of today. It posts to /review, which also marks it as looked at.
    function openReviewMenu(chip) {
      var id = chip.closest('.task').dataset.id;
      var current = chip.dataset.due || '';

      function setReview(value) {
        closeDueMenu();
        request('POST', '/api/tasks/' + encodeURIComponent(id) + '/review', { review_on: value })
          .then(function () { return load().then(function () { flash('次に見る日を ' + monthDay(value) + ' にした'); }); })
          .catch(reloadShowing);
      }

      var menu = document.createElement('div');
      menu.className = 'due-menu';
      menu.setAttribute('role', 'menu');
      menu.setAttribute('aria-label', '次に見る日');

      var sec = document.createElement('div');
      sec.className = 'dm-sec';
      sec.appendChild(dueMenuLabel('次に見る日'));
      var input = document.createElement('input');
      input.type = 'date';
      input.className = 'due-input';
      input.value = current;
      input.setAttribute('aria-label', '次に見る日の日付');
      input.addEventListener('change', function () {
        if (input.value) setReview(input.value);
      });
      sec.appendChild(input);
      menu.appendChild(sec);

      var base = new Date(((state.board && state.board.today) || ymd(new Date())) + 'T00:00:00');
      sec = document.createElement('div');
      sec.className = 'dm-sec';
      sec.appendChild(dueMenuLabel('今日（' + shortDate(base) + '）から'));
      [['+1週間', addDays(base, 7)], ['+2週間', addDays(base, 14)], ['+1ヶ月', addMonths(base, 1)]]
        .forEach(function (o) {
          sec.appendChild(dueMenuItem(o[0], shortDate(o[1]), '', function () { setReview(ymd(o[1])); }));
        });
      menu.appendChild(sec);

      showMenu(chip, menu, input);
    }

    // showMenu keys, places and registers a menu hung off a chip.
    function showMenu(chip, menu, input) {
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

      state.busy = true; // hold the poll while the menu is open
      dueMenu = { chip: chip, node: menu };
      state.menuId = chip.closest('.task').dataset.id; // the chip's aria-expanded, drawn by list.js
      render();
      var first = menu.querySelector('.dm-item');
      if (first) first.focus();
    }

    function closeDueMenu() {
      if (!dueMenu) return;
      dueMenu.node.remove();
      dueMenu = null;
      state.menuId = null;
      state.busy = false;
      render();
    }

    window.addEventListener('resize', closeDueMenu);
    window.addEventListener('resize', closeDropAsk);

    // "タスクを追加" and N open the add page (/add) in a dialog over the list:
    // the same form, due chips and detail as a bare `todo` opens. The list is
    // reloaded after it closes if a task went in.
    function openAdd() {
      window.workbench.openAddDialog('/add', 'タスクを追加', function (changed) {
        if (changed) load();
      });
    }

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

    // The folds, the topic grouping and the scope are buttons in the header and
    // the section heads (list.js); they land here.
    var FOLDS = {
      done: ['doneOpen', DONE_KEY],
      drop: ['dropOpen', DROP_KEY],
      out: ['outOpen', OUT_KEY],
      wait: ['waitOpen', WAIT_KEY],
      someday: ['somedayOpen', SOMEDAY_KEY],
      long: ['longOpen', LONG_KEY],
    };

    function toggleFold(name) {
      var f = FOLDS[name];
      state[f[0]] = !state[f[0]];
      try { localStorage.setItem(f[1], state[f[0]] ? '1' : '0'); } catch (e) { /* private window */ }
      render();
    }

    function toggleGroup() {
      state.groupByTopic = !state.groupByTopic;
      try { localStorage.setItem(GROUP_KEY, state.groupByTopic ? '1' : '0'); } catch (e) { /* private window */ }
      render();
    }

    function pickScope(v) {
      state.scopeKey = v;
      if (v === 'custom') {
        state.scope = String(daysUntil(el.until.value));
      } else {
        state.scope = v;
        try { localStorage.setItem(SCOPE_KEY, v); } catch (e) { /* private window */ }
      }
      render();
      load();
    }

    function pickUntil() {
      state.scope = String(daysUntil(el.until.value));
      load();
    }

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
    var dragRestore = null; // puts the dragged row's element back where Preact left it
    var dropped = false; // a drag ended by Escape or outside both lists saves nothing

    function onDragStart(ev) {
      var li = ev.target.closest('.task');
      if (!li) return;
      state.busy = true;
      dragged = li;
      dropped = false;
      // The preview below moves the row's own element, under Preact. render()
      // puts it back first, so the draw that follows (the reply, or the cancel)
      // finds the DOM it expects and moves the row to where it now belongs.
      var home = li.parentNode;
      var before = li.nextSibling;
      dragRestore = function () {
        if (li.parentNode !== home || li.nextSibling !== before) home.insertBefore(li, before);
      };
      li.classList.add('dragging');
      el.focusBand.classList.add('drop-ready');
      ev.dataTransfer.effectAllowed = 'move';
      ev.dataTransfer.setData('text/plain', li.dataset.id);
    }

    function onDragOver(listId) {
      return function (ev) {
        if (!dragged) return;
        var list = document.getElementById(listId);
        // Grouped: nothing can land in the queue. No preventDefault, so the
        // browser shows the drop as not allowed and the drag ends as a cancel.
        if (state.groupByTopic && listId === 'queue') return;
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

    // scopeDue is the due date a row takes when it is dragged across the
    // scope's edge, so that it stays where it was put: the last day in scope
    // when it comes into the queue, the first day past it when it goes out.
    // Null when the drop does not cross the edge.
    function scopeDue(task, list) {
      var b = state.board;
      if (list === el.outList && task.in_scope) return shiftDay(b.today, b.scope_days + 1);
      if (list === el.queue && !task.in_scope) return shiftDay(b.today, b.scope_days);
      return null;
    }

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
      var list = li.parentNode;
      var inBand = list === el.focusList;
      var task = (state.board.queue || []).filter(function (t) { return t.id === id; })[0];
      if (!task) { state.busy = false; load(); return; }
      // focused is omitted from the JSON when false, so compare a boolean:
      // undefined !== false would send every reorder to /focus as a no-op.
      var crossed = inBand !== Boolean(task.focused);

      // The place is "just after the row above it", read against the whole
      // queue without the moved task. Rows hidden under スコープ外 are not on
      // screen, so counting rows on screen would land somewhere else.
      var rest = (state.board.queue || []).filter(function (t) { return t.id !== id; });
      var ids = rest.map(function (t) { return t.id; });
      var prev = li.previousElementSibling;
      var next = li.nextElementSibling;
      var to;
      if (prev && prev.dataset.id) {
        to = ids.indexOf(prev.dataset.id) + 1;
      } else if (list === el.outList && next && next.dataset.id) {
        // The top of スコープ外 goes just before its first row. In-scope rows
        // can sit between out-of-scope ones in the queue, so "after the last
        // in-scope row" could be behind it.
        to = ids.indexOf(next.dataset.id);
      } else if (list === el.outList && el.queue.lastElementChild) {
        to = ids.indexOf(el.queue.lastElementChild.dataset.id) + 1;
      } else {
        to = inBand ? 0 : rest.filter(function (t) { return t.focused; }).length;
      }
      var due = scopeDue(task, list);
      // スコープ外 is shown by due date, so a row moved within it would only
      // jump back. Nothing is sent; the drop just puts it back.
      if (!crossed && !due && list === el.outList) {
        state.busy = false;
        render();
        return;
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
          scope_days: state.board.scope_days,
          revision: state.board.revision,
        });
      // The due date follows the move, after it: the move carries the revision
      // the place was read at, and changing the due first would spend it.
      if (due) {
        req = req.then(function () {
          return request('PATCH', '/api/tasks/' + encodeURIComponent(id), { due: due });
        });
      }
      req
        .then(function () {
          state.busy = false;
          // Reordering within a section keeps the row open; moving it across one
          // changes its state, as the buttons do.
          if (crossed || due) closeRow(id);
          return load().then(function () {
            if (due) flash('期限を ' + monthDay(due) + ' にした');
            else if (crossed) flash(inBand ? 'フォーカスに入れた' : 'フォーカスから外した');
          });
        })
        .catch(function (err) {
          state.busy = false;
          reloadShowing(err);
        });
    }

    // ---- boot ----

    // A link can name the task to open: http://127.0.0.1:8085/#T007
    openFromHash(location.hash);

    try {
      state.doneOpen = localStorage.getItem(DONE_KEY) === '1';
      state.dropOpen = localStorage.getItem(DROP_KEY) === '1';
      state.somedayOpen = localStorage.getItem(SOMEDAY_KEY) === '1';
      state.outOpen = localStorage.getItem(OUT_KEY) === '1';
      state.waitOpen = localStorage.getItem(WAIT_KEY) === '1';
      state.longOpen = localStorage.getItem(LONG_KEY) === '1';
      state.groupByTopic = localStorage.getItem(GROUP_KEY) === '1';
    } catch (e) { /* storage unavailable; closed is the default */ }

    try {
      var saved = localStorage.getItem(SCOPE_KEY);
      // 7 and 14 were days from today; the same buttons are now calendar weeks.
      saved = { '7': 'week', '14': '2weeks' }[saved] || saved;
      if (saved) {
        state.scope = saved;
        state.scopeKey = saved;
      }
    } catch (e) { /* storage unavailable; the default scope still applies */ }

    // What list.js calls back into: the parts of the list that stay here.
    var ctx = {
      state: state,
      showDetail: showDetail,
      // What the open row's name and panel (detail.js) call back into.
      load: load,
      flash: flash,
      closeRow: closeRow,
      landed: landed,
      reloadShowing: reloadShowing,
      // A field that goes while it has the focus gets no blur: release the poll.
      releaseBusy: function () { state.busy = false; },
      addChild: addChild,
      removeTask: removeTask,
      openDropAsk: openDropAsk,
      showError: showError,
      reviewDone: reviewDone,
      openLongTask: openLongTask,
      toggleFold: toggleFold,
      toggleGroup: toggleGroup,
      pickScope: pickScope,
      pickUntil: pickUntil,
      openAdd: openAdd,
      onCheck: onCheck,
      drag: { start: onDragStart, over: onDragOver, drop: onDrop, end: onDragEnd },
    };

    render(); // the empty frame, until the first reply
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
      closeDropAsk();
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
