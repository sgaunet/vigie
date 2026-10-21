# Data Model: GitHub Support

**Feature**: [spec.md](./spec.md) · **Research**: [research.md](./research.md)

Additions and changes only. Feature 001's [data-model.md](../001-gitlab-status-line/data-model.md)
and feature 002's [data-model.md](../002-multi-repo-table/data-model.md) still describe
everything else. Types are listed by owning package, all concrete except the one interface
`forge.Client` (two implementations).

Dependency direction:

```text
cmd/vigie → cli → {config, repo, token, forge, gitlab, github, scan, watch, render} → status
scan  → {config, repo, token, forge, gitlab, github, status}
gitlab, github → {forge, token, status}
forge → status
config → {status, token}
```

`status` still imports no other vigie package. `forge` imports only `status`. Nothing
imports `gitlab` or `github` except `cli` and `scan` (the two constructor switches, research
G1) and their tests.

## status

### Platform (new)

`type Platform string`: the spec entity *Platform*.

| Value | JSON / table text | Project icon (emoji) |
|---|---|---|
| `PlatformGitLab` | `gitlab` | 🦊 |
| `PlatformGitHub` | `github` | 🐙 |

### Project (changed)

| Field | Type | Notes |
|---|---|---|
| `Platform` (new) | `Platform` | set from the endpoint wherever a `Project` is built: `cli.runner.resolve`, `scan.fetchRepo`, `scan.failedRow` |

`Snapshot.Fingerprint` includes `Project.Platform`.

`LastPipeline` is unchanged. On GitHub:
- `ID` is 0;
- `Ref` is the branch;
- `WebURL` is the commit page;
- `UpdatedAt` is the latest `updated_at` of the counted runs.

## forge (new package)

What both platform clients share. Imports `status` only, plus stdlib (`net/http` allowed by
depguard).

### Client (interface)

| Method | Returns | Notes |
|---|---|---|
| `Project(ctx, ref string)` | `(Project, error)` | `ref` is a path (`group/sub/name`, `owner/repo`) or a numeric ID |
| `Source(p Project)` | `status.Source` | the metrics of that project for `status.Collect` and `watch` |

Implemented by `*gitlab.Client` and `*github.Client`.

### Project

The spec entity *Project* as looked up. It was `gitlab.Project`.

| Field | Type | GitLab | GitHub |
|---|---|---|---|
| `ID` | int64 | project ID | repository ID |
| `Path` | string | `path_with_namespace` | `full_name` (after any rename) |
| `WebURL` | string | `web_url` | `html_url` |
| `DefaultBranch` | string | `""` for an empty project | `""` for an empty repository |
| `Archived` | bool | `archived` | `archived` |

### Options

| Field | Type | Used by | Notes |
|---|---|---|---|
| `Insecure` | bool | both | allow an `http://` base URL |
| `ExtraRootsPEM` | []byte | both | `SSL_CERT_FILE` |
| `Retry` | bool | both | 002 S6 bounded retry; scan mode only |
| `OnThrottle` | func() | both | 002 S6 slow-down hook |
| `Revalidate` | bool | github | conditional REST requests with an in-memory ETag cache (research G7); single mode only |

### Errors (moved from `gitlab`, plus one)

| Sentinel | Text | Exit | Scan class | Latches the host |
|---|---|---|---|---|
| `ErrInsecure` | `refusing a plain http API URL` | 2 | error | no |
| `ErrUnauthorized` | `token rejected` | 3 | auth | yes |
| `ErrForbidden` | `denied` | 3 | auth | no |
| `ErrNotFound` | `not found` (was `gitlab.ErrProjectNotFound`) | 4 | not found | no |
| `ErrNetwork` | `cannot reach` | 5 | network | no |
| `ErrRateLimit` (new) | `request allowance exhausted`; always wrapped together with `ErrNetwork` | 5 | network | yes |

### Functions

