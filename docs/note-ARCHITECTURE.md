---
last_verified: 2026-10-09   # 最後に実装と突合した日
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
| `cmd/note/` | CLI。引数・標準入力・サブコマンドを解釈して `internal/noteclient` を呼ぶ。端末かどうかの判定とブラウザを開く処理はここ |
| `internal/note/` | モデル（Note / Annotation / Link / Filter）とストア。検証、採番、`notes.json` の原子的な書き換え、操作ログの追記。URL の抽出、`[N]` 参照の切り出し（`link.go`）、ソース表示（ホストから導出）もここ |
| `internal/noteapi/` | `/api` の JSON エンドポイント。ストアの入力エラーを 400、存在しない id を 404 に写す |
| `internal/notepages/` | 登録画面（`/notes`）とタイムライン（`/notes/timeline`）の描画、`/notes/static`。ルート登録（`Mount`）と API の取り付けもここ。todo の `/`・`/static` とぶつからないよう `/notes` 配下に置く |
| `internal/noteroute/` | `POST /api/notes/:id/route`。AI が拾ったメモの行き先を決める。タスクを作るので task と note の両方のストアを受け取る（ADR note-0004）。`internal/server` が追加の Mounter として取り付ける |
| `internal/noteclient/` | CLI 用の HTTP クライアント。サーバに届かなければ日本語のエラーで止まる |
| `web/note/` | 埋め込みのテンプレートと静的ファイル（CSS / JS）。フレームワークも CDN も使わない |
| `launchd/` `scripts/install-agent.sh` | 常駐用の plist テンプレートと配置スクリプト（読み込みはしない） |

## データフロー

```
画面（/notes の JS） ─┐
                 ├─ HTTP /api ─ internal/noteapi ─ internal/note.Store ─┬─ notes.json（現在の状態）
CLI (note) ──────┘                                                  └─ events-YYYY-MM.jsonl（追記専用）
                                      タイムライン ← internal/notepages ← Store（読み取り）
```

- 保存先は `NOTE_DATA_DIR`（既定 `~/.local/share/note-stock`）。`notes.json` は一時ファイルに書いてから rename で置き換える
- ストアは mutex で直列化する。データディレクトリの `.lock` を排他ロックし、同じディレクトリを開く 2 つ目のサーバは起動に失敗する
- ログは補助。追記に失敗しても状態の保存は成功として扱い、エラーログに残す
- Host が `127.0.0.1:<port>` / `localhost:<port>` 以外、または別 Origin の要求は 403、`/api` への POST/PUT で Content-Type が application/json 以外は 415（`internal/httpguard`。todo と共通）
- 話題の一覧は登録済みの話題とメモで使われている話題の和集合
- 「補足待ち」は AI がまだ拾っていないメモ（`Note.TriagedAt` が空）。補足の有無では決まらない。拾うと `triaged_at` と `route`（`to`: `todo` / `record`、`todo` のときは `task_id`）が付く
- 行き先は変えられない。同じ行き先で再実行すると現状を返し、違う行き先は 409。`todo` はタスクを先に作ってからメモを更新し、失敗後の再実行は `source_note_id` で既存のタスクを使う（ADR note-0004）
- 起動時に、補足があって `triaged_at` が空のメモを「最初の補足の時刻に `record` で拾い済み」へ直す（`note.Open` の `migrateTriage`）。変更があったときだけ保存する
- タイムラインは行き先を「→ todo T001」（todo の一覧 `/` へのリンク）か「記録」で出す。各メモは `id="note-<id>"` のアンカーを持ち、todo の詳細の「元のメモ」がここへ飛ぶ
- ソース表示（Slack / Notion / Backlog / リンク）は表示のたびに URL のホストから求め、保存しない
- 関連 URL（`Note.Links`）は登録時にだけ付く。送られた順に 1 から番号を振り、以後は変えない。関連 URL が 1 件以上あるメモでは、本文の `[N]` が件数を超えると 400 にする。関連 URL が無いメモの `[N]` はただの文字として扱う
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
| 登録画面の関連 URL 行 | `web/note/static/js/add.js` の `addLinkRow` / `readLinks` |
| API の項目・ステータス | `internal/noteapi/api.go` |
| 画面の見た目 | `web/note/static/css/style.css`、`web/note/templates/` |
| 登録画面の操作（Enter・チップ・元に戻す） | `web/note/static/js/add.js` |
| IME 変換中のキーの扱い | `web/note/static/js/shared.js` の `composing()` |
| CLI のコマンドと出力 | `cmd/note/main.go` |
| 既定のポート・データの置き場 | `cmd/server/main.go`、`Makefile`、`launchd/*.template`、`scripts/install-agent.sh` |

## 経緯（why）は docs/adr/ を見る

設計判断の経緯・捨てた案・壊すと危ない前提は [docs/adr/README.md](adr/README.md)（`note-` 接頭辞）（索引）から辿る。
本ファイルには経緯を書かない（現在形とログを混ぜると両方陳腐化するため）。
