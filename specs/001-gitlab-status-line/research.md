# Research: vigie — GitLab Project Status Line

**Feature**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md) · **Date**: 2026-10-19

Each entry: Decision / Rationale / Alternatives considered. Author decisions taken during
`/speckit-plan` are marked **(author)**.

## R1. Module path and hosting **(author)**

- **Decision**: module `github.com/sgaunet/vigie`; CI on GitHub Actions; releases on GitHub
  Releases through goreleaser v2; Homebrew formula pushed to `sgaunet/homebrew-tools`.
- **Rationale**: the `origin` remote is `git@github.com:sgaunet/vigie.git`, and the tap lives on
  GitHub. The design's `.gitlab-ci.yml` and GitLab Releases are replaced by `.github/workflows/`.
- **Alternatives**: GitLab hosting (needs a mirror and a different module path).
- **License**: MIT, already in `LICENSE`; resolves the design's open question.

## R2. Toolchain

- **Decision**: `go 1.27.0` and `toolchain go1.27.2` in `go.mod`; `mise.toml` pins `go 1.27.2`,
  `task`, `golangci-lint 2.14.x`, `goreleaser 2.x`.
- **Rationale**: Go 1.27 is the current stable line (local 1.27.1, mise offers 1.27.2). The
  feature uses Go ≥ 1.25 APIs: `sync.WaitGroup.Go`, `testing/synctest`.
- **Alternatives**: stay on 1.27.1 (misses the latest patch release).

## R3. Flag parsing **(author)**

- **Decision**: stdlib `flag` with `flag.ContinueOnError`. Each option is registered under its
  long name and, where it has one, its short name, both bound to the same variable (`-w` and
  `--watch`; stdlib accepts `-` and `--` alike). `version` is recognised as the first positional
  argument and parsed by a second `FlagSet` (accepting `--output`). Constitution amended to
  v1.1.1 to allow this.
- **Rationale**: zero dependencies; one default action does not need a framework.
- **Consequences**: no bundled short flags (`-wv` is rejected; write `-w -v`). The default
  usage output is replaced by an embedded help text (`//go:embed help.txt`) that lists pairs,
  defaults, precedence, environment variables and exit codes (FR-033, FR-035).
  `-h/--help` prints help to **stdout** and exits 0 (explicitly requested data); a parse error
  prints one error line plus `Run 'vigie --help' for usage.` to stderr and exits 2.
- **Alternatives**: cobra (allowed, heavier), pflag (needed an amendment, one more dependency).

## R4. Dependencies **(author approved)**

| Module | Licence | Use | Status |
|---|---|---|---|
| `golang.org/x/term` | BSD-3 | `IsTerminal`, `GetSize` | Used |
| `go.yaml.in/yaml/v3` | MIT / Apache-2.0 | config file, strict decoding (`KnownFields`) | Used |
| `golang.org/x/sync/errgroup` | BSD-3 | — | Approved, **not used** |

- **errgroup**: not used. Its value is cancelling siblings on the first error, but FR-015 needs
  every field to finish independently. `sync.WaitGroup.Go` plus one shared `context` covers
  the need with the standard library (Constitution: stdlib first). The approval stays on record
  if the need appears.
- **Windows ANSI**: enabling `ENABLE_VIRTUAL_TERMINAL_PROCESSING` uses `syscall` and a lazy
  `kernel32.dll` `SetConsoleMode` call in a `_windows.go` file, so `golang.org/x/sys` stays an
  indirect dependency (it comes in through `x/term`). If enabling fails, colors are disabled.

## R5. Concurrency of one refresh

- **Decision**: `status.Collect(ctx, source, req)` derives `ctx` with `--timeout`, starts one
  goroutine per requested field with `sync.WaitGroup.Go`, each writing its own result slot;
  no shared mutable state, so no mutex. The pipelines field issues its two counts concurrently
  too.
- **Rationale**: one deadline for the whole refresh (FR-014); a failure never cancels siblings.
- **Branches pagination**: first page `per_page=100`; if `X-Total-Pages` is present, the rest
  are fetched concurrently (at most 4 in flight); if absent (more than 10,000 rows), stop and
  mark the field capped. Hard stop at 100 pages.

## R6. GitLab API behaviours relied on

- `X-Total`/`X-Total-Pages` are omitted when a result exceeds 10,000 rows → `capped` (FR-016).
  A response without `X-Total` but with a full page is treated as capped; with an empty body
  as 0.
