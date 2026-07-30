# google_service_cli

`gsvc` is a command line tool for interacting with Google services — **Drive**
(read and write) and **Chat** (read-only).

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

## Quick start

```bash
gsvc config add personal --auth-type oauth --client-path ~/Downloads/client_secret.json
gsvc auth login
gsvc drive list --limit 10
gsvc chat unread
```

`gsvc` uses your own Google Cloud OAuth client. See
[USAGE.md](USAGE.md#google-cloud-setup) for how to create one.

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

## Documentation

- **[USAGE.md](USAGE.md)** — full guide: setup, profiles, every command, output
  formats, troubleshooting.
- **[CHANGELOG.md](CHANGELOG.md)** — what changed in each release.

## Development

```bash
make build    # build ./gsvc
make install  # build into $(go env GOPATH)/bin
make test     # go test ./...
```

## Project layout

```
.
├── main.go                    # entry point
├── cmd/                       # root command, global flags, auth, config, version
└── internal/
    ├── auth/                  # OAuth loopback + service account providers, token store
    ├── config/                # named-profile YAML store
    ├── gclient/               # lazy authenticated HTTP client factory
    ├── gerr/                  # Google API error mapping
    ├── output/                # table / json / text writers
    └── service/               # service registry
        ├── drive/             # Google Drive
        └── chat/              # Google Chat (read-only)
```
