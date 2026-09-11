// Command todo is the single entry point for the task list — the same one a
// person types and an assistant calls. Output is written for a person by
// default; --json gives the raw server response for scripting.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rengotaku/todo-order/internal/client"
	"github.com/rengotaku/todo-order/internal/legacy"
	"github.com/rengotaku/todo-order/internal/task"
)

var (
	serverURL string
	asJSON    bool
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "todo",
		Short: "着手順で持つ TODO リスト",
		Long: "着手順で持つ TODO リスト。\n\n" +
			"引数にタスク名を渡すと末尾に追加する。順序は人が決めるもので、期限からは導出しない。",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE:         runAdd,
	}

	root.PersistentFlags().StringVar(&serverURL, "server", "", "server URL (default $TODO_ORDER_URL or "+client.DefaultBaseURL+")")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	root.Flags().String("due", "", "due date (YYYY-MM-DD)")
	root.Flags().String("detail", "", "detail text")

	root.AddCommand(listCmd(), doneCmd(), reopenCmd(), removeCmd(), waitCmd(), resumeCmd(),
		orderCmd(), dueCmd(), detailCmd(), askCmd(), whyCmd(), contextCmd(), importCmd())
	return root
}

func newClient() (*client.Client, error) { return client.New(serverURL) }

// ---- add ----

func runAdd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	due, _ := cmd.Flags().GetString("due")
	detail, _ := cmd.Flags().GetString("detail")

	c, err := newClient()
	if err != nil {
		return err
	}
	t, raw, err := c.Add(strings.Join(args, " "), detail, due)
	if err != nil {
		return err
	}
	if asJSON {
		return printRaw(raw)
	}
	fmt.Printf("追加 %s  %s%s\n", t.ID, t.Name, dueSuffix(t))
	return nil
}

// ---- list ----

func listCmd() *cobra.Command {
	var scope string
	c := &cobra.Command{
		Use:   "list",
		Short: "一覧を表示する",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			board, raw, err := cl.List(scope)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			printBoard(board)
			return nil
		},
	}
	c.Flags().StringVar(&scope, "scope", "", `スコープ: 日数、または "all"（既定は 7）`)
	return c
}

func printBoard(b client.Board) {
	fmt.Printf("着手順  %s  スコープ %d日  超過 %d / 期限内 %d / 外 %d\n",
		b.Today, b.ScopeDays, b.Counts.Over, b.Counts.Near, b.Counts.Rest)

	if len(b.Queue) > 0 {
		fmt.Println("\n[着手順]")
		for i, t := range b.Queue {
			fmt.Printf("%3d %-6s %s%s%s\n", i+1, t.ID, t.Name, detailMark(t), dueSuffix(t))
		}
	}
	if len(b.Waiting) > 0 {
		fmt.Println("\n[待ち]")
		for _, t := range b.Waiting {
			fmt.Printf("  ◷ %-6s %s  %s へ %s に渡した（%d日経過）\n",
				t.ID, t.Name, t.HandedTo, t.HandedOn, t.StalledFor)
		}
	}
	if len(b.Done) > 0 {
		fmt.Println("\n[今週完了]")
		for _, t := range b.Done {
			done := ""
			if t.CompletedAt != nil {
				done = t.CompletedAt.Format("1/2")
			}
			fmt.Printf("  ✓ %-6s %s  %s\n", t.ID, t.Name, done)
		}
	}
}

// ---- state changes ----

func doneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <ID>...",
		Short: "完了にする",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			// Each ID is applied in turn and the first failure stops the run,
			// so a typo never looks like a completed batch.
			for _, id := range args {
				t, raw, err := cl.Done(id)
				if err != nil {
					return err
				}
				if asJSON {
					if err := printRaw(raw); err != nil {
						return err
					}
					continue
				}
				fmt.Printf("完了 %s  %s\n", t.ID, t.Name)
			}
			return nil
		},
	}
}

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <ID>...",
		Aliases: []string{"remove"},
		Short:   "タスクを取り除く（戻せない）",
		Long: "タスクを取り除く。**戻せない**。誤って追加したものを消すためのもので、\n" +
			"やめた作業は `done` か、詳細に理由を書いて残す方が経緯が読める。\n\n" +
			"取り除いた名前と期限はイベントログに残るので、何を消したかは追える。",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			// One at a time, stopping at the first failure: a typo must not
			// take out the rest of the list behind it.
			for _, id := range args {
				t, raw, err := cl.Remove(id)
				if err != nil {
					return err
				}
				if asJSON {
					if err := printRaw(raw); err != nil {
						return err
					}
					continue
				}
				fmt.Printf("削除 %s  %s%s\n", t.ID, t.Name, dueSuffix(t))
			}
			return nil
		},
	}
}

func reopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <ID>...",
		Short: "完了を取り消して着手順の末尾へ戻す",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			for _, id := range args {
				t, raw, err := cl.Reopen(id)
				if err != nil {
					return err
				}
				if asJSON {
					if err := printRaw(raw); err != nil {
						return err
					}
					continue
				}
				fmt.Printf("復活 %s  %s（着手順の末尾）\n", t.ID, t.Name)
			}
			return nil
		},
	}
}

func waitCmd() *cobra.Command {
	var to, note string
	c := &cobra.Command{
		Use:   "wait <ID>",
		Short: "待ちにする（ボールを渡す）",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.Wait(args[0], to, note)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("待ち %s  %s → %s（%s に渡した）\n", t.ID, t.Name, t.HandedTo, t.HandedOn)
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "渡した相手（必須）")
	c.Flags().StringVar(&note, "note", "", "何を待っているか")
	return c
}

func resumeCmd() *cobra.Command {
	var note string
	c := &cobra.Command{
		Use:   "resume <ID>",
		Short: "待ちから着手順へ戻す",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.Resume(args[0], note)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("復帰 %s  %s\n", t.ID, t.Name)
			return nil
		},
	}
	c.Flags().StringVar(&note, "note", "", "何が返ってきたか")
	return c
}

func orderCmd() *cobra.Command {
	var scopeDays int
	c := &cobra.Command{
		Use:   "order <ID> <位置>",
		Short: "着手順の位置を変える（1 が先頭）",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			pos, err := parsePosition(args[1])
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			// The CLI moves a task by its own reckoning rather than from a
			// list it read a moment ago, so there is no revision to check.
			t, raw, err := cl.Order(args[0], pos, scopeDays, 0)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("移動 %s  %s → %d 番目\n", t.ID, t.Name, t.Order+1)
			return nil
		},
	}
	c.Flags().IntVar(&scopeDays, "scope-days", 7, "期限の近さを判定するスコープ（日）")
	return c
}

func dueCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "due <ID> <YYYY-MM-DD|->",
		Short: "期限を設定する（- で解除）",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			due := args[1]
			if due == "-" {
				due = ""
			}
			t, raw, err := cl.SetDue(args[0], due)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			if t.Due == "" {
				fmt.Printf("期限解除 %s  %s\n", t.ID, t.Name)
			} else {
				fmt.Printf("期限 %s  %s  %s\n", t.ID, t.Name, t.Due)
			}
			return nil
		},
	}
}

func detailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detail <ID> <本文>",
		Short: "詳細を書く",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.SetDetail(args[0], strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("詳細 %s  %s\n", t.ID, t.Name)
			return nil
		},
	}
}

// ---- context ----

func askCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask",
		Short: "理由が空で、説明が要る変化の一覧",
		Long: "説明が要る変化（1位に上げた / 最下位へ落とした / 期限が近いものを飛び越えた / " +
			"待ちに14日以上）のうち、理由が記録されていないものを出す。",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			pending, raw, err := cl.Ask()
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			if len(pending) == 0 {
				fmt.Println("理由を聞くべき変化はない")
				return nil
			}
			for _, p := range pending {
				fmt.Printf("%s  %s  %s\n", p.Event.ID, p.Task.ID, p.Task.Name)
				fmt.Printf("        %s  %s\n", p.Event.At.Format("01-02 15:04"), p.Trigger)
				for _, j := range p.Event.Jumped {
					fmt.Printf("        飛び越えた: %s %s（期限 %s）\n", j.ID, j.Name, j.Due)
				}
			}
			fmt.Printf("\n理由を書く: todo why <イベントID> \"<理由>\"\n")
			return nil
		},
	}
}

func whyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "why <イベントID> <理由>",
		Short: "イベントに理由を書き込む",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			e, raw, err := cl.Why(args[0], strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("記録 %s  %s\n", e.ID, e.Reason)
			return nil
		},
	}
}

func contextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context <ID>",
		Short: "そのタスクの経緯を時系列で出す",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, events, raw, err := cl.Context(args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("%s  %s\n", t.ID, t.Name)
			if t.Detail != "" {
				fmt.Printf("\n[詳細]\n%s\n", t.Detail)
			}
			fmt.Println("\n[経緯]")
			for _, e := range events {
				fmt.Printf("  %s  %-13s %s%s\n",
					e.At.Format("01-02 15:04"), e.Kind, changeOf(e), actorTag(e))
				if e.Reason != "" {
					fmt.Printf("                 理由: %s\n", e.Reason)
				}
			}
			return nil
		},
	}
}

// ---- migration ----

func importCmd() *cobra.Command {
	var ledgerPath, orderPath string
	var dryRun bool

	c := &cobra.Command{
		Use:   "import",
		Short: "旧台帳（TODO ファイル + 順序ファイル）から移行する",
		Long: "旧台帳をこのアプリへ移す。順序ファイルの並びがそのまま着手順になり、\n" +
			"行に付いていたコメントは詳細として残る。1 回だけ実行する（既にタスクが\n" +
			"入っているときは何も書き込まずに止まる）。",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ledger, err := legacy.ReadLedger(ledgerPath)
			if err != nil {
				return err
			}
			order, err := legacy.ReadOrder(orderPath)
			if err != nil {
				return err
			}
			items, err := legacy.Plan(ledger, order)
			if err != nil {
				return err
			}

			if dryRun {
				printPlan(items)
				return nil
			}

			cl, err := newClient()
			if err != nil {
				return err
			}
			n, raw, err := cl.Import(items)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("移行 %d 件\n", n)
			fmt.Println("旧台帳はこのあと参照されない。集計スクリプトのデータ源を切り替えること")
			return nil
		},
	}
	c.Flags().StringVar(&ledgerPath, "ledger", "", "旧 TODO ファイル（必須）")
	c.Flags().StringVar(&orderPath, "order", "", "旧 順序ファイル（必須）")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "書き込まずに移行計画だけ出す")
	_ = c.MarkFlagRequired("ledger")
	_ = c.MarkFlagRequired("order")
	return c
}

// printPlan shows what an import would do, so the plan can be checked before
// anything is written.
func printPlan(items []task.ImportItem) {
	fmt.Printf("移行計画 %d 件（--dry-run なので書き込んでいない）\n\n", len(items))
	for i, it := range items {
		id := it.ID
		if id == "" {
			id = "（新規採番）"
		}
		state := ""
		if it.Done {
			state = "  [完了]"
		}
		due := ""
		if it.Due != "" {
			due = "  期限 " + it.Due
		}
		fmt.Printf("%3d %-12s %s%s%s\n", i+1, id, it.Name, due, state)
		if it.Detail != "" {
			for _, line := range strings.Split(it.Detail, "\n") {
				if strings.TrimSpace(line) != "" {
					fmt.Printf("             │ %s\n", line)
				}
			}
		}
	}
}

// ---- formatting ----

func changeOf(e task.Event) string {
	switch e.Kind {
	case task.KindReordered:
		return fmt.Sprintf("%d → %d 番目", e.FromOrder+1, e.ToOrder+1)
	case task.KindDueSet:
		from := e.FromValue
		if from == "" {
			from = "なし"
		}
		to := e.ToValue
		if to == "" {
			to = "なし"
		}
		return from + " → " + to
	case task.KindWaiting:
		return e.ToValue + " へ渡した"
	case task.KindResumed:
		return e.FromValue + " から戻した"
	default:
		return e.ToValue
	}
}

func actorTag(e task.Event) string {
	if e.Actor == task.ActorAI {
		return "  (AI)"
	}
	return ""
}

func dueSuffix(t client.Task) string {
	if t.Due == "" {
		return ""
	}
	if t.DueLabel != "" {
		return "  [" + t.DueLabel + "]"
	}
	return "  [" + t.Due + "]"
}

func detailMark(t client.Task) string {
	if t.Detail == "" {
		return ""
	}
	return " +"
}

func parsePosition(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("位置は 1 以上の整数: %q", s)
	}
	if n < 1 {
		return 0, fmt.Errorf("位置は 1 以上: %d", n)
	}
	return n - 1, nil
}

func printRaw(raw []byte) error {
	_, err := os.Stdout.Write(append(raw, '\n'))
	return err
}
