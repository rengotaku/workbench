package task_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rengotaku/workbench/internal/task"
)

// backdateHandoff rewrites tasks.json so one waiting task looks like it was
// handed off days ago. Tests need an aged handoff to exercise the stall
// threshold, and waiting for real time to pass is not an option.
func backdateHandoff(dir, id string, days int) error {
	path := filepath.Join(dir, "tasks.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var st struct {
		Tasks     []task.Task `json:"tasks"`
		NextTask  int         `json:"next_task"`
		NextEvent int         `json:"next_event"`
	}
	if uerr := json.Unmarshal(b, &st); uerr != nil {
		return fmt.Errorf("parsing %s: %w", path, uerr)
	}

	found := false
	for i := range st.Tasks {
		if st.Tasks[i].ID != id {
			continue
		}
		st.Tasks[i].HandedOn = time.Now().AddDate(0, 0, -days).Format(task.DateLayout)
		found = true
	}
	if !found {
		return fmt.Errorf("task %s not present in %s", id, path)
	}

	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