- `GET /projects/:id` with the URL-encoded path returns `id`, `default_branch`, `web_url`,
  `archived`, `path_with_namespace`. `--project` accepts a numeric ID unchanged.
- Latest pipeline: `GET /projects/:id/pipelines?ref=<branch>&per_page=1&order_by=id&sort=desc`.
- Issues: `GET /projects/:id/issues_statistics` → `statistics.counts.opened` (exact, no cap).
- Disabled features answer `403` or `404` on the field endpoint → field unavailable (FR-015).
- `401` anywhere → authentication error. `403`/`404` on `GET /projects/:id` → exit 3 / 4.
- `429` and `5xx` carry an optional `Retry-After` (seconds or HTTP date); both forms parsed.

## R7. Error classification → exit codes

Sentinel errors live in the domain package that detects them; `cmd/vigie` maps them with
`errors.Is` in a single function, covered by a table test.

| Code | Sentinels |
|---|---|
| 2 | `cli.ErrUsage`, `config.ErrInvalid`, `config.ErrTokenInFile`, `config.ErrInsecureHost`, `repo.ErrNotRepository`, `repo.ErrRemoteMissing`, `repo.ErrRemoteURL`, `config.ErrNoHost` |
| 3 | `token.ErrMissing`, `token.ErrCommand`, `gitlab.ErrUnauthorized`, `gitlab.ErrForbidden` (on the project) |
| 4 | `gitlab.ErrProjectNotFound` |
| 5 | `gitlab.ErrNetwork` (DNS, TLS, connection, timeout, `429`/`5xx` in one-shot mode) |
| 1 | anything else, including malformed JSON and panics |

- **One-shot partial failures**: if the project resolves and at least one field succeeds, the
  run exits 0 with per-field errors (FR-015). If **every** requested field failed with a
  network error, the run exits 5 (nothing usable was fetched).
- **Interruption**: watch mode → 0 (FR-028); one-shot → 1, nothing on stdout.
- Every error message is one line, `<what>: <hint>`, built with `%w` wrapping (Principle V).

## R8. Host, token and transport security

- **Host resolution order**: `--host` > detected remote host (through the `hosts` map) >
  `GITLAB_HOST` > error `config.ErrNoHost` (exit 2); constitution v1.2.0 allows fallback-only
  variables below detection. FR-007 makes `GITLAB_HOST` a fallback
  "when it cannot be derived otherwise", so for the host it ranks **below** detection, unlike
  other settings; `--help` says so. There is deliberately no built-in `gitlab.com` default, so
  a token is never sent to a host the user did not name.
- **Token resolution** (FR-008): host entry `token_env` → host entry `token_command` (run with
  a 5 s timeout, stdout trimmed, stderr discarded, non-zero exit → `token.ErrCommand`) →
  `GITLAB_TOKEN` → `token.ErrMissing`. `token_env` and `token_command` together in one entry →
  `config.ErrInvalid`.
- **Token type**: `token.Token` keeps the value unexported; `String()`, `GoString()`,
  `MarshalJSON()`, `Format()` and `slog.LogValuer` all return the mask (`glpat-****`, or `****`
  for other prefixes). Only `gitlab` reads the raw value, through one method, to set
  `PRIVATE-TOKEN`.
- **Transport**: refuse a non-`https` API URL unless the host entry has `insecure: true`
  (`config.ErrInsecureHost`). `CheckRedirect` refuses any redirect that changes host or
  downgrades the scheme: Go strips `Authorization` on cross-host redirects but not custom
  headers like `PRIVATE-TOKEN`. TLS roots: vigie reads `SSL_CERT_FILE` itself and appends it
  to `x509.SystemCertPool()`. Go honours that variable natively only on Linux, so this is
  needed on macOS and Windows; a test proves the appended CA is trusted on the OS it runs on
  (CI runs Linux only until an OS matrix is added, analyze finding E3). An
  unreadable or invalid file → `config.ErrInvalid` (exit 2).
  `InsecureSkipVerify` is never set (a `forbidigo` lint rule enforces it).
- **Config file**: strict YAML decoding; a `token` key anywhere → `config.ErrTokenInFile`;
  unknown keys → `config.ErrInvalid`.
- **Remote URL credentials**: removed at parse time; the parsed type keeps no userinfo.
- **Logging**: slog text handler to stderr; level Warn by default, Debug with `--verbose`,
  Error with `--quiet`. A `ReplaceAttr` hook masks any attribute whose key contains `token`
  (defence in depth). An end-to-end test greps all captured output for the token (SC-003).

