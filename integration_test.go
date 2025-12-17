package main

// Integration tests for lum that run the actual binary with coverage collection.
//
// These tests build a test binary with coverage instrumentation and execute it
// as a subprocess, allowing us to test the full application behavior including
// daemon mode, one-off mode, and signal handling.
//
// How it works:
// 1. buildTestBinary() compiles a test binary with `go test -c -cover`
// 2. runBinary() executes the binary with GOCOVERDIR set to collect coverage
// 3. The binary runs TestRunMain, which calls run() with application arguments
// 4. Tests verify behavior and coverage data is written to GOCOVERDIR
//
// Environment isolation:
// - Each test uses a unique XDG_RUNTIME_DIR to avoid conflicts with live instances
// - GOCOVERDIR is set to collect coverage data from the binary execution
//
// Usage:
//   go test -run='^TestIntegration' -v

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// getTestBinary returns the path to a pre-built test binary with coverage instrumentation.
// The binary must be built beforehand using `make test-binary`.
// If LUM_TEST_EXECUTABLE is set, it uses that path; otherwise it looks for dist/lum.test.
func getTestBinary(t *testing.T) string {
	t.Helper()

	binaryPath := os.Getenv("LUM_TEST_EXECUTABLE")
	if binaryPath == "" {
		binaryPath = "dist/lum.test"
	}

	if _, err := os.Stat(binaryPath); err != nil {
		t.Skipf("Test binary not found at %s. Run 'make test-binary' first.", binaryPath)
	}

	absPath, err := filepath.Abs(binaryPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path for %s: %v", binaryPath, err)
	}

	return absPath
}

// runBinary runs the test binary with the given arguments and returns the process.
// The process runs in an isolated environment with a unique XDG_RUNTIME_DIR.
// Coverage data is written to the coverage directory (either from GOCOVERDIR env var
// for CI, or a temp directory for local testing).
func runBinary(t *testing.T, binaryPath string, args ...string) *exec.Cmd {
	t.Helper()

	// Create isolated runtime directory
	runtimeDir := t.TempDir()

	// Use GOCOVERDIR from environment if set (for CI), otherwise use temp dir
	coverageDir := os.Getenv("GOCOVERDIR")
	if coverageDir == "" {
		coverageDir = t.TempDir()
	}

	// Construct test arguments - run TestRunMain with coverage output
	testArgs := []string{
		"-test.run=^TestRunMain$",
		fmt.Sprintf("-test.gocoverdir=%s", coverageDir),
		"--",
	}
	testArgs = append(testArgs, args...)

	cmd := exec.Command(binaryPath, testArgs...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("XDG_RUNTIME_DIR=%s", runtimeDir),
	)

	return cmd
}

// TestRunMain is the entry point for running the application in the test binary.
// When the test binary is executed with -test.run=^TestRunMain$, this test
// will run, which in turn calls run().
// It strips out test flags from os.Args before calling run() so that
// run() only sees the application arguments.
//
// This test should only be run when executing the test binary directly,
// not during normal "go test" runs. It's skipped if there's no "--" separator
// in os.Args (which indicates it's being run as a regular test).
func TestRunMain(t *testing.T) {
	// Find the "--" separator in os.Args and take everything after it
	var appArgs []string
	foundSeparator := false
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			appArgs = os.Args[i+1:]
			foundSeparator = true
			break
		}
	}

	// Skip this test if running via "go test" (no -- separator found)
	if !foundSeparator {
		t.Skip("TestRunMain should only be run from the compiled test binary, not via go test")
	}

	// Save original os.Args
	origArgs := os.Args

	// Set os.Args to just the binary name plus application arguments
	os.Args = append([]string{os.Args[0]}, appArgs...)

	// Restore os.Args when done
	defer func() {
		os.Args = origArgs
	}()

	exitCode := run()
	if exitCode != 0 {
		t.Fatalf("run() returned non-zero exit code: %d", exitCode)
	}
}

