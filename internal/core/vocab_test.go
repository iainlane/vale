package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readVocab(t *testing.T, src string) (*Vocabulary, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vocab.yml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	vocab := &Vocabulary{}
	return vocab, readVocabYAML(path, vocab, nil)
}

func TestVocabYAMLEntries(t *testing.T) {
	vocab, err := readVocab(t, `
accept:
  - Node.js
  - C++
  - CI/CD
  - LaTeX:
  - failover: {flags: S}
  - config: {ignorecase: true}
  - '[Pp]ython': {regex: true}
reject:
  - k8s
  - master:
      ignorecase: true
`)
	if err != nil {
		t.Fatal(err)
	}

	accepted := []string{"Node.js", `C\+\+`, "CI/CD", "LaTeX", "failover", "failovers", "(?i)config", "[Pp]ython"}
	if !reflect.DeepEqual(vocab.Accepted, accepted) {
		t.Errorf("Accepted = %q, want %q", vocab.Accepted, accepted)
	}
	if want := []string{"k8s", "(?i)master"}; !reflect.DeepEqual(vocab.Rejected, want) {
		t.Errorf("Rejected = %q, want %q", vocab.Rejected, want)
	}
}

func TestVocabYAMLErrors(t *testing.T) {
	cases := map[string]string{
		"reject:\n  - whitelist: allowlist\n":                   "use a substitution rule",
		"accept:\n  - a: {regex: true, flags: S}\n":             "both 'regex' and 'flags'",
		"accept:\n  - '(open': {regex: true}\n":                 "isn't a valid pattern",
		"accept:\n  - quxelate: {flags: S9}\n":                  "isn't an affix flag",
		"accept:\n  - a: {case: lower}\n":                       "isn't a vocabulary option",
		"accept:\n  - a: {regex: true}\n    b: {regex: true}\n": "a term mapped to its options",
		"accept:\n  - a: [x]\n":                                 "should map to its options",
		"affix: nope\naccept:\n  - a: {flags: S}\n":             "'nope.aff' not found",
		"accept: [a\n": "did not find expected",
	}
	for src, want := range cases {
		_, err := readVocab(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("readVocabYAML(%q) = %v, want an error containing %q", src, err, want)
		}
	}
}
