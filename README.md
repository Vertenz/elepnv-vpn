# elepn

Electron + React + TypeScript client with a reserved Go daemon and packaging layout.

## Development

```bash
./scripts/install-deps.sh
make dev
```

On Linux, `install-deps.sh` installs Xray with the official XTLS installer when `xray` is not already available.
For now `make dev` starts the Electron app only. The daemon and packaging folders are structural placeholders.
