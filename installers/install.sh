#!/bin/sh
set -e
REPO="tentaqles/tentaqles"; BIN_DIR="${TQ_BIN_DIR:-$HOME/.local/bin}"
OS=$(uname -s | tr '[:upper:]' '[:lower:]'); ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "unsupported arch $ARCH"; exit 1;; esac
# /releases/latest ignores pre-releases; fall back to the newest release of any kind.
tag_of() { grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'; }
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | tag_of || true)
[ -n "$TAG" ] || TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" | tag_of)
[ -n "$TAG" ] || { echo "no releases found for $REPO"; exit 1; }
VER=${TAG#v}
URL="https://github.com/$REPO/releases/download/$TAG/tq_${VER}_${OS}_${ARCH}.tar.gz"
mkdir -p "$BIN_DIR"; TMP=$(mktemp -d)
curl -fsSL "$URL" | tar -xz -C "$TMP"
install -m 755 "$TMP/tq" "$BIN_DIR/tq"
echo "Installed tq $VER to $BIN_DIR/tq"
case ":$PATH:" in *":$BIN_DIR:"*) ;; *) echo "Add $BIN_DIR to your PATH";; esac
echo 'Next: tq init <base-folder>'
