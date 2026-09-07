package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// references holds the local paths a document points at, resolved to absolute
// paths while rendering. They are the only paths the server will serve on that
// document's behalf: docs are rendered on demand, assets are served as files.
type references struct {
	docs   map[string]bool
	assets map[string]bool
}

func newReferences() references {
	return references{
		docs:   make(map[string]bool),
		assets: make(map[string]bool),
	}
}

// documentContext carries the path of the document being rendered into the
// link transformer, and the references it found back out.
type documentContext struct {
	path string
	refs references
}

var documentContextKey = parser.NewContextKey()

// linkTransformer rewrites links and images that point at local files so they
// resolve against the server rather than the URL of the page they appear on.
// A relative destination in the Markdown is relative to the document's own
// directory, but the browser resolves it against "/?file=...", which drops the
// query string. Rewriting each destination to a URL that names the document
// keeps the reference working.
type linkTransformer struct{}

// Transform implements parser.ASTTransformer
func (t *linkTransformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	doc, ok := pc.Get(documentContextKey).(*documentContext)
	if !ok {
		return
	}

	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := n.(type) {
		case *ast.Link:
			t.rewriteLink(n, doc)
		case *ast.Image:
			t.rewriteImage(n, doc)
		}

		return ast.WalkContinue, nil
	})
}

// rewriteLink points a link at a local Markdown file to that file's own page.
// Links to anything else are left alone.
func (t *linkTransformer) rewriteLink(link *ast.Link, doc *documentContext) {
	path, fragment := resolveReference(string(link.Destination), doc.path)
	if path == "" || !isMarkdownPath(path) {
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}

	doc.refs.docs[path] = true
	link.Destination = []byte(documentURL(path, fragment))
}

// rewriteImage points an image at a local file to the static asset URL for
// that file, naming the document it appears in.
func (t *linkTransformer) rewriteImage(image *ast.Image, doc *documentContext) {
	path, _ := resolveReference(string(image.Destination), doc.path)
	if path == "" {
		return
	}

	doc.refs.assets[path] = true
	image.Destination = []byte(assetURL(path, doc.path))
}

// resolveReference resolves a destination to an absolute path, relative
// destinations against the directory of the document they appear in. It
// returns an empty path for destinations that don't name a local file:
// absolute and protocol-relative URLs, and bare fragments.
func resolveReference(destination, documentPath string) (path, fragment string) {
	u, err := url.Parse(destination)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" || u.Path == "" {
		return "", ""
	}

	path = u.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(documentPath), path)
	}

	return filepath.Clean(path), u.Fragment
}

func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

// documentURL builds the URL of the page for a Markdown file.
func documentURL(path, fragment string) string {
	u := url.URL{
		Path:     "/",
		RawQuery: url.Values{"file": {path}}.Encode(),
		Fragment: fragment,
	}
	return u.String()
}

// assetURL builds the URL for an asset referenced by a document. The path of
// the asset is carried in the URL path and the document that references it in
// the query, which is what handleStaticAsset checks the asset against.
func assetURL(assetPath, documentPath string) string {
	u := url.URL{
		Path:     assetPath,
		RawQuery: url.Values{"file": {documentPath}}.Encode(),
	}
	return u.String()
}

// isLinkedDocument reports whether path is linked from any tracked document.
func isLinkedDocument(path string) bool {
	filesLock.RLock()
	defer filesLock.RUnlock()

	for _, fileState := range files {
		fileState.contentLock.RLock()
		linked := fileState.refs.docs[path]
		fileState.contentLock.RUnlock()

		if linked {
			return true
		}
	}

	return false
}