// TestIntegrationOneOffMode tests lum in one-off mode using a compiled binary with coverage.
func TestIntegrationOneOffMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := getTestBinary(t)

	// Create test markdown file
	testFile := filepath.Join(t.TempDir(), "test.md")
	if err := os.WriteFile(testFile, []byte("# Test\n\nHello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := runBinary(t, binaryPath, "--port", "16500", testFile)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}

	// Read the URL from stdout with timeout
	urlChan := make(chan string, 1)
	errChan := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			urlChan <- scanner.Text()
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		errChan <- strings.Join(lines, "\n")
	}()

	var url string
	select {
	case url = <-urlChan:
		// Got URL
	case <-time.After(2 * time.Second):
		stderrOutput := ""
		select {
		case stderrOutput = <-errChan:
		default:
		}
		t.Fatalf("Timeout waiting for URL. Stderr: %s", stderrOutput)
	}

	// Verify URL format
	expectedPrefix := "http://127.0.0.1:16500/?file="
	if !strings.HasPrefix(url, expectedPrefix) {
		t.Errorf("Expected URL to start with %s, got: %s", expectedPrefix, url)
	}

	// Clean shutdown
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Logf("Failed to send interrupt: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(5 * time.Second):
		if err := cmd.Process.Kill(); err != nil {
			t.Logf("Failed to kill process: %v", err)
		}
		t.Fatal("Process did not exit after interrupt")
	}
}

// TestIntegrationDaemonMode tests lum in daemon mode using a compiled binary with coverage.
func TestIntegrationDaemonMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := getTestBinary(t)

	// Create test markdown file
	testFile := filepath.Join(t.TempDir(), "test.md")
	if err := os.WriteFile(testFile, []byte("# Test\n\nHello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()

	// Use GOCOVERDIR from environment if set (for CI), otherwise use temp dir
	coverageDir := os.Getenv("GOCOVERDIR")
	if coverageDir == "" {
		coverageDir = t.TempDir()
	}

	// Start daemon with initial file
	cmd := exec.Command(binaryPath,
		"-test.run=^TestRunMain$",
		fmt.Sprintf("-test.gocoverdir=%s", coverageDir),
		"--",
		"--daemon",
		"--port", "16501",
		testFile,
	)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("XDG_RUNTIME_DIR=%s", runtimeDir),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	// Capture stdout for debugging
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			t.Logf("Daemon stdout: %s", scanner.Text())
		}
	}()
	defer func() {
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("Failed to kill daemon: %v", err)
			}
			_ = cmd.Wait()
		}
	}()

	// Capture stderr for debugging
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			t.Logf("Daemon stderr: %s", scanner.Text())
		}
	}()

	// Wait for daemon to start with timeout
	socketPath := filepath.Join(runtimeDir, "lum", "control.sock")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify socket was created
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Errorf("Control socket was not created at %s", socketPath)
		// List directory contents for debugging
		if entries, err := os.ReadDir(filepath.Dir(socketPath)); err == nil {
			t.Logf("Directory contents: %v", entries)
		}
		if entries, err := os.ReadDir(runtimeDir); err == nil {
			t.Logf("Runtime directory contents: %v", entries)
		}
		// Check log file
		logPath := filepath.Join(runtimeDir, "lum", "lum.log")
		if logData, err := os.ReadFile(logPath); err == nil {
			t.Logf("Daemon log: %s", string(logData))
		} else {
			t.Logf("Failed to read log file: %v", err)
		}
		return
	}

	// Stop daemon using --stop flag
	stopCmd := exec.Command(binaryPath,
		"-test.run=^TestRunMain$",
		fmt.Sprintf("-test.gocoverdir=%s", coverageDir),
		"--",
		"--stop",
	)
	stopCmd.Env = append(os.Environ(),
		fmt.Sprintf("XDG_RUNTIME_DIR=%s", runtimeDir),
	)

	if output, err := stopCmd.CombinedOutput(); err != nil {
		t.Errorf("Failed to stop daemon: %v\nOutput: %s", err, output)
	}

	// Wait for daemon to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Daemon stopped
	case <-time.After(5 * time.Second):
		t.Error("Daemon did not stop after --stop command")
	}
}

// TestIntegrationInvalidFile tests error handling with a compiled binary.
func TestIntegrationInvalidFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := getTestBinary(t)

	cmd := runBinary(t, binaryPath, "/nonexistent/file.md")
	cmd.Stderr = nil // Capture stderr

	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("Expected binary to exit with error for nonexistent file")
	}

	outputStr := string(output)
	if !contains(outputStr, "does not exist") && !contains(outputStr, "no such file") {
		t.Errorf("Expected error message about nonexistent file, got: %s", outputStr)
	}
}

// TestIntegrationHelp tests the --help flag with a compiled binary.
func TestIntegrationHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	binaryPath := getTestBinary(t)

	cmd := runBinary(t, binaryPath, "--help")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run help: %v", err)
	}

	outputStr := string(output)
	if !contains(outputStr, "Usage:") {
		t.Error("Expected help output to contain 'Usage:'")
	}
}
