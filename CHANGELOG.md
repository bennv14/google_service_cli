# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.1] - 2026-11-26

### Changed

- **Homebrew Cask post-install hook**: migrated deprecated
  `homebrew_casks.hooks.post.install` (`postflight`) to Homebrew's declarative
  `install_steps` (`postflight_steps`) in `.goreleaser.yaml`.
- **Release CI workflow**: upgraded GitHub Actions (`actions/checkout@v7`,
  `actions/setup-go@v7` with `cache: false`, `goreleaser/goreleaser-action@v7`)
  for Node.js 24 support and cleaner release runs.

### Added

- **Homebrew tap mirror releases**: release automation now publishes a
  `gsvc-vX.Y.Z` GitHub Release with build archives, `checksums.txt`, and
  changelog notes to `bennv14/homebrew-tap`.

## [v3.1.0] - 2026-11-26

### Added

- **Version flags (`-v` / `--version`)**: print the local CLI version, commit,
  and build date immediately without making network requests.
- **GitHub Releases update check (`gsvc version`)**:
  - Checks the latest published GitHub Release by default (3s timeout) and
    reports whether an update is available, up to date, or a dev build.
  - Supports `--no-check` and `GSVC_NO_UPDATE_CHECK=1` to skip remote checks.
  - Supports `-o text` (default), `-o table`, and `-o json` with non-fatal
    offline/rate-limit warnings routed to `stderr` so JSON `stdout` stays valid.
  - Detects Homebrew installations (`Caskroom/gsvc/` or `Cellar/gsvc/`) to show
    the `brew upgrade gsvc` hint only when applicable.
- **Documentation**: updated `README.md` and `USAGE.md` for Chat write commands,
  Google Sheets commands, and version/update checking.

## [v3.0.1] - 2026-11-15

### Breaking changes

- `gsvc auth login` now requests additional OAuth scopes for Google Sheets
  (`spreadsheets`, `spreadsheets.readonly`) and Google Chat write operations
  (`chat.messages`). **Existing users must run `gsvc auth login` again** to
  grant the new permissions, and enable the *Google Sheets API* in the GCP
  project behind their OAuth client to use `gsvc sheets`.

### Added

- **Google Sheets (`gsvc sheets`, alias `sheet`)**:
  - `sheets create <title>` (`--tab`) — create a new spreadsheet.
  - `sheets info <spreadsheetId>` (alias `stat`) — show spreadsheet metadata
    and worksheet tabs.
  - `sheets get <spreadsheetId> <range>` (alias `read`, `--render
    formatted|unformatted|formula`) — read cell values from a range.
  - `sheets append` and `sheets update` (alias `write`) — append or overwrite
    cell values from `--file`/`-f` (CSV), piped stdin, or positional arguments,
    with `--input-type user_entered|raw`.
  - `sheets clear <spreadsheetId> <range>` — clear cell values in a range.
  - `sheets add-tab <spreadsheetId> <title>` (`--index`, `--rows`, `--cols`) and
    `sheets delete-tab <spreadsheetId> <sheetId>` — create and delete worksheet
    tabs.
  - `sheets export <spreadsheetId>` (`--sheet`, `--out`) — export a worksheet
    tab as CSV to `stdout` or a file.
- **Google Chat write commands (`gsvc chat`)**:
  - `chat send [text]` (`--space`, `--thread`, `--reply-to`, `--text`/`-m`,
    `--attach`/`-a`, `--links`, `--refresh-names`) — send messages, thread
    replies, and local file attachments (supports positional text, `--text`, or
    piped stdin while preserving native Chat markup).
  - `chat edit <messageId|URL> [text]` (`--space`, `--text`/`-m`, `--links`,
    `--refresh-names`) — update a message's text body while preserving existing
    attachments.
  - `chat delete <messageId|URL>` (`--space`, `--yes`/`-y`) — delete a message
    with interactive confirmation unless `--yes` is provided.
  - `chat react <messageId|URL> <emoji>` (`--space`, `--remove`) — add or
    remove Unicode emoji reactions on a message.

### Changed

- Decoupled `gsvc chat message --download` from `internal/service/drive`:
  Chat-hosted media continues to download via the Chat Media API, while
  `DRIVE_FILE` attachments now emit an actionable
  `gsvc drive download <driveFileId>` command hint instead of invoking the Drive
  client directly.
