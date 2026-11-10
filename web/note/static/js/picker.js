// A topic input with suggestions, used by the add screen ("他 N" and "＋ 話題を
// 追加") and the timeline filter ("他 N"). Typing narrows the topics by partial
// match; ↑ ↓ move, Enter picks, Esc closes. With allowCreate, a name that is not
// among the topics can be confirmed as a new one.
//
// TopicPicker is the Preact component (ADR todo-0025); both screens draw it
// inside their own tree. The input is uncontrolled: the element holds the typed
// text and Preact never writes its value, so a draw keeps the text, the focus
// and the caret. useFit keeps a screen's one-line chip row on one line.
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;
  var useState = P.useState;
  var useRef = P.useRef;
  var useEffect = P.useEffect;
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

  // Under 560px the chip row wraps and nothing is folded.
  var narrow = window.matchMedia('(max-width: 560px)');

  // useFit keeps the one-line chip row on one line. It folds topic chips from
  // the end, one more per pass, until the row no longer overflows, and the
  // "他 N" button counts them. The chip that is picked stays. It returns the
  // names folded here and whether the passes are done (the button is drawn
  // while they run, so it is in the measure). Nothing runs while the picker is
  // open in the row (paused).
  function useFit(rowRef, names, selected, extraCount, paused) {
    // One state, so a fit again and a pass that is already under way cannot
    // mix: a pass only applies if nothing asked for a fresh start since.
    var fitState = useState({ k: 0, done: false });
    var cur = fitState[0];
    var hideable = names.filter(function (n) { return n !== selected; });
    var hidden = cur.k > 0 ? hideable.slice(Math.max(hideable.length - cur.k, 0)) : [];

    function restart() { fitState[1]({ k: 0, done: false }); }

    // Fit again whenever what the row holds changes.
    var sig = names.join('\n') + '\t' + selected + '\t' + extraCount;
    useLayoutEffect(function () {
      if (!paused) restart();
    }, [sig, paused]);

    useLayoutEffect(function () {
      if (cur.done || paused) return;
      var row = rowRef.current;
      var next = narrow.matches || row.scrollWidth <= row.clientWidth + 1 || cur.k >= hideable.length
        ? { k: cur.k, done: true }
        : { k: cur.k + 1, done: false };
      fitState[1](function (s) { return s === cur ? next : s; });
    });

    // The row's width changes and the fonts finish loading: fit again.
    var again = useRef(null);
    again.current = function () { if (!paused) restart(); };
    useEffect(function () {
      var fit = function () { again.current(); };
      var ro = window.ResizeObserver ? new ResizeObserver(fit) : null;
      if (ro) ro.observe(rowRef.current);
      window.addEventListener('resize', fit);
      if (document.fonts && document.fonts.ready) document.fonts.ready.then(fit);
      return function () {
        if (ro) ro.disconnect();
        window.removeEventListener('resize', fit);
      };
    }, []);

    return { hidden: hidden, done: cur.done };
  }

  window.noteShared.TopicPicker = TopicPicker;
  window.noteShared.useFit = useFit;
})();
