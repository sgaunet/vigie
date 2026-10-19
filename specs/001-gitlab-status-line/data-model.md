# Data Model: vigie — GitLab Project Status Line

**Feature**: [spec.md](./spec.md) · **Research**: [research.md](./research.md)

Types are listed by owning package. All are concrete (Principle V); none is generic.

## config

### Settings

The merged configuration (spec entity *Configuration*). Built by `config.Load(overrides,
env, fileFS)`, where `overrides` is a plain struct filled by `internal/cli` (no CLI import).

| Field | Type | Default | Sources (highest first) | Validation |
|---|---|---|---|---|
| `Watch` | bool | false | flag | — |
| `Interval` | `time.Duration` | 5s | flag, `VIGIE_INTERVAL`, file `interval` | ≥ 2s, else `ErrInvalid` |
| `Dir` | string | `.` | flag | must exist |
| `Remote` | string | `origin` | flag | non-empty |
| `Project` | string | "" (detect) | flag | `group/…/name` (≥ 2 segments) or digits |
| `Host` | `*url.URL` | nil (detect) | flag `--host` › detected › `GITLAB_HOST` | absolute, `https` unless insecure host |
| `Fields` | `[]status.FieldName` | all 5, default order | flag, file `fields` | known names, no duplicates, ≥ 1 |
| `Output` | `Output` (`text`\|`json`) | `text` | flag | enum |
| `Color` | bool | true | `--no-color`, `NO_COLOR` (non-empty) | — |
| `Emoji` | bool | true | `--no-emoji`, file `emoji` | — |
| `Timeout` | `time.Duration` | 10s | flag | > 0 |
| `LogLevel` | `slog.Level` | Warn | `--verbose` → Debug, `--quiet` → Error | both flags → `cli.ErrUsage` |
| `ActiveDays` | int | 90 | file `active_days` | ≥ 1 |
| `Hosts` | `map[string]HostProfile` | empty | file `hosts` | see HostProfile |

Config file path: `VIGIE_CONFIG`, else `$XDG_CONFIG_HOME/vigie/config.yaml`, else
`~/.config/vigie/config.yaml`. A missing default file is not an error; a missing
`VIGIE_CONFIG` file is (`ErrInvalid`). An `--interval` without `--watch` is accepted and
ignored.

### HostProfile

Spec entity *Host profile*. Key = host name from the remote URL, without port. `--host` and
`GITLAB_HOST` match an entry by host name (port ignored), by key or by `API` host.

| Field | YAML | Type | Rule |
|---|---|---|---|
| `API` | `api` | `*url.URL` | optional web base URL; default `https://<key>` |
| `TokenEnv` | `token_env` | string | env var name; exclusive with `TokenCommand` |
| `TokenCommand` | `token_command` | `[]string` | argv, no shell; exclusive with `TokenEnv` |
| `Insecure` | `insecure` | bool | allows an `http://` `API` |

Any `token` key, anywhere in the file → `ErrTokenInFile`. Lookup for a host given with `--host`
or `GITLAB_HOST`: the entry whose key or `API` host matches.

## repo

### Remote

| Field | Type | Notes |
|---|---|---|
| `Host` | string | host name only, no port, no userinfo |
| `Path` | string | `group/sub/project`, no `.git`, no leading or trailing `/` |

`repo.ParseRemote(raw string) (Remote, error)` accepts scp-like, `ssh://` (port dropped) and
`http(s)://` (userinfo dropped) forms; anything else → `ErrRemoteURL`, whose message quotes the
URL with credentials removed.

### Checkout

| Field | Type | Source |
|---|---|---|
| `Root` | string | `git rev-parse --show-toplevel` |
| `Branch` | string | `git symbolic-ref --short HEAD`; "" when detached |
| `Remote` | `repo.Remote` | parsed inside `Detect` from `git remote get-url <remote>` (insteadOf applied by git); the raw URL is never stored, returned or logged |

## token

### Token

Opaque value; the raw string is unexported. Formatting, JSON and `slog` all yield the mask
(`glpat-****`, or `****`). `token.Resolve(ctx, profile, env) (Token, error)`.

## gitlab

### Project

Spec entity *Project*.

| Field | JSON (API) | Type |
|---|---|---|
| `ID` | `id` | int64 |
| `Path` | `path_with_namespace` | string |
| `WebURL` | `web_url` | string |
| `DefaultBranch` | `default_branch` | string ("" for an empty project) |
| `Archived` | `archived` | bool |

