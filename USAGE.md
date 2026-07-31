# gsvc — Usage Guide

`gsvc` is a command line tool for reading and writing data in Google services.
It currently supports **Google Drive** (read and write) and **Google Chat**
(read-only).

- [Install](#install)
- [Quick start](#quick-start)
- [Google Cloud setup](#google-cloud-setup)
- [Profiles](#profiles)
- [Authentication](#authentication)
- [Global flags and output formats](#global-flags-and-output-formats)
- [Drive commands](#drive-commands)
- [Chat commands](#chat-commands)
- [Shell completion](#shell-completion)
- [Files on disk](#files-on-disk)
- [Troubleshooting](#troubleshooting)

## Install

### Homebrew (macOS)

```bash
brew install bennv14/tap/gsvc
```

Upgrade with `brew upgrade gsvc`, remove with `brew uninstall --cask gsvc`.

### Pre-built binaries

Download the archive for your platform from the
[releases page](https://github.com/bennv14/google_service_cli/releases), unpack
it, and put `gsvc` somewhere on your `PATH`.

### From source

Requires Go 1.24+.

```bash
git clone https://github.com/bennv14/google_service_cli.git
cd google_service_cli
make install     # builds into $(go env GOPATH)/bin/gsvc
```

Verify the install:

```bash
gsvc version
```

## Quick start

```bash
# 1. Register your OAuth client (see "Google Cloud setup" below for the JSON file)
gsvc config add personal --auth-type oauth --client-path ~/Downloads/client_secret.json

# 2. Grant access — opens a browser, then captures the redirect on localhost
gsvc auth login

# 3. Use it
gsvc drive about
gsvc drive list --limit 10
gsvc chat spaces
gsvc chat unread
```

The first profile you add automatically becomes the active one.

## Google Cloud setup

`gsvc` talks to Google APIs using **your own** OAuth client, so you control the
project, the consent screen, and the quota.

1. Create (or pick) a project in the
   [Google Cloud Console](https://console.cloud.google.com/).
2. **Enable the APIs** you intend to use, under *APIs & Services → Library*:
   - *Google Drive API* — for `gsvc drive`
   - *Google Chat API* — for `gsvc chat`
   - *People API* — for `gsvc chat` to show senders' names instead of raw
     `users/1234…` IDs. Without it, chat commands still work and print a
     one-line warning.
3. Configure the **OAuth consent screen**. While it is in *Testing* mode, add
   your own Google account under *Test users* — the Chat scopes are restricted
   scopes and will be refused otherwise.
4. Create credentials: *APIs & Services → Credentials → Create Credentials →
   OAuth client ID*, application type **Desktop app**. Download the JSON.
5. Pass that JSON path to `gsvc config add --client-path`.

`gsvc auth login` requests the union of every service's scopes in one consent
screen:

| Service | Scopes |
| --- | --- |
| Drive | `drive.readonly`, `drive.file` |
| Chat | `chat.spaces.readonly`, `chat.messages.readonly`, `chat.users.readstate.readonly`, `directory.readonly` |

All Chat scopes are read-only; `gsvc` never calls a mutating Chat endpoint.
`directory.readonly` is what turns a sender's `users/1234…` ID into their name:
the Chat API leaves `displayName` empty for every user when the caller
authenticates as a person rather than as a Chat app.

> **Google Chat requires a Google Workspace account.** The Chat API does not
> serve consumer (`@gmail.com`) accounts. Drive works with both.

## Profiles

A profile is a named set of credentials. Use profiles to keep a work account and
a personal account side by side.

```bash
# OAuth (interactive, browser-based)
gsvc config add work --auth-type oauth --client-path ~/creds/work_client.json

# Service account (non-interactive, for servers and CI)
gsvc config add ci --auth-type service_account --key-path ~/creds/sa_key.json

gsvc config list          # * marks the active profile
gsvc config show          # details of the active profile
gsvc config show work     # details of a named profile
gsvc config use work      # switch the active profile
```

`--auth-type` defaults to `oauth`. Short aliases are accepted: `--auth`,
`--client`, `--key`.

Every command takes `--profile` to override the active profile for one
invocation:

```bash
gsvc --profile work drive list
```

## Authentication

```bash
gsvc auth login     # authenticate the active profile
gsvc auth status    # show whether a valid token is stored
gsvc auth logout    # delete the stored token
```

For an **OAuth** profile, `login` opens your browser, waits on a loopback
redirect, and stores the resulting token. Refresh happens automatically, so you
normally log in once per profile.

For a **service account** profile, there is nothing interactive to do: `login`
validates the key by minting a token and reports whether it worked.

`auth status` prints one of:

```
profile: personal
auth:    oauth
status:  logged in (token valid until 2026-07-31 18:40)
```

An expired token is not an error — it is refreshed on next use.

## Global flags and output formats

| Flag | Description |
| --- | --- |
| `--profile <name>` | Use this profile instead of the active one |
| `-o, --output <format>` | `table`, `json`, or `text` |
| `--verbose` | Verbose error output |

- **`table`** — aligned columns. The default for `drive` and `config`.
- **`json`** — machine-readable, for piping into `jq`.
- **`text`** — human-readable prose or a nested tree. The default for `chat`.

Each command picks the format that suits it, and an explicit `--output` always
wins:

```bash
gsvc chat unread                # text tree (chat's natural default)
gsvc chat unread -o json        # explicit flag overrides the default
gsvc drive list -o json | jq '.files[].name'
```

## Drive commands

### `drive about`

Account details and storage quota.

```bash
gsvc drive about
```

### `drive list` (alias `ls`)

List files and folders.

| Flag | Default | Description |
| --- | --- | --- |
| `--folder <id>` | | Parent folder ID to list within |
| `--query <text>` | | Filter by name or full text |
| `--limit <n>` | `100` | Maximum number of results |

```bash
gsvc drive list
gsvc drive list --folder 1AbC...xyz --limit 20
gsvc drive list --query "quarterly report"
```

### `drive search`

Search by name or full text. The query is a positional argument.

```bash
gsvc drive search "budget 2026"
gsvc drive search invoice --limit 50
```

### `drive info` (alias `stat`)

Metadata for one file.

```bash
gsvc drive info 1AbC...xyz
```

### `drive download`

Download a file. Without `--out`, the file keeps its Drive name and lands in the
current directory.

```bash
gsvc drive download 1AbC...xyz
gsvc drive download 1AbC...xyz --out ./report.pdf
```

### `drive upload`

Upload a local file. Without `--to`, it goes to *My Drive*; without `--name`, it
keeps its local filename.

```bash
gsvc drive upload ./notes.md
gsvc drive upload ./notes.md --to 1FolderId --name "Meeting notes.md"
```

## Chat commands

Read-only. Requires a Google Workspace account and the Chat API enabled in your
OAuth client's project.

### `chat spaces`

List the spaces you belong to. This lists *spaces*, never messages.

| Flag | Description |
| --- | --- |
| `--unread` | Only spaces with unread messages, with counts |
| `--type <types>` | Restrict to `space`, `dm`, `group` (comma-separated) |
| `--links` | Print URLs on their own lines instead of embedding them |
| `--refresh-names` | Ignore cached display names and look them up again |

```bash
gsvc chat spaces
gsvc chat spaces --unread
gsvc chat spaces --type dm,group
```

### `chat messages`

The general query command — every other chat command is a preset over this one.
All filters combine freely.

| Flag | Default | Description |
| --- | --- | --- |
| `--space <s>` | | Space ID, display name, or a DM partner's email |
| `--thread <name>` | | Thread resource name (`spaces/X/threads/Y`) |
| `--since <t>` | | Lower time bound |
| `--until <t>` | | Upper time bound |
| `--unread` | | Only messages you have not read |
| `--mention-me` | | Only messages that mention you |
| `--type <types>` | | Restrict to `space`, `dm`, `group` |
| `--limit <n>` | `50` | Maximum messages in total (`0` = no limit) |
| `--group <mode>` | adaptive | `space`, `thread`, or `flat` |
| `--links` | | Print URLs on their own lines |
| `--refresh-names` | | Ignore cached display names and look them up again |

`--limit` keeps the **newest** matching messages, and counts only messages that
pass every filter — `--mention-me --limit 20` gives you 20 mentions, not 20
messages that may or may not mention you.

**Time bounds** (`--since` / `--until`) accept three forms:

| Form | Examples |
| --- | --- |
| Relative duration | `30m`, `12h`, `3d` |
| Plain date | `2026-07-25` |
| RFC3339 timestamp | `2026-07-25T09:00:00Z` |

```bash
# Everything in one space over the last 3 days
gsvc chat messages --space "Engineering" --since 3d

# A DM, addressed by the other person's email
gsvc chat messages --space alice@example.com --since 24h

# A bounded window, flattened into a single chronological list
gsvc chat messages --since 2026-07-20 --until 2026-07-25 --group flat

# Unread mentions in DMs only, as JSON
gsvc chat messages --unread --mention-me --type dm -o json
```

> Mentions cannot be filtered server-side, so `--mention-me` without `--space`
> reads every space and filters locally. It is slow and prints progress to
> stderr. Narrow it with `--space` or `--since` when you can.

### `chat unread`

Preset for `messages --unread` — unread messages across every space.

```bash
gsvc chat unread
gsvc chat unread --type dm --limit 20
```

Each space is scanned from its own read marker, however far back that sits. A
space you have never opened has no marker, so it is scanned over the default
7-day window instead of its whole history — pass `--since` to widen it. Spaces
whose read state cannot be read at all are reported as warnings on stderr rather
than scanned.

### `chat mentions`

Preset for `messages --mention-me --since 7d`. Override `--since` to widen or
narrow the window.

```bash
gsvc chat mentions
gsvc chat mentions --since 30d
```

### `chat thread`

Preset for `messages --thread <T> --limit 0` — reads one thread in full, with no
message cap. Takes the thread ID as a positional argument.

```bash
gsvc chat thread spaces/AAAA1111/threads/BBBB2222
```

### `chat threads`

List threads in a space within a time window (`--since` defaults to `30d`,
`--limit` to `20` threads).

```bash
gsvc chat threads --space "Engineering"
gsvc chat threads --space "Engineering" --since 7d --limit 5
```

> The Chat API has no `threads.list`, so this scans the messages in the window
> and groups them. `--limit` cuts the number of threads *after* grouping and
> therefore saves no API calls. The window is always scanned in full: stopping
> early would show only each thread's tail and mislabel it.

## Shell completion

**Homebrew** installs the bash, zsh, and fish completions with the cask — start
a new shell and they work. If completion still does nothing under zsh, your
setup is likely running `compinit` before Homebrew's `site-functions` directory
is on `fpath`; refresh the cache with `rm -f ~/.zcompdump*` and reopen the
terminal.

The **release archives** ship the same scripts under `completions/`, so you can
copy them into place directly.

For any other install method, generate them from the binary:

```bash
# zsh
gsvc completion zsh > "${fpath[1]}/_gsvc"

# bash
gsvc completion bash > /usr/local/etc/bash_completion.d/gsvc

# fish
gsvc completion fish > ~/.config/fish/completions/gsvc.fish
```

Run `gsvc completion --help` for PowerShell and for per-shell installation
notes. Start a new shell afterwards.

## Files on disk

Configuration lives in a per-user directory:

| Platform | Location |
| --- | --- |
| macOS / Linux | `~/.config/google_service_cli/` |
| Windows | `%AppData%\google_service_cli\` |
| Any, if `XDG_CONFIG_HOME` is set | `$XDG_CONFIG_HOME/google_service_cli/` |

```
google_service_cli/
├── config.yaml          # profiles and the active profile
├── cache/
│   └── people-<profile>.json   # resolved display names, mode 0600
└── tokens/
    └── <profile>.json   # OAuth token, mode 0600
```

`cache/people-<profile>.json` remembers the names behind Chat's `users/1234…`
IDs for 30 days, so a 333-space account does not pay a directory lookup on every
command. It is per profile, so one account never sees another's names. Deleting
it is always safe; `--refresh-names` rebuilds it in place. Messages, threads, and
read state are never cached — those must always be fresh.

`config.yaml` looks like this:

```yaml
active: personal
profiles:
  personal:
    auth_type: oauth
    client_path: /Users/you/creds/client_secret.json
  ci:
    auth_type: service_account
    key_path: /Users/you/creds/sa_key.json
```

`config.yaml` is written atomically, so an interrupted write cannot corrupt it.
It stores *paths* to your credential files, never the credentials themselves.

## Troubleshooting

**`no active profile; run 'gsvc config add'`**
No profile exists yet, or none is marked active. Run `gsvc config add <name>`,
or `gsvc config use <name>` to pick one.

**`missing OAuth scopes: run 'gsvc auth login' again to grant the new permissions`**
Your stored token predates a scope that the command needs — this is expected
after upgrading from v1.x, which requested Drive scopes only. Run
`gsvc auth login` again and accept the new permissions.

**`the Google Chat API is not enabled for this OAuth client's GCP project`**
Enable the *Google Chat API* in the project that owns your OAuth client, under
*APIs & Services → Library*. Enabling can take a minute to propagate.

**`warning: cannot resolve sender names`**
Chat commands still work; senders show as raw `users/1234…` IDs. Either the
*People API* is not enabled in your OAuth client's project, or your stored token
predates the `directory.readonly` scope. Enable the API, then run
`gsvc auth login` again.

**`permission denied (check auth/scopes)`**
A 403 that is neither of the two above. Common causes: the account is not listed
as a *Test user* on a consent screen still in Testing mode, a Workspace admin
policy blocks the API, or you genuinely lack access to the resource.

**Chat commands return nothing on a `@gmail.com` account**
The Chat API does not serve consumer accounts. Use a Google Workspace account.

**`rate limited, please try again shortly`**
You hit a Google quota. Wait, then narrow the query — `--space`, a shorter
`--since` window, or a smaller `--limit` all reduce the number of API calls.
`--mention-me` across every space is the most expensive query `gsvc` can run.

**Anything else** — re-run with `--verbose` for the underlying Google API error.
