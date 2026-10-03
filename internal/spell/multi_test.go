package spell

import (
	"os"
	"path/filepath"
	"testing"
)

// readAsset must keep searching the remaining roots when an earlier root
// neither contains the file nor is a symlink. A regression made it return the
// Readlink error from the first root, so $DICPATH (the `system` root) was
// never consulted -- see #1014.
func TestReadAssetFallsThroughToLaterRoots(t *testing.T) {
	empty := t.TempDir()   // stands in for StylesPath/config/dictionaries
	dicpath := t.TempDir() // stands in for $DICPATH

	want := filepath.Join(dicpath, "en_GB.dic")
	if err := os.WriteFile(want, []byte("1\nword\n"), 0600); err != nil {
		t.Fatal(err)
	}

	m := &Checker{options: Options{path: empty, system: dicpath}}

	got, err := m.readAsset("en_GB.dic")
	if err != nil {
		t.Fatalf("readAsset: %v", err)
	}
	if got != want {
		t.Errorf("readAsset = %q, want %q", got, want)
	}
}

// A symlinked asset (how Nix typically exposes dictionaries via $DICPATH)
// must be found in its root. os.Stat follows the link, so readAsset returns
// the link path itself, which os.Open then follows to the target.
func TestReadAssetFindsSymlinkedAsset(t *testing.T) {
	target := filepath.Join(t.TempDir(), "real.dic")
	if err := os.WriteFile(target, []byte("1\nword\n"), 0600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	link := filepath.Join(root, "en_GB.dic")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	m := &Checker{options: Options{system: root}}

	got, err := m.readAsset("en_GB.dic")
	if err != nil {
		t.Fatalf("readAsset: %v", err)
	}
	if got != link {
		t.Errorf("readAsset = %q, want %q", got, link)
	}
}

// The bundled dictionary uses no directive the reader ignores.
// A new directive appearing here means the reader has fallen behind the
// dictionary it ships with.
func TestDefaultDictionaryIgnoredDirectives(t *testing.T) {
	checker, err := NewChecker()
	if err != nil {
		t.Fatal(err)
	}

	if got := checker.Ignored(); len(got) != 0 {
		t.Errorf("Ignored() = %v, want none", got)
	}
}

// The bundled dictionary knows common technical vocabulary, including
// plurals and possessives of acronyms, but still rejects names in the wrong case.
func TestDefaultDictionaryTechnicalVocabulary(t *testing.T) {
	checker, err := NewChecker()
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"repo": true, "repos": true, "config's": true, "namespaces": true,
		"hostname": true, "deduplicated": true, "boolean": true, "RESTful": true,
		"APIs": true, "API's": true, "PRs": true, "SDKs": true, "VMs": true,
		"Grafana": true, "Xcode": true, "walkthroughs": true,

		"grafana": false, "XCode": false, "github": false, "Javascript": false,
		"yaml": false, "everytime": false, "walkthroughes": false,

		// A contraction missing its apostrophe is a typo, not the rare word.
		"cant": false, "wont": false, "can't": true, "won't": true,
		"decanted": true, "recant": true, "canted": true, "wonted": true,

		// Obscure entries one edit from a common word are gone; the common
		// words their flags used to produce are not.
		"pervious": false, "flor": false, "typw": false, "relict": false,
		"previous": true, "rather": true, "bitten": true, "stricken": true,
		"descend": true, "reduce": true, "derelict": true, "redux": true,
		"unbidden": true, "moduli": true,
	}
	for word, ok := range want {
		if got := checker.Spell(word); got != ok {
			t.Errorf("Spell(%q) = %v, want %v", word, got, ok)
		}
	}
}
