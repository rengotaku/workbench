# ADR 索引

<!-- generate-adr-index.zsh による自動生成。手で編集しない -->

| ADR | タイトル | status | date | 要旨 |
|---|---|---|---|---|
| [0001](0001-ordered-list-instead-of-kanban.md) | 着手順はカンバンではなく1本の順序リストで持つ | accepted | 2026-09-10 | 管理対象は着手順であり、列は順序を表現しないためカンバンを採らない |
| [0002](0002-json-plus-append-only-log.md) | 保存は tasks.json と追記専用の JSONL で行い SQLite を使わない | accepted | 2026-09-10 | 件数が数十のため DB を置かず、状態は JSON、履歴は月次 JSONL の追記で持つ |
| [0003](0003-cli-writes-through-the-server.md) | CLI はサーバ経由でのみ書き込み、データファイルを直接触らない | accepted | 2026-09-10 | 書き手をサーバ1つに限り、サーバ不通時はエラーで止めて正本の二重化を防ぐ |
| [0004](0004-ask-for-reasons-after-the-fact.md) | 並べ替えの理由は後から聞き、対象を4条件に限る | accepted | 2026-09-10 | ドラッグ時に理由を聞かず空で記録し、説明が要る4条件だけ後から質問する |
