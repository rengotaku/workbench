package main

import (
	"fmt"

	"github.com/spf13/cobra"
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
		Short: "長期のタスクにつなぐ（- で外す）",
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
