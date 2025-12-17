package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type options struct {
	port   int
	daemon bool
	stop   bool
	help   bool
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: lum [OPTIONS] [FILE]

Render Markdown files in a web browser with live reload.

Options:
  -p, --port PORT     Port to run the server on (default: 6333)
  -d, --daemon        Run as daemon (allows serving multiple files)
  -s, --stop          Stop the running daemon
  -h, --help          Show this help message

Examples:
  lum file.md              Serve file in one-off mode
  lum --daemon             Start daemon with no files
  lum --daemon file.md     Start daemon with initial file
  lum file.md              Add file to existing daemon (if running)
  lum --stop               Stop the daemon
`)
}

func parseArgs(args []string) (*options, []string, error) {
	opts := &options{
		port: 6333,
	}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch arg {
		case "-h", "--help":
			opts.help = true
		case "-d", "--daemon":
			opts.daemon = true
		case "-s", "--stop":
			opts.stop = true
		case "-p", "--port":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag needs an argument: %s", arg)
			}
			i++
			port, err := strconv.Atoi(args[i])
			if err != nil {
				return nil, nil, fmt.Errorf("invalid port value: %s", args[i])
			}
			if port < 1 || port > 65535 {
				return nil, nil, fmt.Errorf("port must be between 1 and 65535: %d", port)
			}
			opts.port = port
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, nil, fmt.Errorf("unknown flag: %s", arg)
			}
			positional = append(positional, arg)
		}
	}

	return opts, positional, nil
}

func main() {
	os.Exit(run())
}

func run() int {
	// Parse command line arguments
	opts, args, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	if opts.help {
		printUsage()
		return 0
	}

	port := opts.port
	daemon := opts.daemon
	stop := opts.stop

	// Handle --stop
	if stop {
		if err := stopDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stop daemon: %v\n", err)
			return 1
		}
		return 0
	}

	// Handle --daemon mode
	if daemon {
		// Check if we're the daemonized child process
		isDaemonized := os.Getenv("LUM_DAEMONIZED") == "1"

		if !isDaemonized {
			// Parent process - validate and daemonize
			if daemonExists() {
				fmt.Fprintf(os.Stderr, "Daemon already running\n")
				return 1
			}

			// Daemon mode allows 0 or 1 file arguments
			if len(args) > 1 {
				fmt.Fprintf(os.Stderr, "Usage: lum --daemon [<path-to-markdown-file>]\n")
				return 1
			}

			var initialFile string
			if len(args) == 1 {
				absPath, err := filepath.Abs(args[0])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Failed to get absolute path: %v\n", err)
					return 1
				}
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "File does not exist: %s\n", absPath)
					return 1
				}
				initialFile = absPath
			}

			// Daemonize and exit
			if err := daemonize(port, initialFile); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to daemonize: %v\n", err)
				return 1
			}
			// Parent process exits here
			return 0
		}

		// We are the daemonized child - parse args again to get initialFile
		var initialFile string
		if len(args) == 1 {
			initialFile = args[0] // Already validated and converted to absolute path by parent
		}

		// Start the daemon server
		if err := startDaemon(port, initialFile); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
			return 1
		}
		return 0
	}

	// Auto-detect mode: requires exactly 1 file argument
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: lum <path-to-markdown-file> [--port PORT]\n")
		return 1
	}

	absPath, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get absolute path: %v\n", err)
		return 1
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "File does not exist: %s\n", absPath)
		return 1
	}

	// Try to add to existing daemon
	url, err := tryAddToExistingServer(absPath)
	if err == nil {
		// Added to existing daemon
		fmt.Println(url)
		return 0
	}

	// No daemon running - start in one-off mode
	if err := startOneOff(port, absPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		return 1
	}
	return 0
}

// daemonize re-executes the current process as a daemon
func daemonize(port int, initialFile string) error {
	// Build command to re-execute ourselves
	var args []string

	// If running as a test binary, preserve test flags
	if strings.HasSuffix(os.Args[0], ".test") {
		// Find test-specific flags to preserve
		for i, arg := range os.Args[1:] {
			if arg == "--" {
				// Stop at separator
				break
			}
			if strings.HasPrefix(arg, "-test.") {
				args = append(args, arg)
				// Check if this flag takes a value
				if arg == "-test.run" || arg == "-test.coverprofile" {
					if i+2 < len(os.Args) {
						args = append(args, os.Args[i+2])
					}
				}
			}
		}
		// Add separator
		args = append(args, "--")
	}

	args = append(args, "--daemon", "--port", fmt.Sprintf("%d", port))
	if initialFile != "" {
		args = append(args, initialFile)
	}

	cmd := exec.Command(os.Args[0], args...)

	// Set environment variable to indicate this is the daemonized child
	cmd.Env = append(os.Environ(), "LUM_DAEMONIZED=1")

	// Detach from parent
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	return nil
}

// daemonExists checks if a daemon is already running
func daemonExists() bool {
	socketPath, err := getSocketPath()
	if err != nil {
		return false
	}

	// Try to connect to the socket to verify daemon is actually running
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stopDaemon sends a STOP command to the running daemon
func stopDaemon() error {
	socketPath, err := getSocketPath()
	if err != nil {
		return fmt.Errorf("failed to get socket path: %w", err)
	}

	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return fmt.Errorf("no daemon running")
	}

	conn, err := dialSocket(socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close connection: %v\n", err)
		}
	}()

	if _, err := fmt.Fprintf(conn, "STOP\n"); err != nil {
		return fmt.Errorf("failed to send STOP command: %w", err)
	}

	return nil
}

// startDaemon initializes and starts a daemon instance
func startDaemon(port int, initialFile string) error {
	// Setup log file
	if err := setupLogFile(); err != nil {
		return fmt.Errorf("failed to setup log file: %w", err)
	}

	// Start control socket
	if err := startControlSocket(port); err != nil {
		return fmt.Errorf("failed to start control socket: %w", err)
	}

	// Setup cleanup on exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cleanupSocket()
		os.Exit(0)
	}()

	// Add initial file if provided
	if initialFile != "" {
		if err := addFile(initialFile); err != nil {
			return fmt.Errorf("failed to add initial file: %w", err)
		}
	}

	// Setup HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/events", handleSSE)
	mux.HandleFunc("/events/index", handleIndexSSE)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("Daemon started on http://%s", addr)
	if initialFile != "" {
		log.Printf("Serving %s", initialFile)
	}

	// TODO: Daemonize (detach from terminal)

	if err := http.ListenAndServe(addr, mux); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

// startOneOff starts a simple one-off server for a single file
func startOneOff(port int, filePath string) error {
	// Suppress all log output in one-off mode
	log.SetOutput(io.Discard)

	// Add the file
	if err := addFile(filePath); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	// Setup HTTP handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/events", handleSSE)
	mux.HandleFunc("/events/index", handleIndexSSE)

	// Try to create listener first to check if port is available
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	// Port is available, print URL
	url := fmt.Sprintf("http://%s/?file=%s", addr, filePath)
	fmt.Println(url)

	// Start serving
	if err := http.Serve(listener, mux); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}
