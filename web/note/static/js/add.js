// The add screen: one field, plus optional related-URL rows numbered [1], [2]…
// that the memo text can point at. Enter saves the memo and empties the
// fields for the next one. A topic chip, when picked, stays picked between
// saves.
//
// It is drawn with Preact (ADR todo-0025). The memo field is uncontrolled: the
// element holds the text and Preact never writes its value, so a draw (a chip
// pressed, a row added, a reply landing) keeps what is typed, the focus and the
// caret. The related-URL rows are links.js, the topic input is picker.js.
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
  var links = window.workbench.links;
  var TopicPicker = shared.TopicPicker;
  var useFit = shared.useFit;

  // How many topic chips the row shows before folding the rest into "他 N"
  // (visibleTopics in internal/notepages).
  var VISIBLE_TOPICS = 6;

  // active is true while the screen is on show: the Escape listener on
  // document stays for the life of the page and must not act from a hidden
  // screen.
  var active = false;

  // byRecent orders topic details by their newest memo, newest first; topics
  // with no memo keep their order, after the rest (note.RankByRecent).
  function byRecent(details) {
    return details
      .map(function (d, i) { return { name: d.name, at: d.last_used_at ? Date.parse(d.last_used_at) : null, i: i }; })
      .sort(function (a, b) {
        if (a.at === null && b.at === null) return a.i - b.i;
        if (a.at === null) return 1;
        if (b.at === null) return -1;
        return b.at - a.at || a.i - b.i;
      });
  }

  // What happened, then what to do next.
  function failure(what, err, next) {
    if (shared.unreachable(err)) {
      return what + '。サーバに接続できません。note-stock のサーバが起動しているか確認してから、' + next + '。';
    }
    return what + '。' + err.message + '。' + next + '。';
  }

  function two(n) { return (n < 10 ? '0' : '') + n; }
  function stamp(iso) {
    var d = new Date(iso);
    return two(d.getMonth() + 1) + '/' + two(d.getDate()) + ' ' + two(d.getHours()) + ':' + two(d.getMinutes());
  }

  // LinkedText draws text with each [N] that names one of links ({n, url,
  // label}) as a link to its URL, as on the timeline; the rest is plain text.
  function LinkedText(p) {
    var byN = {};
    (p.links || []).forEach(function (l) { byN[l.n] = l; });
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var out = [];
    var at = 0;
    var m;
    while ((m = re.exec(p.text)) !== null) {
      var l = byN[Number(m[1])];
      if (!l) continue;
      if (m.index > at) out.push(p.text.slice(at, m.index));
      out.push(html`<a class="ref" href=${l.url} target="_blank" rel="noopener noreferrer"
        title=${l.label ? l.label + ' — ' + l.url : l.url} draggable=${false}>${m[0]}</a>`);
      at = m.index + m[0].length;
    }
    if (at < p.text.length) out.push(p.text.slice(at));
    return out;
  }

  // ---- the screen ----

  function AddScreen(p) {
    var memoRef = useRef(null);
    var rowRef = useRef(null);

    // The failure band: appears for failures only and stays until the next success.
    var statusState = useState('');
    var status = statusState[0];
    function show(text) { statusState[1](text); }
    function clearStatus() { statusState[1](''); }

    var busyState = useState(false);
    var busy = busyState[0];
    var guard = useRef(false); // a save is out

    // ---- topics ----

    // The chips come from the server: the most recently used VISIBLE_TOPICS,
    // the rest folded into "他 N" (the same split the timeline uses,
    // splitTopics in internal/notepages). The picked topic and the list of
    // every topic (the input suggests the folded ones too) are kept in refs as
    // well, for the replies that land later.
    var selState = useState('');
    var selected = selState[0];
    var selRef = useRef('');
    var chipsState = useState([]); // names of the chips, in row order
    var chipNames = chipsState[0];
    var allState = useState([]);
    var allTopics = allState[0];
    var allRef = useRef([]);
    var pickerState = useState(null); // {key, from, suggest, initial, onPick, onClose}
    var picker = pickerState[0];
    var pickerKey = useRef(0);

    // selectionVersion counts selection changes, so a late reply can tell that
    // the person chose something else while it was in flight.
    var selectionVersion = useRef(0);
    function select(name) {
      selectionVersion.current++;
      selRef.current = name;
      selState[1](name);
    }
    function setAll(list) {
      allRef.current = list;
      allState[1](list);
    }
    function addChip(name) {
      chipsState[1](function (list) { return list.indexOf(name) === -1 ? list.concat([name]) : list; });
      return name;
    }

    // topicSeq numbers the reads: a reply that is not the newest is dropped, so
    // an old list cannot bring back a topic renamed or removed since. Creating
    // a topic bumps it too, so a list read before the topic existed is dropped.
    var topicSeq = useRef(0);
    // It runs on every show: a topic renamed, merged or deleted elsewhere must
    // not keep its chip, or saving with it would bring the topic back. The
    // picked topic stays picked while it still exists.
    function loadTopics() {
      var mine = ++topicSeq.current;
      return shared.request('GET', '/api/topics')
        .then(function (res) {
          if (mine !== topicSeq.current) return;
          var all = (res.topics || []).slice();
          setAll(all);
          var names = byRecent(res.details || []).slice(0, VISIBLE_TOPICS).map(function (t) { return t.name; });
          var keep = selRef.current !== '' && all.indexOf(selRef.current) !== -1;
          if (keep && names.indexOf(selRef.current) === -1) names.push(selRef.current);
          chipsState[1](names);
          select(keep ? selRef.current : '');
        })
        .catch(function (err) {
          if (mine !== topicSeq.current) return;
          // The picked topic may have been renamed or removed meanwhile; saving
          // with it would bring it back.
          select('');
          show(failure('話題を読み込めませんでした', err, 'ページを再読み込みしてください'));
        });
    }

    // A save waits for the topic read in flight: until it lands, the picked
    // topic may be one renamed or removed while another screen was on show.
    var topicsReady = useRef(Promise.resolve());

    // The topics that have no chip, and the chips folded for want of room;
    // together they are what "他 N" counts and what its input suggests.
    var extra = allTopics.filter(function (t) { return chipNames.indexOf(t) === -1; });
    var fit = useFit(rowRef, chipNames, selected, extra.length, picker !== null);
    var folded = extra.concat(fit.hidden);
    var moreCount = folded.length;

    function focusMemo() {
      var memo = memoRef.current;
      // A disabled field (a save is out) cannot take the focus, so the
      // request waits for the draw that enables it.
      if (memo.disabled) { wantFocus.current = true; return; }
      memo.focus();
    }
    var wantFocus = useRef(false);
    useLayoutEffect(function () {
      if (wantFocus.current && !memoRef.current.disabled) {
        wantFocus.current = false;
        memoRef.current.focus();
      }
    });

    // openPicker shows the input where the button was. A pick of an existing
    // topic selects it; a new name is created on the server first.
    var stalePicker = useRef(null);
    function openPicker(from, suggest, initial) {
      // done: the input is gone. A reply that arrives after that is stale: the
      // person has moved on, so it must not touch the current selection.
      var done = false;
      // Opening another input replaces this one: its replies are stale too.
      if (stalePicker.current) stalePicker.current();
      stalePicker.current = function () { done = true; };
      function close() {
        if (done) return;
        done = true;
        pickerState[1](null);
        focusMemo();
      }
      function onPick(name, isNew) {
        if (!isNew) { select(addChip(name)); close(); return; }
        var sentVersion = selectionVersion.current;
        shared.request('POST', '/api/topics', { name: name })
          .then(function () {
            topicSeq.current++; // a list read before this topic existed must not land
            // The topic exists on the server either way; show its chip, but only
            // select it when the input is still open and nothing else was picked.
            if (allRef.current.indexOf(name) === -1) setAll(allRef.current.concat([name]));
            addChip(name);
            if (done) return;
            if (selectionVersion.current === sentVersion) select(name);
            close();
          })
          .catch(function (err) {
            if (done) return;
            // A name that already exists is not a failure for the person: pick it.
            if (allRef.current.indexOf(name) !== -1) { select(addChip(name)); close(); return; }
            // Reopen with the typed name so it is not lost.
            close();
            show(failure('話題を追加できませんでした', err, 'もう一度名前を入れて Enter を押してください'));
            openPicker(from, suggest, name);
          });
      }
      pickerState[1]({ key: ++pickerKey.current, from: from, suggest: suggest, initial: initial, onPick: onPick, onClose: close });
    }

    // ---- related URLs: one row each, numbered by position ----

    // The memo points at the rows with [N]: a link pasted into it becomes
    // "text [N]" with the URL as related URL N (/shared/js/paste.js). Not
    // while a save holds the rows.
    var rowsRef = useRef(null);
    if (!rowsRef.current) {
      rowsRef.current = links.create({
        text: function () { return memoRef.current.value; },
        subject: '本文',
        focusEmpty: true,
        onError: function (msg) { show(msg); },
        // A message about a row number may no longer hold.
        onRemoved: function () { clearStatus(); focusMemo(); },
        onEnter: function () { save(); },
      });
    }
    var rows = rowsRef.current;

    useLayoutEffect(function () {
      window.workbench.paste.wireLinkPaste(memoRef.current, function (link) {
        if (guard.current) return 0;
        return rows.add(link.url + (link.label ? ' ' + link.label : ''), true);
      }, true);
    }, []);

    // ---- the waiting list: memos the AI has not picked up yet ----

    // The rows, newest saved first: [{note, isNew, undone, saved}]. A row is
    // found by the memo's id: a memo the list read and the save both bring is
    // one row, whichever reply lands first. saved counts the memos saved from
    // this screen; a row remembers the count it was saved at, so a list read
    // before that save cannot drop it.
    var recentState = useState([]);
    var recent = recentState[0];
    var savedCount = useRef(0);
    var pendingSeq = useRef(0);

    function remember(n) {
      var saved = ++savedCount.current;
      recentState[1](function (list) {
        var row = { note: n, isNew: true, undone: false, saved: saved };
        return [row].concat(list.filter(function (r) { return r.note.id !== n.id; }));
      });
    }

    // On a later show (fresh) the list is made to match the server: a memo the
    // AI picked up meanwhile leaves it, so its 削除 cannot remove a handled
    // memo. Rows for memos still waiting stay as they are. pendingSeq numbers
    // the reads: only the newest reply is used, so an old list cannot bring
    // back a memo the AI has picked up since.
    function loadPending(fresh) {
      var mine = ++pendingSeq.current;
      var savedAtRead = savedCount.current;
      shared.request('GET', '/api/notes?pending=1')
        .then(function (res) {
          if (mine !== pendingSeq.current) return;
          var notes = res.notes || [];
          recentState[1](function (list) {
            var waiting = {};
            notes.forEach(function (n) { waiting[n.id] = true; });
            // A memo saved after the read started is not in it, yet waiting.
            var kept = fresh
              ? list.filter(function (r) { return r.saved > savedAtRead || waiting[r.note.id]; })
              : list;
            var have = {};
            kept.forEach(function (r) { have[r.note.id] = true; });
            return kept.concat(notes.filter(function (n) { return !have[n.id]; }).map(function (n) {
              return { note: n, isNew: false, undone: false, saved: 0 };
            }));
          });
        })
        .catch(function (err) {
          if (mine !== pendingSeq.current) return;
          show(failure('補足待ちのメモを読み込めませんでした', err, 'ページを再読み込みしてください'));
        });
    }

    function remove(n) {
      var told = window.workbench.tell('busy');
      shared.request('DELETE', '/api/notes/' + n.id)
        .then(function () {
          recentState[1](function (list) {
            return list.map(function (r) { return r.note.id === n.id ? Object.assign({}, r, { undone: true, isNew: false }) : r; });
          });
          window.workbench.tell('changed', told);
          clearStatus();
          focusMemo();
        })
        .catch(function (err) { show(failure('削除できませんでした', err, 'もう一度「削除」を押してください')); })
        .then(function () { window.workbench.tell('idle', told); });
    }

    // ---- save ----

    function save() {
      topicsReady.current.then(saveNow);
    }

    function saveNow() {
      var text = memoRef.current.value.trim();
      if (!text || guard.current) { focusMemo(); return; }
      var related = rows.read(false);
      if (!related) return;
      var body = { text: text };
      if (selRef.current) body.topic = selRef.current;
      if (related.length) body.links = related;

      // While a save is in flight the rows must not change: the reply clears
      // them, which would drop an edit made after the request was sent.
      guard.current = true;
      busyState[1](true);
      rows.lock(true);
      var told = window.workbench.tell('busy');
      shared.request('POST', '/api/notes', body)
        .then(function (n) {
          remember(n);
          window.workbench.tell('changed', told);
          clearStatus(); // the band is for failures; a success clears an old one
          memoRef.current.value = '';
          rows.set([]);
        })
        .catch(function (err) {
          // Keep what was typed so nothing is lost.
          show(failure('保存できませんでした', err, 'もう一度 Enter を押してください'));
        })
        .then(function () {
          guard.current = false;
          busyState[1](false);
          rows.lock(false);
          focusMemo();
          window.workbench.tell('idle', told);
        });
    }

    useEffect(function () {
      function onKey(e) {
        if (!active) return;
        if (shared.composing(e)) return;
        if (e.key === 'Escape') { window.workbench.leave('/notes/timeline'); }
      }
      document.addEventListener('keydown', onKey);
      return function () { document.removeEventListener('keydown', onKey); };
    }, []);

    // Each time the screen is shown. While another screen was on show, memos
    // may have been saved or picked up and topics renamed or removed: read
    // both again.
    // The topics are read before the memo takes the focus, so a save made at
    // once waits for them (topicsReady) instead of sending a stale pick.
    useLayoutEffect(function () {
      topicsReady.current = loadTopics();
      // autofocus only works once per page; the screen is shown many times.
      focusMemo();
    }, [p.visit]);
    useEffect(function () {
      loadPending(p.visit > 1);
    }, [p.visit]);

    var waiting = recent.filter(function (r) { return !r.undone; }).length;

    return html`<div class="page">
      <div class="frame">
        <header class="hd">
          <h1 class="page-title">メモを残す</h1>
          <a href="/notes/timeline" data-leave="">タイムラインへ <kbd>Esc</kbd></a>
        </header>

        <label class="sr-only" for="memo">メモ</label>
        <input ref=${memoRef} class="big" id="memo" type="text" autofocus autocomplete="off"
          placeholder="リンクと一言を貼って Enter" disabled=${busy}
          onKeyDown=${function (e) {
            if (shared.composing(e)) return;
            if (e.key === 'Enter') { e.preventDefault(); save(); }
          }} />

        <${links.LinkEditor} ctl=${rows} addClass="chip dashed link-add" addId="link-add" />

        <div ref=${rowRef} class="filters one" id="topics" role="group" aria-label="話題（任意）">
          <span>話題（任意）:</span>
          ${chipNames.map(function (name) {
            return html`<button key=${name} type="button" class="chip" data-topic=${name}
              aria-pressed=${String(name === selected)} hidden=${fit.hidden.indexOf(name) !== -1}
              onClick=${function () {
                select(selected === name ? '' : name);
                focusMemo();
              }}>${name}</button>`;
          })}
          <button type="button" class="chip more" id="topic-more"
            hidden=${(fit.done && moreCount === 0) || (picker !== null && picker.from === 'more')}
            onClick=${function () { openPicker('more', folded, ''); }}>${'他 ' + moreCount + ' ▾'}</button>
          ${picker ? html`<${TopicPicker} key=${picker.key} topics=${picker.suggest} allowCreate=${true}
            initial=${picker.initial} label="話題を探す・新しい話題の名前" focus=${true}
            onPick=${picker.onPick} onClose=${picker.onClose} />` : null}
          <button type="button" class="chip dashed" id="topic-add" hidden=${picker !== null && picker.from === 'add'}
            onClick=${function () { openPicker('add', allTopics, ''); }}>＋ 話題を追加</button>
        </div>

        <div class="hint">
          <span>リンクと一言をそのまま貼る。関連 URL は本文で [1] [2] と書いて指せる。補足は後で AI が付ける</span>
          <span><kbd>Enter</kbd> で保存</span>
        </div>

        <div class="status is-error" id="status" role="alert" hidden=${status === ''}>${status}</div>

        <div class="sec"><b id="pending-title">${'補足待ち ' + waiting + ' 件'}</b><span>AI が拾うと一覧から消える</span></div>
        <ul class="recent" id="recent" aria-labelledby="pending-title">
          ${recent.map(function (r) {
            var n = r.note;
            var nLinks = (n.links || []).length;
            return html`<li key=${n.id} data-id=${String(n.id)}
              class=${r.undone ? 'is-undone' : r.isNew ? 'new' : undefined}>
              <span class="rtext">
                <span class="when">${stamp(n.created_at)}</span>${'#' + n.id + '  '}<${LinkedText} text=${n.text} links=${n.links || []} />${(n.topic ? '  [' + n.topic + ']' : '') + (nLinks ? '  ＋関連 ' + nLinks : '')}
              </span>
              ${r.undone
                ? html`<span class="gone">削除しました</span>`
                : html`<button type="button" class="linklike danger" aria-label=${'メモ #' + n.id + ' を削除'}
                  onClick=${function () { remove(n); }}>削除</button>`}
            </li>`;
          })}
        </ul>
      </div>
    </div>`;
  }

  // The root of the screen and the times it has been shown. The router builds
  // the screen once (mount) and shows it many times (show), in the page or in
  // the add dialog; each show draws it again into the same root.
  var screen = { root: null, visit: 0 };

  function draw() {
    render(html`<${AddScreen} visit=${screen.visit} />`, screen.root);
  }

  window.workbench.router.register('note-add', {
    title: 'note',
    mount: function (root) {
      active = true;
      screen.root = root;
    },
    show: function () {
      active = true;
      screen.visit++;
      draw();
    },
    hide: function () {
      active = false;
    },
  });
})();
