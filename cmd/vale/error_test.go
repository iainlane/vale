package main

import (
	"bytes"
	"errors"
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
