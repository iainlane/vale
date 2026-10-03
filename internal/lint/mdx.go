package lint

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	grh "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/vale-cli/vale/v3/internal/core"
)

// MDX is CommonMark plus ESM statements, JSX elements, and JavaScript
// expressions. The JavaScript -- ESM, expressions, tags and their
// attributes, self-closing elements -- holds no prose and renders as inline
// or fenced code, which the walker skips. A JSX element's children are
// Markdown, though, just as MDX itself reads them: a flow element becomes a
// div classed with the element's name, so its content is linted and a rule
// can reach (or a user can ignore) it by class, and an inline element
// becomes a span the same way. A `{/* ... */}` flow comment becomes an HTML
// comment instead, so comment-based configuration keeps working.

// MDX configuration: Markdown, plus the MDX constructs.
var goldMdx = goldmark.New(
	goldmark.WithParser(parser.NewParser(
		parser.WithBlockParsers(mdxBlockParsers()...),
		parser.WithInlineParsers(parser.DefaultInlineParsers()...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
	)),
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		mathExtension{},
		mdxExtension{},
	),
	goldmark.WithRendererOptions(
		grh.WithUnsafe(),
	),
)

type mdxExtension struct{}

func (mdxExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		// Ahead of the HTML block parser (900), which would otherwise claim
		// a JSX element, and the paragraph parser (1000).
		util.Prioritized(&mdxEsmParser{}, 880),
		util.Prioritized(&mdxFlowExprParser{}, 881),
		util.Prioritized(&mdxJsxFlowParser{}, 882),
	))
	m.Parser().AddOptions(parser.WithInlineParsers(
		// Behind the autolink parser (300), so `<https://...>` stays a link,
		// and ahead of the raw-HTML parser (400), which would otherwise
		// claim an inline JSX tag.
		util.Prioritized(&mdxInlineParser{}, 350),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(mdxRenderer{}, 1),
	))
}

// An mdxScan tracks nesting through JavaScript-ish source: one depth for
// every kind of bracket, with strings, template literals, and comments
// recognized so that a bracket inside them doesn't count.
//
// It is an approximation -- a regex literal holding a bracket would fool
// it -- but it covers the JavaScript that appears in documentation.
type mdxScan struct {
	depth   int
	quote   byte // ' " ` or 0
	comment bool // inside /* ... */

	// jsx is an element inside the expression, until it completes. Its text
	// is prose, where a quote or a bracket means nothing.
	jsx *mdxJsxScan

	// prev is the last byte outside a string or comment, and word the
	// identifier it ends; they say whether a `<` can begin an element.
	prev byte
	word []byte
}

// scan processes one line.
func (s *mdxScan) scan(line []byte) {
	s.walk(line, false)
}

// scanExpr processes one line of a `{...}` expression, stopping once it
// closes, and returns the index just past the closing brace, or -1.
func (s *mdxScan) scanExpr(line []byte) int {
	return s.walk(line, true)
}

func (s *mdxScan) walk(line []byte, expr bool) int {
	for i := 0; i < len(line); i++ {
		c := line[i]

		if s.jsx != nil {
			n := s.jsx.scan(line[i:])
			if !s.jsx.done {
				return -1
			}
			s.jsx = nil
			s.note('>')
			i += n - 1
			continue
		}
		if s.comment {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				s.comment = false
				i++
			}
			continue
		}
		if s.quote != 0 {
			if c == '\\' {
				i++
			} else if c == s.quote {
				s.quote = 0
				s.note(c)
			}
			continue
		}

		switch c {
		case '\'', '"', '`':
			s.quote = c
			continue
		case '/':
			if i+1 < len(line) {
				if line[i+1] == '*' {
					s.comment = true
					i++
					continue
				} else if line[i+1] == '/' {
					return -1 // a line comment runs to the end
				}
			}
		case '<':
			if s.jsxCanStart() && jsxStarts(line[i:]) {
				s.jsx = &mdxJsxScan{}
				n := s.jsx.scan(line[i:])
				if !s.jsx.done {
					return -1
				}
				s.jsx = nil
				s.note('>')
				i += n - 1
				continue
			}
		case '{', '(', '[':
			s.depth++
		case '}', ')', ']':
			s.depth--
			if expr && s.depth <= 0 {
				return i + 1
			}
		}
		s.note(c)
	}
	return -1
}

// note records c as the last significant byte.
func (s *mdxScan) note(c byte) {
	if util.IsSpace(c) {
		return
	}
	s.prev = c
	if mdxIdent(c) {
		s.word = append(s.word, c)
	} else {
		s.word = s.word[:0]
	}
}

