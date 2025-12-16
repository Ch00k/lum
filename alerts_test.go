package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubAlerts(t *testing.T) {
	t.Run("AllAlertTypes", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "alerts.md")

		content := `# Alert Types

> [!NOTE]
> This is a note alert.

> [!TIP]
> This is a tip alert.

> [!IMPORTANT]
> This is an important alert.

> [!WARNING]
> This is a warning alert.

> [!CAUTION]
> This is a caution alert.
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render alerts: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Check all alert types are present with correct CSS classes
		expectedClasses := []string{
			"markdown-alert markdown-alert-note",
			"markdown-alert markdown-alert-tip",
			"markdown-alert markdown-alert-important",
			"markdown-alert markdown-alert-warning",
			"markdown-alert markdown-alert-caution",
		}

		for _, class := range expectedClasses {
			if !strings.Contains(html, class) {
				t.Errorf("Missing alert class: %s", class)
			}
		}

		// Check alert titles are rendered
		expectedTitles := []string{
			"Note",
			"Tip",
			"Important",
			"Warning",
			"Caution",
		}

		for _, title := range expectedTitles {
			if !strings.Contains(html, title) {
				t.Errorf("Missing alert title: %s", title)
			}
		}

		// Check that SVG icons are present
		if !strings.Contains(html, "<svg") {
			t.Error("Missing SVG icons in alerts")
		}

		// Check title paragraphs have correct class
		if !strings.Contains(html, `class="markdown-alert-title"`) {
			t.Error("Missing markdown-alert-title class")
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("RegularBlockquoteUnaffected", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "blockquote.md")

		content := `# Regular Blockquote

> This is a regular blockquote.
> It should not be transformed into an alert.

> Another regular blockquote
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should have blockquotes but not alert classes
		if !strings.Contains(html, "<blockquote>") {
			t.Error("Missing blockquote tags")
		}

		if strings.Contains(html, "markdown-alert") {
			t.Error("Regular blockquotes should not have alert classes")
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("AlertWithMultipleParagraphs", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "multi-para.md")

		content := `# Multi-paragraph Alert

> [!NOTE]
> First paragraph of the note.
>
> Second paragraph with more details.
>
> Third paragraph for completeness.
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should have alert class
		if !strings.Contains(html, "markdown-alert-note") {
			t.Error("Missing note alert class")
		}

		// Should contain all paragraphs
		paragraphCount := strings.Count(html, "<p>")
		if paragraphCount < 3 {
			t.Errorf("Expected at least 3 paragraphs, found %d", paragraphCount)
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("InvalidAlertType", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "invalid.md")

		content := `# Invalid Alert

> [!INVALID]
> This should not be transformed.

> [!CUSTOM]
> Neither should this.
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should not have alert classes
		if strings.Contains(html, "markdown-alert-invalid") {
			t.Error("Invalid alert type should not be transformed")
		}

		if strings.Contains(html, "markdown-alert-custom") {
			t.Error("Custom alert type should not be transformed")
		}

		// Should still have blockquotes
		if !strings.Contains(html, "<blockquote>") {
			t.Error("Should still have blockquote tags")
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("CaseSensitivity", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "case.md")

		content := `# Case Sensitivity

> [!note]
> Lowercase should work.

> [!Note]
> Mixed case should work.

> [!NOTE]
> Uppercase should work.
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// All three should be transformed (case-insensitive)
		alertCount := strings.Count(html, "markdown-alert-note")
		if alertCount != 3 {
			t.Errorf("Expected 3 note alerts, found %d", alertCount)
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("AlertWithInlineCode", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "inline-code.md")

		content := `# Alert with Code

> [!NOTE]
> Use ` + "`sudo`" + ` to run this command.
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should have alert class
		if !strings.Contains(html, "markdown-alert-note") {
			t.Error("Missing note alert class")
		}

		// Should have inline code
		if !strings.Contains(html, "<code>") {
			t.Error("Missing inline code")
		}

		if !strings.Contains(html, "sudo") {
			t.Error("Missing code content")
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("MixedAlertsAndBlockquotes", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "mixed.md")

		content := `# Mixed Content

> Regular blockquote first

> [!NOTE]
> An alert in the middle

> Another regular blockquote

> [!WARNING]
> Another alert
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should have 2 alerts
		if !strings.Contains(html, "markdown-alert-note") {
			t.Error("Missing note alert")
		}
		if !strings.Contains(html, "markdown-alert-warning") {
			t.Error("Missing warning alert")
		}

		// Should have 4 total blockquotes (2 regular + 2 alerts)
		blockquoteCount := strings.Count(html, "<blockquote")
		if blockquoteCount != 4 {
			t.Errorf("Expected 4 blockquotes, found %d", blockquoteCount)
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("AlertIconsPresent", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "icons.md")

		content := `> [!NOTE]
> Note with icon

> [!TIP]
> Tip with icon

> [!IMPORTANT]
> Important with icon

> [!WARNING]
> Warning with icon

> [!CAUTION]
> Caution with icon
`

		if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		err := renderMarkdown(testFile)
		if err != nil {
			t.Fatalf("Failed to render: %v", err)
		}

		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		html := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Count SVG elements (should be 5, one per alert)
		svgCount := strings.Count(html, "<svg")
		if svgCount != 5 {
			t.Errorf("Expected 5 SVG icons, found %d", svgCount)
		}

		// Verify SVG attributes
		if !strings.Contains(html, `viewBox="0 0 24 24"`) {
			t.Error("SVG missing viewBox attribute")
		}

		if !strings.Contains(html, `stroke="currentColor"`) {
			t.Error("SVG missing stroke attribute")
		}

		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})
}

func TestAlertDump(t *testing.T) {
	alert := NewAlert("note")

	// Dump should not panic
	alert.Dump([]byte("test source"), 0)
	alert.Dump([]byte("test source"), 1)
	alert.Dump([]byte("test source"), 5)

	// Test with different alert types
	for _, alertType := range []string{"note", "tip", "important", "warning", "caution"} {
		a := NewAlert(alertType)
		a.Dump([]byte(""), 0)
	}
}

func TestAlertKind(t *testing.T) {
	alert := NewAlert("note")

	if alert.Kind() != KindAlert {
		t.Error("Alert.Kind() should return KindAlert")
	}
}
