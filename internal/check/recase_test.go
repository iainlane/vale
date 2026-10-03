package check

import "testing"

func TestCertainRecase(t *testing.T) {
	cases := map[[2]string]bool{
		{"github", "GitHub"}:         true,
		{"Github", "GitHub"}:         true,
		{"json", "JSON"}:             true,
		{"cpus", "CPUs"}:             true,
		{"Javascript", "JavaScript"}: true,
		{"kubernetes", "Kubernetes"}: false, // capitalized only
		{"los", "Los"}:               false,
		{"ui", "UI"}:                 false, // too short to be sure
		{"CNs", "CNS"}:               false, // already chose its capitals
		{"PAM's", "Pam's"}:           false,
	}
	for c, want := range cases {
		if got := certainRecase(c[0], c[1]); got != want {
			t.Errorf("certainRecase(%q, %q) = %v, want %v", c[0], c[1], got, want)
		}
	}
}
