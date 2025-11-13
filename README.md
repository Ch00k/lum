# lum

A command-line tool that starts a simple web server to display Markdown files rendered as HTML in the browser, with live
reload on file changes.

## Features

- Server-side Markdown rendering with [goldmark](https://github.com/yuin/goldmark)
- Syntax highlighting for code blocks using [goldmark-highlighting](https://github.com/yuin/goldmark-highlighting)
- Live reload on file changes via Server-Sent Events (SSE)
- File watching with [fsnotify](https://github.com/fsnotify/fsnotify)
- GitHub Flavored Markdown support (tables, task lists, strikethrough)
- Minimal, clean CSS styling
- Single binary with embedded assets

## Installation

```bash
go install github.com/Ch00k/lum@latest
```

Or build from source:

```bash
git clone https://github.com/Ch00k/lum.git
cd lum
go build
```

## Usage

```bash
lum <path-to-markdown-file>              # Start server on default port 6333
lum --port 8080 <path-to-markdown-file>  # Start server on custom port
```

Then open your browser to `http://localhost:6333` (or your custom port).

The page will automatically reload whenever the Markdown file is modified.

## Example

```bash
lum README.md
```

Then navigate to `http://localhost:6333` in your browser.
