---
last_verified: 2026-10-14   # 最後に実装と突合した日
---

# ARCHITECTURE

1 行メモ（リンクと一言）を溜め、外から CLI で補足と話題を追記し、話題で絞ったタイムラインで読む 1 人用アプリ。
メモには登録時に関連 URL を添えられ、本文から `[1]` の形で指せる。
サーバ 1 つが唯一の書き手で、画面も CLI もサーバの HTTP API を通る。アプリは AI を呼ばない。

## 構成

| 場所 | 責務 |
|---|---|
| `cmd/server/` | todo と共通のサーバの起動。設定（`LISTEN_ADDR` / `PORT` / `TODO_DATA_DIR` / `NOTE_DATA_DIR`）の読み込み、ストアを開く、シャットダウン |
| `internal/server/` | todo と note のルートを 1 つの gin エンジンにまとめる。httpguard と `/healthz` はここで 1 回だけ掛ける |
| `cmd/note/` | CLI。引数・標準入力・サブコマンドを解釈して `internal/noteclient` を呼ぶ。端末かどうかの判定とブラウザを開く処理はここ。サブコマンドの打ち間違いを登録にしない判定は `internal/cliguard/`（todo と共通） |
| `internal/note/` | モデル（Note / Annotation / Link / Filter / TopicStat）とストア。検証、採番、`notes.json` の原子的な書き換え、操作ログの追記。話題の名前変更・統合・削除、話題ごとの件数と最終使用日時（`TopicStats`）、最近使った順の並べ替え（`RankByRecent`）もここ。URL の抽出、`[N]` 参照の切り出し（`link.go`）、ソース表示（ホストから導出）もここ |
| `internal/noteapi/` | `/api` の JSON エンドポイント。ストアの入力エラーを 400、存在しない id・話題を 404 に写す。話題は `GET/POST /api/topics`、`PUT/DELETE /api/topics/*name`（名前変更・統合・削除。名前に `/` が入っても届くようワイルドカード）。応答は名前一覧 `topics` と、件数・最終使用日時つきの `details` |
| `internal/notepages/` | 登録画面（`/notes`）とタイムライン（`/notes/timeline`）の外枠（中身は `add.js` / `timeline.js` がルーターの画面として描く）、`/notes/static`。話題チップに出す「最近使った上位 6 個＋選択中」と「他 N」の振り分け（`splitTopics`）もここ。ルート登録（`Mount`）と API の取り付けもここ。`GET /api/notes/timeline` はタイムラインと同じ組み立て（`buildTimeline`）の結果を JSON で返す（`?topic=` / `?no_topic=1` も同じ意味）。todo の `/`・`/static` とぶつからないよう `/notes` 配下に置く |
| `internal/noteroute/` | `POST /api/notes/:id/route`。AI が拾ったメモの行き先を決める。タスクを作るので task と note の両方のストアを受け取る（ADR note-0004）。`internal/server` が追加の Mounter として取り付ける |
| `internal/noteclient/` | CLI 用の HTTP クライアント。サーバに届かなければ日本語のエラーで止まる |
| `web/shared/` | todo と note が共有するレイアウト（サイトヘッダ）・CSS 変数（`tokens.css`）・基底 CSS・`common.js`（IME ガードと API 呼び出し）・`router.js`（画面の切り替え。ページを読み直さずに切り替え、画面ごとのスクロール位置を戻す。追加ダイアログへ画面を貸し出す `lend`）・`modal.js`（追加ダイアログ）。4 画面は 1 ページに置く（ADR todo-0013）。サイトヘッダは上部に固定する。`/shared` で配信し、`internal/server` がガードの内側にマウントする |
| `web/note/` | 埋め込みのテンプレートと静的ファイル（CSS / JS）。フレームワークも CDN も使わない |
| `launchd/` `scripts/install-agent.sh` | 常駐用の plist テンプレートと配置スクリプト（読み込みはしない） |

## データフロー

```
画面（/notes の JS） ─┐
                 ├─ HTTP /api ─ internal/noteapi ─ internal/note.Store ─┬─ notes.json（現在の状態）
CLI (note) ──────┘                                                  └─ events-YYYY-MM.jsonl（追記専用）
                                      タイムライン ← timeline.js ← GET /api/notes/timeline ← internal/notepages ← Store（読み取り）
```

