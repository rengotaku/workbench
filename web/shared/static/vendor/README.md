# vendor

画面の描画に使う外部のライブラリを、ビルド工程なしでそのまま置く（ADR todo-0025）。npm では管理しない。

| ファイル | 中身 | 取得元 | ライセンス |
|---|---|---|---|
| `htm-preact.umd.js` | htm 3.1.1 の `preact/standalone.umd.js`。htm と Preact 10 系（hooks を含む）を 1 ファイルにまとめたもの。読み込むとグローバルの `htmPreact` に `html`・`render`・`h`・`useState` などが入る | https://registry.npmjs.org/htm/-/htm-3.1.1.tgz | htm: Apache-2.0（`LICENSE-htm`）、Preact: MIT（`LICENSE-preact`） |

`htm-preact.umd.js` の SHA-256: `62c4ffea400078c0eda7f435b22726b4961ebdf74c8506f188e92bec41d8ac51`

## 入れ替えるとき

1. 新しい版の tarball を registry から取り、`package/preact/standalone.umd.js` を `htm-preact.umd.js` に上書きする
2. この表の版と SHA-256（`shasum -a 256 htm-preact.umd.js`）を書き換える
3. 画面を開いて動くことを確かめる
