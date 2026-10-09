// The add page: the whole screen is one field. Enter adds the task and clears
// the field for the next one, so several can go in without leaving the page.
(function () {
  'use strict';

  var shared = window.todoShared;
  var el = {
    name: document.getElementById('ap-name'),
    link: document.getElementById('ap-link'),
    chips: document.getElementById('ap-chips'),
    pick: document.getElementById('ap-pick'),
    resolved: document.getElementById('ap-resolved'),
    detail: document.getElementById('ap-detail'),
    submit: document.getElementById('ap-submit'),
    toast: document.getElementById('ap-toast'),
    recentWrap: document.getElementById('ap-recent-wrap'),
    recent: document.getElementById('ap-recent'),
  };
  var due = '';
  var busy = false;

  // ---- dates ----

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

  function choices() {
    var t = new Date();
    var today = new Date(t.getFullYear(), t.getMonth(), t.getDate());
    return [
      { label: 'なし', due: '' },
      { label: '今日', due: ymd(today) },
      { label: '明日', due: ymd(plus(today, 1)) },
      { label: '金曜', due: ymd(friday(today)) },
      { label: '来週月', due: ymd(nextWeek(today, 1)) },
      { label: '来週水', due: ymd(nextWeek(today, 3)) },
      { label: '日付…', pick: true },
    ];
  }

  // ---- due chips ----

  var chipButtons = [];

  function renderChips() {
    el.chips.textContent = '';
    chipButtons = choices().map(function (c, i) {
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'ap-chip';
      b.textContent = c.label;
      var k = document.createElement('kbd');
      k.textContent = String(i);
      b.appendChild(k);
      b.setAttribute('aria-pressed', 'false');
      b.addEventListener('click', function () { choose(i); });
      b.choice = c;
      el.chips.appendChild(b);
      return b;
    });
    choose(0);
  }

  function setDue(value, index) {
    due = value;
    chipButtons.forEach(function (b, i) { b.setAttribute('aria-pressed', String(i === index)); });
    el.resolved.textContent = due ? '→ ' + due : '';
  }

  function choose(i) {
    var b = chipButtons[i];
    if (!b) return;
    if (!b.choice.pick) {
      setDue(b.choice.due, i);
      el.name.focus();
      return;
    }
    el.pick.value = due;
    if (el.pick.showPicker) {
      try { el.pick.showPicker(); return; } catch (e) { /* needs a user gesture; fall through */ }
    }
    el.pick.focus();
  }

  el.pick.addEventListener('change', function () {
    if (el.pick.value) setDue(el.pick.value, chipButtons.length - 1);
    el.name.focus();
  });

  // ---- link pasted into the name ----

  shared.wirePaste(el.name, true);

  function showLink() {
    if (!el.name.pastedURL) { el.link.hidden = true; return; }
    el.link.hidden = false;
    el.link.textContent = '🔗 ' + (el.name.pastedLinkText || el.name.value) + ' → ' + el.name.pastedURL;
    el.link.title = el.name.pastedURL;
  }
  el.name.addEventListener('input', function () {
    if (!el.name.value) { el.name.pastedURL = null; el.name.pastedLinkText = null; }
    showLink();
  });
  el.name.addEventListener('paste', function () { window.setTimeout(showLink, 0); });

  // ---- add / undo ----

  function toast(parts, isError) {
    el.toast.textContent = '';
    el.toast.classList.toggle('is-error', !!isError);
    parts.forEach(function (p) { el.toast.appendChild(p); });
    el.toast.hidden = false;
  }
  function text(s) { return document.createTextNode(s); }

  // fetch rejects with a TypeError when the server cannot be reached at all;
  // say that plainly instead of the browser's "Failed to fetch".
  function reason(err) {
    return err instanceof TypeError ? 'サーバに接続できません（todo-order のサーバが起動しているか確認してください）' : err.message;
  }

  function remember(t) {
    var li = document.createElement('li');
    var n = document.createElement('span');
    n.className = 'ap-recent-name';
    n.textContent = t.name;
    var d = document.createElement('span');
    d.className = 'ap-recent-due';
    d.textContent = t.due || '期限なし';
    li.appendChild(n);
    li.appendChild(d);
    el.recent.prepend(li);
    el.recentWrap.hidden = false;
    return li;
  }

  function undo(li, name) {
    shared.request('POST', '/api/undo')
      .then(function () {
        li.classList.add('is-undone');
        toast([text('取り消しました: ' + name)]);
      })
      .catch(function (err) { toast([text('取り消せませんでした: ' + reason(err))], true); });
  }

  function reset() {
    el.name.value = '';
    el.name.pastedURL = null;
    el.name.pastedLinkText = null;
    el.detail.value = '';
    showLink();
    setDue('', 0);
    el.name.focus();
  }

  function submit() {
    var name = el.name.value.trim();
    if (!name || busy) { el.name.focus(); return; }
    var body = { name: name };
    if (due) body.due = due;
    var detail = el.detail.value.trim();
    if (detail) body.detail = detail;
    if (el.name.pastedURL) {
      body.url = el.name.pastedURL;
      body.link_text = el.name.pastedLinkText || '';
    }

    busy = true;
    el.name.disabled = true;
    shared.request('POST', '/api/tasks', body)
      .then(function (data) {
        var t = data.task || body;
        var li = remember(t);
        var again = document.createElement('button');
        again.type = 'button';
        again.textContent = '元に戻す';
        again.addEventListener('click', function () { undo(li, t.name); });
        toast([text('追加しました: ' + t.name), again]);
        el.name.disabled = false;
        reset();
      })
      .catch(function (err) {
        // Keep what was typed so nothing is lost; the message says why.
        el.name.disabled = false;
        el.name.focus();
        toast([text('追加できませんでした: ' + reason(err))], true);
      })
      .then(function () { busy = false; });
  }

  el.submit.addEventListener('click', submit);
  el.name.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    if (e.key === 'Enter') { e.preventDefault(); submit(); }
  });

  document.addEventListener('keydown', function (e) {
    if (shared.composing(e)) return;
    // ⌥+digit types a symbol on macOS (¡ ™ …), so read the physical key.
    var m = /^Digit([0-9])$/.exec(e.code || '');
    if (e.altKey && m) { e.preventDefault(); choose(Number(m[1])); return; }
    if (e.key === 'Escape' && document.activeElement !== el.pick) { location.href = '/'; }
  });

  renderChips();
  el.name.focus();
})();