| Function | Notes |
|---|---|
| `RefreshError(fields []status.FieldResult) error` | moved from `gitlab`: an `ErrUnauthorized` of any field, or the first error when every field failed with `ErrNetwork` |
| `RetryAfter(err error) (time.Duration, bool)` | `errors.As` on `interface{ RetryAfter() time.Duration }`; both clients' status errors implement it. Wired into `watch.Config.RetryAfter` |
| `NewHTTPClient(opts Options) (*http.Client, error)` | transport (proxy, 5 s dial/handshake, TLS ≥ 1.2, extra roots) and redirect policy (same host, no downgrade, ≤ 10), moved from `gitlab` |
| `ParseRetryAfter(v string) time.Duration` | moved from `gitlab` |

## github (new package)

A read-only client for the GitHub REST API (`2022-11-28`) and GraphQL API. Imports `forge`,
`token`, `status`.

### Client

| Field | Notes |
|---|---|
| `rest`, `graphql` | API URLs from the web base (research G2): github.com → `https://api.github.com`, `…/graphql`; Enterprise Server `B` → `B/api/v3`, `B/api/graphql` |
| `host` | web host, for messages (`github.com`) |
| `tok` | `token.Token`, sent as `Authorization: Bearer` |
| `hc` | from `forge.NewHTTPClient` |
| `retry`, `onThrottle` | from `forge.Options` |
| `cache` | revalidation cache, nil unless `Options.Revalidate` |
| `gate` | per-pool block-until times (research G8) |

| Method | Request | Errors |
|---|---|---|
| `NewClient(base *url.URL, tok token.Token, opts forge.Options) (*Client, error)` | – | `forge.ErrInsecure` for `http://` without `Insecure` |
| `Project(ctx, ref)` | `GET /repos/{ref}` or `GET /repositories/{id}` | 404 → `forge.ErrNotFound` (hint: token access, single sign-on); 403 → `forge.ErrForbidden` (hint: single sign-on, or the permission named by `X-Accepted-GitHub-Permissions`); 401 → `forge.ErrUnauthorized`; limits → `ErrRateLimit`/`ErrNetwork` |
| `Source(p forge.Project) status.Source` | – | – |

### revalidation cache

`map[string]entry` keyed by full request URL, guarded by a mutex. `entry` holds `etag string`
and `body []byte`.
- A 200 with an `ETag` stores the entry.
- A 304 decodes the stored body.
- A 304 without an entry, which should not happen, is a malformed-response error.

The cache lives as long as the client, which is one process; nothing is persisted.

### gate

`map[pool]time.Time` guarded by a mutex; pools `core` and `graphql`.

| Event | Effect |
|---|---|
| primary limit (`x-ratelimit-remaining: 0`) | block that pool until `x-ratelimit-reset` + 1 s |
| secondary limit | block both pools until `retry-after`, else 60 s |
| request while blocked | answered locally with the stored error (same `RetryAfter`), no network |

### rateLimitError

Implements `RetryAfter()`. It wraps `forge.ErrRateLimit` and `forge.ErrNetwork` (primary), or
only `forge.ErrNetwork` (secondary).

Message: `<host> request allowance exhausted until 14:05 (in 13m): wait for the reset; it is
shared by all your GitHub tokens`.

### repoSource (implements `status.Source`)

| Method | Request(s) | Result |
|---|---|---|
| `Branches(ctx, since)` | GraphQL `Branches`: `refs(refPrefix: "refs/heads/", first: 100, after: $cursor)`, nodes' `target { ... on Commit { committedDate } }`, ≤ 100 pages | `Total` = `totalCount`, `Active` = count with `committedDate ≥ since`; > 100 pages → both capped |
| `Pipelines(ctx)` | REST `actions/runs?status=…&per_page=1` × `in_progress`, `queued`, `requested`, `pending`, concurrently | `Running` = in_progress; `Pending` = sum of the three; any `total_count ≥ 1000` → field error "more than 1,000 workflow runs …" |
| `LastPipeline(ctx, ref)` | REST `actions/runs?branch=ref&per_page=30`, then `…&head_sha=…&per_page=100` only if the page is all one commit | `combine` (below); `""` ref or no run → `PipelineNone` |
| `OpenMRs(ctx)` | GraphQL `PullRequests`: `pullRequests(states: OPEN, first: 1) { totalCount }` | count; > 10,000 → capped |
| `OpenIssues(ctx)` | GraphQL `Issues`: `hasIssuesEnabled`, `issues(states: OPEN, first: 1) { totalCount }` | `hasIssuesEnabled: false` → `status.ErrUnavailable`; else count; > 10,000 → capped |

