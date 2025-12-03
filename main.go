package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	port := flag.Int("port", 6333, "Port to run the server on")
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: lum <path-to-markdown-file> [--port PORT]\n")
		os.Exit(1)
	}

	// Convert to absolute path and validate
	absPath, err := filepath.Abs(args[0])
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}

	// Validate file exists and is readable
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", absPath)
	}

	// Try to add to existing server first
	url, err := tryAddToExistingServer(absPath)
	if err == nil {
		// Success - file added to existing server
		fmt.Printf("Added to existing server: %s\n", url)
		os.Exit(0)
	}

	// No existing server or connection failed - start new one
	log.Printf("Starting new server...")
	if err := startNewServer(*port, absPath); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// startNewServer initializes and starts a new lum server instance
func startNewServer(port int, initialFile string) error {
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

	// Add initial file
	if err := addFile(initialFile); err != nil {
		return fmt.Errorf("failed to add initial file: %w", err)
	}

	// Setup HTTP handlers with a new ServeMux to avoid conflicts in tests
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/events", handleSSE)
	mux.HandleFunc("/events/index", handleIndexSSE)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("Serving %s at http://%s", initialFile, addr)
	log.Printf("Index page at http://%s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}
