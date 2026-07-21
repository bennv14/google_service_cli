# google_service_cli

A command line tool for interacting with various Google services.

## Requirements

- Go 1.24+

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