Field-level 403 and 404 → `status.ErrUnavailable` with the reason, including the missing
permission when GitHub names it.

### run (decoded REST workflow run) and pure functions

| Field | JSON |
|---|---|
| `ID` | `id` |
| `WorkflowID` | `workflow_id` |
| `HeadSHA` | `head_sha` |
| `Status` | `status` |
| `Conclusion` | `conclusion` (nullable) |
| `UpdatedAt` | `updated_at` |

| Function | Rule |
|---|---|
| `runState(status, conclusion string) status.PipelineStatus` | the mapping table of research G6 / spec FR-014 |
| `combine(runs []run, ref, commitURL string) status.LastPipeline` | runs of the newest run's commit; per `WorkflowID` the highest `ID`; the combined state is the first of `running, pending, manual, failed, canceled, unknown, success, skipped`; `UpdatedAt` is the max; `ID` 0 |

### State transitions of the gate

```text
open ──(primary or secondary limit answer)──▶ blocked(pool, until)
blocked ──(now ≥ until)──▶ open
blocked ──(request)──▶ blocked   (answered locally, nothing sent)
```

## githubtest (new package, tests only)

A fake GitHub in the spirit of `gitlabtest`. REST is under `/api/v3`, GraphQL at
`/api/graphql` (Enterprise Server layout), and GraphQL dispatches on `operationName`.

| Type | Fields |
|---|---|
| `Repo` | `ID`, `FullName`, `DefaultBranch`, `Archived`, `HasIssues`, `Branches []Branch`, `Runs []Run` (newest first), `OpenPRs`, `OpenIssues`, `MovedFrom` (old full name answering 301) |
| `Branch` | `Name`, `CommittedDate` |
| `Run` | `ID`, `WorkflowID`, `HeadBranch`, `HeadSHA`, `Event`, `Status`, `Conclusion`, `UpdatedAt` |
| `Behavior` | per endpoint: `Status`, `Header` (e.g. `X-GitHub-SSO`, `X-Accepted-GitHub-Permissions`, `Retry-After`), `RateLimited` (remaining 0, reset), `Times` |
| `Server` | `URL`, `AddRepo`, `Set(Behavior)`, `Billed(pool) int` (non-304 REST requests; GraphQL queries), `Tokens() []string` (`Authorization` values seen), ETag support |

## config

### fileHost / HostProfile (changed)

| Field | Type | YAML | Validation |
|---|---|---|---|
| `Platform` (new) | `status.Platform` | `platform` | `gitlab` (default when absent) or `github`; any other value → `ErrInvalid` "platform of host X in config file P: use gitlab or github"; `gitlab` on a github.com-family key → `ErrInvalid` |

A profile whose `API` is `https://github.com` has platform GitHub regardless of the key (an SSH
alias).

### Endpoint (changed)

| Field | Type | Notes |
|---|---|---|
| `Platform` (new) | `status.Platform` | research G2 |
| `Token` | `token.Source` | now carries `Vars` per kind of host (research G3) |

`API` stays the **web base URL** for both platforms; `github.NewClient` derives the API
addresses.

### Functions (changed or new)

| Function | Change |
|---|---|
| `hostKey(h)` | maps `www.github.com`, `ssh.github.com`, `api.github.com` to `github.com` |
| `(Settings) KnownHost(h)` | also true for `github.com` |
| `(Settings) OnHost(h, base)` | unchanged rule, now family-aware through `hostKey` |
| `ResolveHost(s, remoteHost)` | sets `Platform` and `Token.Vars`. A github.com-family host without a profile gets `API = https://github.com`. Another `*.github.com` host → `ErrInvalid` "gist.github.com is not a repository host" |
| `CheckProject(ep Endpoint, path string) error` (new) | on GitHub, a non-numeric path must have exactly two segments, else `ErrInvalid` "project path a/b/c on github.com: GitHub repositories are owner/repo" |

