#!/bin/bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_DIR="${AIRVAULT_INSTALL_DIR:-$HOME/.airvault}"
BIN_DIR="$INSTALL_DIR/bin"
BIN_PATH="$BIN_DIR/airvault"
LINK_DIR="${AIRVAULT_LINK_DIR:-$HOME/.local/bin}"

if [ ! -x "$PROJECT_DIR/airvault" ]; then
  echo "Building airvault..."
  (cd "$PROJECT_DIR" && go build -o airvault .)
fi

mkdir -p "$BIN_DIR" "$LINK_DIR"
cp "$PROJECT_DIR/airvault" "$BIN_PATH"
ln -sf "$BIN_PATH" "$LINK_DIR/airvault"

echo "Installed: $BIN_PATH"
echo "Linked: $LINK_DIR/airvault"
case ":$PATH:" in
  *":$LINK_DIR:"*) ;;
  *) echo "Add to PATH: export PATH=\"$LINK_DIR:\$PATH\"" ;;
esac
"$BIN_PATH" version
