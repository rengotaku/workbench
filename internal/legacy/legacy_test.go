package legacy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rengotaku/workbench/internal/legacy"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const ledgerSample = `# TODO（自分の作業・チケット外）

> 1 行 1 件。用途は README を参照。

- [x] T002 | 2026-09-07 | キックオフのドキュメント作成 | due:2026-09-07 | done:2026-09-08
- [ ] T004 | 2026-09-08 | 話し方の改善をどう進めるかを検討する | due:2026-09-11 | ref:W26
- [x] T006 | 2026-09-09 | 行動目標の点数を再計算する | due:2026-09-11 | done:2026-09-09
`

const orderSample = `# 着手順。上から順に作業する。

order:
  # 期限が今日・明日のもの
  - DSC-11989   # 剣持へ結論をコメント（本人メモ「明日の作業1番目」）

  # 🔴 優先度引き上げ（本人指示）
  #    継続 6 件を並行させる合意分。
  - DSC-11991   # DB パスワードを Secrets Manager 参照へ移行（RDS / Redis）

  # 期限に余裕があるもの
  - T004        # 話し方の改善の進め方
`

func TestReadLedger_KeepsIDsDueDatesAndCompletion(t *testing.T) {
	rows, err := legacy.ReadLedger(write(t, "_todo.md", ledgerSample))
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.Equal(t, "T002", rows[0].ID)
	assert.Equal(t, "キックオフのドキュメント作成", rows[0].Name)
	assert.Equal(t, "2026-09-07", rows[0].Due)
	assert.True(t, rows[0].Done)

	assert.Equal(t, "T004", rows[1].ID)
	assert.False(t, rows[1].Done)
	assert.Equal(t, "W26", rows[1].Ref)
}

// A line that looks like an entry but does not parse has to stop the import:
// skipping it would drop work without saying so.
func TestReadLedger_RefusesALineItCannotParse(t *testing.T) {
	path := write(t, "_todo.md", "- [ ] これは形式が違う\n")
	_, err := legacy.ReadLedger(path)
	assert.ErrorContains(t, err, "does not parse")
}

func TestReadLedger_RefusesAFileWithNoEntries(t *testing.T) {
	path := write(t, "_todo.md", "# 見出しだけ\n")
	_, err := legacy.ReadLedger(path)
	assert.ErrorContains(t, err, "holds no entries")
}

func TestReadLedger_ReportsAMissingFile(t *testing.T) {
	_, err := legacy.ReadLedger(filepath.Join(t.TempDir(), "absent.md"))
	assert.ErrorContains(t, err, "opening")
}

func TestReadOrder_KeepsTheNotesAroundEachEntry(t *testing.T) {
	rows, err := legacy.ReadOrder(write(t, "_todo-order.yml", orderSample))
	require.NoError(t, err)
	require.Len(t, rows, 3)

	assert.Equal(t, "DSC-11989", rows[0].Key)
	assert.Contains(t, rows[0].Inline, "剣持へ結論をコメント")
	assert.Equal(t, []string{"期限が今日・明日のもの"}, rows[0].Context)

	assert.Equal(t, "DSC-11991", rows[1].Key)
	require.Len(t, rows[1].Context, 2, "the whole block above the entry is kept")
	assert.Contains(t, rows[1].Context[0], "優先度引き上げ")

	assert.Equal(t, "T004", rows[2].Key)
}

// Comments above "order:" describe the file, not the first entry.
func TestReadOrder_IgnoresTheHeaderComments(t *testing.T) {
	rows, err := legacy.ReadOrder(write(t, "_todo-order.yml", orderSample))
	require.NoError(t, err)
	assert.NotContains(t, rows[0].Context, "着手順。上から順に作業する。")
}

func TestReadOrder_RefusesAFileWithoutTheOrderKey(t *testing.T) {
	_, err := legacy.ReadOrder(write(t, "_todo-order.yml", "# just a comment\n- DSC-1\n"))
	assert.ErrorContains(t, err, `no "order:" key`)
}

func TestReadOrder_RefusesALineThatIsNeitherCommentNorEntry(t *testing.T) {
	_, err := legacy.ReadOrder(write(t, "_todo-order.yml", "order:\n  nonsense\n"))
	assert.ErrorContains(t, err, "neither a comment nor an entry")
}

func TestPlan_PutsTheOrderFileFirstAndKeepsLedgerIDs(t *testing.T) {
	ledger, err := legacy.ReadLedger(write(t, "_todo.md", ledgerSample))
	require.NoError(t, err)
	order, err := legacy.ReadOrder(write(t, "_todo-order.yml", orderSample))
	require.NoError(t, err)

	items, err := legacy.Plan(ledger, order)
	require.NoError(t, err)
	require.Len(t, items, 5, "3 ordered entries plus the 2 ledger rows the order file omits")

	// The order file leads, in its own order.
	assert.Empty(t, items[0].ID, "a ticket key gets a fresh number")
	assert.Contains(t, items[0].Name, "DSC-11989")
	assert.Contains(t, items[0].Name, "剣持へ結論をコメント")
	assert.Contains(t, items[0].Detail, "本人メモ", "the reasoning is kept as detail")

	assert.Contains(t, items[1].Name, "DSC-11991")

	// A ledger row named by the order file keeps its ID, due date and notes.
	assert.Equal(t, "T004", items[2].ID)
	assert.Equal(t, "話し方の改善をどう進めるかを検討する", items[2].Name)
	assert.Equal(t, "2026-09-11", items[2].Due)
	assert.Contains(t, items[2].Detail, "参照: W26")

	// Rows the order file leaves out still come over, completed ones included.
	rest := map[string]bool{items[3].ID: items[3].Done, items[4].ID: items[4].Done}
	assert.Equal(t, map[string]bool{"T002": true, "T006": true}, rest)
}

// A name has to stay one line, so only the first clause of a note becomes the
// title and the rest stays in the detail.
func TestPlan_TrimsALongNoteDownForTheName(t *testing.T) {
	order, err := legacy.ReadOrder(write(t, "_todo-order.yml",
		"order:\n  - DSC-1   # 短い見出し。このあとに長い説明が続く（補足つき）\n"))
	require.NoError(t, err)
	ledger, err := legacy.ReadLedger(write(t, "_todo.md", ledgerSample))
	require.NoError(t, err)

	items, err := legacy.Plan(ledger, order)
	require.NoError(t, err)

	assert.Equal(t, "DSC-1 短い見出し", items[0].Name)
	assert.Contains(t, items[0].Detail, "長い説明")
}

func TestPlan_RefusesDuplicates(t *testing.T) {
	ledger, err := legacy.ReadLedger(write(t, "_todo.md", ledgerSample))
	require.NoError(t, err)

	order, err := legacy.ReadOrder(write(t, "_todo-order.yml",
		"order:\n  - DSC-1\n  - DSC-1\n"))
	require.NoError(t, err)

	_, err = legacy.Plan(ledger, order)
	assert.ErrorContains(t, err, "twice")
}

func TestPlan_RefusesALedgerWithTheSameIDTwice(t *testing.T) {
	ledger, err := legacy.ReadLedger(write(t, "_todo.md",
		"- [ ] T002 | 2026-09-07 | 一つ目 | due:2026-09-07\n- [ ] T002 | 2026-09-07 | 二つ目 |\n"))
	require.NoError(t, err)
	order, err := legacy.ReadOrder(write(t, "_todo-order.yml", "order:\n  - T002\n"))
	require.NoError(t, err)

	_, err = legacy.Plan(ledger, order)
	assert.ErrorContains(t, err, "twice")
}