- 保存先は `NOTE_DATA_DIR`（既定 `~/.local/share/note-stock`）。`notes.json` は一時ファイルに書いてから rename で置き換える
- ストアは mutex で直列化する。データディレクトリの `.lock` を排他ロックし、同じディレクトリを開く 2 つ目のサーバは起動に失敗する
- ログは補助。追記に失敗しても状態の保存は成功として扱い、エラーログに残す
- Host が `127.0.0.1:<port>` / `localhost:<port>` 以外、または別 Origin の要求は 403、`/api` への POST/PUT で Content-Type が application/json 以外は 415（`internal/httpguard`。todo と共通）
- 話題の一覧は登録済みの話題とメモで使われている話題の和集合
- 話題の名前変更・統合・削除は、話題の一覧とメモの `topic` を 1 回の保存でそろえて書き換え、操作ログに `rename_topic` / `merge_topic` / `delete_topic` を足す。既にある名前への名前変更は統合になる。削除した話題のメモは残り、話題なしになる
- 話題チップの並びは「その話題が付いたメモの最新の登録日時」の降順で、メモの無い話題は登録順で後ろに回す。タイムラインではサーバ（`GET /api/notes/timeline` の `chips` / `other`）が上位 6 個と選択中の話題を出し、残りは「他 N」。登録画面は `GET /api/topics` の `details` から JS（`add.js` の `byRecent`）が同じ規則で上位 6 個を出す。さらに 1 行に入らない分は、登録画面では `add.js` の `useFit`、タイムラインでは素の JS の `picker.js` の `fitChips` が、末尾の話題チップから隠して「他 N」の数と候補に足す（選択中・すべて・話題なしは隠さない。幅 560px 以下は隠さない）。`useFit` は隠す数を部品の状態に持ち、1 回の描画ごとに 1 個ずつ隠して溢れなくなるまで測り直す（要素の `hidden` を素の JS で書き換えない）。入力欄を開いている間は測り直さない。登録画面は `topics` の全話題を入力欄の候補に使う
- 登録画面はルーター（`router.js`）に登録した画面で、表示するたびに `add.js` の `AddScreen` が同じルート要素へ Preact で描き直す（ADR todo-0025、#125）。レイアウトがどのページにもルート要素（`data-view="note-add"`）と note の CSS・JS を置くので、todo 一覧との行き来でページを読み直さず、書きかけが残る。CSS は `:where(.view-note-add, .view-timeline)` の下に絞ってある。`Esc` は登録画面を表示しているときだけ受け、表示するたびにメモ欄へフォーカスを当てる。メモ欄は書き換えない要素（uncontrolled）で、文字は要素が持ち、Preact は value を書かない。話題・関連 URL の行・補足待ちの行・失敗の帯は部品の状態から描くので、チップを押す・行を足す・応答が届くといった描き直しをまたいで、メモ欄と行の入力の要素・文字・フォーカス・キャレットが残る
- 話題の候補入力（`TopicPicker`）は Preact の部品。入力欄は書き換えない要素で、入力した文字から候補（部分一致・最大 8 件・一致した名前を先頭・`allowCreate` のとき新しい話題として作る行を末尾）を描く。↑↓ で候補を移り（端でつながる）、Enter で選び、Esc で閉じる（Esc は画面の「タイムラインへ」に伝えない）。入力欄は `aria-controls` と `aria-activedescendant` で選んでいる候補を示し、候補は `role="option"` と ID を持つ。新しい話題の作成を送っている間も入力欄は残り、Esc で閉じられる（閉じたあとに応答が届いても、その話題は選ばず、チップだけ足す）。タイムラインはまだ素の JS なので、`createPicker(opts)` が `TopicPicker` を `display: contents` の要素へ描いて `{el, input}` を返す（`el.remove()` で部品も外す）
- タイムラインもルーターに登録した画面で、`timeline.js` が `GET /api/notes/timeline` から描く（本文などはすべて `textContent` で入れ、HTML としては解釈しない）。表示するたびに読み直す。話題チップはリンクのままで、ルーターがクエリ（`?topic=` / `?no_topic=1`）だけ変えて読み直すので、絞り込みでページを読み直さない（新しい絞り込みは先頭から表示する）。追加ダイアログや「話題を整理」で変更があったときも、ページではなくタイムラインだけを読み直す。`N` はタイムラインを表示しているときだけ受ける
- 登録画面の補足待ち一覧は、初めて表示した時点で `GET /api/notes?pending=1` から読み、別の画面から戻ったときはサーバに合わせる（AI が拾ったものは消し、増えたものは足す。読み始めたあとに保存したものは消さない）。保存した分は先頭に足し、削除した行は取り消し線で残す。行はメモの id を key にし、一覧の読み込みと保存の応答の両方を id で突き合わせて足すので、どちらが先に届いても同じメモが 2 行にならない
- タイムラインの `N` で開く追加ダイアログは、登録画面そのものをダイアログへ貸し出す（iframe ではない）。書きかけはダイアログと note タブで共通
- 「補足待ち」は AI がまだ拾っていないメモ（`Note.TriagedAt` が空）。補足の有無では決まらない。拾うと `triaged_at` と `route`（`to`: `todo` / `record`、`todo` のときは `task_id`）が付く
- 行き先は変えられない。同じ行き先で再実行すると現状を返し、違う行き先は 409。`todo` はタスクを先に作ってからメモを更新し、失敗後の再実行は `source_note_id` で既存のタスクを使う（ADR note-0004）
- 起動時に、補足があって `triaged_at` が空のメモを「最初の補足の時刻に `record` で拾い済み」へ直す（`note.Open` の `migrateTriage`）。変更があったときだけ保存する
- タイムラインは行き先を「→ todo T001」（todo の一覧 `/` へのリンク）か「記録」で出す。各メモは `id="note-<id>"` のアンカーを持ち、todo の詳細の「元のメモ」がここへ飛ぶ
- ソース表示（Slack / Notion / Backlog / リンク）は表示のたびに URL のホストから求め、保存しない
- 関連 URL（`Note.Links`）は登録時にだけ付く。送られた順に 1 から番号を振り、以後は変えない。関連 URL が 1 件以上あるメモでは、本文の `[N]` が件数を超えると 400 にする。関連 URL が無いメモの `[N]` はただの文字として扱う
- 登録画面のメモ欄にリンク付きのテキスト（トラッカーやチャットからコピーしたもの）を貼ると、カーソル位置に「テキスト [N]」が入り、関連 URL の [N] にその URL（一言はリンクの文字）が足される。リンクの無いテキストは文字だけが入る（`web/shared/static/js/paste.js` の `wireLinkPaste`。todo と共通）
- タイムラインでは本文の `[N]` を対応する関連 URL へのリンクにする。URL の中の `[N]` は URL の一部として扱う

