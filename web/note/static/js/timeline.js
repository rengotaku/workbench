// The timeline: memos by day, newest first, drawn from GET /api/notes/timeline.
// The filter chips change the query (?topic= / ?no_topic=1) through the router,
// so filtering reads the timeline again without reloading the page.
// "＋ メモを追加" and N open the add screen in a dialog over the timeline;
// "他 N" picks a topic from the ones folded out of the chips; "話題を整理" opens
// the topic dialog (rename / merge / delete). Either dialog reads the timeline
// again after it closes if something changed.
//
// It is drawn with Preact (ADR todo-0025). The router shows it by drawing
// TimelineScreen again into the same root on every show. The topic dialog is a
// native <dialog> drawn inside the same tree; the component opens it
// (showModal) when the button is pressed, and the dialog's own close (Esc, the
// router leaving the screen) comes back as an event. The rename input is
// uncontrolled: the element holds the text and Preact never writes its value.
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var render = P.render;
  var useState = P.useState;
  var useRef = P.useRef;
  var useEffect = P.useEffect;
  var useLayoutEffect = P.useLayoutEffect;

  var shared = window.noteShared;
  var TopicPicker = shared.TopicPicker;
  var useFit = shared.useFit;

  var TIMELINE_PATH = '/notes/timeline';

  // active is true while the timeline is on show: the N listener on document
  // stays for the life of the page and must not act from a hidden screen.
  var active = false;

  // What happened, then what to do next.
  function failure(what, err) {
    if (shared.unreachable(err)) return what + '。サーバに接続できません。note-stock のサーバが起動しているか確認してから、もう一度押してください。';
    return what + '。' + err.message + '。内容を確認して、もう一度押してください。';
  }

  // An external link, opened in a new tab (as the page did).
  function OutLink(p) {
    return html`<a class=${p.cls} href=${p.url} title=${p.title} target="_blank" rel="noopener noreferrer">${p.text}</a>`;
  }

  function NoteView(p) {
    var n = p.note;
    var parts = [n.time];
    if (n.source) parts.push(html`<span class="src">${n.source}</span>`);
    if (n.topic) parts.push(html`<span class="topic">${n.topic}</span>`);
    parts.push(html`<span class="nid">${'#' + n.id}</span>`);
    if (n.route) {
      parts.push(n.route.href
        ? html`<a class="route" href=${n.route.href}>${n.route.label}</a>`
        : html`<span class="route">${n.route.label}</span>`);
    }
    var meta = [];
    parts.forEach(function (part, i) {
      if (i > 0) meta.push(' · ');
      meta.push(part);
    });

    return html`<article class="ev" id=${'note-' + n.id}>
      <div class="meta">${meta}</div>
      <p class="memo">${n.segments.map(function (s, i) {
        if (s.ref) return html`<${OutLink} key=${i} cls="ref" url=${s.url} title=${s.url} text=${s.text} />`;
        if (s.url) return html`<${OutLink} key=${i} url=${s.url} text=${s.text} />`;
        return s.text;
      })}</p>
      ${n.links.length ? html`<ol class="links" aria-label="関連 URL">
        ${n.links.map(function (l, i) {
          return html`<li key=${i}><span class="ln">${'[' + l.n + ']'}</span>${' '}<span class="src">${l.source}</span>${' '}<${OutLink} url=${l.url} text=${l.label || l.url} /></li>`;
        })}
      </ol>` : null}
      ${n.annotations.map(function (a, i) {
        var who = a.label + ' · ' + a.author + ' · ' + a.when + (a.source ? ' · ' + a.source : '');
        return html`<div key=${i} class=${'add k-' + a.kind}>
          <div class="who">${who}</div>
          ${a.kind === 'quote' ? html`<blockquote class="body">${a.body}</blockquote>` : html`<p class="body">${a.body}</p>`}
        </div>`;
      })}
      ${n.pending ? html`<div class="pending">補足待ち</div>` : null}
    </article>`;
  }

  // ---- 話題を整理: the edit area under a topic's row ----
  // props: topic ({name, count}), topics (all of them), call(method, name,
  // body) (a write; answers {err} on failure, null while another is out),
  // onCancel().

  function RenameEdit(p) {
    var t = p.topic;
    var inputRef = useRef(null);
    var typed = useState(t.name);
    // A message from a refused submit or a failed call; typing clears it.
    var override = useState(null);

    useLayoutEffect(function () {
      inputRef.current.value = t.name;
      inputRef.current.focus();
    }, []);

    var v = typed[0].trim();
    var other = p.topics.filter(function (o) { return o.name === v && o.name !== t.name; })[0];
    var explain = other
      ? '「' + v + '」は既にある話題なので統合になる。「' + t.name + '」の ' + t.count + ' 件を「' + v + '」に付け替え、「' + t.name + '」を一覧から消す'
      : t.count + ' 件のメモも新しい名前になる。既にある話題名を入れると統合として扱う';

    function submit() {
      var name = inputRef.current.value.trim();
      if (name === '' || name === t.name) { override[1]('今と違う名前を入れてください'); return; }
      p.call('PUT', t.name, { name: name }).then(function (r) {
        if (r && r.err) override[1](failure('名前を変えられませんでした', r.err));
      });
    }

    return html`<div class="edit">
      <span>新しい名前</span>
      <input type="text" aria-label="新しい名前" ref=${inputRef}
        onInput=${function (e) { override[1](null); typed[1](e.target.value); }}
        onKeyDown=${function (e) {
          if (shared.composing(e)) return;
          if (e.key === 'Enter') { e.preventDefault(); submit(); }
        }} />
      <button type="button" class="btn" onClick=${submit}>変える</button>
      <button type="button" class="btn plain" onClick=${p.onCancel}>やめる</button>
      <span class=${override[0] === null ? 'warn muted' : 'warn'}>${override[0] === null ? explain : override[0]}</span>
    </div>`;
  }

  function MergeEdit(p) {
    var t = p.topic;
    var others = p.topics.filter(function (o) { return o.name !== t.name; });
    var selRef = useRef(null);
    var val = useState(others.length ? others[0].name : '');
    var override = useState(null);

    useLayoutEffect(function () {
      if (selRef.current) selRef.current.focus();
    }, []);

    if (others.length === 0) {
      return html`<div class="edit">
        <span>統合先</span>
        <button type="button" class="btn plain" onClick=${p.onCancel}>やめる</button>
        <span class="warn">統合先になる話題がありません</span>
      </div>`;
    }
    var explain = '「' + t.name + '」の ' + t.count + ' 件を「' + val[0] + '」に付け替え、「' + t.name + '」を一覧から消す';

    return html`<div class="edit">
      <span>統合先</span>
      <select ref=${selRef} aria-label="統合先" onChange=${function (e) { override[1](null); val[1](e.target.value); }}>
        ${others.map(function (o) { return html`<option key=${o.name} value=${o.name}>${o.name}</option>`; })}
      </select>
      <button type="button" class="btn" onClick=${function () {
        p.call('PUT', t.name, { merge_into: selRef.current.value }).then(function (r) {
          if (r && r.err) override[1](failure('統合できませんでした', r.err));
        });
      }}>統合する</button>
      <button type="button" class="btn plain" onClick=${p.onCancel}>やめる</button>
      <span class="warn">${override[0] === null ? explain : override[0]}</span>
    </div>`;
  }

  function DeleteEdit(p) {
    var t = p.topic;
    return html`<div class="edit del">
      <span>${t.count > 0
        ? '「' + t.name + '」を一覧から消す。付いている ' + t.count + ' 件のメモは話題なしになる'
        : '「' + t.name + '」を一覧から消す。付いているメモは無い'}</span>
      <button type="button" class="btn red" onClick=${function () {
        p.call('DELETE', t.name, null).then(function (r) {
          if (r && r.err) p.showError(failure('削除できませんでした', r.err));
        });
      }}>削除する</button>
      <button type="button" class="btn plain" onClick=${p.onCancel}>やめる</button>
    </div>`;
  }

  var EDITS = { rename: RenameEdit, merge: MergeEdit, del: DeleteEdit };

  // ---- 話題を整理: the dialog ----

  // props: opens (counts the presses of the button; each one opens the dialog
  // and reads the topics), reload() (the timeline reads again).
  function OrganizeDialog(p) {
    var dlgRef = useRef(null);
    var topicsState = useState(null); // [{name, count}] from GET /api/topics; null until read
    var topics = topicsState[0];
    var errorState = useState('');
    var editState = useState(null); // {name, kind, id}
    var edit = editState[0];

    var isOpen = useRef(false);
    var changed = useRef(false);
    var busy = useRef(false);
    var editId = useRef(0);
    // topicSeq numbers the reads and writes of the list: a reply that is not
    // the newest is dropped, so an earlier open's list cannot draw over a later
    // one.
    var topicSeq = useRef(0);
    var reloadRef = useRef(null);
    reloadRef.current = p.reload;

    function showError(msg) { errorState[1](msg); }

    useLayoutEffect(function () {
      if (p.opens === 0) return;
      changed.current = false;
      isOpen.current = true;
      showError('');
      editState[1](null);
      topicsState[1](null);
      if (!dlgRef.current.open) dlgRef.current.showModal();
      var mine = ++topicSeq.current;
      shared.request('GET', '/api/topics')
        .then(function (res) {
          if (mine !== topicSeq.current) return;
          topicsState[1](res.details || []);
        })
        .catch(function (err) {
          if (mine !== topicSeq.current) return;
          showError(failure('話題を読み込めませんでした', err));
        });
    }, [p.opens]);

    // call runs a write. On success the dialog shows the topics the server
    // answered with; a failure comes back as {err}, for the edit area it came
    // from to show. The dialog may have been closed while the write was out:
    // the timeline still has to read again then.
    function call(method, name, body) {
      if (busy.current) return Promise.resolve(null);
      busy.current = true;
      showError('');
      return shared.request(method, '/api/topics/' + encodeURIComponent(name), body)
        .then(function (res) {
          changed.current = true;
          topicSeq.current++;
          editState[1](null);
          topicsState[1](res.details || []);
          if (!isOpen.current) reloadRef.current();
          return { ok: true };
        })
        .catch(function (err) { return { err: err }; })
        .then(function (r) { busy.current = false; return r; });
    }

    function onClosed() {
      isOpen.current = false;
      // A renamed or removed topic changes the chips and the memos' topics.
      if (changed.current) reloadRef.current();
    }

    function startEdit(name, kind) { editState[1]({ name: name, kind: kind, id: ++editId.current }); }

    return html`<dialog class="organize-dialog" id="organize-dialog" aria-labelledby="organize-title" ref=${dlgRef}
      onClose=${onClosed}
      onClick=${function (e) { if (e.target === e.currentTarget) e.currentTarget.close(); }}>
      <div class="dlg-hd"><h2 id="organize-title">話題を整理</h2><button type="button" class="x" id="organize-close" aria-label="閉じる"
        onClick=${function () { dlgRef.current.close(); }}>×</button></div>
      <p class="sub">件数はその話題が付いているメモの数。変更はすべて履歴に残る</p>
      <div class="status is-error" id="organize-error" role="alert" hidden=${errorState[0] === ''}>${errorState[0]}</div>
      <ul class="rows" id="organize-rows">
        ${(topics || []).map(function (t) {
          var Edit = edit && edit.name === t.name ? EDITS[edit.kind] : null;
          return html`<li key=${t.name} class="row">
            <span class="name">${t.name}</span>
            <span class="n">${t.count + ' 件'}</span>
            <span class="acts">
              <button type="button" class="linklike" onClick=${function () { startEdit(t.name, 'rename'); }}>名前を変える</button>
              <button type="button" class="linklike" onClick=${function () { startEdit(t.name, 'merge'); }}>統合</button>
              <button type="button" class="linklike danger" onClick=${function () { startEdit(t.name, 'del'); }}>削除</button>
            </span>
            ${Edit ? html`<${Edit} key=${edit.id} topic=${t} topics=${topics} call=${call} showError=${showError}
              onCancel=${function () { editState[1](null); }} />` : null}
          </li>`;
        })}
        ${topics !== null && topics.length === 0 ? html`<li class="empty">話題はまだありません</li>` : null}
      </ul>
      <p class="foot">メモが付いている話題を削除すると、そのメモは「話題なし」になる。Esc で閉じる</p>
    </dialog>`;
  }

  // ---- the screen ----

  // props: url (the URL object of this show), visit (counts the shows).
  function TimelineScreen(p) {
    var rowRef = useRef(null);
    var pageRef = useRef(null);
    var dataState = useState(null); // the last GET /api/notes/timeline; null until read
    var data = dataState[0];
    var errorState = useState('');
    var pickerState = useState(null); // {topics} while the "他 N" input is open
    var picker = pickerState[0];
    var opensState = useState(0);

    // seq numbers the reads, so a slow reply for an old filter cannot draw
    // over a newer one.
    var seq = useRef(0);
    // The hash of the show whose reply is on its way (#note-12, a link from a
    // todo's source memo): the memo is scrolled to once it is drawn.
    var scrollHash = useRef('');

    // load reads the timeline for url (a URL object) and draws it.
    function load(url) {
      var mine = ++seq.current;
      return shared.request('GET', '/api/notes/timeline' + url.search)
        .then(function (res) {
          if (mine !== seq.current) return;
          errorState[1]('');
          scrollHash.current = url.hash;
          dataState[1](res);
        })
        .catch(function (err) {
          if (mine !== seq.current) return;
          errorState[1](shared.unreachable(err)
            ? 'タイムラインを読み込めませんでした。サーバに接続できません。サーバが起動しているか確認してから、ページを再読み込みしてください。'
            : 'タイムラインを読み込めませんでした。' + err.message + '。ページを再読み込みしてください。');
        });
    }

    // reload reads the timeline again with the filter on screen. The dialogs
    // and the key listener outlive a draw, so they reach it through a ref.
    function reload() { return load(new URL(location.href)); }
    var reloadRef = useRef(null);
    reloadRef.current = reload;

    function openAdd() {
      window.workbench.openAddDialog('/notes', 'メモを追加', function (changed) {
        if (changed) reloadRef.current();
      });
    }
    var openAddRef = useRef(null);
    openAddRef.current = openAdd;

    // Every show reads the timeline: the first one, a new filter (the query
    // changed) and a return from another screen, where memos may have been
    // added or picked up meanwhile. What was drawn stays until the reply.
    var urlRef = useRef(null);
    urlRef.current = p.url;
    useLayoutEffect(function () {
      load(urlRef.current);
    }, [p.visit]);

    useLayoutEffect(function () {
      var hash = scrollHash.current;
      scrollHash.current = '';
      if (!data || !hash) return;
      var target = null;
      try { target = pageRef.current.querySelector(hash.replace(/[^#\w-]/g, '')); } catch (e) { target = null; }
      if (target) target.scrollIntoView();
    }, [data]);

    useEffect(function () {
      function onKey(e) {
        if (!active) return;
        if (shared.composing(e) || e.metaKey || e.ctrlKey || e.altKey) return;
        if (document.querySelector('dialog[open]')) return;
        var t = e.target;
        if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
        if (e.key === 'n' || e.key === 'N') { e.preventDefault(); openAddRef.current(); }
      }
      document.addEventListener('keydown', onKey);
      return function () { document.removeEventListener('keydown', onKey); };
    }, []);

    // ---- the chips ----

    var chips = data ? data.chips : [];
    var names = chips.filter(function (c) { return c.topic; }).map(function (c) { return c.topic; });
    var on = chips.filter(function (c) { return c.on && c.topic; })[0];
    var extra = data ? data.other : [];
    // The chips that do not fit on the line fold into "他 N" with the ones the
    // server already left out.
    var fit = useFit(rowRef, names, on ? on.topic : '', extra.length, picker !== null);
    var folded = extra.concat(fit.hidden);

    return html`<div class="page" ref=${pageRef}>
      <div class="frame">
        <header class="hd">
          <h1 class="page-title">タイムライン</h1>
          <span class="hd-acts">
            <button type="button" class="organize" id="organize" onClick=${function () { opensState[1](opensState[0] + 1); }}><svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6"><path d="M2 4h12M2 8h8M2 12h5"/></svg>話題を整理</button>
            <a href="/notes" id="to-add" onClick=${function (e) {
              // A modified click (new tab, new window) still opens the page itself.
              if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
              e.preventDefault();
              openAdd();
            }}>＋ メモを追加 <kbd>N</kbd></a>
          </span>
        </header>

        <nav ref=${rowRef} class="filters one" id="filters" aria-label="話題で絞り込む">
          <span>話題:</span>
          ${chips.map(function (c) {
            return html`<a key=${c.href} class=${'chip' + (c.on ? ' on' : '')} href=${c.href}
              data-topic=${c.topic || undefined} aria-current=${c.on ? 'page' : undefined}
              hidden=${!!c.topic && fit.hidden.indexOf(c.topic) !== -1}>${c.label}</a>`;
          })}
          ${picker ? html`<${TopicPicker} topics=${picker.topics} allowCreate=${false} label="絞り込む話題を探す" focus=${true}
            onPick=${function (name) {
              pickerState[1](null);
              window.workbench.router.navigate(TIMELINE_PATH + '?topic=' + encodeURIComponent(name));
            }}
            onClose=${function () { pickerState[1](null); }} />` : null}
          <button type="button" class="chip more" id="chip-more"
            hidden=${(fit.done && folded.length === 0) || picker !== null}
            onClick=${function () { pickerState[1]({ topics: folded }); }}>${'他 ' + folded.length + ' ▾'}</button>
          <span class="push" id="tl-pending">${'補足待ち ' + (data ? data.pending : 0)}</span>
        </nav>

        <div id="tl-days">
          ${data ? data.days.map(function (d) {
            return html`<section key=${d.heading}>
              <h2 class="day">${d.heading}</h2>
              ${d.notes.map(function (n) { return html`<${NoteView} key=${n.id} note=${n} />`; })}
            </section>`;
          }) : null}
          ${data && data.days.length === 0 ? html`<p class="empty">${data.empty}</p>` : null}
        </div>
        <div class="status is-error" id="tl-error" role="alert" hidden=${errorState[0] === ''}>${errorState[0]}</div>
      </div>
    </div>
    <${OrganizeDialog} opens=${opensState[0]} reload=${reload} />`;
  }

  // The root of the screen and the times it has been shown. The router builds
  // the screen once (mount) and shows it many times (show); each show draws it
  // again into the same root.
  var screen = { root: null, visit: 0 };

  window.workbench.router.register('timeline', {
    title: 'note タイムライン',
    mount: function (root) {
      active = true;
      screen.root = root;
    },
    show: function (url) {
      active = true;
      screen.visit++;
      render(html`<${TimelineScreen} url=${url} visit=${screen.visit} />`, screen.root);
    },
    hide: function () {
      active = false;
    },
  });
})();
