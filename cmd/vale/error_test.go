package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// TestShowErrorRuntime formats an E100, which has no file position, for the
// formats a caller parses: one line, and JSON holding only the message.
func TestShowErrorRuntime(t *testing.T) {
	err := core.NewE100("ls-metrics", errors.New("one argument expected"))

	var line bytes.Buffer
	ShowError(err, "line", &line)
	if got := strings.TrimSpace(line.String()); got != "[ls-metrics]:E100:one argument expected" {
		t.Errorf("line = %q", got)
	}

	var js bytes.Buffer
	ShowError(err, "JSON", &js)
	if got := js.String(); !strings.Contains(got, `"Text": "one argument expected"`) ||
		!strings.Contains(got, `"Code": "E100"`) {
		t.Errorf("JSON = %s", got)
	}
}

// TestShowErrorTyped reads the fields of an error instead of parsing its
// banner: a message with a blank line in it reaches JSON whole, and an E201
// wrapped in an E100 is still the E201.
func TestShowErrorTyped(t *testing.T) {
	ini := filepath.Join(t.TempDir(), ".vale.ini")
	if err := os.WriteFile(ini, []byte("StylesPath = styles\n[*.md]\nBasedOnStyles = Nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e201 := core.NewE201FromTarget("first paragraph\n\nsecond paragraph", "Nope", ini)
	err := core.NewE100("stdin", e201)

	var js bytes.Buffer
	ShowError(err, "JSON", &js)
	for _, want := range []string{`"Code": "E201"`, `"Line": 3`, `"Span": 17`,
		`"Text": "first paragraph\n\nsecond paragraph"`} {
		if !strings.Contains(js.String(), want) {
			t.Errorf("JSON is missing %s:\n%s", want, js.String())
		}
	}

	var line bytes.Buffer
	ShowError(err, "line", &line)
	if got := strings.TrimSpace(line.String()); got != filepath.ToSlash(ini)+":3:E201:first paragraph" {
		t.Errorf("line = %q", got)
	}
}
