# google_service_cli

A command line tool for interacting with various Google services.

## Install

### Homebrew (macOS)

```bash
brew install bennv14/tap/gsvc
```

Upgrade with `brew upgrade gsvc`, remove with `brew uninstall --cask gsvc`.

### From source

Requires Go 1.24+.

```bash
go install github.com/bennv/google_service_cli@latest
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
