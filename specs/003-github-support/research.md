# Research: GitHub Support

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-10-21

The decisions of features 001 ([research.md](../001-gitlab-status-line/research.md), R1–R12)
and 002 ([research.md](../002-multi-repo-table/research.md), S1–S14) still hold unless
changed here. GitHub API facts come from docs.github.com and the GitHub CLI manual, as read
on 2026-10-21. Facts that are **observed** on the live API rather than documented are marked;
each one is covered by a test against the fake server and by a manual check in
[quickstart.md](./quickstart.md). No new dependency is needed.

## G1. One contract for both platforms: package `forge`

- **Decision**: a new package `internal/forge` holds what both platform clients share:
  - the `Client` interface (`Project`, `Source`);
  - the `Project` lookup result (ID, path, web URL, default branch, archived);
  - `Options` for client construction;
  - the error sentinels (`ErrInsecure`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`,
    `ErrNetwork`, plus the new `ErrRateLimit`);
  - `RefreshError` (moved from `gitlab`) and `RetryAfter`;
  - the HTTP transport (TLS roots, proxy, dial and handshake timeouts, redirect policy), moved
    from `gitlab/client.go` and `gitlab/tls.go`.

  `gitlab` and the new `github` package both import `forge`.
- **Why**: `cli`, `scan`, `watch` wiring and `cmd/vigie/exitcode.go` today name
  `gitlab.ErrUnauthorized` and its siblings in five places. With shared sentinels, every
  consumer classifies errors once, whatever the platform. Exit codes, scan failure classes,
  the auth latch and watch-mode fatal errors stay single tables.
- **Constructor choice**: `forge` cannot build clients, because `gitlab` and `github` import
  it. The switch on `Endpoint.Platform` (`github.NewClient` or `gitlab.NewClient`) is written
  twice, in `cli.newRunner` and `scan.host.connect`, each about six lines. A third caller
  would move it into a package of its own, the same rule of three the constitution applies
  to generics.
- **Naming**: "forge" is the usual name for a code-hosting service; the user-facing term
  stays "platform" (spec), as in the config key and the JSON property.
- **depguard**: the `http` rule allows `internal/forge/**`, `internal/gitlab/**` and
  `internal/github/**`.
- **Alternatives rejected**:
  - duplicating the sentinels per platform and listing both sets in every table (eleven extra
    rows across five tables, easy to forget one);
  - registration through `init()` (implicit wiring);
  - a single client switching per request on platform (mixes two APIs in one type).

## G2. Recognising GitHub and choosing the platform

- **Decision**: in `config`, a host belongs to the **github.com family** when its name is
  `github.com`, `www.github.com`, `ssh.github.com` or `api.github.com` (FR-001). `hostKey`
  maps all four to `github.com`, so profile keys, `KnownHost`, `OnHost` and `--host` all treat
  them as one instance.
  - **Platform of an endpoint**: GitHub when the host is in the family, or when its profile has
    `api: https://github.com`, or `platform: github` (FR-002). Every other host is GitLab, as
    before.
  - **Other `*.github.com` names** (e.g. `gist.github.com`) are not repository hosts.
    `ResolveHost` refuses them with a setup error (exit 2), so single mode never sends
    `GITLAB_TOKEN` to a GitHub-owned host. Scan mode skips them as `unknown host`.
  - **Config key `platform`**: `gitlab` (default) or `github`. Any other value is a setup error
    (`ErrInvalid`). `platform: gitlab` on a github.com-family key is also a setup error,
    because github.com is always GitHub.
  - **`--host` without a matching profile**: GitHub only for the github.com family. A GitHub
    Enterprise Server instance needs a `hosts` entry, per the no-guessing rule (FR-002).
  - **`GITLAB_HOST`** stays GitLab-only (FR-003).
- **Path rule** (FR-004): `config.CheckProject(ep, path)` rejects a GitHub path that does not
  have exactly two segments (`ErrInvalid`, exit 2). Single mode calls it on the remote path
  and on `--project` once the endpoint is known; scan mode skips such clones as
  `remote URL not understood`. Numeric IDs pass.
- **API addresses** (derived in `github`, from the web base URL held in `Endpoint.API`):

  | Instance | REST | GraphQL |
  |---|---|---|
  | github.com | `https://api.github.com` | `https://api.github.com/graphql` |
  | Enterprise Server at `B` | `B/api/v3` | `B/api/graphql` |

- **GitHub Enterprise Cloud with data residency** (`*.ghe.com`) uses `https://api.SUB.ghe.com`,
  which fits neither row. It is out of scope here and noted as a follow-up.

## G3. Tokens per kind of host (Clarifications Q1)

- **Decision**: `token.Source` gains `Vars []string`, the default variables in precedence
  order. The zero value keeps meaning `GITLAB_TOKEN`, so existing callers and tests are
  unaffected. `token` exports the three conventions; `config.ResolveHost` picks one per
  endpoint:

  | Endpoint | `Vars` |
  |---|---|
  | GitLab | `GITLAB_TOKEN` |
  | github.com | `GH_TOKEN`, `GITHUB_TOKEN` |
  | Enterprise Server | `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN` |

  This is the GitHub CLI convention (`gh help environment`: `GH_TOKEN` over `GITHUB_TOKEN` for
  github.com, `GH_ENTERPRISE_TOKEN` over `GITHUB_ENTERPRISE_TOKEN` for Enterprise Server).
- **Resolution**: the host entry's `token_env` or `token_command` comes first, then the first
  non-empty variable in `Vars`. A token from default variables therefore only reaches hosts of
  that kind (FR-006), because each endpoint's `Vars` is fixed by its kind.
- **Missing token message** (FR-008): it names every variable in `Vars`, with a hint keyed by
  the convention:
  - GitHub: "create a fine-grained token with read access to the repository and export it, or
    set `token_command: ["gh", "auth", "token"]` for this host in the config file";
  - Enterprise Server: the same, plus "or set `token_env`".

  vigie never runs `gh` on its own. `gh auth token` prints the token on stdout and may refresh
  it first, so a `token_command` always yields a fresh one.
- **Scan, no token at all** (FR-025a): `(Source) Configured(env) bool` is true when `Env` or
  `Command` is set (even if the variable named by `Env` is empty), or when any variable in
  `Vars` is non-empty. `scan.Select` skips a GitHub repository whose endpoint is not
  configured with the new reason `no token`, and sends no request for it.
- **Masking** (FR-007): `token.Mask` keeps the known prefixes and hides the rest: `glpat-`,
  `ghp_`, `github_pat_`, `gho_`, `ghu_`, `ghs_`, `ghr_`. Installation tokens now come in a
  longer `ghs_…` format; only the prefix is kept, so length does not matter.
- **Permissions to document** (README, FR-023): a fine-grained token needs read access to
  Metadata, Contents (branches), Actions, Issues and Pull requests. A classic token needs no
  scope for public repositories and `repo` for private ones. GitHub offers no read-only
  private scope for classic tokens, so the README recommends fine-grained tokens.

## G4. Which API serves each field

- **Constraint**: GraphQL cannot count workflow runs by status; `Repository` has no workflow
  runs, and `WorkflowRun` has no status of its own. REST cannot give branch commit dates in
  bulk. Each field therefore uses the API that can answer it in one call:

  | Field / call | API | Request | Cost per call |
  |---|---|---|---|
  | project lookup | REST | `GET /repos/{owner}/{repo}` (or `/repositories/{id}`) | 1, or 0 when revalidated (304) |
  | `branches` | GraphQL | `refs(refPrefix: "refs/heads/", first: 100)` with `committedDate`, paged | 1 point per 100 branches |
  | `pipelines` | REST | `GET …/actions/runs?status=S&per_page=1` for `in_progress`, `queued`, `requested`, `pending` | 4, mostly 304 in watch |
  | `last` | REST | `GET …/actions/runs?branch=B&per_page=30` (G6) | 1, mostly 304 in watch |
  | `mrs` | GraphQL | `pullRequests(states: OPEN, first: 1) { totalCount }` | 1 point |
  | `issues` | GraphQL | `hasIssuesEnabled`, `issues(states: OPEN, first: 1) { totalCount }` | 1 point |

- **Why GraphQL for the two counts**:
  - `issues.totalCount` counts issues only; pull requests are a separate type.
  - REST would need `open_issues_count` minus the pull request count. The docs never say that
    `open_issues_count` includes pull requests, and two reads taken at different moments can
    disagree.
  - The REST alternative for pull requests, counting through `per_page=1` and the `rel="last"`
    link, is not a documented counting method, and the link is "omitted if it can't be
    calculated".
- **`first: 1`**: the docs require `first`/`last` (1–100) on every connection, so the count
  queries ask for one node and read only `totalCount`.
- **`last` via REST, not GraphQL check suites**:
  - Fine-grained tokens have no Checks permission ("Checks API" is a listed fine-grained gap).
  - It is not documented where a `pull_request` run's check suite attaches.
  - The runs endpoint is documented, needs only Actions read, and supports 304s.
  - **Observed**: a `pull_request` run reports the PR's head branch in `head_branch` and the
    PR's head commit in `head_sha`. So filtering by `branch` includes pull request runs, as
    Clarifications Q2 requires.
- **One field, one request** (except paged branches): every field stays independent, so
  `status.Collect` and the `status.Source` interface are unchanged. Coalescing `mrs` and
  `issues` into one GraphQL query would save 720 points an hour but needs a single-flight
  mechanism inside the source; the budget holds without it (G7).
- **API version**: `X-GitHub-Api-Version: 2022-11-28`. It is supported until 2028-03-10 and is
  the only version GitHub Enterprise Server 3.20 and older accept. None of the fields used
  changed in `2026-03-10`. Other headers:
  - `Accept: application/vnd.github+json`;
  - `User-Agent: vigie` (required; GitHub rejects requests without one);
  - `Authorization: Bearer <token>`.

## G5. Counts and caps (FR-010, FR-016)

- **Decision**: `running` is the `total_count` of `status=in_progress`. `pending` is the sum
  for `queued`, `requested` and `pending`. `waiting` and `action_required` are not counted
  (Clarifications Q4). `status` takes a single value; **observed**, a comma list silently
  returns 0. Hence four requests.
- **Cap**: GitHub returns "up to 1,000 results" for filtered run listings, and **observed**
  `total_count` values look rounded far above that. A run count of 1,000 or more is therefore
  never shown as exact: the field gets an error, "more than 1,000 workflow runs: GitHub does
  not report an exact count". It shows `–`, with JSON `null` and the reason (spec amendment,
  G15).
- **GraphQL counts** (`totalCount` of issues and pull requests) have no documented cap. Above
  10,000 they are shown as `10k+` / `"capped": true`, the same rule as GitLab (FR-016).
- **Branches**: the pages are walked up to 100 pages (10,000 branches), as on GitLab; beyond
  that both counts are capped. `refs` cannot be ordered by commit date, so every page is
  needed.

## G6. The `last` field on GitHub (FR-011, FR-014)

- **Decision**: `GET /repos/{o}/{r}/actions/runs?branch=B&per_page=30` lists the branch's
  runs, newest first, from every event (push, pull_request, schedule, manual).
  1. **Commit**: the `head_sha` of the newest run. Re-runs keep their run ID and creation
     time; only `run_attempt` grows. Scheduled and manual runs start on the branch head. So
     the newest run belongs to the latest commit that has runs. If the head commit started no
     run, it is an older commit, which matches the "previous result stays shown" rule.
  2. **Runs counted**: the runs of that commit on the first page. When all 30 belong to it,
     one more request, `…&head_sha=SHA&per_page=100`, fetches the rest.
  3. **Latest per workflow**: for each `workflow_id`, keep the run with the highest ID. A
     re-run is the same run with a later attempt, so it replaces the earlier result by
     construction.
  4. **State of each run**: mapped per the table below (FR-014).
  5. **Combined state**: the first of `running`, `pending`, `manual`, `failed`, `canceled`,
     `unknown`, `success`, `skipped` that any counted run has.
  6. **Time**: the latest `updated_at`.
  7. **Link**: the commit page, `<repository html_url>/commit/<sha>`.
  8. **ID**: zero, so JSON omits `last.id` (the existing `omitempty`).
  9. **No run on the branch**: `none`.
- **Mapping** (documented enums: status `requested, in_progress, completed, queued, pending,
  waiting`; conclusion `action_required, cancelled, failure, neutral, skipped, stale, success,
  timed_out, startup_failure`):

  | `status` | `conclusion` | vigie state |
  |---|---|---|
  | `in_progress` | – | `running` |
  | `queued`, `requested`, `pending` | – | `pending` |
  | `waiting` | – | `manual` |
  | `completed` | `success`, `neutral` | `success` |
  | `completed` | `failure`, `timed_out`, `startup_failure` | `failed` |
  | `completed` | `cancelled`, `stale` | `canceled` |
  | `completed` | `skipped` | `skipped` |
  | `completed` | `action_required` | `manual` |
  | anything else | anything else | `unknown` |

- **Pure functions**: `github.runState(status, conclusion)` and `github.combine(runs)` hold the
  table and the precedence, tested without HTTP.
- **Order assumption** (observed, not documented): the runs list is newest first. A
  quickstart check confirms it on a live repository.

## G7. Request allowance and SC-005

- **Facts**:
  - Authenticated users get 5,000 REST requests and 5,000 GraphQL points per hour, in
    **separate pools**.
  - Both pools are **per user**, shared by every token of that user (PATs, `gh`, OAuth apps).
  - A conditional REST request answered `304 Not Modified` does not count, when sent with the
    `Authorization` header.
  - GraphQL has no conditional requests; a query costs at least 1 point.
  - Enterprise Server has rate limits off by default, so the headers may be absent.
- **Decision**: the GitHub client keeps an in-memory **revalidation cache** for REST GETs.
  - It is keyed by URL and stores the `ETag` and the body.
  - It sends `If-None-Match` and decodes the cached body on a 304.
  - It is enabled with `forge.Options.Revalidate`: on in single mode (watch benefits; a
    one-shot run sends each URL once anyway), off in scan mode, where every URL is fetched
    once.
  - Nothing is written to disk (001 FR-012).
- **Budget at the default 5 s interval** (720 refreshes per hour, repository with ≤ 100
  branches):

  | Pool | Calls per hour | Billed |
  |---|---|---|
  | GraphQL | `mrs` 720 + `issues` 720 + `branches` 60 (60 s cache, 001 FR-018) | ≈ 1,500 points |
  | REST | lookup 12, `pipelines` 2,880, `last` 720 | only responses that changed: a few per pushed commit and per run state change; ≪ 2,500 |

  Both stay under 2,500, half of each pool (SC-005). SC-005 is restated per pool and for
  ≤ 100 branches (G15). A repository with more branches costs one more point per 100
  branches per minute.
- **One-shot cost**: without revalidation, one run costs 7 REST requests and 3 GraphQL
  points. A status bar that polls one-shot every 5 s would use 5,040 REST requests an hour, so
  the README tells GitHub users to run `vigie -w -o json` instead, which streams a record on
  each change.
- **Alternatives rejected**:
  - coalescing `mrs` and `issues` into one query: saves 720 points, adds a single-flight
    mechanism; worth it only if the budget tightens;
  - `/rate_limit` polling: an extra request;
  - a disk cache: forbidden by 001 FR-012.

## G8. Throttling and exhausted allowance (FR-018)

- **Classification** of a REST or GraphQL answer, in this order:

  | Answer | Meaning | Error |
  |---|---|---|
  | 403 or 429 with `x-ratelimit-remaining: 0`, or GraphQL 200 with `errors` and `x-ratelimit-remaining: 0` | primary allowance exhausted | `ErrRateLimit` (wraps `ErrNetwork`), retry after `x-ratelimit-reset` |
  | 403 or 429 with `retry-after`, or a body mentioning a secondary rate limit | secondary limit | `ErrNetwork`, retry after `retry-after`, else 60 s (GitHub: "wait for at least one minute") |
  | 429, 5xx | throttled or server error | `ErrNetwork` (as GitLab) |

- **Gate**: after either kind of limit, the client blocks its own requests to that pool
  (`core` or `graphql`; a secondary limit blocks both) until the reset time or retry delay.
  It answers locally with the same error and sends nothing. GitHub warns that requesting while
  limited "may result in the banning of your integration". This also covers fields served by
  the other, still open pool.
- **One-shot**: exit 5. The message names the reset time: "github.com request allowance
  exhausted until 14:05 (in 13m): wait for the reset; it is shared by all your GitHub tokens".
- **Watch**: the failed fields carry `RetryAfter`. When every field failed, the loop marks the
  line stale and waits until the reset (FR-029, 001). Two changes:
  - `watch.MaxRetryAfter` goes from 5 min to 1 h, because GitHub's reset can be up to an hour
    away and FR-018 forbids requesting before it.
  - The reset wait adds 1 s of margin, for clock skew.

  When only some fields fail (one pool exhausted), those fields show `–` and the gate keeps
  them from requesting until the reset.
- **Scan**: the 002 rules apply (bounded retry within `--timeout`, slots halved). In addition,
  an exhausted primary allowance **latches** the host like a rejected token: its remaining
  rows fail with the `network` marker without any request, and stderr gets one line for the
  host, even with `--quiet` (spec amendment, G15). A secondary limit does not latch.

## G9. Errors and exit codes (FR-019, FR-020)

| GitHub answer | Where | vigie error | Exit (one-shot) |
|---|---|---|---|
| 401 (`Bad credentials`) | any | `ErrUnauthorized`, latches in scan | 3 |
| 403 with `X-GitHub-SSO` | lookup | `ErrForbidden`: "authorize the token for the organization's SAML single sign-on" | 3 |
| 403 "Resource not accessible …" / `X-Accepted-GitHub-Permissions` | lookup | `ErrForbidden`, naming the missing permission | 3 |
| same | a field | `status.ErrUnavailable`, field `–`, naming the missing permission | 0 |
| 404 | lookup | `ErrNotFound`: "repository o/r not found on github.com: check that the token can see it (for a fine-grained token, add the repository to it)" | 4 |
| 404 | a field | `status.ErrUnavailable` (feature disabled) | 0 |
| GraphQL `data.repository: null` with an error | lookup by GraphQL (not used) / a field | field unavailable | 0 |
| rate limits | see G8 | `ErrRateLimit` / `ErrNetwork` | 5 |
| 301 on lookup | renamed or transferred | followed; same host, so the redirect policy allows it | – |

- **SSO answering 404**: GitHub may answer 404 instead of 403 for SAML SSO, so the not-found
  hint also mentions single sign-on.
- **Renames** (FR-020): REST answers 301 and Go follows it (same host). GraphQL
  `repository(owner, name)` follows renames by default (`followRenames: true`). The lookup
  returns the new `full_name`. When it differs from the remote path, ignoring case, `cli` logs
  at debug level "repository moved to owner/new: update the remote URL". The 5-minute
  re-resolve of watch mode picks up a rename that happens mid-session.
- **Actions disabled**: undocumented. GitHub lists 200 as the only answer of the runs
  endpoint. The `enabled` flag (`GET …/actions/permissions`) needs Administration read, which
  a read-only token lacks. So `pipelines` and `last` show `–` only when GitHub refuses the
  listing (403/404). Otherwise they show what GitHub returns, typically `0r/0p` and `∅`
  (spec amendment to US1-6 and FR-015, G15).
- **Repeated 401s**: GitHub then answers 403 for every attempt for a while. The scan latch
  stops at the first 401, and watch mode stops at once (001 FR-030).

## G10. Transport and client structure

- **Decision**: `forge.NewHTTPClient(opts)` builds the `*http.Client` both platforms use:
  - proxy from the environment;
  - 5 s dial and TLS handshake timeouts;
  - TLS 1.2 minimum, plus `SSL_CERT_FILE` roots;
  - a redirect policy that refuses a host change or an https→http downgrade.

  The redirect message no longer names GitLab.
- **`github.Client`**:
  - `get(ctx, pool, path, query, out)` for REST, with revalidation (G7), the gate and
    classification (G8, G9), and the 002 bounded retry when `Options.Retry` is set (scan);
  - `query(ctx, op, vars, out)` for GraphQL: `POST` with `operationName`, decoding `data` and
    `errors`;
  - errors carry `RetryAfter()`, read by `forge.RetryAfter` for watch mode.
- **`gitlab.Client`**: unchanged behaviour. It uses the shared transport and sentinels, and
  `Project` returns a `forge.Project`. `ForProject(id)` stays; `Source(p)` calls it.
- **Body limit**: 16 MiB for each response (unchanged).

## G11. Output: platform icon, table column, JSON property (Clarifications Q5, FR-021–FR-022)

- **Line**: on a terminal with emoji, the project segment starts with 🦊 (GitLab) or 🐙
  (GitHub) instead of 🗼. Both are two cells wide, like 🗼, so width math is unchanged. Without
  emoji, the project segment has no label, as before.
- **Table**: a `PLATFORM` column between `DIR` and `PROJECT`, holding `gitlab` or `github` in
  every mode, including failed rows (whose platform comes from the endpoint).
  - Bold header like the others; plain cells.
  - Never truncated; `fit` still cuts `PROJECT`, then `DIR`.
- **JSON**: a required `platform` property in every record, single and scan mode
  ([contracts/status.schema.json](./contracts/status.schema.json)).
  - `$id` keeps `v1`, as 002 did for `dir`.
  - The schema also documents the GitHub meaning of `project`, `last.id` (absent) and
    `last.web_url` (the commit).
- **Domain**: `status.Platform` (`gitlab`, `github`) and `status.Project.Platform`. Every site
  that builds a `status.Project` sets it from the endpoint. The fingerprint includes it.
- **Version**: the table column and the required JSON property break text parsers and
  strict validators of the old schema. The release carrying this feature is therefore
  **v0.3.0** (pre-1.0, so the minor version marks the break), and the commit message lists the
  contract changes (FR-027).

## G12. Folder scan changes (FR-024, FR-025, FR-025a)

- **`Select(in, s, env)`**:
  - recognition gains the github.com family and GitHub profiles;
  - a GitHub repository whose token source is not configured (G3) is skipped as `no token`,
    with or without `--host` (spec FR-025a);
  - a GitHub path that is not `owner/repo` is skipped as `remote URL not understood`.
- **Skip reasons**, in summary order: `bare`, `unreadable`, `no remote "<name>"`,
  `remote URL not understood`, `unknown host`, `no token`, `other host`.
- **Nothing listed**: the error becomes "no GitLab or GitHub repository found: …".
  - When `no token` skips exist, the hint adds "export GH_TOKEN for github.com clones".
  - The sentinel is renamed `ErrNothingListed` (exit 2, unchanged).
- **Hosts**: `hostID` adds the platform and `Vars`, so github.com and an Enterprise Server
  never share a host entry, token or slots.
- **Latches**: `forge.ErrUnauthorized`, `token.ErrMissing`, `token.ErrCommand`, and now
  `forge.ErrRateLimit` (G8).
- **Classify**: forge sentinels only. `ErrRateLimit` is `network`.

## G13. Testing strategy (Principle III)

| Package | What | How |
|---|---|---|
| `forge` | `RefreshError`, `RetryAfter`, redirect policy, TLS roots | moved tests from `gitlab`, black box |
| `github` | lookup (path, ID, 301 rename, 404, SSO, 401); each field; caps; run mapping and combination; gate; revalidation (304 decodes the cached body and is not billed); GraphQL errors | `githubtest` fake server; pure-function table tests for `runState`/`combine` |
| `github` | SC-005: one simulated hour of watch at 5 s, static repository and a repository with runs changing every 2 min; billed REST ≤ 2,500 and GraphQL ≤ 2,500 | `testing/synctest` with `watch.Run` and `githubtest` counters |
| `config` | family hosts, `platform` key (values, errors), `CheckProject`, `gist.github.com` refused, `Vars` per endpoint, `KnownHost`/`OnHost` with `ssh.github.com` | table tests |
| `token` | `Vars` precedence, missing-token hints, `Configured`, GitHub prefixes in `Mask` | table tests |
| `scan` | `no token` skip; mixed hosts; rate-limit latch; GitHub row through `Fetch` | `gitlabtest` + `githubtest` |
| `render` | 🦊/🐙 goldens, `PLATFORM` column goldens, JSON `platform`, schema keys | `-update` goldens |
| `cmd/vigie` | e2e below | built binary |

**`githubtest`** serves REST under `/api/v3` and GraphQL at `/api/graphql`, the Enterprise
Server layout. It dispatches GraphQL on `operationName`; `github` names every query. It
supports:
- `ETag` and 304 answers;
- per-pool billing counters;
- rate-limit, secondary-limit, SSO, 301 and permission behaviours;
- a request log of the `Authorization` header values.

The github.com address derivation (`api.github.com`) is checked through `export_test.go`.

**End-to-end** (a GitHub Enterprise Server entry pointing at `githubtest`, so the binary runs
unmodified):
- a GitHub clone gives the 🐙-free plain line (non-TTY) and the JSON record with
  `"platform":"github"`;
- a mixed folder (`gitlabtest` + `githubtest`) gives the golden table with `PLATFORM`;
- **token separation** (SC-004): `GITLAB_TOKEN`, `GH_TOKEN` and `GH_ENTERPRISE_TOKEN` hold
  distinct values; each server's log may hold only its own, and the Enterprise Server never
  sees `GH_TOKEN`;
- **no token, github.com clone**:
  - single mode exits 3, and the recording `HTTPS_PROXY` (002 S12) sees zero connections;
  - scan mode skips it as `no token`, with zero connections;
- **exhausted allowance**: one-shot exits 5, naming the reset; a scan latches the host and
  prints one stderr line;
- **a GitLab-only folder**: identical to 002 apart from the 🦊 icon (terminal only), the
  `PLATFORM` column and the `platform` property (SC-008).

## G14. Help, README, docs

- `--help`:
  - the first line names GitLab or GitHub;
  - the `--host` line: "GitLab or GitHub base URL";
  - "Folder of clones" lists github.com;
  - Precedence gets a token line per kind of host;
  - Environment lists the four GitHub variables;
  - the config sample shows `platform: github`.
- README:
  - an intro with both platforms;
  - the field mapping table (spec);
  - GitHub quick start with a fine-grained token and its permissions;
  - the `gh auth token` recipe;
  - the status-bar advice (G7);
  - the `PLATFORM` column and 🦊/🐙 in the examples;
  - the v0.3.0 contract changes.
- `docs/status.schema.json` is copied from `contracts/` (gate in the quickstart).

## G15. Spec amendments made while planning

Recorded in [spec.md](./spec.md):

1. **US1-6, FR-015**: Actions disabled shows `–` only when GitHub refuses the run listing; a
   read-only token cannot read the disabled flag, so otherwise the fields show what GitHub
   returns (G9).
2. **FR-016**: a workflow-run count of 1,000 or more is shown `–` with a reason, because GitHub
   caps filtered run listings at 1,000 (G5).
3. **FR-011**: "the latest commit that has any run" is the commit of the newest run on the
   branch (G6).
4. **FR-018**: in a scan, an exhausted allowance latches the host: its rows show `network`
   without further requests, and stderr gets one line for the host (G8).
5. **SC-005**: measured per allowance pool (REST requests, GraphQL points), each ≤ 2,500, on a
   repository with up to 100 branches; the allowance is per user, shared by all of the user's
   tokens (G7).
6. **Assumptions**: GitHub Enterprise Cloud with data residency (`*.ghe.com`) is out of scope
   (G2). The watch-mode retry-delay cap rises from 5 min to 1 h for every platform (G8).

## G16. Lookup deadline (from feature 002)

002 S14 noted that single mode's project lookup has no deadline beyond dial and handshake.
GitHub lookups share that path.

- **Decision**: `cli.runner.resolve` wraps the lookup in `context.WithTimeout(ctx, s.Timeout)`
  on both platforms; the 5-minute re-resolve of watch mode gets the same bound. A lookup
  that runs out of time is `forge.ErrNetwork` (exit 5, "raise --timeout").
- **Why**: without it, GitHub lookups would break constitution Principle VI ("All I/O MUST
  have an explicit timeout"). Found by `/speckit-analyze` (C1); spec FR-017 amended.
- **GitLab effect**: a stalled GitLab lookup now ends with exit 5 after `--timeout` instead
  of hanging until interrupted.
