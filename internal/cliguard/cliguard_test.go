package cliguard

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run parses args the way the todo root does and returns what the guard said.
func run(t *testing.T, args ...string) (error, bool) {
	t.Helper()
	var guard error
	added := false
	root := &cobra.Command{
		Use:          "todo",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, a []string) error {
			if guard = UnknownSubcommand(cmd, a); guard != nil {
				return guard
			}
			added = true
			return nil
		},
	}
	root.SilenceErrors = true
	root.Flags().String("due", "", "")
	root.AddCommand(&cobra.Command{Use: "list", Run: func(*cobra.Command, []string) {}})
	root.SetArgs(args)
	_ = root.Execute()
	return guard, added
}

func TestUnknownSubcommand_StopsATypedWordThatIsNoCommand(t *testing.T) {
	err, added := run(t, "show", "T094")
	require.Error(t, err)
	assert.False(t, added)
	assert.Contains(t, err.Error(), `"show" というサブコマンドはありません`)
	assert.Contains(t, err.Error(), "todo -- show T094")
	assert.Contains(t, err.Error(), `todo "show T094"`)
}

func TestUnknownSubcommand_SuggestsTheNearestCommand(t *testing.T) {
	err, _ := run(t, "lst")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list のことですか")
	assert.Contains(t, err.Error(), "todo -- lst と書いて")
	assert.NotContains(t, err.Error(), `"lst" と書いて`, "quoting one word does not help, so it is not offered")
}

func TestUnknownSubcommand_LetsAddingThrough(t *testing.T) {
	cases := map[string][]string{
		"quoted as one argument": {"show T094"},
		"after --":               {"--", "show", "T094"},
		"flag then --":           {"--due", "2026-11-01", "--", "deploy"},
		"japanese words":         {"牛乳", "買う"},
		"url first":              {"https://x.slack.com/a/1", "変わった"},
		"capitalised word":       {"Terraform", "の更新"},
		"id first":               {"T094", "の続き"},
		"digit first":            {"3", "件まとめる"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			err, added := run(t, args...)
			assert.NoError(t, err)
			assert.True(t, added)
		})
	}
}

func TestUnknownSubcommand_NoArgs(t *testing.T) {
	assert.NoError(t, UnknownSubcommand(&cobra.Command{Use: "todo"}, nil))
}
