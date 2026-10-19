<!-- Verbatim copy of the design document passed to /speckit-specify on 2026-10-19.
     Source of truth for implementation details; the requirements live in spec.md.
     Where they differ, spec.md wins: see its Clarifications (e.g. `--output=text|json`
     replaces `--format`, no forced colour off a terminal, JSON `stale` records). -->

# vigie — Specification

Oct 19, 2026 · @Sylvain

## Overview

vigie is a Go CLI that, run inside a git working copy, detects the GitLab project behind it and prints one colored status line: branches, running pipelines, last pipeline status, open merge requests and open issues. With `--watch`, it redraws that line every few seconds.

The name is French for the lookout in a ship's crow's nest: it watches, it reports, it never steers.

**Goals**

- Zero configuration inside a repository: the project is found from the git remote, the token from `GITLAB_TOKEN`.
- Readable at a glance: one line, fixed order of fields, color and an icon per field.
- Fast: a refresh completes in under 1 s on gitlab.com for a typical project, by running API calls concurrently.
- Works with gitlab.com and self-hosted GitLab instances.
- Small: a single static binary, few dependencies, no runtime needed.
- Read-only and safe: the token is never printed, logged or written to disk.

**Non-goals for v1**

- Not a replacement for `glab`: no write actions (no retry, no merge, no issue creation).
- No full-screen TUI or dashboard; one line only.
- No GitHub, Gitea or Bitbucket support.

## Usage and CLI interface

vigie has one default action (print the status line) and a few flags; there are no subcommands in v1 apart from `version`.

```
vigie [flags]
vigie version
```

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--watch` | `-w` | off | Redraw the status line in place until Ctrl+C. |
| `--interval` | `-i` | `5s` | Refresh period in watch mode (Go duration, minimum `2s`). |
| `--dir` | `-C` | `.` | Run as if started in this directory. |
| `--remote` | `-r` | `origin` | Git remote used to detect the project. |
| `--project` | `-p` | auto | Force the project path (`group/sub/project`) or numeric ID. |
| `--host` |  | auto | Force the GitLab base URL (e.g. `https://gitlab.example.com`). |
| `--fields` | `-f` | all | Comma-separated list and order of fields: `branches,pipelines,last,mrs,issues`. |
| `--format` |  | `line` | Output format: `line`, `plain` (no color, no icons) or `json`. |
| `--no-color` |  | off | Disable ANSI colors; also honored via `NO_COLOR`. |
| `--no-emoji` |  | off | Replace emojis with short ASCII labels. |
| `--timeout` |  | `10s` | Timeout for one full refresh. |
| `--verbose` | `-v` | off | Print debug information to stderr (never the token). |

Examples:

```
vigie                         # one-shot status line
vigie -w                      # refresh every 5 s
vigie -w -i 10s -f pipelines,last,mrs
vigie -C ~/src/cats --format json | jq .pipelines.running
vigie -p meteorage/infra/cats --host https://gitlab.example.com
```

## Project detection

vigie derives the GitLab host and project path from the URL of a git remote, then resolves the project through the API once at startup.

1. Find the repository root with `git rev-parse --show-toplevel` (run in `--dir`). Not a repository → exit code 2.
2. Read the remote URL with `git remote get-url <remote>`. This command already applies `url.<base>.insteadOf` rewrites. Missing remote → exit code 2 with the list of existing remotes.
3. Parse the URL into host and project path. Supported forms:
   - `git@gitlab.com:group/sub/project.git` (scp-like SSH)
   - `ssh://git@gitlab.example.com:2222/group/project.git`
   - `https://gitlab.example.com/group/project.git`, credentials in the URL are stripped and never logged
   - The `.git` suffix and trailing slashes are optional.
4. Build the API base URL: `https://<host>/api/v4`. When SSH and HTTP hosts differ (e.g. `ssh.gitlab.example.com` vs `gitlab.example.com`), the `hosts` map in the config file translates one to the other.
5. Call `GET /projects/:id` with the URL-encoded path (`group%2Fsub%2Fproject`) to get the numeric ID and `default_branch`. All later calls use the numeric ID.
6. Read the current branch with `git symbolic-ref --short HEAD`. On a detached HEAD, the default branch is used instead.

vigie shells out to the `git` binary rather than embedding a git library: it keeps the binary small and honors the user's git config. `--project` and `--host` skip steps 2 to 4, which also lets vigie run outside a repository.

