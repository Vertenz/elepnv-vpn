#!/bin/sh
# Manual install for xrayd on a fresh Ubuntu/Debian host.
# Assumes you've already run: cd daemon && go build -o ../dist/xrayd ./cmd/xrayd/
set -e

if [ "$(id -u)" -ne 0 ]; then
    echo "install.sh: must run as root (use sudo)" >&2
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${REPO_ROOT}/dist/xrayd"
UNIT="${REPO_ROOT}/daemon/packaging/xrayd.service"

if [ ! -x "$BIN" ]; then
    echo "install.sh: $BIN not found; run 'cd daemon && go build -o ../dist/xrayd ./cmd/xrayd/' first" >&2
    exit 1
fi

# user/group
if ! getent group xrayd >/dev/null 2>&1; then
    addgroup --system xrayd
fi
if ! getent passwd xrayd >/dev/null 2>&1; then
    adduser --system --ingroup xrayd --no-create-home --shell /usr/sbin/nologin xrayd
fi

# binary
install -m 0755 "$BIN" /usr/local/bin/xrayd

# unit
install -m 0644 "$UNIT" /etc/systemd/system/xrayd.service
systemctl daemon-reload

# disable XTLS's own xray.service if present
systemctl list-unit-files --type=service 2>/dev/null | grep -q '^xray\.service' && \
    systemctl disable --now xray.service 2>/dev/null || true

systemctl enable --now xrayd.service

# add invoking user to xrayd group
if [ -n "$SUDO_USER" ] && [ "$SUDO_USER" != "root" ]; then
    adduser "$SUDO_USER" xrayd >/dev/null 2>&1 || true
    echo "Added $SUDO_USER to group xrayd. Log out and back in for it to take effect." >&2
fi

echo "xrayd installed. Check status with: systemctl status xrayd" >&2
