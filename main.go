package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed assets/*
var assets embed.FS

var (
	// Version is set via ldflags during build
	Version = "dev"

	markdownPath string
	markdownLock sync.RWMutex
	htmlContent  []byte
	sseClients   = make(map[chan string]bool)
	clientsLock  sync.RWMutex
	md           goldmark.Markdown
	htmlTemplate *template.Template
)

func init() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithStyle("friendly"),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)

	tmplContent, err := assets.ReadFile("assets/template.html")
	if err != nil {
		log.Fatalf("Failed to read template: %v", err)
	}
	htmlTemplate = template.Must(template.New("index").Parse(string(tmplContent)))
}

func main() {
	port := flag.Int("port", 6333, "Port to run the server on")
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: lum <path-to-markdown-file> [--port PORT]\n")
		os.Exit(1)
	}

	// Convert to absolute path immediately
	absPath, err := filepath.Abs(args[0])
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}
	markdownPath = absPath

	// Validate file exists and is readable
	if _, err := os.Stat(markdownPath); os.IsNotExist(err) {
		log.Fatalf("File does not exist: %s", markdownPath)
	}

	if err := renderMarkdown(); err != nil {
		log.Fatalf("Failed to render markdown: %v", err)
	}

	// Start file watcher
	go watchFile()

	// Setup HTTP handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/events", handleSSE)

	addr := fmt.Sprintf(":%d", *port)
	absPath, _ = filepath.Abs(markdownPath)
	log.Printf("Serving %s at http://localhost%s", absPath, addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func renderMarkdown() error {
	markdownLock.Lock()
	defer markdownLock.Unlock()

	content, err := os.ReadFile(markdownPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var buf bytes.Buffer
	if err := md.Convert(content, &buf); err != nil {
		return fmt.Errorf("failed to convert markdown: %w", err)
	}

	htmlContent = buf.Bytes()
	return nil
}

func watchFile() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			log.Printf("Error closing watcher: %v", err)
		}
	}()

	// Watch the parent directory instead of the file directly
	// This handles atomic saves where the file is deleted and recreated
	// https://github.com/fsnotify/fsnotify/issues/372
	absPath, err := filepath.Abs(markdownPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}
	watchDir := filepath.Dir(absPath)
	watchFileName := filepath.Base(absPath)

	if err := watcher.Add(watchDir); err != nil {
		log.Fatalf("Failed to watch directory: %v", err)
	}

	// Debouncing: track last reload time to avoid multiple rapid reloads
	var lastReload time.Time
	debounceDelay := 100 * time.Millisecond

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only process events for our specific file
			if filepath.Base(event.Name) != watchFileName {
				continue
			}

			// Handle Write, Create, and Rename events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				// Debounce: skip if we reloaded very recently
				now := time.Now()
				if now.Sub(lastReload) < debounceDelay {
					continue
				}
				lastReload = now

				log.Printf("File changed: %s (event: %s)", event.Name, event.Op)

				// Retry rendering in case file is temporarily missing during atomic save
				var err error
				for range 10 {
					err = renderMarkdown()
					if err == nil {
						break
					}
					// Check if error is "file does not exist" using errors.Is
					if errors.Is(err, os.ErrNotExist) {
						time.Sleep(50 * time.Millisecond)
						continue
					}
					break
				}

				if err != nil {
					log.Printf("Failed to render markdown: %v", err)
					continue
				}
				notifyClients("reload")
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	markdownLock.RLock()
	content := htmlContent
	markdownLock.RUnlock()

	cssContent, err := assets.ReadFile("assets/style.css")
	if err != nil {
		log.Printf("Failed to read CSS: %v", err)
		cssContent = []byte("")
	}

	jsContent, err := assets.ReadFile("assets/script.js")
	if err != nil {
		log.Printf("Failed to read JavaScript: %v", err)
		jsContent = []byte("")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := struct {
		Title   string
		CSS     template.CSS
		Content template.HTML
		JS      template.JS
	}{
		Title:   filepath.Base(markdownPath),
		CSS:     template.CSS(cssContent),
		Content: template.HTML(content),
		JS:      template.JS(jsContent),
	}

	if err := htmlTemplate.Execute(w, data); err != nil {
		log.Printf("Failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := make(chan string)

	clientsLock.Lock()
	sseClients[clientChan] = true
	clientsLock.Unlock()

	defer func() {
		clientsLock.Lock()
		delete(sseClients, clientChan)
		close(clientChan)
		clientsLock.Unlock()
	}()

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-clientChan:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				log.Printf("Error writing SSE message: %v", err)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
				log.Printf("Error writing keepalive: %v", err)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func notifyClients(message string) {
	clientsLock.RLock()
	defer clientsLock.RUnlock()

	for client := range sseClients {
		select {
		case client <- message:
		default:
		}
	}
}
