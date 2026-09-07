package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// trackDocument writes content to path, tracks it, and renders it, so the test
// sees the same state a served document has. Tracking is undone when the test
// ends.
func trackDocument(t *testing.T, path, content string) *FileState {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	filesLock.Lock()
	files[path] = &FileState{
		path:       path,
		sseClients: make(map[chan string]bool),
	}
	filesLock.Unlock()

	t.Cleanup(func() {
		filesLock.Lock()
		if fileState, ok := files[path]; ok {
			if fileState.watcher != nil {
				_ = fileState.watcher.Close()
			}
			delete(files, path)
		}
		filesLock.Unlock()
	})

	if err := renderMarkdown(path); err != nil {
		t.Fatalf("Failed to render %s: %v", path, err)
	}

	filesLock.RLock()
	fileState := files[path]
	filesLock.RUnlock()

	return fileState
}

// renderedHTML returns the HTML currently rendered for a tracked file.
func renderedHTML(fileState *FileState) string {
	fileState.contentLock.RLock()
	defer fileState.contentLock.RUnlock()

	return string(fileState.htmlContent)
}

// attrValue is a URL as it appears in an HTML attribute, with the ampersand
// between query parameters escaped the way the renderer writes it.
func attrValue(url string) string {
	return strings.ReplaceAll(url, "&", "&amp;")
}

// TestRewriteDocumentLinks checks that links to local Markdown files are
// rewritten to the URL of the page for that file, links to other local files
// to the asset URL for them, and that links pointing anywhere else are left
// untouched.
func TestRewriteDocumentLinks(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	subDir := filepath.Join(docsDir, "sub")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Targets around the document: beside it, below it, and above it
	sibling := filepath.Join(docsDir, "sibling.md")
	nested := filepath.Join(subDir, "nested.md")
	parent := filepath.Join(tmpDir, "parent.markdown")
	notes := filepath.Join(docsDir, "notes.pdf")
	for _, path := range []string{sibling, nested, parent, notes} {
		if err := os.WriteFile(path, []byte("# Target"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	spaced := filepath.Join(docsDir, "with space.md")
	if err := os.WriteFile(spaced, []byte("# Spaced"), 0o600); err != nil {
		t.Fatal(err)
	}

	doc := filepath.Join(docsDir, "index.md")
	content := strings.Join([]string{
		"[sibling](./sibling.md)",
		"[bare](sibling.md)",
		"[nested](sub/nested.md)",
		"[parent](../parent.markdown)",
		"[absolute](" + sibling + ")",
		"[fragment](./sibling.md#a-heading)",
		"[spaced](with%20space.md)",
		"[external](https://example.com/doc.md)",
		"[protocol relative](//example.com/doc.md)",
		"[mail](mailto:someone@example.com)",
		"[anchor](#local-heading)",
		"[other file](./notes.pdf)",
		"[missing](./missing.md)",
	}, "\n\n")

	fileState := trackDocument(t, doc, content)
	html := renderedHTML(fileState)

	rewritten := []struct {
		name string
		href string
	}{
		{"Sibling", documentURL(sibling, "")},
		{"BareSibling", documentURL(sibling, "")},
		{"Nested", documentURL(nested, "")},
		{"Parent", documentURL(parent, "")},
		{"Absolute", documentURL(sibling, "")},
		{"Fragment", documentURL(sibling, "a-heading")},
		{"Spaced", documentURL(spaced, "")},
		{"NonMarkdownFile", attrValue(assetURL(notes, doc, ""))},
	}
	for _, tc := range rewritten {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(html, `href="`+tc.href+`"`) {
				t.Errorf("Expected href %q in rendered HTML:\n%s", tc.href, html)
			}
		})
	}

	untouched := []struct {
		name string
		href string
	}{
		{"ExternalURL", "https://example.com/doc.md"},
		{"ProtocolRelativeURL", "//example.com/doc.md"},
		{"MailtoURL", "mailto:someone@example.com"},
		{"LocalAnchor", "#local-heading"},
		{"MissingFile", "./missing.md"},
	}
	for _, tc := range untouched {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(html, `href="`+tc.href+`"`) {
				t.Errorf("Expected href %q to be left alone in rendered HTML:\n%s", tc.href, html)
			}
		})
	}

	t.Run("LinkedDocumentsRecorded", func(t *testing.T) {
		for _, path := range []string{sibling, nested, parent, spaced} {
			if !isLinkedDocument(path) {
				t.Errorf("Expected %s to be recorded as a linked document", path)
			}
		}

		if isLinkedDocument(filepath.Join(docsDir, "missing.md")) {
			t.Error("A link to a file that does not exist should not be recorded")
		}

		if isLinkedDocument(notes) {
			t.Error("A link to a file that is not Markdown should not be recorded as a document")
		}
	})

	t.Run("LinkedAttachmentRecordedAsAsset", func(t *testing.T) {
		fileState.contentLock.RLock()
		defer fileState.contentLock.RUnlock()

		if !fileState.refs.assets[notes] {
			t.Errorf("Expected %s to be recorded as a referenced asset", notes)
		}
	})
}

