#!/bin/sh
set -e

# ---------------------------------------------------------------------------
# Section 1: Install xray-core binary (via official XTLS installer) if absent.
# ---------------------------------------------------------------------------
if [ "$(uname -s)" = "Linux" ]; then
    if command -v xray >/dev/null 2>&1 || [ -x "/usr/local/bin/xray" ]; then
        echo "Xray is already installed."
    else
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
        sh "$installer" install
    fi
fi

# ---------------------------------------------------------------------------
# Section 2: Create xrayd system user/group if absent.
# ---------------------------------------------------------------------------
if ! getent group xrayd >/dev/null 2>&1; then
    addgroup --system xrayd
fi
if ! getent passwd xrayd >/dev/null 2>&1; then
    adduser --system --ingroup xrayd --no-create-home --shell /usr/sbin/nologin xrayd
fi

# ---------------------------------------------------------------------------
# Section 3: Disable XTLS installer's own xray.service if present
#             (would fight xrayd for port 10808).
# ---------------------------------------------------------------------------
if [ -d /run/systemd/system ] && systemctl list-unit-files --type=service 2>/dev/null | grep -q '^xray\.service'; then
    systemctl disable --now xray.service 2>/dev/null || true
fi
if [ -d /run/systemd/system ] && systemctl list-unit-files --type=service 2>/dev/null | grep -q '^xray@\.service'; then
    systemctl disable 'xray@*.service' 2>/dev/null || true
fi

# ---------------------------------------------------------------------------
# Section 4: Reload systemd + enable xrayd.
# ---------------------------------------------------------------------------
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable --now xrayd.service
fi

# ---------------------------------------------------------------------------
# Section 5: Best-effort — add the invoking sudo user to the xrayd group
#             so the renderer can talk to the daemon without re-login.
# ---------------------------------------------------------------------------
if [ -n "$SUDO_USER" ] && [ "$SUDO_USER" != "root" ]; then
    adduser "$SUDO_USER" xrayd >/dev/null 2>&1 || true
    echo "Added $SUDO_USER to group xrayd. Log out and back in for it to take effect." >&2
fi

exit 0