## R9. Timeouts and retries (Principle VI)

| I/O | Timeout | Retries |
|---|---|---|
| Each `git` command | 5 s (`exec.CommandContext`) | none |
| `token_command` | 5 s | none |
| One refresh (all HTTP calls) | `--timeout`, default 10 s | none inside a refresh |
| HTTP transport | dial 5 s, TLS handshake 5 s, response header = refresh deadline | — |
| Watch loop after `429`/`5xx`/network | — | unbounded count, bounded delay: `interval × 2^n`, cap 60 s, or `Retry-After` if larger (capped at 5 min) |

The watch loop is the only retry mechanism: one-shot mode fails fast.

## R10. Watch loop

- **Decision**: `watch.Run(ctx, deps)` is a plain `for` loop with `time.NewTimer`: refresh →
  render → sleep `nextDelay` → repeat; the next refresh starts one delay after the previous
  ended (FR-026). Branch counts are reused while younger than 60 s (FR-018); the project is
  re-resolved every 5 minutes (FR-005).
- **Signals**: `cmd/vigie` creates the root context with `signal.NotifyContext(SIGINT,
  SIGTERM)`. `SIGWINCH` (unix build tag only) feeds a channel that triggers an immediate
  redraw from the last snapshot; on Windows the width is re-read at each draw.
- **Terminal restore**: a `display.Close()` deferred in `run()` shows the cursor and writes the
  final newline; a deferred `recover` in `main` calls it before exiting 1 on a panic (FR-028).
- **Tests**: `testing/synctest` (stdlib, Go ≥ 1.25) runs the loop with a fake clock inside a
  bubble, so backoff and intervals are asserted without sleeping. This replaces the design's
  injected clock: less code, same guarantee.

## R11. Output and change detection

- **Text** (FR-019–FR-022): `render.Text` produces a string from a `status.Snapshot` and a
  `render.Style` (color, emoji, width). TTY detection happens once in `cmd/vigie`; `NO_COLOR`
  (non-empty) or `--no-color` turns color off; a non-TTY stdout turns color, icons, cursor
  control and the in-flight marker off, and nothing can turn them back on (Clarification Q3).
- **Width**: measured in display cells (emoji = 2 cells, `…` = 1), with a small table for the
  icons used; the project path is cut from the left until the line fits; other segments are
  never cut.
- **JSON** (FR-023, FR-031a): `encoding/json` over dedicated output structs (not the domain
  types), so the schema in [contracts/status.schema.json](./contracts/status.schema.json) is
  the only contract. One object per line, `\n`-terminated.
- **Change detection** (FR-031): compare a fingerprint of the snapshot with time fields zeroed
  (`fetched_at`, `stale_since`, ages). In text mode on a non-TTY, ages are rendered but
  excluded from the fingerprint, so `3m ago` → `4m ago` alone does not emit a new line.

## R12. Build, lint, release

- **Taskfile**: `build` (`CGO_ENABLED=0 go build -trimpath -ldflags …`), `test`
  (`go test -count=2 -race ./...`), `lint` (`golangci-lint run`), `snapshot`
  (`goreleaser release --snapshot --clean`), `release` (`goreleaser release --clean`).
  `-race` requires cgo, so `task test` runs with `CGO_ENABLED=1`; the shipped binary does not.
- **Version injection**: `-ldflags -X main.version= -X main.commit= -X main.date=`, falling
  back to `runtime/debug.ReadBuildInfo` for `go install` builds.
- **goreleaser v2**: linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`, `-trimpath`,
  `checksums.txt` (SHA-256), SBOM via syft, Homebrew formula to `sgaunet/homebrew-tools`.
  Cosign signing is left out of v1 (optional per design).
- **GitHub Actions**: `ci.yml` (lint, test, `govulncheck`, `CGO_ENABLED=0` build) on push/PR;
  `release.yml` on `v*` tags.
- **golangci-lint v2**: standard linters plus `errorlint`, `wrapcheck`, `gosec`, `revive`,
  `forbidigo` (no `fmt.Print*` outside `cmd/vigie` and `internal/render`, no
  `InsecureSkipVerify`), `depguard` (`flag` only in `internal/cli`; `net/http` only in
  `internal/gitlab`; `os/exec` only in `internal/repo` and `internal/token`), and
  `testpackage` (black-box tests, Principle III). This turns the architecture rules into lint
  failures instead of review comments.
- **Code generation**: none needed in v1, so there is no `tool` directive yet.
