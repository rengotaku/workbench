// 一覧に重なって出るもの（期限のメニュー・見直しのメニュー・削除の理由欄・通知）を Preact で描く。
// ADR todo-0025。
//
//   DueMenu    期限の札を押すと出るメニュー（日付・ずらす・期限なし・いつかにする）
//   ReviewMenu 長期の行の「次に見る日」のメニュー
//   DropAsk    ごみ箱の横に出る「やめる理由」の欄
//   Flash      したことの通知と、元に戻す・見るの導線（失敗の表示も同じ場所）
//
// 何を出すかは app.js の state（state.menu・state.dropAsk・state.flash）が決める。
// ここは state から描くだけで、開閉・API 呼び出し・読み直しは ctx 経由で app.js に頼む。
// メニューと理由欄は行の外（.menus）に描く。行は読み直しのたびに描き直されるので、
// 行の中に置くと消える。位置は、押した札・ボタンの位置を測って決める。
//
// 理由欄の入力は書き換えない要素（uncontrolled）にして、描き直しをまたいで同じ要素が
// 残るようにする。打ち込み中の文字とフォーカスは、一覧の応答が来ても消えない。
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var useState = P.useState;
  var useRef = P.useRef;
  var useEffect = P.useEffect;
  var useLayoutEffect = P.useLayoutEffect;

  var shared = window.todoShared;
  var composing = shared.composing;
  var request = shared.request;
  var monthDay = window.workbench.todoList.monthDay;

  // ---- 日付（純粋な関数） ----

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

  // ---- 位置 ----

  // useAnchored hangs a box off an element: its right edge under the element's
  // right edge, kept inside the window. The box is measured once it is drawn
  // (its width is the CSS's), and placed before the next paint.
  function useAnchored(anchor) {
    var ref = useRef(null);
    var st = useState(null);
    useLayoutEffect(function () {
      var box = ref.current;
      var r = anchor.getBoundingClientRect();
      var left = Math.max(8, Math.min(r.right - box.offsetWidth, window.innerWidth - box.offsetWidth - 8));
      st[1]({ left: (left + window.scrollX) + 'px', top: (r.bottom + 4 + window.scrollY) + 'px' });
    }, []);
    return [ref, st[0] || undefined];
  }

  // ---- 期限と見直しのメニュー ----

  function Item(p) {
    return html`<button type="button" class=${'dm-item' + (p.cls ? ' ' + p.cls : '')} role="menuitem"
      onClick=${p.run}><span>${p.label}</span>${p.hint ? html`<span class="dm-hint">${p.hint}</span>` : null}</button>`;
  }

  // Menu is the box both menus share: the keys, the place, and the first
  // item taking the focus.
  //   Escape       closes and returns the focus to the chip
  //   ↑ ↓          move between the fields and items (not inside the date field,
  //                where the arrows step the date)
  //   Enter, Space the item's own click runs; the row must not toggle
  function Menu(p) {
    var ctx = p.ctx;
    var m = p.menu;
    var placed = useAnchored(m.chip);
    var ref = placed[0];
    // Focus waits for the place: focusing a box that is still at the foot of
    // the page would scroll the window to it.
    var placedAt = !!placed[1];
    useLayoutEffect(function () {
      var first = placedAt && ref.current.querySelector('.dm-item');
      if (first) first.focus();
    }, [placedAt]);

    function onKeyDown(e) {
      if (composing(e)) return;
      var box = ref.current;
      var items = Array.prototype.slice.call(box.querySelectorAll('input, button:not([disabled])'));
      var at = items.indexOf(document.activeElement);
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        ctx.closeMenu();
        m.chip.focus();
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        // Inside the date field the arrows step the date; leave them alone.
        if (document.activeElement === box.querySelector('input')) return;
        e.preventDefault();
        e.stopPropagation();
        var step = e.key === 'ArrowDown' ? 1 : -1;
        items[(at + step + items.length) % items.length].focus();
      } else if (e.key === 'Enter' || e.key === ' ') {
        e.stopPropagation();
      }
    }

    return html`<div ref=${ref} class="due-menu" role="menu" aria-label=${p.label} style=${placed[1]}
      onKeyDown=${onKeyDown}>${p.children}</div>`;
  }

  // DateField is the menu's date input. It keeps its own text (uncontrolled):
  // a half-typed date is not a value, and a draw must not wipe it.
  function DateField(p) {
    var ref = useRef(null);
    useLayoutEffect(function () { ref.current.value = p.value; }, []);
    return html`<input ref=${ref} type="date" class="due-input" aria-label=${p.label}
      onChange=${function (e) { p.onPick(e.target.value); }} />`;
  }

  function DueMenu(p) {
    var ctx = p.ctx;
    var m = p.menu;
    var id = m.taskId;
    var current = m.current;
    var path = '/api/tasks/' + encodeURIComponent(id);

    function setDue(value, said) {
      ctx.closeMenu();
      request('PATCH', path, { due: value })
        .then(function () { return ctx.load().then(function () { ctx.flash(said); }); })
        .catch(ctx.reloadShowing);
    }

    function shelve() {
      ctx.closeMenu();
      request('POST', path + '/someday', {})
        .then(function () { ctx.closeRow(id); return ctx.load().then(function () { ctx.flash('いつかへ棚上げした'); }); })
        .catch(ctx.reloadShowing);
    }

    var base = shiftBase(current);
    var steps = [['+1週間', addDays(base, 7)], ['+2週間', addDays(base, 14)], ['+1ヶ月', addMonths(base, 1)]];
    // A long-run task is not shelved: it is already out of the queue.
    var last = [
      current ? html`<${Item} key="clear" label="期限なし" cls="dm-clear" run=${function () { setDue('', '期限を外した'); }} />` : null,
      m.inLong ? null : html`<${Item} key="someday" label="いつかにする" cls="dm-someday"
        hint=${shortDate(addMonths(shiftBase(''), 1)) + ' に見直す'} run=${shelve} />`,
    ].filter(Boolean);

    return html`<${Menu} ctx=${ctx} menu=${m} label="期限">
      <div class="dm-sec">
        <div class="dm-label">期限</div>
        <${DateField} value=${current} label="期限の日付" onPick=${function (v) {
          if (!v || v === current) return;
          setDue(v, '期限を ' + shortDate(new Date(v + 'T00:00:00')) + ' にした');
        }} />
      </div>
      <div class="dm-sec">
        <div class="dm-label">${'後ろにずらす（' + shortDate(base) + ' から）'}</div>
        ${steps.map(function (o) {
          return html`<${Item} key=${o[0]} label=${o[0]} hint=${shortDate(o[1])}
            run=${function () { setDue(ymd(o[1]), '期限を ' + shortDate(o[1]) + ' にずらした'); }} />`;
        })}
      </div>
      ${last.length ? html`<div class="dm-sec">${last}</div>` : null}
    <//>`;
  }

  // ReviewMenu is the menu for a long-run row's review day: a date, or a step
  // ahead of today. It posts to /review, which also marks it as looked at.
  function ReviewMenu(p) {
    var ctx = p.ctx;
    var m = p.menu;
    var id = m.taskId;

    function setReview(value) {
      ctx.closeMenu();
      request('POST', '/api/tasks/' + encodeURIComponent(id) + '/review', { review_on: value })
        .then(function () { return ctx.load().then(function () { ctx.flash('次に見る日を ' + monthDay(value) + ' にした'); }); })
        .catch(ctx.reloadShowing);
    }

    var board = ctx.state.board;
    var base = new Date(((board && board.today) || ymd(new Date())) + 'T00:00:00');
    var steps = [['+1週間', addDays(base, 7)], ['+2週間', addDays(base, 14)], ['+1ヶ月', addMonths(base, 1)]];

    return html`<${Menu} ctx=${ctx} menu=${m} label="次に見る日">
      <div class="dm-sec">
        <div class="dm-label">次に見る日</div>
        <${DateField} value=${m.current} label="次に見る日の日付" onPick=${function (v) { if (v) setReview(v); }} />
      </div>
      <div class="dm-sec">
        <div class="dm-label">${'今日（' + shortDate(base) + '）から'}</div>
        ${steps.map(function (o) {
          return html`<${Item} key=${o[0]} label=${o[0]} hint=${shortDate(o[1])} run=${function () { setReview(ymd(o[1])); }} />`;
        })}
      </div>
    <//>`;
  }

  // ---- 削除の理由欄 ----

  // DropAsk hangs a reason field off the trash icon, the way the due menu
  // hangs off its chip. Enter in the reason field deletes once something is
  // written; Enter in the name field below hands the task to that person
  // instead (a wait, nothing is deleted), with the reason as the wait note.
  // Escape or a click elsewhere leaves the task where it is.
  function DropAsk(p) {
    var ctx = p.ctx;
    var ask = p.ask;
    var placed = useAnchored(ask.btn);
    var boxRef = placed[0];
    var inputRef = useRef(null);
    var whoRef = useRef(null);
    // The field that was used last gets the caret back after a refusal.
    var lastRef = useRef(null);
    var emptyState = useState(true);
    var empty = emptyState[0];
    var sendState = useState(false);
    var sending = sendState[0];
    // The click-away listener outlives a draw, so it reads the flag from a ref.
    var sendingRef = useRef(false);
    var wasSending = useRef(false);

    // Focus waits for the place (see Menu).
    var placedAt = !!placed[1];
    useLayoutEffect(function () { if (placedAt) inputRef.current.focus(); }, [placedAt]);

    // The listener is on the document, in the capture phase: a click on a row
    // (which stops its own bubbling) still reaches it.
    useLayoutEffect(function () {
      function outside(ev) {
        if (!sendingRef.current && !boxRef.current.contains(ev.target)) ctx.closeDropAsk();
      }
      document.addEventListener('mousedown', outside, true);
      return function () { document.removeEventListener('mousedown', outside, true); };
    }, []);

    // After a refused DELETE the field comes back with the caret in it.
    useLayoutEffect(function () {
      if (wasSending.current && !sending) (lastRef.current || inputRef.current).focus();
      wasSending.current = sending;
    }, [sending]);

    // While the DELETE is out the field stays: closing it would let the
    // trash be pressed again and send a second DELETE behind an undo.
    function submit() {
      var input = inputRef.current;
      var reason = input.value.trim();
      if (!reason || sendingRef.current) { input.focus(); return; }
      send(input, ctx.removeTask(ask.task, reason));
    }

    // wait hands the task on instead of deleting it. The reason, if written,
    // becomes the wait note; an empty name sends nothing.
    function wait() {
      var who = whoRef.current;
      var to = who.value.trim();
      if (!to || sendingRef.current) { who.focus(); return; }
      send(who, ctx.waitTask(ask.task, to, inputRef.current.value.trim()));
    }

    function send(field, promise) {
      lastRef.current = field;
      sendingRef.current = true;
      sendState[1](true);
      promise
        .then(function () { if (ctx.state.dropAsk === ask) ctx.closeDropAsk(); })
        .catch(function (err) {
          sendingRef.current = false;
          sendState[1](false);
          ctx.showError(err);
        });
    }

    // The keydown is on the box, not the input, so Escape also works from the button.
    return html`<div ref=${boxRef} class="drop-ask" role="dialog" aria-label=${ask.task.name + ' をやめる理由'} style=${placed[1]}
      onKeyDown=${function (ev) {
        if (composing(ev) || ev.key !== 'Escape' || sendingRef.current) return;
        ev.preventDefault();
        ev.stopPropagation();
        ctx.closeDropAsk();
        ask.btn.focus();
      }}>
      <div class="dm-label">やめる理由（作ってから1時間以上たっている）</div>
      <input ref=${inputRef} type="text" class="drop-ask-input" placeholder="なぜやめるか" aria-label="やめる理由"
        disabled=${sending}
        onInput=${function (ev) { emptyState[1](!ev.target.value.trim()); }}
        onKeyDown=${function (ev) {
          if (composing(ev)) return;
          if (ev.key === 'Enter') { ev.preventDefault(); submit(); }
        }} />
      <button type="button" class="drop-ask-go" disabled=${sending || empty} onClick=${submit}>削除する</button>
      <div class="dm-label drop-ask-sep">削除せず、相手に渡して待ちにする</div>
      <input ref=${whoRef} type="text" class="drop-ask-input" placeholder="渡した相手" aria-label="渡した相手"
        disabled=${sending}
        onKeyDown=${function (ev) {
          if (composing(ev)) return;
          if (ev.key === 'Enter') { ev.preventDefault(); wait(); }
        }} />
      <button type="button" class="drop-ask-wait" disabled=${sending} onClick=${wait}>待ちにする</button>
    </div>`;
  }

  // ---- 通知 ----

  // Flash says what just happened without looking like a failure, and offers
  // to take it back. Undo is where a mis-click gets fixed, so it belongs next
  // to the message about the click. f.go, when given, is one more button
  // placed before undo: { label, run }. A failure is the same box without
  // the notice colour (f.notice false).
  function Flash(p) {
    var f = p.flash;
    var kids = [];
    if (f) {
      kids.push(f.text);
      if (f.go) {
        kids.push('  ', html`<button type="button" class="undo-link" onClick=${f.go.run}>${f.go.label}</button>`);
      }
      if (f.undoable) {
        kids.push('  ', html`<button type="button" class="undo-link" onClick=${p.undo}>元に戻す</button>`);
      }
    }
    return html`<p class=${'err' + (f && f.notice ? ' notice' : '')} id="err" hidden=${!f}>${kids}</p>`;
  }

  // ---- 描く ----

  function Menus(p) {
    var s = p.state;
    var ctx = p.ctx;
    var m = s.menu;
    return [
      m ? html`<${m.kind === 'review' ? ReviewMenu : DueMenu} key=${m.seq} ctx=${ctx} menu=${m} />` : null,
      s.dropAsk ? html`<${DropAsk} key=${s.dropAsk.seq} ctx=${ctx} ask=${s.dropAsk} />` : null,
    ];
  }

  // render draws the overlays from view.state: the menu and the reason field
  // into menusEl, the notice into flashEl. view.ctx is what they call back into.
  function render(menusEl, flashEl, view) {
    P.render(html`<${Menus} state=${view.state} ctx=${view.ctx} />`, menusEl);
    P.render(html`<${Flash} flash=${view.state.flash} undo=${view.ctx.undo} />`, flashEl);
  }

  window.workbench = window.workbench || {};
  window.workbench.todoMenus = { render: render };
})();
