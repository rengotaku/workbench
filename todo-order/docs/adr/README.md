# ADR 索引

<!-- generate-adr-index.zsh による自動生成。手で編集しない -->

| ADR | タイトル | status | date | 要旨 |
|---|---|---|---|---|
| [0001](0001-ordered-list-instead-of-kanban.md) | 着手順はカンバンではなく1本の順序リストで持つ | accepted | 2026-09-10 | 管理対象は着手順であり、列は順序を表現しないためカンバンを採らない |
| [0002](0002-json-plus-append-only-log.md) | 保存は tasks.json と追記専用の JSONL で行い SQLite を使わない | accepted | 2026-09-10 | 件数が数十のため DB を置かず、状態は JSON、履歴は月次 JSONL の追記で持つ |
| [0003](0003-cli-writes-through-the-server.md) | CLI はサーバ経由でのみ書き込み、データファイルを直接触らない | accepted | 2026-09-10 | 書き手をサーバ1つに限り、サーバ不通時はエラーで止めて正本の二重化を防ぐ |
| [0004](0004-ask-for-reasons-after-the-fact.md) | 並べ替えの理由は後から聞き、対象を4条件に限る | accepted | 2026-09-10 | ドラッグ時に理由を聞かず空で記録し、説明が要る4条件だけ後から質問する |
| [0005](0005-focus-is-a-mark-not-a-state.md) | フォーカスは状態ではなく印として持つ | accepted | 2026-09-11 | Focused を状態遷移に組み込まず独立したフラグにし、フォーカス中のタスクを着手順から抜かない |
| [0006](0006-undo-replays-inverse-events.md) | 取り消しはサーバ側でイベントを逆適用する | accepted | 2026-09-11 | undo をクライアントのスタックではなくイベントログの逆適用で実装し、逆操作自体も消費済みにして往復を防ぐ |
| [0007](0007-dropped-tasks-are-read-not-completed.md) | やめたタスクは完了にせず、週の記録として読ませる | accepted | 2026-09-14 | 削除に理由を持たせるにあたり完了へ寄せず、理由は後から聞き「今週やめた」区画で読ませる |
| [0008](0008-links-are-stored-not-derived.md) | リンクは貼られたものを保存する。名前から組み立てない | accepted | 2026-09-16 | チケットキーとベース URL から href を作る方式をやめ、貼り付けた URL をタスクに保存してそこへ飛ばす |
| [0009](0009-someday-is-a-fourth-state-not-waiting-or-due.md) | いつかは第4の状態。待ちへ寄せず、期限も流用しない | accepted | 2026-09-18 | 着手順に置かないが捨てもしないタスクの置き場を、待ち・期限の流用ではなく新しい状態と新しい日付で持つ |
