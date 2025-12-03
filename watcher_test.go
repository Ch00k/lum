package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartWatchingFile(t *testing.T) {
	t.Run("WatchValidFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Initial"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Render the file first
		if err := renderMarkdown(testFile); err != nil {
			t.Fatal(err)
		}

		// Start watching
		err := startWatchingFile(testFile)
		if err != nil {
			t.Fatalf("Failed to start watching: %v", err)
		}

		// Verify watcher is set
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		if fileState.watcher == nil {
			t.Error("Watcher should be set")
		}

		// Modify file and verify it triggers a re-render
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(testFile, []byte("# Updated"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Give watcher time to process
		time.Sleep(500 * time.Millisecond)

		// Verify content was updated
		fileState.contentLock.RLock()
		content := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		if !contains(content, "Updated") {
			t.Error("Content should have been updated after file change")
		}

		// Cleanup
		filesLock.Lock()
		if fileState.watcher != nil {
			_ = fileState.watcher.Close()
		}
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("WatchFileNotInTracking", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Don't add to tracking
		err := startWatchingFile(testFile)
		if err == nil {
			t.Error("Expected error when file not in tracking")
		}
	})

	t.Run("WatchNonExistentFile", func(t *testing.T) {
		testFile := "/nonexistent-dir-12345/nonexistent-watch-file.md"

		// Add to tracking (even though file doesn't exist)
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Try to watch - should fail when trying to watch non-existent directory
		err := startWatchingFile(testFile)
		if err == nil {
			t.Error("Expected error when watching file in non-existent directory")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("FileAtomicSave", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Initial"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking and render
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		if err := renderMarkdown(testFile); err != nil {
			t.Fatal(err)
		}

		// Start watching
		if err := startWatchingFile(testFile); err != nil {
			t.Fatal(err)
		}

		time.Sleep(200 * time.Millisecond)

		// Simulate atomic save: remove and recreate file
		if err := os.Remove(testFile); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(testFile, []byte("# After Atomic Save"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Give watcher time to process (retry logic should handle this)
		time.Sleep(800 * time.Millisecond)

		// Verify content was eventually updated
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		content := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		if !contains(content, "After Atomic Save") {
			t.Error("Content should be updated after atomic save")
		}

		// Cleanup
		filesLock.Lock()
		if fileState.watcher != nil {
			_ = fileState.watcher.Close()
		}
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("RapidFileChanges", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Initial"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking and render
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		if err := renderMarkdown(testFile); err != nil {
			t.Fatal(err)
		}

		// Start watching
		if err := startWatchingFile(testFile); err != nil {
			t.Fatal(err)
		}

		time.Sleep(200 * time.Millisecond)

		// Make rapid changes (should be debounced)
		for i := 0; i < 5; i++ {
			content := []byte("# Rapid change " + string(rune('0'+i)))
			if err := os.WriteFile(testFile, content, 0o600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}

		// Give time for debouncing to settle and final render
		time.Sleep(500 * time.Millisecond)

		// Should have the final content
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		content := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Should contain some version of "Rapid change"
		if !contains(content, "Rapid change") {
			t.Error("Content should reflect rapid changes")
		}

		// Cleanup
		filesLock.Lock()
		if fileState.watcher != nil {
			_ = fileState.watcher.Close()
		}
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("NotifyClientsOnChange", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Initial"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file with mock client
		clientChan := make(chan string, 10)
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: map[chan string]bool{clientChan: true},
		}
		filesLock.Unlock()

		if err := renderMarkdown(testFile); err != nil {
			t.Fatal(err)
		}

		// Start watching
		if err := startWatchingFile(testFile); err != nil {
			t.Fatal(err)
		}

		time.Sleep(200 * time.Millisecond)

		// Modify file
		if err := os.WriteFile(testFile, []byte("# Changed"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Should receive "reload" message
		select {
		case msg := <-clientChan:
			if msg != "reload" {
				t.Errorf("Expected 'reload' message, got '%s'", msg)
			}
		case <-time.After(1 * time.Second):
			t.Error("No reload message received")
		}

		// Cleanup
		filesLock.Lock()
		fileState := files[testFile]
		if fileState.watcher != nil {
			_ = fileState.watcher.Close()
		}
		close(clientChan)
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("WatcherIgnoresOtherFiles", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")
		otherFile := filepath.Join(tmpDir, "other.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(otherFile, []byte("# Other"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		if err := renderMarkdown(testFile); err != nil {
			t.Fatal(err)
		}

		// Start watching
		if err := startWatchingFile(testFile); err != nil {
			t.Fatal(err)
		}

		time.Sleep(200 * time.Millisecond)

		// Get initial content
		filesLock.RLock()
		fileState := files[testFile]
		filesLock.RUnlock()

		fileState.contentLock.RLock()
		initialContent := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		// Modify OTHER file
		if err := os.WriteFile(otherFile, []byte("# Other Modified"), 0o600); err != nil {
			t.Fatal(err)
		}

		time.Sleep(300 * time.Millisecond)

		// Our file's content should NOT have changed
		fileState.contentLock.RLock()
		currentContent := string(fileState.htmlContent)
		fileState.contentLock.RUnlock()

		if currentContent != initialContent {
			t.Error("Content should not change when other files are modified")
		}

		// Cleanup
		filesLock.Lock()
		if fileState.watcher != nil {
			_ = fileState.watcher.Close()
		}
		delete(files, testFile)
		filesLock.Unlock()
	})
}
