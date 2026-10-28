package main

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/rengotaku/workbench/internal/task"
)

// longCmd builds both `long` and `unlong`, the same call with the flag flipped
// (ADR todo-0019).
func longCmd(on bool) *cobra.Command {
	use, short := "unlong <ID>...", "長期から外す（着手順の先頭へ戻す）"
	if on {
		use, short = "long <ID>...", "長期にする（着手順から外し、長期の枠に置く）"
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
				t, raw, err := cl.Long(id, on)
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
					fmt.Printf("長期 %s  %s\n", t.ID, t.Name)
				} else {
					fmt.Printf("長期から外した %s  %s\n", t.ID, t.Name)
				}
			}
			return nil
		},
	}
}

func parentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "parent <ID> <親ID|->",
		Short: "親のタスクにつなぐ（- で外す）",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			parent := args[1]
			if parent == "-" {
				parent = ""
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.SetParent(args[0], parent)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			if t.Parent == "" {
				fmt.Printf("親を外した %s  %s\n", t.ID, t.Name)
			} else {
				fmt.Printf("親 %s ← %s  %s\n", t.Parent, t.ID, t.Name)
			}
			return nil
		},
	}
}

func reviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review <ID> [YYYY-MM-DD|+1w|+2w|+1m]",
		Short: "長期のタスクを見直した印を付け、次に見る日を決める（既定は +1w）",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			on := "+1w"
			if len(args) == 2 {
				on = args[1]
			}
			day, err := reviewDate(on, time.Now())
			if err != nil {
				return err
			}
			cl, err := newClient()
			if err != nil {
				return err
			}
			t, raw, err := cl.Review(args[0], day)
			if err != nil {
				return err
			}
			if asJSON {
				return printRaw(raw)
			}
			fmt.Printf("見直した %s  %s  次に見る日 %s\n", t.ID, t.Name, t.ReviewOn)
			return nil
		},
	}
}

var relativeDate = regexp.MustCompile(`^\+(\d+)([dwm])$`)

// reviewDate turns the argument into YYYY-MM-DD: a date as it is, or +Nd /
// +Nw / +Nm counted from now (days, weeks, months).
func reviewDate(arg string, now time.Time) (string, error) {
	if m := relativeDate.FindStringSubmatch(arg); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", fmt.Errorf("日付の数が大きすぎる: %s", arg)
		}
		d := now
		switch m[2] {
		case "d":
			d = d.AddDate(0, 0, n)
		case "w":
			d = d.AddDate(0, 0, 7*n)
		case "m":
			d = d.AddDate(0, n, 0)
		}
		return d.Format(task.DateLayout), nil
	}
	if _, err := task.ParseDate(arg, time.Local); err != nil {
		return "", fmt.Errorf("YYYY-MM-DD か +1w / +2w / +1m で指定する: %q", arg)
	}
	return arg, nil
}
