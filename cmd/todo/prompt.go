package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rengotaku/todo-order/internal/duedate"
)

// addInput is what the interactive prompt collects for one new task.
type addInput struct {
	Name   string
	Due    string
	Detail string
}

// isTerminal reports whether f is attached to a terminal. Only then does a
// bare `todo` ask questions; from a pipe or a script it keeps printing help so
// a non-interactive caller is never left waiting on stdin.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// promptAdd asks for the name (required), the due date and the detail in
// turn, asking again until the name is non-empty and the due date parses.
func promptAdd(in io.Reader, out io.Writer, today time.Time) (addInput, error) {
	r := bufio.NewReader(in)
	var got addInput

	for got.Name == "" {
		line, eof, err := ask(r, out, "タスク名: ")
		if err != nil {
			return addInput{}, err
		}
		got.Name = line
		if eof && got.Name == "" {
			return addInput{}, errors.New("中断しました（タスク名が未入力）")
		}
	}

	for {
		line, eof, err := ask(r, out, "期限 ("+duedate.Hint+"): ")
		if err != nil {
			return addInput{}, err
		}
		due, perr := duedate.Parse(line, today)
		if perr == nil {
			got.Due = due
			if due != "" {
				_, _ = fmt.Fprintf(out, "  → %s\n", due)
			}
			break
		}
		_, _ = fmt.Fprintln(out, "  "+perr.Error())
		if eof {
			return addInput{}, perr
		}
	}

	line, _, err := ask(r, out, "詳細・URL (空で無し): ")
	if err != nil {
		return addInput{}, err
	}
	got.Detail = line
	return got, nil
}

// ask prints prompt and reads one line. eof is true when input ended; a
// final line without a trailing newline is still returned.
func ask(r *bufio.Reader, out io.Writer, prompt string) (line string, eof bool, err error) {
	_, _ = fmt.Fprint(out, prompt)
	s, err := r.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return strings.TrimSpace(s), true, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(s), false, nil
}
