# note-stock

1 行のメモ（リンクと一言）を溜め、あとから AI が補足（引用・考察）と話題を足し、話題で絞ったタイムラインで読む 1 人用アプリ。

メモを書く場面ではリンクを貼って一言添えるだけにする。内容の整理は後で、アプリの外にいる AI が `note` コマンドで追記する。

## 何を持ち、何を持たないか

- 持つもの: メモ（入力した 1 行そのまま）/ メモ中の最初の URL / 任意の話題 / 追記される補足（引用・考察・メモ）
- 持たないもの
  - AI の呼び出し。アプリは AI を呼ばず、補足は外から CLI で書く
  - 補足の編集と削除。補足は追記だけで、後日の考察は日付つきで下に積まれる
  - リンク先の取得。URL は貼られたまま保存し、中身は読まない
  - 認証。ループバック（127.0.0.1）でだけ待ち受ける
- ソース表示（Slack / Notion / Backlog / リンク）は URL のホストから表示のたびに付ける。保存はしない

## 画面

| URL | 中身 |
|---|---|
| `/` | 登録画面。大きな 1 行入力と話題チップだけ。Enter で保存して欄を空にし、直後なら「元に戻す」で消せる。Esc でタイムラインへ |
| `/timeline` | 日付見出しごとに新しい順。話題チップ（すべて / 各話題 / 話題なし）で絞り込み、右上に補足待ちの件数。N で登録画面へ |

- 話題は 1 つだけ選ぶ。選んだチップは保存後も残り、もう一度押すと外れる。「＋ 話題を追加」はその場で名前を打つ
- 日本語入力の変換中の Enter / Esc は無視する
- 補足が 0 件のメモには「補足待ち」と出る

## コマンド

```bash
note                          # 端末から引数なし: ブラウザで登録画面を開く
note "https://… 一言"          # 引数を空白で連結して 1 件登録（--topic 青分離 で話題つき）
pbpaste | note                # 標準入力の全体を 1 件登録
note "[1] で決定" --link "https://… 一言"   # 関連 URL を添えて登録（--link を書いた順に [1] [2] …）
note list [--topic X] [--pending] [--no-topic] [--json]
note show 41 [--json]         # メモと補足を全部
note annotate 41 --kind quote --source slack --body-file quote.md
note annotate 41 --kind analysis --body "考察"
note topic 41 青分離           # 話題を設定（--clear で外す）
note topics
note delete 41
note help
```

- `--kind` は `quote`（引用）/ `analysis`（考察）/ `memo`（メモ）の 3 種。ほかは 400 で断られる
- `--author` は `ai`（既定）か `human`
- 書き込みが成功すると `note #41 追記` のような 1 行だけ出す
- サーバに届かなければ日本語のエラーを標準エラーに出して exit 1。ファイルを直接書くフォールバックは無い

| 変数 | 既定 | 用途 |
|---|---|---|
| `NOTE_SERVER` | `http://127.0.0.1:8086` | CLI が叩くサーバ |

## AI が補足を付ける流れ

```bash
note list --pending                                   # 補足待ち（補足 0 件）のメモ
note show 41                                          # 1 件を読む
note annotate 41 --kind quote --source slack --body-file quote.md
note annotate 41 --kind analysis --body-file analysis.md
note topic 41 青分離                                   # 話題が空なら AI が付ける
```

- 補足が 1 件付くと補足待ちから外れる
- あとで考え直したら `analysis` をもう 1 件足す。前の補足は残り、時刻つきで並ぶ

## サーバ

```bash
make install   # 依存取得
make build     # bin/server と bin/note
make run       # 起動（既定 127.0.0.1:8086）
make status    # 起動確認
make stop      # 停止
make ci        # lint + テスト（カバレッジ）
```

| 変数 | 既定 | 用途 |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1` | ループバック固定。認証は無いので LAN へ出さない |
| `PORT` | `8086` | ポート |
| `DATA_DIR` | `~/.local/share/note-stock` | データの置き場 |

データディレクトリの中身:

| ファイル | 中身 |
|---|---|
| `notes.json` | 現在の状態。一時ファイル + rename で原子的に書く |
| `events-YYYY-MM.jsonl` | 操作ログ。追記だけで、月ごとに分かれる |

## 常駐させる

```bash
make install-agent   # bin を ~/.local/bin に、plist を ~/Library/LaunchAgents に置く（読み込みはしない）
```

置いたあと、ログイン項目として登録するのは人が行う。

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.user.note-stock.server.plist
launchctl kickstart -k gui/$(id -u)/com.user.note-stock.server
curl -s http://127.0.0.1:8086/healthz
```

外すとき:

```bash
launchctl bootout gui/$(id -u)/com.user.note-stock.server
rm ~/Library/LaunchAgents/com.user.note-stock.server.plist
```

CLI は `~/.local/bin/note-stock` に置かれる。ログは `~/Library/Logs/note-stock/`。

## 構成と経緯

- 構成・データフロー・どこを触れば何が変わるか: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- 設計判断の経緯: [docs/adr/README.md](docs/adr/README.md)
