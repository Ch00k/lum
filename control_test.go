package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGetSocketPath(t *testing.T) {
	t.Run("WithXDGRuntimeDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", tmpDir)

		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatalf("Failed to get socket path: %v", err)
		}

		expectedDir := filepath.Join(tmpDir, "lum")
		if !strings.HasPrefix(socketPath, expectedDir) {
			t.Errorf("Expected socket path to start with %s, got %s", expectedDir, socketPath)
		}

		if !filepath.IsAbs(socketPath) {
			t.Errorf("Socket path should be absolute: %s", socketPath)
		}
	})

	t.Run("WithoutXDGRuntimeDir", func(t *testing.T) {
		// Empty XDG_RUNTIME_DIR triggers the same fallback as unset
		t.Setenv("XDG_RUNTIME_DIR", "")

		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatalf("Failed to get socket path: %v", err)
		}

		if socketPath == "" {
			t.Error("Socket path should not be empty")
		}

		if !filepath.IsAbs(socketPath) {
			t.Errorf("Socket path should be absolute: %s", socketPath)
		}

		// Should contain UID in path
		uid := os.Getuid()
		expectedPattern := fmt.Sprintf("lum-%d", uid)
		if !contains(socketPath, expectedPattern) {
			t.Errorf("Expected socket path to contain %s, got %s", expectedPattern, socketPath)
		}
	})
}

func TestStartControlSocket(t *testing.T) {
	t.Run("SuccessfulStart", func(t *testing.T) {
		port := 16400

		// Use a unique socket path for this test
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		err := startControlSocket(port)
		if err != nil {
			t.Fatalf("Failed to start control socket: %v", err)
		}

		// Give it time to start
		time.Sleep(100 * time.Millisecond)

		// Verify socket exists
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatalf("Failed to get socket path: %v", err)
		}

		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			t.Errorf("Socket file should exist at %s", socketPath)
		}
	})

	t.Run("RestartsWithExistingSocket", func(t *testing.T) {
		port := 16401

		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		// Start first socket
		err := startControlSocket(port)
		if err != nil {
			t.Fatalf("Failed to start first control socket: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		// Should be able to "restart" by removing and recreating
		cleanupSocket()
		time.Sleep(100 * time.Millisecond)

		err = startControlSocket(port)
		if err != nil {
			t.Fatalf("Failed to restart control socket: %v", err)
		}
	})

	// A live listener on the socket belongs to another daemon and must not be hijacked.
	t.Run("RefusesWhenSocketLive", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		if err := startControlSocket(16406); err != nil {
			t.Fatalf("Failed to start first control socket: %v", err)
		}
		time.Sleep(100 * time.Millisecond)

		err := startControlSocket(16407)
		if err == nil {
			t.Fatal("Expected error when the socket is in use by a live daemon")
		}
		if !strings.Contains(err.Error(), "already running") {
			t.Errorf("Expected 'already running' error, got: %v", err)
		}
	})

	// A socket file with no listener (unclean shutdown) is removed and replaced.
	t.Run("RemovesStaleSocket", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		// Leave a socket file behind with nothing listening on it
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		listener.SetUnlinkOnClose(false)
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(socketPath); err != nil {
			t.Fatalf("Stale socket file should exist: %v", err)
		}

		if err := startControlSocket(16408); err != nil {
			t.Fatalf("Expected stale socket to be replaced, got error: %v", err)
		}
	})
}

func TestHandleControlCommand(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 16402

	// Setup environment
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Cleanup(cleanupSocket)

	// Start control socket
	if err := startControlSocket(port); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	t.Run("InvalidCommandFormat", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Send command without path
		if _, err := fmt.Fprintf(conn, "ADD\n"); err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}

		expectedResponse := "ERROR invalid command: expected 'ADD <path>'\n"
		actualResponse := string(buf[:n])
		if actualResponse != expectedResponse {
			t.Errorf("Expected response:\n%q\nGot:\n%q", expectedResponse, actualResponse)
		}
	})

	t.Run("UnknownCommand", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		if _, err := fmt.Fprintf(conn, "UNKNOWN /path\n"); err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}

		expectedResponse := "ERROR invalid command: expected 'ADD <path>' or 'STOP'\n"
		actualResponse := string(buf[:n])
		if actualResponse != expectedResponse {
			t.Errorf("Expected response:\n%q\nGot:\n%q", expectedResponse, actualResponse)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		if _, err := fmt.Fprintf(conn, "ADD /nonexistent/file.md\n"); err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}

		expectedResponse := "ERROR file does not exist: /nonexistent/file.md\n"
		actualResponse := string(buf[:n])
		if actualResponse != expectedResponse {
			t.Errorf("Expected response:\n%q\nGot:\n%q", expectedResponse, actualResponse)
		}
	})

	t.Run("SuccessfulAdd", func(t *testing.T) {
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		if _, err := fmt.Fprintf(conn, "ADD %s\n", testFile); err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}

		expectedResponse := fmt.Sprintf("OK http://localhost:%d/?file=%s\n", port, testFile)
		actualResponse := string(buf[:n])
		if actualResponse != expectedResponse {
			t.Errorf("Expected response:\n%q\nGot:\n%q", expectedResponse, actualResponse)
		}
	})
}

