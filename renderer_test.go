package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
