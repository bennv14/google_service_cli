# gsvc — Usage Guide

`gsvc` is a command line tool for reading and writing data in Google services.
It currently supports **Google Drive** (read and write), **Google Chat**
(read and write), and **Google Sheets** (read and write).

- [Install](#install)
- [Quick start](#quick-start)
- [Google Cloud setup](#google-cloud-setup)
- [Profiles](#profiles)
- [Authentication](#authentication)
- [Global flags and output formats](#global-flags-and-output-formats)
- [Drive commands](#drive-commands)
- [Chat commands](#chat-commands)
- [Sheets commands](#sheets-commands)
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

Verify the install and check for updates:

```bash
gsvc -v                       # instant local version (also: gsvc --version)
gsvc version                  # local version + checks GitHub for newer releases
gsvc version --no-check       # skip remote update check (or set GSVC_NO_UPDATE_CHECK=1)
gsvc version -o json          # machine-readable JSON output
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
   - *Google Sheets API* — for `gsvc sheets`
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
| Chat | `chat.spaces.readonly`, `chat.messages.readonly`, `chat.messages`, `chat.users.readstate.readonly`, `directory.readonly` |
| Sheets | `spreadsheets`, `spreadsheets.readonly` |

`chat.messages` enables sending, editing, deleting, uploading attachments, and
reacting to Chat messages. `directory.readonly` is what turns a sender's
`users/1234…` ID into their name: the Chat API leaves `displayName` empty for
every user when the caller authenticates as a person rather than as a Chat app.

> **Google Chat requires a Google Workspace account.** The Chat API does not
> serve consumer (`@gmail.com`) accounts. Drive and Sheets work with both.

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
| `-v, --version` | Print the local version and exit (no network call) |
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

Requires a Google Workspace account and the Chat API enabled in your OAuth
client's project.

**Thread titles.** Chat has no thread-title field — a thread's subject is
whatever its first message says. Every command that shows threads labels each
one with that first message, `Sender · "excerpt"`, in the default tree output,
and carries it untruncated as `thread.title` in JSON, alongside
`thread.headSender` — for a partial thread that first message is not in
`messages`, so its author would otherwise appear nowhere. (`--group flat` and
`--output table` have no thread header, so they show the short thread ID as
before, and they do not pay for a title they cannot print.) When the first
message falls outside the window you scanned, it is fetched: one extra request
per such thread, eight at a time. A first message that cannot be read (deleted,
or in a space you have lost access to) costs the thread its title and prints one
warning line to stderr; nothing fails. A first message that is only an
attachment or a card has no text to quote, so the label is just the name of
whoever posted it. Titles are never cached, so an edited subject shows up on the
next run.

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

A thread's first message almost never mentions you and is usually older than the
window besides, so most threads here come back `partial`. Their titles are
fetched so that a mention still arrives with the question it answers.

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

### `chat message`

Read a single message by ID, web URL, or resource name, and optionally download
its attachments. The message ID is passed as a positional argument.

Message IDs across all commands are formatted as `spaceId/threadId/messageId` so
they can be copied directly into `chat message`.

**Supported ID formats:**

| Format | Example | Description |
| --- | --- | --- |
| 3-segment ID | `AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY` | Standard `spaceId/threadId/messageId` format |
| Chat web URL | `https://chat.google.com/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY` | Direct browser URL from "Copy link to message" |
| Resource name | `spaces/AAAA9GOspFY/messages/t-uT1uhCWAg.emH4eHFJkeY` | Canonical Chat API resource name |
| Short ID with `--space` | `t-uT1uhCWAg.emH4eHFJkeY --space AAAA9GOspFY` | Message ID or `threadId/messageId` with `--space` |

| Flag | Default | Description |
| --- | --- | --- |
| `--download` | | Download all assets attached to this message |
| `--output-dir <path>` | `.` | Directory to save downloaded assets |
| `--out <filename>` | | Custom output filename for a single asset |
| `--space <s>` | | Space ID if passing a shortened message ID |
| `--links` | | Print URLs on their own lines instead of embedding them |
| `--refresh-names` | | Ignore cached display names and look them up again |

Attachments (both Chat uploaded files and Google Drive attachments) are listed in
the message output with their name and MIME type (`📎 filename (type)`).
With `--download`, Chat-hosted attachments are downloaded via the Chat Media API
and saved locally (untrusted remote filenames are sanitized to prevent path
traversal). For Google Drive attachments, `gsvc` prints the corresponding
`gsvc drive download <driveFileId>` command to run.

```bash
# Read a single message
gsvc chat message AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY

# Read a message via copied Chat URL
gsvc chat message https://chat.google.com/room/AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY

# Read and download all attachments to a specific directory
gsvc chat message AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY --download --output-dir ./downloads

# Download a single attachment with a custom filename
gsvc chat message AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY --download --out ./invoice.pdf

# Read message metadata and attachments as JSON
gsvc chat message AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY -o json
```

### `chat send`

Send a message to a space, DM (by partner email), thread, or in reply to an
existing message. Text can come from a positional argument, `--text`/`-m`, or
piped stdin, and is sent verbatim so native Google Chat formatting works without
mangling.

| Flag | Description |
| --- | --- |
| `--space <s>` | Space resource name (`spaces/...`), display name, or DM partner's email |
| `--thread <t>` | Thread resource name (`spaces/X/threads/Y`), short ID, or Chat URL |
| `--reply-to <msg>` | Message ID, resource name, or Chat URL to reply to |
| `-m, --text <body>` | Message text body |
| `-a, --attach <path>` | Local file path to upload and attach (repeatable) |
| `--links` | Print URLs on their own lines instead of embedding them |
| `--refresh-names` | Ignore cached display names and look them up again |

```bash
gsvc chat send --space engineering "Build is green"
gsvc chat send --space colleague@company.com --attach ./report.pdf "Here is the report"
gsvc chat send --reply-to AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY "Thanks!"
echo "Multi-line message" | gsvc chat send --space engineering
```

### `chat edit`

Update the text body of an existing Google Chat message while preserving its
attachments. New text can come from a second positional argument, `--text`/`-m`,
or piped stdin.

| Flag | Description |
| --- | --- |
| `--space <s>` | Space ID, display name, or DM email if passing a shortened message ID |
| `-m, --text <body>` | New message text |
| `--links` | Print URLs on their own lines instead of embedding them |
| `--refresh-names` | Ignore cached display names and look them up again |

```bash
gsvc chat edit AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY "Updated text"
```

### `chat delete`

Delete a Google Chat message by ID, URL, or resource name. Requires interactive
confirmation unless `--yes`/`-y` is passed.

| Flag | Description |
| --- | --- |
| `--space <s>` | Space ID, display name, or DM email if passing a shortened message ID |
| `-y, --yes` | Skip confirmation prompt |

```bash
gsvc chat delete AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY --yes
```

### `chat react`

Add or remove a Unicode emoji reaction on a Google Chat message.

| Flag | Description |
| --- | --- |
| `--space <s>` | Space ID, display name, or DM email if passing a shortened message ID |
| `--remove` | Remove your matching reaction instead of adding it |

```bash
gsvc chat react AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY "👍"
gsvc chat react AAAA9GOspFY/t-uT1uhCWAg/emH4eHFJkeY "👍" --remove
```

## Sheets commands

Alias: `sheet`. Requires the *Google Sheets API* enabled in your OAuth client's
GCP project.

### `sheets create`

Create a new Google Spreadsheet.

| Flag | Description |
| --- | --- |
| `--tab <title>` | Title for the default initial worksheet tab |

```bash
gsvc sheets create "Q4 Budget" --tab Summary
```

### `sheets info` (alias `stat`)

Show spreadsheet metadata and worksheet tabs (including each tab's `sheetId`).

```bash
gsvc sheets info <spreadsheetId>
```

### `sheets get` (alias `read`)

Retrieve cell values within an A1 range.

| Flag | Default | Description |
| --- | --- | --- |
| `--render <mode>` | `formatted` | Value rendering: `formatted` \| `unformatted` \| `formula` |

```bash
gsvc sheets get <spreadsheetId> 'Sheet1!A1:C10'
gsvc sheets get <spreadsheetId> 'Sheet1!A1:C10' --render formula -o json
```

### `sheets append` and `sheets update` (alias `write`)

Append rows after existing content or overwrite cells in a range. Values can be
passed as positional arguments, via `--file`/`-f` (CSV), or piped on stdin.

| Flag | Default | Description |
| --- | --- | --- |
| `-f, --file <path>` | | Read rows from a local CSV file |
| `--input-type <type>` | `user_entered` | Input interpretation: `user_entered` \| `raw` |

```bash
gsvc sheets append <spreadsheetId> 'Sheet1!A:C' Alice 100 true
gsvc sheets update <spreadsheetId> 'Sheet1!A1:C2' --file ./data.csv
cat rows.csv | gsvc sheets append <spreadsheetId> 'Sheet1!A:C'
```

### `sheets clear`

Clear all cell values within a specified range.

```bash
gsvc sheets clear <spreadsheetId> 'Sheet1!A2:C10'
```

### `sheets add-tab` and `sheets delete-tab`

Add a new worksheet tab or delete an existing tab by its integer `sheetId` (from
`gsvc sheets info`).

`sheets add-tab` flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--index <n>` | `-1` (append) | Zero-based tab index position |
| `--rows <n>` | `1000` | Initial row count |
| `--cols <n>` | `26` | Initial column count |

```bash
gsvc sheets add-tab <spreadsheetId> "Archive" --index 0
gsvc sheets delete-tab <spreadsheetId> 123456789
```

### `sheets export`

Export a worksheet tab as CSV to `stdout` or a file.

| Flag | Default | Description |
| --- | --- | --- |
| `--sheet <title>` | first tab | Specific tab title to export |
| `--out <path>` | stdout | Target output file path |

```bash
gsvc sheets export <spreadsheetId> --sheet Summary --out ./summary.csv
```

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
after upgrading to a release that adds new service or write scopes (such as
v2.0.0 or v3.0.1). Run `gsvc auth login` again and accept the new permissions.

**`the Google Chat API is not enabled for this OAuth client's GCP project`** /
**`the Google Sheets API is not enabled for this OAuth client's GCP project`**
Enable the corresponding API in the project that owns your OAuth client, under
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
