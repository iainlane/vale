package lint

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark/text"
)

// TestMdxHTML pins what the MDX extension makes of each construct: ESM,
// expressions, tags, and self-closing elements are code the walker skips,
// while an element's children stay Markdown -- a flow element is a div and
// an inline element a span, each classed with the element's name. A
// `{/* ... */}` flow comment is an HTML comment so comment-based
// configuration works, and everything else is ordinary Markdown.
func TestMdxHTML(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   []string
		absent []string
	}{
		{
			"contiguous ESM is one skipped block",
			"import {A} from './a.js'\nimport b from './b.js'\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxjsEsm">`, "<p>Prose here.</p>"},
			[]string{"<p>import"},
		},
		{
			"a multiline export ends when its braces do",
			"export function A() {\n  return 1\n}\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxjsEsm">`, "<p>Prose here.</p>"},
			[]string{"<p>export"},
		},
		{
			"an arrow export with JSX and strings",
			"export const L = p => <span style={{color: 'red'}} {...p} />\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxjsEsm">`, "<p>Prose here.</p>"},
			[]string{"<p>export"},
		},
		{
			"a flow comment is a comment",
			"{/* vale off */}\n\nProse here.\n",
			[]string{"<!-- vale off -->", "<p>Prose here.</p>"},
			[]string{"mdxNode"},
		},
		{
			"a comment followed by prose is a paragraph",
			"{/* note */}Thiss paragraph.\n",
			[]string{"<p><!-- note -->Thiss paragraph.</p>"},
			[]string{"mdxFlowExpression"},
		},
		{
			"an expression ends at its brace, whatever follows",
			"{/* note */}See [the docs](https://example.com/docs) now.\n\nProse here.\n",
			[]string{`<a href="https://example.com/docs">`, "<p>Prose here.</p>"},
			[]string{"mdxFlowExpression"},
		},
		{
			"a fence indented under a component is code",
			"<Steps>\n  <Step>\n    Run:\n\n    ```bash\n    npm install\n\n    npx it\n    ```\n\n    Prose here.\n  </Step>\n</Steps>\n",
			[]string{`<pre><code class="language-bash">`, "<p>Prose here.</p>"},
			[]string{"<p>npm", "<p>npx"},
		},
		{
			"blocks indented four spaces are not code",
			"    ## Setup\n\n    - one\n    - two\n\n    > quoted\n    > more\n",
			[]string{"<h2>Setup</h2>", "<li>one</li>", "<li>two</li>", "<blockquote>\n<p>quoted\nmore</p>"},
			[]string{"<pre>", "<li>one\n<ul>"},
		},
		{
			"a flow expression is skipped",
			"{(function () {\n  return 'a { in a string'\n})()}\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxFlowExpression">`, "<p>Prose here.</p>"},
			[]string{"<p>{("},
		},
		{
			"a self-closing element with expression attributes",
			"<Chart data={population} label={'a > b'} />\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxJsxFlowElement">`, "<p>Prose here.</p>"},
			[]string{"<p><Chart", "<chart"},
		},
		{
			"an element's children are prose in a classed div",
			"<div className=\"note\">\n  > Some notable things!\n</div>\n\nProse here.\n",
			[]string{`<div class="div">`, "<blockquote>", "</div>", "<p>Prose here.</p>"},
			[]string{"mdxNode", "note"},
		},
		{
			"multiline attributes",
			"<Component\n  open\n  x={1}\n  icon={<Icon />}\n/>\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxJsxFlowElement">`, "<p>Prose here.</p>"},
			[]string{"<p><Component"},
		},
		{
			"inline JSX children stay prose",
			"An <External>XXX</External> component TODO.\n",
			[]string{`An <span class="External">XXX</span> component TODO.`},
			[]string{"mdxNode"},
		},
		{
			"a member-expression component",
			"Use the <myComponents.thisOne /> component.\n",
			[]string{`<code class="mdxNode mdxJsxTextElement">`},
			[]string{"<mycomponents.thisone"},
		},
		{
			"an inline expression is a code span",
			"Two is: {Math.PI * 2}, TODO\n",
			[]string{`Two is: <code class="mdxNode mdxTextExpression">{Math.PI * 2}</code>, TODO`},
			nil,
		},
		{
			"a heading attribute is a code span",
			"## Some Markdown {#initial-setup}\n",
			[]string{"Some Markdown", `<code class="mdxNode mdxTextExpression">{#initial-setup}</code>`},
			nil,
		},
		{
			"autolinks are still links",
			"See <https://example.com/config> for details.\n",
			[]string{`<a href="https://example.com/config">`},
			[]string{"mdxNode"},
		},
		{
			"indented lines are a paragraph, not code",
			"Intro.\n\n    XXX = False\n    more here\n",
			[]string{"<p>XXX = False\nmore here</p>"},
			[]string{"<pre><code>XXX"},
		},
		{
			"a fragment's children are prose",
			"<>\nInside a fragment.\n</>\n\nProse here.\n",
			[]string{"<div>\n<p>Inside a fragment.</p>\n</div>", "<p>Prose here.</p>"},
			[]string{"mdxNode"},
		},
		{
			"a nested same-name element on one line stays inside its parent",
			"<Box>\n  <Box>inner</Box>\n</Box>\n\nProse here.\n",
			[]string{"<div class=\"Box\">\n<p><span class=\"Box\">inner</span></p>\n</div>", "<p>Prose here.</p>"},
			[]string{"mdxNode"},
		},
		{
			"an element on one line with text is a paragraph",
			"<Tip>This is a tip</Tip>\n\n<Badge color=\"orange\">Deprecated</Badge> since v2.\n",
			[]string{`<p><span class="Tip">This is a tip</span></p>`, `<p><span class="Badge">Deprecated</span> since v2.</p>`},
			[]string{"mdxJsxFlowElement"},
		},
		{
			"an element on one line without text is code",
			"<video src=\"a.mp4\"></video>\n\n<Box><Chart data={x} /></Box>\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxJsxFlowElement">&lt;video`, `<code class="mdxNode mdxJsxFlowElement">&lt;Box&gt;`, "<p>Prose here.</p>"},
			[]string{"<p>&lt;"},
		},
		{
			"nested same-name elements close in order",
			"<Box>\n\nOuter start.\n\n<Box>\n\nInner text.\n\n</Box>\n\nOuter end.\n\n</Box>\n\nProse here.\n",
			[]string{"<p>Outer start.</p>", "<p>Inner text.</p>", "<p>Outer end.</p>", "<p>Prose here.</p>"},
			[]string{"mdxNode"},
		},
		{
			"a container's attributes are not prose",
			"<AstroAside type=\"info\" x={a > b}>\n  Renders inside a colored box.\n</AstroAside>\n\nProse here.\n",
			[]string{`<div class="AstroAside">`, "<p>Renders inside a colored box.</p>", "<p>Prose here.</p>"},
			[]string{"info", "a &gt; b"},
		},
		{
			"markdown children keep their structure",
			"<Steps>\n\n1. First point here.\n\n2. Second point here.\n\n</Steps>\n\nProse here.\n",
			[]string{`<div class="Steps">`, "<ol>", "<li>", "First point here.", "<p>Prose here.</p>"},
			[]string{"mdxNode"},
		},
		{
			"a JSX tag quoted in a fence doesn't close the element",
			"<Steps>\n\n```\n</Steps>\n```\n\nStill inside.\n\n</Steps>\n\nProse here.\n",
			[]string{"<p>Still inside.</p>", "<p>Prose here.</p>"},
			[]string{`<p>&lt;/Steps&gt;`},
		},
		{
			"a member-expression container's dots become hyphens",
			"<my.Component>\n\nText here.\n\n</my.Component>\n\nProse here.\n",
			[]string{`<div class="my-Component">`, "<p>Text here.</p>", "<p>Prose here.</p>"},
			[]string{"my.Component<"},
		},
		{
			"an escaped quote inside an attribute expression",
			"<Table rows={[{d: 'the cluster\\'s version', e: \"a \\\"quoted\\\" word\"}]} />\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxJsxFlowElement">`, "<p>Prose here.</p>"},
			[]string{"<p>Prose here.</p></code>"},
		},
		{
			"an export whose template literal spans lines",
			"export const c = `a(\n  \\`b\\`, {x}\n)`;\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxjsEsm">`, "<p>Prose here.</p>"},
			[]string{"<p>export", "<p>)`"},
		},
		{
			"a fragment with an apostrophe inside an attribute expression",
			"<C\n  title={<><Code>\n    X\n  </Code> d'env (note)</>\n  }\n>\n  Body here.\n</C>\n\nProse here.\n",
			[]string{"<p>Body here.</p>", "<p>Prose here.</p>"},
			[]string{"<p>X", "<p>d'env"},
		},
		{
			"JSX text inside a flow expression",
			"{\n\n<table><tr><td>(per month) don't</td></tr></table>\n\n}\n\nProse here.\n",
			[]string{`<pre><code class="mdxNode mdxFlowExpression">`, "<p>Prose here.</p>"},
			[]string{"<p>(per", "<p>}"},
		},
		{
			"JSX returned from a function inside an expression",
			"{(() => { return <b>don't</b> })()}\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxFlowExpression">`, "<p>Prose here.</p>"},
			[]string{"<p>{("},
		},
		{
			"a type argument inside an expression is not JSX",
			"{useState<string>('x')}\n\nProse here.\n",
			[]string{`<code class="mdxNode mdxFlowExpression">`, "<p>Prose here.</p>"},
			[]string{"<p>{use"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := goldMdx.Convert([]byte(c.in), &buf); err != nil {
				t.Fatal(err)
			}
			html := buf.String()

			for _, want := range c.want {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q in:\n%s", want, html)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(html, absent) {
					t.Errorf("unwanted %q in:\n%s", absent, html)
				}
			}
		})
	}
}

// TestMdxTagMasks pins what is blanked out of the search context: the
// tags of JSX elements, whose attributes the walker never sees. Code the
// walker does see -- ESM, expressions, fences -- is left for it, and the
// prose and comments are never touched.
func TestMdxTagMasks(t *testing.T) {
	src := "import A from './a'\n\n<Update rss={{ title:\"Same words\" }}>\n\n## Same words\n\n</Update>\n\n{/* vale off */}\n\nA <Badge color=\"red\">tag</Badge> and {props.x} here.\n\n```js\nconst y = 1\n```\n"
	doc := goldMdx.Parser().Parse(text.NewReader([]byte(src)))
	got := maskSpans(src, mdxTagMasks(doc))

	for _, absent := range []string{"rss=", "title:", "</Update>", "color=", "</Badge>"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q survived masking:\n%s", absent, got)
		}
	}
	for _, present := range []string{"import A", "## Same words", "{/* vale off */}", "A ", "tag", " and ", "{props.x}", " here.", "const y"} {
		if !strings.Contains(got, present) {
			t.Errorf("%q was masked:\n%s", present, got)
		}
	}
	if len(got) != len(src) || strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Errorf("masking changed the layout: %d bytes vs %d", len(got), len(src))
	}
}
