// The add page: the whole screen is one field. Enter adds the task and clears
// the field for the next one, so several can go in without leaving the page.
(function () {
  'use strict';

  // The markup of the screen. It used to be templates/add.html; the router
  // shows every screen in one page, so the screen draws itself when first shown.
  // The add dialog (modal.js) borrows the same screen.
  var MARKUP = [
    '<div class="ap-page">',
    '  <header class="ap-head">',
    '    <span>着手順に追加 <span class="ap-date"></span></span>',
    '    <a href="/" id="to-list" data-leave>一覧へ →</a>',
    '  </header>',
    '',
    '  <main class="ap-main">',
    '    <label class="ap-label" for="ap-name">なにをやる？</label>',
    '    <input class="ap-name" id="ap-name" autofocus autocomplete="off" placeholder="タスク名を入力して Enter">',
    '    <div class="ap-link" id="ap-link" hidden></div>',
    '',
    '    <div class="ap-row">',
    '      <div class="ap-field">',
    '        <p class="ap-label">期限</p>',
    '        <div class="ap-chips" id="ap-chips"></div>',
    '        <input type="date" id="ap-pick" class="ap-pick" tabindex="-1" aria-label="期限の日付">',
    '        <div class="ap-resolved" id="ap-resolved"></div>',
    '      </div>',
    '      <div class="ap-field">',
    '        <label class="ap-label" for="ap-detail">詳細（任意）</label>',
    '        <textarea class="ap-detail" id="ap-detail" placeholder="メモや URL"></textarea>',
    '      </div>',
    '    </div>',
    '',
    '    <div class="ap-actions">',
    '      <button type="button" class="ap-submit" id="ap-submit">追加する</button>',
    '      <span class="ap-hint"><kbd>⏎</kbd> 追加して次を入力 · <kbd>⌥</kbd>+数字 で期限 · <kbd>Esc</kbd> 一覧へ</span>',
    '    </div>',
    '',
    '    <div class="ap-toast" id="ap-toast" role="status" hidden></div>',
    '',
    '    <section class="ap-recent" id="ap-recent-wrap" hidden>',
    '      <p class="ap-label">いま追加したもの</p>',
    '      <ul id="ap-recent"></ul>',
    '    </section>',
    '  </main>',
    '</div>',
  ].join('\n');

  // active is true while the screen is on show (as a page or in the add
  // dialog): the key listener on document stays for the life of the page and
  // must not act from a hidden screen.
  var active = false;
  var hooks = {};

  // The date beside the heading, as the due chips count it: 1/2 (曜).
  function todayLabel() {
    var d = new Date();
    return (d.getMonth() + 1) + '/' + d.getDate() + ' (' + '日月火水木金土'.charAt(d.getDay()) + ')';
  }

  function mount(root) {
    root.innerHTML = MARKUP;
    root.querySelector('.ap-date').textContent = '· ' + todayLabel();

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
      window.workbench.tell('busy');
      shared.request('POST', '/api/undo')
        .then(function () {
          li.classList.add('is-undone');
          window.workbench.tell('changed');
          toast([text('取り消しました: ' + name)]);
        })
        .catch(function (err) { toast([text('取り消せませんでした: ' + reason(err))], true); })
        .then(function () { window.workbench.tell('idle'); });
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
      window.workbench.tell('busy');
      shared.request('POST', '/api/tasks', body)
        .then(function (data) {
          var t = data.task || body;
          var li = remember(t);
          window.workbench.tell('changed');
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
        .then(function () { busy = false; window.workbench.tell('idle'); });
    }

    el.submit.addEventListener('click', submit);
    el.name.addEventListener('keydown', function (e) {
      if (shared.composing(e)) return;
      if (e.key === 'Enter') { e.preventDefault(); submit(); }
    });

    document.addEventListener('keydown', function (e) {
      if (!active) return;
      if (shared.composing(e)) return;
      // ⌥+digit types a symbol on macOS (¡ ™ …), so read the physical key.
      var m = /^Digit([0-9])$/.exec(e.code || '');
      if (e.altKey && m) { e.preventDefault(); choose(Number(m[1])); return; }
      if (e.key === 'Escape' && document.activeElement !== el.pick) { window.workbench.leave('/'); }
    });

    renderChips();

    hooks.show = function () {
      // The toast was about the last visit; what went in stays listed below.
      el.toast.hidden = true;
      // autofocus only works once per page; the screen is shown many times.
      el.name.focus();
    };
  }

  window.workbench.router.register('todo-add', {
    title: '着手順に追加',
    mount: function (root) {
      active = true;
      mount(root);
    },
    show: function () {
      active = true;
      if (hooks.show) hooks.show();
    },
    hide: function () {
      active = false;
    },
  });
})();
