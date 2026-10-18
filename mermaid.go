package main

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Mermaid is a block node holding the source of a mermaid diagram
type Mermaid struct {
	ast.BaseBlock
	Source []byte
}

// Dump implements Node.Dump
func (n *Mermaid) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

// KindMermaid is the NodeKind for Mermaid
var KindMermaid = ast.NewNodeKind("Mermaid")

// Kind implements Node.Kind
func (n *Mermaid) Kind() ast.NodeKind {
	return KindMermaid
}

// NewMermaid returns a new Mermaid node
func NewMermaid(source []byte) *Mermaid {
	return &Mermaid{Source: source}
}

// mermaidTransformer replaces ```mermaid fenced code blocks with Mermaid nodes
// so the syntax highlighter never renders them as highlighted code.
type mermaidTransformer struct{}

// Transform implements parser.ASTTransformer
func (t *mermaidTransformer) Transform(node *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()

	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		block, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}

		if lang := block.Language(source); lang != nil && strings.EqualFold(string(lang), "mermaid") {
			blocks = append(blocks, block)
		}

		return ast.WalkContinue, nil
	})

	for _, block := range blocks {
		mermaid := NewMermaid(codeBlockValue(block, source))
		block.Parent().ReplaceChild(block.Parent(), block, mermaid)
	}
}

// codeBlockValue extracts the raw text content of a fenced code block
func codeBlockValue(block *ast.FencedCodeBlock, source []byte) []byte {
	var buf bytes.Buffer

	lines := block.Lines()
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		buf.Write(segment.Value(source))
	}

	return buf.Bytes()
}

// mermaidRenderer renders Mermaid nodes as <pre class="mermaid"> blocks that the
// mermaid runtime turns into diagrams in the browser.
type mermaidRenderer struct{}

// RegisterFuncs implements renderer.NodeRenderer
func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMermaid, r.renderMermaid)
}

// renderMermaid writes the diagram source into a <pre class="mermaid"> element
func (r *mermaidRenderer) renderMermaid(
	w util.BufWriter,
	_ []byte,
	node ast.Node,
	entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*Mermaid)

	_, _ = w.WriteString(`<pre class="mermaid">`)
	_, _ = w.Write(util.EscapeHTML(n.Source))
	_, _ = w.WriteString("</pre>\n")

	return ast.WalkContinue, nil
}
