package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var md goldmark.Markdown

func init() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("friendly"),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(newAlertTransformer(), 100),
				util.Prioritized(&linkTransformer{}, 200),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(
				util.Prioritized(&alertRenderer{}, 100),
			),
		),
	)
}

// renderMarkdown renders a tracked markdown file, updating the file's state
func renderMarkdown(filePath string) error {
	// Look up the file state
	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		return fmt.Errorf("file not tracked: %s", filePath)
	}

	return renderInto(fileState)
}

// renderInto reads the markdown file a state describes and renders it to HTML,
// updating that state. It takes the state rather than looking it up by path so
// a file can be fully rendered before it is published to the tracked files,
// leaving no window in which a request can find a state without content.
func renderInto(fileState *FileState) error {
	// Read and render the file (without holding any locks)
	content, err := os.ReadFile(fileState.path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	doc := &documentContext{
		path: fileState.path,
		refs: newReferences(),
	}
	pc := parser.NewContext()
	pc.Set(documentContextKey, doc)

	var buf bytes.Buffer
	if err := md.Convert(content, &buf, parser.WithContext(pc)); err != nil {
		return fmt.Errorf("failed to convert markdown: %w", err)
	}

	// Update the HTML content with the file's lock
	fileState.contentLock.Lock()
	fileState.source = content
	fileState.htmlContent = buf.Bytes()
	fileState.refs = doc.refs
	fileState.contentLock.Unlock()

	return nil
}
