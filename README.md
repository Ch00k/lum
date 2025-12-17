[![CI](https://github.com/Ch00k/lum/actions/workflows/build.yml/badge.svg)](https://github.com/Ch00k/lum/actions/workflows/build.yml)&nbsp;
[![codecov](https://codecov.io/gh/Ch00k/lum/graph/badge.svg?token=5UwJHZOL5G)](https://codecov.io/gh/Ch00k/lum)&nbsp;
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/Ch00k/lum)](https://github.com/Ch00k/lum/releases/latest)

# lum

A command-line tool that starts a simple web server to display Markdown files rendered as HTML in the browser, with live reload on file changes.

## Features

- **Two modes**: One-off mode for quick viewing, daemon mode for multiple files
- **Server-side rendering**: Markdown to HTML conversion with [goldmark](https://github.com/yuin/goldmark)
- **Syntax highlighting**: Code blocks styled with [goldmark-highlighting](https://github.com/yuin/goldmark-highlighting)
- **Live reload**: Automatic browser refresh on file changes via Server-Sent Events (SSE)
- **Multiple file support**: Serve multiple Markdown files from a single daemon instance
- **File watching**: Intelligent file monitoring with [fsnotify](https://github.com/fsnotify/fsnotify)
- **GitHub Flavored Markdown**: Tables, task lists, strikethrough, alerts, and more
- **Index page**: Browse all tracked files from a single page
- **Minimal styling**: Clean, readable CSS
- **Single binary**: All assets embedded, no external dependencies

## Installation

Download an executable for your operating system from the [releases page](https://github.com/Ch00k/lum/releases/).

## Usage

### Command Line Options

```bash
lum [OPTIONS] [FILE]

Options:
  -p, --port PORT     Port to run the server on (default: 6333)
  -d, --daemon        Run as daemon (allows serving multiple files)
  -s, --stop          Stop the running daemon
  -h, --help          Show help message
```

### One-Off Mode

For quickly viewing a single file, just run:

```bash
lum README.md
```

This starts a server that:
- Prints the URL to access the file
- Stays in the foreground (Ctrl+C to stop)
- Serves only the specified file
- No daemon process or log files

If a daemon is already running, the file is automatically added to it instead.

### Daemon Mode

For working with multiple files, start a daemon:

```bash
# Start daemon with no files
lum --daemon

# Or start daemon with an initial file
lum --daemon README.md
```

The daemon:
- Runs in the background (detaches from terminal)
- Logs to `$XDG_RUNTIME_DIR/lum/lum.log` (typically `/run/user/$UID/lum/lum.log`)
- Allows adding multiple files
- Persists until explicitly stopped

### Adding Files to Daemon

With a daemon running, add files simply by running:

```bash
lum CONTRIBUTING.md
lum docs/API.md
```

lum automatically detects the running daemon and adds files to it.

### Viewing Files

- **Index page**: `http://localhost:6333/` - Lists all tracked files
- **Specific file**: `http://localhost:6333/?file=/path/to/file.md`

Pages automatically reload when their source file changes.

### Stopping the Daemon

```bash
lum --stop
# or
lum -s
```

### Custom Port

```bash
# One-off mode on custom port
lum --port 8080 README.md

# Daemon mode on custom port
lum --daemon --port 8080
```