## token

### Source (changed)

| Field | Type | Notes |
|---|---|---|
| `Env` | string | unchanged |
| `Command` | []string | unchanged |
| `Vars` (new) | []string | default variables in precedence order; nil means `GitLabVars` |

| Exported value | Content |
|---|---|
| `GitLabVars` | `GITLAB_TOKEN` |
| `GitHubVars` | `GH_TOKEN`, `GITHUB_TOKEN` |
| `EnterpriseVars` | `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN` |

| Function | Change |
|---|---|
| `Resolve(ctx, src, env)` | `Env` > `Command` > first non-empty of `Vars`. Missing: "GH_TOKEN and GITHUB_TOKEN are not set: <hint>", the hint chosen by the first variable (research G3) |
| `(Source) Configured(env) bool` (new) | `Env != ""` or `len(Command) > 0` or any `Vars` value non-empty |
| `Mask(s)` | keeps `glpat-`, `ghp_`, `github_pat_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, then `****` |

## gitlab (changed)

| Change | Detail |
|---|---|
| sentinels | removed; wraps `forge.*` (`ErrProjectNotFound` → `forge.ErrNotFound`) |
| `Project` (type) | removed; `Client.Project` returns `forge.Project` |
| `Source(p forge.Project) status.Source` (new) | `ForProject(p.ID)` |
| `RefreshError`, `RetryAfter`, transport, TLS roots | moved to `forge` |
| `NewClient(base, tok, opts forge.Options)` | takes the shared options |

## scan (changed)

| Item | Change |
|---|---|
| `Select(in, s, env)` | new `env` argument; github.com family and GitHub profiles recognised; unconfigured GitHub token → `ReasonNoToken`; GitHub path not `owner/repo` → `ReasonRemoteURL` |
| `ReasonNoToken` (new) | text `no token`; summary order `bare, unreadable, no remote, remote URL, unknown host, no token, other host` |
| `ErrNoGitLab` → `ErrNothingListed` | text `no GitLab or GitHub repository found`; hint adds "export GH_TOKEN for github.com clones" when `no token` skips exist |
| `hostID` | adds platform and `Vars` |
| `host.connect` | constructor switch on `Endpoint.Platform`; options add `Revalidate: false` |
| `latches` | `forge.ErrUnauthorized`, `forge.ErrRateLimit`, `token.ErrMissing`, `token.ErrCommand` |
| `failures` (Classify) | forge sentinels |

## render (changed)

| Item | Change |
|---|---|
| `platformLooks` (new) | `map[status.Platform]look`: GitLab 🦊, GitHub 🐙, bold; unknown platform falls back to 🗼 |
| `projectSegment` | icon from `platformLooks` |
| `Table` | `PLATFORM` column between `DIR` and `PROJECT`; header bold, cells plain; widths and `fit` index shift; never truncated |
| `jsonRecord.Platform` (new) | `json:"platform"`, first property, always present |

## watch (changed)

| Item | Change |
|---|---|
| `MaxRetryAfter` | 5 min → 1 h (research G8) |

## cli (changed)

| Item | Change |
|---|---|
| `runner.client` | `forge.Client` |
| `newRunner` | `config.CheckProject` on the remote path or `--project`; constructor switch; `Revalidate: true` |
| `runner.resolve` | `status.Project{…, Platform: ep.Platform}`; debug "repository moved to …: update the remote URL" when the looked-up path differs from the remote path |
| `fatalInWatch` | `forge.ErrUnauthorized`, `forge.ErrForbidden`, `forge.ErrNotFound` |
| `watch.Config.RetryAfter` | `forge.RetryAfter` |
| `runScan` | passes `std.Env` to `scan.Select` |
| `help.txt` | research G14 |

## cmd/vigie (changed)

`exitcode.go`: the table names `forge.ErrInsecure`, `ErrUnauthorized`, `ErrForbidden`,
`ErrNotFound`, `ErrNetwork` (which `ErrRateLimit` wraps), and `scan.ErrNothingListed`. The
`AllFailedError` rule is unchanged.