## Authentication and configuration

vigie authenticates with a personal, group or project access token read from `GITLAB_TOKEN`, sent as the `PRIVATE-TOKEN` header; the `read_api` scope is enough.

**Security rules**

- The token is never printed, logged, included in errors or written to disk; `--verbose` shows it as `glpat-****`.
- The token is never sent over plain `http://`, unless the host is explicitly marked `insecure: true` in the config.
- The config file never holds a token itself; it can only point to an environment variable or a command (e.g. `pass`, `op`, `security find-generic-password`).
- A `401` stops vigie with a clear message; in watch mode, it stops the loop rather than retrying every interval.

**Environment variables**

| Variable | Purpose |
| --- | --- |
| `GITLAB_TOKEN` | API token (required unless a config entry provides one). |
| `GITLAB_HOST` | Default GitLab base URL when it cannot be derived from the remote. |
| `VIGIE_INTERVAL` | Default watch interval. |
| `VIGIE_CONFIG` | Path to an alternative config file. |
| `NO_COLOR` | Disables colors, per the no-color.org convention. |

**Config file (optional)**

Location: `$XDG_CONFIG_HOME/vigie/config.yaml`, falling back to `~/.config/vigie/config.yaml`.

```yaml
interval: 5s
fields: [branches, pipelines, last, mrs, issues]
emoji: true
hosts:
  gitlab.com: {}
  ssh.gitlab.example.com:
    api: https://gitlab.example.com
    token_env: GITLAB_EXAMPLE_TOKEN
  gitlab.internal.lan:
    token_command: ["pass", "show", "gitlab/internal"]
    insecure: false
```

Precedence, highest first: command-line flags, environment variables, config file, values detected from the repository, built-in defaults.

## Metrics and GitLab API

Each refresh runs six read-only calls to the GitLab REST API v4 concurrently. Counts come from the `X-Total` response header of a `per_page=1` request, so no list is downloaded, except for branches.

| Field | Value shown | API call | How it is computed |
| --- | --- | --- | --- |
| `branches` | Active / total branches | `GET /projects/:id/repository/branches?per_page=100` (all pages) | Total = number of branches; active = branches whose `commit.committed_date` is within the last 90 days (`active_days` in config). |
| `pipelines` | Running and pending pipelines | `GET /projects/:id/pipelines?status=running&per_page=1`, then `status=pending` | `X-Total` of each call. |
| `last` | Status of the latest pipeline on the current branch | `GET /projects/:id/pipelines?ref=<branch>&per_page=1` | First result's `status`, `id` and `updated_at`; no pipeline → `none`. |
| `mrs` | Open merge requests | `GET /projects/:id/merge_requests?state=opened&per_page=1` | `X-Total`. |
| `issues` | Open issues | `GET /projects/:id/issues_statistics` | `statistics.counts.opened`. |

**Notes**

- GitLab omits `X-Total` when a result has more than 10,000 rows. vigie then shows `10k+` instead of a wrong number.
- Branches are the only field that may need several pages. Its result is cached for 60 s in watch mode, so a refresh every 5 s does not list every branch every time.
- A field whose feature is disabled on the project (issues or merge requests turned off, CI not used) returns `403` or `404`. vigie shows `–` for that field and keeps the others.
- The `last` field maps all GitLab pipeline statuses: `created`, `waiting_for_resource`, `preparing`, `pending`, `running`, `success`, `failed`, `canceled`, `skipped`, `manual`, `scheduled`.
- A sixth call, `GET /projects/:id`, runs once at startup (Project detection, step 5) and again every 5 minutes in watch mode, to pick up a renamed default branch.

## Output format

The default `line` format prints the project, then one segment per field, in the order of `--fields`, separated by two spaces:

```
🗼 meteorage/infra/cats  🌿 12/42  ⚙️ 2 running · 1 pending  ✅ main #48213 3m ago  🔀 5 MRs  🐛 17 issues
```

**Segments**

| Field | Icon | Text | Color rule |
| --- | --- | --- | --- |
| project | 🗼 | `group/sub/project` | Bold; truncated from the left (`…/sub/project`) when the terminal is too narrow. |
| `branches` | 🌿 | `active/total` | Default color; dim when total is 1. |
| `pipelines` | ⚙️ | `N running · M pending` | Blue when N > 0, yellow when only M > 0, dim when both are 0. |
| `last` | per status | `<branch> #<id> <age>` | Per status, table below. |
| `mrs` | 🔀 | `N MRs` | Bold when N > 0, dim when 0. |
| `issues` | 🐛 | `N issues` | Bold when N > 0, dim when 0. |

