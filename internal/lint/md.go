package lint

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	grh "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Markdown configuration.
var goldMd = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		// Treat `$$…$$` display math as a skipped block so it isn't
		// spell-checked as prose. See #878 and math.go.
		mathExtension{},
	),
	goldmark.WithRendererOptions(
		grh.WithUnsafe(),
	),
)

// Convert extended info strings -- e.g., ```callout{'title': 'NOTE'} -- that
// might confuse Blackfriday into normal "```".
var reExInfo = regexp.MustCompile("`{3,}" + `.+`)

var reLinkRef = regexp.MustCompile(`\]\[(?:[^]\n]+)\]`)
var reLinkDef = regexp.MustCompile(`\[(?:[^]\n]+)\]:`)

var reNumericList = regexp.MustCompile(`(?m)^\d+\.`)

func (l *Linter) lintMarkdown(f *core.File) error {
	return l.lintMarkdownWith(f, goldMd)
}

// lintMarkdownWith lints f as Markdown read by the given converter -- the
// plain configuration, or a dialect's such as MyST's.
func (l *Linter) lintMarkdownWith(f *core.File, md goldmark.Markdown) error {
	var buf bytes.Buffer

	err := l.lintMetadata(f)
	if err != nil {
		return err
	}

	s, err := l.Transform(f)
	if err != nil {
		return err
	}

	src := []byte(s)
	ctx := l.mystContext()
	doc := md.Parser().Parse(text.NewReader(src), parser.WithContext(ctx))
	if err = md.Renderer().Render(&buf, src, doc); err != nil {
		return core.NewE100(f.Path, err)
	}

	if md == goldMdx {
		// The transform rewrites the front matter, so the spans of the
		// parsed text are not the file's; the file is parsed again for them.
		fileCtx, fileSrc := ctx, src
		if s != f.Content {
			fileCtx, fileSrc = l.mystContext(), []byte(f.Content)
			doc = md.Parser().Parse(text.NewReader(fileSrc), parser.WithContext(fileCtx))
		}

		// The rest of a file after a block that never closed was read as
		// code, so none of it would be checked; MDX itself rejects the file.
		if open, ok := ctx.Get(mdxUnclosedKey).(mdxUnclosed); ok {
			if placed, found := fileCtx.Get(mdxUnclosedKey).(mdxUnclosed); found {
				open, src = placed, fileSrc
			}
			line := bytes.Count(src[:open.offset], []byte("\n")) + 1
			return core.NewE201FromPosition(fmt.Sprintf(
				"Invalid MDX: %s, so the rest of the file can't be read.", open.what), f.Path, line)
		}
		f.Content = maskSpans(f.Content, mdxTagMasks(doc))
	}
	f.Content = prepMarkdown(f.Content)
	return l.lintHTMLTokens(f, buf.Bytes(), 0)
}

func prepMarkdown(content string) string {
	// NOTE: This is required to avoid finding matches inside info strings. For
	// example, if we're looking for 'json' we many incorrectly report the
	// location as being in an infostring like '```json'.
	//
	// See https://github.com/errata-ai/vale/v2/issues/248.
	body := reExInfo.ReplaceAllStringFunc(content, func(m string) string {
		parts := strings.Split(m, "`")

		// This ensures that we respect the number of opening backticks, which
		// could be more than 3.
		//
		// See https://github.com/errata-ai/vale/v2/issues/271.
		tags := strings.Repeat("`", len(parts)-1)
		span := strings.Repeat("*", nlp.StrLen(parts[len(parts)-1]))

		return tags + span
	})

	// NOTE: This is required to avoid finding matches inside link references.
	body = reLinkRef.ReplaceAllStringFunc(body, func(m string) string {
		return "][" + strings.Repeat("*", nlp.StrLen(m)-3) + "]"
	})
	body = reLinkDef.ReplaceAllStringFunc(body, func(m string) string {
		return "[" + strings.Repeat("*", nlp.StrLen(m)-3) + "]:"
	})

	// NOTE: This is required to avoid finding matches inside ordered lists.
	body = reNumericList.ReplaceAllStringFunc(body, func(m string) string {
		return strings.Repeat("*", nlp.StrLen(m))
	})

	return body
}
