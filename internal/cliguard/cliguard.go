// Package cliguard keeps a mistyped subcommand from turning into a write.
//
// The todo and note roots take any words as the name or body to add, so
// `todo show T094` used to add a task called "show T094". A first argument that
// looks like a subcommand (one word of lowercase ASCII) and matches none is
// taken as a typo instead. Adding text that starts that way stays possible
// after `--`, or with the whole text quoted as one argument.
package cliguard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var commandLike = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// UnknownSubcommand returns an error when args[0] reads as a subcommand that
// root does not have. Cobra has already routed every real subcommand away, so
// reaching the root's RunE with such a word means it is not one.
func UnknownSubcommand(root *cobra.Command, args []string) error {
	if len(args) == 0 || root.ArgsLenAtDash() == 0 || !commandLike.MatchString(args[0]) {
		return nil
	}
	name := root.Name()
	var b strings.Builder
	fmt.Fprintf(&b, "%q というサブコマンドはありません。", args[0])
	// SuggestionsFor leaves the distance at 0 unless cobra's own unknown-command
	// path has set it; 2 is the distance that path uses.
	if root.SuggestionsMinimumDistance <= 0 {
		root.SuggestionsMinimumDistance = 2
	}
	if s := root.SuggestionsFor(args[0]); len(s) > 0 {
		fmt.Fprintf(&b, "%s のことですか。", strings.Join(s, " / "))
	}
	text := strings.Join(args, " ")
	// Quoting one word changes nothing the shell passes on, so only -- helps.
	if len(args) == 1 {
		fmt.Fprintf(&b, "この文字列のまま追加するなら %s -- %s と書いてください", name, text)
	} else {
		fmt.Fprintf(&b, "この文字列のまま追加するなら %s -- %s か %s %q と書いてください", name, text, name, text)
	}
	return fmt.Errorf("%s", b.String())
}