### Client

Holds the API base URL, the `Token` and an `*http.Client` configured per R8/R9. It implements
`status.Source`:

```go
type Source interface {
    Branches(ctx context.Context, activeSince time.Time) (status.Branches, error)
    Pipelines(ctx context.Context) (status.Pipelines, error)
    LastPipeline(ctx context.Context, ref string) (status.LastPipeline, error)
    OpenMRs(ctx context.Context) (status.Count, error)
    OpenIssues(ctx context.Context) (status.Count, error)
}
```

A single interface with one concrete implementation in production and one fake in tests; this
is the seam that lets `status` and `watch` be tested without HTTP.

## status

### FieldName

Enum: `branches`, `pipelines`, `last`, `mrs`, `issues`. Default order is the declaration
order. Parsed from text; unknown → error naming the valid values.

### Count

| Field | Type | Meaning |
|---|---|---|
| `N` | int | exact value when `Capped` is false |
| `Capped` | bool | more than 10,000; `N` undefined |

### Field values

| Type | Fields |
|---|---|
| `Branches` | `Active`, `Total` `Count` |
| `Pipelines` | `Running`, `Pending` `Count` |
| `LastPipeline` | `ID` int64, `Status` `PipelineStatus`, `Ref` string, `UpdatedAt` time, `WebURL` string; `Status = None` when no pipeline |
| `Count` | for `mrs` and `issues` |

### PipelineStatus

Enum: `created`, `waiting_for_resource`, `preparing`, `pending`, `running`, `success`,
`failed`, `canceled`, `skipped`, `manual`, `scheduled`, `none`. An unknown string from the API
maps to `unknown` (rendered `?`, gray) rather than failing the refresh: the API may add
statuses.

### FieldResult

| Field | Type | Notes |
|---|---|---|
| `Name` | `FieldName` | |
| `Value` | one of the field value types | zero when `Err` is set |
| `Err` | error | `nil`, or wraps `ErrUnavailable` (403/404, disabled feature) or a transport error |

### Snapshot

Spec entity *Snapshot*.

| Field | Type | Notes |
|---|---|---|
| `Project` | `gitlab.Project` view (path, web URL, archived) | copied, so `status` imports no `gitlab` |
| `Branch` | string | reporting branch (current, or default when detached) |
| `Fields` | `[]FieldResult` | in requested order |
| `FetchedAt` | `time.Time` | time of the last **successful** collection |
| `Stale` | bool | the latest refresh failed; values are from `FetchedAt` |
| `StaleSince` | `time.Time` | first failed refresh of the current failure streak |

`Snapshot.Fingerprint()` returns a comparable value excluding `FetchedAt`, `StaleSince` and
ages (FR-031).

### State transitions (watch)

```text
          ok                     fail
 [Fresh] ────▶ [Fresh]   [Fresh] ────▶ [Stale(since=t)]
 [Stale] ────▶ [Fresh]   [Stale] ────▶ [Stale(since unchanged)]
 any ── 401 / 403 on project ──▶ [Stopped, exit 3]
 any ── SIGINT / SIGTERM ─────▶ [Stopped, exit 0]
```

A field-level error (`ErrUnavailable`) is a value, not a failed refresh: the snapshot stays
Fresh. A refresh fails when the deadline passes, or when every field failed with a transport
error.

## watch

### Backoff

| Field | Type | Rule |
|---|---|---|
| `Base` | `time.Duration` | `Interval` |
| `Failures` | int | consecutive throttled or server-error refreshes |
| `RetryAfter` | `time.Duration` | last server hint, capped at 5 min |

`Next() = max(min(Base × 2^Failures, 60s), RetryAfter)`; reset on success.

## render

### Style

| Field | Type | Source |
|---|---|---|
| `Color` | bool | `Settings.Color` and stdout is a TTY |
| `Emoji` | bool | `Settings.Emoji` and stdout is a TTY |
| `Width` | int | terminal columns; 0 = unlimited (non-TTY) |
| `InFlight` | bool | watch mode, stderr is a TTY; the marker is written to stderr, never stdout |

Icon, ASCII label and color per field and per `PipelineStatus`: one table in
`internal/render/icons.go`, the single source for the spec's FR-019 tables.
