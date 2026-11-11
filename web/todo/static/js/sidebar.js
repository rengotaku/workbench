// todo 一覧の左のサイドバーを Preact で描く（ADR todo-0025）。
//
// 区画の一覧（件数つき。押すとその区画を開いてそこまでスクロールする）、話題の
// 一覧（未完了タスクの数つき）、note とタイムラインへのリンク。状態は持たず、
// app.js が渡す盤面から件数を数えて描く。app.js の .side に描く別の Preact の根で、
// 一覧の描画（list.js）とは互いの要素に触れない。
(function () {
  'use strict';

  var P = window.htmPreact;
  var html = P.html;

  // 押せる区画。key は app.js の openSection が受ける名前（開く先の要素の id は
  // app.js が持つ）。
  var SECTIONS = [
    { key: 'focus', label: 'フォーカス' },
    { key: 'queue', label: '着手順' },
    { key: 'long', label: '長期' },
    { key: 'out', label: 'スコープ外' },
    { key: 'wait', label: '待ち' },
    { key: 'someday', label: 'いつか' },
    { key: 'done', label: '今週完了' },
    { key: 'drop', label: '今週やめた' },
  ];

  // 件数は、一覧の見出しと同じ盤面の同じ絞り込みで数える（list.js の render）。
  function sectionCounts(b) {
    var queue = (b.queue || []).filter(function (t) { return !t.focused; });
    return {
      focus: (b.focus || []).length,
      queue: queue.filter(function (t) { return t.in_scope; }).length,
      long: (b.long || []).length,
      out: queue.filter(function (t) { return !t.in_scope; }).length,
      wait: (b.waiting || []).length,
      someday: (b.someday || []).length,
      done: (b.done || []).length,
      drop: (b.dropped || []).length,
    };
  }

  // 話題ごとの未完了タスク数。フォーカスは queue の一部なので queue だけ数え、
  // 待ち・いつか・長期も未完了として数える。
  function topicCounts(b) {
    var seen = {};
    var counts = {};
    ['queue', 'waiting', 'someday', 'long'].forEach(function (k) {
      (b[k] || []).forEach(function (t) {
        if (seen[t.id] || !t.topic) return;
        seen[t.id] = true;
        counts[t.topic] = (counts[t.topic] || 0) + 1;
      });
    });
    return counts;
  }

  // render draws the sidebar into container.
  //   view.board  the board the API sent (null before the first reply)
  //   view.ctx    ctx.openSection(key) opens a section and scrolls to it
  function render(container, view) {
    var b = view.board || { queue: [], focus: [] };
    var ctx = view.ctx;
    var counts = sectionCounts(b);
    var topics = b.topics || [];
    var perTopic = topicCounts(b);

    var tree = html`<div class="side-inner">
      <h2 class="side-h">区画</h2>
      <ul class="side-list" id="side-sections">${SECTIONS.map(function (s) {
        return html`<li key=${s.key}><button type="button" class="side-item" data-section=${s.key}
          onClick=${function () { ctx.openSection(s.key); }}><span class="side-name">${s.label}</span><span class="side-n">${counts[s.key]}</span></button></li>`;
      })}</ul>
      <h2 class="side-h">話題</h2>
      <ul class="side-list" id="side-topics">${topics.map(function (name) {
        return html`<li key=${name} class="side-row"><span class="side-name">${name}</span><span class="side-n">${perTopic[name] || 0}</span></li>`;
      })}</ul>
      <p class="side-empty" hidden=${topics.length > 0}>話題はまだない</p>
      <div class="side-links">
        <a href="/notes">note</a>
        <a href="/notes/timeline">タイムライン</a>
      </div>
    </div>`;
    P.render(tree, container);
  }

  window.workbench = window.workbench || {};
  window.workbench.todoSidebar = { render: render };
})();