func mdxIdent(c byte) bool {
	return c == '_' || c == '$' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// jsxCanStart reports whether a `<` here begins an element rather than a
// comparison or a type argument: at the start of the expression, after a
// byte no operand ends with, or after a keyword an expression follows.
func (s *mdxScan) jsxCanStart() bool {
	if s.prev == 0 || strings.IndexByte("{([,=:?&|!>;", s.prev) >= 0 {
		return true
	}
	switch string(s.word) {
	case "return", "yield", "await":
		return true
	}
	return false
}

// jsxStarts reports whether rest, which begins with `<`, opens an element
// or a fragment.
func jsxStarts(rest []byte) bool {
	if len(rest) < 2 {
		return false
	}
	c := rest[1]
	return c == '>' || c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// An mdxBlock is one flow-level MDX node: an ESM block, a flow expression,
// or a JSX element. It renders as code -- or, for a `{/* ... */}` comment,
// as an HTML comment.
type mdxBlock struct {
	ast.BaseBlock

	typ      string
	finished bool

	// scan carries the node's parsing state across lines.
	scan mdxScan
}

var kindMdxBlock = ast.NewNodeKind("MdxBlock")

func (n *mdxBlock) Kind() ast.NodeKind { return kindMdxBlock }
func (n *mdxBlock) IsRaw() bool        { return true }
func (n *mdxBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// source reassembles the node's text, without the trailing newline.
func (n *mdxBlock) source(src []byte) []byte {
	var buf bytes.Buffer
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		buf.Write(line.Value(src))
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// consume appends the current line to the node and moves past it.
func mdxConsume(n *mdxBlock, reader text.Reader) {
	_, segment := reader.PeekLine()
	seg := text.NewSegment(segment.Start, segment.Stop)
	seg.ForceNewline = true
	n.Lines().Append(seg)
	reader.Advance(segment.Len() - 1)
}

// mdxBlockParsers is CommonMark without indented code, which MDX removed
// from the grammar, and with every other block opening at any indentation.
func mdxBlockParsers() []util.PrioritizedValue {
	return []util.PrioritizedValue{
		util.Prioritized(mdxIndented{parser.NewSetextHeadingParser()}, 100),
		util.Prioritized(mdxIndented{parser.NewThematicBreakParser()}, 200),
		util.Prioritized(mdxIndented{parser.NewListParser()}, 300),
		util.Prioritized(mdxIndented{parser.NewListItemParser()}, 400),
		util.Prioritized(mdxIndented{parser.NewATXHeadingParser()}, 600),
		util.Prioritized(mdxIndented{parser.NewFencedCodeBlockParser()}, 700),
		util.Prioritized(mdxIndented{parser.NewBlockquoteParser()}, 800),
		util.Prioritized(mdxIndented{mdxHTMLBlock{parser.NewHTMLBlockParser()}}, 900),
		util.Prioritized(mdxIndented{parser.NewParagraphParser()}, 1000),
	}
}

// An mdxHTMLBlock is the HTML block parser, declining the line the JSX flow
// parser declined: a one-line element with text between its tags, which MDX
// reads as a paragraph holding an inline element. Left to the HTML block
// parser, `<summary>Click me</summary>` opened a block that ran to the next
// blank line, and a code fence on the line after it was read as prose.
type mdxHTMLBlock struct {
	parser.BlockParser
}

func (b mdxHTMLBlock) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	if pos := pc.BlockOffset(); pos >= 0 && opensJsx(line, pos) {
		var s mdxJsxScan
		if end := mdxTagEnd(line[pos:], &s); end >= 0 &&
			(s.done || mdxDoneOnLine(s, line[pos+end:])) && mdxHasText(line[pos+end:]) {
			return nil, parser.NoChildren
		}
	}
	return b.BlockParser.Open(parent, reader, pc)
}

// An mdxIndented wraps a block parser so that indentation of four or more
// columns is whitespace, as it is in MDX: the wrapper consumes it before
// the parser looks at the line, and a block opened that way consumes the
// same amount on each of its later lines, so its offsets stay consistent.
type mdxIndented struct {
	parser.BlockParser
}

var mdxIndentKey = parser.NewContextKey()

// mdxIndentState tracks what each open block consumes per line, and what a
// block consumed on the current line before closing, which the next block
// opened on that line inherits.
type mdxIndentState struct {
	owned    map[ast.Node]int
	line     int
	orphaned int
}

func mdxIndentOf(pc parser.Context) *mdxIndentState {
	if st, ok := pc.Get(mdxIndentKey).(*mdxIndentState); ok {
		return st
	}
	st := &mdxIndentState{owned: map[ast.Node]int{}}
	pc.Set(mdxIndentKey, st)
	return st
}

// orphaned returns the columns consumed on line by blocks since closed.
func (st *mdxIndentState) orphan(line int) int {
	if st.line != line {
		st.line, st.orphaned = line, 0
	}
	return st.orphaned
}

// mdxEatIndent consumes up to cols columns of indentation, or all of it
// when cols is negative, and returns how many it consumed.
func mdxEatIndent(reader text.Reader, cols int) int {
	line, _ := reader.PeekLine()
	w, _ := util.IndentWidth(line, reader.LineOffset())
	if cols < 0 || cols > w {
		cols = w
	}
	if cols == 0 || util.IsBlank(line) {
		return 0
	}
	pos, padding := util.IndentPosition(line, reader.LineOffset(), cols)
	reader.AdvanceAndSetPadding(pos, padding)
	return cols
}

func (p mdxIndented) CanAcceptIndentedLine() bool { return true }

func (p mdxIndented) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	lineNum, seg := reader.Position()
	offset, indent := pc.BlockOffset(), pc.BlockIndent()
	st := mdxIndentOf(pc)

	eaten := 0
	if indent > 3 {
		eaten = mdxEatIndent(reader, -1)
		line, _ := reader.PeekLine()
		w, pos := util.IndentWidth(line, reader.LineOffset())
		pc.SetBlockOffset(pos)
		pc.SetBlockIndent(w)
	}

	node, state := p.BlockParser.Open(parent, reader, pc)
	if node == nil {
		reader.SetPosition(lineNum, seg)
		pc.SetBlockOffset(offset)
		pc.SetBlockIndent(indent)
		return nil, state
	}
	// The columns a closed block left on this line pass to the first block
	// opened in its place. A block opened within that one on the same line
	// -- a list's first item, say -- must not take them too: its parent eats
	// them first on every later line, so it would eat past the line's own
	// indentation and close on a line its parent kept, and the parent, left
	// open with no item, would take the next block as a child of its own.
	if own := st.orphan(lineNum) + eaten; own > 0 {
		st.owned[node] = own
		st.orphaned = 0
	}
	return node, state
}

func (p mdxIndented) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	st := mdxIndentOf(pc)
	lineNum, _ := reader.Position()

	// A list holds items. One that was given another kind of child can't
	// continue: the list parser reads its last child as an item, and panics.
	if list, ok := node.(*ast.List); ok && list.LastChild() != nil {
		if _, isItem := list.LastChild().(*ast.ListItem); !isItem {
			return parser.Close
		}
	}

	eaten := 0
	if own := st.owned[node]; own > 0 {
		eaten = mdxEatIndent(reader, own)
	}
	state := p.BlockParser.Continue(node, reader, pc)
	if state&parser.Continue == 0 {
		st.orphan(lineNum)
		st.orphaned += eaten
	}
	return state
}

func (p mdxIndented) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	p.BlockParser.Close(node, reader, pc)
	delete(mdxIndentOf(pc).owned, node)
}

