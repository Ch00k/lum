#!/bin/sh
set -e

REPO="Ch00k/lum"

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required but not installed" >&2
    exit 1
fi

OS=$(uname -s)
case $OS in
Linux)
    OS=linux
    ;;
Darwin)
    OS=darwin
    ;;
*)
    echo "Error: unsupported operating system: $OS" >&2
    exit 1
    ;;
esac

ARCH=$(uname -m)
case $ARCH in
x86_64 | amd64)
    ARCH=amd64
    ;;
aarch64 | arm64)
    ARCH=arm64
    ;;
*)
    echo "Error: unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Pick the first candidate directory that exists, is writable, and is in PATH
INSTALL_DIR=""
for DIR in "$HOME/.local/bin" "$HOME/bin" /usr/local/bin; do
    case ":$PATH:" in
    *":$DIR:"*) ;;
    *)
        continue
        ;;
    esac
    if [ -d "$DIR" ] && [ -w "$DIR" ]; then
        INSTALL_DIR=$DIR
        break
    fi
done

if [ -z "$INSTALL_DIR" ]; then
    echo "Error: no writable directory found in PATH" >&2
    echo "Checked: $HOME/.local/bin, $HOME/bin, /usr/local/bin" >&2
    exit 1
fi

URL="https://github.com/$REPO/releases/latest/download/lum-$OS-$ARCH"

echo "Downloading $URL"
# The temp file lives in the install directory so the final mv is an atomic
# rename, which safely replaces an existing (even currently running) binary
TMP_FILE=$(mktemp "$INSTALL_DIR/.lum.XXXXXX")
trap 'rm -f "$TMP_FILE"' EXIT
curl -fsSL -o "$TMP_FILE" "$URL"

chmod 755 "$TMP_FILE"
mv "$TMP_FILE" "$INSTALL_DIR/lum"
trap - EXIT

echo "Installed lum to $INSTALL_DIR/lum"