// TestRewriteImageLinks checks that images pointing at local files get the
// static asset URL for that file, and that remote and inline images are left
// untouched.
func TestRewriteImageLinks(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	beside := filepath.Join(docsDir, "beside.png")
	above := filepath.Join(tmpDir, "above.png")
	sprite := filepath.Join(docsDir, "sprite.svg")
	for _, path := range []string{beside, above, sprite} {
		if err := os.WriteFile(path, []byte("fake image data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	doc := filepath.Join(docsDir, "index.md")
	content := strings.Join([]string{
		"![beside](./beside.png)",
		"![above](../above.png)",
		"![view](./sprite.svg#icon-view)",
		"![remote](https://example.com/logo.png)",
		"![inline](data:image/gif;base64,R0lGOD)",
	}, "\n\n")

	fileState := trackDocument(t, doc, content)
	html := renderedHTML(fileState)

	expected := []struct {
		name string
		src  string
	}{
		{"ImageBesideDocument", attrValue(assetURL(beside, doc, ""))},
		{"ImageAboveDocument", attrValue(assetURL(above, doc, ""))},
		{"ImageFragmentPreserved", attrValue(assetURL(sprite, doc, "icon-view"))},
		{"RemoteImage", "https://example.com/logo.png"},
		{"InlineImage", "data:image/gif;base64,R0lGOD"},
	}
	for _, tc := range expected {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(html, `src="`+tc.src+`"`) {
				t.Errorf("Expected src %q in rendered HTML:\n%s", tc.src, html)
			}
		})
	}

	t.Run("AssetsRecorded", func(t *testing.T) {
		fileState.contentLock.RLock()
		defer fileState.contentLock.RUnlock()

		for _, path := range []string{beside, above, sprite} {
			if !fileState.refs.assets[path] {
				t.Errorf("Expected %s to be recorded as a referenced asset", path)
			}
		}
	})
}

// TestReferencesReplacedOnRerender checks that references from a previous
// version of a document don't outlive it: a link removed from the file stops
// being followable.
func TestReferencesReplacedOnRerender(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.md")
	if err := os.WriteFile(target, []byte("# Target"), 0o600); err != nil {
		t.Fatal(err)
	}

	doc := filepath.Join(tmpDir, "index.md")
	trackDocument(t, doc, "[target](./target.md)")

	if !isLinkedDocument(target) {
		t.Fatal("Expected the target to be linked after the first render")
	}

	if err := os.WriteFile(doc, []byte("# No links here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renderMarkdown(doc); err != nil {
		t.Fatal(err)
	}

	if isLinkedDocument(target) {
		t.Error("Expected the target to stop being linked once the link was removed")
	}
}

// TestServeLinkedDocument checks that following a link to an untracked
// document serves it, tracks it, and makes the documents it links to
// followable in turn, while a document nothing links to stays unreachable.
func TestServeLinkedDocument(t *testing.T) {
	tmpDir := t.TempDir()

	unreferenced := filepath.Join(tmpDir, "unreferenced.md")
	if err := os.WriteFile(unreferenced, []byte("# Unreferenced"), 0o600); err != nil {
		t.Fatal(err)
	}

	deep := filepath.Join(tmpDir, "deep.md")
	if err := os.WriteFile(deep, []byte("# Deep"), 0o600); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(tmpDir, "linked.md")
	if err := os.WriteFile(linked, []byte("# Linked\n\n[deep](./deep.md)"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupTracking(t, linked)
	cleanupTracking(t, deep)

	trackDocument(t, filepath.Join(tmpDir, "index.md"), "[linked](./linked.md)")

	t.Run("ServesLinkedDocument", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?file="+linked, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Linked") {
			t.Error("Expected the linked document's content in the response")
		}

		filesLock.RLock()
		_, tracked := files[linked]
		filesLock.RUnlock()

		if !tracked {
			t.Error("Expected the linked document to be tracked after being served")
		}
	})

	t.Run("ServesDocumentLinkedFromLinkedDocument", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?file="+deep, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("Return404ForUnreferencedDocument", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?file="+unreferenced, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for an unreferenced document, got %d", w.Code)
		}
	})
}

