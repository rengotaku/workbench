// The add page: the whole screen is one field. Enter adds the task and clears
// the field for the next one, so several can go in without leaving the page.
// It is drawn with Preact (ADR todo-0025). The add dialog (modal.js) borrows
// the same screen.
//
// The name, the detail and the date picker are uncontrolled: the element holds
// the text and Preact never writes its value, so a draw (a chip pressed, a toast,
// a row added) keeps what is typed, the focus and the caret. The related-URL
// rows are links.js.
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var render = P.render;
  var useState = P.useState;
  var useRef = P.useRef;
  var useEffect = P.useEffect;
  var useLayoutEffect = P.useLayoutEffect;

  var shared = window.todoShared;
  var links = window.workbench.todoLinks;

  // active is true while the screen is on show (as a page or in the add
  // dialog): the key listener on document stays for the life of the page and
  // must not act from a hidden screen.
  var active = false;

  // ---- dates ----

  // The date beside the heading, as the due chips count it: 1/2 (曜).
  function todayLabel(d) {
    return (d.getMonth() + 1) + '/' + d.getDate() + ' (' + '日月火水木金土'.charAt(d.getDay()) + ')';
  }

  function ymd(d) {
    var pad = function (n) { return (n < 10 ? '0' : '') + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }
  function plus(d, days) { return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days); }

  // This week's Friday (today when it is Friday), or next week's once it has passed.
  function friday(today) { return plus(today, (5 - today.getDay() + 7) % 7); }

  // A weekday of next week, weeks starting on Monday. wd: 1 = Monday … 0 = Sunday.
  function nextWeek(today, wd) {
    var sinceMonday = (today.getDay() + 6) % 7;
    return plus(today, 7 - sinceMonday + (wd + 6) % 7);
  }

  // The chips for the day `t` is in, and the label of that day. The chips hold
  // dates (今日, 明日 …): drawn on another day, they would save that day's dates.
  function choicesFor(t) {
    var today = new Date(t.getFullYear(), t.getMonth(), t.getDate());
    return {
      on: ymd(today),
      label: todayLabel(today),
      list: [
        { label: 'なし', due: '' },
        { label: '今日', due: ymd(today) },
        { label: '明日', due: ymd(plus(today, 1)) },
        { label: '金曜', due: ymd(friday(today)) },
        { label: '来週月', due: ymd(nextWeek(today, 1)) },
        { label: '来週水', due: ymd(nextWeek(today, 3)) },
        { label: '日付…', pick: true },
      ],
    };
  }

  // fetch rejects with a TypeError when the server cannot be reached at all;
  // say that plainly instead of the browser's "Failed to fetch".
  function reason(err) {
    return err instanceof TypeError ? 'サーバに接続できません（todo-order のサーバが起動しているか確認してください）' : err.message;
  }

  // ---- the screen ----

  // AddScreen props: parent (the task the new tasks are cut out of, when the
  // screen was opened from its 子を追加 button, ?parent=T051) and visit (counts
  // the times the screen was shown).
  function AddScreen(p) {
    var nameRef = useRef(null);
    var detailRef = useRef(null);
    var pickRef = useRef(null);

    var chipsState = useState(function () { return choicesFor(new Date()); });
    var chips = chipsState[0];
    // The chip pressed and the date it stands for ('' for なし).
    var dueState = useState({ index: 0, due: '' });
    var due = dueState[0];
    var busyState = useState(false);
    var busy = busyState[0];
    // toast: { text, error, undo: {id, name} | null } or null.
    var toastState = useState(null);
    var toast = toastState[0];
    // What was added, newest first: [{id, name, due, undone}].
    var recentState = useState([]);
    var recent = recentState[0];

    // The handlers made once (the key listener, the rows' callbacks) read the
    // latest state and handlers through here.
    var latest = useRef(null);
    latest.current = { due: due, chips: chips, parent: p.parent };
    var guard = useRef(false); // a save is out
    var seq = useRef(0);
    var wantFocus = useRef(false);

    function showToast(text, error, undo) { toastState[1]({ text: text, error: !!error, undo: undo || null }); }

    // focusName puts the caret in the name. A disabled field (a save is out)
    // cannot take it, so the request waits for the draw that enables it.
    function focusName() {
      var name = nameRef.current;
      if (name.disabled) { wantFocus.current = true; return; }
      name.focus();
    }
    useLayoutEffect(function () {
      if (wantFocus.current && !nameRef.current.disabled) {
        wantFocus.current = false;
        nameRef.current.focus();
      }
    });

    // The related-URL rows. The name points at them with [N]: a link pasted
    // into the name goes in as "text [N]" with its URL as related URL N
    // (/shared/js/paste.js).
    var rowsRef = useRef(null);
    if (!rowsRef.current) {
      rowsRef.current = links.create({
        text: function () { return nameRef.current.value; },
        onError: function (msg) { showToast(msg, true); },
        onEnter: function () { latest.current.submit(); },
      });
    }
    var rows = rowsRef.current;

    useLayoutEffect(function () {
      window.workbench.paste.wireLinkPaste(nameRef.current, function (link) {
        if (guard.current) return 0;
        return rows.add(link.url + (link.label ? ' ' + link.label : ''), true);
      }, true);
    }, []);

    // ---- due chips ----

    function setDue(value, index) { dueState[1]({ index: index, due: value }); }

    function choose(i) {
      var c = latest.current.chips.list[i];
      if (!c) return;
      if (!c.pick) {
        setDue(c.due, i);
        focusName();
        return;
      }
      var pick = pickRef.current;
      pick.value = latest.current.due.due;
      if (pick.showPicker) {
        try { pick.showPicker(); return; } catch (e) { /* needs a user gesture; fall through */ }
      }
      pick.focus();
    }

    // ---- add / undo ----

    function undo(id, name) {
      var told = window.workbench.tell('busy');
      shared.request('POST', '/api/undo')
        .then(function () {
          recentState[1](function (list) {
            return list.map(function (r) { return r.id === id ? Object.assign({}, r, { undone: true }) : r; });
          });
          window.workbench.tell('changed', told);
          showToast('取り消しました: ' + name);
        })
        .catch(function (err) { showToast('取り消せませんでした: ' + reason(err), true); })
        .then(function () { window.workbench.tell('idle', told); });
    }

    function reset() {
      nameRef.current.value = '';
      detailRef.current.value = '';
      rows.set([]);
      setDue('', 0);
      focusName();
    }

    function submit() {
      var name = nameRef.current.value.trim();
      if (!name || guard.current) { focusName(); return; }
      var body = { name: name };
      var cur = latest.current;
      if (cur.due.due) body.due = cur.due.due;
      var detail = detailRef.current.value.trim();
      if (detail) body.detail = detail;
      var related = rows.read(false);
      if (!related) return;
      if (related.length) body.links = related;
      if (cur.parent) body.parent = cur.parent;

      guard.current = true;
      busyState[1](true);
      rows.lock(true);
      var told = window.workbench.tell('busy');
      shared.request('POST', '/api/tasks', body)
        .then(function (data) {
          var t = data.task || body;
          var id = ++seq.current;
          recentState[1](function (list) {
            return [{ id: id, name: t.name, due: t.due || '', undone: false }].concat(list);
          });
          window.workbench.tell('changed', told);
          showToast('追加しました: ' + t.name, false, { id: id, name: t.name });
          busyState[1](false);
          rows.lock(false);
          reset();
        })
        .catch(function (err) {
          // Keep what was typed so nothing is lost; the message says why.
          busyState[1](false);
          rows.lock(false);
          focusName();
          showToast('追加できませんでした: ' + reason(err), true);
        })
        .then(function () { guard.current = false; window.workbench.tell('idle', told); });
    }

    latest.current.submit = submit;
    latest.current.choose = choose;

    useEffect(function () {
      function onKey(e) {
        if (!active) return;
        if (shared.composing(e)) return;
        // ⌥+digit types a symbol on macOS (¡ ™ …), so read the physical key.
        var m = /^Digit([0-9])$/.exec(e.code || '');
        if (e.altKey && m) { e.preventDefault(); latest.current.choose(Number(m[1])); return; }
        if (e.key === 'Escape' && document.activeElement !== pickRef.current) { window.workbench.leave('/'); }
      }
      document.addEventListener('keydown', onKey);
      return function () { document.removeEventListener('keydown', onKey); };
    }, []);

    // Each time the screen is shown.
    useLayoutEffect(function () {
      // The chips were drawn for a day; once the day has moved, draw them again.
      var today = choicesFor(new Date());
      if (chips.on !== today.on) {
        chipsState[1](today);
        setDue('', 0);
      }
      // The toast was about the last visit; what went in stays listed below.
      toastState[1](null);
      // autofocus only works once per page; the screen is shown many times.
      focusName();
    }, [p.visit]);

    return html`<div class="ap-page">
      <header class="ap-head">
        <span>着手順に追加 <span class="ap-date">· ${chips.label}</span></span>
        <a href="/" id="to-list" data-leave="">一覧へ →</a>
      </header>

      <main class="ap-main">
        <label class="ap-label" for="ap-name">なにをやる？</label>
        <p class="ap-parent" id="ap-parent" hidden=${!p.parent}>${p.parent ? p.parent + ' の子として追加する' : ''}</p>
        <input ref=${nameRef} class="ap-name" id="ap-name" autofocus autocomplete="off" placeholder="タスク名を入力して Enter"
          disabled=${busy}
          onKeyDown=${function (e) {
            if (shared.composing(e)) return;
            if (e.key === 'Enter') { e.preventDefault(); submit(); }
          }} />
        <div class="ap-links" id="ap-links">
          <${links.LinkEditor} ctl=${rows} />
        </div>

        <div class="ap-row">
          <div class="ap-field">
            <p class="ap-label">期限</p>
            <div class="ap-chips" id="ap-chips">
              ${chips.list.map(function (c, i) {
                return html`<button key=${c.label} type="button" class="ap-chip" aria-pressed=${String(i === due.index)}
                  onClick=${function () { choose(i); }}>${c.label}<kbd>${i}</kbd></button>`;
              })}
            </div>
            <input ref=${pickRef} type="date" id="ap-pick" class="ap-pick" tabindex="-1" aria-label="期限の日付"
              onChange=${function (e) {
                if (e.target.value) setDue(e.target.value, chips.list.length - 1);
                focusName();
              }} />
            <div class="ap-resolved" id="ap-resolved">${due.due ? '→ ' + due.due : ''}</div>
          </div>
          <div class="ap-field">
            <label class="ap-label" for="ap-detail">詳細（任意）</label>
            <textarea ref=${detailRef} class="ap-detail" id="ap-detail" placeholder="メモや URL"></textarea>
          </div>
        </div>

        <div class="ap-actions">
          <button type="button" class="ap-submit" id="ap-submit" onClick=${submit}>追加する</button>
          <span class="ap-hint"><kbd>⏎</kbd> 追加して次を入力 · <kbd>⌥</kbd>+数字 で期限 · <kbd>Esc</kbd> 一覧へ</span>
        </div>

        <div class=${'ap-toast' + (toast && toast.error ? ' is-error' : '')} id="ap-toast" role="status" hidden=${!toast}>
          ${toast ? toast.text : ''}${toast && toast.undo
            ? html`<button type="button" onClick=${function () { undo(toast.undo.id, toast.undo.name); }}>元に戻す</button>`
            : null}
        </div>

        <section class="ap-recent" id="ap-recent-wrap" hidden=${recent.length === 0}>
          <p class="ap-label">いま追加したもの</p>
          <ul id="ap-recent">
            ${recent.map(function (r) {
              return html`<li key=${r.id} class=${r.undone ? 'is-undone' : undefined}>
                <span class="ap-recent-name">${r.name}</span><span class="ap-recent-due">${r.due || '期限なし'}</span>
              </li>`;
            })}
          </ul>
        </section>
      </main>
    </div>`;
  }

  // The root of the screen and the times it has been shown. The router builds
  // the screen once (mount) and shows it many times (show), in the page or in
  // the add dialog; each show draws it again with the new query.
  var screen = { root: null, parent: '', visit: 0 };

  function draw() {
    render(html`<${AddScreen} parent=${screen.parent} visit=${screen.visit} />`, screen.root);
  }

  window.workbench.router.register('todo-add', {
    title: '着手順に追加',
    mount: function (root) {
      active = true;
      screen.root = root;
    },
    show: function (url) {
      active = true;
      screen.parent = (url && url.searchParams.get('parent')) || '';
      screen.visit++;
      draw();
    },
    hide: function () {
      active = false;
    },
  });
})();