**Pipeline status of the `last` field**

| Status | Icon | Color |
| --- | --- | --- |
| `success` | ✅ | Green |
| `failed` | ❌ | Red, bold |
| `running` | 🔄 | Blue |
| `pending`, `created`, `preparing`, `waiting_for_resource` | ⏳ | Yellow |
| `manual` | ✋ | Magenta |
| `scheduled` | 🕒 | Cyan |
| `canceled`, `skipped` | ⏭️ | Gray |
| no pipeline | ∅ | Dim |

The age is relative and short: `12s`, `3m`, `2h`, `4d`.

**Other formats**

- `--no-emoji` replaces icons with short labels: `br 12/42  ci 2r/1p  last:ok main #48213  mr 5  is 17`.
- `--format plain`, or stdout not being a terminal, removes colors and icons; `--format line` forces them back.
- `--format json` prints one JSON object per refresh (one per line in watch mode), for scripts and status bars:

```json
{"project":"meteorage/infra/cats","url":"https://gitlab.com/meteorage/infra/cats","branch":"main",
 "branches":{"active":12,"total":42},"pipelines":{"running":2,"pending":1},
 "last":{"id":48213,"status":"success","ref":"main","updated_at":"2026-10-19T09:12:03Z","web_url":"…"},
 "mrs":{"opened":5},"issues":{"opened":17},"fetched_at":"2026-10-19T09:15:00Z","errors":[]}
```

A count above the 10,000 cap is `null` in JSON with `"capped": true` on that field, and `10k+` on the line.

## Watch mode

`--watch` refreshes every `--interval` (default 5 s, minimum 2 s) and redraws the same terminal line until Ctrl+C, which exits with code 0.

**Refresh loop**

- The next refresh starts one interval after the previous one ends, so slow responses never pile up concurrent refreshes.
- Each refresh shares one `--timeout` (default 10 s) across all its calls, through a Go `context`.
- While a refresh is in flight, a dim `·` appears at the end of the line; it disappears when new values are drawn.

**Redraw**

- On a terminal, vigie hides the cursor, then rewrites the line with `\r` and the ANSI "erase line" sequence `ESC[2K`.
- On SIGINT, SIGTERM or a panic, the cursor is restored and a final newline is printed, so the shell prompt is never left on a broken line.
- On SIGWINCH (terminal resized), the line is redrawn at once with the new width.
- When stdout is not a terminal (a pipe, a log file), each refresh prints a new line, and only when the content changed.

**Failures during watch**

- A failed refresh keeps the last good values and appends a yellow `⚠ stale 15s` with the age of those values.
- A `429 Too Many Requests` or a `5xx` triggers exponential backoff (interval × 2, up to 60 s), honoring the `Retry-After` header when present.
- A `401` or `403` on the project itself stops the loop with exit code 3 (Errors and edge cases).

**API budget**

At the default 5 s interval, vigie sends about 5 requests per refresh (branches are cached for 60 s), or about 60 requests per minute per running instance. That stays well below gitlab.com's default per-user API limits; on self-hosted instances, the admin's rate-limit settings apply, and `--interval` is the knob to turn.

## Errors and edge cases

vigie fails loudly on setup problems and degrades quietly on per-field problems: a single unavailable field never hides the rest of the line.

**Exit codes**

| Code | Meaning | Examples |
| --- | --- | --- |
| 0 | Success, including partial results | One field returned `403`; it shows `–`. |
| 1 | Unexpected error | Bug, malformed API response. |
| 2 | Local setup problem | Not a git repository, remote missing, unparsable remote URL, invalid flag. |
| 3 | Authentication problem | `GITLAB_TOKEN` empty, `401`, `403` on the project itself. |
| 4 | Project not found | `404` on `GET /projects/:id` (wrong host mapping, or no access). |
| 5 | Network problem in one-shot mode | DNS failure, TLS error, timeout. |

Error messages go to stderr, on one line, with a hint: for example `GITLAB_TOKEN is not set: create a token with the read_api scope and export it`.

**Edge cases**