// TestServeReferencedPathOutsideDocumentTree pins the trust boundary the
// README describes: a document decides what lum serves on its behalf, so a
// path it references is served even when it lives nowhere near the document.
// Confining references to a root would be a deliberate change, not a fix.
func TestServeReferencedPathOutsideDocumentTree(t *testing.T) {
	tmpDir := t.TempDir()
	docsDir := filepath.Join(tmpDir, "docs")
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(docsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}

	// An asset and a document in an unrelated directory tree
	outsideAsset := filepath.Join(elsewhere, "logo.png")
	if err := os.WriteFile(outsideAsset, []byte("fake png data"), 0o600); err != nil {
		t.Fatal(err)
	}

	outsideDoc := filepath.Join(elsewhere, "outside.md")
	if err := os.WriteFile(outsideDoc, []byte("# Outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupTracking(t, outsideDoc)

	doc := filepath.Join(docsDir, "index.md")
	trackDocument(t, doc, "![logo]("+outsideAsset+")\n\n[outside]("+outsideDoc+")")

	t.Run("Asset", func(t *testing.T) {
		req := httptest.NewRequest("GET", assetURL(outsideAsset, doc, ""), nil)
		w := httptest.NewRecorder()

		handleStaticAsset(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("Document", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?file="+outsideDoc, nil)
		w := httptest.NewRecorder()

		handleIndex(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

// TestConcurrentFirstOpenOfLinkedDocument checks that requests racing to open
// a document that is not tracked yet all get its content. The file is rendered
// before it is published to the tracked files, so no request can find a state
// that has none.
func TestConcurrentFirstOpenOfLinkedDocument(t *testing.T) {
	tmpDir := t.TempDir()

	linked := filepath.Join(tmpDir, "linked.md")
	if err := os.WriteFile(linked, []byte("# Concurrent Content"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupTracking(t, linked)

	trackDocument(t, filepath.Join(tmpDir, "index.md"), "[linked](./linked.md)")

	const requests = 8

	var (
		wg     sync.WaitGroup
		codes  [requests]int
		bodies [requests]string
		start  = make(chan struct{})
	)

	for i := range requests {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			req := httptest.NewRequest("GET", "/?file="+linked, nil)
			w := httptest.NewRecorder()

			handleIndex(w, req)

			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}()
	}

	close(start)
	wg.Wait()

	for i := range requests {
		if codes[i] != http.StatusOK {
			t.Errorf("Request %d: expected status 200, got %d", i, codes[i])
		}
		if !strings.Contains(bodies[i], "Concurrent Content") {
			t.Errorf("Request %d: expected the document's content in the response", i)
		}
	}
}

// cleanupTracking removes a file from tracking when the test ends, for files
// that get tracked by the code under test rather than by the test itself.
func cleanupTracking(t *testing.T, path string) {
	t.Helper()

	t.Cleanup(func() {
		filesLock.Lock()
		if fileState, ok := files[path]; ok {
			if fileState.watcher != nil {
				_ = fileState.watcher.Close()
			}
			delete(files, path)
		}
		filesLock.Unlock()
	})
}
