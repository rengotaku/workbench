// Command note is the single entry point for the memo store: the same one a
// person types and an AI calls. It reads and writes only through the server.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rengotaku/workbench/internal/cliguard"
	"github.com/rengotaku/workbench/internal/note"
	"github.com/rengotaku/workbench/internal/noteclient"
	"github.com/rengotaku/workbench/internal/noteroute"
)

// env is what the commands touch outside themselves, so tests can replace it.
type env struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	openURL    func(url string) error
	serverURL  string // --server; "" falls back to $NOTE_SERVER, then the default
	stdinIsTTY bool
	asJSON     bool
}

func main() {
	e := &env{
		stdin:      os.Stdin,
		stdinIsTTY: term.IsTerminal(int(os.Stdin.Fd())),
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		// `open` is swapped out in tests so they never launch a window.
		openURL: func(url string) error { return exec.Command("open", url).Run() },
	}
	os.Exit(execute(e, os.Args[1:]))
}

// execute runs one invocation and returns the exit code.
func execute(e *env, args []string) int {
	root := rootCmd(e)
	root.SetArgs(args)
	root.SetOut(e.stdout)
	root.SetErr(e.stderr)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(e.stderr, "error:", err)
		return 1
	}
	return 0
}

func rootCmd(e *env) *cobra.Command {
	root := &cobra.Command{
		Use:   "note",
		Short: "1 行メモを溜めて、あとで補足と話題を足す",
		Long: "1 行メモ（リンクと一言）を溜め、あとから補足（引用・考察）と話題を追記して読む。\n\n" +
			"  note                 端末から引数なしで実行すると、ブラウザで登録画面を開く\n" +
			"  note 文字列...        その文字列を 1 件のメモとして登録する\n" +
			"  pbpaste | note       標準入力の全体を 1 件のメモとして登録する\n" +
			"  note \"本文 [1]\" --link \"<URL> 一言\"   関連 URL を添えて登録する。本文の [1] は 1 つ目の --link を指す\n\n" +
			"  note list --pending  AI がまだ拾っていないメモ\n" +
			"  note route <id> --todo 名前 | --record   拾ったメモの行き先を決める\n\n" +
			"英小文字の 1 語で始まり、どのサブコマンドとも一致しない引数は打ち間違いとみなし、登録せずに止める。\n" +
			"そのまま登録するなら note -- 文字列... か note \"文字列...\" と書く。\n\n" +
			"サーバ（既定 " + noteclient.DefaultBaseURL + "、$NOTE_SERVER で変更）経由でのみ読み書きする。",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          e.runAdd,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&e.serverURL, "server", "", "サーバの URL（既定 $NOTE_SERVER または "+noteclient.DefaultBaseURL+"）")
	root.PersistentFlags().BoolVar(&e.asJSON, "json", false, "サーバの応答の JSON をそのまま出す")
	root.Flags().String("topic", "", "話題")
	root.Flags().StringArray("link", nil, "関連 URL（\"<URL> 一言\" の形。一言は省略可）。書いた順に [1] [2] … と番号が付く")

	root.AddCommand(e.listCmd(), e.showCmd(), e.annotateCmd(), e.routeCmd(), e.topicCmd(), e.topicsCmd(), e.deleteCmd())
	return root
}

func (e *env) client() (*noteclient.Client, error) { return noteclient.New(e.serverURL) }

func (e *env) println(a ...any) { _, _ = fmt.Fprintln(e.stdout, a...) }

func (e *env) printRaw(raw []byte) {
	_, _ = e.stdout.Write(raw)
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		_, _ = fmt.Fprintln(e.stdout)
	}
}

// ---- add ----

func (e *env) runAdd(cmd *cobra.Command, args []string) error {
	topic, _ := cmd.Flags().GetString("topic")
	rawLinks, _ := cmd.Flags().GetStringArray("link")
	links := make([]note.LinkInput, 0, len(rawLinks))
	for _, l := range rawLinks {
		links = append(links, note.ParseLinkInput(l))
	}

	if len(args) == 0 && e.stdinIsTTY {
		if len(links) > 0 {
			return errors.New("--link を付けるときは本文も渡してください。note \"本文 [1]\" --link <URL> の形で入れてください")
		}
		return e.openAddPage()
	}

	var text string
	if len(args) > 0 {
		if err := cliguard.UnknownSubcommand(cmd, args); err != nil {
			return err
		}
		text = strings.Join(args, " ")
	} else {
		b, err := io.ReadAll(e.stdin)
		if err != nil {
			return fmt.Errorf("標準入力を読めません: %w", err)
		}
		text = string(b)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("メモが空です。note 文字列、または pbpaste | note の形で入れてください")
	}

	c, err := e.client()
	if err != nil {
		return err
	}
	n, raw, err := c.Add(text, strings.TrimSpace(topic), links...)
	if err != nil {
		return err
	}
	if e.asJSON {
		e.printRaw(raw)
		return nil
	}
	e.println(fmt.Sprintf("note #%d 保存", n.ID))
	return nil
}