// mdxEsm matches the start of an ESM statement.
var mdxEsm = regexp.MustCompile(`^(?:import|export)\b`)

// An mdxEsmParser reads a block of import/export statements. Contiguous
// statements are one node, matching how MDX reads them.
type mdxEsmParser struct{}

func (*mdxEsmParser) Trigger() []byte {
	return []byte{'i', 'e'}
}

func (*mdxEsmParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos != 0 || !mdxEsm.Match(line) {
		return nil, parser.NoChildren
	}

	node := &mdxBlock{typ: "mdxjsEsm"}
	node.scan.scan(line)
	node.finished = node.scan.depth <= 0 && !node.scan.comment && node.scan.quote == 0
	mdxConsume(node, reader)

	return node, parser.NoChildren
}

func (*mdxEsmParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	n := node.(*mdxBlock) //nolint:errcheck // only mdxBlock is opened
	line, _ := reader.PeekLine()

	if n.finished {
		// Another statement directly below joins the block.
		if !mdxEsm.Match(line) {
			return parser.Close
		}
		n.scan = mdxScan{}
	}

	n.scan.scan(line)
	n.finished = n.scan.depth <= 0 && !n.scan.comment && n.scan.quote == 0
	mdxConsume(n, reader)

	return parser.Continue | parser.NoChildren
}

func (*mdxEsmParser) Close(ast.Node, text.Reader, parser.Context) {}

