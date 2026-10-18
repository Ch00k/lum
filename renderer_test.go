package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// renderContent renders content as a tracked document and returns the rendered
// HTML together with the document's mermaid flag.
func renderContent(t *testing.T, content string) (string, bool) {
	t.Helper()

	fileState := trackDocument(t, filepath.Join(t.TempDir(), "doc.md"), content)

	fileState.contentLock.RLock()
	defer fileState.contentLock.RUnlock()

	return string(fileState.htmlContent), fileState.hasMermaid
}

// TestMermaidRendering checks that mermaid fenced code blocks render as
// <pre class="mermaid"> elements holding the escaped diagram source, wherever
// they are nested, and that the document's mermaid flag tracks their presence.
func TestMermaidRendering(t *testing.T) {
	t.Run("RendersAsMermaidPreBlock", func(t *testing.T) {
		content := "# Title\n\n```mermaid\ngraph TD\n    A --> B\n```\n"

		html, hasMermaid := renderContent(t, content)

		if !hasMermaid {
			t.Error("hasMermaid should be true when a mermaid block is present")
		}
		if !strings.Contains(html, `<pre class="mermaid">graph TD`) {
			t.Errorf("Expected a mermaid pre block, got:\n%s", html)
		}
		// The mermaid block must not be handed to the syntax highlighter, which
		// wraps code in inline-styled spans.
		if strings.Contains(html, `<pre class="mermaid"><span`) {
			t.Error("Mermaid block should not be syntax-highlighted")
		}
	})

	t.Run("EscapesHTMLInSource", func(t *testing.T) {
		content := "```mermaid\ngraph TD\n    A -->|\"a < b & c\"| B\n```\n"

		html, _ := renderContent(t, content)

		if !strings.Contains(html, "a &lt; b &amp; c") {
			t.Errorf("Mermaid source should be HTML-escaped, got:\n%s", html)
		}
		if strings.Contains(html, "a < b & c") {
			t.Error("Raw unescaped angle bracket/ampersand leaked into HTML")
		}
	})

	t.Run("LanguageMatchIsCaseInsensitive", func(t *testing.T) {
		content := "```Mermaid\ngraph TD\n    A --> B\n```\n"

		html, hasMermaid := renderContent(t, content)

		if !hasMermaid || !strings.Contains(html, `<pre class="mermaid">`) {
			t.Errorf("Uppercase language token should still be treated as mermaid, got:\n%s", html)
		}
	})

	t.Run("RendersInsideNestedBlocks", func(t *testing.T) {
		content := "> [!NOTE]\n> ```mermaid\n> graph TD\n>     A --> B\n> ```\n\n" +
			"- item\n\n  ```mermaid\n  graph LR\n      C --> D\n  ```\n"

		html, hasMermaid := renderContent(t, content)

		if !hasMermaid {
			t.Error("hasMermaid should be true for nested mermaid blocks")
		}
		if strings.Count(html, `<pre class="mermaid">`) != 2 {
			t.Errorf("Expected mermaid blocks inside both the alert and the list item, got:\n%s", html)
		}
	})

	t.Run("NonMermaidCodeStillHighlighted", func(t *testing.T) {
		content := "```go\nfunc main() {}\n```\n"

		html, hasMermaid := renderContent(t, content)

		if hasMermaid {
			t.Error("hasMermaid should be false without a mermaid block")
		}
		if strings.Contains(html, `class="mermaid"`) {
			t.Error("A go code block must not be rendered as mermaid")
		}
		// goldmark-highlighting emits inline styles for highlighted code.
		if !strings.Contains(html, "<pre") || !strings.Contains(html, "style=") {
			t.Errorf("Go code block should be syntax-highlighted, got:\n%s", html)
		}
	})
}

func TestRenderMarkdownEdgeCases(t *testing.T) {
	t.Run("EmptyFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "empty.md")

		if err := os.WriteFile(testFile, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render
		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render empty file: %v", err)
		}

		// Check content
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		_ = fileState.htmlContent
		fileState.contentLock.RUnlock()

		// Empty file produces empty byte slice, not nil
		// This is fine - just verify no error occurred

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("LargeFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "large.md")

		// Create a large markdown file
		var content strings.Builder
		for i := 0; i < 1000; i++ {
			content.WriteString("# Heading ")
			content.WriteString(string(rune('0' + (i % 10))))
			content.WriteString("\n\nSome paragraph text with **bold** and *italic*.\n\n")
		}

		if err := os.WriteFile(testFile, []byte(content.String()), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render
		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render large file: %v", err)
		}

		// Check content
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		htmlContent := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		if len(htmlContent) == 0 {
			t.Error("Rendered content should not be empty")
		}

		if !strings.Contains(htmlContent, "<h1") {
			t.Error("Should contain heading tags")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("ComplexMarkdown", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "complex.md")

		content := `# Main Heading

## Subheading

This is a paragraph with **bold**, *italic*, and ***bold italic***.

### Lists

- Item 1
- Item 2
  - Nested item
  - Another nested

1. Numbered
2. List

### Code

Inline ` + "`code`" + ` here.

` + "```go\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n```" + `

### Links and Images

[Link text](https://example.com)

### Tables

| Col1 | Col2 |
|------|------|
| A    | B    |

### Blockquotes

> This is a quote
> spanning multiple lines

### Task Lists

- [x] Done
- [ ] Not done
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render
		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render complex markdown: %v", err)
		}

		// Check content has various elements
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		checks := []string{
			"<h1",
			"<h2",
			"<h3",
			"<strong>",
			"<em>",
			"<ul>",
			"<ol>",
			"<code",
			"<a ",
			"<table>",
			"<blockquote>",
		}

		for _, check := range checks {
			if !strings.Contains(html, check) {
				t.Errorf("Rendered HTML missing: %s", check)
			}
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("FileNotTracked", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Don't add to tracking
		err := renderMarkdown(testFile)
		if err == nil {
			t.Error("Expected error when rendering untracked file")
		}

		expectedError := fmt.Sprintf("file not tracked: %s", testFile)
		if err.Error() != expectedError {
			t.Errorf("Expected error:\n%q\nGot:\n%q", expectedError, err.Error())
		}
	})

	t.Run("FileDisappearsAfterTracking", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Remove the file
		if err := os.Remove(testFile); err != nil {
			t.Fatal(err)
		}

		// Try to render - should error
		err := renderMarkdown(testFile)
		if err == nil {
			t.Error("Expected error when file is missing")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("UnreadableFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "unreadable.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Make file unreadable
		if err := os.Chmod(testFile, 0o000); err != nil {
			t.Fatal(err)
		}

		// Restore permissions for cleanup
		defer func() {
			if err := os.Chmod(testFile, 0o600); err != nil {
				t.Logf("Failed to restore file permissions: %v", err)
			}
		}()

		// Try to render - should error
		err := renderMarkdown(testFile)
		if err == nil {
			t.Error("Expected error when file is unreadable")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("SpecialCharactersInContent", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "special.md")

		content := `# Special Characters

< > & " '

HTML entities: &lt; &gt; &amp;

Unicode: 你好 🌍 émojis

Math: x < y > z
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render
		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render file with special characters: %v", err)
		}

		// Check content
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		if len(html) == 0 {
			t.Error("Rendered content should not be empty")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("SyntaxHighlighting", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "code.md")

		content := "# Code Examples\n\n```go\npackage main\n\nfunc main() {\n\tfmt.Println(\"test\")\n}\n```\n"

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render
		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		// Check that syntax highlighting is applied
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should contain code-related HTML
		if !strings.Contains(html, "<code") {
			t.Error("Should contain code tags")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})
}
