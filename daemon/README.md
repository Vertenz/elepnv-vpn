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
| 1 (this) | Daemon binary, IPC server, `Daemon.Ping`, `Daemon.GetVersion` |
| 2 | `Configs.*` registry under `/var/lib/xrayd/configs/` |
| 3 | Supervisor + state machine; `Tunnel.Connect`/`Disconnect`/`GetStatus` |
| 4 | Health probe; quotas; done-criteria tests |
