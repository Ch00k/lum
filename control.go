package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// getSocketPath returns the platform-specific Unix domain socket path for the control socket.
// Uses XDG_RUNTIME_DIR on Linux, falls back to /tmp/lum-$UID/ if not available.
// On macOS, uses os.TempDir() to avoid CGo dependency
// (ideally would use confstr(_CS_DARWIN_USER_TEMP_DIR) but avoiding CGo).
func getSocketPath() (string, error) {
	var baseDir string

	// Try XDG_RUNTIME_DIR first (Linux standard)
	if xdgDir := os.Getenv("XDG_RUNTIME_DIR"); xdgDir != "" {
		baseDir = filepath.Join(xdgDir, "lum")
	} else {
		// Fallback to /tmp/lum-$UID/
		uid := os.Getuid()
		baseDir = filepath.Join(os.TempDir(), fmt.Sprintf("lum-%d", uid))
	}

	// Ensure directory exists
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create socket directory: %w", err)
	}

	return filepath.Join(baseDir, "control.sock"), nil
}

// startControlSocket starts a Unix domain socket listener and handles incoming control commands.
// This allows new lum invocations to communicate with an existing server instance.
func startControlSocket(port int) error {
	socketPath, err := getSocketPath()
	if err != nil {
		return fmt.Errorf("failed to get socket path: %w", err)
	}

	// Remove existing socket if it exists (in case of unclean shutdown)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket listener: %w", err)
	}

	log.Printf("Control socket listening at %s", socketPath)

	go func() {
		defer func() {
			if err := listener.Close(); err != nil {
				log.Printf("Failed to close listener: %v", err)
			}
		}()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
			go handleControlCommand(conn, port)
		}
	}()

	return nil
}

// handleControlCommand processes a single control command from a client connection.
// Protocol: "ADD /absolute/path/to/file.md\n"
// Response: "OK http://localhost:PORT/?file=/absolute/path/to/file.md\n" or "ERROR <message>\n"
func handleControlCommand(conn net.Conn, port int) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close connection: %v", err)
		}
	}()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Failed to read from control socket: %v", err)
		return
	}

	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, " ", 2)

	if len(parts) != 2 || parts[0] != "ADD" {
		if _, err := fmt.Fprintf(conn, "ERROR invalid command: expected 'ADD <path>'\n"); err != nil {
			log.Printf("Failed to write error response: %v", err)
		}
		return
	}

	filePath := parts[1]

	// Validate file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if _, err := fmt.Fprintf(conn, "ERROR file does not exist: %s\n", filePath); err != nil {
			log.Printf("Failed to write error response: %v", err)
		}
		return
	}

	// Add file to tracked files
	if err := addFile(filePath); err != nil {
		if _, err := fmt.Fprintf(conn, "ERROR failed to add file: %v\n", err); err != nil {
			log.Printf("Failed to write error response: %v", err)
		}
		return
	}

	url := fmt.Sprintf("http://localhost:%d/?file=%s", port, filePath)
	if _, err := fmt.Fprintf(conn, "OK %s\n", url); err != nil {
		log.Printf("Failed to write success response: %v", err)
		return
	}
	log.Printf("Added file via control socket: %s", filePath)
}

// tryAddToExistingServer attempts to add a file to an existing server instance via the control socket.
// Returns the URL where the file can be accessed if successful, or an error if no server is running
// or the request fails.
func tryAddToExistingServer(filePath string) (string, error) {
	socketPath, err := getSocketPath()
	if err != nil {
		return "", fmt.Errorf("failed to get socket path: %w", err)
	}

	// Check if socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return "", fmt.Errorf("no existing server (socket does not exist)")
	}

	// Try to connect to the socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return "", fmt.Errorf("failed to connect to existing server: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Failed to close connection: %v", err)
		}
	}()

	// Send ADD command
	if _, err := fmt.Fprintf(conn, "ADD %s\n", filePath); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	response = strings.TrimSpace(response)

	if url, found := strings.CutPrefix(response, "OK "); found {
		return url, nil
	}

	if url, found := strings.CutPrefix(response, "ERROR "); found {
		return "", fmt.Errorf("server error: %s", url)
	}

	return "", fmt.Errorf("unexpected response: %s", response)
}

// cleanupSocket removes the control socket on shutdown
func cleanupSocket() {
	socketPath, err := getSocketPath()
	if err != nil {
		log.Printf("Failed to get socket path for cleanup: %v", err)
		return
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to remove socket: %v", err)
	}
}
