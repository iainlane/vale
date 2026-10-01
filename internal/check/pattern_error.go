package check

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	rx "github.com/vale-cli/vale/v3/internal/regex"
)

// patternError reports a rule whose pattern didn't compile.
//
// Vale wraps a rule's tokens in a larger pattern before compiling it, and the
// engine's message quotes that whole pattern -- `(?m)\b(?:(unclosed)\b` for a
// token written `(unclosed`. So each of the rule's own patterns is compiled
// alone, and the error points at the first that fails, with the engine's
// message about it. When none fails alone, the fault is in how they combine,
// and the original error is reported as it was.
var leadingFlags = regexp.MustCompile(`^\(\?[a-zA-Z-]+\)`)

func patternError(err error, path string, patterns ...string) error {
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, pErr := rx.Compile(p); pErr != nil {
			// A flag group Vale added, `(?-i)` on an exception, isn't in
			// the file, so the pattern is found and shown without it.
			written := leadingFlags.ReplaceAllString(p, "")
			msg := "Invalid pattern: " + strings.TrimPrefix(pErr.Error(), "error parsing regexp: ")
			msg = strings.Replace(msg, "`"+p+"`", "`"+written+"`", 1)
			placed := core.NewE201FromTarget(msg, written, path)
			var ve *core.Error
			if errors.As(placed, &ve) && ve.Line == 0 {
				// Written differently in the file -- escaped, or quoted --
				// so the line can't be found by its text.
				return core.NewE201FromPosition(fmt.Sprintf("%s (in '%s')", msg, written), path, 1)
			}
			return placed
		}
	}
	return core.NewE201FromPosition(err.Error(), path, 1)
}
