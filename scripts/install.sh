#!/usr/bin/env bash
set -e

REPO="semihy/seyir"
INSTALL_DIR="/usr/local/bin"
LOCAL_BIN="$HOME/.local/bin"
TMP_DIR=$(mktemp -d)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then ARCH="amd64"; fi
if [ "$ARCH" = "aarch64" ]; then ARCH="arm64"; fi

echo "Installing Seyir..."

# Fetch latest release version
LATEST=$(curl -s https://api.github.com/repos/$REPO/releases/latest | grep tag_name | cut -d '"' -f 4)
if [ -z "$LATEST" ]; then
  echo "❌ Could not fetch latest release info."
  exit 1
fi

echo "Latest version: $LATEST"

# Build download URL
ASSET_URL="https://github.com/$REPO/releases/download/$LATEST/seyir_${OS}_${ARCH}.tar.gz"
echo "⬇️  Downloading $ASSET_URL"
curl -L "$ASSET_URL" -o "$TMP_DIR/seyir.tar.gz"

# Extract binary
tar -xzf "$TMP_DIR/seyir.tar.gz" -C "$TMP_DIR"
# Find the binary (it might be in a subdirectory due to wrap_in_directory)
BINARY_PATH=$(find "$TMP_DIR" -name "seyir" -type f | head -1)
if [ -z "$BINARY_PATH" ]; then
  echo "❌ Could not find seyir binary in archive"
  exit 1
fi
chmod +x "$BINARY_PATH"

# Install binary
if [ -w "$INSTALL_DIR" ]; then
  mv "$BINARY_PATH" "$INSTALL_DIR/seyir"
  DEST="$INSTALL_DIR/seyir"
else
  mkdir -p "$LOCAL_BIN"
  mv "$BINARY_PATH" "$LOCAL_BIN/seyir"
  DEST="$LOCAL_BIN/seyir"
  if ! echo "$PATH" | grep -q "$LOCAL_BIN"; then
    echo "⚠️  Add $LOCAL_BIN to your PATH:"
    echo "    export PATH=\"\$PATH:$LOCAL_BIN\""
  fi
fi

echo "✅ Installed Seyir to: $DEST"
"$DEST" --version || echo "Run 'seyir --version' to verify installation."

rm -rf "$TMP_DIR"