- Refactored the Chat query engine and message action commands into focused
  responsibility-based files.

## [v2.0.5] - 2026-08-19

### Added

- Single message lookup: `gsvc chat message <messageId>` reads a single message
  by ID, web URL, or resource name, with support for downloading attachments.
- Standardized message ID format: `spaceId/threadId/messageId` across all outputs
  for consistent identification and direct reuse in `gsvc chat message`.
- Attachment display in terminal views: messages with uploaded files or Google
  Drive attachments show attachment names, MIME types, and download links
  (`📎 <name> (<type>)`).
- Asset downloading: `gsvc chat message --download` automatically downloads
  message attachments (supporting both Chat uploaded media via the Chat Media
  API and Google Drive attachments via the Drive API), with `--output-dir` and
  `--out` for custom destination paths.

## [v2.0.4] - 2026-08-01

### Added

- Thread titles. `gsvc chat` now labels each thread it heads with its opening
  message — `Sender · "excerpt"` in the default tree output, `thread.title`
  (untruncated) and `thread.headSender` in JSON. Chat has no thread-title field,
  so the opening message is fetched when the scan did not already contain it: one
  request per such thread, eight at a time, never cached. That cost is only paid
  by output with a thread header, so `--group flat`, `--output table`, and
  `chat spaces --unread` skip it. This matters most for `gsvc chat mentions`,
  where the thread you were mentioned in almost always began before the window.

### Fixed

- A partial thread's label no longer credits the wrong person with starting it.
  It named the thread after the earliest message *in hand*, which for a thread
  that began before the scanned window is a mid-thread reply.

## [v2.0.3] - 2026-07-31

### Added

- `gsvc chat` now shows people's names instead of raw `users/1234…` IDs, in
  sender lines, thread labels, and DM space names. The Chat API leaves
  `displayName` empty for every user when the caller authenticates as a person,
  so names come from the People API instead. JSON output gains
  `sender.email` alongside `sender.name`.
- `--refresh-names` on every `chat` subcommand: ignore the cached display names
  and look them up again.
- Resolved names are cached in `<config>/cache/people-<profile>.json` for 30
  days, per profile. Messages, threads, and read state are still never cached.

### Changed

- `gsvc auth login` now also requests `directory.readonly`. **Existing users
  must run `gsvc auth login` again**, and enable the *People API* in the GCP
  project behind their OAuth client. Until both are done, `chat` works exactly
  as before and prints one warning line to stderr.

### Fixed

- Cancelling a `chat` command (Ctrl-C, or a `--timeout` expiring) now stops the
  scan immediately. The `break` on context cancellation escaped only the inner
  `select`, not the loop, so the remaining spaces and People API batches were
  still queued before the command gave up.
- DM space names no longer fall back to a raw `users/1234…` ID when the sender's
  name could not be resolved; such senders are skipped, as if unnamed.

### Removed

- The `spaces.members.list` fallback for naming DMs. It required a scope `gsvc`
  never requested, so it returned 403 on every call and its result was silently
  discarded; the People API path replaces it.

## [v2.0.2] - 2026-07-31

### Added

- Shell completion scripts for bash, zsh, and fish are now generated from the
  binary at release time, so they can never drift from the command tree. The
  release archives ship them, and the Homebrew cask installs them — after
  `brew install` or `brew upgrade`, completion works in a new shell with no
  extra setup.
- `CHANGELOG.md` covering every release from v1.0.0 onward.
- `USAGE.md`: a full user guide — Google Cloud setup, profiles, authentication,
  every `drive` and `chat` command with examples, output formats, shell
  completion, config file layout, and troubleshooting.
- Release archives now include `README.md`.

### Changed

- The Go module path is now `github.com/bennv14/google_service_cli`, matching
  the GitHub account that owns the repository. This affects only code that
  imports these packages; the `gsvc` binary is unchanged.
- `README.md` now links to the usage guide and the changelog instead of
  duplicating command documentation.

## [v2.0.1] - 2026-07-28

### Added

- GoReleaser configuration: cross-compiled builds for macOS, Linux, and Windows
  on both amd64 and arm64, checksums, and archive naming.
- Homebrew cask publishing to `bennv14/homebrew-tap`, so `gsvc` installs with
  `brew install bennv14/tap/gsvc`. A post-install hook strips the macOS
  quarantine attribute from the downloaded binary.

