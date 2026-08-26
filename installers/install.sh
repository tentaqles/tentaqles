#!/bin/sh
set -e
REPO="tentaqles/tentaqles"; BIN_DIR="${TQ_BIN_DIR:-$HOME/.local/bin}"
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "unsupported arch $ARCH"; exit 1;; esac
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
VER=${TAG#v}
URL="https://github.com/$REPO/releases/download/$TAG/tq_${VER}_${OS}_${ARCH}.tar.gz"
mkdir -p "$BIN_DIR"; TMP=$(mktemp -d)
curl -fsSL "$URL" | tar -xz -C "$TMP"
install -m 755 "$TMP/tq" "$BIN_DIR/tq"
echo "Installed tq $VER to $BIN_DIR/tq"
case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "Add $BIN_DIR to your PATH";; esac
echo 'Next: tq init <base-folder>'
