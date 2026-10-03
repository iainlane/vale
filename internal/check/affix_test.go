package check

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/spell"
)

func TestAffixToken(t *testing.T) {
	cases := map[string]string{
		"failover/S":  `(?:failover|failovers)`,
		`CI\/CD/M`:    `(?:CI/CD|CI/CD's)`,
		`and\/or`:     `and\/or`,
		"plain":       "plain",
		`\bplain\b`:   `\bplain\b`,
		"Grafana/M":   `(?:Grafana|Grafana's)`,
		"rebase/DG":   `(?:rebase|rebased|rebasing)`,
		"walkthrough": "walkthrough",

		// Each flagged word of a phrase expands; the rest is left as written.
		"jump/DGS the gun":           `(?:jump|jumped|jumping|jumps) the gun`,
		`\bclick/DG (on|the|this)\b`: `\b(?:click|clicked|clicking) (on|the|this)\b`,
		"tight deadline/S":           `tight (?:deadline|deadlines)`,
		`\bdogfood/DGS\b`:            `\b(?:dogfood|dogfooded|dogfooding|dogfoods)\b`,
	}
	for token, want := range cases {
		got, err := affixToken(spell.Expand, token)
		if err != nil {
			t.Fatalf("affixToken(%q): %v", token, err)
		}
		// The order of forms is the expander's; compare as sets.
		if strings.Contains(want, "(?:") {
			if !sameForms(got, want) {
				t.Errorf("affixToken(%q) = %q, want %q", token, got, want)
			}
		} else if got != want {
			t.Errorf("affixToken(%q) = %q, want %q", token, got, want)
		}
	}
}

func TestAffixTokenErrors(t *testing.T) {
	for token, want := range map[string]string{
		"and/or":        "isn't an affix flag",
		"[Ll]everage/S": "must be plain text",
	} {
		_, err := affixToken(spell.Expand, token)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("affixToken(%q) = %v, want an error containing %q", token, err, want)
		}
	}
}

// sameForms compares two expansions, ignoring the order of the forms inside
// each `(?:...)` group.
func sameForms(a, b string) bool {
	return normalizeForms(a) == normalizeForms(b)
}

var formGroup = regexp.MustCompile(`\(\?:([^()]*)\)`)

func normalizeForms(s string) string {
	return formGroup.ReplaceAllStringFunc(s, func(g string) string {
		forms := strings.Split(g[3:len(g)-1], "|")
		sort.Strings(forms)
		return "(?:" + strings.Join(forms, "|") + ")"
	})
}