func (*mdxEsmParser) CanInterruptParagraph() bool { return false }
func (*mdxEsmParser) CanAcceptIndentedLine() bool { return false }

// An mdxFlowExprParser reads a `{...}` expression standing on its own.
type mdxFlowExprParser struct{}

func (*mdxFlowExprParser) Trigger() []byte {
	return []byte{'{'}
}

func (*mdxFlowExprParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos >= len(line) || line[pos] != '{' {
		return nil, parser.NoChildren
	}

	node := &mdxBlock{typ: "mdxFlowExpression"}
	end := node.scan.scanExpr(line[pos:])
	if end >= 0 && !util.IsBlank(line[pos+end:]) {
		// Prose follows on the line: a text expression in a paragraph.
		return nil, parser.NoChildren
	}
	node.finished = end >= 0
	mdxConsume(node, reader)

	return node, parser.NoChildren
}

func (*mdxFlowExprParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	n := node.(*mdxBlock) //nolint:errcheck // only mdxBlock is opened
	if n.finished {
		return parser.Close
	}

	line, _ := reader.PeekLine()
	n.finished = n.scan.scanExpr(line) >= 0
	mdxConsume(n, reader)

	return parser.Continue | parser.NoChildren
}

func (*mdxFlowExprParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	if n, ok := node.(*mdxBlock); ok && !n.finished && n.Lines().Len() > 0 && mdxAtEnd(reader) {
		mdxNoteUnclosed(pc, n.Lines().At(0).Start, "the expression opened here never closes")
	}
}

// mdxUnclosedKey holds the first block a document left open at its end: an
// expression or a tag the parser read to the end of the file as code, which
// MDX itself rejects.
var mdxUnclosedKey = parser.NewContextKey()

// mdxUnclosed is where a block that never closed began, and what it was.
type mdxUnclosed struct {
	offset int
	what   string
}

// mdxAtEnd reports whether the reader has no input left: a block closed
// there ran to the end of the file, not to the end of its parent.
func mdxAtEnd(reader text.Reader) bool {
	line, _ := reader.PeekLine()
	return line == nil
}

func mdxNoteUnclosed(pc parser.Context, offset int, what string) {
	if pc.Get(mdxUnclosedKey) == nil {
		pc.Set(mdxUnclosedKey, mdxUnclosed{offset, what})
	}
}

func (*mdxFlowExprParser) CanInterruptParagraph() bool { return true }
func (*mdxFlowExprParser) CanAcceptIndentedLine() bool { return true }

// An mdxJsxScan walks a JSX element to its end: tags are pushed and popped,
// and `{...}` expressions -- in attributes or children -- are handed to an
// mdxScan so their contents can't end a tag or the element.
type mdxJsxScan struct {
	stack []string

	// mode is where the scan is: 0 children text, 1 inside a tag's name or
	// attributes, 2 inside a JavaScript expression.
	mode int

	js mdxScan

	// wasInTag remembers whether the active expression began inside a tag,
	// so its end returns the scan to the right mode.
	wasInTag bool

	name    []byte // the tag being read
	named   bool   // the name is complete
	closing bool   // the tag is </...>
	selfEnd bool   // the tag ends with />
	quote   byte   // inside an attribute string

	begun bool // at least one tag has been read
	done  bool // the element is complete
}

