# lum

A command-line tool that starts a simple web server that allows displaying a Markdown file rendered as HTML in the
browser.

## Tech stack

- Go
- yuin/goldmark
- yuin/goldmark-highlighting
- fsnotify/fsnotify

## Approach

The user runs `lum <path-to-markdown-file>` in the terminal. The tool validates that the file exists and is readable,
then starts a web server on a specified port (default 6333) and serves the rendered HTML of the Markdown file at the
root URL (`/`). The user can then open their web browser and navigate to `http://localhost:6333` to view the rendered
Markdown.

## Implementation details

- Use Go standard library for HTTP server and CLI argument parsing (no cobra, no chi)
- All assets (CSS, JavaScript) embedded in the binary using `go:embed`
- Live reload implemented using Server-Sent Events (SSE)
- File watching with fsnotify on the specified Markdown file only
- Fail fast at startup if file doesn't exist or isn't readable
- Single binary distribution
- Project lifecycle: alpha (0.0.x) - fail fast and break things

## Features

- Server-side rendering of Markdown to HTML using goldmark
- Syntax highlighting for code blocks using goldmark-highlighting
- Live reload on file changes via SSE
- Single CLI flag: `--port` (default 6333)
- Minimal CSS styling for readability

## CLI Usage

```bash
lum <path-to-markdown-file>              # Start server on default port 6333
lum <path-to-markdown-file> --port 8080  # Start server on custom port
```
