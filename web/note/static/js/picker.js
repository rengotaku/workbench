// A topic input with suggestions, used by the add screen ("他 N" and "＋ 話題を
// 追加") and the timeline filter ("他 N"). Typing narrows the topics by partial
// match; ↑ ↓ move, Enter picks, Esc closes. With allowCreate, a name that is not
// among the topics can be confirmed as a new one.
// A topic input with suggestions, used by the add screen ("他 N" and "＋ 話題を
// 追加") and the timeline filter ("他 N"). Typing narrows the topics by partial
// match; ↑ ↓ move, Enter picks, Esc closes. With allowCreate, a name that is not
// among the topics can be confirmed as a new one.
//
// TopicPicker is the Preact component (ADR todo-0025); the add screen draws it
// inside its own tree. The timeline is still plain JS, so createPicker draws a
// TopicPicker into a host element and hands back {el, input} as it always did.
// The input is uncontrolled: the element holds the typed text and Preact never
// writes its value, so a draw keeps the text, the focus and the caret.
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var render = P.render;
  var useState = P.useState;
  var useRef = P.useRef;
  var useLayoutEffect = P.useLayoutEffect;

  var MAX_SHOWN = 8;

  function candidates(topics, allowCreate, q) {
    var needle = q.toLowerCase();
    var hits = topics.filter(function (t) { return t.toLowerCase().indexOf(needle) !== -1; });
    // An exact match goes first so Enter on a typed name picks that topic.
    hits.sort(function (a, b) { return (b === q ? 1 : 0) - (a === q ? 1 : 0); });
    var out = hits.slice(0, MAX_SHOWN).map(function (t) { return { name: t, isNew: false }; });
    if (allowCreate && q !== '' && topics.indexOf(q) === -1) out.push({ name: q, isNew: true });
    return out;
  }

  var pickerSeq = 0;

  // TopicPicker props:
  //   topics      names to suggest
  //   allowCreate offer "「q」を新しい話題として作る"
  //   label       aria-label of the input
  //   initial     text the input starts with
  //   focus       take the focus when drawn
  //   onPick(name, isNew)
  //   onClose()   Esc, or focus left with nothing typed
  // A pick of an existing topic ends the picker (the caller removes it). A new
  // name leaves it open while the caller creates the topic, and Esc can still
  // close it then; a second pick is ignored.
  function TopicPicker(p) {
    var inputRef = useRef(null);
    var idRef = useRef(null);
    if (!idRef.current) idRef.current = 'suggest-' + (++pickerSeq);
    var flags = useRef({ closed: false, picked: false });
    var typed = useState(p.initial || '');
    var hlState = useState(0);

    var items = candidates(p.topics, p.allowCreate, typed[0].trim());
    var hl = Math.min(hlState[0], Math.max(items.length - 1, 0));
    var listId = idRef.current;

    // The element is made once with its text; from then on the text is its own.
    useLayoutEffect(function () {
      inputRef.current.value = p.initial || '';
      if (p.focus) inputRef.current.focus();
    }, []);

    function pick(it) {
      var f = flags.current;
      if (f.closed || f.picked) return;
      if (it.isNew) f.picked = true; else f.closed = true;
      p.onPick(it.name, it.isNew);
    }
    function close() {
      var f = flags.current;
      if (f.closed) return;
      f.closed = true;
      p.onClose();
    }

    return html`<div class="suggest-wrap">
      <input ref=${inputRef} type="text" class="topic-q" autocomplete="off" placeholder="話題を探す"
        role="combobox" aria-expanded="true" aria-autocomplete="list" aria-controls=${listId}
        aria-activedescendant=${items.length ? listId + '-' + hl : undefined}
        aria-label=${p.label || '話題を探す'}
        onInput=${function (e) { typed[1](e.target.value); hlState[1](0); }}
        onKeyDown=${function (e) {
          if (window.noteShared.composing(e)) return;
          if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); return; }
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault();
            if (items.length === 0) return;
            hlState[1]((hl + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length);
            return;
          }
          if (e.key === 'Enter') {
            e.preventDefault();
            if (items[hl]) pick(items[hl]);
          }
        }}
        onBlur=${function () { if (!inputRef.current.value.trim()) close(); }} />
      <ul class="suggest" id=${listId} role="listbox" hidden=${items.length === 0}>
        ${items.map(function (it, i) {
          var cls = [i === hl ? 'hl' : '', it.isNew ? 'create' : ''].filter(Boolean).join(' ');
          // mousedown, not click: the input would lose focus (and close) first.
          return html`<li key=${(it.isNew ? '+' : '=') + it.name} id=${listId + '-' + i} role="option"
            aria-selected=${String(i === hl)} class=${cls || undefined}
            onMouseDown=${function (e) { e.preventDefault(); pick(it); }}>${it.isNew ? '「' + it.name + '」を新しい話題として作る' : it.name}</li>`;
        })}
      </ul>
    </div>`;
  }

  // createPicker returns {el, input} for plain-JS callers (the timeline). The
  // caller puts el in the page, calls input.focus(), and removes el when
  // onPick or onClose runs; removing el also takes the component down.
  //   opts.topics      names to suggest
  //   opts.allowCreate offer "「q」を新しい話題として作る"
  //   opts.label       aria-label of the input
  //   opts.initial     text the input starts with
  //   opts.onPick(name, isNew)
  //   opts.onClose()   Esc, or focus left with nothing typed
  function createPicker(opts) {
    var host = document.createElement('div');
    host.style.display = 'contents'; // the .suggest-wrap inside is the row's item
    render(html`<${TopicPicker} topics=${opts.topics} allowCreate=${!!opts.allowCreate} label=${opts.label}
      initial=${opts.initial} onPick=${opts.onPick} onClose=${opts.onClose} />`, host);
    var removeHost = Element.prototype.remove;
    host.remove = function () {
      render(null, host);
      removeHost.call(host);
    };
    return { el: host, input: host.querySelector('input') };
  }

  // fitChips keeps a plain-JS one-line chip row on one line (the timeline's;
  // the add screen folds its chips itself, useFit in add.js). It hides topic chips
  // (elements with data-topic) from the end until the row no longer overflows,
  // and shows how many are folded away on the "他 N" button. Chips that are
  // selected (.on / aria-pressed=true) stay, and so does everything that has no
  // data-topic (すべて, 話題なし, buttons, the count on the right). Under 560px
  // the row wraps and nothing is hidden.
  //   extra: names already folded away by the page (not in the row at all)
  // Returns the names of the chips hidden here, so the caller can offer them
  // together with extra. It does nothing while a picker is open in the row.
  var narrow = window.matchMedia('(max-width: 560px)');
  function fitChips(row, more, extra) {
    if (row.querySelector('.suggest-wrap')) return [];
    var hideable = Array.prototype.slice.call(row.querySelectorAll('[data-topic]')).filter(function (c) {
      return !c.classList.contains('on') && c.getAttribute('aria-pressed') !== 'true';
    });
    var all = Array.prototype.slice.call(row.querySelectorAll('[data-topic]'));
    all.forEach(function (c) { c.hidden = false; });
    var hidden = [];
    function label() {
      var n = extra.length + hidden.length;
      more.textContent = '他 ' + n + ' ▾';
      more.hidden = n === 0;
    }
    label();
    if (narrow.matches) return hidden;
    // The button must be in the row while measuring, with its widest label.
    more.hidden = false;
    while (row.scrollWidth > row.clientWidth + 1) {
      var last = hideable.filter(function (c) { return !c.hidden; }).pop();
      if (!last) break;
      last.hidden = true;
      hidden.unshift(last.dataset.topic);
      label();
      more.hidden = false;
    }
    label();
    return hidden;
  }

  // watchChips runs fit now, when the row's width changes, and when fonts
  // finish loading (they change chip widths).
  function watchChips(row, fit) {
    fit();
    if (window.ResizeObserver) new ResizeObserver(fit).observe(row);
    window.addEventListener('resize', fit);
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(fit);
  }

  window.noteShared.createPicker = createPicker;
  window.noteShared.TopicPicker = TopicPicker;
  window.noteShared.fitChips = fitChips;
  window.noteShared.watchChips = watchChips;
})();
