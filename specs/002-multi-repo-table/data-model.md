# Data Model: Multi-Repository Status Table

**Feature**: [spec.md](./spec.md) · **Research**: [research.md](./research.md)

Additions and changes only; feature 001's [data-model.md](../001-gitlab-status-line/data-model.md)
still describes everything else. Types are listed by owning package, all concrete
(Principle V). The dependency direction stays `cmd → cli → {config, repo, token, gitlab,
scan, render} → status`; `scan` imports `config`, `repo`, `token`, `gitlab` and `status`, and
nothing imports `scan` except `cli` and `cmd/vigie`.

## config

### Overrides / Settings (changed)

| Field | Type | Default | Sources | Validation |
|---|---|---|---|---|
| `Depth` (new) | `*int` / int | 4 (`DefaultDepth`) | flag `--depth` | ≥ 1, else `ErrInvalid` (`invalid --depth 0: minimum is 1`); a non-integer is a flag parse error (`cli.ErrUsage`) |

`Dir` now also names the scan root; `Host` now also filters a scan (FR-005a). No config file
key or environment variable for the depth (not in the spec).

### Host recognition (new methods, `host.go`)

| Method | Rule |
|---|---|
| `(Settings) KnownHost(remoteHost string) bool` | `hostKey(remoteHost)` is `gitlab.com`, a `Hosts` key, the host of a profile `API`, or the host of `FallbackHost` (`GITLAB_HOST`) |
| `(Settings) OnHost(remoteHost string, base *url.URL) bool` | `hostKey(remoteHost) == hostKey(base.Host)`, or the profile matched by `remoteHost` has `API` equal to `base` (scheme, host incl. port, path) |

`ResolveHost` is unchanged and gives every listed repository its `Endpoint`.

## repo

| Change | Detail |
|---|---|
| `ErrNoGit` (new sentinel) | wrapped together with `ErrNotRepository` when `git` is not on PATH, so callers can tell "no git" from "not a repository"; exit code unchanged (2) |

## gitlab

| Change | Detail |
|---|---|
| `Options.Retry bool` (new) | retry `429`/`5xx` inside the context deadline (research S6); false in single mode |
| `Options.OnThrottle func()` (new) | called once per `429`/`5xx` answer, before any retry; may be nil |
| `RefreshError(fields []status.FieldResult) error` (moved) | was `cli.runner.fatal`: the 401 of any field, or the first error when every field failed on the network; nil otherwise |

## status

### Failure (new)

`type Failure string`: why a scan row has no values. The zero value means the row was fetched.

| Value | Marker text | Errors (assigned by `scan.Classify`) |
|---|---|---|
| `FailureAuth` | `auth` | `token.ErrMissing`, `token.ErrCommand`, `gitlab.ErrUnauthorized`, `gitlab.ErrForbidden` |
| `FailureNotFound` | `not found` | `gitlab.ErrProjectNotFound` |
| `FailureNetwork` | `network` | `gitlab.ErrNetwork` |
| `FailureError` | `error` | anything else |

### Row (new)

Spec entity *Row*. One listed repository of a scan, rendered by `render.Table` and
`render.JSONRow`.

| Field | Type | Notes |
|---|---|---|
| `Dir` | string | path relative to the scan root, `/`-separated on every platform, e.g. `tools/cli` |
| `Snapshot` | `Snapshot` | when fetched: as in single mode (`Stale` always false). When failed: `Project.Path` from the remote, `Project.WebURL` = endpoint base + `/` + path, `Branch` from the checkout, `Fields` = requested names with no values, `FetchedAt` = attempt time |
| `Failure` | `Failure` | `""` when fetched |
| `Err` | error | the row error when failed; never contains the token (001 FR-009) |

`Row.OK()` reports `Failure == ""`.

## scan (new package)

Spec entities *Scan root*, *Discovered repository* and *Scan summary*. Business logic only, no
terminal or flag code (Principle IV); `net/http` and `os/exec` stay in `gitlab` and `repo`
(depguard).

### Candidate

A directory holding a `.git` entry, found by `Find`.

| Field | Type | Notes |
|---|---|---|
| `Path` | string | absolute OS path, given to `repo.Detect` |
| `Rel` | string | relative to the root, `/`-separated; becomes `Row.Dir` |

### Inspected

A candidate that git could read: `Inspected{Rel string; Checkout repo.Checkout}`.

### Reason and Skip

`type Reason string`, why a repository gets no row. Each one is counted in the summary and
listed with `--verbose` (FR-006). `Reason.Text(remote string)` gives the text below; only
`ReasonNoRemote` uses the remote name.

| Reason | Text | Set by |
|---|---|---|
| `ReasonBare` | `bare` | `Find` (`HEAD` + `objects/` + `refs/`) |
| `ReasonUnreadable` | `unreadable` | `Find` (walk error), `Inspect` (`repo.ErrNotRepository`) |
| `ReasonNoRemote` | `no remote "<name>"` | `Inspect` (`repo.ErrRemoteMissing`) |
| `ReasonRemoteURL` | `remote URL not understood` | `Inspect` (`repo.ErrRemoteURL`) |
| `ReasonUnknownHost` | `unknown host` | `Select`, without `--host` |
| `ReasonOtherHost` | `other host` | `Select`, with `--host` |

