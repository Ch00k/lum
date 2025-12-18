package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMultiFileEndToEnd tests the complete multi-file workflow
func TestMultiFileEndToEnd(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "test1.md")
	file2 := filepath.Join(tmpDir, "test2.md")

	// Create test markdown files
	if err := os.WriteFile(file1, []byte("# Test 1\nContent 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("# Test 2\nContent 2"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Use a unique port for this test
	port := 16333

	// Start server in background
	done := make(chan error, 1)
	go func() {
		done <- startDaemon(port, file1)
	}()

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Ensure cleanup
	t.Cleanup(func() {
		cleanupSocket()
		// Give cleanup time to complete
		time.Sleep(100 * time.Millisecond)
	})

	// Test 1: Verify first file is accessible
	t.Run("FirstFileAccessible", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/?file=%s", port, file1)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET %s: %v", url, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Test 1") {
			t.Error("Response doesn't contain 'Test 1'")
		}
	})

	// Test 2: Add second file via control socket
	t.Run("AddSecondFile", func(t *testing.T) {
		url, err := tryAddToExistingServer(file2)
		if err != nil {
			t.Fatalf("Failed to add second file: %v", err)
		}

		if !strings.Contains(url, file2) {
			t.Errorf("URL doesn't contain file path: %s", url)
		}

		// Verify second file is accessible
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET %s: %v", url, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Test 2") {
			t.Error("Response doesn't contain 'Test 2'")
		}
	})

	// Test 3: Verify index page lists both files
	t.Run("IndexListsBothFiles", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/", port)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET index: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		if !strings.Contains(bodyStr, "test1.md") {
			t.Error("Index doesn't contain test1.md")
		}
		if !strings.Contains(bodyStr, "test2.md") {
			t.Error("Index doesn't contain test2.md")
		}
	})

	// Test 4: Add duplicate file should succeed
	t.Run("AddDuplicateFile", func(t *testing.T) {
		url, err := tryAddToExistingServer(file1)
		if err != nil {
			t.Fatalf("Failed to add duplicate file: %v", err)
		}

		if !strings.Contains(url, file1) {
			t.Errorf("URL doesn't contain file path: %s", url)
		}
	})

	// Test 5: File modification triggers re-rendering
	t.Run("FileModificationTriggersReload", func(t *testing.T) {
		// Update file1
		if err := os.WriteFile(file1, []byte("# Test 1 Updated\nNew content"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Give watcher time to detect and process change
		time.Sleep(300 * time.Millisecond)

		// Verify updated content is served
		url := fmt.Sprintf("http://localhost:%d/?file=%s", port, file1)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET %s: %v", url, err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Test 1 Updated") {
			t.Error("Response doesn't contain updated content")
		}
	})

	// Test 6: SSE endpoint works
	t.Run("SSEEndpoint", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/events?file=%s", port, file1)

		// Create a client with short timeout
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url)

		// We expect a timeout since SSE keeps the connection open
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			if resp.Header.Get("Content-Type") != "text/event-stream" {
				t.Errorf("Expected Content-Type text/event-stream, got %s", resp.Header.Get("Content-Type"))
			}
		}
		// Timeout is expected behavior for SSE, so don't fail on timeout
	})

	// Test 7: Missing file parameter returns 400
	t.Run("SSEMissingFileParam", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/events", port)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET SSE: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	// Test 8: Non-existent file returns 404
	t.Run("NonExistentFileReturns404", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/?file=/tmp/nonexistent.md", port)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to GET %s: %v", url, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", resp.StatusCode)
		}
	})
}

// TestControlSocketProtocol tests the control socket protocol
func TestControlSocketProtocol(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 16334

	// Start server
	go func() {
		_ = startDaemon(port, testFile)
	}()

	time.Sleep(500 * time.Millisecond)

	t.Cleanup(func() {
		cleanupSocket()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("ValidADDCommand", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect to socket: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Send ADD command
		if _, err := fmt.Fprintf(conn, "ADD %s\n", testFile); err != nil {
			t.Fatal(err)
		}

		// Read response
		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(response, "OK ") {
			t.Errorf("Expected OK response, got: %s", response)
		}
	})

	t.Run("InvalidCommand", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect to socket: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Send invalid command
		if _, err := fmt.Fprintf(conn, "INVALID command\n"); err != nil {
			t.Fatal(err)
		}

		// Read response
		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(response, "ERROR ") {
			t.Errorf("Expected ERROR response, got: %s", response)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect to socket: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Send ADD command with non-existent file
		if _, err := fmt.Fprintf(conn, "ADD /tmp/nonexistent.md\n"); err != nil {
			t.Fatal(err)
		}

		// Read response
		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(response, "ERROR") || !strings.Contains(response, "does not exist") {
			t.Errorf("Expected error about non-existent file, got: %s", response)
		}
	})
}

// TestRenderMarkdown tests markdown rendering
func TestRenderMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	// Create markdown file with various features
	content := `# Heading 1

## Heading 2

This is **bold** and this is *italic*.

- List item 1
- List item 2

` + "```go\nfunc main() {\n\tfmt.Println(\"Hello\")\n}\n```"

	if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Add file to tracking
	filesLock.Lock()
	files[testFile] = &FileState{
		path:       testFile,
		sseClients: make(map[chan string]bool),
	}
	filesLock.Unlock()

	// Render
	if err := renderMarkdown(testFile); err != nil {
		t.Fatalf("Failed to render markdown: %v", err)
	}

	// Check rendered content
	filesLock.RLock()
	html := string(files[testFile].htmlContent)
	filesLock.RUnlock()

	// Verify HTML elements
	checks := []string{
		"<h1",
		"<h2",
		"<strong>bold</strong>",
		"<em>italic</em>",
		"<li>List item 1</li>",
		"<code",
		"Println",
	}

	for _, check := range checks {
		if !strings.Contains(html, check) {
			t.Errorf("Rendered HTML doesn't contain: %s", check)
		}
	}

	// Cleanup
	filesLock.Lock()
	delete(files, testFile)
	filesLock.Unlock()
}

// TestStartOneOff tests the one-off mode server startup
func TestStartOneOff(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 16400

	// Start server in goroutine
	done := make(chan error, 1)
	go func() {
		done <- startOneOff(port, testFile)
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Verify server is responding
	url := fmt.Sprintf("http://localhost:%d/?file=%s", port, testFile)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
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
}

// TestRunErrorPaths tests error handling in run()
func TestRunErrorPaths(t *testing.T) {
	t.Run("NoArguments", func(t *testing.T) {
		opts, args, err := parseArgs([]string{})
		if err != nil {
			t.Fatal(err)
		}

		// Verify parsing result
		if len(args) == 1 || opts.help || opts.stop || opts.daemon {
			t.Error("Expected no args and no flags set")
		}
	})

	t.Run("StopDaemonWhenNoneRunning", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
				t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
			}
		}()

		err := stopDaemon()
		if err == nil {
			t.Error("Expected error when stopping non-existent daemon")
		}
		expectedError := "no daemon running"
		if err.Error() != expectedError {
			t.Errorf("Expected error %q, got: %q", expectedError, err.Error())
		}
	})

	t.Run("DaemonExistsCheck", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
				t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
			}
		}()

		// No daemon should exist
		if daemonExists() {
			t.Error("daemonExists should return false when no daemon running")
		}
	})
}