// tagName reports whether c can appear in a JSX element name -- which allows
// member expressions (`myComponents.thisOne`) and, at the start, a fragment's
// empty name.
func mdxTagName(c byte) bool {
	return c == '.' || c == '-' || c == '_' || c == '$' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// scan processes one line of the element and returns how much of it was
// read: all of it, or up to where the element completed.
func (s *mdxJsxScan) scan(line []byte) int {
	for i := 0; i < len(line); i++ {
		c := line[i]

		switch s.mode {
		case 2: // expression
			// The expression scanner reads the rest of the line, so an
			// escape or a line comment is seen whole; its depth began at 1
			// for the opening brace.
			end := s.js.scanExpr(line[i:])
			if end < 0 {
				return len(line)
			}
			i += end - 1
			if s.wasInTag {
				s.mode = 1
			} else {
				s.mode = 0
			}
		case 1: // tag
			if s.quote != 0 {
				if c == s.quote {
					s.quote = 0
				}
				continue
			}
			switch {
			case !s.named && mdxTagName(c):
				s.name = append(s.name, c)
			case !s.named:
				s.named = true
				i-- // reprocess as an attribute character
			case c == '\'' || c == '"':
				s.quote = c
			case c == '{':
				s.js = mdxScan{depth: 1}
				s.wasInTag = true
				s.mode = 2
			case c == '/':
				s.selfEnd = true
			case c == '>':
				s.endTag()
			default:
				// an attribute character
			}
		default: // children
			switch c {
			case '<':
				s.mode = 1
				s.name = nil
				s.named = false
				s.closing = false
				s.selfEnd = false
				if i+1 < len(line) && line[i+1] == '/' {
					s.closing = true
					i++
				}
			case '{':
				s.js = mdxScan{depth: 1}
				s.wasInTag = false
				s.mode = 2
			}
		}

		if s.done {
			return i + 1
		}
	}
	return len(line)
}

func (s *mdxJsxScan) endTag() {
	name := string(s.name)
	switch {
	case s.closing:
		if n := len(s.stack); n > 0 {
			s.stack = s.stack[:n-1]
		}
	case s.selfEnd:
		// opens and closes at once
	default:
		s.stack = append(s.stack, name)
	}

	s.begun = true
	s.mode = 0
	if len(s.stack) == 0 {
		s.done = true
	}
}

// clone copies the scan, its slices included, so a caller can probe ahead
// without committing.
func (s *mdxJsxScan) clone() mdxJsxScan {
	t := *s
	t.stack = append([]string(nil), s.stack...)
	t.name = append([]byte(nil), s.name...)
	t.js = s.js.clone()
	return t
}

// clone copies the scan, so a trial scan of a prefix leaves the original
// and the element it may be inside untouched.
func (s *mdxScan) clone() mdxScan {
	t := *s
	t.word = append([]byte(nil), s.word...)
	if s.jsx != nil {
		jsx := s.jsx.clone()
		t.jsx = &jsx
	}
	return t
}

// mdxTagEnd returns the offset just past the line's first completed tag,
// scanning from the carried state, or -1 when the tag needs more lines. On
// success the carried state is advanced to that offset.
//
// The scan reads a character ahead of itself (`</`, `/*`), so each prefix is
// probed whole from a copy rather than a character at a time.
func mdxTagEnd(line []byte, carried *mdxJsxScan) int {
	for i := 1; i <= len(line); i++ {
		t := carried.clone()
		t.scan(line[:i])
		if t.begun && t.mode == 0 {
			*carried = t
			return i
		}
	}
	return -1
}

// opensJsx reports whether line[pos:] begins a JSX tag: `<` followed by a
// name, a fragment's `>`, or a closing `/`. `<!`, `<?`, and autolink-style
// `<scheme:` starts are left to other parsers.
func opensJsx(line []byte, pos int) bool {
	if pos >= len(line) || line[pos] != '<' {
		return false
	}
	rest := line[pos+1:]
	if len(rest) == 0 {
		return false
	}
	if rest[0] == '>' || rest[0] == '/' {
		return true
	}
	if !mdxTagName(rest[0]) {
		return false
	}
	// A scheme (`https:`) or an email (`user@host`) means an autolink.
	for _, c := range rest {
		if c == ' ' || c == '\t' || c == '>' || c == '/' || c == '\n' || c == '{' {
			return true
		}
		if !mdxTagName(c) {
			return false
		}
	}
	return true
}

// An mdxJsxContainer is a flow JSX element whose children are Markdown, per
// MDX's own grammar: only the tags, attributes, and expressions are
// JavaScript. It renders as a div classed with the element's name.
//
// An element whose open tag spans lines but that turns out to be childless
// (a multiline `<Component ... />`) collects its source in raw and renders
// as code, the way a single-line element does.
type mdxJsxContainer struct {
	ast.BaseBlock

	name    string
	jsx     mdxJsxScan   // carries open-tag state across lines
	raw     bytes.Buffer // the source read while pending
	pending bool         // still inside the open tag
	rawOnly bool         // finished childless; render raw as code

	// depth counts same-name elements opened on child lines, so a nested
	// element's close tag isn't taken for this one's; fenced guards the
	// count against JSX quoted in a code fence.
	depth  int
	fenced bool

	// spans are the source ranges of the element's own tags, which hold
	// no prose.
	spans [][2]int
}

var kindMdxJsxContainer = ast.NewNodeKind("MdxJsxContainer")

func (n *mdxJsxContainer) Kind() ast.NodeKind { return kindMdxJsxContainer }
func (n *mdxJsxContainer) IsRaw() bool        { return n.rawOnly }
func (n *mdxJsxContainer) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// mdxIsCloseTag reports whether line is exactly a closing tag for name.
func mdxIsCloseTag(line []byte, name string) bool {
	if !bytes.HasPrefix(line, []byte("</")) {
		return false
	}
	rest := line[2:]
	if !bytes.HasPrefix(rest, []byte(name)) {
		return false
	}
	rest = bytes.TrimLeft(rest[len(name):], " \t")
	return len(rest) == 1 && rest[0] == '>'
}

// mdxNetOpens counts the name's tags opened minus closed on a child line, so
// a same-name element handled by a descendant parser is not mistaken for
// this one's close. It reads one line at a time -- a tag broken across lines
// or a `>` quoted in an attribute can miscount -- which covers the JSX that
// appears in documentation.
func mdxNetOpens(line []byte, name string) int {
	if name == "" {
		return 0
	}

	net := 0
	tag := []byte(name)

	for i := 0; i+1 < len(line); i++ {
		if line[i] != '<' {
			continue
		}
		j := i + 1
		closing := line[j] == '/'
		if closing {
			j++
		}
		if !bytes.HasPrefix(line[j:], tag) {
			continue
		}
		k := j + len(tag)
		if k < len(line) && mdxTagName(line[k]) {
			continue // a longer name
		}
		if closing {
			net--
			continue
		}
		end := bytes.IndexByte(line[k:], '>')
		if end < 0 {
			net++ // the tag spans lines
		} else if end == 0 || line[k+end-1] != '/' {
			net++
		}
	}
	return net
}

// An mdxJsxFlowParser reads a block-level JSX element. An element opened and
// closed on one line -- self-closing or otherwise -- is a code node, and an
// element with lines of its own is a container whose children keep parsing
// as Markdown.
type mdxJsxFlowParser struct{}

func (*mdxJsxFlowParser) Trigger() []byte {
	return []byte{'<'}
}

func (*mdxJsxFlowParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || !opensJsx(line, pos) {
		return nil, parser.NoChildren
	}

	var s mdxJsxScan
	end := mdxTagEnd(line[pos:], &s)

	if end < 0 {
		// The open tag itself spans lines.
		_, seg := reader.Position()
		node := &mdxJsxContainer{pending: true}
		node.jsx.scan(line[pos:])
		node.raw.Write(line[pos:])
		node.spans = append(node.spans, [2]int{seg.Start + pos, seg.Stop})
		mdxAdvanceLine(reader, line)
		return node, parser.HasChildren
	}

	if s.done || mdxDoneOnLine(s, line[pos+end:]) {
		if mdxHasText(line[pos+end:]) {
			// Text beside the tags: MDX reads the line as a paragraph
			// holding an inline element, so its text is prose.
			return nil, parser.NoChildren
		}
		// The whole element sits on this line with nothing to lint.
		node := &mdxBlock{typ: "mdxJsxFlowElement", finished: true}
		mdxConsume(node, reader)
		return node, parser.NoChildren
	}

	_, seg := reader.Position()
	node := &mdxJsxContainer{name: s.stack[len(s.stack)-1]}
	node.spans = append(node.spans, [2]int{seg.Start + pos, seg.Start + pos + end})
	reader.Advance(pos + end)
	return node, parser.HasChildren
}

// mdxHasText reports whether rest holds text outside its tags and
// expressions.
func mdxHasText(rest []byte) bool {
	for i := 0; i < len(rest); {
		switch {
		case rest[i] == '<':
			end := mdxTagEnd(rest[i:], &mdxJsxScan{})
			if end < 0 {
				return false
			}
			i += end
		case rest[i] == '{':
			n := (&mdxScan{}).scanExpr(rest[i:])
			if n < 0 {
				return false
			}
			i += n
		case !util.IsSpace(rest[i]):
			return true
		default:
			i++
		}
	}
	return false
}

// mdxDoneOnLine reports whether the element completes in the rest of its
// first line.
func mdxDoneOnLine(s mdxJsxScan, rest []byte) bool {
	t := s.clone()
	t.scan(rest)
	return t.done
}

// mdxAdvanceLine consumes the rest of the current line.
func mdxAdvanceLine(reader text.Reader, line []byte) {
	reader.Advance(len(line) - 1)
}

func (*mdxJsxFlowParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	n, ok := node.(*mdxJsxContainer)
	if !ok {
		// The single-line code form closes itself.
		return parser.Close
	}

	line, seg := reader.PeekLine()

	if n.pending {
		end := mdxTagEnd(line, &n.jsx)
		if end < 0 {
			n.jsx.scan(line)
			n.raw.Write(line)
			n.spans = append(n.spans, [2]int{seg.Start, seg.Stop})
			mdxAdvanceLine(reader, line)
			return parser.Continue | parser.HasChildren
		}
		if n.jsx.done {
			// Childless after all: a multiline self-closing element.
			n.pending = false
			n.raw.Write(bytes.TrimRight(line, "\n"))
			n.rawOnly = true
			n.spans = append(n.spans, [2]int{seg.Start, seg.Stop})
			mdxAdvanceLine(reader, line)
			return parser.Close
		}
		n.pending = false
		n.name = n.jsx.stack[len(n.jsx.stack)-1]
		n.spans = append(n.spans, [2]int{seg.Start, seg.Start + end})
		reader.Advance(end)
		return parser.Continue | parser.HasChildren
	}

	trimmed := bytes.TrimSpace(line)

	if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
		n.fenced = !n.fenced
	} else if !n.fenced {
		if mdxIsCloseTag(trimmed, n.name) {
			if n.depth == 0 {
				n.spans = append(n.spans, [2]int{seg.Start, seg.Stop})
				mdxAdvanceLine(reader, line)
				return parser.Close
			}
			n.depth--
		} else {
			n.depth += mdxNetOpens(line, n.name)
		}
	}

	return parser.Continue | parser.HasChildren
}

