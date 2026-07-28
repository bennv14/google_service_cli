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

## Shell completion

Homebrew installs the bash, zsh and fish completions with the cask — just start
a new shell. If completion still does nothing, your zsh setup is likely running
`compinit` before Homebrew's `site-functions` directory is on `fpath`; refresh
the cache with `rm -f ~/.zcompdump*` and reopen the terminal.

For other install methods, generate the script yourself:

```bash
# zsh
gsvc completion zsh > "${fpath[1]}/_gsvc"
# bash
gsvc completion bash > /usr/local/etc/bash_completion.d/gsvc
# fish
gsvc completion fish > ~/.config/fish/completions/gsvc.fish
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
