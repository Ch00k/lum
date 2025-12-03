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
		// Set XDG_RUNTIME_DIR
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		tmpDir := t.TempDir()
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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
		// Unset XDG_RUNTIME_DIR
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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
		tmpDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupSocket()
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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

		tmpDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupSocket()
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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
}

func TestHandleControlCommand(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")

	if err := os.WriteFile(testFile, []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 16402

	// Setup environment
	tmpRuntimeDir := t.TempDir()
	oldXDG := os.Getenv("XDG_RUNTIME_DIR")
	if err := os.Setenv("XDG_RUNTIME_DIR", tmpRuntimeDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupSocket()
		if oldXDG != "" {
			if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
				t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
			}
		} else {
			if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
				t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
			}
		}
	})

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

		response := string(buf[:n])
		if !contains(response, "ERROR") {
			t.Errorf("Expected ERROR response, got: %s", response)
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

		response := string(buf[:n])
		if !contains(response, "ERROR") {
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

		response := string(buf[:n])
		if !contains(response, "ERROR") || !contains(response, "does not exist") {
			t.Errorf("Expected ERROR about non-existent file, got: %s", response)
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

		response := string(buf[:n])
		if !contains(response, "OK") {
			t.Errorf("Expected OK response, got: %s", response)
		}
		if !contains(response, testFile) {
			t.Errorf("Expected response to contain file path, got: %s", response)
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
		tmpRuntimeDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpRuntimeDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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
		tmpRuntimeDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpRuntimeDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupSocket()
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

		// Start server
		go func() {
			_ = startNewServer(port, testFile)
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
		tmpDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

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
		tmpDir := t.TempDir()
		oldXDG := os.Getenv("XDG_RUNTIME_DIR")
		if err := os.Setenv("XDG_RUNTIME_DIR", tmpDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if oldXDG != "" {
				if err := os.Setenv("XDG_RUNTIME_DIR", oldXDG); err != nil {
					t.Logf("Failed to restore XDG_RUNTIME_DIR: %v", err)
				}
			} else {
				if err := os.Unsetenv("XDG_RUNTIME_DIR"); err != nil {
					t.Logf("Failed to unset XDG_RUNTIME_DIR: %v", err)
				}
			}
		})

		// Should not panic or error when socket doesn't exist
		cleanupSocket()
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