func (*mdxJsxFlowParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	if n, ok := node.(*mdxJsxContainer); ok && n.pending && len(n.spans) > 0 && mdxAtEnd(reader) {
		mdxNoteUnclosed(pc, n.spans[0][0], "the tag opened here never ends")
	}
}

func (*mdxJsxFlowParser) CanInterruptParagraph() bool { return true }
func (*mdxJsxFlowParser) CanAcceptIndentedLine() bool { return true }

// An mdxInline is an inline MDX node: a text expression, a self-closing JSX
// element -- both rendered as code spans -- or one tag of a JSX element
// whose children stay inline prose, rendered as a span so the element's
// name reaches rules as a class.
type mdxInline struct {
	ast.BaseInline

	typ  string
	text []byte

	form int    // 0 code, 1 an open tag, 2 a close tag
	name string // the tag's name, for form 1

	span [2]int // the source range of the tag or expression
}

var kindMdxInline = ast.NewNodeKind("MdxInline")

func (n *mdxInline) Kind() ast.NodeKind { return kindMdxInline }
func (n *mdxInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mdxInlineParser struct{}

func (*mdxInlineParser) Trigger() []byte {
	return []byte{'{', '<'}
}

func (*mdxInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 {
		return nil
	}

	if line[0] == '{' {
		return mdxParseInline(block, "mdxTextExpression", func() *mdxJsxScan {
			return nil
		})
	}
	if opensJsx(line, 0) {
		return mdxParseInline(block, "mdxJsxTextElement", func() *mdxJsxScan {
			return &mdxJsxScan{}
		})
	}
	return nil
}

// mdxParseInline consumes an expression or one JSX tag that may span lines
// within its paragraph, returning nil -- with the reader restored -- when it
// never ends. An element's children are left inline, so they keep parsing
// as prose; only the tag itself becomes a node.
func mdxParseInline(block text.Reader, typ string, newJsx func() *mdxJsxScan) ast.Node {
	l, pos := block.Position()

	var collected []byte
	js := mdxScan{}
	jsx := newJsx()

	for {
		line, _ := block.PeekLine()
		if line == nil {
			block.SetPosition(l, pos)
			return nil
		}

		if jsx != nil {
			end := mdxTagEnd(line, jsx)
			if end < 0 {
				jsx.scan(line)
				collected = append(collected, line...)
				block.AdvanceLine()
				continue
			}
			collected = append(collected, line[:end]...)
			block.Advance(end)
			node := mdxInlineTag(collected, jsx)
			_, at := block.Position()
			node.span = [2]int{pos.Start, at.Start}
			return node
		}

		if used := js.scanExpr(line); used >= 0 {
			collected = append(collected, line[:used]...)
			block.Advance(used)
			_, at := block.Position()
			return &mdxInline{typ: typ, text: bytes.TrimRight(collected, "\n"),
				span: [2]int{pos.Start, at.Start}}
		}

		collected = append(collected, line...)
		block.AdvanceLine()
	}
}

// mdxInlineTag builds the node for one completed inline tag: an open tag is
// a span whose children follow as prose, a close tag ends one, and a
// self-closing element stays a code span.
func mdxInlineTag(collected []byte, jsx *mdxJsxScan) *mdxInline {
	node := &mdxInline{typ: "mdxJsxTextElement", text: bytes.TrimRight(collected, "\n")}

	switch {
	case bytes.HasPrefix(collected, []byte("</")):
		node.form = 2
	case !jsx.done:
		node.form = 1
		node.name = jsx.stack[len(jsx.stack)-1]
	}
	return node
}

type mdxRenderer struct{}

func (mdxRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMdxBlock, renderMdxBlock)
	reg.Register(kindMdxJsxContainer, renderMdxContainer)
	reg.Register(kindMdxInline, renderMdxInline)
}

