package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ergochat/readline"

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
	return promptWith(&plainReader{r: bufio.NewReader(in), out: out}, out, today)
}

// promptWith runs the questions over any lineReader. The terminal uses a line
// editor so that Backspace erases a whole full-width character; tests and
// non-terminal input use plainReader.
func promptWith(r lineReader, out io.Writer, today time.Time) (addInput, error) {
	var got addInput

	for got.Name == "" {
		line, eof, err := ask(r, "タスク名: ")
		if err != nil {
			return addInput{}, err
		}
		got.Name = line
		if eof && got.Name == "" {
			return addInput{}, errors.New("中断しました（タスク名が未入力）")
		}
	}

	for {
		line, eof, err := ask(r, "期限 ("+duedate.Hint+"): ")
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

	line, _, err := ask(r, "詳細・URL (空で無し): ")
	if err != nil {
		return addInput{}, err
	}
	got.Detail = line
	return got, nil
}

// lineReader shows prompt and returns one line without the newline. eof is
// true when input ended; a final line without a trailing newline is still
// returned.
type lineReader interface {
	ReadLine(prompt string) (line string, eof bool, err error)
}

func ask(r lineReader, prompt string) (string, bool, error) {
	line, eof, err := r.ReadLine(prompt)
	return strings.TrimSpace(line), eof, err
}

// plainReader reads lines as-is, leaving echo and erase to whatever is on the
// other side. Used by tests.
type plainReader struct {
	r   *bufio.Reader
	out io.Writer
}

func (p *plainReader) ReadLine(prompt string) (string, bool, error) {
	_, _ = fmt.Fprint(p.out, prompt)
	s, err := p.r.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return s, true, nil
	}
	if err != nil {
		return "", false, err
	}
	return s, false, nil
}

// termReader edits the line itself instead of relying on the kernel's
// canonical mode, which erases one column per Backspace and so leaves half of
// a full-width character on screen.
type termReader struct {
	rl *readline.Instance
}

func newTermReader() (*termReader, error) {
	rl, err := readline.NewFromConfig(&readline.Config{DisableAutoSaveHistory: true})
	if err != nil {
		return nil, err
	}
	return &termReader{rl: rl}, nil
}

func (t *termReader) ReadLine(prompt string) (string, bool, error) {
	t.rl.SetPrompt(prompt)
	s, err := t.rl.Readline()
	switch {
	case errors.Is(err, io.EOF):
		return s, true, nil
	case errors.Is(err, readline.ErrInterrupt):
		return "", false, errors.New("中断しました")
	case err != nil:
		return "", false, err
	}
	return s, false, nil
}

func (t *termReader) Close() error { return t.rl.Close() }