### Fixed

- Install instructions in `README.md`.

## [v2.0.0] - 2026-07-27

Adds Google Chat support. The major bump is for the login change described under
*Breaking changes* — after upgrading you must run `gsvc auth login` again.

### Breaking changes

- `gsvc auth login` now requests the deduplicated union of every registered
  service's OAuth scopes, not just Drive's. Existing tokens lack the Chat
  scopes, so any `gsvc chat` command fails with a missing-scope error until you
  re-run `gsvc auth login` and grant the new permissions.

### Added

- `gsvc chat`: a read-only Google Chat command subtree.
  - `chat spaces` — list spaces, optionally filtered by `--type` (space, dm,
    group) or narrowed to `--unread` spaces with their unread counts.
  - `chat messages` — the general query command. Filters combine freely:
    `--space`, `--thread`, `--since`, `--until`, `--unread`, `--mention-me`,
    `--type`, `--limit`, `--group`.
  - `chat unread`, `chat mentions`, `chat thread <threadId>`, `chat threads` —
    presets over `chat messages` for the common cases.
- Time bounds accept relative durations (`3d`, `12h`, `30m`), plain dates
  (`2026-07-25`), and RFC3339 timestamps.
- `--space` resolves a space ID, a display name, or a DM partner's email
  address.
- Hierarchical text renderer that groups output by space and thread, with
  `--group space|thread|flat` to override and `--links` to print URLs on their
  own lines instead of embedding them.
- Deep links to messages, threads, and spaces built from Chat resource names.
- New `text` output format, alongside `table` and `json`. Commands may choose
  their own natural default: `gsvc chat` defaults to `text`, and an explicit
  `--output` always wins.
- Error mapping now identifies two specific 403s: a Chat API that is not enabled
  in the OAuth client's GCP project, and missing OAuth scopes — the latter tells
  you to re-run `gsvc auth login`.

### Fixed

- Pagination is bounded by a page cap and a repeated-page-token guard, so a
  server returning an endless token stream cannot grow the result slice without
  bound.
- A malformed `lastReadTime` is surfaced as an error instead of being silently
  treated as "everything is unread".
- `windowLabel` keeps its rounded count inside the unit it chose.
- The text renderer omits the tree connector for levels that have no children.

## [v1.0.0] - 2026-07-23

First release.

### Added

- `gsvc drive`: `about`, `list` (alias `ls`), `search`, `info` (alias `stat`),
  `download`, `upload`.
- `gsvc config`: named YAML profiles — `add`, `list`, `show`, `use`. Config is
  written atomically; the first profile added becomes the active one.
- `gsvc auth`: `login`, `logout`, `status`. OAuth via a loopback redirect for
  interactive accounts; service-account keys are validated by minting a token.
- Token store on disk, keyed by profile name.
- Lazily constructed authenticated HTTP client, so commands that need no API
  access never trigger an auth error.
- `table` and `json` output formats via a shared result writer.
- Global flags: `--profile`, `--output`/`-o`, `--verbose`.
- Build-time version injection surfaced by `gsvc version`.

[Unreleased]: https://github.com/bennv14/google_service_cli/compare/v3.1.1...HEAD
[v3.1.1]: https://github.com/bennv14/google_service_cli/compare/v3.1.0...v3.1.1
[v3.1.0]: https://github.com/bennv14/google_service_cli/compare/v3.0.1...v3.1.0
[v3.0.1]: https://github.com/bennv14/google_service_cli/compare/v2.0.5...v3.0.1
[v2.0.5]: https://github.com/bennv14/google_service_cli/compare/v2.0.4...v2.0.5
[v2.0.4]: https://github.com/bennv14/google_service_cli/compare/v2.0.3...v2.0.4
[v2.0.3]: https://github.com/bennv14/google_service_cli/compare/v2.0.2...v2.0.3
[v2.0.2]: https://github.com/bennv14/google_service_cli/compare/v2.0.1...v2.0.2
[v2.0.1]: https://github.com/bennv14/google_service_cli/compare/v2.0.0...v2.0.1
[v2.0.0]: https://github.com/bennv14/google_service_cli/compare/v1.0.0...v2.0.0
[v1.0.0]: https://github.com/bennv14/google_service_cli/releases/tag/v1.0.0