## どこを触れば何が変わるか

| 変えたいこと | 触る場所 |
|---|---|
| 補足の種別を増やす | `internal/note/note.go` の `Kind*` と `internal/note/store.go` の `Annotate`、`internal/notepages/pages.go` の `kindLabels` |
| 行き先を決める手順・ステータス | `internal/noteroute/route.go` |
| 拾い済みの判定・移行 | `internal/note/note.go` の `Pending`、`internal/note/store.go` の `SetRoute` / `migrateTriage` |
| ソース表示のホスト判定 | `internal/note/note.go` の `sourceHosts` |
| URL の取り出し方 | `internal/note/note.go` の `urlPattern` / `urlTrailing` |
| `[N]` 参照の書き方・関連 URL の検証 | `internal/note/link.go` の `refPattern` / `buildLinks` / `checkRefs` |
| 登録画面の関連 URL 行（todo 追加と共通の部品） | `web/todo/static/js/links.js` の `create` / `LinkEditor`。登録画面は `add.js` が `subject: '本文'` で使う |
| リンク付きテキストの貼り付け（todo と共通） | `web/shared/static/js/paste.js` |
| API の項目・ステータス | `internal/noteapi/api.go` |
| 画面の見た目 | `web/shared/static/css/tokens.css`（色・文字・角丸・余白）、`web/note/static/css/style.css` |
| 登録画面の操作（Enter・チップ・補足待ち一覧・削除） | `web/note/static/js/add.js` の `AddScreen` |
| 候補つきの話題入力（登録画面は `TopicPicker` を直接描き、タイムラインは素の JS から `createPicker` で使う） | `web/note/static/js/picker.js` の `TopicPicker` / `createPicker` |
| 1 行に入らない話題チップを「他 N」へ隠す処理 | 登録画面は `add.js` の `useFit`、タイムラインは `picker.js` の `fitChips` / `watchChips` |
| タイムラインの描画・話題を整理するモーダル・タイムラインの「他 N」 | `web/note/static/js/timeline.js` |
| 話題チップに出す個数・並び | `internal/notepages/pages.go` の `visibleTopics` / `splitTopics`、`internal/note/store.go` の `RankByRecent` |
| 話題の名前変更・統合・削除 | `internal/note/store.go` の `RenameTopic` / `MergeTopic` / `DeleteTopic`、`internal/noteapi/api.go` の `updateTopic` / `deleteTopic` |
| IME 変換中のキーの扱い | `web/shared/static/js/common.js` の `composing()` |
| CLI のコマンドと出力 | `cmd/note/main.go` |
| 既定のポート・データの置き場 | `cmd/server/main.go`、`Makefile`、`launchd/*.template`、`scripts/install-agent.sh` |

## 経緯（why）は docs/adr/ を見る

設計判断の経緯・捨てた案・壊すと危ない前提は [docs/adr/README.md](adr/README.md)（`note-` 接頭辞）（索引）から辿る。
本ファイルには経緯を書かない（現在形とログを混ぜると両方陳腐化するため）。
