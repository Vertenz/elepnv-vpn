#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

install_xray_on_linux() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    echo "Skipping Xray install: only Linux is supported by the official XTLS installer."
    return
  fi

  if command -v xray >/dev/null 2>&1 || [[ -x "/usr/local/bin/xray" ]]; then
    echo "Xray is already installed."
    return
  fi

  if ! command -v curl >/dev/null 2>&1; then
    echo "error: curl is required to download the official XTLS installer." >&2
    exit 1
  fi

  local installer
  installer="$(mktemp)"
  trap 'rm -f "$installer"' RETURN

  echo "Downloading official XTLS Xray installer..."
  curl -fsSL "https://github.com/XTLS/Xray-install/raw/main/install-release.sh" -o "$installer"

  echo "Installing Xray via official XTLS installer..."
  if [[ "$(id -u)" -eq 0 ]]; then
    bash "$installer" install
  else
    sudo bash "$installer" install
  fi
}

install_xray_on_linux

cd app
npm install