- Empty project (no commit yet): branches `0/0`, `last` shows `∅`.
- Fork: the counts are the fork's own; `--remote upstream` shows the parent project.
- Git worktrees and submodules: detection follows the git commands, so a submodule directory shows the submodule's project.
- Archived project: the project segment gets a gray `archived` tag.
- Draft merge requests are counted with the others in v1.
- Self-signed certificates: vigie uses the system trust store; `SSL_CERT_FILE` adds a custom CA, and there is no option to skip verification.

## Architecture and tech choices

vigie is a small Go module split into internal packages, each with one job; only `internal/gitlab` talks to the network and only `internal/render` writes to the terminal.

&#91;embedded content: vigie architecture · 7 packages, 4 external parts\]

Config and project detection run once at startup; in watch mode, `internal/watch` repeats only the fetch and render steps.

**Project layout**

```
vigie/
├── cmd/vigie/main.go
├── internal/
│   ├── config/    flags, env, YAML, precedence
│   ├── repo/      git calls, remote URL parsing
│   ├── gitlab/    HTTP client, one function per metric
│   ├── status/    Snapshot type shared by fetch and render
│   ├── render/    line, plain, json; icons and colors
│   └── watch/     loop, backoff, redraw, signals
├── testdata/      API fixtures, golden outputs
├── Taskfile.yml
├── .goreleaser.yaml
└── .gitlab-ci.yml
```

**Dependencies**, kept to a minimum:

| Need | Choice | Why |
| --- | --- | --- |
| GitLab API | Standard library `net/http` and `encoding/json` | Six endpoints do not justify a full GitLab client library. |
| Concurrency | `golang.org/x/sync/errgroup` | Runs the calls in parallel under one context and timeout. |
| Flags | `github.com/spf13/pflag` | GNU-style short and long flags without a full CLI framework. |
| Terminal | `golang.org/x/term` | TTY detection and terminal width. |
| Config file | `go.yaml.in/yaml/v3` | Small YAML parser for the optional config. |
| Colors | Hand-written ANSI codes in `internal/render` | A dozen escape sequences; no library needed. |

**Testing**

- `internal/repo`: table-driven tests over remote URL forms (SSH, scp-like, HTTPS, ports, credentials, `.git` suffix).
- `internal/gitlab`: tests against `httptest.Server` with JSON fixtures, covering `X-Total` missing, `403`, `404`, `429` and timeouts.
- `internal/render`: golden files for each format, with and without colors and emojis, and for narrow terminal widths.
- `internal/watch`: loop and backoff tested with an injected clock, so tests never sleep.
- A manual smoke test against a real gitlab.com project before each release.

## Distribution and release

vigie ships as a static binary built by GoReleaser from a git tag, published to GitLab Releases and to the Homebrew tap `sgaunet/homebrew-tools`.

- Targets: linux, darwin and windows on amd64 and arm64, with `CGO_ENABLED=0`.
- Version, commit and build date are injected with `-ldflags` and shown by `vigie version`.
- Each release carries a `checksums.txt` and an SBOM; signing with cosign is optional for v1.
- Install paths: `brew install sgaunet/tools/vigie`, `go install <module>/cmd/vigie@latest`, or a download from the release page.
- Local tasks run through Taskfile (`task build`, `task test`, `task lint`, `task snapshot`), with the Go toolchain pinned by mise.
- GitLab CI stages: lint (`golangci-lint`), test (with `-race`), vulnerability scan (`govulncheck`), then release on tags only.

Open questions: the module path (GitLab or GitHub hosting) and the license (MIT, as for most CLI tools of this kind, or another).

## Roadmap

These ideas stay out of v1 to keep it a one-line, read-only tool; each can land later without breaking the CLI.

- Prompt and status-bar integration: a `--cache` mode reading a short-lived local cache, so a shell prompt, tmux or Starship can call vigie on every prompt without hitting the API.
- Personal filters: MRs assigned to me or awaiting my review, issues assigned to me.
- Draft MRs counted separately, and MRs with failed pipelines.
- Notifications in watch mode: a desktop notification or terminal bell when the last pipeline turns `failed`.
- `vigie open`: open the project, the last pipeline or the MR list in the browser.
- GraphQL backend: one request per refresh instead of six.
- Conditional requests with ETags, to cut load on large instances.
- Multi-project view: one line per repository under a directory.
