package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
)

func TestRunsNothing(t *testing.T) {
	cases := []struct {
		desc    string
		base    []string
		checks  map[string]bool
		unset   map[string]bool
		global  map[string]bool
		nothing bool
	}{
		{desc: "a style", base: []string{"Vale"}},
		{desc: "a rule on in a section", checks: map[string]bool{"T.W": true}},
		{desc: "a rule on globally", global: map[string]bool{"T.W": true}},
		{desc: "nothing at all", nothing: true},
		{
			desc:    "a global rule the section switched off",
			checks:  map[string]bool{"T.W": false},
			global:  map[string]bool{"T.W": true},
			nothing: true,
		},
		{
			desc:    "a global rule the section unset",
			unset:   map[string]bool{"T.W": true},
			global:  map[string]bool{"T.W": true},
			nothing: true,
		},
	}
	for _, c := range cases {
		cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
		if err != nil {
			t.Fatal(err)
		}
		if c.global != nil {
			cfg.GChecks = c.global
		}
		l, lErr := NewLinter(cfg)
		if lErr != nil {
			t.Fatal(lErr)
		}

		f := &core.File{BaseStyles: c.base, Checks: c.checks, Unset: c.unset}
		if f.Checks == nil {
			f.Checks = map[string]bool{}
		}
		if got := l.runsNothing(f); got != c.nothing {
			t.Errorf("%s: runsNothing = %v, want %v", c.desc, got, c.nothing)
		}
	}
}

// TestSkippedFileIsNotParsed checks that a file no rule runs on is never
// parsed: parsing is what counts its metrics.
func TestSkippedFileIsNotParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(path, []byte("# Title\n\nA paragraph.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, skipped := range []bool{false, true} {
		cfg, err := core.NewConfig(&core.CLIFlags{IgnoreGlobal: true})
		if err != nil {
			t.Fatal(err)
		}
		cfg.GBaseStyles = []string{"Vale"}
		cfg.Flags.InExt = ".txt" // as ls-metrics sets it
		if skipped {
			pat, gErr := glob.Compile("*.md")
			if gErr != nil {
				t.Fatal(gErr)
			}
			cfg.SecToPat["*.md"] = pat
			cfg.RuleKeys = []string{"*.md"}
			cfg.SBaseStyles["*.md"] = []string{}
		}

		linter, err := NewLinter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		files, err := linter.Lint([]string{path}, "*")
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("linted %d files, want 1", len(files))
		}
		if parsed := len(files[0].Metrics) > 0; parsed == skipped {
			t.Errorf("skipped = %v, but parsed = %v", skipped, parsed)
		}
	}
}
