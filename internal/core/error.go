package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
)

// An Error is a failure of Vale itself rather than a finding in a document:
// E100 when something went wrong at runtime, E201 when a configuration file
// or rule holds an invalid value.
//
// It keeps its parts apart and is formatted only when shown, so the JSON and
// line outputs read the fields instead of parsing a rendered message back.
type Error struct {
	Code string // E100 or E201

	// Context says where an E100 came from: a command, or the file being
	// linted. An E201 has a position instead.
	Context string

	Path string // the file holding the invalid value
	Line int    // 1-based; 0 when the value couldn't be placed
	Span int    // 1-based column of the value

	// Excerpt is the lines around Line, shown above the message.
	Excerpt []ExcerptLine

	Msg string
}

// An ExcerptLine is one line of an Error's excerpt.
type ExcerptLine struct {
	Number int
	Text   string
	Marked bool // the line the error is about
}

// Error renders the banner shown on a terminal:
//
//	<code> <title>
//
//	<excerpt>
//	<msg>
//
//	Execution stopped with code 2.
func (e *Error) Error() string {
	title := fmt.Sprintf("[%s] Runtime error", e.Context)
	if e.Code != "E100" {
		title = fmt.Sprintf("Invalid value [%s:%d:%d]:", filepath.ToSlash(e.Path), e.Line, e.Span)
	}

	body := e.Msg
	if len(e.Excerpt) > 0 {
		var sb strings.Builder
		for _, l := range e.Excerpt {
			mark := " "
			if l.Marked {
				mark = "*"
			}
			fmt.Fprintf(&sb, "%s%s %s\n", gutter(l.Number), mark, l.Text)
		}
		body = sb.String() + "\n" + e.Msg
	}

	return fmt.Sprintf("%s %s\n\n%s\n\n%s",
		pterm.BgRed.Sprint(e.Code),
		title,
		body,
		pterm.Fuzzy.Sprint(pterm.Italic.Sprintf("Execution stopped with code 2.")))
}

// gutter is a line number in an error's excerpt, colored through pterm so
// `--no-color` reaches it.
func gutter(n int) string {
	return pterm.NewStyle(pterm.FgGreen, pterm.Bold).Sprintf("%4d", n)
}

type errorCondition func(position int, line, target string) bool

// placed is where annotate found a value, and the lines around it.
type placed struct {
	line, span int
	excerpt    []ExcerptLine
}

// annotate finds the first line finder accepts and returns it with up to two
// lines either side. A value that isn't found has no line and no excerpt.
func annotate(file []byte, target string, finder errorCondition) (placed, error) {
	var all []ExcerptLine
	at := placed{span: 1}

	scanner := bufio.NewScanner(bytes.NewBuffer(file))
	for idx := 1; scanner.Scan(); idx++ {
		if at.line != 0 && idx-at.line > 2 {
			break
		}
		text := StripANSI(scanner.Text())
		marked := at.line == 0 && finder(idx, text, target)
		if marked {
			at.line = idx
			if target != "" {
				at.span = strings.Index(text, target) + 1
			}
		}
		all = append(all, ExcerptLine{Number: idx, Text: text, Marked: marked})
	}
	if err := scanner.Err(); err != nil {
		return placed{}, err
	} else if at.line == 0 {
		return at, nil
	}

	for _, l := range all {
		if at.line-l.Number < 3 {
			at.excerpt = append(at.excerpt, l)
		}
	}
	return at, nil
}

// Warn reports something the user should know about but that doesn't stop the
// run -- a key we don't recognize, say.
//
// It goes to stderr to stay clear of `--output=JSON` and the other formats a
// caller parses, which are written to stdout.
func Warn(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", pterm.BgYellow.Sprint("W101"), msg)
}

// NewE100 creates an "unexpected" error, with a context that says where it
// came from. An error that is already Vale's own passes through unchanged,
// so it isn't shown as a banner inside another.
func NewE100(context string, err error) error {
	var ve *Error
	if errors.As(err, &ve) {
		return err
	}
	return &Error{Code: "E100", Context: context, Msg: err.Error()}
}

// NewE201 creates an error about an invalid value in the file at path,
// placed at the first line finder accepts.
func NewE201(msg, value, path string, finder errorCondition) error {
	f, err := os.ReadFile(path)
	if err != nil {
		return NewE100(filepath.Base(path), errors.New(msg))
	}

	at, err := annotate(f, value, finder)
	if err != nil {
		return NewE100(filepath.Base(path), err)
	}

	return &Error{
		Code: "E201", Path: path, Line: at.line, Span: at.span,
		Excerpt: at.excerpt, Msg: msg,
	}
}

// NewE201FromTarget creates an E201 at the first line containing value.
func NewE201FromTarget(msg, value, file string) error {
	return NewE201(
		msg,
		value,
		file,
		func(_ int, line, target string) bool {
			return strings.Contains(line, target)
		})
}

// NewE201FromPosition creates an E201 at a given line.
func NewE201FromPosition(msg, file string, goal int) error {
	return NewE201(
		msg,
		"",
		file,
		func(position int, _, _ string) bool {
			return position == goal
		})
}

// NewE201FromKey creates an E201 at the line of a configuration file that
// sets key to a value match accepts, searched across the files the run
// loaded. It falls back to an E100 when none of them does.
func NewE201FromKey(cfg *Config, key string, match func(value string) bool, msg string) error {
	// hit is the value that matched, so the column points at it.
	var hit string
	names := func(_ int, line, _ string) bool {
		k, v, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(k) != key {
			return false
		}
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); match(item) {
				hit = item
				return true
			}
		}
		return false
	}
	for _, path := range cfg.ConfigFiles {
		f, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if at, aErr := annotate(f, "", names); aErr == nil && at.line != 0 {
			return NewE201(msg, hit, path, names)
		}
	}
	return NewE100(key, errors.New(msg))
}
