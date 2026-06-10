package main

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// browserCommand returns the command used to open a URL in the default
// browser, or "" if no browser is expected to be available: the
// LUM_NO_BROWSER environment variable is set, the system is headless,
// or the opener is not installed
func browserCommand() string {
	if os.Getenv("LUM_NO_BROWSER") != "" {
		return ""
	}

	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	default:
		// A graphical session exposes DISPLAY (X11) or WAYLAND_DISPLAY
		// (Wayland); a headless system has neither
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return ""
		}
		command = "xdg-open"
	}

	if _, err := exec.LookPath(command); err != nil {
		return ""
	}
	return command
}

// openBrowser opens url in the default browser. It is best-effort: when no
// browser is available it does nothing, and failures are silent because the
// URL is always printed to the terminal
func openBrowser(url string) {
	command := browserCommand()
	if command == "" {
		return
	}

	cmd := exec.Command(command, url)
	if err := cmd.Start(); err != nil {
		return
	}

	// Reap the opener so it does not linger as a zombie while the server runs
	go func() {
		_ = cmd.Wait()
	}()
}

// openBrowserWhenReady waits until the server at addr accepts connections,
// then opens url in the default browser. It gives up silently if the server
// does not come up within 2 seconds
func openBrowserWhenReady(addr, url string) {
	if browserCommand() == "" {
		return
	}

	for range 200 {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			openBrowser(url)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
