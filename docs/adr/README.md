# ADR 索引

<!-- generate-adr-index.zsh による自動生成。手で編集しない -->

| ADR | タイトル | status | date | 要旨 |
|---|---|---|---|---|
| [0001](0001-annotations-are-append-only-and-written-from-outside.md) | 補足は追記のみで上書きしない。アプリは AI を呼ばず、補足は外から CLI で書く | accepted | 2026-10-06 | 補足（引用・考察・メモ）は追記専用で編集・削除 API を持たない。AI の呼び出しはアプリに入れず、外から note annotate で書く。 |
| [0002](0002-notes-json-plus-append-only-log-single-writer.md) | 保存は notes.json と追記専用 JSONL、書き手はサーバ 1 つ | accepted | 2026-10-06 | 現在の状態は notes.json を原子的に書き換え、操作は月ごとの events JSONL に追記する。書き手はサーバだけで、CLI もサーバ経由。 |
