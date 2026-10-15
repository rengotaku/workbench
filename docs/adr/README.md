# ADR 索引

<!-- generate-adr-index.zsh による自動生成。手で編集しない -->

`todo-` は todo-order、`note-` は note-stock の判断記録。

| ADR | タイトル | status | date | 要旨 |
|---|---|---|---|---|
| [todo-0001](todo-0001-ordered-list-instead-of-kanban.md) | 着手順はカンバンではなく1本の順序リストで持つ | accepted | 2026-09-10 | 管理対象は着手順であり、列は順序を表現しないためカンバンを採らない |
| [todo-0002](todo-0002-json-plus-append-only-log.md) | 保存は tasks.json と追記専用の JSONL で行い SQLite を使わない | accepted | 2026-09-10 | 件数が数十のため DB を置かず、状態は JSON、履歴は月次 JSONL の追記で持つ |
| [todo-0003](todo-0003-cli-writes-through-the-server.md) | CLI はサーバ経由でのみ書き込み、データファイルを直接触らない | accepted | 2026-09-10 | 書き手をサーバ1つに限り、サーバ不通時はエラーで止めて正本の二重化を防ぐ |
| [todo-0004](todo-0004-ask-for-reasons-after-the-fact.md) | 並べ替えの理由は後から聞き、対象を4条件に限る | accepted | 2026-09-10 | ドラッグ時に理由を聞かず空で記録し、説明が要る4条件だけ後から質問する |
| [todo-0005](todo-0005-focus-is-a-mark-not-a-state.md) | フォーカスは状態ではなく印として持つ | accepted | 2026-09-11 | Focused を状態遷移に組み込まず独立したフラグにし、フォーカス中のタスクを着手順から抜かない |
| [todo-0006](todo-0006-undo-replays-inverse-events.md) | 取り消しはサーバ側でイベントを逆適用する | accepted | 2026-09-11 | undo をクライアントのスタックではなくイベントログの逆適用で実装し、逆操作自体も消費済みにして往復を防ぐ |
| [todo-0007](todo-0007-dropped-tasks-are-read-not-completed.md) | やめたタスクは完了にせず、週の記録として読ませる | accepted | 2026-09-14 | 削除に理由を持たせるにあたり完了へ寄せず、理由は後から聞き「今週やめた」区画で読ませる |
| [todo-0008](todo-0008-links-are-stored-not-derived.md) | リンクは貼られたものを保存する。名前から組み立てない | accepted | 2026-09-16 | チケットキーとベース URL から href を作る方式をやめ、貼り付けた URL をタスクに保存してそこへ飛ばす |
| [todo-0009](todo-0009-someday-is-a-fourth-state-not-waiting-or-due.md) | いつかは第4の状態。待ちへ寄せず、期限も流用しない | accepted | 2026-09-18 | 着手順に置かないが捨てもしないタスクの置き場を、待ち・期限の流用ではなく新しい状態と新しい日付で持つ |
| [todo-0010](todo-0010-annotations-are-not-undoable.md) | タスクの補足は undo の対象にしない | accepted | 2026-10-09 | タスクに付ける補足（引用・考察・メモ）は追記専用で、undo は補足のイベントを飛ばして直前の取り消せる操作を戻す。人の undo で AI の補足が消えないようにする。 |
| [todo-0011](todo-0011-colour-only-start-dates-that-have-arrived.md) | 色は着手日が来た行だけに付ける。期限が近いだけでは付けない | accepted | 2026-10-15 | 期限を着手の目安として使い、優先度は並び順で表す運用に合わせ、行頭の帯と期限チップの色を「今日（琥珀）」「過ぎた（赤）」だけにする |
| [note-0001](note-0001-annotations-are-append-only-and-written-from-outside.md) | 補足は追記のみで上書きしない。アプリは AI を呼ばず、補足は外から CLI で書く | accepted | 2026-10-06 | 補足（引用・考察・メモ）は追記専用で編集・削除 API を持たない。AI の呼び出しはアプリに入れず、外から note annotate で書く。 |
| [note-0002](note-0002-notes-json-plus-append-only-log-single-writer.md) | 保存は notes.json と追記専用 JSONL、書き手はサーバ 1 つ | accepted | 2026-10-06 | 現在の状態は notes.json を原子的に書き換え、操作は月ごとの events JSONL に追記する。書き手はサーバだけで、CLI もサーバ経由。 |
| [note-0003](note-0003-related-links-fixed-at-add-and-referenced-by-bracket-number.md) | 関連 URL は登録時にメモ本体へ固定し、本文から [N] で指す | accepted | 2026-10-09 | 関連 URL は Note.Links として登録時にだけ付け、メモごとに 1 から振った番号を変えない。本文からは [N] で指し、#N は使わない。 |
| [note-0004](note-0004-note-keeps-its-route-and-cross-store-calls-write-the-task-first.md) | 行き先はメモが持ち、2 つのストアをまたぐ操作はタスク先行＋source_note_id で冪等にする | accepted | 2026-10-09 | AI が拾ったメモの行き先（todo か記録）と拾った時刻をメモに持たせる。todo を作るときはタスクを先に作り、メモ側の更新が失敗しても source_note_id で同じタスクを再利用して二重に作らない。 |
