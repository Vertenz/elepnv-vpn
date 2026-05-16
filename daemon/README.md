# xrayd — elepn privileged helper daemon

A long-running Go daemon that supervises one `xray-core` child at a time on
behalf of the elepn renderer. Communicates over a Unix socket using
newline-delimited JSON-RPC 2.0.

Full design: [`docs/superpowers/specs/2026-05-15-xrayd-daemon-backbone-design.md`](../docs/superpowers/specs/2026-05-15-xrayd-daemon-backbone-design.md)

## Building

```bash
go build -o ../dist/xrayd ./cmd/xrayd
```

For a versioned build:

```bash
go build \
  -ldflags="-X elepn/daemon/internal/version.Version=$(git describe --tags --always)" \
  -o ../dist/xrayd ./cmd/xrayd
```

## Running locally (development)

You do not need root for the v1 backbone — there are no capabilities. You DO
need to either run as a member of the `xrayd` group or as root.

```bash
# Create the group locally if needed
sudo addgroup --system xrayd
sudo adduser "$USER" xrayd  # log out + back in for it to take effect

# Run with a temp socket
mkdir -p /tmp/xrayd-run
XRAYD_SOCK=/tmp/xrayd-run/control.sock \
    XRAYD_LOG_LEVEL=debug \
    ./xrayd
```

In another terminal:

```bash
socat - UNIX-CONNECT:/tmp/xrayd-run/control.sock
{"jsonrpc":"2.0","id":"1","method":"Daemon.Ping"}
# Server replies: {"jsonrpc":"2.0","id":"1","result":{"ok":true}}
```

`Ctrl-C` or SIGTERM exits the daemon cleanly.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `XRAYD_SOCK` | `/run/xrayd/control.sock` | Control socket path |
| `XRAYD_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `XRAYD_CONFIGS_DIR` | `/var/lib/xrayd/configs` | Where `Configs.Add` stores `<ulid>.json` files |
| `XRAYD_EXPECTED_SOCKS_ADDR` | `127.0.0.1:10808` | The local SOCKS5 endpoint configs MUST declare (§6.7) |

## Adding a config

Plan 2 ships the `Configs.*` RPC family.

```bash
socat - UNIX-CONNECT:/run/xrayd/control.sock
{"jsonrpc":"2.0","id":"1","method":"Configs.Add","params":{"json":"<exact xray JSON as a single string>"}}
# → {"jsonrpc":"2.0","id":"1","result":{"id":"01HXAB..."}}

{"jsonrpc":"2.0","id":"2","method":"Configs.List"}
# → {"jsonrpc":"2.0","id":"2","result":{"configs":[{"id":"01HXAB...","sha256":"...","addedAt":"..."}]}}

{"jsonrpc":"2.0","id":"3","method":"Configs.Remove","params":{"id":"01HXAB..."}}
# → {"jsonrpc":"2.0","id":"3","result":{"ok":true}}
```

Subscribers receive `{"jsonrpc":"2.0","method":"Configs.Changed","params":{"added":["..."]}}`
after every successful Add and `{"removed":["..."]}` after every Remove.

### Inbound contract

The submitted config MUST contain **exactly one inbound**, which MUST be a
SOCKS5 listener on the daemon's expected address (default
`127.0.0.1:10808`, override via `XRAYD_EXPECTED_SOCKS_ADDR`) with
`auth: "noauth"`. Public binds (`0.0.0.0`, `::`) are rejected.

Configs with the canonical "SOCKS + HTTP" pair from the upstream
xray-core docs (lines 107–137) are rejected because the renderer is the
single source of inbound shape — it always synthesises one SOCKS5 inbound
from the user's `vless://` / `vmess://` URL. If you're submitting configs
by hand via `socat`, trim down to one SOCKS5 inbound.

### Path restrictions in v1

The daemon refuses any filesystem path under a path-bearing JSON key
(`certificateFile`, `keyFile`, `caCertificateFile`, `access`, `error`,
`path`, `dat`, `file`) that does not resolve under `/usr/local/share/xray/`.
This is enforced regardless of what the path "looks like" — `"cert.pem"`,
`"/etc/letsencrypt/live/<domain>/fullchain.pem"`, and `"~/cert.pem"` are
all rejected. The `ext:` selector is banned entirely.

Two implications you'll hit immediately:

- **TLS material**: `/etc/letsencrypt/...` is the most common cert path on
  production hosts and is rejected. Workarounds: (a) inline the PEM via
  `certificates[].certificate` / `key` (string arrays), or (b) place / symlink
  the files under `/usr/local/share/xray/` so they're inside the allowed
  root. Daemon-side `systemd ProtectSystem=strict` means writes from inside
  the daemon's namespace stay limited; the operator owns staging.
- **Installer's own etc dir**: `/usr/local/etc/xray/` (where the XTLS
  installer puts its sample configs) is also outside the allowed root.
  This is deliberate — the daemon owns its config registry; the installer's
  files are reference material only.

Path-bearing files must EXIST at `Configs.Add` time (the daemon resolves
symlinks and rejects missing targets). Plan 4 may relax this for lazy-load
cases; for v1 the admin pre-stages.

## Conflicts with the official `xray.service`

The XTLS installer (`Xray-install`) ships a `xray.service` unit that may
start its own `xray` process. If both that service and `xrayd` are active
they will fight for the same xray-core binary in Plan 3+. On startup `xrayd`
logs a `WARN` when it detects an enabled `xray.service`; disable it once:

```bash
sudo systemctl disable --now xray.service
```

`xrayd` itself does not touch the installer's unit — disabling is left to
the operator on purpose.

## Testing

```bash
go test ./...
go test ./... -race -v
```

### Build prerequisites

Build with `CGO_ENABLED=1` (the Go default unless cross-compiling). `os/user`
relies on cgo for full NSS support — without it, only `/etc/passwd` and
`/etc/group` are consulted and LDAP/SSSD users won't appear as members of the
`xrayd` group.

```bash
CGO_ENABLED=1 go build -o /tmp/xrayd ./cmd/xrayd
```

### Integration tests that need the `xrayd` group

Most developer machines don't have an `xrayd` group, so the following tests
**skip with a clear message** rather than fail:

- `internal/ipc/server_test.go::TestPingRoundtrip`
- `internal/ipc/server_test.go::TestGetVersionRoundtrip`
- `internal/ipc/server_test.go::TestUnknownMethodReturnsError`
- `internal/ipc/server_test.go::TestServerEchoesIDOnInvalidRequest`
- `internal/ipc/server_test.go::TestServerNullIDOnParseError`
- `internal/ipc/server_test.go::TestServerSilentlyDropsNotification`
- `internal/ipc/server_test.go::TestServerCloseUnlinksSocket`
- `cmd/xrayd/smoke_test.go::TestBinaryRespondsToPing`

When you see `--- SKIP: ... xrayd group not present` in `go test` output,
that's expected. To exercise them locally:

```bash
sudo addgroup --system xrayd
sudo adduser "$USER" xrayd
# log out and back in for the new group to take effect
go test ./...
```

## Plan progression

| Plan | What it adds |
|---|---|
| 1 | Daemon binary, IPC server, `Daemon.Ping`, `Daemon.GetVersion` |
| 2 (this) | `Configs.*` registry under `/var/lib/xrayd/configs/` with path-safety + inbound-safety validation; `Configs.Changed` broadcast |
| 3 | Supervisor + state machine; `Tunnel.Connect`/`Disconnect`/`GetStatus` |
| 4 | Health probe; quotas; done-criteria tests |
