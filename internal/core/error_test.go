package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pterm/pterm"
)

// TestAnnotateNotFound shows no excerpt for a value that isn't in the file,
// rather than every line of it.
func TestAnnotateNotFound(t *testing.T) {
	ctx, err := annotate([]byte("a = 1\nb = 2\n"), "missing", func(_ int, line, target string) bool {
		return strings.Contains(line, target)
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.line != 0 || ctx.content != "" {
		t.Errorf("annotate = line %d, content %q; want no excerpt", ctx.line, ctx.content)
	}
}

// TestGutterHonorsNoColor keeps raw escape codes out of an error's excerpt
// under `--no-color`.
func TestGutterHonorsNoColor(t *testing.T) {
	pterm.DisableColor()
	defer pterm.EnableColor()

	if g := gutter(7); strings.Contains(g, "\x1b[") {
		t.Errorf("gutter(7) = %q; want no escape codes", g)
	}
}

func TestNewE201FromKey(t *testing.T) {
	dir := t.TempDir()
	ini := filepath.Join(dir, ".vale.ini")
	src := "StylesPath = styles\n\n# Nope is only mentioned here.\n[*.md]\nBasedOnStyles = Vale, Nope\n"
	if err := os.WriteFile(ini, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigFiles = []string{ini}

	pterm.DisableColor()
	defer pterm.EnableColor()

	got := NewE201FromKey(cfg, "BasedOnStyles", func(v string) bool { return v == "Nope" }, "msg").Error()
	if !strings.Contains(got, filepath.ToSlash(ini)+":5:23]") {
		t.Errorf("want the BasedOnStyles line and the value's column, got:\n%s", got)
	}

	missing := NewE201FromKey(cfg, "BasedOnStyles", func(v string) bool { return v == "Other" }, "msg").Error()
	if !strings.HasPrefix(missing, "E100 [BasedOnStyles]") {
		t.Errorf("want an E100 when no file sets the value, got:\n%s", missing)
	}
}