// mdxClassName renders an element name as a class: a member expression's
// dots become hyphens, since a dot separates scope parts.
func mdxClassName(name string) string {
	return strings.ReplaceAll(name, ".", "-")
}

func renderMdxContainer(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*mdxJsxContainer)
	if !ok {
		return ast.WalkContinue, nil
	}

	if n.rawOnly {
		// A childless element is tags and nothing else, so it renders as
		// nothing, like a container's tags: its source is masked out of the
		// search context, and the walker must not then look for it there,
		// which moved every later match and lost the alerts whose words the
		// element also held.
		return ast.WalkContinue, nil
	}

	if !entering {
		_, _ = w.WriteString("</div>\n")
	} else if cls := mdxClassName(n.name); cls != "" {
		_, _ = w.WriteString(`<div class="` + cls + `">` + "\n")
	} else {
		_, _ = w.WriteString("<div>\n")
	}
	return ast.WalkContinue, nil
}

// isMdxComment reports whether source is a `{/* ... */}` comment.
func isMdxComment(source []byte) bool {
	return bytes.HasPrefix(source, []byte("{/*")) && bytes.HasSuffix(source, []byte("*/}"))
}

func renderMdxBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*mdxBlock)
	if !ok || !entering {
		return ast.WalkContinue, nil
	}

	src := n.source(source)
	if n.typ == "mdxFlowExpression" && isMdxComment(src) {
		_, _ = w.WriteString("<!--")
		_, _ = w.Write(src[3 : len(src)-3])
		_, _ = w.WriteString("-->\n")
		return ast.WalkContinue, nil
	}

	if bytes.ContainsRune(src, '\n') {
		_, _ = w.WriteString(`<pre><code class="mdxNode ` + n.typ + `">`)
		_, _ = w.Write(util.EscapeHTML(src))
		_, _ = w.WriteString("</code></pre>\n")
	} else {
		_, _ = w.WriteString(`<code class="mdxNode ` + n.typ + `">`)
		_, _ = w.Write(util.EscapeHTML(src))
		_, _ = w.WriteString("</code>\n")
	}
	return ast.WalkContinue, nil
}

