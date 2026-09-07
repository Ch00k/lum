package main

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

//go:embed assets/*
var assets embed.FS

// FileState holds the state for a single tracked markdown file
type FileState struct {
	path        string
	source      []byte
	htmlContent []byte
	refs        references
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

	fileTemplate   *template.Template
	indexTemplate  *template.Template
	exportTemplate *template.Template
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

	// Load export template
	exportContent, err := assets.ReadFile("assets/export.html")
	if err != nil {
		log.Fatalf("Failed to read export template: %v", err)
	}
	exportTemplate = template.Must(template.New("export").Parse(string(exportContent)))
}

// addFile renders a new file, adds it to the tracked files, and starts
// watching it. If the file is already tracked, this is a no-op and returns nil.
//
// The file is rendered before it is published to the tracked files, so every
// state a request can find already has its content and references. Callers
// racing to add the same file both render it and the loser discards its work,
// which costs a redundant render but keeps a half-initialized file from ever
// being served.
func addFile(filePath string) error {
	fileState := &FileState{
		path:       filePath,
		sseClients: make(map[chan string]bool),
	}

	if err := renderInto(fileState); err != nil {
		return fmt.Errorf("failed to render file: %w", err)
	}

	filesLock.Lock()
	if _, exists := files[filePath]; exists {
		filesLock.Unlock()
		return nil
	}
	files[filePath] = fileState
	filesLock.Unlock()

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

// trackedFile returns the state for a tracked file. A file that is not tracked
// yet but is linked from a document that is gets added on the spot, so
// following a link between documents serves the target without a separate lum
// invocation. Files discovered this way are rendered and watched from the
// moment they are first opened.
func trackedFile(filePath string) (*FileState, bool) {
	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if exists {
		return fileState, true
	}

	if !isLinkedDocument(filePath) {
		return nil, false
	}

	if err := addFile(filePath); err != nil {
		log.Printf("Failed to add linked document %s: %v", filePath, err)
		return nil, false
	}

	filesLock.RLock()
	fileState, exists = files[filePath]
	filesLock.RUnlock()

	return fileState, exists
}

// handleIndex serves either a specific file (if ?file= query param is present)
// or an index page listing all tracked files
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// If no file specified, show index page
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		renderIndexPage(w, r)
		return
	}

	// Look up the file
	fileState, exists := trackedFile(filePath)
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

// scriptCloseTag matches the </script sequence in any case; HTML treats
// script end tags case-insensitively.
var scriptCloseTag = regexp.MustCompile(`(?i)<(/script)`)

// handleExport serves a self-contained HTML snapshot of a tracked file:
// embedded CSS, the rendered HTML, and the original Markdown source inside
// <script type="text/markdown">. The </script sequence (in any case) is
// escaped to <\/script, preserving the original case, so the script block
// can't be closed early by the embedded content.
// The width query parameter records the viewport width selected in the file
// view, baking the corresponding container class into the snapshot.
func handleExport(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	filesLock.RLock()
	fileState, exists := files[filePath]
	filesLock.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	fileState.contentLock.RLock()
	content := fileState.htmlContent
	source := fileState.source
	fileState.contentLock.RUnlock()

	cssContent, err := assets.ReadFile("assets/style.css")
	if err != nil {
		log.Printf("Failed to read CSS: %v", err)
		cssContent = []byte("")
	}

	escapedSource := scriptCloseTag.ReplaceAll(source, []byte("<\\$1"))

	containerClass := "container"
	if r.URL.Query().Get("width") == "1200" {
		containerClass += " w1200"
	}

	baseName := filepath.Base(filePath)
	downloadName := strings.TrimSuffix(baseName, filepath.Ext(baseName)) + ".html"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, downloadName))

	data := struct {
		Title          string
		CSS            template.CSS
		Content        template.HTML
		Source         template.HTML
		ContainerClass string
	}{
		Title:          baseName,
		CSS:            template.CSS(cssContent),
		Content:        template.HTML(content),
		Source:         template.HTML(escapedSource),
		ContainerClass: containerClass,
	}

	if err := exportTemplate.Execute(w, data); err != nil {
		log.Printf("Failed to execute export template: %v", err)
	}
}

// handleStaticAsset serves a file referenced by a tracked Markdown file: the
// document in the file parameter, and the asset in the path parameter as the
// link transformer resolved it while rendering that document. Only paths the
// document actually references are served.
func handleStaticAsset(w http.ResponseWriter, r *http.Request) {
	markdownFilePath := r.URL.Query().Get("file")
	assetPath := r.URL.Query().Get("path")

	if markdownFilePath == "" || assetPath == "" {
		http.Error(w, "Missing file or path parameter", http.StatusBadRequest)
		return
	}

	filesLock.RLock()
	fileState, exists := files[markdownFilePath]
	filesLock.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	fileState.contentLock.RLock()
	referenced := fileState.refs.assets[assetPath]
	fileState.contentLock.RUnlock()

	if !referenced {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(assetPath)
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

	http.ServeFile(w, r, assetPath)
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