// openAddPage opens the screen with the field ready for typing.
func (e *env) openAddPage() error {
	c, err := e.client()
	if err != nil {
		return err
	}
	url := c.BaseURL() + "/notes"
	if err := e.openURL(url); err != nil {
		return fmt.Errorf("ブラウザを開けませんでした（%s）: %w", url, err)
	}
	e.println("ブラウザで開きました:", url)
	return nil
}

// ---- list / show ----

func (e *env) listCmd() *cobra.Command {
	var f note.Filter
	c := &cobra.Command{
		Use:   "list",
		Short: "メモを新しい順に一覧する",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := e.client()
			if err != nil {
				return err
			}
			r, raw, err := cl.List(f)
			if err != nil {
				return err
			}
			if e.asJSON {
				e.printRaw(raw)
				return nil
			}
			for _, n := range r.Notes {
				e.println(listLine(n))
			}
			return nil
		},
	}
	c.Flags().StringVar(&f.Topic, "topic", "", "この話題のメモだけ")
	c.Flags().BoolVar(&f.Pending, "pending", false, "補足待ち（AI がまだ拾っていない = 行き先が未決）のメモだけ")
	c.Flags().BoolVar(&f.NoTopic, "no-topic", false, "話題のないメモだけ")
	return c
}

func listLine(n note.Note) string {
	topic := ""
	if n.Topic != "" {
		topic = "[" + n.Topic + "]  "
	}
	return fmt.Sprintf("#%d  %s  %s%s  (補足 %d)", n.ID, n.CreatedAt.Local().Format("01/02 15:04"), topic, n.Text, len(n.Annotations))
}

func (e *env) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "メモと補足を全部出す",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			cl, err := e.client()
			if err != nil {
				return err
			}
			n, raw, err := cl.Get(id)
			if err != nil {
				return err
			}
			if e.asJSON {
				e.printRaw(raw)
				return nil
			}
			e.println(showText(n))
			return nil
		},
	}
}

func showText(n note.Note) string {
	var b strings.Builder
	b.WriteString(listLine(n))
	if n.URL != "" {
		fmt.Fprintf(&b, "\n  リンク: %s（%s）", n.URL, note.SourceLabel(n.URL))
	}
	for _, l := range n.Links {
		fmt.Fprintf(&b, "\n  [%d] %s（%s）", l.N, l.URL, note.SourceLabel(l.URL))
		if l.Label != "" {
			b.WriteString(" " + l.Label)
		}
	}
	switch {
	case n.Route != nil && n.Route.To == note.RouteTodo:
		fmt.Fprintf(&b, "\n  → todo %s", n.Route.TaskID)
	case n.Route != nil:
		b.WriteString("\n  → 記録")
	case n.Pending():
		b.WriteString("\n  補足待ち")
	}
	for _, a := range n.Annotations {
		fmt.Fprintf(&b, "\n\n--- %s · %s · %s", a.Kind, a.Author, a.CreatedAt.Local().Format("01/02 15:04"))
		if a.Source != "" {
			fmt.Fprintf(&b, " · %s", a.Source)
		}
		b.WriteString("\n" + strings.TrimRight(a.Body, "\n"))
	}
	return b.String()
}

// ---- annotate / topic / delete ----

func (e *env) annotateCmd() *cobra.Command {
	var kind, body, bodyFile, source, author string
	c := &cobra.Command{
		Use:   "annotate <id> --kind quote|analysis|memo (--body TEXT | --body-file PATH)",
		Short: "補足を追記する（追記のみ。既存の補足は変えない）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			if kind == "" {
				return errors.New("--kind が要ります。quote / analysis / memo のどれかを指定してください")
			}
			hasBody, hasFile := cmd.Flags().Changed("body"), cmd.Flags().Changed("body-file")
			switch {
			case hasBody == hasFile:
				return errors.New("--body か --body-file のどちらか一方を指定してください")
			case hasFile:
				b, readErr := os.ReadFile(bodyFile)
				if readErr != nil {
					return fmt.Errorf("--body-file を読めません: %w", readErr)
				}
				body = string(b)
			}
			cl, err := e.client()
			if err != nil {
				return err
			}
			if _, err := cl.Annotate(id, note.AnnotationInput{Kind: kind, Body: body, Source: source, Author: author}); err != nil {
				return err
			}
			e.println(fmt.Sprintf("note #%d 追記", id))
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "種別: quote（引用）/ analysis（考察）/ memo（メモ）")
	c.Flags().StringVar(&body, "body", "", "補足の本文")
	c.Flags().StringVar(&bodyFile, "body-file", "", "補足の本文を読むファイル")
	c.Flags().StringVar(&source, "source", "", "出どころ（任意。例: slack）")
	c.Flags().StringVar(&author, "author", "", "書いた人: ai（既定）/ human")
	return c
}

