// Package legacy reads the two files this list replaces: the plain-text ledger
// of non-ticket work, and the YAML file that held the order.
//
// The order file is the one that matters most. Its inline comments carry the
// reasoning behind each position, so they are kept as the task's detail rather
// than dropped on the floor.
package legacy

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/rengotaku/todo-order/internal/task"
)

// LedgerRow is one line of the old non-ticket ledger.
type LedgerRow struct {
	ID    string
	Name  string
	Due   string
	Ref   string
	Done  bool
	Order int // position in the file, for a stable fallback ordering
}

// ledgerLine matches: - [x] T002 | 2026-09-07 | title | due:2026-09-07 | done:2026-09-08
var ledgerLine = regexp.MustCompile(`^-\s*\[( |x|X)\]\s*(T\d+)\s*\|([^|]*)\|(.*)$`)

// ReadLedger parses the non-ticket ledger. A line that looks like an entry but
// does not parse is an error rather than a skip: silently dropping a row would
// lose work without saying so.
func ReadLedger(path string) ([]LedgerRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var rows []LedgerRow
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") {
			continue
		}
		if !strings.HasPrefix(line, "- [") {
			continue
		}

		m := ledgerLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%s line %d does not parse as a ledger entry: %q", path, lineNo, line)
		}

		row := LedgerRow{
			ID:    m[2],
			Done:  strings.EqualFold(strings.TrimSpace(m[1]), "x"),
			Order: len(rows),
		}

		// The tail is "title | key:value | key:value ...".
		parts := strings.Split(m[4], "|")
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			switch {
			case i == 0:
				row.Name = p
			case strings.HasPrefix(p, "due:"):
				row.Due = strings.TrimPrefix(p, "due:")
			case strings.HasPrefix(p, "ref:"):
				row.Ref = strings.TrimPrefix(p, "ref:")
			}
			// A "done:" date is deliberately not carried over: the new list
			// keeps its own completion times, and back-dating them would put
			// finished work into the wrong week.
		}
		if row.Name == "" {
			return nil, fmt.Errorf("%s line %d has no title: %q", path, lineNo, line)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s holds no entries", path)
	}
	return rows, nil
}

// OrderRow is one entry of the old order file, with the notes that sat around
// it in the file.
type OrderRow struct {
	Key     string
	Inline  string   // the comment on the same line
	Context []string // comment lines directly above the entry
}

var orderEntry = regexp.MustCompile(`^-\s+(\S+)\s*(?:#\s*(.*))?$`)

// ReadOrder parses the order file, keeping each entry's comments. Only the
// entries under the "order:" key are read.
func ReadOrder(path string) ([]OrderRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var (
		rows    []OrderRow
		context []string
		started bool
	)
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)

		if !started {
			// Comments above "order:" describe the file, not any one entry.
			if trimmed == "order:" {
				started = true
			}
			continue
		}

		switch {
		case trimmed == "":
			context = nil // a blank line ends a block of notes
		case strings.HasPrefix(trimmed, "#"):
			context = append(context, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
		case strings.HasPrefix(trimmed, "- "):
			m := orderEntry.FindStringSubmatch(trimmed)
			if m == nil {
				return nil, fmt.Errorf("%s line %d does not parse as an order entry: %q", path, lineNo, trimmed)
			}
			rows = append(rows, OrderRow{
				Key:     m[1],
				Inline:  strings.TrimSpace(m[2]),
				Context: context,
			})
			context = nil
		default:
			return nil, fmt.Errorf("%s line %d is neither a comment nor an entry: %q", path, lineNo, trimmed)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if !started {
		return nil, fmt.Errorf("%s has no \"order:\" key", path)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s holds no order entries", path)
	}
	return rows, nil
}

// Plan turns the two files into the rows to import, in the order they should
// sit in the queue.
//
// The order file leads: whatever it lists comes first, in its order. Ledger
// rows it does not mention follow, completed ones included, so nothing is
// lost. An order entry that names a ledger row keeps that row's ID and due
// date; anything else (a ticket key) becomes a new task whose name is the key
// plus the note that sat beside it.
func Plan(ledger []LedgerRow, order []OrderRow) ([]task.ImportItem, error) {
	byID := make(map[string]LedgerRow, len(ledger))
	for _, r := range ledger {
		if _, dup := byID[r.ID]; dup {
			return nil, fmt.Errorf("ledger holds %s twice", r.ID)
		}
		byID[r.ID] = r
	}

	items := make([]task.ImportItem, 0, len(order)+len(ledger))
	used := make(map[string]bool, len(order))

	for _, o := range order {
		if used[o.Key] {
			return nil, fmt.Errorf("order file lists %s twice", o.Key)
		}
		used[o.Key] = true

		if row, ok := byID[o.Key]; ok {
			items = append(items, task.ImportItem{
				ID:     row.ID,
				Name:   row.Name,
				Detail: detailOf(o, row.Ref),
				Due:    row.Due,
				Done:   row.Done,
			})
			continue
		}

		name := o.Key
		if o.Inline != "" {
			name = o.Key + " " + headline(o.Inline)
		}
		items = append(items, task.ImportItem{
			Name:   name,
			Detail: detailOf(o, ""),
		})
	}

	for _, r := range ledger {
		if used[r.ID] {
			continue
		}
		items = append(items, task.ImportItem{
			ID:     r.ID,
			Name:   r.Name,
			Detail: detailOf(OrderRow{}, r.Ref),
			Due:    r.Due,
			Done:   r.Done,
		})
	}
	return items, nil
}

// detailOf keeps the notes that explained a position, so the reasoning behind
// the old order survives the move.
func detailOf(o OrderRow, ref string) string {
	var b strings.Builder
	if o.Inline != "" {
		b.WriteString(o.Inline)
	}
	if len(o.Context) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.Join(o.Context, "\n"))
	}
	if ref != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("参照: " + ref)
	}
	return b.String()
}

// headline takes the first clause of a note for the task name and leaves the
// rest in the detail, so a name stays one line.
func headline(s string) string {
	for _, cut := range []string{"。", "（", "(", "、"} {
		if i := strings.Index(s, cut); i > 0 {
			s = s[:i]
		}
	}
	const max = 40
	r := []rune(strings.TrimSpace(s))
	if len(r) > max {
		return string(r[:max])
	}
	return string(r)
}
