package spell

import "testing"

func TestRecase(t *testing.T) {
	checker, err := NewChecker()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"github":     "GitHub",
		"Github":     "GitHub",
		"javascript": "JavaScript",
		"github's":   "GitHub's",
		"kubernetes": "Kubernetes",
		"xcode":      "Xcode",
	}
	for word, want := range cases {
		if got, ok := checker.Recase(word); !ok || got != want {
			t.Errorf("Recase(%q) = %q, %v; want %q", word, got, ok, want)
		}
	}
	// A misspelling, not a case difference; or two spellings to choose from.
	for _, word := range []string{"githbu", "javscript", "zorbblat", "https"} {
		if got, ok := checker.Recase(word); ok {
			t.Errorf("Recase(%q) = %q, want no recasing", word, got)
		}
	}
}
