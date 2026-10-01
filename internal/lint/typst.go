package lint

import (
	"errors"
	"strings"
	"sync"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// Typst is its own grammar, so Vale reads it with its own parser: documents
// go to typst2vast, which parses them with typst-syntax -- the compiler's
// parser, error-tolerant and evaluation-free -- and emits the HTML the
// walker reads. Nothing is compiled or executed: a document that doesn't
// build still lints, no package is ever fetched, and no show rule can make
// the linted text differ from the source. See #651.

// typst2vast converts one document per process unless asked otherwise;
// `--batch` keeps the process up and takes documents over the same framing
// the other converter pools use. Support is probed rather than assumed, so
// an older install falls back to one process per file.
var (
	typstOnce   sync.Once
	typstDirect []string
)

func typstFastPath() []string {
	typstOnce.Do(func() {
		exe := system.Which([]string{"typst2vast"})
		if exe == "" {
			return
		}

		candidate := []string{exe, "--batch"}

		probe, err := startExtProc(candidate, nil)
		if err != nil {
			return
		}
		defer probe.close()

		// Non-ASCII on purpose: encoding is what broke the equivalent path
		// for AsciiDoc, and an ASCII-only probe would not have caught it.
		got, err := probe.convert("= naïve heading\n")
		if err != nil || !strings.Contains(got, "naïve heading") {
			return
		}

		typstDirect = candidate
	})

	return typstDirect
}

// lintTypst lints Typst: its markup layer, parsed without evaluation.
func (l *Linter) lintTypst(f *core.File) error {
	exe := system.Which([]string{"typst2vast"})
	if exe == "" {
		return core.NewE100(f.Path, errors.New("typst2vast not found; it's needed to read Typst"))
	}

	err := l.lintMetadata(f)
	if err != nil {
		return err
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	html, err := l.callTypst(s, exe)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	return l.lintHTMLTokens(f, []byte(html), 0)
}

// callTypst converts one document, over a pooled process when the installed
// typst2vast supports it.
func (l *Linter) callTypst(text, exe string) (string, error) {
	if direct := typstFastPath(); direct != nil {
		l.typstPoolOnce.Do(func() {
			pool, err := newProcPool(direct, nil, l.poolSize())
			if err == nil {
				l.typst = pool
			}
		})

		if l.typst != nil {
			return l.typst.convert(text, direct, nil)
		}
	}

	return system.ExecuteWithInput(exe, text)
}
