# google_service_cli

A command line tool for interacting with various Google services.

## Install

### Homebrew (macOS)

```bash
brew install bennv14/tap/gsvc
```

Upgrade with `brew upgrade gsvc`, remove with `brew uninstall --cask gsvc`.

### Pre-built binaries

Download for Linux or Windows from the
[releases page](https://github.com/bennv14/google_service_cli/releases).

### From source

Requires Go 1.24+.

```bash
git clone https://github.com/bennv14/google_service_cli.git
cd google_service_cli
make install
```

## Build

```bash
go build -o google_service_cli .
```

## Run

```bash
go run . version
```

## Project layout

```
.
├── main.go              # entry point
├── cmd/                 # cobra commands
│   ├── root.go
│   └── version.go
└── internal/
    └── service/         # Google service clients / business logic
```