func (e *env) routeCmd() *cobra.Command {
	var todoName, due, detail string
	var record bool
	c := &cobra.Command{
		Use:   "route <id> (--todo 名前 [--due YYYY-MM-DD] [--detail TEXT] | --record)",
		Short: "拾ったメモの行き先を決める（todo を作る / 記録として残す）",
		Long: "メモを拾った AI が行き先を決める。行為なら --todo でタスクを作り、記録なら --record で残す。\n" +
			"どちらも、そのメモは補足待ちから外れる。同じメモに同じ行き先でもう一度実行しても、タスクは増えない。\n" +
			"別の行き先に変えることはできない。",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			hasTodo := cmd.Flags().Changed("todo")
			switch {
			case hasTodo == record:
				return errors.New("--todo 名前 か --record のどちらか一方を指定してください")
			case record && (due != "" || detail != ""):
				return errors.New("--due / --detail は --todo のときだけ指定できます")
			case hasTodo && strings.TrimSpace(todoName) == "":
				return errors.New("--todo にタスクの名前を渡してください")
			}
			cl, err := e.client()
			if err != nil {
				return err
			}
			var (
				res noteroute.Response
				raw []byte
			)
			if record {
				res, raw, err = cl.RouteRecord(id)
			} else {
				res, raw, err = cl.RouteTodo(id, noteroute.TaskInput{Name: strings.TrimSpace(todoName), Due: due, Detail: detail})
			}
			if err != nil {
				return err
			}
			if e.asJSON {
				e.printRaw(raw)
				return nil
			}
			if res.Task != nil {
				e.println(fmt.Sprintf("note #%d → todo %s", id, res.Task.ID))
			} else {
				e.println(fmt.Sprintf("note #%d → 記録", id))
			}
			return nil
		},
	}
	c.Flags().StringVar(&todoName, "todo", "", "タスクを作る。タスクの名前")
	c.Flags().StringVar(&due, "due", "", "タスクの期限（YYYY-MM-DD。--todo のとき任意）")
	c.Flags().StringVar(&detail, "detail", "", "タスクの詳細（--todo のとき任意）")
	c.Flags().BoolVar(&record, "record", false, "記録として残す（タスクは作らない）")
	return c
}

func (e *env) topicCmd() *cobra.Command {
	var drop bool
	c := &cobra.Command{
		Use:   "topic <id> <話題>",
		Short: "メモの話題を設定する（--clear で外す）",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			topic := ""
			switch {
			case drop && len(args) == 2:
				return errors.New("--clear のときは話題を渡さないでください")
			case !drop && len(args) == 1:
				return errors.New("話題を渡してください。外すときは --clear を付けます")
			case !drop:
				topic = args[1]
			}
			cl, err := e.client()
			if err != nil {
				return err
			}
			n, err := cl.SetTopic(id, topic)
			if err != nil {
				return err
			}
			if n.Topic == "" {
				e.println(fmt.Sprintf("note #%d 話題を外した", id))
			} else {
				e.println(fmt.Sprintf("note #%d 話題 %s", id, n.Topic))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&drop, "clear", false, "話題を外す")
	return c
}

func (e *env) topicsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "topics",
		Short: "話題の一覧",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := e.client()
			if err != nil {
				return err
			}
			topics, err := cl.Topics()
			if err != nil {
				return err
			}
			for _, t := range topics {
				e.println(t)
			}
			return nil
		},
	}
}

func (e *env) deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "メモを削除する（補足ごと消える）",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			cl, err := e.client()
			if err != nil {
				return err
			}
			if err := cl.Delete(id); err != nil {
				return err
			}
			e.println(fmt.Sprintf("note #%d 削除", id))
			return nil
		},
	}
}

// parseID accepts "41" and "#41".
func parseID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("id は 1 以上の数字にしてください（受け取った値: %q）", s)
	}
	return id, nil
}
