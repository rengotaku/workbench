// Command todo is the single entry point for the task list — the same one a
// person types and an assistant calls. Output is written for a person by
// default; --json gives the raw server response for scripting.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rengotaku/workbench/internal/cliguard"
	"github.com/rengotaku/workbench/internal/legacy"
	"github.com/rengotaku/workbench/internal/task"
	"github.com/rengotaku/workbench/internal/taskclient"
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
			"引数にタスク名を渡すと末尾に追加する。順序は人が決めるもので、期限からは導出しない。\n" +
			"端末から引数なしで実行すると、ブラウザで追加画面を開く。\n\n" +
			"  todo \"名前 [1]\" --link \"<URL> 一言\"   関連 URL を添えて追加する。名前の [1] は 1 つ目の --link を指す\n\n" +
			"英小文字の 1 語で始まり、どのサブコマンドとも一致しない引数は打ち間違いとみなし、追加せずに止める。\n" +
			"そのまま追加するなら todo -- 'show T094' のように -- の後ろに単一引用符で書く。",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		// main prints the error once; cobra would print it a second time.
		SilenceErrors: true,
		RunE:          runAdd,
	}

	root.PersistentFlags().StringVar(&serverURL, "server", "", "server URL (default $TODO_ORDER_URL or "+taskclient.DefaultBaseURL+")")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	root.Flags().String("due", "", "due date (YYYY-MM-DD)")
	root.Flags().String("detail", "", "detail text")
	root.Flags().String("topic", "", "話題（"+fmt.Sprint(task.MaxTopicLen)+" 文字まで）。着手順を話題で分けて見るための札")
	root.Flags().String("parent", "", "親にするタスクの ID。そのタスクの子として追加する")
	root.Flags().StringArray("link", nil, "関連 URL（\"<URL> 一言\" の形。一言は省略可）。書いた順に [1] [2] … と番号が付く")

	root.AddCommand(listCmd(), doneCmd(), reopenCmd(), removeCmd(), waitCmd(), resumeCmd(),
		somedayCmd(), orderCmd(), dueCmd(), detailCmd(), topicCmd(), annotateCmd(), renameCmd(), focusCmd(true), focusCmd(false),
		longCmd(true), longCmd(false), parentCmd(), reviewCmd(),
		undoCmd(), askCmd(), whyCmd(), contextCmd(), importCmd())
	return root
}

func newClient() (*taskclient.Client, error) { return taskclient.New(serverURL) }

// ---- add ----

func runAdd(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if !isTerminal(os.Stdin) {
			return cmd.Help()
		}
		return openAddPage()
	}
	if err := cliguard.UnknownSubcommand(cmd, args); err != nil {
		return err
	}
	due, _ := cmd.Flags().GetString("due")
	detail, _ := cmd.Flags().GetString("detail")
	parent, _ := cmd.Flags().GetString("parent")
	topic, _ := cmd.Flags().GetString("topic")
	// Checked before anything is created, so a bad topic adds nothing.
	topic, err := task.NormalizeTopic(topic)
	if err != nil {
		return err
	}
	name := strings.Join(args, " ")
	rawLinks, _ := cmd.Flags().GetStringArray("link")
	links := make([]task.LinkInput, 0, len(rawLinks))
	for _, l := range rawLinks {
		links = append(links, task.ParseLinkInput(l))
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	var (
		t   taskclient.Task
		raw []byte
	)
	if parent != "" {
		t, raw, err = c.AddChild(parent, name, detail, due, links...)
	} else {
		t, raw, err = c.Add(name, detail, due, links...)
	}
	if err != nil {
		return err
	}
	if topic != "" {
		// SetTopic returns a zero Task on failure, so keep the added ID apart.
		added := t.ID
		tt, rawTopic, terr := c.SetTopic(added, topic)
		if terr != nil {
			return fmt.Errorf("追加 %s は済んだが話題を付けられなかった（todo topic %s %s で付け直せる）: %w", added, added, topic, terr)
		}
		t, raw = tt, rawTopic
	}
	if asJSON {
		return printRaw(raw)
	}
	fmt.Printf("追加 %s  %s%s\n", t.ID, t.Name, dueSuffix(t))
	return nil
}

