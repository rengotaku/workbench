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
    var wirePaste = shared.wirePaste;
    // The list's drawing and its date helpers are list.js's (loaded before this).
    var list = window.workbench.todoList;
    var monthDay = list.monthDay;
    var shiftDay = list.shiftDay;
    var linkRange = list.linkRange;

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

    // The detail box keeps the last task it was given. Letting it empty when the
    // pointer leaves the list would blank it mid-read, which is when it is most
    // in use.
    var trailCache = {};

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
      if (still) { trailCache[still.id] = null; delete trailCache[still.id]; showDetail(still); return; }
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

    // boardTasks lists every task the board carries, whatever its section.
    function boardTasks() {
      var b = state.board || {};
      return (b.queue || []).concat(b.long || [], b.waiting || [], b.someday || [], b.done || []);
    }

    // longTasks are the long-run tasks, the parents that sit above the queue.
    function longTasks() {
      return (state.board && state.board.long) || [];
    }

    // treeFacts reads the parent links off the board: each task's children, and
    // how many levels the tree under a task has (the task itself counts).
    function treeFacts() {
      var kids = {};
      boardTasks().forEach(function (x) {
        if (x.parent) (kids[x.parent] = kids[x.parent] || []).push(x.id);
      });
      function height(id, hops) {
        var deepest = 0;
        if (hops < 12) (kids[id] || []).forEach(function (c) { deepest = Math.max(deepest, height(c, hops + 1)); });
        return deepest + 1;
      }
      return { kids: kids, height: function (id) { return height(id, 0); } };
    }

    // MAX_DEPTH is how many levels a tree may have, as in the server (ADR todo-0020).
    var MAX_DEPTH = 3;

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

    // addChildButton is the panel's button for it. A task already on the last
    // level offers none.
    function addChildButton(t) {
      var b = document.createElement('button');
      b.type = 'button';
      b.textContent = '子を追加';
      b.title = t.id + ' の子としてタスクを追加する';
      b.addEventListener('click', function (ev) {
        ev.stopPropagation(); // the row underneath must not open or close
        addChild(t);
      });
      return b;
    }

    // parentPicker ties a task to a parent, or unties it. It lists the long-run
    // tasks and the queue, leaving out the task itself, what hangs under it and
    // anything that would push the tree past MAX_DEPTH. The server checks the
    // same rules (and the due dates, which the list cannot judge).
    function parentPicker(t) {
      var facts = treeFacts();
      var height = facts.height(t.id);
      var below = {};
      (function mark(id, hops) {
        if (hops > 12) return;
        (facts.kids[id] || []).forEach(function (c) { below[c] = true; mark(c, hops + 1); });
      })(t.id, 0);
      function offered(p) {
        return p.id !== t.id && !below[p.id] && (p.parent_path || []).length + 1 + height <= MAX_DEPTH;
      }

      var label = document.createElement('label');
      label.className = 'parent-pick';
      label.appendChild(document.createTextNode('親'));
      var sel = document.createElement('select');
      var none = document.createElement('option');
      none.value = '';
      none.textContent = 'なし';
      sel.appendChild(none);
      var listed = {};
      function group(title, tasks) {
        var rows = tasks.filter(offered);
        if (!rows.length) return;
        var og = document.createElement('optgroup');
        og.label = title;
        rows.forEach(function (p) {
          var o = document.createElement('option');
          o.value = p.id;
          o.textContent = p.id + ' ' + p.name;
          if (p.id === t.parent) o.selected = true;
          listed[p.id] = true;
          og.appendChild(o);
        });
        sel.appendChild(og);
      }
      group('長期', longTasks());
      group('着手順', (state.board && state.board.queue) || []);
      if (t.parent && !listed[t.parent]) {
        // The parent is somewhere the list does not offer (waiting, someday,
        // finished): show the link as it is, so the picker tells the truth and
        // なし takes it off.
        var kept = document.createElement('option');
        kept.value = t.parent;
        kept.textContent = t.parent + (t.parent_name ? ' ' + t.parent_name : '');
        kept.selected = true;
        sel.appendChild(kept);
      }
      sel.addEventListener('change', function () {
        sel.disabled = true;
        request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), { parent: sel.value })
          .then(function () {
            return load().then(function () { flash(sel.value ? sel.value + ' の子にした' : '親を外した'); });
          })
          .catch(reloadShowing);
      });
      label.appendChild(sel);
      return label;
    }

    // longButton moves a task into the long-run band or back to the queue.
    function longButton(t) {
      var on = t.state !== 'long';
      var b = document.createElement('button');
      b.type = 'button';
      b.className = on ? '' : 'long-on';
      b.textContent = on ? '長期にする' : '長期を外す';
      b.title = on
        ? '着手順から外し、長期の枠に置く（フォーカスも外れる）'
        : '着手順の先頭（フォーカスのすぐ下）へ戻す';
      b.addEventListener('click', function () {
        b.disabled = true;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/long', { on: on })
          .then(function () {
            closeRow(t.id);
            return load().then(function () { flash(on ? '長期にした' : '長期から外した'); });
          })
          .catch(function (err) { b.disabled = false; showError(err); });
      });
      return b;
    }

    // showDetail fills the box beside the list. It reads; every change is still
    // made in the row, so the box can disappear at narrow widths without taking
    // any operation with it.
    function showDetail(t) {
      if (!el.detail) return;
      // Typing in the box's topic field: another row under the pointer must not
      // swap the box (and the field) away mid-edit.
      if (el.detail.contains(document.activeElement) && document.activeElement.tagName === 'INPUT') return;
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

      el.detail.appendChild(topicField(t, 'd-topic-' + t.id));

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

    // What a person writes as status (memo, or progress / blocked from the CLI)
    // carries no tag: telling those apart was more work than it was worth. A
    // quote or an analysis keeps its tag, and anything AI wrote is labelled AI's.
    var ANNOTATION_LABEL = { progress: '進んだ', blocked: '詰まり', memo: 'メモ', quote: '引用', analysis: '考察' };
    var UNTAGGED_KINDS = { memo: true, progress: true, blocked: true };

    function annotationLabel(a) {
      if (a.author !== 'ai' && UNTAGGED_KINDS[a.kind]) return '';
      var label = ANNOTATION_LABEL[a.kind] || a.kind;
      return a.author === 'ai' ? 'AI ' + label : label;
    }

    // annotationTag returns an empty span for an untagged supplement, so the
    // columns of the status list stay in place.
    function annotationTag(a) {
      var tag = document.createElement('span');
      var label = annotationLabel(a);
      tag.className = label ? 'ann-tag k-' + String(a.kind).replace(/[^a-z]/g, '') : 'ann-tag-none';
      tag.textContent = label;
      return tag;
    }

    // topicField is the one-line input for a task's topic, in the open row and
    // in the box beside the list. The value is saved when typing stops or the
    // field is left, like the name and detail; "" clears it. Suggestions come
    // from the datalist of topics in use.
    function topicField(t, id) {
      var line = document.createElement('div');
      line.className = 'topic-line';
      var label = document.createElement('label');
      label.className = 'topic-label';
      label.htmlFor = id;
      label.textContent = '話題';
      var input = document.createElement('input');
      input.type = 'text';
      input.id = id;
      input.className = 'topic-input';
      input.autocomplete = 'off';
      input.spellcheck = false;
      input.maxLength = 20; // task.MaxTopicLen; the server checks it again
      input.setAttribute('list', 'todo-topics');
      input.placeholder = 'なし';
      input.value = t.topic || '';

      var last = t.topic || '';
      var timer = null;
      // One PATCH at a time: a later save waits for the one in flight and then
      // sends whatever the field holds, so an older value can never land last.
      var saving = false;
      var again = false;
      var reloadAfter = false;
      function save(reload) {
        if (reload) reloadAfter = true;
        if (saving) { again = true; return; }
        var value = input.value.trim();
        if (value === last) { finish(); return; }
        saving = true;
        var inRow = !!input.closest('.panel'); // the box beside the list has no save slot
        if (inRow) mark(input, '保存中…');
        request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), { topic: value })
          .then(function () {
            last = value;
            t.topic = value;
            if (inRow) mark(input, '保存した', true);
          })
          // Keep what was typed: it may already be newer than the failed value.
          .catch(function (err) { showError(err); })
          .then(function () {
            saving = false;
            if (again) { again = false; save(false); return; }
            finish();
          });
      }
      function finish() {
        if (!reloadAfter || saving) return;
        reloadAfter = false;
        if (!state.busy) load();
      }
      input.addEventListener('focus', function () { state.busy = true; });
      input.addEventListener('input', function () {
        window.clearTimeout(timer);
        timer = window.setTimeout(function () { save(false); }, 900);
      });
      input.addEventListener('blur', function () {
        window.clearTimeout(timer);
        state.busy = false;
        save(true);
      });
      input.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
        if (e.key === 'Escape') { e.stopPropagation(); input.blur(); }
      });
      line.appendChild(label);
      line.appendChild(input);
      return line;
    }

    // statusBlock is the field that appends one line of status to the open row.
    // A supplement is append-only, so this only ever adds; a failed send keeps
    // what was typed.
    function statusBlock(t) {
      var wrap = document.createElement('div');
      wrap.className = 'status-add';

      var line = document.createElement('div');
      line.className = 'status-line';
      var label = document.createElement('label');
      label.className = 'sr-only';
      var inputId = 'status-' + t.id;
      label.htmlFor = inputId;
      label.textContent = 'いまの状況';
      var input = document.createElement('input');
      input.type = 'text';
      input.id = inputId;
      input.className = 'status-input';
      input.autocomplete = 'off';
      input.placeholder = 'いまの状況を 1 行で（Enter で追記）';
      // The draft lives in state, not in the field: a redraw (the poll, a
      // reload after sending) builds a new field and must not lose what was typed.
      input.value = state.statusDrafts[t.id] || '';
      input.addEventListener('input', function () { state.statusDrafts[t.id] = input.value; });
      var send = document.createElement('button');
      send.type = 'button';
      send.className = 'status-send';
      send.textContent = '追記';
      line.appendChild(label);
      line.appendChild(input);
      line.appendChild(send);
      wrap.appendChild(line);

      var sending = false;
      function submit() {
        var body = input.value.trim();
        if (!body || sending) return;
        sending = true;
        send.disabled = true;
        request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/annotations',
          { kind: 'memo', body: body, actor: 'human' })
          .then(function () {
            // Clear only what was sent: anything typed while waiting stays.
            var draft = state.statusDrafts[t.id] || '';
            if (draft.trim() === body) delete state.statusDrafts[t.id];
            if (input.value.trim() === body) input.value = '';
            var refocus = document.activeElement === input || document.activeElement === send
              || document.activeElement === document.body;
            // The redraw rebuilds the row, so the field is a new one: focus it
            // again once the list is back, unless the user has moved on.
            state.busy = false;
            return load().then(function () {
              var again = document.getElementById('status-' + t.id);
              if (again && refocus) again.focus();
            });
          }, function (err) {
            showError(err);
          })
          .then(function () {
            // load() reports its own failure and does not redraw then, so the
            // guard is released here whether or not the list came back.
            sending = false;
            send.disabled = false;
          });
      }
      send.addEventListener('click', submit);
      input.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); submit(); }
        // Like the live fields: the first Escape leaves the field.
        if (e.key === 'Escape') { e.stopPropagation(); input.blur(); }
      });
      // Typing here holds the poll, as the name and detail do.
      input.addEventListener('focus', function () { state.busy = true; });
      input.addEventListener('blur', function () { state.busy = false; });
      return wrap;
    }

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

    // linkSaves keeps one save queue per task, outside the rows: a row closed
    // and opened again while a save is out gets a new editor, and two editors
    // must not send two whole-list PATCHes that could land in either order.
    //   busy  a PATCH is out
    //   next  the save to run once it answers (the newest editor's)
    //   last  the list the server last confirmed, as JSON
    var linkSaves = {};

    // linksBlock is the open row's related URLs, edited in place like the name:
    // adding (or pasting) a URL, editing a row and removing one save at once.
    // Numbers follow the rows' order, and a row the name still points at (or
    // one before it) cannot be removed, so a [N] never moves to another URL.
    function linksBlock(t) {
      function nameNow() {
        var n = document.querySelector('.task[data-id="' + t.id + '"] .txt.live');
        return n ? n.textContent : t.name;
      }
      var q = linkSaves[t.id] || (linkSaves[t.id] = { busy: false, next: null, last: '' });
      // With no save out, the list just read is what the server holds (it may
      // have moved through the CLI meanwhile).
      if (!q.busy) q.last = JSON.stringify((t.links || []).map(function (l) { return { url: l.url, label: l.label || '' }; }));
      var ed = shared.linkEditor({
        // A row the name points at cannot go. Both the name being typed and the
        // saved one count: the typed one may yet fail to save and come back.
        text: function () { return nameNow() + ' ' + t.name; },
        onError: function (msg) { showError(new Error(msg)); },
        onChange: save,
        onEnter: function () { if (document.activeElement) document.activeElement.blur(); },
      });
      // One save at a time per task (linkSaves): each PATCH replaces the whole
      // list, so two in flight could land in either order and the older list
      // would win. A save asked for meanwhile runs once the one out has
      // answered, and compares with what the server last confirmed, so
      // changing a row and changing it back while a save is out still lands.
      function save() {
        if (q.busy) { q.next = save; return; }
        var links = ed.read(true);
        if (!links) return; // an empty row: saved once it has a URL
        var now = JSON.stringify(links);
        if (now === q.last) return;
        q.busy = true;
        mark(ed.el, '保存中…');
        request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), { links: links })
          .then(function (res) {
            q.last = now;
            t.links = (res && res.task && res.task.links) || links.map(function (l, i) { return { n: i + 1, url: l.url, label: l.label }; });
            mark(ed.el, '保存した', true);
          })
          .catch(function (err) { showError(err); })
          .then(function () {
            q.busy = false;
            var next = q.next;
            q.next = null;
            if (next) next();
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
        h.textContent = '状況（新しい順）';
        sec.appendChild(h);
      }
      anns.slice().reverse().forEach(function (a) {
        var item = document.createElement('div');
        item.className = 'd-ann-item k-' + String(a.kind).replace(/[^a-z]/g, '');
        var when = document.createElement('time');
        when.className = 'd-ann-when';
        var d = new Date(a.created_at);
        when.textContent = (d.getMonth() + 1) + '/' + d.getDate() + ' '
          + ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
        var body = document.createElement('div');
        body.className = 'd-ann-body' + (a.author === 'ai' ? ' ai' : '');
        body.textContent = a.body + (a.source ? ' · ' + a.source : '');
        item.appendChild(when);
        item.appendChild(annotationTag(a));
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

    // panelFor fills the open row's panel with what I wrote above what happened.
    // The panel element is the host list.js leaves to this code: Preact never
    // touches what is inside it. The trail is fetched per task, so opening one
    // row does not pull every task's history.
    function panelFor(t, panel) {
      // The detail is edited where it is read: no label, no box, no save button.
      var detail = document.createElement('div');
      detail.className = 'live detail';
      detail.contentEditable = 'true';
      detail.spellcheck = false;
      detail.dataset.placeholder = '詳細を書く（任意）';
      detail.textContent = t.detail || '';
      wireLiveField(detail, t, 'detail', { single: false });
      panel.appendChild(detail);

      panel.appendChild(topicField(t, 'topic-' + t.id));

      panel.appendChild(statusBlock(t));

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
              closeRow(t.id);
              return load().then(function () {
                flash(was ? 'フォーカスから外した' : 'フォーカスに入れた');
              });
            })
            .catch(function (err) { focusBtn.disabled = false; showError(err); });
        });
        actions.appendChild(focusBtn);
        // A child is not a long-run task itself (one level only).
        if (!t.parent) actions.appendChild(longButton(t));
      }
      if (t.state === 'long') actions.appendChild(longButton(t));
      // Any task that is not done can have children, down to MAX_DEPTH levels.
      if (t.state !== 'done' && (t.parent_path || []).length + 1 < MAX_DEPTH) actions.appendChild(addChildButton(t));
      // A long-run task is the top of a tree, so it has no parent to choose.
      if (t.state !== 'long' && t.state !== 'done') actions.appendChild(parentPicker(t));

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
            .then(function () { closeRow(t.id); return load().then(function () { landed(t); }); })
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
          .then(function () { closeRow(t.id); return load().then(function () { flash('いつかへ棚上げした'); }); })
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
            .then(function () { closeRow(t.id); return load().then(function () { flash('待ちへ移した'); }); })
            .catch(function (err) { go.disabled = false; showError(err); });
        }
        go.addEventListener('click', submit);
        who.addEventListener('keydown', function (e) {
          if (composing(e)) return;
          if (e.key === 'Enter') { e.preventDefault(); submit(); }
          if (e.key === 'Escape') { span.textContent = ''; span.appendChild(ask); }
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
      longed: '長期にした',
      unlonged: '長期から外した',
      parent_set: '親を変更',
      review_set: '見直し',
      topic_set: '話題',
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
        if (!save || !input.value.trim()) { input.replaceWith(anchor); return; }
        request('POST', '/api/events/' + encodeURIComponent(e.id) + '/why',
          { reason: input.value.trim() })
          .then(function () { return load().then(function () { flash('理由を記録した'); }); })
          .catch(function (err) { showError(err); if (input.isConnected) input.replaceWith(anchor); });
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
        case 'parent_set':
        case 'review_set':
        case 'topic_set':
          return ' ' + (e.from_value || 'なし') + ' → ' + (e.to_value || 'なし');
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
      // The open row's name field and its panel are filled and wired here.
      mountName: function (txt, t) {
        txt.textContent = t.name;
        wireLiveField(txt, t, 'name', { single: true });
      },
      mountPanel: function (host, t) { panelFor(t, host); },
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
