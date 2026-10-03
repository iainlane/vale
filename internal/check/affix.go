package check

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

// affixTokens expands each `word/FLAGS` word in the tokens of a rule that
// sets `affix` into an alternation of the forms its flags name. A slash
// escaped as `\/` is text.
func affixTokens(cfg *core.Config, affix string, tokens []string, path string) ([]string, error) {
	if affix == "" {
		return tokens, nil
	}
	path = core.RelToWD(path)

	expand, err := core.AffixExpander(cfg, affix)
	if err != nil {
		return nil, core.NewE201FromTarget(err.Error(), affix, path)
	}

	out := make([]string, len(tokens))
	for i, token := range tokens {
		out[i], err = affixToken(expand, token)
		if err != nil {
			return nil, core.NewE201FromTarget(err.Error(), token, path)
		}
	}
	return out, nil
}

// affixToken returns token with each space-separated `word/FLAGS` part
// replaced by an alternation of its forms. Other parts, and a token with no
// unescaped slash, are left as written.
func affixToken(expand core.ExpandFunc, token string) (string, error) {
	parts := strings.Split(token, " ")
	for i, part := range parts {
		cut := unescapedSlash(part)
		if cut < 0 {
			continue
		}

		// A word boundary on either side stays outside the alternation.
		var before, after string
		if strings.HasPrefix(part, `\b`) {
			before, part, cut = `\b`, part[2:], cut-2
		}
		if strings.HasSuffix(part, `\b`) {
			after, part = `\b`, part[:len(part)-2]
		}

		word := strings.ReplaceAll(part[:cut], `\/`, "/")
		if regexp.QuoteMeta(word) != word {
			return "", fmt.Errorf("'%s' has affix flags, so its word must be plain text; write a literal slash as '\\/'", part)
		}

		forms, err := expand(word, part[cut+1:])
		if err != nil {
			return "", fmt.Errorf("'%s': %w; write a literal slash as '\\/'", part, err)
		}
		for j, f := range forms {
			forms[j] = regexp.QuoteMeta(f)
		}
		parts[i] = before + "(?:" + strings.Join(forms, "|") + ")" + after
	}
	return strings.Join(parts, " "), nil
}

// unescapedSlash returns the index of the last `/` in s not preceded by a
// backslash, or -1.
func unescapedSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' && (i == 0 || s[i-1] != '\\') {
			return i
		}
	}
	return -1
}
