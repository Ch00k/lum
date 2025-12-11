package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

//go:embed assets/*
var assets embed.FS

// FileState holds the state for a single tracked markdown file
type FileState struct {
	path        string
	htmlContent []byte
	contentLock sync.RWMutex
	watcher     *fsnotify.Watcher
	sseClients  map[chan string]bool
	clientsLock sync.RWMutex
}

var (
	// Version is set via ldflags during build
	Version = "dev"

	files     = make(map[string]*FileState)
	filesLock sync.RWMutex

	indexSSEClients     = make(map[chan string]bool)
	indexSSEClientsLock sync.RWMutex

	fileTemplate  *template.Template
	indexTemplate *template.Template
)

func init() {
	// Load file template
	tmplContent, err := assets.ReadFile("assets/file.html")
	if err != nil {
		log.Fatalf("Failed to read file template: %v", err)
	}
	fileTemplate = template.Must(template.New("file").Parse(string(tmplContent)))

	// Load index template
	indexContent, err := assets.ReadFile("assets/index.html")
	if err != nil {
		log.Fatalf("Failed to read index template: %v", err)
	}
	indexTemplate = template.Must(template.New("index").Parse(string(indexContent)))
}

// addFile adds a new file to the tracked files, renders it, and starts watching it.
// If the file is already tracked, this is a no-op and returns nil.
func addFile(filePath string) error {
	filesLock.Lock()
	// Check if file is already tracked
	if _, exists := files[filePath]; exists {
		filesLock.Unlock()
		return nil
	}

	// Create new file state
	fileState := &FileState{
		path:       filePath,
		sseClients: make(map[chan string]bool),
	}
	files[filePath] = fileState
	filesLock.Unlock()

	// Render the file
	if err := renderMarkdown(filePath); err != nil {
		filesLock.Lock()
		delete(files, filePath)
		filesLock.Unlock()
		return fmt.Errorf("failed to render file: %w", err)
	}

	// Start watching the file
	if err := startWatchingFile(filePath); err != nil {
		filesLock.Lock()
		delete(files, filePath)
		filesLock.Unlock()
		return fmt.Errorf("failed to start watching file: %w", err)
	}

	// Notify index page clients that a new file was added
	notifyIndexClients("reload")

	return nil
}

