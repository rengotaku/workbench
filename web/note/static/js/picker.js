// A topic input with suggestions, used by the add screen ("他 N" and "＋ 話題を
// 追加") and the timeline filter ("他 N"). Typing narrows the topics by partial
// match; ↑ ↓ move, Enter picks, Esc closes. With allowCreate, a name that is not
// among the topics can be confirmed as a new one.
(function () {
  'use strict';

  var MAX_SHOWN = 8;

  // createPicker returns {el, input}. The caller puts el in the page, calls
  // input.focus(), and removes el when onPick or onClose runs.
  //   opts.topics      names to suggest
  //   opts.allowCreate offer "「q」を新しい話題として作る"
  //   opts.label       aria-label of the input
  //   opts.initial     text the input starts with
  //   opts.onPick(name, isNew)
  //   opts.onClose()   Esc, or focus left with nothing typed
  function createPicker(opts) {
    var wrap = document.createElement('div');
    wrap.className = 'suggest-wrap';
    var input = document.createElement('input');
    input.type = 'text';
    input.className = 'topic-q';
    input.autocomplete = 'off';
    input.placeholder = '話題を探す';
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-expanded', 'true');
    input.setAttribute('aria-label', opts.label || '話題を探す');
    var list = document.createElement('ul');
    list.className = 'suggest';
    list.setAttribute('role', 'listbox');
    wrap.appendChild(input);
    wrap.appendChild(list);

    var items = []; // {name, isNew}
    var hl = 0;
    var closed = false;

    function candidates(q) {
      var needle = q.toLowerCase();
      var hits = opts.topics.filter(function (t) { return t.toLowerCase().indexOf(needle) !== -1; });
      // An exact match goes first so Enter on a typed name picks that topic.
      hits.sort(function (a, b) { return (b === q ? 1 : 0) - (a === q ? 1 : 0); });
      var out = hits.slice(0, MAX_SHOWN).map(function (t) { return { name: t, isNew: false }; });
      if (opts.allowCreate && q !== '' && opts.topics.indexOf(q) === -1) out.push({ name: q, isNew: true });
      return out;
    }

    function render() {
      var q = input.value.trim();
      items = candidates(q);
      hl = Math.min(hl, Math.max(items.length - 1, 0));
      list.textContent = '';
      items.forEach(function (it, i) {
        var li = document.createElement('li');
        li.setAttribute('role', 'option');
        li.setAttribute('aria-selected', String(i === hl));
        if (i === hl) li.className = 'hl';
        if (it.isNew) { li.className += ' create'; li.textContent = '「' + it.name + '」を新しい話題として作る'; }
        else li.textContent = it.name;
        // mousedown, not click: the input would lose focus (and close) first.
        li.addEventListener('mousedown', function (e) { e.preventDefault(); pick(it); });
        list.appendChild(li);
      });
      list.hidden = items.length === 0;
    }

    function pick(it) {
      if (closed) return;
      closed = true;
      opts.onPick(it.name, it.isNew);
    }
    function close() {
      if (closed) return;
      closed = true;
      opts.onClose();
    }

    input.addEventListener('input', function () { hl = 0; render(); });
    input.addEventListener('keydown', function (e) {
      if (window.noteShared.composing(e)) return;
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); close(); return; }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        if (items.length === 0) return;
        hl = (hl + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
        render();
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        if (items[hl]) pick(items[hl]);
      }
    });
    input.addEventListener('blur', function () { if (!input.value.trim()) close(); });

    input.value = opts.initial || '';
    render();
    return { el: wrap, input: input };
  }

  // fitChips keeps a one-line chip row on one line. It hides topic chips
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
  window.noteShared.fitChips = fitChips;
  window.noteShared.watchChips = watchChips;
})();
