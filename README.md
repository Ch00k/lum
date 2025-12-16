[![CI](https://github.com/Ch00k/lum/actions/workflows/build.yml/badge.svg)](https://github.com/Ch00k/lum/actions/workflows/build.yml)&nbsp;
[![codecov](https://codecov.io/gh/Ch00k/lum/graph/badge.svg?token=5UwJHZOL5G)](https://codecov.io/gh/Ch00k/lum)&nbsp;
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/Ch00k/lum)](https://github.com/Ch00k/lum/releases/latest)

# lum

A command-line tool that starts a simple web server to display Markdown files rendered as HTML in the browser, with live reload on file changes.

## Features

- **Server-side rendering**: Markdown to HTML conversion with [goldmark](https://github.com/yuin/goldmark)
- **Syntax highlighting**: Code blocks styled with [goldmark-highlighting](https://github.com/yuin/goldmark-highlighting)
- **Live reload**: Automatic browser refresh on file changes via Server-Sent Events (SSE)
- **Multiple file support**: Add multiple Markdown files to a single server instance
- **File watching**: Intelligent file monitoring with [fsnotify](https://github.com/fsnotify/fsnotify)
- **GitHub Flavored Markdown**: Tables, task lists, strikethrough, and more
- **Index page**: Browse all tracked files from a single page
- **Minimal styling**: Clean, readable CSS
- **Single binary**: All assets embedded, no external dependencies

## Installation

Download an executable for your operating system from the [releases page](https://github.com/Ch00k/lum/releases/).

## Usage

### Basic Usage

```bash
# Start server on default port 6333
lum <path-to-markdown-file>
```

The first invocation starts a new server. Subsequent invocations add files to the existing server instance.

### Multiple Files

```bash
# Start server with first file
lum README.md

# Add more files to the same server (in a different terminal)
lum CONTRIBUTING.md
lum docs/API.md
```

All files are accessible from the index page at `http://localhost:6333/`

Each file can also be accessed directly:
- `http://localhost:6333/?file=/absolute/path/to/README.md`
- `http://localhost:6333/?file=/absolute/path/to/CONTRIBUTING.md`

### Viewing Files

- **Index page**: `http://localhost:6333/` - Lists all tracked files
- **Specific file**: `http://localhost:6333/?file=/path/to/file.md`

Pages automatically reload when their source file changes.
