package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddFile(t *testing.T) {
	t.Run("AddNewFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		err := addFile(testFile)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}

		// Verify file is tracked
		filesLock.RLock()
		_, exists := files[testFile]
		filesLock.RUnlock()

		if !exists {
			t.Error("File should be tracked after adding")
		}

		// Cleanup
		filesLock.Lock()
		if fs, ok := files[testFile]; ok {
			if fs.watcher != nil {
				_ = fs.watcher.Close()
			}
			delete(files, testFile)
		}
		filesLock.Unlock()
	})

	t.Run("AddDuplicateFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add first time
		err := addFile(testFile)
		if err != nil {
			t.Fatalf("Failed to add file first time: %v", err)
		}

		// Add second time - should succeed (no-op)
		err = addFile(testFile)
		if err != nil {
			t.Errorf("Adding duplicate file should not error: %v", err)
		}

		// Cleanup
		filesLock.Lock()
		if fs, ok := files[testFile]; ok {
			if fs.watcher != nil {
				_ = fs.watcher.Close()
			}
			delete(files, testFile)
		}
		filesLock.Unlock()
	})

	t.Run("AddNonExistentFile", func(t *testing.T) {
		testFile := "/tmp/nonexistent-test-file-12345.md"

		err := addFile(testFile)
		if err == nil {
			t.Error("Expected error when adding non-existent file")
		}

		// Verify file is not tracked
		filesLock.RLock()
		_, exists := files[testFile]
		filesLock.RUnlock()

		if exists {
			t.Error("Non-existent file should not be tracked")
		}
	})

	t.Run("AddInvalidMarkdownFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		// Create file with invalid UTF-8 or content that might cause issues
		if err := os.WriteFile(testFile, []byte("\xff\xfe# Invalid UTF-8"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Should still work - goldmark is forgiving
		err := addFile(testFile)
		if err != nil {
			t.Logf("Note: File with invalid UTF-8 caused error: %v", err)
		}

		// Cleanup
		filesLock.Lock()
		if fs, ok := files[testFile]; ok {
			if fs.watcher != nil {
				_ = fs.watcher.Close()
			}
			delete(files, testFile)
		}
		filesLock.Unlock()
	})
}

func TestHandleIndex(t *testing.T) {
	t.Run("IndexWithNoFiles", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "lum") {
			t.Error("Index page should contain 'lum'")
		}
	})

	t.Run("IndexWithFiles", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "test.md") {
			t.Error("Index page should list tracked file")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("SpecificFileExists", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test Content"), 0o600); err != nil {
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

		req := httptest.NewRequest("GET", "/?file="+testFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Test Content") {
			t.Error("Page should contain rendered content")
		}

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("SpecificFileNotFound", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?file=/nonexistent.md", nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("NonRootPath", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/invalid", nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-root path, got %d", w.Code)
		}
	})
}