func TestTryAddToExistingServer(t *testing.T) {
	t.Run("NoExistingServer", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		// Use a unique socket path
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

		_, err := tryAddToExistingServer(testFile)
		if err == nil {
			t.Error("Expected error when no server is running")
		}
	})

	t.Run("ServerRunning", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		port := 16403

		// Setup environment
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		// Start server
		go func() {
			_ = startDaemon(port, testFile)
		}()

		time.Sleep(500 * time.Millisecond)

		// Try to add another file
		testFile2 := filepath.Join(tmpDir, "test2.md")
		if err := os.WriteFile(testFile2, []byte("# Test 2"), 0o600); err != nil {
			t.Fatal(err)
		}

		url, err := tryAddToExistingServer(testFile2)
		if err != nil {
			t.Fatalf("Expected success, got error: %v", err)
		}

		if url == "" {
			t.Error("Expected URL to be returned")
		}
		if !contains(url, testFile2) {
			t.Errorf("Expected URL to contain file path, got: %s", url)
		}
	})
}

func TestCleanupSocket(t *testing.T) {
	t.Run("RemovesSocket", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

		// Start a socket
		port := 16404
		if err := startControlSocket(port); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)

		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		// Verify socket exists
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			t.Fatal("Socket should exist before cleanup")
		}

		// Cleanup
		cleanupSocket()

		// Verify socket is removed
		if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
			t.Error("Socket should be removed after cleanup")
		}
	})

	t.Run("NoErrorWhenSocketDoesNotExist", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

		// Should not panic or error when socket doesn't exist
		cleanupSocket()
	})
}

func TestControlSocketErrorHandling(t *testing.T) {
	t.Run("ADDCommandWithoutPath", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.md")

		if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
			t.Fatal(err)
		}

		port := 16405

		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		t.Cleanup(cleanupSocket)

		go func() {
			_ = startDaemon(port, testFile)
		}()

		time.Sleep(500 * time.Millisecond)

		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatal(err)
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// Send ADD without a path
		if _, err := fmt.Fprintf(conn, "ADD\n"); err != nil {
			t.Fatal(err)
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}

		expectedResponse := "ERROR invalid command: expected 'ADD <path>'\n"
		actualResponse := string(buf[:n])
		if actualResponse != expectedResponse {
			t.Errorf("Expected response:\n%q\nGot:\n%q", expectedResponse, actualResponse)
		}
	})

	t.Run("GetSocketPathWithoutXDG", func(t *testing.T) {
		// Empty XDG_RUNTIME_DIR triggers the same fallback as unset
		t.Setenv("XDG_RUNTIME_DIR", "")

		// Should fallback to /tmp/lum-$UID/control.sock
		socketPath, err := getSocketPath()
		if err != nil {
			t.Fatalf("getSocketPath should not error on fallback: %v", err)
		}

		expectedPrefix := filepath.Join(os.TempDir(), fmt.Sprintf("lum-%d", os.Getuid()), "control.sock")
		if socketPath != expectedPrefix {
			t.Errorf("Expected socket path:\n%q\nGot:\n%q", expectedPrefix, socketPath)
		}
	})
}

func TestSetupLogFile(t *testing.T) {
	t.Run("CreatesLogFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", tmpDir)

		err := setupLogFile()
		if err != nil {
			t.Fatalf("setupLogFile failed: %v", err)
		}

		// Verify log directory was created
		logDir := filepath.Join(tmpDir, "lum")
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			t.Error("Log directory should be created")
		}

		// Verify log file exists
		logPath := filepath.Join(logDir, "lum.log")
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Error("Log file should be created")
		}
	})
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
