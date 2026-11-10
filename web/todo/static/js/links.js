// 関連 URL の行（todo 追加画面と、開いた行の LinksBlock が共有する）を Preact で描く。
// ADR todo-0025。
//
//   create(opts)  行の持ち主（controller）を作る。画面は add / read / set / lock で触る
//   LinkEditor    controller の行と「＋ 関連 URL」のボタンを描く部品
//
// 名前は [N] で行を指す。行は URL ごとに 1 つ、並びの順に番号が付く（サーバも送った順に
// 同じ番号を付ける）。番号・aria-label は行の並びから描くので、外しても振り直しは描き直しで済む。
//
// 行の入力は書き換えない要素（uncontrolled）にする。文字は要素が持ち、Preact は value を
// 書かない。行は key で保つので、別の行を足す・外す描き直しで、打っている行の文字・
// フォーカス・キャレットは消えない。read は要素から文字を読む。
// URL の入った行は、リンク（一言、無ければ URL）と「直す」ボタンに見え、入力欄に
// フォーカスが無く http(s) の URL を持っているときだけそうなる（描き分けは LinkRow が持つ）。
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var useState = P.useState;
  var useRef = P.useRef;
  var useLayoutEffect = P.useLayoutEffect;

  var composing = window.workbench.composing;
  var splitLinkInput = window.workbench.paste.splitLinkInput;

  // The first [N] (N >= k) in text, or "". A removal of row k would move every
  // later row up by one, so a [k] or later in the name would quietly point at
  // a different URL; the editor refuses it instead (the note screens do too).
  function referencedFrom(text, k) {
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var m;
    while ((m = re.exec(text)) !== null) {
      if (Number(m[1]) >= k) return m[0];
    }
    return '';
  }

  // create makes the owner of the rows. It holds the list of rows and tells the
  // LinkEditor drawing them to draw again; the text of each row is in its input.
  //   opts.text()        the name the rows belong to (for the removal check)
  //   opts.subject       what text() is called in the message ("名前" by default;
  //                      the note screen calls it "本文")
  //   opts.onError(msg)  a row cannot be removed or is empty
  //   opts.onChange()    a row was added (with a value), edited or removed
  //   opts.onRemoved()   a row was removed (after onChange)
  //   opts.onEnter()     Enter in a row
  //   opts.focusEmpty    read(false) puts the caret in the empty row it names
  // It returns
  //   add(value, keepFocus)  appends a row, prefilled with value when given, and
  //                          returns its number. It focuses the row unless the
  //                          caret stays where it is (a paste into the name)
  //   read(quiet)            the rows as [{url, label}], or null when a row is
  //                          empty: an empty row in the middle would shift every
  //                          number after it. quiet skips the message (a save on
  //                          the fly that will run again)
  //   set(links)             replaces the rows with links ([{url, label}]); not a
  //                          change, so nothing is saved
  //   lock(locked)           disables the rows and the button
  function create(opts) {
    var seq = 0;
    var ctl = {
      rows: [], // [{key, init, focus, input}]
      locked: false,
      redraw: null, // set by the LinkEditor drawing the rows
    };

    function update() { if (ctl.redraw) ctl.redraw(); }

    // quiet is true while set() lays out rows the task already has: they are
    // not a change, and saving each one as it lands would send a partial list.
    var quiet = false;
    function changed() { if (!quiet && opts.onChange) opts.onChange(); }

    function text(row) { return row.input ? row.input.value : row.init; }

    function add(value, keepFocus) {
      ctl.rows = ctl.rows.concat([{ key: ++seq, init: value || '', focus: !keepFocus, input: null }]);
      update();
      if (value) changed();
      return ctl.rows.length;
    }

    function remove(row) {
      var k = ctl.rows.indexOf(row) + 1;
      var from = referencedFrom(opts.text(), k);
      if (from) {
        var subject = opts.subject || '名前';
        if (opts.onError) opts.onError(subject + 'が ' + from + ' を指しているので [' + k + '] は外せません。' + subject + 'の番号を直してから外してください。');
        return;
      }
      ctl.rows = ctl.rows.filter(function (r) { return r !== row; });
      update();
      // Always a change to look at: a saved URL emptied before its removal
      // was never saved empty, so the removal is what takes it off.
      changed();
      if (opts.onRemoved) opts.onRemoved();
    }

    function read(silent) {
      var out = [];
      for (var i = 0; i < ctl.rows.length; i++) {
        var v = text(ctl.rows[i]).trim();
        if (v === '') {
          if (!silent && opts.onError) opts.onError('関連 URL [' + (i + 1) + '] が空です。URL を入れるか「外す」を押してください。');
          if (!silent && opts.focusEmpty && ctl.rows[i].input) ctl.rows[i].input.focus();
          return null;
        }
        var m = /^(\S+)(?:[\s　]+([\s\S]*))?$/.exec(v);
        out.push({ url: m[1], label: (m[2] || '').trim() });
      }
      return out;
    }

    function set(links) {
      quiet = true;
      ctl.rows = [];
      (links || []).forEach(function (l) { add(l.url + (l.label ? ' ' + l.label : ''), true); });
      quiet = false;
      update();
    }

    function lock(locked) {
      ctl.locked = locked;
      update();
    }

    ctl.add = add;
    ctl.read = read;
    ctl.set = set;
    ctl.lock = lock;
    ctl.remove = remove;
    ctl.changed = changed;
    ctl.enter = function () { if (opts.onEnter) opts.onEnter(); };
    return ctl;
  }

  function openable(value) {
    var l = splitLinkInput(value);
    return l && /^https?:\/\//i.test(l.url) ? l : null;
  }

  // LinkRow is one related URL: its number, the link it shows as, the input it
  // is typed in, and the buttons. It shows as a link that opens in a new tab
  // when the input has lost the focus holding an http(s) URL; an empty or
  // unusable value stays in the input to be fixed.
  function LinkRow(p) {
    var ctl = p.ctl;
    var row = p.row;
    var n = '[' + (p.index + 1) + ']';
    var inputRef = useRef(null);
    var wantFocus = useRef(row.focus);
    var shown = useState(function () { return openable(row.init); });
    var link = shown[0];
    var setLink = shown[1];

    // The input is made once with its text; from then on the text is its own.
    useLayoutEffect(function () {
      var input = inputRef.current;
      input.value = row.init;
      row.input = input;
      // A save queued behind one in flight reads the rows after the row is
      // gone (a closed row): the text goes back to row.init so it is the last
      // typed, not the one the row was made with.
      return function () { row.init = input.value; row.input = null; };
    }, []);

    // The focus waits until the input is on show (直す shows it, then focuses).
    useLayoutEffect(function () {
      if (wantFocus.current && !link) {
        wantFocus.current = false;
        inputRef.current.focus();
      }
    });

    return html`<div class="link-row">
      <span class="ln">${n}</span>
      <a class="link-view" href=${link ? link.url : undefined} target="_blank" rel="noopener noreferrer"
        draggable=${false} title=${link ? link.url : undefined} hidden=${!link}>${link ? (link.label || link.url) : ''}</a>
      <input ref=${inputRef} type="text" class="link-input" autocomplete="off" placeholder="URL と一言（一言は省略可）"
        aria-label=${'関連 URL ' + n + '（URL と一言）'} hidden=${!!link} disabled=${ctl.locked}
        onKeyDown=${function (e) {
          if (composing(e)) return;
          if (e.key === 'Enter') { e.preventDefault(); ctl.enter(); }
        }}
        onChange=${ctl.changed}
        onBlur=${function () { setLink(openable(inputRef.current.value)); }} />
      <button type="button" class="linklike link-edit" aria-label=${'関連 URL ' + n + ' を直す'}
        hidden=${!link} disabled=${ctl.locked}
        onClick=${function () { wantFocus.current = true; setLink(null); }}>直す</button>
      <button type="button" class="linklike link-remove" aria-label=${'関連 URL ' + n + ' を外す'}
        disabled=${ctl.locked} onClick=${function () { ctl.remove(row); }}>外す</button>
    </div>`;
  }

  // LinkEditor draws the rows of ctl (from create) and the button that adds one.
  //   addClass / addId                 class and id of the add button ("link-add" by default)
  //   hostRef                          set to the .link-editor element
  //   onFocusIn / onFocusOut           the focus entered / left the editor
  function LinkEditor(p) {
    var ctl = p.ctl;
    var redraw = useState(0)[1];
    // Set before the first paint, so a row added right after is not missed.
    useLayoutEffect(function () {
      ctl.redraw = function () { redraw(function (n) { return n + 1; }); };
      return function () { ctl.redraw = null; };
    }, [ctl]);

    return html`<div class="link-editor" ref=${p.hostRef} onfocusin=${p.onFocusIn} onfocusout=${p.onFocusOut}>
      <div class="link-rows" role="group" aria-label="関連 URL（任意）">
        ${ctl.rows.map(function (row, i) {
          return html`<${LinkRow} key=${row.key} ctl=${ctl} row=${row} index=${i} />`;
        })}
      </div>
      <button type="button" class=${p.addClass || 'link-add'} id=${p.addId} disabled=${ctl.locked}
        onClick=${function () { ctl.add('', false); }}>＋ 関連 URL</button>
    </div>`;
  }

  window.workbench = window.workbench || {};
  window.workbench.todoLinks = { create: create, LinkEditor: LinkEditor };
})();
