package spell

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestExpand(t *testing.T) {
	cases := []struct {
		word, flags string
		want        []string
	}{
		{"failover", "S", []string{"failover", "failovers"}},
		{"rebase", "DG", []string{"rebase", "rebased", "rebasing"}},
		{"Grafana", "M", []string{"Grafana", "Grafana's"}},
		{"CI/CD", "M", []string{"CI/CD", "CI/CD's"}},
		{"kubeadm", "", []string{"kubeadm"}},
	}
	for _, c := range cases {
		got, err := Expand(c.word, c.flags)
		if err != nil {
			t.Fatalf("Expand(%q, %q): %v", c.word, c.flags, err)
		}
		sort.Strings(got)
		sort.Strings(c.want)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Expand(%q, %q) = %v, want %v", c.word, c.flags, got, c.want)
		}
	}
}

func TestExpandRejectsBadEntries(t *testing.T) {
	for _, c := range [][2]string{{"quxelate", "S9"}, {"", "S"}} {
		if _, err := Expand(c[0], c[1]); err == nil {
			t.Errorf("Expand(%q, %q) succeeded, want an error", c[0], c[1])
		}
	}
}

// A project's own `.aff` is read in the encoding its SET names.
func TestExpandWith(t *testing.T) {
	path := filepath.Join(t.TempDir(), "de_mini.aff")
	aff := "SET ISO8859-1\nSFX N Y 1\nSFX N 0 n e\nSFX U Y 1\nSFX U 0 \xfcr .\n"
	if err := os.WriteFile(path, []byte(aff), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ExpandWith(path, "Schnittstelle", "NU")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if want := []string{"Schnittstelle", "Schnittstellen", "Schnittstelleür"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandWith = %v, want %v", got, want)
	}

	// S is a flag in the default file, not in this one.
	if _, err = ExpandWith(path, "Schnittstelle", "S"); err == nil {
		t.Error("ExpandWith accepted a flag the file doesn't define")
	}
}
