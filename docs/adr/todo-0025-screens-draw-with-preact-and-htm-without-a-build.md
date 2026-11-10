---
adr: todo-0025
title: 画面の描画に Preact と htm を使い、ビルド工程は持たない
status: accepted
superseded_by: null
date: 2026-11-09
issues: [119, 120]
tags: [preact, htm, render, ui, vendor, spa]
description: 画面の描画を素の JS の DOM 組み立てから Preact と htm に移す。htm の Preact 同梱版 1 ファイルを web/shared/static/vendor/ に置いて script タグで読み込み、npm・TypeScript・ビルド工程は持たない。行は key で同じ DOM を保つ。ルーター・ダイアログ・API 呼び出しは素の JS のまま残す
---

# ADR todo-0025: 画面の描画に Preact と htm を使い、ビルド工程は持たない

todo-order と note-stock の両方の画面に関わる判断だが、番号は todo 側に置く（ADR todo-0013 と同じ）。

## 背景

画面は素の JS で DOM を組み立てている。todo 一覧の `render()` は区画のリストを空にしてから全行を作り直すので、描き直しをまたいで残したい表示（開いた行・入力中の欄・一時的な印やアニメーション）を、そのつど手で保っている。2026-11-09 時点で `web/todo/static/js/app.js` は 2,990 行、`createElement` は 118 か所ある。

作り直しが原因の不具合も出ている（#115: ポーリングの応答で、入力中の欄が置き換わる）。予定している改修（着手順へ戻した行に印を付け、点滅させる）も、描き直しのたびに印と点滅を手で保つことになる。

ADR todo-0013 は「React（+ TypeScript + Vite）を入れる」を捨てた。理由は、npm とビルド工程が増えるわりに、当時の目的（画面を移っても状態が残る）への効果が素の JS と変わらないことだった。今回の目的は「描き直しで変わっていない行の DOM を残す」ことで、そこでは差分描画の効果がある。

## 決定

**Preact と htm を使う。** htm 3.1.1 の `preact/standalone.umd.js`（htm と Preact 10 系と hooks を 1 ファイルにまとめたもの）を `web/shared/static/vendor/htm-preact.umd.js` に置く。レイアウト（`web/shared/templates/base.html`）が、どの画面のスクリプトよりも先に読み込む。グローバルの `htmPreact` から `html`・`render`・`useState` などを取り出して使う。

**ビルド工程は持たない。** npm・package.json・TypeScript・バンドラは入れない。同梱のファイルは `go:embed` でバイナリに入り、CDN は見に行かない。版・取得元・SHA-256・入れ替え手順は `web/shared/static/vendor/README.md` に書く。

**行は key で保つ。** 一覧の行は task の ID を key にし、描き直しで変わっていない行の DOM を残す。

**ルーターとダイアログは素の JS のまま残す。** `router.js`・`modal.js`・`common.js` は DOM を描かず、画面の出し入れとダイアログへの貸し出し（`lend`）・API 呼び出しだけを持つ。各画面は `mount` で受け取ったルート要素に Preact で描く。

**画面ごとに移す。** 移していない画面は素の JS のまま動く。移す順番と範囲は #119 に置く。

## 捨てた案

- **React + TypeScript + Vite**: ADR todo-0013 が捨てた理由（npm とビルド工程、ビルドした成果物を embed する経路、CI の追加）がそのまま残る。型と部品の多さは得られるが、この規模の画面には見合わない
- **素の JS のまま、変わった行だけ直す仕組みを自前で書く**: 依存は増えないが、差分を取る仕組みを自前で持ち続けることになる
- **Preact を ES modules（`import`）で読み込む**: 既存の画面は `defer` の IIFE で、グローバルを通じてつながっている。移行の途中で読み込み方が 2 通りになるのを避け、UMD 1 本にした
- **CDN から読み込む**: オフラインで動かなくなる。画面は CDN を見に行かない（レイアウトのテストが確かめている）

## 変えてよい前提 / 壊すと危ない前提

- **変えてよい**: 同梱する版（README の手順で入れ替える）。どの画面から移すか。画面の中でのコンポーネントの分け方
- **壊すと危ない**:
  - ADR todo-0013 の前提は Preact の画面でも守る。`document` / `window` に足す処理は `active` を見る、`mount` の外で DOM を触らない、CSS は画面のルート要素の外に効かせない
  - Preact が描く要素を、素の JS から直接書き換えない。次の描画で Preact が上書きするか、差分がずれる（例外はドラッグ中のプレビューだけ。ADR todo-0026）
  - 同梱のファイルは、どの画面のスクリプトよりも先に読み込む。後ろに置くと、画面のスクリプトが読み込んだ時点で `htmPreact` がまだ無い
