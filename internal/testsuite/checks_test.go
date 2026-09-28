package testsuite

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every check type, each a rule under testdata/testsuite carrying a case that
// fires and one that stays quiet. The runner is asked for all of them at
// once, with coverage, so a check that stopped firing in isolation -- one
// that came to need a project's configuration, a tagger, or a dictionary the
// isolated linter no longer provides -- fails here rather than in a user's
// suite.
func TestEveryCheckRunsInIsolation(t *testing.T) {
	styles, err := filepath.Abs(filepath.Join("..", "..", "testdata", "testsuite", "styles"))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"capitalization", "conditional", "consistency", "existence", "metric",
		"occurrence", "readability", "repetition", "script", "sequence",
		"spelling", "substitution",
	}
	files, err := filepath.Glob(filepath.Join(styles, "Ext", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		b, rErr := os.ReadFile(f)
		if rErr != nil {
			t.Fatal(rErr)
		}
		var rule struct {
			Extends string `yaml:"extends"`
		}
		if uErr := yaml.Unmarshal(b, &rule); uErr != nil {
			t.Fatal(uErr)
		}
		got = append(got, rule.Extends)
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Fatalf("fixture covers %v; want one rule per check: %v", got, want)
	}

	runner := NewRunner(nil)
	runner.paths, runner.pathsSet = []string{styles}, true

	var results []Result
	for _, f := range files {
		cases, lErr := Load(f)
		if lErr != nil {
			t.Fatal(lErr)
		}
		if len(cases) < 2 {
			t.Errorf("%s: want a firing case and a clean one, got %d", filepath.Base(f), len(cases))
		}
		for _, c := range cases {
			r := runner.Run(c)
			if r.Failed() {
				t.Errorf("%s: %q: %s%v\n%s", filepath.Base(f), c.Name, r.Reason, r.Err, r.Got)
			}
			results = append(results, r)
		}
	}

	rules, err := runner.Rules([]string{styles})
	if err != nil {
		t.Fatal(err)
	}
	if uncovered := Uncovered(rules, results); len(uncovered) > 0 {
		t.Errorf("no case fired for %s", strings.Join(uncovered, ", "))
	}
}