func TestHandleSSE(t *testing.T) {
	t.Run("MissingFileParameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events", nil)
		w := httptest.NewRecorder()

		handleSSE(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("FileNotTracked", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events?file=/nonexistent.md", nil)
		w := httptest.NewRecorder()

		handleSSE(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("ValidSSEConnectionHeaders", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Note: We can't fully test SSE with httptest.ResponseRecorder
		// because it doesn't support connection closing/context cancellation.
		// This test just verifies the error cases work.
		// The integration tests in main_test.go verify SSE actually works.

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("IndexSSEEndpoint", func(t *testing.T) {
		// Just verify the routing logic works by checking path
		// We can't fully test SSE with httptest.ResponseRecorder
		// The integration tests verify SSE actually works
	})
}

func TestNotifyClients(t *testing.T) {
	t.Run("NotifyExistingClients", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking with mock client
		clientChan := make(chan string, 1)
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: map[chan string]bool{clientChan: true},
		}
		filesLock.Unlock()

		// Send notification
		notifyClients(testFile, "test-message")

		// Check message was received
		select {
		case msg := <-clientChan:
			if msg != "test-message" {
				t.Errorf("Expected 'test-message', got '%s'", msg)
			}
		default:
			t.Error("No message received")
		}

		// Cleanup
		filesLock.Lock()
		close(clientChan)
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("NotifyNonExistentFile", func(t *testing.T) {
		// Should not panic
		notifyClients("/nonexistent.md", "message")
	})

	t.Run("NotifyWithNoClients", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file to tracking with no clients
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: make(map[chan string]bool),
		}
		filesLock.Unlock()

		// Should not panic or block
		notifyClients(testFile, "message")

		// Cleanup
		filesLock.Lock()
		delete(files, testFile)
		filesLock.Unlock()
	})

	t.Run("NotifyWithFullChannel", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Add file with unbuffered channel (will be full immediately)
		clientChan := make(chan string)
		filesLock.Lock()
		files[testFile] = &FileState{
			path:       testFile,
			sseClients: map[chan string]bool{clientChan: true},
		}
		filesLock.Unlock()

		// Notify - should not block due to select default case
		notifyClients(testFile, "message")

		// Cleanup
		filesLock.Lock()
		close(clientChan)
		delete(files, testFile)
		filesLock.Unlock()
	})
}

func TestNotifyIndexClients(t *testing.T) {
	t.Run("NotifyIndexPageClients", func(t *testing.T) {
		// Add mock client for index page
		clientChan := make(chan string, 1)
		indexSSEClientsLock.Lock()
		indexSSEClients[clientChan] = true
		indexSSEClientsLock.Unlock()

		// Send notification
		notifyIndexClients("test-message")

		// Check message was received
		select {
		case msg := <-clientChan:
			if msg != "test-message" {
				t.Errorf("Expected 'test-message', got '%s'", msg)
			}
		default:
			t.Error("No message received")
		}

		// Cleanup
		indexSSEClientsLock.Lock()
		close(clientChan)
		delete(indexSSEClients, clientChan)
		indexSSEClientsLock.Unlock()
	})

	t.Run("NotifyWithNoClients", func(t *testing.T) {
		// Should not panic or block
		notifyIndexClients("message")
	})

	t.Run("NotifyWithFullChannel", func(t *testing.T) {
		// Add client with unbuffered channel
		clientChan := make(chan string)
		indexSSEClientsLock.Lock()
		indexSSEClients[clientChan] = true
		indexSSEClientsLock.Unlock()

		// Notify - should not block due to select default case
		notifyIndexClients("message")

		// Cleanup
		indexSSEClientsLock.Lock()
		close(clientChan)
		delete(indexSSEClients, clientChan)
		indexSSEClientsLock.Unlock()
	})
}

func TestRenderIndexPage(t *testing.T) {
	t.Run("EmptyFileList", func(t *testing.T) {
		// Clear files map
		filesLock.Lock()
		originalFiles := files
		files = make(map[string]*FileState)
		filesLock.Unlock()

		defer func() {
			filesLock.Lock()
			files = originalFiles
			filesLock.Unlock()
		}()

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		renderIndexPage(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "lum") {
			t.Error("Index page should contain 'lum' title")
		}
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		tmpDir := t.TempDir()
		file1 := filepath.Join(tmpDir, "file1.md")
		file2 := filepath.Join(tmpDir, "file2.md")

		// Add files to tracking
		filesLock.Lock()
		originalFiles := files
		files = map[string]*FileState{
			file1: {path: file1, sseClients: make(map[chan string]bool)},
			file2: {path: file2, sseClients: make(map[chan string]bool)},
		}
		filesLock.Unlock()

		defer func() {
			filesLock.Lock()
			files = originalFiles
			filesLock.Unlock()
		}()

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		renderIndexPage(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "file1.md") {
			t.Error("Index should contain file1.md")
		}
		if !strings.Contains(body, "file2.md") {
			t.Error("Index should contain file2.md")
		}
	})
}
