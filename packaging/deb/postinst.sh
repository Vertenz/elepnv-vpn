#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  exit 0
fi

if command -v xray >/dev/null 2>&1 || [[ -x "/usr/local/bin/xray" ]]; then
  echo "Xray is already installed."
  exit 0
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required to install Xray via the official XTLS installer." >&2
  exit 1
fi

installer="$(mktemp)"
cleanup() {
  rm -f "$installer"
}
trap cleanup EXIT

echo "Downloading official XTLS Xray installer..."
curl -fsSL "https://github.com/XTLS/Xray-install/raw/main/install-release.sh" -o "$installer"

echo "Installing Xray via official XTLS installer..."
bash "$installer" install
