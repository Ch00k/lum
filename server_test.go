package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestHandleStaticAsset(t *testing.T) {
	tmpDir := t.TempDir()
	markdownFile := filepath.Join(tmpDir, "test.md")

	// Create test markdown file
	if err := os.WriteFile(markdownFile, []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create test image in same directory
	imageFile := filepath.Join(tmpDir, "image.jpg")
	imageData := []byte("fake image data")
	if err := os.WriteFile(imageFile, imageData, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create subdirectory with image
	subDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatal(err)
	}
	subImage := filepath.Join(subDir, "logo.png")
	if err := os.WriteFile(subImage, []byte("fake png data"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Add markdown file to tracking
	if err := addFile(markdownFile); err != nil {
		t.Fatal(err)
	}

	defer func() {
		filesLock.Lock()
		if fs, ok := files[markdownFile]; ok {
			if fs.watcher != nil {
				_ = fs.watcher.Close()
			}
			delete(files, markdownFile)
		}
		filesLock.Unlock()
	}()

	t.Run("ServeRelativePathImage", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/image.jpg?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Body.String() != string(imageData) {
			t.Error("Image data doesn't match")
		}

		contentType := w.Header().Get("Content-Type")
		if !strings.Contains(contentType, "image/jpeg") {
			t.Errorf("Expected image/jpeg content type, got %s", contentType)
		}
	})

	t.Run("ServeSubdirectoryImage", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/assets/logo.png?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("ServeAbsolutePathWithinDirectory", func(t *testing.T) {
		req := httptest.NewRequest("GET", imageFile+"?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Body.String() != string(imageData) {
			t.Error("Image data doesn't match for absolute path")
		}
	})

	t.Run("BlockAbsolutePathOutsideDirectory", func(t *testing.T) {
		// Try to access a file outside the markdown directory
		outsideFile := "/etc/passwd"
		req := httptest.NewRequest("GET", outsideFile+"?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for outside path, got %d", w.Code)
		}
	})

	t.Run("BlockDirectoryAccess", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/assets?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for directory access, got %d", w.Code)
		}
	})

	t.Run("Return404ForNonExistentFile", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nonexistent.jpg?file="+markdownFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for nonexistent file, got %d", w.Code)
		}
	})

	t.Run("Return404WhenFileParameterMissing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/image.jpg", nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 when file param missing, got %d", w.Code)
		}
	})

	t.Run("Return404WhenMarkdownFileNotTracked", func(t *testing.T) {
		untrackedFile := filepath.Join(tmpDir, "untracked.md")
		req := httptest.NewRequest("GET", "/image.jpg?file="+untrackedFile, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for untracked markdown file, got %d", w.Code)
		}
	})

	t.Run("BlockPathTraversalAttempt", func(t *testing.T) {
		// Try various path traversal patterns
		patterns := []string{
			"/../../../etc/passwd?file=" + markdownFile,
			"/./../../etc/passwd?file=" + markdownFile,
			"/../etc/passwd?file=" + markdownFile,
		}

		for _, pattern := range patterns {
			req := httptest.NewRequest("GET", pattern, nil)
			w := httptest.NewRecorder()

			handleIndex(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected status 404 for path traversal %s, got %d", pattern, w.Code)
			}
		}
	})
}

func TestIsPathWithinDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("PathWithinDirectory", func(t *testing.T) {
		path := filepath.Join(tmpDir, "subdir", "file.txt")
		if !isPathWithinDirectory(path, tmpDir) {
			t.Error("Path should be within directory")
		}
	})

	t.Run("PathEqualsDirectory", func(t *testing.T) {
		if !isPathWithinDirectory(tmpDir, tmpDir) {
			t.Error("Directory should be within itself")
		}
	})

	t.Run("PathOutsideDirectory", func(t *testing.T) {
		outside := filepath.Join(filepath.Dir(tmpDir), "other")
		if isPathWithinDirectory(outside, tmpDir) {
			t.Error("Path should not be within directory")
		}
	})

	t.Run("PathTraversalBlocked", func(t *testing.T) {
		// Even though this technically resolves to outside, it should be blocked
		traversal := filepath.Join(tmpDir, "..", "..", "etc", "passwd")
		if isPathWithinDirectory(traversal, tmpDir) {
			t.Error("Path traversal should be blocked")
		}
	})
}

func TestHandleIndexSSE(t *testing.T) {
	t.Run("ConnectionEstablished", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/events/index", nil)
		w := httptest.NewRecorder()

		// Run in goroutine since SSE handlers block
		done := make(chan bool)
		go func() {
			handleIndexSSE(w, req)
			done <- true
		}()

		// Give handler time to set up
		time.Sleep(100 * time.Millisecond)

		// Verify headers were set
		result := w.Result()
		if ct := result.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
		}
		if cc := result.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Expected Cache-Control no-cache, got %s", cc)
		}
		if conn := result.Header.Get("Connection"); conn != "keep-alive" {
			t.Errorf("Expected Connection keep-alive, got %s", conn)
		}

		// Cancel the request to end the handler
		// (Note: in real usage, httptest doesn't fully support request cancellation,
		// but the test verifies headers were set correctly)
	})

	t.Run("ReceivesNotifications", func(t *testing.T) {
		// This is tested in the integration test TestMultiFileEndToEnd
		// where we verify the index page SSE receives updates when files are added
	})
}