// handleIndex serves either a specific file (if ?file= query param is present),
// an index page listing all tracked files, or static assets relative to the Markdown file
func handleIndex(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")

	// If path is not "/" and file parameter is present, try to serve as static asset
	if r.URL.Path != "/" && filePath != "" {
		handleStaticAsset(w, r, filePath)
		return
	}

	// Path must be "/" for HTML pages
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// If no file specified, show index page
	if filePath == "" {
		renderIndexPage(w, r)
		return
	}

	// Look up the file
	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	// Read content with the file's lock
	fileState.contentLock.RLock()
	content := fileState.htmlContent
	fileState.contentLock.RUnlock()

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
		File    string
	}{
		Title:   filepath.Base(filePath),
		CSS:     template.CSS(cssContent),
		Content: template.HTML(content),
		JS:      template.JS(jsContent),
		File:    filePath,
	}

	if err := fileTemplate.Execute(w, data); err != nil {
		log.Printf("Failed to execute file template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleStaticAsset serves a static file relative to the Markdown file's directory
func handleStaticAsset(w http.ResponseWriter, r *http.Request, markdownFilePath string) {
	// Verify the markdown file is tracked
	filesLock.RLock()
	_, exists := files[markdownFilePath]
	filesLock.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	// Get the requested asset path
	// URL paths always start with /, so strip it first
	assetPath := r.URL.Path[1:]
	if assetPath == "" {
		http.NotFound(w, r)
		return
	}

	markdownDir := filepath.Dir(markdownFilePath)

	// Try two interpretations:
	// 1. Relative to markdown directory (most common)
	// 2. Absolute filesystem path (for explicit absolute paths in markdown)

	relativePath := filepath.Join(markdownDir, assetPath)
	relativePath = filepath.Clean(relativePath)

	absolutePath := filepath.Clean("/" + assetPath)

	// Check which interpretation is valid (within directory AND file exists)
	var fullAssetPath string
	relativeValid := isPathWithinDirectory(relativePath, markdownDir)
	absoluteValid := isPathWithinDirectory(absolutePath, markdownDir)

	// Prefer the interpretation where the file actually exists
	_, relativeExists := os.Stat(relativePath)
	_, absoluteExists := os.Stat(absolutePath)

	if relativeValid && relativeExists == nil {
		// Relative interpretation: file exists
		fullAssetPath = relativePath
	} else if absoluteValid && absoluteExists == nil {
		// Absolute interpretation: file exists
		fullAssetPath = absolutePath
	} else if relativeValid {
		// Fall back to relative even if file doesn't exist (will 404 later)
		fullAssetPath = relativePath
	} else if absoluteValid {
		// Fall back to absolute even if file doesn't exist (will 404 later)
		fullAssetPath = absolutePath
	} else {
		// Neither interpretation is within allowed directory - return 404 to avoid leaking info
		http.NotFound(w, r)
		return
	}

	// Check if file exists
	info, err := os.Stat(fullAssetPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Don't serve directories - return 404 to avoid leaking info
	if info.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Serve the file
	http.ServeFile(w, r, fullAssetPath)
}

// renderIndexPage renders the index page listing all tracked files
func renderIndexPage(w http.ResponseWriter, r *http.Request) {
	filesLock.RLock()
	defer filesLock.RUnlock()

	type FileInfo struct {
		Name string
		Path string
	}

	var fileList []FileInfo
	for path := range files {
		fileList = append(fileList, FileInfo{
			Name: filepath.Base(path),
			Path: path,
		})
	}

	cssContent, err := assets.ReadFile("assets/style.css")
	if err != nil {
		log.Printf("Failed to read CSS: %v", err)
		cssContent = []byte("")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := struct {
		Files []FileInfo
		CSS   template.CSS
	}{
		Files: fileList,
		CSS:   template.CSS(cssContent),
	}

	if err := indexTemplate.Execute(w, data); err != nil {
		log.Printf("Failed to execute index template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleSSE handles Server-Sent Events for a specific file
func handleSSE(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := make(chan string)

	fileState.clientsLock.Lock()
	fileState.sseClients[clientChan] = true
	fileState.clientsLock.Unlock()

	defer func() {
		fileState.clientsLock.Lock()
		delete(fileState.sseClients, clientChan)
		close(clientChan)
		fileState.clientsLock.Unlock()
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

// notifyClients sends a message to all SSE clients watching a specific file
func notifyClients(filePath, message string) {
	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		return
	}

	fileState.clientsLock.RLock()
	defer fileState.clientsLock.RUnlock()

	for client := range fileState.sseClients {
		select {
		case client <- message:
		default:
		}
	}
}

// handleIndexSSE handles Server-Sent Events for the index page
func handleIndexSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientChan := make(chan string)

	indexSSEClientsLock.Lock()
	indexSSEClients[clientChan] = true
	indexSSEClientsLock.Unlock()

	defer func() {
		indexSSEClientsLock.Lock()
		delete(indexSSEClients, clientChan)
		close(clientChan)
		indexSSEClientsLock.Unlock()
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

// notifyIndexClients sends a message to all SSE clients watching the index page
func notifyIndexClients(message string) {
	indexSSEClientsLock.RLock()
	defer indexSSEClientsLock.RUnlock()

	for client := range indexSSEClients {
		select {
		case client <- message:
		default:
		}
	}
}

// isPathWithinDirectory checks if path is within dir or its subdirectories
func isPathWithinDirectory(path, dir string) bool {
	// Get absolute paths
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	// Ensure absDir ends with separator for proper prefix matching
	if !os.IsPathSeparator(absDir[len(absDir)-1]) {
		absDir += string(filepath.Separator)
	}

	// Check if absPath starts with absDir (meaning it's within the directory tree)
	// Also allow exact match with the directory itself
	return absPath == absDir[:len(absDir)-1] || len(absPath) >= len(absDir) && absPath[:len(absDir)] == absDir
}