`Skip{Rel string; Reason Reason; Err error}`. `Err` only appears in debug logs, never in the
summary line. Hidden directories and symbolic links are not repositories and are not counted.

### Repo

A listed repository, the output of `Select` and the input of `Fetch`.

| Field | Type | Notes |
|---|---|---|
| `Rel` | string | from `Candidate` |
| `Checkout` | `repo.Checkout` | branch and parsed remote |
| `Endpoint` | `config.Endpoint` | from `config.ResolveHost` |
| `Err` | error | set instead of `Endpoint` when resolution fails (e.g. http host not marked insecure); the row then fails with `FailureError` and no request is sent |

### Summary

| Field | Type | Notes |
|---|---|---|
| `Found` | int | `Listed` plus the number of skips (candidates, bare repositories and unreadable directories), so the summary line always adds up |
| `Listed` | int | `len(repos)` |
| `Skipped` | `map[Reason]int` | rendered in a fixed order (table above) |
| `Remote` | string | `--remote`, for the `no remote "<name>"` text |

Methods: `Skips()` gives `2 skipped (1 no remote "origin", 1 unknown host)`, reasons in the
fixed order, or `""` when nothing was skipped; `Line()` gives the stderr summary line;
`Err(depth)` is nil when something is listed, else the "nothing to list" error of the
Errors table. One formatter feeds the summary line, `ErrNoGitLab` and `AllFailedError`.

### HostFailure

`HostFailure{Err error; Rows int}`: a host that rejected the token or whose token could not
be read, printed on stderr as `<Err> (<Rows> repositories)`, or `(1 repository)`.

### Functions

| Function | Signature (sketch) | Notes |
|---|---|---|
| `Find` | `(ctx, root string, depth int) ([]Candidate, []Skip, error)` | `ErrRoot` when root is not a readable directory |
| `Inspect` | `(ctx, []Candidate, remote string, log) ([]Inspected, []Skip)` | `repo.Detect`, 8 at a time, order kept |
| `Select` | `([]Inspected, config.Settings) ([]Repo, []Skip)` | pure; recognition and `--host` filter |
| `Fetch` | `(ctx, []Repo, FetchConfig) []status.Row` | rows in input order (tree order) |
| `Classify` | `(error) status.Failure` | table above |
| `Summarize` | `(listed int, skips []Skip, remote string) Summary` | `Found = listed + len(skips)` |
| `Outcome` | `(rows []status.Row, sum Summary) error` | nil when any row is OK; else `*AllFailedError` carrying `sum` |
| `Latched` | `([]status.Row) ([]HostFailure, []status.Row)` | failed rows whose `Err` wraps a 401 or token error, grouped by identical error value in first-appearance order; second result: the other failed rows |

`FetchConfig{Fields []status.FieldName; Timeout time.Duration; ActiveDays int; Env
map[string]string; Log *slog.Logger; OnDone func(done, total int)}`. `OnDone` is called under a
mutex, in completion order.

### host (internal)

One per distinct `Endpoint.API` + token source.

| Field | Notes |
|---|---|
| `once`, `client`, `initErr` | token resolved and client built on first use; `initErr` (token error, insecure URL) latches |
| `latched error` | first 401 or token error, set by compare-and-swap; later repositories fail with it without a request, and a repository whose own request got 401 stores this same value, so every row of the host carries one error value (FR-009), which `Latched` groups on |
| `slots` | semaphore that can shrink; limit starts at `ReposPerHost` = 4; `Acquire(ctx) error`, `Release()`, `Halve()` (≥ 1, at most once per second) |

### Errors

| Error | Meaning | Exit code |
|---|---|---|
| `ErrRoot` | the scan root is missing or unreadable | 2 |
| `ErrNoGitLab` | repositories found, none listed; message carries the skip counts | 2 |
| *(wraps `repo.ErrNotRepository`)* | no repository below the root at all; 001 message prefix kept | 2 |
| `*AllFailedError{Errs []error; Skipped Summary}` | every listed row failed; no `Unwrap` | common exit code of `Errs` (mapped in `cmd/vigie`), else 1 |

## render

| Addition | Notes |
|---|---|
| `Table(rows []status.Row, fields []status.FieldName, st Style, now time.Time) string` | header + rows, `\n`-terminated lines (research S10) |
| `JSONRow(w io.Writer, r status.Row) error` | 001 record + `dir`; failed row as in research S11 |
| `Progress` | `NewProgress(w io.Writer, color bool)`, `Update(done, total int) error`, `Clear() error`; writes `\r ESC[2K · done/total` |
| `ShowProgress(stdoutTTY, stderrTTY, quiet, verbose bool) bool` | true only when both streams are terminals and neither flag is set (FR-013a, constitution Principle II) |

`Style` is unchanged; `Width` limits the table as it limits the line.

## State: one scan

```text
walk ──► candidates ──► inspect (git) ──► select (hosts) ──► fetch (GitLab) ──► rows ──► output
  │            │                 │                 │                    │
  └─ skips ────┴──── skips ──────┴──── skips ──────┘                    └─ per-row failure

no candidates            → ErrNotRepository (exit 2)
candidates, none listed  → ErrNoGitLab (exit 2)
listed, none OK          → AllFailedError (common code, else 1), stdout empty
listed, ≥ 1 OK           → table / JSON on stdout, summary + latched hosts on stderr, exit 0
Ctrl+C at any stage      → counter erased, stdout empty, exit 1
```
