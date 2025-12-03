package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
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
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)
}

// renderMarkdown reads a markdown file and renders it to HTML, updating the file's state
func renderMarkdown(filePath string) error {
	// Look up the file state
	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		return fmt.Errorf("file not tracked: %s", filePath)
	}

	// Read and render the file (without holding any locks)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var buf bytes.Buffer
	if err := md.Convert(content, &buf); err != nil {
		return fmt.Errorf("failed to convert markdown: %w", err)
	}

	// Update the HTML content with the file's lock
	fileState.contentLock.Lock()
	fileState.htmlContent = buf.Bytes()
	fileState.contentLock.Unlock()

	return nil
}
