package main

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Alert is an inline node for GitHub-style alerts
type Alert struct {
	ast.BaseInline
	AlertType string
}

// Dump implements Node.Dump
func (n *Alert) Dump(source []byte, level int) {
	m := map[string]string{
		"AlertType": n.AlertType,
	}
	ast.DumpHelper(n, source, level, m, nil)
}

// KindAlert is the NodeKind for Alert
var KindAlert = ast.NewNodeKind("Alert")

// Kind implements Node.Kind
func (n *Alert) Kind() ast.NodeKind {
	return KindAlert
}

// NewAlert returns a new Alert node
func NewAlert(alertType string) *Alert {
	return &Alert{
		BaseInline: ast.BaseInline{},
		AlertType:  alertType,
	}
}

// alertTransformer is an AST transformer that converts blockquotes
// with [!NOTE] syntax into styled alert blocks
type alertTransformer struct {
	alertTypes map[string]bool
}

// newAlertTransformer creates a new alert transformer
func newAlertTransformer() *alertTransformer {
	return &alertTransformer{
		alertTypes: map[string]bool{
			"note":      true,
			"tip":       true,
			"important": true,
			"warning":   true,
			"caution":   true,
		},
	}
}

// Transform implements parser.ASTTransformer
func (t *alertTransformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		blockquote, ok := n.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}

		return t.transformBlockquote(blockquote, reader)
	})
}

// transformBlockquote checks if a blockquote is an alert and transforms it
func (t *alertTransformer) transformBlockquote(v *ast.Blockquote, reader text.Reader) (ast.WalkStatus, error) {
	// Get the first paragraph in the blockquote
	firstParagraph := v.FirstChild()
	if firstParagraph == nil {
		return ast.WalkContinue, nil
	}

	// Try to extract alert pattern: [!TYPE]
	alertType, nodesToRemove := t.extractAlertPattern(firstParagraph, reader)
	if alertType == "" {
		return ast.WalkContinue, nil
	}

	// Add CSS class to the blockquote
	v.SetAttributeString("class", []byte("markdown-alert markdown-alert-"+alertType))

	// Create a new paragraph for the alert title
	titleParagraph := ast.NewParagraph()
	titleParagraph.SetAttributeString("class", []byte("markdown-alert-title"))

	// Add the alert icon (as a custom node)
	titleParagraph.AppendChild(titleParagraph, NewAlert(alertType))

	// Add the alert type text (capitalized)
	typeText := strings.ToUpper(string(alertType[0])) + alertType[1:]
	titleParagraph.AppendChild(titleParagraph, ast.NewString([]byte(typeText)))

	// Insert the title paragraph before the first paragraph
	firstParagraph.Parent().InsertBefore(firstParagraph.Parent(), firstParagraph, titleParagraph)

	// Remove the [!TYPE] nodes from the first paragraph
	for _, node := range nodesToRemove {
		firstParagraph.RemoveChild(firstParagraph, node)
	}

	// If the first paragraph is now empty, remove it
	if firstParagraph.ChildCount() == 0 {
		firstParagraph.Parent().RemoveChild(firstParagraph.Parent(), firstParagraph)
	}

	return ast.WalkContinue, nil
}

// extractAlertPattern extracts the alert type from pattern 3: [!TYPE]
// Returns the alert type and nodes to remove
func (t *alertTransformer) extractAlertPattern(firstParagraph ast.Node, reader text.Reader) (string, []ast.Node) {
	if firstParagraph.ChildCount() < 3 {
		return "", nil
	}

	// Check for pattern 3: Text("[") Text("!TYPE") Text("]")
	node1, ok := firstParagraph.FirstChild().(*ast.Text)
	if !ok {
		return "", nil
	}
	node2, ok := node1.NextSibling().(*ast.Text)
	if !ok {
		return "", nil
	}
	node3, ok := node2.NextSibling().(*ast.Text)
	if !ok {
		return "", nil
	}

	val1 := string(node1.Segment.Value(reader.Source()))
	val2 := string(node2.Segment.Value(reader.Source()))
	val3 := string(node3.Segment.Value(reader.Source()))

	if val1 != "[" || val3 != "]" || !strings.HasPrefix(val2, "!") {
		return "", nil
	}

	alertType := strings.ToLower(val2[1:])
	if !t.alertTypes[alertType] {
		return "", nil
	}

	return alertType, []ast.Node{node1, node2, node3}
}

// alertRenderer renders Alert nodes as SVG icons
type alertRenderer struct {
	html.Config
}

// RegisterFuncs implements renderer.NodeRenderer
func (r *alertRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindAlert, r.renderAlert)
}

// renderAlert renders an Alert node as an SVG icon
func (r *alertRenderer) renderAlert(
	w util.BufWriter,
	source []byte,
	node ast.Node,
	entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*Alert)
	var svg string

	switch n.AlertType {
	case "note":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`
	case "tip":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/></svg>`
	case "important":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z"/><path d="M12 15h.01"/><path d="M12 7v4"/></svg>`
	case "warning":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/></svg>`
	case "caution":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 16h.01"/><path d="M12 8v4"/><path d="M15.312 2a2 2 0 0 1 1.414.586l4.688 4.688A2 2 0 0 1 22 8.688v6.624a2 2 0 0 1-.586 1.414l-4.688 4.688a2 2 0 0 1-1.414.586H8.688a2 2 0 0 1-1.414-.586l-4.688-4.688A2 2 0 0 1 2 15.312V8.688a2 2 0 0 1 .586-1.414l4.688-4.688A2 2 0 0 1 8.688 2z"/></svg>`
	default:
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>`
	}

	_, _ = w.WriteString(svg)
	return ast.WalkContinue, nil
}
