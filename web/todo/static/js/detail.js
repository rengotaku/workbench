// 開いた行の詳細を Preact で描く。ADR todo-0025。
//
//   NameField  開いた行の名前の欄（contenteditable）
//   Panel      開いた行の詳細パネル（詳細・話題・状況・関連 URL・補足・操作ボタン・経緯）
//   renderSide 一覧の横の詳細欄（読むだけ。話題の欄だけ書ける）
//
// list.js が行の中に置く。状態（state）・API 呼び出し・読み直し（load）は app.js のもので、
// ctx 経由で受け取る。ここは状態を持たない: 持つのは欄ごとの「最後に保存した文字」と
// 保存中の印だけ（部品の中）。
//
// 打ち込み中の欄を取り替えない作り（#115）:
//   - 欄は書き換えない要素（uncontrolled）にする。文字は要素が持ち、Preact は子を触らない。
//     行・パネル・欄は同じ部品のまま残るので、描き直しをまたいで要素・文字・フォーカス・
//     キャレットが残る
//   - 盤面が進んでも、何かが握っている間（state.busy）は、パネルは前の task を表示したまま
//     にする（useHeld）。離れた後の最初の描画で追いつく
//   - 外から変わった値は、欄が編集中（フォーカスがある・保存中・未保存の文字がある）でなければ
//     欄に流し込む。編集中なら流し込まず、保存が済んでから追いつく
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var h = P.h;
  var useState = P.useState;
  var useRef = P.useRef;
  var useEffect = P.useEffect;
  var useLayoutEffect = P.useLayoutEffect;

  var shared = window.todoShared;
  var composing = shared.composing;
  var request = shared.request;

  // MAX_DEPTH is how many levels a tree may have, as in the server (ADR todo-0020).
  var MAX_DEPTH = 3;

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

  // What a person writes as status (memo, or progress / blocked from the CLI)
  // carries no tag: telling those apart was more work than it was worth. A
  // quote or an analysis keeps its tag, and anything AI wrote is labelled AI's.
  var ANNOTATION_LABEL = { progress: '進んだ', blocked: '詰まり', memo: 'メモ', quote: '引用', analysis: '考察' };
  var UNTAGGED_KINDS = { memo: true, progress: true, blocked: true };

  // ---- 読み取り（純粋な関数） ----

  function annotationLabel(a) {
    if (a.author !== 'ai' && UNTAGGED_KINDS[a.kind]) return '';
    var label = ANNOTATION_LABEL[a.kind] || a.kind;
    return a.author === 'ai' ? 'AI ' + label : label;
  }

  function kindClass(kind) { return String(kind).replace(/[^a-z]/g, ''); }

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

  function pad2(n) { return ('0' + n).slice(-2); }

  // M/D hh:mm
  function whenText(at) {
    var d = new Date(at);
    return (d.getMonth() + 1) + '/' + d.getDate() + ' ' + pad2(d.getHours()) + ':' + pad2(d.getMinutes());
  }

  function eventWhat(e) {
    return (KIND_LABEL[e.kind] || e.kind) + changeOf(e) + (e.actor === 'ai' ? '（AI）' : '');
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

  // taskUrl is the address that opens this row: the page reads #Txxx on load.
  // It is what gets pasted to an AI to say which task is meant.
  function taskUrl(t) {
    return location.origin + location.pathname + '#' + t.id;
  }

  // boardTasks lists every task the board carries, whatever its section.
  function boardTasks(board) {
    var b = board || {};
    return (b.queue || []).concat(b.long || [], b.waiting || [], b.someday || [], b.done || []);
  }

  // treeFacts reads the parent links off the board: each task's children, and
  // how many levels the tree under a task has (the task itself counts).
  function treeFacts(board) {
    var kids = {};
    boardTasks(board).forEach(function (x) {
      if (x.parent) (kids[x.parent] = kids[x.parent] || []).push(x.id);
    });
    function height(id, hops) {
      var deepest = 0;
      if (hops < 12) (kids[id] || []).forEach(function (c) { deepest = Math.max(deepest, height(c, hops + 1)); });
      return deepest + 1;
    }
    return { kids: kids, height: function (id) { return height(id, 0); } };
  }

  // ---- 共有する小さな仕組み ----

  // useHeld returns the task (and the board revision it came from) the part
  // shows. A newer one is taken up only while nothing holds the poll
  // (state.busy: a field being typed in, a drag, a menu); until then the part
  // keeps showing the older one, so a reply cannot move the fields under the
  // caret, and the first draw after the hold catches up.
  function useHeld(t, rev, ctx) {
    var ref = useRef(null);
    if (ref.current === null || (ref.current.rev !== rev && !ctx.state.busy)) {
      ref.current = { t: t, rev: rev };
    }
    return ref.current;
  }

  // useReleaseOnUnmount frees the poll when the part goes while the focus is
  // inside it: the browser may send no blur for an element that is removed.
  function useReleaseOnUnmount(ref, ctx) {
    useEffect(function () {
      var node = ref.current;
      return function () {
        if (node && node.contains(document.activeElement)) ctx.releaseBusy();
      };
    }, []);
  }

  // The save-state slot of each open row ("保存中…" / "保存した"), by task id:
  // the name (in the row) and the fields of the panel report to one place.
  var saveMarks = {};
  function markSave(id, text, done) {
    var set = saveMarks[id];
    if (set) set(text, done);
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

  // ---- 書き換えない欄 ----

  // LiveText is a piece of text that saves itself: a pause in typing writes it,
  // and so does leaving the field. There is no save button to forget. It is
  // the open row's name (single line) and the detail (several lines).
  //   p.field   the task's field: 'name' or 'detail'
  //   p.value   what the board holds now
  function LiveText(p) {
    var ref = useRef(null);
    var props = useRef(p);
    props.current = p;
    var own = useRef(null);
    if (own.current === null) own.current = { last: p.value || '', applied: p.value || '', timer: null, inflight: 0, sent: null };
    var s = own.current;

    function single() { return props.current.field === 'name'; }
    function read(node) {
      var v = node.textContent;
      return single() ? v.replace(/\s+/g, ' ').trim() : v;
    }

    // The text goes in once, before the first paint; a Preact-drawn child
    // would be fought over with the typing.
    useLayoutEffect(function () { ref.current.textContent = s.applied; }, []);

    // Whatever the board holds now flows into the field unless it is being
    // edited. Run after every draw: the draw that ends a hold is what brings it.
    useEffect(function () {
      var node = ref.current;
      var v = p.value || '';
      if (v === s.applied) return;
      if (s.inflight > 0 || document.activeElement === node || read(node) !== s.last) return;
      node.textContent = v;
      s.last = v;
      s.applied = v;
    });

    useEffect(function () {
      var node = ref.current;
      var field = props.current.field;
      var id = props.current.t.id;
      var ctx = props.current.ctx;
      var isName = field === 'name';

      function save(reload) {
        var value = read(node);
        if (value === s.last) return;
        if (isName && !value) { node.textContent = s.last; return; } // a name cannot be empty

        var body = {};
        body[field] = value;
        markSave(id, '保存中…');
        s.inflight++;
        s.sent = value;
        request('PATCH', '/api/tasks/' + encodeURIComponent(id), body)
          .then(function () {
            s.inflight--;
            s.last = value;
            markSave(id, '保存した', true);
            // Not while another field of the row is being typed in (its related
            // URL rows): redrawing would drop what is typed there.
            if (reload && !ctx.state.busy) ctx.load();
          })
          .catch(function (err) { s.inflight = Math.max(0, s.inflight - 1); s.sent = null; ctx.showError(err); node.textContent = s.last; });
      }

      if (isName) {
        // A link pasted into the name goes in as "text [N]" with its URL as
        // related URL N, in the open row's rows below (/shared/js/paste.js).
        window.workbench.paste.wireLinkPaste(node, function (link) {
          var ed = linkEditors[id];
          return ed ? ed.add(link.url + (link.label ? ' ' + link.label : ''), true) : 0;
        }, true);
        paintLink(node, props.current.t);
      } else {
        shared.wirePaste(node, false);
      }
      node.addEventListener('focus', function () { ctx.state.busy = true; });
      node.addEventListener('input', function () {
        if (isName) paintLink(node, props.current.t);
        window.clearTimeout(s.timer);
        s.timer = window.setTimeout(function () { save(false); }, 900);
      });
      node.addEventListener('blur', function () {
        window.clearTimeout(s.timer);
        ctx.state.busy = false;
        if (isName && window.CSS && CSS.highlights) CSS.highlights.delete(HIGHLIGHT_NAME);
        save(true);
      });
      node.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter' && isName) { e.preventDefault(); node.blur(); }
        if (e.key === 'Escape') { e.stopPropagation(); node.blur(); }
      });
      // The part can go (a reply moved the row to another section) before the
      // pause in typing is over, and no blur is sent for a removed element: what
      // was typed is saved for the task it belongs to, through the same path.
      return function () {
        window.clearTimeout(s.timer);
        var value = read(node);
        if (value !== s.last && value !== s.sent) save(false);
      };
    }, []);
    useReleaseOnUnmount(ref, p.ctx);

    return h(p.tag, {
      ref: ref,
      'class': p.className,
      contenteditable: 'true',
      spellcheck: false,
      'data-placeholder': p.placeholder,
    });
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
    var r = window.workbench.todoList.linkRange({ url: t.url, name: text.data, link_text: t.link_text });
    if (!r) return;
    var range = document.createRange();
    range.setStart(text, r.start);
    range.setEnd(text, r.end);
    CSS.highlights.set(HIGHLIGHT_NAME, new Highlight(range));
  }

  // NameField is the open row's name: the same text in the same place, edited
  // where it is read. Ticket keys stop being links while editing: a link inside
  // an editable line fights the caret.
  function NameField(p) {
    var held = useHeld(p.t, p.rev, p.ctx);
    return html`<${LiveText} tag="span" className="txt live" field="name" value=${held.t.name}
      t=${held.t} ctx=${p.ctx} />`;
  }

  // TopicField is the one-line input for a task's topic, in the open row and
  // in the box beside the list. The value is saved when typing stops or the
  // field is left, like the name and detail; "" clears it. Suggestions come
  // from the datalist of topics in use.
  function TopicField(p) {
    var ref = useRef(null);
    var props = useRef(p);
    props.current = p;
    var own = useRef(null);
    if (own.current === null) {
      own.current = { last: p.t.topic || '', applied: p.t.topic || '', timer: null,
        // One PATCH at a time: a later save waits for the one in flight and then
        // sends whatever the field holds, so an older value can never land last.
        saving: false, again: false, reloadAfter: false };
    }
    var s = own.current;

    useLayoutEffect(function () { ref.current.value = s.applied; }, []);

    useEffect(function () {
      var node = ref.current;
      var v = p.t.topic || '';
      if (v === s.applied) return;
      if (s.saving || document.activeElement === node || node.value.trim() !== s.last) return;
      node.value = v;
      s.last = v;
      s.applied = v;
    });

    useEffect(function () {
      var input = ref.current;
      var ctx = props.current.ctx;
      var id = props.current.t.id;
      var inRow = props.current.inRow; // the box beside the list has no save slot

      function finish() {
        if (!s.reloadAfter || s.saving) return;
        s.reloadAfter = false;
        if (!ctx.state.busy) ctx.load();
      }
      function save(reload) {
        if (reload) s.reloadAfter = true;
        if (s.saving) { s.again = true; return; }
        var value = input.value.trim();
        if (value === s.last) { finish(); return; }
        s.saving = true;
        if (inRow) markSave(id, '保存中…');
        request('PATCH', '/api/tasks/' + encodeURIComponent(id), { topic: value })
          .then(function () {
            s.last = value;
            if (inRow) markSave(id, '保存した', true);
          })
          // Keep what was typed: it may already be newer than the failed value.
          .catch(function (err) { ctx.showError(err); })
          .then(function () {
            s.saving = false;
            if (s.again) { s.again = false; save(false); return; }
            finish();
          });
      }
      input.addEventListener('focus', function () { ctx.state.busy = true; });
      input.addEventListener('input', function () {
        window.clearTimeout(s.timer);
        s.timer = window.setTimeout(function () { save(false); }, 900);
      });
      input.addEventListener('blur', function () {
        window.clearTimeout(s.timer);
        ctx.state.busy = false;
        save(true);
      });
      input.addEventListener('keydown', function (e) {
        if (composing(e)) return;
        if (e.key === 'Enter') { e.preventDefault(); input.blur(); }
        if (e.key === 'Escape') { e.stopPropagation(); input.blur(); }
      });
      // Saved on the way out for the same reason as LiveText: no blur comes for
      // a removed element, so a pending pause in typing would drop the text.
      return function () {
        window.clearTimeout(s.timer);
        if (input.value.trim() !== s.last) save(false);
      };
    }, []);
    useReleaseOnUnmount(ref, p.ctx);

    return html`<div class="topic-line">
      <label class="topic-label" for=${p.id}>話題</label>
      <input ref=${ref} type="text" id=${p.id} class="topic-input" autocomplete="off" spellcheck=${false}
        maxLength=${20} list="todo-topics" placeholder="なし" />
    </div>`;
  }

  // StatusBlock is the field that appends one line of status to the open row.
  // A supplement is append-only, so this only ever adds; a failed send keeps
  // what was typed. The draft lives in state.statusDrafts as well, so it
  // survives the row being closed and opened again.
  function StatusBlock(p) {
    var ref = useRef(null);
    var props = useRef(p);
    props.current = p;
    var sendingState = useState(false);
    var sending = sendingState[0];
    var setSending = sendingState[1];
    var sendRef = useRef(null);
    var guard = useRef(false);
    var id = p.t.id;

    useLayoutEffect(function () { ref.current.value = props.current.ctx.state.statusDrafts[id] || ''; }, []);

    function submit() {
      var ctx = props.current.ctx;
      var input = ref.current;
      var body = input.value.trim();
      if (!body || guard.current) return;
      guard.current = true;
      setSending(true);
      request('POST', '/api/tasks/' + encodeURIComponent(id) + '/annotations',
        { kind: 'memo', body: body, actor: 'human' })
        .then(function () {
          // Clear only what was sent: anything typed while waiting stays.
          var drafts = ctx.state.statusDrafts;
          var draft = drafts[id] || '';
          if (draft.trim() === body) delete drafts[id];
          if (input.value.trim() === body) input.value = '';
          var refocus = document.activeElement === input || document.activeElement === document.body
            || document.activeElement === sendRef.current;
          ctx.state.busy = false;
          return ctx.load().then(function () {
            if (refocus && input.isConnected) input.focus();
          });
        }, function (err) {
          ctx.showError(err);
        })
        .then(function () {
          // load() reports its own failure and does not redraw then, so the
          // guard is released here whether or not the list came back.
          guard.current = false;
          setSending(false);
        });
    }

    useReleaseOnUnmount(ref, p.ctx);
    var inputId = 'status-' + id;
    return html`<div class="status-add">
      <div class="status-line">
        <label class="sr-only" for=${inputId}>いまの状況</label>
        <input ref=${ref} type="text" id=${inputId} class="status-input" autocomplete="off"
          placeholder="いまの状況を 1 行で（Enter で追記）"
          onInput=${function (ev) { props.current.ctx.state.statusDrafts[id] = ev.target.value; }}
          onKeyDown=${function (e) {
            if (composing(e)) return;
            if (e.key === 'Enter') { e.preventDefault(); submit(); }
            // Like the live fields: the first Escape leaves the field.
            if (e.key === 'Escape') { e.stopPropagation(); e.target.blur(); }
          }}
          onFocus=${function () { props.current.ctx.state.busy = true; }}
          onBlur=${function () { props.current.ctx.state.busy = false; }} />
        <button ref=${sendRef} type="button" class="status-send" disabled=${sending} onClick=${submit}>追記</button>
      </div>
    </div>`;
  }

  // LinksBlock is the open row's related URLs, edited in place like the name:
  // adding (or pasting) a URL, editing a row and removing one save at once.
  // Numbers follow the rows' order, and a row the name still points at (or
  // one before it) cannot be removed, so a [N] never moves to another URL.
  //
  // The rows themselves are links.js (the todo add screen draws the same
  // widget). This component owns the editor's owner (ed) and draws its rows;
  // the editor is made once and the board's links flow into it only while it
  // is not being edited.
  function LinksBlock(p) {
    var ref = useRef(null);
    var props = useRef(p);
    props.current = p;
    // applied: the list the server last confirmed (read from the board or saved
    // from here); seen: the last board list looked at.
    var own = useRef(null);
    var id = p.t.id;

    function norm(links) {
      return JSON.stringify((links || []).map(function (l) { return { url: l.url, label: l.label || '' }; }));
    }

    // Made in the first draw, so the rows are in it from the start.
    if (!own.current) {
      var q = linkSaves[id] || (linkSaves[id] = { busy: false, next: null, last: '' });
      var ed = window.workbench.todoLinks.create({
        // A row the name points at cannot go. Both the name being typed and the
        // saved one count: the typed one may yet fail to save and come back.
        text: function () { return nameNow() + ' ' + props.current.t.name; },
        onError: function (msg) { props.current.ctx.showError(new Error(msg)); },
        onChange: save,
        onEnter: function () { if (document.activeElement) document.activeElement.blur(); },
      });
      own.current = { ed: ed, q: q, applied: '', seen: '' };
      q.last = norm(p.t.links);
      own.current.applied = q.last;
      own.current.seen = q.last;
      ed.set(p.t.links || []);
    }
    var o0 = own.current;

    function nameNow() {
      var n = document.querySelector('.task[data-id="' + id + '"] .txt.live');
      return n ? n.textContent : props.current.t.name;
    }

    // One save at a time per task (linkSaves): each PATCH replaces the whole
    // list, so two in flight could land in either order and the older list
    // would win. A save asked for meanwhile runs once the one out has
    // answered, and compares with what the server last confirmed, so
    // changing a row and changing it back while a save is out still lands.
    function save() {
      var q = o0.q;
      var ed = o0.ed;
      if (q.busy) { q.next = save; return; }
      var links = ed.read(true);
      if (!links) return; // an empty row: saved once it has a URL
      var now = JSON.stringify(links);
      if (now === q.last) return;
      q.busy = true;
      markSave(id, '保存中…');
      request('PATCH', '/api/tasks/' + encodeURIComponent(id), { links: links })
        .then(function () {
          q.last = now;
          o0.applied = now;
          markSave(id, '保存した', true);
        })
        .catch(function (err) { props.current.ctx.showError(err); })
        .then(function () {
          q.busy = false;
          var next = q.next;
          q.next = null;
          if (next) next();
        });
    }

    useEffect(function () {
      var ed = o0.ed;
      linkEditors = {};
      linkEditors[id] = ed;
      return function () {
        if (ref.current && ref.current.contains(document.activeElement)) props.current.ctx.releaseBusy();
        if (linkEditors[id] === ed) delete linkEditors[id];
      };
    }, []);

    // What the board holds flows into the rows unless they are being edited
    // (the focus is inside, a save is out, a row waits for its URL or holds a
    // change not saved yet). With no save out, the list just read is what the
    // server holds (it may have moved through the CLI meanwhile).
    useEffect(function () {
      var o = own.current;
      var q = o.q;
      if (!o.ed || !q) return;
      var want = norm(p.t.links);
      // The board has to have moved: the old list is still what it holds until
      // the reply that carries a save made here comes in.
      if (want === o.seen) return;
      if (q.busy || ref.current.contains(document.activeElement)) return;
      q.last = want;
      var cur = o.ed.read(true);
      if (!cur) return;
      var now = JSON.stringify(cur);
      if (now === want) { o.applied = want; o.seen = want; return; }
      if (now !== o.applied) return;
      o.ed.set(p.t.links || []);
      o.applied = want;
      o.seen = want;
    });

    // Typing in a row holds the poll, as the name and detail do.
    return html`<${window.workbench.todoLinks.LinkEditor} ctl=${o0.ed} hostRef=${ref}
      onFocusIn=${function () { props.current.ctx.state.busy = true; }}
      onFocusOut=${function () { props.current.ctx.state.busy = false; }} />`;
  }

  // ---- 読むだけの部品 ----

  // Supplements shows what was appended to a task (newest first, like the
  // trail) and the link back to the memo it was made from. A body is shown as
  // text and never read as HTML. It draws nothing when there is nothing to show.
  function Supplements(p) {
    var t = p.t;
    var anns = t.annotations || [];
    if (anns.length === 0 && !t.source_note_id) return null;
    return html`<div class="d-sec d-ann">
      ${t.source_note_id ? html`<a class="d-from" href=${'/notes/timeline#note-' + encodeURIComponent(t.source_note_id)}>元のメモ #${t.source_note_id}</a>` : null}
      ${anns.length > 0 ? html`<h2>状況（新しい順）</h2>` : null}
      ${anns.slice().reverse().map(function (a, i) {
        // An empty tag keeps the columns of the status list in place.
        var label = annotationLabel(a);
        return html`<div key=${i} class=${'d-ann-item k-' + kindClass(a.kind)}>
          <time class="d-ann-when">${whenText(a.created_at)}</time>
          <span class=${label ? 'ann-tag k-' + kindClass(a.kind) : 'ann-tag-none'}>${label}</span>
          <div class=${'d-ann-body' + (a.author === 'ai' ? ' ai' : '')}>${a.body + (a.source ? ' · ' + a.source : '')}</div>
        </div>`;
      })}
    </div>`;
  }

  // RelatedList shows a task's related URLs ([N] 一言 / URL) in the detail
  // box, or nothing when it has none. A task from before related URLs keeps
  // its one link in the name and has no list.
  function RelatedList(p) {
    var links = p.t.links || [];
    if (links.length === 0) return null;
    return html`<div class="d-sec">
      <h2>関連 URL</h2>
      <ul class="d-links">
        ${links.map(function (l) {
          return html`<li key=${l.n}>
            <span class="ln">${'[' + l.n + ']'}</span>
            <a href=${l.url} target="_blank" rel="noopener noreferrer" title=${l.url}>${l.label || l.url}</a>
          </li>`;
        })}
      </ul>
    </div>`;
  }

  // ---- 操作ボタン ----

  // Act is a button that runs one request. It is held shut once pressed (the
  // task usually leaves the row's section, and a second press would send a
  // second request); a failure opens it again and says why.
  function Act(p) {
    var st = useState(false);
    var off = st[0];
    var setOff = st[1];
    var guard = useRef(false);
    function onClick() {
      if (guard.current) return;
      guard.current = true;
      setOff(true);
      p.run().catch(function (err) { guard.current = false; setOff(false); p.ctx.showError(err); });
    }
    return html`<button type="button" class=${p.cls} title=${p.title} disabled=${off} onClick=${onClick}>${p.label}</button>`;
  }

  // CopyUrl puts taskUrl on the clipboard. Success is said on the button
  // itself, not in the shared notice: a copy often follows a blur that saves,
  // and the notice is where a failed save is reported. The clipboard API needs
  // a secure context; 127.0.0.1 counts as one, but if it is refused anyway the
  // URL is shown so it can still be copied by hand.
  function CopyUrl(p) {
    var st = useState(false);
    var copied = st[0];
    var setCopied = st[1];
    var timer = useRef(null);
    useEffect(function () { return function () { window.clearTimeout(timer.current); }; }, []);
    function onClick() {
      var url = taskUrl(p.t);
      if (!navigator.clipboard || !navigator.clipboard.writeText) {
        p.ctx.showError(new Error('クリップボードに書けない: ' + url));
        return;
      }
      navigator.clipboard.writeText(url)
        .then(function () {
          setCopied(true);
          window.clearTimeout(timer.current);
          timer.current = window.setTimeout(function () { setCopied(false); }, 1500);
        })
        .catch(function (err) {
          p.ctx.showError(new Error('クリップボードに書けない（' + err.message + '）: ' + url));
        });
    }
    return html`<button type="button" title=${taskUrl(p.t) + '\nAI にこのタスクを伝えるときに貼る'}
      onClick=${onClick}>${copied ? 'コピーした' : 'URLをコピー'}</button>`;
  }

  // WaitControl asks who holds the ball before moving a task out of the queue:
  // the elapsed count has no meaning without it.
  function WaitControl(p) {
    var st = useState(false);
    var asking = st[0];
    var setAsking = st[1];
    var whoRef = useRef(null);
    var guard = useState(false);
    var sending = guard[0];
    var setSending = guard[1];
    var t = p.t;
    var ctx = p.ctx;

    useLayoutEffect(function () { if (asking && whoRef.current) whoRef.current.focus(); }, [asking]);

    function submit() {
      var who = whoRef.current;
      if (!who.value.trim()) { who.focus(); return; }
      setSending(true);
      request('POST', '/api/tasks/' + encodeURIComponent(t.id) + '/wait',
        { to: who.value.trim(), note: '' })
        .then(function () { ctx.closeRow(t.id); return ctx.load().then(function () { ctx.flash('待ちへ移した'); }); })
        .catch(function (err) { setSending(false); ctx.showError(err); });
    }

    if (!asking) {
      return html`<span><button type="button" title="相手に渡す。経過日数が数え始められる"
        onClick=${function () { setAsking(true); }}>待ちにする</button></span>`;
    }
    return html`<span>
      <input ref=${whoRef} type="text" placeholder="渡した相手" aria-label="渡した相手"
        onKeyDown=${function (e) {
          if (composing(e)) return;
          if (e.key === 'Enter') { e.preventDefault(); submit(); }
          if (e.key === 'Escape') setAsking(false);
        }} />
      <button type="button" class="primary" disabled=${sending} onClick=${submit}>渡した</button>
    </span>`;
  }

  // ParentPicker ties a task to a parent, or unties it. It lists the long-run
  // tasks and the queue, leaving out the task itself, what hangs under it and
  // anything that would push the tree past MAX_DEPTH. The server checks the
  // same rules (and the due dates, which the list cannot judge).
  function ParentPicker(p) {
    var t = p.t;
    var ctx = p.ctx;
    var st = useState(null);
    var picked = st[0]; // what was chosen while the change is out; null otherwise
    var setPicked = st[1];

    var board = ctx.state.board || {};
    var facts = treeFacts(board);
    var height = facts.height(t.id);
    var below = {};
    (function mark(id, hops) {
      if (hops > 12) return;
      (facts.kids[id] || []).forEach(function (c) { below[c] = true; mark(c, hops + 1); });
    })(t.id, 0);
    function offered(x) {
      return x.id !== t.id && !below[x.id] && (x.parent_path || []).length + 1 + height <= MAX_DEPTH;
    }
    var listed = {};
    function group(title, tasks) {
      var rows = tasks.filter(offered);
      if (!rows.length) return null;
      return html`<optgroup label=${title}>${rows.map(function (x) {
        listed[x.id] = true;
        return html`<option key=${x.id} value=${x.id}>${x.id + ' ' + x.name}</option>`;
      })}</optgroup>`;
    }
    var longGroup = group('長期', board.long || []);
    var queueGroup = group('着手順', board.queue || []);
    // The parent is somewhere the list does not offer (waiting, someday,
    // finished): show the link as it is, so the picker tells the truth and
    // なし takes it off.
    var kept = t.parent && !listed[t.parent]
      ? html`<option key="kept" value=${t.parent}>${t.parent + (t.parent_name ? ' ' + t.parent_name : '')}</option>` : null;

    function onChange(ev) {
      var value = ev.target.value;
      setPicked(value);
      request('PATCH', '/api/tasks/' + encodeURIComponent(t.id), { parent: value })
        .then(function () {
          return ctx.load().then(function () { ctx.flash(value ? value + ' の子にした' : '親を外した'); });
        })
        .catch(ctx.reloadShowing)
        // The picker stays in the row: it shows the task's own parent again,
        // whether the change landed or was refused.
        .then(function () { setPicked(null); });
    }
    return html`<label class="parent-pick">親<select value=${picked !== null ? picked : (t.parent || '')}
      disabled=${picked !== null} onChange=${onChange}>
      <option value="">なし</option>
      ${longGroup}
      ${queueGroup}
      ${kept}
    </select></label>`;
  }

  // TrailLinks is the one-line history: a link whose tooltip holds the whole
  // thing (listing it would push the row open by as many lines as the task has
  // events, and it is read rarely), and, when something is still unexplained, a
  // link to write the reason for the latest one. The trail is fetched per task,
  // so opening one row does not pull every task's history.
  function TrailLinks(p) {
    var t = p.t;
    var ctx = p.ctx;
    var st = useState(null);
    var got = st[0]; // null until the first reply; then { events } or { error }
    var setGot = st[1];
    var ed = useState(false);
    var editing = ed[0];
    var setEditing = ed[1];
    var inputRef = useRef(null);
    var done = useRef(false);
    var rb = useState(0); // counts the reasons written: the trail is read again after one
    var reasons = rb[0];
    var setReasons = rb[1];

    useEffect(function () {
      var live = true;
      request('GET', '/api/tasks/' + encodeURIComponent(t.id) + '/context')
        .then(function (data) { if (live) setGot({ events: data.events || [] }); })
        .catch(function (err) { if (live) setGot({ error: err.message }); });
      return function () { live = false; };
    }, [t.id, p.rev, reasons]);

    useLayoutEffect(function () { if (editing && inputRef.current) inputRef.current.focus(); }, [editing]);

    var events = got && got.events;
    var text = '経緯';
    var title;
    if (got && got.error !== undefined) {
      text = '経緯を読めない';
      title = got.error;
    } else if (events) {
      if (events.length === 0) {
        text = '経緯なし';
      } else {
        text = '経緯 ' + events.length + '件';
        title = events.slice().reverse().map(function (e) {
          var why = e.reason ? '\n    理由: ' + e.reason : (needsReason(e) ? '\n    理由なし' : '');
          return whenText(e.at) + '  ' + eventWhat(e) + why;
        }).join('\n');
      }
    }
    var unexplained = (events || []).filter(needsReason);
    var target = unexplained[unexplained.length - 1];

    // openReasonEditor is the human side of `todo ask`: the question can be
    // answered where it is asked, instead of only from the terminal.
    function start() {
      done.current = false;
      ctx.state.busy = true;
      setEditing(true);
    }
    function finish(save) {
      if (done.current) return;
      done.current = true;
      ctx.state.busy = false;
      var value = inputRef.current ? inputRef.current.value.trim() : '';
      if (!save || !value) { setEditing(false); return; }
      request('POST', '/api/events/' + encodeURIComponent(target.id) + '/why', { reason: value })
        .then(function () { return ctx.load().then(function () { setReasons(function (n) { return n + 1; }); setEditing(false); ctx.flash('理由を記録した'); }); })
        .catch(function (err) { ctx.showError(err); setEditing(false); });
    }
    // The reason input goes with the panel while it has the focus: no blur comes.
    useEffect(function () {
      return function () {
        var n = inputRef.current;
        if (n && n.contains(document.activeElement)) ctx.releaseBusy();
      };
    }, []);

    return [
      html`<span key="trail" class="trail-link" title=${title}>${text}</span>`,
      editing
        ? html`<input key="reason-input" ref=${inputRef} type="text" placeholder="なぜそうしたか" aria-label="理由"
            onKeyDown=${function (ev) {
              if (composing(ev)) return;
              if (ev.key === 'Enter') { ev.preventDefault(); finish(true); }
              if (ev.key === 'Escape') finish(false);
            }}
            onBlur=${function () { finish(true); }} />`
        : html`<span key="reason-link" class="reason-link" hidden=${unexplained.length === 0}
            title=${target ? (KIND_LABEL[target.kind] || target.kind) + changeOf(target) + ' の理由' : undefined}
            onClick=${function () { if (target) start(); }}>${unexplained.length ? '理由を書く' + (unexplained.length > 1 ? '（' + unexplained.length + '件）' : '') : ''}</span>`,
    ];
  }

  // ---- パネル ----

  // Panel is the open row's detail: what I wrote above what happened. The
  // detail is edited where it is read: no label, no box, no save button.
  function Panel(p) {
    var ctx = p.ctx;
    var held = useHeld(p.t, p.rev, ctx);
    var t = held.t;
    var st = useState({ text: '', cls: 'save-state' });
    var saveState = st[0];
    var setSaveState = st[1];
    var fade = useRef(null);

    // The fields report "保存中…" / "保存した" through markSave, by task id.
    useEffect(function () {
      function set(text, saved) {
        window.clearTimeout(fade.current);
        setSaveState({ text: text, cls: 'save-state on' + (saved ? ' saved' : '') });
        if (saved) {
          fade.current = window.setTimeout(function () {
            setSaveState(function (prev) { return { text: prev.text, cls: 'save-state saved' }; });
          }, 1600);
        }
      }
      saveMarks[t.id] = set;
      return function () {
        window.clearTimeout(fade.current);
        if (saveMarks[t.id] === set) delete saveMarks[t.id];
      };
    }, [t.id]);

    function post(path, body) {
      return request('POST', '/api/tasks/' + encodeURIComponent(t.id) + path, body);
    }

    // Only queue rows offer focus: the band shows nothing else, so the button
    // would claim a move that does not happen for waiting or someday rows. It is
    // the keyboard's way to do what dragging between the band and the list does.
    var focusBtn = t.state === 'open' ? html`<${Act} key="focus" ctx=${ctx} cls=${t.focused ? 'focus-on' : ''}
      label=${t.focused ? 'フォーカスを外す' : 'フォーカス'}
      title=${t.focused ? 'フォーカスから外す（一覧の先頭へ移る）' : 'フォーカスに入れる（フォーカスの末尾へ移る）'}
      run=${function () {
        var was = t.focused;
        return post('/focus', { on: !was }).then(function () {
          ctx.closeRow(t.id);
          return ctx.load().then(function () { ctx.flash(was ? 'フォーカスから外した' : 'フォーカスに入れた'); });
        });
      }} />` : null;

    // longButton moves a task into the long-run band or back to the queue. A
    // child is not a long-run task itself (one level only).
    var longOn = t.state !== 'long';
    var longBtn = (t.state === 'open' && !t.parent) || t.state === 'long' ? html`<${Act} key="long" ctx=${ctx}
      cls=${longOn ? '' : 'long-on'} label=${longOn ? '長期にする' : '長期を外す'}
      title=${longOn ? '着手順から外し、長期の枠に置く（フォーカスも外れる）' : '着手順の先頭（フォーカスのすぐ下）へ戻す'}
      run=${function () {
        return post('/long', { on: longOn }).then(function () {
          ctx.closeRow(t.id);
          return ctx.load().then(function () { ctx.flash(longOn ? '長期にした' : '長期から外した'); });
        });
      }} />` : null;

    // Any task that is not done can have children, down to MAX_DEPTH levels.
    var childBtn = t.state !== 'done' && (t.parent_path || []).length + 1 < MAX_DEPTH
      ? html`<button key="child" type="button" title=${t.id + ' の子としてタスクを追加する'}
          onClick=${function (ev) {
            ev.stopPropagation(); // the row underneath must not open or close
            ctx.addChild(t);
          }}>子を追加</button>` : null;

    // A long-run task is the top of a tree, so it has no parent to choose.
    var picker = t.state !== 'long' && t.state !== 'done' ? html`<${ParentPicker} key="parent" t=${t} ctx=${ctx} />` : null;

    var resume = t.state === 'waiting' || t.state === 'someday' ? html`<${Act} key="resume" ctx=${ctx}
      label="着手順へ戻す"
      title=${t.state === 'waiting'
        ? ((t.handed_on || '?') + ' に ' + (t.handed_to || '?') + ' へ渡した分を戻す')
        : ((t.until || '?') + ' まで棚上げしていた分を戻す')}
      run=${function () {
        return post('/resume', { note: '' })
          .then(function () { ctx.closeRow(t.id); return ctx.load().then(function () { ctx.landed(t); }); });
      }} />` : null;

    // somedayButton shelves a task with one click: unlike WaitControl, nobody
    // holds it, so there is nothing to ask before sending the request. The
    // until date is left to the server, which sets it a month out.
    var someday = t.state === 'waiting' ? html`<${Act} key="someday" ctx=${ctx} label="いつかにする"
      title="いまは着手順に置かず、いつか再開する（1ヶ月後に見る番が来る）"
      run=${function () {
        return post('/someday', {})
          .then(function () { ctx.closeRow(t.id); return ctx.load().then(function () { ctx.flash('いつかへ棚上げした'); }); });
      }} />` : null;

    // いつかにする lives in the due chip's menu for open rows.
    var wait = t.state === 'open' ? html`<${WaitControl} key="wait" t=${t} ctx=${ctx} />` : null;

    return html`<div class="panel">
      <${LiveText} tag="div" className="live detail" placeholder="詳細を書く（任意）" field="detail"
        value=${t.detail || ''} t=${t} ctx=${ctx} />
      <${TopicField} t=${t} id=${'topic-' + t.id} inRow=${true} ctx=${ctx} />
      <${StatusBlock} t=${t} ctx=${ctx} />
      <${LinksBlock} t=${t} ctx=${ctx} />
      <${Supplements} t=${t} />
      <div class="panel-actions">
        ${focusBtn}
        ${longBtn}
        ${childBtn}
        ${picker}
        <${CopyUrl} t=${t} ctx=${ctx} />
        <span class=${saveState.cls} data-save="1">${saveState.text}</span>
        <span class="spacer"></span>
        ${resume}
        ${someday}
        ${wait}
        <${TrailLinks} t=${t} rev=${held.rev} ctx=${ctx} />
        ${t.state === 'waiting' && t.handed_to ? html`<span class="handoff-note">${t.handed_on + ' に ' + t.handed_to + ' へ渡した'
          + (t.wait_note ? ' · ' + t.wait_note : '')}</span>` : null}
      </div>
    </div>`;
  }

  // ---- 一覧の横の詳細欄 ----

  // The side box keeps the trail of the tasks it was shown, by id. A trail
  // is read again when the board has moved on (rev).
  var trailCache = {};

  // SideTrail is the box's history list. The cached copy shows at once while
  // a newer one is read.
  function SideTrail(p) {
    var id = p.t.id;
    var st = useState(function () { return trailCache[id] || null; });
    var got = st[0];
    var setGot = st[1];
    useEffect(function () {
      var live = true;
      request('GET', '/api/tasks/' + encodeURIComponent(id) + '/context')
        .then(function (data) {
          var next = { events: data.events || [] };
          trailCache[id] = next;
          if (live) setGot(next);
        })
        .catch(function () { if (live) setGot({ error: true }); });
      return function () { live = false; };
    }, [id, p.rev]);

    var items;
    if (got && got.events) {
      items = got.events.length === 0
        ? html`<li>経緯なし</li>`
        : got.events.slice().reverse().map(function (e, i) {
          return html`<li key=${i}><time>${whenText(e.at)}</time>${eventWhat(e) + (e.reason ? ' — ' + e.reason : '')}</li>`;
        });
    } else if (got && got.error) {
      items = html`<li>経緯を読めない</li>`;
    } else {
      items = html`<li>読み込み中</li>`;
    }
    return html`<div class="d-sec">
      <h2>経緯</h2>
      <ul class="d-trail">${items}</ul>
    </div>`;
  }

  // Side is the box beside the list. It reads; every change is still made in
  // the row (the topic is the one field it takes), so the box can disappear at
  // narrow widths without taking any operation with it.
  function Side(p) {
    var t = p.t;
    return [
      html`<div key="eyebrow" class="d-eyebrow"><span>${t.id}</span><span>${detailState(t)}</span></div>`,
      html`<p key="name" class="d-name">${window.workbench.todoList.nameParts(t)}</p>`,
      html`<${TopicField} key="topic" t=${t} id=${'d-topic-' + t.id} inRow=${false} ctx=${p.ctx} />`,
      html`<div key="body" class=${'d-body' + (t.detail ? '' : ' none')}>${t.detail || '詳細なし'}</div>`,
      html`<${RelatedList} key="related" t=${t} />`,
      html`<${Supplements} key="supplements" t=${t} />`,
      html`<${SideTrail} key="trail" t=${t} rev=${p.rev} />`,
    ];
  }

  // renderSide draws task t into the box (the aside element). Keyed by the
  // task, so another task is a new box and the same one only updates.
  function renderSide(container, t, rev, ctx) {
    P.render(html`<${Side} key=${t.id} t=${t} rev=${rev} ctx=${ctx} />`, container);
  }

  window.workbench = window.workbench || {};
  window.workbench.todoDetail = {
    NameField: NameField,
    Panel: Panel,
    renderSide: renderSide,
  };
})();
