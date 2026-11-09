// 着手順の一覧（区画と行）を Preact で描く。ADR todo-0025。
//
// app.js が持つ状態（state）と盤面（board）を受け取って描くだけで、状態は持たない。
// 描くたびに render(container, view) を呼ぶと、前の描画との差だけを DOM に当てる。
// 行は task の ID を key にするので、描き直しをまたいで同じ行の DOM が残る
// （開いた行・入力中の欄・フォーカス・CSS アニメーション）。
//
// 開いた行の名前の欄と詳細パネルは detail.js の部品（NameField・Panel）が描く。
// メニュー・削除の理由欄・通知は menus.js が描く。ドラッグ中の行の薄い表示と、フォーカスの帯の
// 「落とせる」印は state.drag から描く。Preact が描く要素を素の JS から直接書き換えない
// （ドラッグ中の行の要素を動かす見せ方だけが例外で、app.js が描画の前後で戻す）。
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;

  // 作ってから 1 時間以上たったタスクは、ごみ箱の前に理由を聞く（ADR todo-0016）。
  var ASK_DROP_AFTER_MS = 60 * 60 * 1000;

  var TRASH_SVG = '<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true">'
    + '<path d="M2.5 4.5h11M6 4.5V2.8h4v1.7M4.2 4.5l.7 8.7h6.2l.7-8.7M6.6 6.8v4.6M9.4 6.8v4.6"'
    + ' fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>';

  var EMPTY_BOARD = {
    counts: { over: 0, near: 0, rest: 0 },
    queue: [], focus: [], long: [], waiting: [], someday: [], done: [], dropped: [],
    revision: 0,
  };

  // ---- 日付と名前の読み取り（純粋な関数） ----

  // dateLabel turns the board's today (YYYY-MM-DD, the server's date, which
  // the due colours are judged against) into the heading's 1/2 (曜). The
  // weekday comes from the date itself, not the browser's time zone.
  function dateLabel(today) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(today || '');
    if (!m) return '';
    var day = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])).getUTCDay();
    return (+m[2]) + '/' + (+m[3]) + ' (' + '日月火水木金土'.charAt(day) + ')';
  }

  function monthDay(ymd) {
    var m = /^\d{4}-(\d{2})-(\d{2})$/.exec(ymd || '');
    return m ? (+m[1]) + '/' + (+m[2]) : '';
  }

  // shiftDay moves a YYYY-MM-DD date (the board's today) by n days. The
  // scope's edges are counted from the server's today, the same date
  // in_scope was judged against: due > today + scope_days is out.
  function shiftDay(ymd, n) {
    var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd || '');
    if (!m) return '';
    var d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3] + n));
    return d.toISOString().slice(0, 10);
  }

  // daysBetween counts calendar days from one YYYY-MM-DD date to another.
  function daysBetween(from, to) {
    var a = Date.parse(from + 'T00:00:00Z');
    var b = Date.parse(to + 'T00:00:00Z');
    return Math.round((b - a) / 86400000);
  }

  // TICKET_KEY is the ticket number a name may start with, and the spaces
  // after it. The name in the data keeps it; only the row's name drops it.
  var TICKET_KEY = /^([A-Z]+-\d+)\s*/;

  function stripTicket(name) {
    var m = TICKET_KEY.exec(name || '');
    return m && name.length > m[0].length ? name.slice(m[0].length) : (name || '');
  }

  // linkRange finds the part of a name that should carry the link: the words
  // that were the link where it was copied from. A task with no link, or whose
  // name no longer contains those words, has no range.
  function linkRange(t) {
    if (!t.url) return null;
    var label = t.link_text || t.name;
    var at = t.name.indexOf(label);
    if (at === -1 || !label) return null;
    return { start: at, end: at + label.length };
  }

  // The due date is when to start, and the order already says what comes
  // first. Colour is only for a start date that has arrived while the row sits
  // where it is: red once it has passed, amber on the day. A date still ahead
  // and no date at all stay uncoloured, so the few that need moving stand out.
  // dueChip is the chip's colour class; dueStripe is the row's.
  // 1-3 days out ("soon", ADR todo-0023) is amber too, but only on the stripe
  // and the chip: the row keeps no tint. A date 4 or more days ahead fades the row's text.
  function dueChip(t) {
    if (t.due_class === 'over') return 'over';
    if (t.due_class === 'today' || t.due_class === 'soon') return 'near';
    return '';
  }

  function dueStripe(t) {
    if (t.due_class === 'over') return 'overdue';
    if (t.due_class === 'today') return 'in-window';
    if (t.due_class === 'soon') return 'soon';
    if (t.due_class === 'near' || t.due_class === 'plain') return 'ahead';
    return '';
  }

  // dueTitle is the chip's hover: only what the label does not already say.
  // That is the date, plus the exact days where the label has rounded them
  // to weeks or more. The colours are named in the legend under the list.
  function dueTitle(t, today) {
    if (!t.due) return '';
    var n = daysBetween(today, t.due);
    var date = dateLabel(t.due);
    if (n >= 1) return date + '・あと' + n + '日';
    if (n <= -7) return date + '・' + -n + '日超過';
    return date;
  }

  // byDue orders スコープ外 rows nearest due first. Every one of them has a
  // due date (a row without one is in scope), and YYYY-MM-DD compares as a
  // string. The sort is stable, so rows due the same day keep queue order.
  function byDue(rows) {
    return rows.slice().sort(function (a, b) {
      return a.due < b.due ? -1 : a.due > b.due ? 1 : 0;
    });
  }

  // groupByTopic splits rows (already in queue order) into topics. Topics
  // come in the order of their top row; rows without one go last. Each group
  // keeps the queue order, so no row moves relative to its group-mates.
  function groupByTopic(rows) {
    var order = [];
    var by = Object.create(null); // a topic may be spelled like an Object method
    rows.forEach(function (t) {
      var k = t.topic || '';
      if (!by[k]) { by[k] = []; if (k !== '') order.push(k); }
      by[k].push(t);
    });
    var groups = order.map(function (k) { return { topic: k, rows: by[k] }; });
    if (by['']) groups.push({ topic: '', rows: by[''] });
    return groups;
  }

  function needsDropReason(t) {
    var made = Date.parse(t.created_at);
    return !isNaN(made) && Date.now() - made >= ASK_DROP_AFTER_MS;
  }

  // ---- 名前と札 ----

  // linkedText cuts text at each [N] that names one of links and makes that [N]
  // a link to its URL. The rest stays plain text, never HTML.
  function linkedText(text, links) {
    var byN = {};
    (links || []).forEach(function (l) { byN[l.n] = l; });
    var re = /\[([1-9][0-9]{0,3})\]/g;
    var at = 0;
    var m;
    var out = [];
    while ((m = re.exec(text)) !== null) {
      var l = byN[Number(m[1])];
      if (!l) continue;
      if (m.index > at) out.push(text.slice(at, m.index));
      out.push(html`<a class="ticket ref" href=${l.url} target="_blank" rel="noopener noreferrer"
        title=${l.label ? l.label + ' — ' + l.url : l.url} draggable=${false}>${m[0]}</a>`);
      at = m.index + m[0].length;
    }
    if (at < text.length) out.push(text.slice(at));
    return out;
  }

  // nameParts is a task's name with the link range as a link and the rest as
  // plain text. Each [N] naming one of the task's related URLs is a link to it
  // (ADR todo-0014). A task from before them also keeps its one link over the
  // words it covered (ADR todo-0008), related URLs added later or not.
  function nameParts(t) {
    var links = t.links || [];
    var r = linkRange(t);
    if (!r) return linkedText(t.name, links);
    return [
      r.start > 0 ? linkedText(t.name.slice(0, r.start), links) : null,
      html`<a class="ticket" href=${t.url} target="_blank" rel="noopener noreferrer"
        title=${t.url} draggable=${false}>${t.name.slice(r.start, r.end)}</a>`,
      r.end < t.name.length ? linkedText(t.name.slice(r.end), links) : null,
    ];
  }

  // splitTicket cuts a leading ticket number off a name and makes the badge
  // for it. It returns the badge and a copy of the task with the rest of the
  // name; the task's own name is untouched. When the task's link starts on
  // the ticket key, the badge is that link and the rest keeps what is left of
  // it. A link that only partly covers the key is left alone: no badge.
  function splitTicket(t) {
    var m = TICKET_KEY.exec(t.name || '');
    if (!m || t.name.length === m[0].length) return null;
    var key = m[1];
    var copy = Object.assign({}, t, { name: t.name.slice(m[0].length) });
    var r = linkRange(t);
    if (t.url && r && r.start < key.length && !(r.start === 0 && r.end >= key.length)) return null;
    var badge;
    if (t.url && r && r.start === 0) {
      badge = html`<a class="ticket tk-tag" href=${t.url} target="_blank" rel="noopener noreferrer"
        title=${t.url} draggable=${false}>${key}</a>`;
      if (r.end > m[0].length) {
        copy.link_text = t.name.slice(m[0].length, r.end);
      } else {
        copy.url = '';
        copy.link_text = '';
      }
    } else {
      badge = html`<span class="tk-tag">${key}</span>`;
    }
    return { badge: badge, task: copy };
  }

  // PARENT_TAG_CHARS is how much of the parent's name the tag shows.
  var PARENT_TAG_CHARS = 6;

  // parentTagText is what the parent tag spells out. After the ticket key,
  // the part of the parent's name that the child's name starts with too is
  // skipped (siblings of one project all begin the same way), cutting only
  // after a space or a closing bracket so no word is split. When nothing is
  // left, the name without its ticket key is used.
  function parentTagText(parentName, childName) {
    var p = Array.from(stripTicket(parentName));
    var c = Array.from(stripTicket(childName));
    var n = 0;
    while (n < p.length && n < c.length && p[n] === c[n]) n++;
    for (var k = n; k > 0; k--) {
      if (!/[\s　）)]/.test(p[k - 1])) continue;
      var rest = p.slice(k).join('').replace(/^[\s　]+/, '');
      return rest || p.join('');
    }
    return p.join('');
  }

  // parentTag names the task a row was cut out of, with the first words of
  // its name; the full path to it is in the tooltip. A parent the board does
  // not know (removed) shows its ID.
  function parentTag(t) {
    if (!t.parent) return null;
    var shown = t.parent;
    var path = t.parent;
    if (t.parent_name) {
      // The ticket key says nothing the row below does not already, so the
      // tag spends its characters on the words after it.
      var chars = Array.from(parentTagText(t.parent_name, t.name));
      shown = chars.slice(0, PARENT_TAG_CHARS).join('') + (chars.length > PARENT_TAG_CHARS ? '…' : '');
      path = (t.parent_path || []).map(function (p) {
        return (p.long ? '長期 ' : '') + p.id + ' ' + p.name;
      }).join(' ＞ ');
    }
    return html`<span class="parent-tag" title=${path + ' の子'}>${'↑' + shown}</span>`;
  }

  // kidsTag counts the children of a row that has any: done / all of them.
  // Long-run rows say it under the name instead, with the next step.
  function kidsTag(t) {
    var total = (t.kids_done || 0) + (t.kids_open || 0);
    if (!total) return null;
    return html`<span class="kids-tag" title=${'子 ' + total + ' 件のうち完了 ' + (t.kids_done || 0) + ' 件'}>${'子 ' + (t.kids_done || 0) + '/' + total}</span>`;
  }

  // idleTag says how long a task has been left without a person touching it.
  // The server counts the days and picks the stage (ADR todo-0021); the title
  // says which date the count started from.
  function idleTag(t) {
    if (!t.idle_days) return null;
    var from = {
      due: '期限（' + monthDay(t.due) + '）',
      annotation: '人の補足',
      review: '見直し',
      created: '作成',
    }[t.idle_from] || '';
    var title = from + 'から ' + t.idle_days + '日'
      + (t.idle_from === 'annotation' ? '' : '。人が補足を書くと、そこから数え直す');
    return html`<span class=${'idle-tag s' + t.idle_stage} title=${title}>${'止まって ' + t.idle_days + '日'}</span>`;
  }

  // reasonLabel is the words for one reason a long-run task needs a review.
  // The server decides the reasons; this only says them.
  function reasonLabel(t, reason) {
    switch (reason) {
      case 'no_next': return '次の一手なし';
      case 'review_due':
        return t.review_on ? '見直し日 ' + monthDay(t.review_on) + ' を過ぎた' : '見直し日が未設定';
      case 'stalled': return '止まって ' + (t.idle_days || 0) + '日';
      default: return reason;
    }
  }

  function reasonTags(t) {
    return (t.review_reasons || []).map(function (r) {
      return html`<span class="reason-tag">${reasonLabel(t, r)}</span>`;
    });
  }

  // ---- 行 ----

  function RemoveIcon(p) {
    var t = p.t;
    var ctx = p.ctx;
    var ask = needsDropReason(t);
    // The row opens on click; deleting must not also open or close it. There is
    // no confirmation: the delete is recorded with the whole task in it, so the
    // notice that follows offers 元に戻す (ADR todo-0016 for the reason field).
    function onClick(ev) {
      ev.stopPropagation();
      var btn = ev.currentTarget;
      if (ask) { ctx.openDropAsk(btn, t); return; }
      btn.disabled = true;
      ctx.removeTask(t, '').catch(function (err) { btn.disabled = false; ctx.showError(err); });
    }
    return html`<button type="button" class="del del-slot" title=${ask ? '理由を書いて削除する' : '削除する'}
      aria-label=${t.name + ' を削除する'} onClick=${onClick}
      dangerouslySetInnerHTML=${{ __html: TRASH_SVG }}></button>`;
  }

  // Row builds the shared 5-column shape: every section lines up on the same
  // left baseline, so the grip column is present even where it is unused.
  //   o.tags       parent and kids tags (queue, waiting and someday rows)
  //   o.long       the next step and child counts under the name (long-run rows)
  function Row(p) {
    var o = p.o;
    var t = o.task;
    var ctx = p.ctx;
    var st = ctx.state;
    var open = !!o.hasPanel && st.openId === t.id;
    // The open row's name and panel (detail.js, loaded after this file).
    var D = window.workbench.todoDetail;

    var cls = 'task' + (o.extraClass ? ' ' + o.extraClass : '') + (st.landedId === t.id ? ' landed' : '')
      + (st.drag && st.drag.active && st.drag.id === t.id ? ' dragging' : '');
    if (o.hasPanel) cls += ' openable' + (open ? ' row-open' : '');
    if (o.noNum) cls += ' no-num';

    // The parent and kids tags go inside the name text (inline, so wrapped
    // lines run under them) for queue-style rows, or beside it while the name
    // is being edited, so the editable text stays plain.
    var tags = o.tags ? [parentTag(t), kidsTag(t)] : null;
    var tagsInside = !!tags && !!o.noNum && !open;
    var txt;
    if (open) {
      // An open row is the editable one. The name is the same text in the same
      // place, so there is no second copy of it inside the panel. Ticket keys
      // stop being links while editing: a link inside an editable line fights
      // the caret.
      txt = html`<${D.NameField} key="name" t=${t} rev=${p.rev} ctx=${ctx} />`;
    } else {
      var split = o.ticketBadge ? splitTicket(t) : null;
      // The badge sits at the head of the name text, so a wrapped line runs
      // under it instead of leaving a column of its own.
      txt = html`<span class="txt" title=${t.name}>${tagsInside ? tags : null}${split ? split.badge : null}${nameParts(split ? split.task : t)}</span>`;
    }

    var due;
    // The due chip doubles as the way to set a due date: clicking it opens a
    // menu (app.js). Keeping it in the chip avoids adding a column to a row
    // that is meant to stay one line.
    if (o.editableDue) {
      var dcls = 'due due-edit' + (o.reviewChip ? ' review-chip' : '') + (o.dueClass ? ' ' + o.dueClass : '')
        // "期限なし" only shows on the hovered row (CSS), so the list is not a
        // column of them; the review chip of a long-run row always shows.
        + (!o.reviewChip && !t.due ? ' due-none' : '');
      due = html`<button type="button" class=${dcls} title=${o.dueTitle || undefined}
        aria-haspopup="menu" aria-expanded=${String(!!st.menu && st.menu.taskId === t.id)}
        data-due=${o.reviewChip ? (t.review_on || '') : (t.due || '')}>${o.dueLabel}</button>`;
    } else {
      due = html`<span class=${'due' + (o.dueClass ? ' ' + o.dueClass : '')} title=${o.dueTitle || undefined}>${o.dueLabel}</span>`;
    }

    return html`<li class=${cls} data-id=${t.id}
      tabindex=${o.hasPanel ? 0 : undefined}
      aria-expanded=${o.hasPanel ? String(open) : undefined}
      draggable=${o.draggable}
      onMouseEnter=${function () { ctx.showDetail(t); }}>
      <span class=${'grip' + (o.draggable ? '' : ' empty')} aria-hidden="true">${o.draggable ? '⠿' : ''}</span>
      ${o.noNum ? null : html`<span class=${'num' + (o.markClass ? ' ' + o.markClass : '')}
        aria-hidden=${o.markClass ? 'true' : undefined}>${o.mark}</span>`}
      <input type="checkbox" class="check" checked=${!!o.checked}
        aria-label=${o.checked ? '完了を取り消す' : '完了にする'}
        onChange=${function (ev) { ctx.onCheck(ev.target, t); }} />
      <div class="body">
        <div class="name">${tagsInside ? null : tags}${txt}${idleTag(t)}${o.long ? reasonTags(t) : null}</div>
        ${o.long ? longNext(t, ctx) : null}
        ${open ? html`<${D.Panel} key="panel" t=${t} rev=${p.rev} ctx=${ctx} />` : null}
      </div>
      ${due}
      ${html`<${RemoveIcon} t=${t} ctx=${ctx} />`}
    </li>`;
  }

  // longNext is the next step and the child counts of a long-run row.
  function longNext(t, ctx) {
    return html`<div class=${'long-next' + (t.next ? '' : ' warn')}>
      <span class="ln-label">次の一手</span>
      <span class="ln-task">${t.next ? t.next.id + ' ' + t.next.name : '未完の子が無い。次にやることを切り出す'}</span>
      ${t.next ? null : html`<button type="button" class="ln-add" title=${t.id + ' の子としてタスクを追加する'}
        onClick=${function (ev) { ev.stopPropagation(); ctx.addChild(t); }}>子を追加</button>`}
      <span class="ln-kids">${'子 完了 ' + (t.kids_done || 0) + ' · 未完 ' + (t.kids_open || 0)}</span>
    </div>`;
  }

  // The rows of each section: which shape a task takes there.

  // queueRow draws a queue task, in the band or in the list below it: the same
  // row either way, so a focused task opens where it is. fixed rows (under a
  // topic heading) cannot be dragged.
  function queueRow(t, fixed, v) {
    return html`<${Row} key=${t.id} rev=${v.rev} ctx=${v.ctx} o=${{
      task: t,
      draggable: !fixed,
      // The position is not shown. The order is what the list is for, and the
      // row above/below already says it; a number only adds a value that goes
      // stale the moment anything moves. It is still kept server-side, and
      // `todo order` uses it.
      mark: '',
      markClass: '',
      noNum: true,
      ticketBadge: true,
      tags: true,
      dueLabel: t.due_label,
      dueClass: dueChip(t),
      hasPanel: true,
      editableDue: true,
      dueTitle: dueTitle(t, v.board.today),
      extraClass: dueStripe(t),
    }} />`;
  }

  // longRow is a long-run task: not draggable (it is out of the queue), with
  // the next step and the child counts under the name.
  function longRow(t, v) {
    return html`<${Row} key=${t.id} rev=${v.rev} ctx=${v.ctx} o=${{
      task: t,
      mark: '▶',
      markClass: 'mark-long',
      // The chip slot holds the next review day, not the due date.
      // The weekday goes to the hover: with it the label outgrows the chip.
      dueLabel: t.review_on ? '見る ' + monthDay(t.review_on) : '見る 未設定',
      dueClass: t.needs_review && (t.review_reasons || []).indexOf('review_due') >= 0 ? 'near' : '',
      hasPanel: true,
      editableDue: true,
      reviewChip: true,
      long: true,
      dueTitle: (t.review_on ? dateLabel(t.review_on) + '・' : '') + '次に見る日。押して決める（見直すと 1 週間先になる）',
      extraClass: 'long',
    }} />`;
  }

  function waitRow(t, v) {
    var days = t.stalled_for || 0;
    var level = days >= 10 ? '赤: 10日以上' : (days >= 5 ? '黄: 5日以上' : '灰: 5日未満');
    return html`<${Row} key=${t.id} rev=${v.rev} ctx=${v.ctx} o=${{
      task: t,
      // No mark: ◷ sat in the same column as the focus ◎ and read alike.
      // The section heading already says these are waiting.
      mark: '',
      markClass: '',
      tags: true,
      dueLabel: days + '日',
      dueClass: days >= 10 ? 'over' : (days >= 5 ? 'near' : ''),
      dueTitle: level + '経過\n' + (t.handed_on || '?') + ' に ' + (t.handed_to || '?')
        + ' へ渡してから ' + days + '日。14日で理由を聞かれる',
      hasPanel: true,
      extraClass: 'waiting',
    }} />`;
  }

  // somedayRow shows a shelved task. Arriving is not overdue — nothing was
  // promised — so it borrows the same amber stripe waiting uses for "worth a
  // look" rather than the red one, and the tooltip says why in words. The
  // chip is not editable here: Until is not Due, and there is no editor for
  // it in the row (only `todo someday --until` and the resume button).
  function somedayRow(t, v) {
    return html`<${Row} key=${t.id} rev=${v.rev} ctx=${v.ctx} o=${{
      task: t,
      mark: '◇',
      markClass: 'mark-someday',
      tags: true,
      dueLabel: t.until ? t.until + ' まで' : '',
      dueClass: t.until_reached ? 'near' : '',
      dueTitle: t.until_reached
        ? ('見る番が来た（' + t.until + ' までにしていた）')
        : (t.until ? (t.until + ' まで見なくていい') : ''),
      hasPanel: true,
      extraClass: 'someday',
    }} />`;
  }

  function doneRow(t, v) {
    var label = '';
    if (t.completed_at) {
      var d = new Date(t.completed_at);
      label = (d.getMonth() + 1) + '/' + d.getDate();
    }
    return html`<${Row} key=${t.id} rev=${v.rev} ctx=${v.ctx} o=${{
      task: t,
      mark: '', // the strike-through and the ticked box already say it is done
      markClass: 'mark-done',
      checked: true,
      dueLabel: label,
      dueTitle: label + ' に完了。直近の月曜以降の分だけ残る\nチェックを外すと着手順の末尾へ戻る',
      hasPanel: false,
      extraClass: 'done',
    }} />`;
  }

  // dropRow shows a task that is gone. It cannot reuse Row: there is no task
  // left to tick, open, or drag — only the name it had and why it was dropped.
  function dropRow(d) {
    var at = new Date(d.at);
    var when = (at.getMonth() + 1) + '/' + at.getDate();
    return html`<li class="task drop" key=${d.event_id}>
      <span class="grip empty" aria-hidden="true"></span>
      <span class="num" aria-hidden="true"></span>
      <span class="check empty" aria-hidden="true"></span>
      <div class="body">
        <div class="name"><span class="txt" title=${d.name}>${nameParts(d)}</span></div>
        <div class=${'drop-why' + (d.reason ? '' : ' none')}>${d.reason || '理由なし'}</div>
      </div>
      <span class="due" title=${when + ' にやめた。直近の月曜以降の分だけ残る'}>${when}</span>
    </li>`;
  }

  // ---- 区画 ----

  // Fold is a section with a heading that opens and closes its list.
  function Fold(p) {
    return html`<section class="sec" id=${p.secId} hidden=${p.hidden} ...${p.secProps}>
      <button type="button" class="fold-head" id=${p.foldId} aria-expanded=${String(p.open)}
        aria-controls=${p.listId} title=${p.title} onClick=${p.onToggle}>
        <span class="caret" aria-hidden="true">${p.open ? '▼' : '▶'}</span>${p.label}<span class="n" id=${p.countId}>${p.count}</span>${p.noteId ? html`<span class="fold-note" id=${p.noteId}>${p.note}</span>` : null}
      </button>
      <ul class=${p.listClass || 'list'} id=${p.listId} hidden=${!p.open}>${p.children}</ul>
    </section>`;
  }

  // reviewLine is one line of the review band: a long-run task that needs a look.
  function reviewLine(t, ctx) {
    return html`<li class="review-line" key=${t.id}>
      <span class="rv-mark" aria-hidden="true">!</span>
      <button type="button" class="rv-name" title=${t.id + ' ' + t.name + ' を長期の枠で開く'}
        onClick=${function () { ctx.openLongTask(t.id); }}>${t.name}</button>
      ${reasonTags(t)}
      <button type="button" class="rv-done" disabled=${!!ctx.state.reviewing[t.id]} title="次に見る日を 1 週間先にして、見直した印を付ける"
        onClick=${function (ev) { ctx.reviewDone(t); }}>見直した</button>
    </li>`;
  }

  // topicHead is the small heading over a topic's rows in the grouped queue.
  function topicHead(g) {
    return html`<li class=${'topic-head' + (g.topic === '' ? ' none' : '')} key=${'topic-head:' + g.topic}>
      <span class="th-name">${g.topic === '' ? '話題なし' : g.topic}</span><span class="th-n">${g.rows.length}</span>
    </li>`;
  }

  // The in-scope queue under a small heading per topic. The rows cannot be
  // dragged here: a drop inside a group has no meaning for a queue that is
  // still one list (ADR todo-0024).
  function groupedRows(rows, v) {
    var out = [];
    groupByTopic(rows).forEach(function (g) {
      out.push(topicHead(g));
      g.rows.forEach(function (t) { out.push(queueRow(t, true, v)); });
    });
    return out;
  }

  function dragProps(ctx, listId) {
    return {
      onDragStart: ctx.drag.start,
      onDragOver: ctx.drag.over(listId),
      onDrop: ctx.drag.drop,
      onDragEnd: ctx.drag.end,
    };
  }

  // render draws the whole list screen into container (the .app element).
  //   view.board  the board the API sent (null before the first reply)
  //   view.state  app.js's state: scope, folds, openId, landedId, menu, drag, ...
  //   view.ctx    what the rows call back into app.js
  function render(container, view) {
    var b = view.board || EMPTY_BOARD;
    var st = view.state;
    var ctx = view.ctx;
    var v = { board: b, rev: b.revision, ctx: ctx };
    var counts = b.counts;

    // Focused tasks are the top of the queue (ADR todo-0012) and are shown in
    // the band only, so each task appears once. The band stays when empty: it
    // is where a row is dropped to focus it.
    var focus = b.focus || [];
    // The queue splits in two by scope. The in-scope part keeps queue order;
    // スコープ外 is shown nearest due first (queue order among the same day),
    // since none of it is to be started yet and the date is what tells them
    // apart. A drop is still placed against the whole queue (onDragEnd), so
    // the split is only how the rows are shown.
    var queue = (b.queue || []).filter(function (t) { return !t.focused; });
    var outRows = byDue(queue.filter(function (t) { return !t.in_scope; }));
    var inScope = queue.filter(function (t) { return t.in_scope; });
    var long = b.long || [];
    var review = long.filter(function (t) { return t.needs_review; });
    var waiting = (b.waiting || []).slice().sort(function (a, c) {
      return (c.stalled_for || 0) - (a.stalled_for || 0);
    });
    // Server-sorted already: soonest until first, no-until last. Arrived rows
    // sit ahead of the rest of that order because an earlier until means
    // arrived, not because the client re-sorts anything.
    var someday = b.someday || [];
    var reached = someday.filter(function (t) { return t.until_reached; }).length;
    var done = b.done || [];
    var dropped = b.dropped || [];
    var scopes = [['week', '1週間', '今週の日曜まで'], ['2weeks', '2週間', '来週の日曜まで'], ['all', 'すべて'], ['custom', '指定']];
    var nd = b.next_due;

    var tree = [
      html`<div class="app-head">
        <div class="head-left">
          <h1 class="app-name">着手順</h1>
          <span class="app-date">${dateLabel(b.today)}</span>
        </div>
        <div class="head-right">
          <div class="group-set">
            <button type="button" class="group-toggle" id="group-toggle" aria-pressed=${String(st.groupByTopic)}
              title="着手順を話題ごとの見出しで区切って見せる。順序は変わらない" onClick=${ctx.toggleGroup}>話題で分ける</button>
            <span class="group-note" id="group-note" hidden=${!st.groupByTopic}>分けている間は並べ替えできない</span>
          </div>
          <div class="scope-set" role="group" aria-label="スコープ">
            ${scopes.map(function (s) {
              return html`<button type="button" key=${s[0]} data-scope=${s[0]} aria-pressed=${String(st.scopeKey === s[0])}
                title=${s[2]} onClick=${function () { ctx.pickScope(s[0]); }}>${s[1]}</button>`;
            })}
          </div>
          <label class="scope-until" id="scope-until-wrap" hidden=${st.scopeKey !== 'custom'}>～<input type="date" id="scope-until"
            aria-label="スコープの締め日" onChange=${ctx.pickUntil} /></label>
          <div class="counts-col">
            <div class="counts">
              <span class="count over" title=${'赤: 期限を過ぎている — ' + counts.over + '件'}><i aria-hidden="true"></i>超過 <b id="c-over">${counts.over}</b><span class="sr">件 期限超過</span></span>
              <span class="count near" title=${'黄: 期限が今日 — ' + counts.near + '件'}><i aria-hidden="true"></i>今日 <b id="c-near">${counts.near}</b><span class="sr">件 期限が今日</span></span>
              <span class="count rest" title=${'灰: 期限がまだ先、または期限なし — ' + counts.rest + '件'}><i aria-hidden="true"></i>ほか <b id="c-rest">${counts.rest}</b><span class="sr">件 期限がまだ先・期限なし</span></span>
            </div>
            <p class="next-due" id="next-due" hidden=${!nd} title=${nd ? nd.name : undefined}>${nd ? '次の期限 ' + monthDay(nd.due) + '　' + stripTicket(nd.name) : ''}</p>
          </div>
        </div>
      </div>`,

      html`<section class="review-band" id="review-band" hidden=${review.length === 0}>
        <div class="review-head">長期の見直し<span class="n" id="review-count">${review.length}</span></div>
        <ul class="review-list" id="review-list">${review.map(function (t) { return reviewLine(t, ctx); })}</ul>
      </section>`,

      html`<section class=${'focus-band' + (st.drag && st.drag.active ? ' drop-ready' : '')} id="focus-band" ...${dragProps(ctx, 'focus-list')}>
        <div class="focus-head">フォーカス<span class="n" id="focus-count">${focus.length}</span></div>
        <ul class="list focus-list" id="focus-list">${focus.map(function (t) { return queueRow(t, false, v); })}</ul>
        <p class="focus-empty" id="focus-empty" hidden=${focus.length > 0}>ここへドラッグするとフォーカスになる</p>
      </section>`,

      html`<section class="sec" ...${dragProps(ctx, 'queue')}>
        <ul class="list" id="queue">${st.groupByTopic ? groupedRows(inScope, v) : inScope.map(function (t) { return queueRow(t, false, v); })}</ul>
        <div class="add" id="add-slot">
          <span></span>
          <span class="plus" aria-hidden="true">＋</span>
          <button type="button" class="as-button" id="add-trigger" onClick=${ctx.openAdd}>タスクを追加</button>
          <kbd aria-hidden="true">N</kbd>
        </div>
      </section>`,

      // The long-run fold (ADR todo-0022): closed by default, with the others
      // below the queue. With no long-run task it is not shown at all. What
      // needs attention comes up in the review band instead, by condition.
      html`<${Fold} secId="long-band" hidden=${long.length === 0} foldId="long-fold" open=${st.longOpen}
        listId="long-list" listClass="list long-list" label="長期" countId="long-count" count=${long.length}
        noteId="long-note" note=${long.length ? '次に見る日が来たら上に出る' : ''}
        onToggle=${function () { ctx.toggleFold('long'); }}>${long.map(function (t) { return longRow(t, v); })}<//>`,

      html`<${Fold} secId="out-sec" hidden=${outRows.length === 0} foldId="out-fold" open=${st.outOpen}
        listId="outlist" label="スコープ外" countId="out-count" count=${outRows.length}
        noteId="out-from" note=${'期限 ' + monthDay(shiftDay(b.today, b.scope_days + 1)) + ' 以降'}
        secProps=${dragProps(ctx, 'outlist')}
        onToggle=${function () { ctx.toggleFold('out'); }}>${outRows.map(function (t) { return queueRow(t, false, v); })}<//>`,

      // Folded, the longest wait is what would otherwise go unseen.
      html`<${Fold} secId="waiting-sec" hidden=${waiting.length === 0} foldId="wait-fold" open=${st.waitOpen}
        listId="waitlist" title="ボールを渡してからの経過日数で並ぶ" label="待ち" countId="wait-count" count=${waiting.length}
        noteId="wait-max" note=${waiting.length ? '最長 ' + (waiting[0].stalled_for || 0) + '日' : ''}
        onToggle=${function () { ctx.toggleFold('wait'); }}>${waiting.map(function (t) { return waitRow(t, v); })}<//>`,

      html`<${Fold} secId="someday-sec" hidden=${someday.length === 0} foldId="someday-fold" open=${st.somedayOpen}
        listId="somedaylist" label="いつか" countId="someday-count"
        count=${someday.length + (reached > 0 ? '（到来 ' + reached + '）' : '')}
        onToggle=${function () { ctx.toggleFold('someday'); }}>${someday.map(function (t) { return somedayRow(t, v); })}<//>`,

      html`<${Fold} secId="done-sec" hidden=${done.length === 0} foldId="done-fold" open=${st.doneOpen}
        listId="donelist" label="今週完了" countId="done-count" count=${done.length}
        onToggle=${function () { ctx.toggleFold('done'); }}>${done.map(function (t) { return doneRow(t, v); })}<//>`,

      html`<${Fold} secId="drop-sec" hidden=${dropped.length === 0} foldId="drop-fold" open=${st.dropOpen}
        listId="droplist" label="今週やめた" countId="drop-count" count=${dropped.length}
        onToggle=${function () { ctx.toggleFold('drop'); }}>${dropped.map(dropRow)}<//>`,

      html`<div class="add-row">
        <code>todo "タスク名" --due YYYY-MM-DD</code>
        <span>→ 端末からも同じ口に入る</span>
      </div>`,
    ];
    P.render(tree, container);
  }

  window.workbench = window.workbench || {};
  window.workbench.todoList = {
    render: render,
    dateLabel: dateLabel,
    monthDay: monthDay,
    shiftDay: shiftDay,
    linkRange: linkRange,
    nameParts: nameParts,
  };
})();
