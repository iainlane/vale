package check

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

func TestPatternError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Rule.yml")
	src := "extends: existence\nmessage: x\ntokens:\n  - fine\n  - '(unclosed'\nexceptions:\n  - 'a[b'\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	wrapped := errors.New("error parsing regexp: missing closing ) in `(?m)\\b(?:fine|(unclosed)\\b`")

	cases := []struct {
		desc     string
		patterns []string
		line     int
		msg      string
	}{
		{"the token that fails", []string{"fine", "(unclosed"}, 5, "Invalid pattern: missing closing ) in `(unclosed`"},
		{"a flag group Vale added", []string{"(?-i)a[b"}, 7, "Invalid pattern: unterminated [] set in `a[b`"},
		{"not in the file as written", []string{"(gone"}, 1, "(in '(gone')"},
		{"none fails alone", []string{"fine"}, 1, wrapped.Error()},
	}
	for _, c := range cases {
		var ve *core.Error
		if !errors.As(patternError(wrapped, path, c.patterns...), &ve) {
			t.Fatalf("%s: not a Vale error", c.desc)
		}
		if ve.Line != c.line || !strings.Contains(ve.Msg, c.msg) {
			t.Errorf("%s: line %d, %q; want line %d, %q", c.desc, ve.Line, ve.Msg, c.line, c.msg)
		}
	}
}