// isTerminal reports whether f is attached to a terminal. Only then does a
// bare `todo` open the browser; from a pipe or a script it keeps printing help
// so a non-interactive caller never launches a window.
// A character-device check is not enough: /dev/null is one too, and it is
// what cron, launchd and tool runners hand a process as stdin.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// openBrowser is swapped out in tests so they never launch a real window.
var openBrowser = func(url string) error { return exec.Command("open", url).Run() }

// openAddPage opens the web page with the add field ready for typing.
func openAddPage() error {
	c, err := newClient()
	if err != nil {
		return err
	}
	url := addPageURL(c.BaseURL())
	if err := openBrowser(url); err != nil {
		return fmt.Errorf("ブラウザを開けませんでした (%s): %w", url, err)
	}
	fmt.Println("ブラウザで開きました:", url)
	return nil
}

// addPageURL is the page that is nothing but the add field.
func addPageURL(base string) string { return strings.TrimRight(base, "/") + "/add" }

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
	c.Flags().StringVar(&scope, "scope", "", `スコープ: "week"（今週の日曜まで。既定）、"2weeks"、日数、または "all"`)
	return c
}

func printBoard(b taskclient.Board) {
	fmt.Printf("着手順  %s  スコープ %d日  超過 %d / 今日 %d / その他 %d\n",
		b.Today, b.ScopeDays, b.Counts.Over, b.Counts.Near, b.Counts.Rest)

	if n := reviewCount(b.Long); n > 0 {
		fmt.Printf("長期の見直し %d 件\n", n)
	}
	if len(b.Focus) > 0 {
		fmt.Println("\n[フォーカス]")
		for _, t := range b.Focus {
			fmt.Printf("  ◎ %-6s %s%s%s%s\n", t.ID, t.Name, dueSuffix(t), idleSuffix(t), topicMark(t))
		}
	}
	if len(b.Queue) > 0 {
		fmt.Println("\n[着手順]")
		for i, t := range b.Queue {
			fmt.Printf("%3d %-6s %s%s%s%s%s%s\n", i+1, t.ID, t.Name, detailMark(t), parentMark(t), dueSuffix(t), idleSuffix(t), topicMark(t))
		}
	}
	if len(b.Long) > 0 {
		fmt.Println("\n[長期]")
		for _, t := range b.Long {
			fmt.Printf("  ▶ %-6s %s  子 完了 %d / 未完 %d  %s%s%s\n", t.ID, t.Name, t.KidsDone, t.KidsOpen,
				reviewDay(t.ReviewOn), idleSuffix(t.Task), reviewReasons(t))
			if t.Next != nil {
				fmt.Printf("           次の一手 %s %s\n", t.Next.ID, t.Next.Name)
			} else {
				fmt.Println("           次の一手なし")
			}
		}
	}
	if len(b.Waiting) > 0 {
		fmt.Println("\n[待ち]")
		for _, t := range b.Waiting {
			fmt.Printf("  ◷ %-6s %s  %s へ %s に渡した（%d日経過）\n",
				t.ID, t.Name, t.HandedTo, t.HandedOn, t.StalledFor)
		}
	}
	if len(b.Someday) > 0 {
		fmt.Println("\n[いつか]")
		// The API already sorts by Until with arrived dates first, so what to
		// look at soonest sits at the top without the CLI re-sorting anything.
		for _, t := range b.Someday {
			mark := " "
			if t.UntilReached {
				mark = "●"
			}
			until := t.Until
			if until == "" {
				until = "?"
			}
			fmt.Printf(" %s %-6s %s  %s まで\n", mark, t.ID, t.Name, until)
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

// reviewCount is how many long-run tasks need a review.
func reviewCount(long []taskclient.LongTask) int {
	n := 0
	for _, t := range long {
		if t.NeedsReview {
			n++
		}
	}
	return n
}

// reviewDay writes a long-run task's next review date as 見る M/D.
func reviewDay(on string) string {
	if on == "" {
		return "見る 未設定"
	}
	return "見る " + monthDay(on)
}

// monthDay writes YYYY-MM-DD as M/D.
func monthDay(ymd string) string {
	d, err := time.Parse(task.DateLayout, ymd)
	if err != nil {
		return ymd
	}
	return fmt.Sprintf("%d/%d", int(d.Month()), d.Day())
}

// reviewReasons says in words why a long-run task needs a review; empty when it
// does not.
func reviewReasons(t taskclient.LongTask) string {
	if !t.NeedsReview {
		return ""
	}
	var parts []string
	for _, r := range t.ReviewReasons {
		switch r {
		case "no_next":
			parts = append(parts, "次の一手なし")
		case "review_due":
			if t.ReviewOn == "" {
				parts = append(parts, "見直し日が未設定")
			} else {
				parts = append(parts, "見直し日 "+monthDay(t.ReviewOn)+" を過ぎた")
			}
		case "stalled":
			parts = append(parts, fmt.Sprintf("止まって %d日", t.IdleDays))
		}
	}
	return "  要見直し: " + strings.Join(parts, "・")
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

// focusCmd builds both `focus` and `unfocus`: the same call with the flag
// flipped, so the two cannot drift apart.
func focusCmd(on bool) *cobra.Command {
	use, short := "unfocus <ID>...", "フォーカスから外す"
	if on {
		use, short = "focus <ID>...", "フォーカス（画面上部の一覧）に入れる"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			for _, id := range args {
				t, raw, err := cl.Focus(id, on)
				if err != nil {
					return err
				}
				if asJSON {
					if err := printRaw(raw); err != nil {
						return err
					}
					continue
				}
				if on {
					fmt.Printf("フォーカス %s  %s\n", t.ID, t.Name)
				} else {
					fmt.Printf("フォーカス解除 %s  %s\n", t.ID, t.Name)
				}
			}
			return nil
		},
	}
}

func undoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "直前の変更を取り消す（繰り返すとさらに前へ）",
		Long: "直前の変更を取り消す。画面からの操作も端末からの操作も同じ順で戻せる。\n" +
			"取り消し自体もイベントとして残るので、履歴は消えない。",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			done, raw, err := cl.Undo()
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Println(done.Summary)
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
			"ID はスペースでもカンマでも区切れる（`todo rm T001,T002 T003`）。\n" +
			"取り除いた名前と期限はイベントログに残るので、何を消したかは追える。",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ids := splitIDs(args)
			if len(ids) == 0 {
				return fmt.Errorf("ID がありません")
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			// One at a time, stopping at the first failure: a typo must not
			// take out the rest of the list behind it.
			for _, id := range ids {
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

// splitIDs turns arguments into IDs, splitting each on commas as well, so a
// list copied as "T001,T002" works like "T001 T002". Empty parts and repeats
// are dropped: a repeated ID would fail as already gone and stop the rest.
func splitIDs(args []string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, a := range args {
		for _, id := range strings.Split(a, ",") {
			if id = strings.TrimSpace(id); id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
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
		Short: "待ち／いつかから着手順へ戻す",
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

func somedayCmd() *cobra.Command {
	var until string
	c := &cobra.Command{
		Use:   "someday <ID>",
		Short: "いつかへ棚上げする（着手順・待ちから外す）",
		Long: "いつかへ棚上げする。いまは着手順に置かないが、いつか再開したいタスクの置き場で、\n" +
			"--until を省略すると今日から1ヶ月後になる。着手順へ戻すのは `todo resume`。\n" +
			"すでにいつかのタスクに打つと、着手順を経由せず期限だけを延ばす。",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			before, _, _, err := cl.Context(args[0])
			if err != nil {
				return err
			}
			wasSomeday := before.State == task.StateSomeday

			t, raw, err := cl.Shelve(args[0], until)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			if wasSomeday {
				fmt.Printf("いつか %s  %s（%s へ延ばした）\n", t.ID, t.Name, t.Until)
			} else {
				fmt.Printf("いつか %s  %s（%s まで見なくていい）\n", t.ID, t.Name, t.Until)
			}
			return nil
		},
	}
	c.Flags().StringVar(&until, "until", "", "この日まで見なくていい（YYYY-MM-DD、省略時は今日+1ヶ月）。すでにいつかのタスクに打つと期限を延ばす")
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

func annotateCmd() *cobra.Command {
	var kind, body, bodyFile, source string
	c := &cobra.Command{
		Use:   "annotate <ID> --kind quote|analysis|memo|progress|blocked (--body TEXT | --body-file PATH) [--source NAME]",
		Short: "補足を追記する（追記のみ。undo の対象にならない）",
		Long: "タスクに補足（引用・考察・メモ・進んだ・詰まり）を追記する。既存の補足は変えず、todo undo でも取り消されない。\n" +
			"書いた人は TODO_ORDER_ACTOR（human / ai）で決まる。",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind == "" {
				return errors.New("--kind が要ります。quote / analysis / memo / progress / blocked のどれかを指定してください")
			}
			hasBody, hasFile := cmd.Flags().Changed("body"), cmd.Flags().Changed("body-file")
			switch {
			case hasBody == hasFile:
				return errors.New("--body か --body-file のどちらか一方を指定してください")
			case hasFile:
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("--body-file を読めません: %w", err)
				}
				body = string(b)
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			_, raw, err := cl.Annotate(args[0], task.AnnotationInput{Kind: kind, Body: body, Source: source})
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("補足 %s  %s\n", args[0], kind)
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "種別: quote（引用）/ analysis（考察）/ memo（メモ）/ progress（進んだ）/ blocked（詰まり）")
	c.Flags().StringVar(&body, "body", "", "補足の本文")
	c.Flags().StringVar(&bodyFile, "body-file", "", "補足の本文を読むファイル")
	c.Flags().StringVar(&source, "source", "", "出どころ（任意。例: slack）")
	return c
}

func topicCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "topic <ID> <話題|->",
		Short: "話題を付ける（- で外す）",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			topic := strings.Join(args[1:], " ")
			if topic == "-" {
				topic = ""
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.SetTopic(args[0], topic)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			if t.Topic == "" {
				fmt.Printf("話題を外した %s  %s\n", t.ID, t.Name)
			} else {
				fmt.Printf("話題 %s  %s  {%s}\n", t.ID, t.Name, t.Topic)
			}
			return nil
		},
	}
}

func renameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <ID> <新しい名前>",
		Short: "タスク名を変える",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.SetName(args[0], strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("改名 %s  %s\n", t.ID, t.Name)
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

// idleSuffix says how many days a task has been left alone, when it has been.
func idleSuffix(t taskclient.Task) string {
	if t.IdleDays <= 0 {
		return ""
	}
	return fmt.Sprintf("  止まって %d日", t.IdleDays)
}

func dueSuffix(t taskclient.Task) string {
	if t.Due == "" {
		return ""
	}
	// The label coarsens past a week (taskapi.Span), so the date goes with it:
	// the CLI has no hover to recover it.
	if strings.HasSuffix(t.DueLabel, " から") {
		// The label drops the year, so the full date stands in for it.
		return "  [" + t.Due + " から]"
	}
	if t.DueLabel != "" {
		return "  [" + t.Due + " " + t.DueLabel + "]"
	}
	return "  [" + t.Due + "]"
}

// topicMark puts the topic at the end of a row as {話題}.
func topicMark(t taskclient.Task) string {
	if t.Topic == "" {
		return ""
	}
	return "  {" + t.Topic + "}"
}

func detailMark(t taskclient.Task) string {
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

// parentMark names the task a queue row was cut out of.
func parentMark(t taskclient.Task) string {
	if t.Parent == "" {
		return ""
	}
	return "  (親 " + t.Parent + ")"
}
