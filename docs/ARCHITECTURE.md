---
last_verified: 2026-10-06   # 最後に実装と突合した日
---

# ARCHITECTURE

1 行メモ（リンクと一言）を溜め、外から CLI で補足と話題を追記し、話題で絞ったタイムラインで読む 1 人用アプリ。
サーバ 1 つが唯一の書き手で、画面も CLI もサーバの HTTP API を通る。アプリは AI を呼ばない。

## 構成

| 場所 | 責務 |
|---|---|
| `cmd/server/` | サーバの起動。設定（`LISTEN_ADDR` / `PORT` / `DATA_DIR`）の読み込み、ストアを開く、シャットダウン |
| `cmd/note/` | CLI。引数・標準入力・サブコマンドを解釈して `internal/client` を呼ぶ。端末かどうかの判定とブラウザを開く処理はここ |
| `internal/note/` | モデル（Note / Annotation / Filter）とストア。検証、採番、`notes.json` の原子的な書き換え、操作ログの追記。URL の抽出とソース表示（ホストから導出）もここ |
| `internal/api/` | `/api` の JSON エンドポイント。ストアの入力エラーを 400、存在しない id を 404 に写す |
| `internal/pages/` | 登録画面（`/`）とタイムライン（`/timeline`）の描画、`/static`、`/healthz`。API の取り付けもここ |
| `internal/client/` | CLI 用の HTTP クライアント。サーバに届かなければ日本語のエラーで止まる |
| `web/` | 埋め込みのテンプレートと静的ファイル（CSS / JS）。フレームワークも CDN も使わない |
| `launchd/` `scripts/install-agent.sh` | 常駐用の plist テンプレートと配置スクリプト（読み込みはしない） |

## データフロー

```
画面（/ の JS） ─┐
                 ├─ HTTP /api ─ internal/api ─ internal/note.Store ─┬─ notes.json（現在の状態）
CLI (note) ──────┘                                                  └─ events-YYYY-MM.jsonl（追記専用）
                                      タイムライン ← internal/pages ← Store（読み取り）
```

- 保存先は `DATA_DIR`（既定 `~/.local/share/note-stock`）。`notes.json` は一時ファイルに書いてから rename で置き換える
- ストアは mutex で直列化する。データディレクトリの `.lock` を排他ロックし、同じディレクトリを開く 2 つ目のサーバは起動に失敗する
- ログは補助。追記に失敗しても状態の保存は成功として扱い、エラーログに残す
- Host が `127.0.0.1:<port>` / `localhost:<port>` 以外、または別 Origin の要求は 403、`/api` への POST/PUT で Content-Type が application/json 以外は 415（`internal/pages/guard.go`）
- 話題の一覧は登録済みの話題とメモで使われている話題の和集合
- 「補足待ち」は補足が 0 件のメモ。状態として保存せず、補足の数から求める
- ソース表示（Slack / Notion / Backlog / リンク）は表示のたびに URL のホストから求め、保存しない

## どこを触れば何が変わるか

| 変えたいこと | 触る場所 |
|---|---|
| 補足の種別を増やす | `internal/note/note.go` の `Kind*` と `internal/note/store.go` の `Annotate`、`internal/pages/pages.go` の `kindLabels` |
| ソース表示のホスト判定 | `internal/note/note.go` の `sourceHosts` |
| URL の取り出し方 | `internal/note/note.go` の `urlPattern` / `urlTrailing` |
| API の項目・ステータス | `internal/api/api.go` |
| 画面の見た目 | `web/static/css/style.css`、`web/templates/` |
| 登録画面の操作（Enter・チップ・元に戻す） | `web/static/js/add.js` |
| IME 変換中のキーの扱い | `web/static/js/shared.js` の `composing()` |
| CLI のコマンドと出力 | `cmd/note/main.go` |
| 既定のポート・データの置き場 | `cmd/server/main.go`、`Makefile`、`launchd/*.template`、`scripts/install-agent.sh` |

## 経緯（why）は docs/adr/ を見る

設計判断の経緯・捨てた案・壊すと危ない前提は [docs/adr/README.md](adr/README.md)（索引）から辿る。
本ファイルには経緯を書かない（現在形とログを混ぜると両方陳腐化するため）。