func renderMdxInline(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n, ok := node.(*mdxInline)
	if !ok || !entering {
		return ast.WalkContinue, nil
	}

	switch n.form {
	case 1:
		if cls := mdxClassName(n.name); cls != "" {
			_, _ = w.WriteString(`<span class="` + cls + `">`)
		} else {
			_, _ = w.WriteString("<span>")
		}
	case 2:
		_, _ = w.WriteString("</span>")
	case 0:
		if n.typ == "mdxTextExpression" && isMdxComment(n.text) {
			_, _ = w.WriteString("<!--")
			_, _ = w.Write(n.text[3 : len(n.text)-3])
			_, _ = w.WriteString("-->")
			return ast.WalkContinue, nil
		}
		fallthrough
	default:
		_, _ = w.WriteString(`<code class="mdxNode ` + n.typ + `">`)
		_, _ = w.Write(util.EscapeHTML(n.text))
		_, _ = w.WriteString("</code>")
	}
	return ast.WalkContinue, nil
}

// mdxTagMasks returns the source ranges of the JSX tags in doc. An alert
// is placed by searching the source for its text, and a tag is the one
// piece of source the walker never sees, so its attributes are still there
// to be found unless they are blanked.
func mdxTagMasks(doc ast.Node) [][2]int {
	var spans [][2]int
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *mdxJsxContainer:
			spans = append(spans, n.spans...)
		case *mdxInline:
			if n.form != 0 && n.span[1] > n.span[0] {
				spans = append(spans, n.span)
			}
		}
		return ast.WalkContinue, nil
	})
	return spans
}

// maskSpans blanks the spans of content, byte for byte, keeping newlines.
func maskSpans(content string, spans [][2]int) string {
	if len(spans) == 0 {
		return content
	}
	b := []byte(content)
	for _, s := range spans {
		for i := max(s[0], 0); i < s[1] && i < len(b); i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

// lintMDX lints MDX: Markdown, parsed with the MDX constructs.
func (l *Linter) lintMDX(f *core.File) error {
	return l.lintMarkdownWith(f, goldMdx)
}
