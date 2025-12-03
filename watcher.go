package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// startWatchingFile creates a file watcher for the specified file and starts a goroutine
// to handle file change events
func startWatchingFile(filePath string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Store watcher in file state
	filesLock.Lock()
	fileState, exists := files[filePath]
	if !exists {
		filesLock.Unlock()
		if err := watcher.Close(); err != nil {
			log.Printf("Failed to close watcher: %v", err)
		}
		return errors.New("file not in tracked files")
	}
	fileState.watcher = watcher
	filesLock.Unlock()

	// Watch the parent directory instead of the file directly
	// This handles atomic saves where the file is deleted and recreated
	// https://github.com/fsnotify/fsnotify/issues/372
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		if closeErr := watcher.Close(); closeErr != nil {
			log.Printf("Failed to close watcher: %v", closeErr)
		}
		return err
	}
	watchDir := filepath.Dir(absPath)
	watchFileName := filepath.Base(absPath)

	if err := watcher.Add(watchDir); err != nil {
		if closeErr := watcher.Close(); closeErr != nil {
			log.Printf("Failed to close watcher: %v", closeErr)
		}
		return err
	}

	// Start watching in a goroutine
	go func() {
		defer func() {
			if err := watcher.Close(); err != nil {
				log.Printf("Failed to close watcher: %v", err)
			}
		}()

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
						err = renderMarkdown(filePath)
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
					notifyClients(filePath, "reload")
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error for %s: %v", filePath, err)
			}
		}
	}()

	return nil
}
