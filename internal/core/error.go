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

type lineError struct {
	content string
	line    int
	span    []int
}

type errorCondition func(position int, line, target string) bool

// gutter is a line number in an error's excerpt, colored through pterm so
// `--no-color` reaches it.
func gutter(n int) string {
	return pterm.NewStyle(pterm.FgGreen, pterm.Bold).Sprintf("%4d", n)
}

func annotate(file []byte, target string, finder errorCondition) (lineError, error) {
	var sb strings.Builder

	scanner := bufio.NewScanner(bytes.NewBuffer(file))
	context := lineError{span: []int{1, 1}}

	idx := 1
	for scanner.Scan() {
		markup := scanner.Text()
		plain := StripANSI(markup)
		if idx-context.line > 2 && context.line != 0 { //nolint:gocritic
			break
		} else if finder(idx, plain, target) && context.line == 0 {
			context.line = idx

			s := strings.Index(plain, target) + 1
			context.span = []int{s, s + len(target)}

			fmt.Fprintf(&sb, "%s* %s\n", gutter(idx), markup)
		} else {
			fmt.Fprintf(&sb, "%s  %s\n", gutter(idx), markup)
		}
		idx++
	}

	if err := scanner.Err(); err != nil {
		return lineError{}, err
	} else if context.line == 0 {
		// The value wasn't found, so there is no line to show; printing the
		// whole file around nothing helps no one.
		return context, nil
	}

	lines := []string{}
	for i, l := range strings.Split(sb.String(), "\n") {
		if context.line-i < 3 {
			lines = append(lines, l)
		}
	}

	context.content = strings.Join(lines, "\n")
	return context, nil
}

// NewError creates a colored error from the given information.
//
// # The standard format is
//
// ```
// <code> [<context>] <title>
//
// <msg>
// ```
func NewError(code, title, msg string) error {
	return fmt.Errorf(
		"%s %s\n\n%s\n\n%s",
		pterm.BgRed.Sprint(code),
		title,
		msg,
		pterm.Fuzzy.Sprint(pterm.Italic.Sprintf("Execution stopped with code 2.")),
	)
}

// Warn reports something the user should know about but that doesn't stop the
// run -- a key we don't recognize, say.
//
// It goes to stderr to stay clear of `--output=JSON` and the other formats a
// caller parses, which are written to stdout.
func Warn(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", pterm.BgYellow.Sprint("W101"), msg)
}

// NewE100 creates a new, formatted "unexpected" error.
//
// Since E100 errors can occur anywhere, we include a "context" that makes it
// clear where exactly the error was generated.
func NewE100(context string, err error) error {
	title := fmt.Sprintf("[%s] %s", context, "Runtime error")
	return NewError("E100", title, err.Error())
}

// NewE201 creates a formatted user-generated error.
//
// 201 errors involve a specific configuration asset and should contain
// parsable location information on their last line of the form:
//
// <path>:<line>:<start>:<end>
func NewE201(msg, value, path string, finder errorCondition) error {
	f, err := os.ReadFile(path)
	if err != nil {
		return NewE100("NewE201", errors.New(msg))
	}

	ctx, err := annotate(f, value, finder)
	if err != nil {
		return NewE100("NewE201/annotate", err)
	}

	title := fmt.Sprintf(
		"Invalid value [%s:%d:%d]:",
		filepath.ToSlash(path),
		ctx.line,
		ctx.span[0])

	body := msg
	if ctx.content != "" {
		body = fmt.Sprintf("%s\n%s", ctx.content, msg)
	}
	return NewError("E201", title, body)
}

// NewE201FromTarget creates a new E201 error from a target string.
func NewE201FromTarget(msg, value, file string) error {
	return NewE201(
		msg,
		value,
		file,
		func(_ int, line, target string) bool {
			return strings.Contains(line, target)
		})
}

// NewE201FromPosition creates a new E201 error from an in-file location.
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
		if ctx, aErr := annotate(f, "", names); aErr == nil && ctx.line != 0 {
			return NewE201(msg, hit, path, names)
		}
	}
	return NewE100(key, errors.New(msg))
}
