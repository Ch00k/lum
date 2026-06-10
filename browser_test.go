package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestMain suppresses browser opening for the entire test suite. This covers
// both in-process tests and the integration tests, which execute the
// application through this same test binary (via TestRunMain), so no test run
// ever opens a real browser on a machine with a graphical session.
func TestMain(m *testing.M) {
	if err := os.Setenv("LUM_NO_BROWSER", "1"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set LUM_NO_BROWSER: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// openerName returns the browser opener command name for the current platform
func openerName() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// installFakeOpener creates a fake browser opener in a temp directory, points
// PATH at only that directory, and re-enables browser opening by clearing
// LUM_NO_BROWSER and providing a graphical session via DISPLAY. It returns
// the path of the file the fake opener records its URL argument to.
func installFakeOpener(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	recordFile := filepath.Join(dir, "opened-url")
	script := fmt.Sprintf("#!/bin/sh\necho \"$1\" > %q\n", recordFile)
	if err := os.WriteFile(filepath.Join(dir, openerName()), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("LUM_NO_BROWSER", "")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")

	return recordFile
}

// waitForFile polls until path exists or the timeout expires, returning its
// contents or "" if it never appeared
func waitForFile(t *testing.T, path string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

// TestBrowserCommand verifies that browserCommand detects when a browser is
// available: it returns the platform opener in a graphical session and ""
// when suppressed via LUM_NO_BROWSER, when the session is headless, or when
// the opener is not installed
func TestBrowserCommand(t *testing.T) {
	t.Run("GraphicalSession", func(t *testing.T) {
		installFakeOpener(t)
		if got := browserCommand(); got != openerName() {
			t.Errorf("Expected %q, got %q", openerName(), got)
		}
	})

	t.Run("WaylandSession", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("Session detection only applies to Linux")
		}
		installFakeOpener(t)
		t.Setenv("DISPLAY", "")
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		if got := browserCommand(); got != "xdg-open" {
			t.Errorf("Expected \"xdg-open\", got %q", got)
		}
	})

	t.Run("Suppressed", func(t *testing.T) {
		installFakeOpener(t)
		t.Setenv("LUM_NO_BROWSER", "1")
		if got := browserCommand(); got != "" {
			t.Errorf("Expected \"\", got %q", got)
		}
	})

	t.Run("Headless", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("Session detection only applies to Linux")
		}
		installFakeOpener(t)
		t.Setenv("DISPLAY", "")
		if got := browserCommand(); got != "" {
			t.Errorf("Expected \"\", got %q", got)
		}
	})

	t.Run("OpenerNotInstalled", func(t *testing.T) {
		installFakeOpener(t)
		t.Setenv("PATH", t.TempDir())
		if got := browserCommand(); got != "" {
			t.Errorf("Expected \"\", got %q", got)
		}
	})
}

// TestOpenBrowser verifies that openBrowser invokes the platform opener with
// the URL when a browser is available, using a fake opener that records its
// argument to a file
func TestOpenBrowser(t *testing.T) {
	recordFile := installFakeOpener(t)

	openBrowser("http://127.0.0.1:6333/?file=/tmp/test.md")

	got := waitForFile(t, recordFile)
	if got != "http://127.0.0.1:6333/?file=/tmp/test.md\n" {
		t.Errorf("Opener was invoked with %q", got)
	}
}

// TestOpenBrowserSuppressed verifies that openBrowser does nothing when
// LUM_NO_BROWSER is set, by checking that the fake opener is never invoked
func TestOpenBrowserSuppressed(t *testing.T) {
	recordFile := installFakeOpener(t)
	t.Setenv("LUM_NO_BROWSER", "1")

	openBrowser("http://127.0.0.1:6333")

	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(recordFile); !os.IsNotExist(err) {
		t.Error("Opener was invoked despite LUM_NO_BROWSER being set")
	}
}

// TestOpenBrowserWhenReady verifies that openBrowserWhenReady opens the
// browser once the server address accepts connections
func TestOpenBrowserWhenReady(t *testing.T) {
	recordFile := installFakeOpener(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	url := fmt.Sprintf("http://%s", listener.Addr())
	openBrowserWhenReady(listener.Addr().String(), url)

	got := waitForFile(t, recordFile)
	if got != url+"\n" {
		t.Errorf("Opener was invoked with %q, expected %q", got, url)
	}
}

// TestOpenBrowserWhenReadyNoBrowser verifies that openBrowserWhenReady
// returns immediately without waiting for the server when no browser is
// available
func TestOpenBrowserWhenReadyNoBrowser(t *testing.T) {
	installFakeOpener(t)
	t.Setenv("LUM_NO_BROWSER", "1")

	start := time.Now()
	openBrowserWhenReady("127.0.0.1:1", "http://127.0.0.1:1")
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Expected immediate return, took %v", elapsed)
	}
}
