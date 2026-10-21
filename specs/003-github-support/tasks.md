---

description: "Task list for vigie — GitHub Support"
---

# Tasks: vigie — GitHub Support

**Input**: Design documents from `specs/003-github-support/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/cli.md](./contracts/cli.md),
[contracts/status.schema.json](./contracts/status.schema.json), [quickstart.md](./quickstart.md)

**Tests**: MANDATORY (constitution Principle III, TDD). Each test task is written first and
must **fail** before its implementation task starts; a compile failure counts. The two rules
of feature 002 still apply:

- **Pairs per package**: all `_test.go` files of a package compile into one test binary. So
  within a package, a test task is followed by the implementation that makes it pass, and the
  next test of that package starts only after that. Pairs in different packages run in
  parallel.
- **E2E first**: each story's e2e task (built binary, plus its `--help` assertions) opens the
  phase and fails. The `cli` wiring at the end of the phase makes it pass.

All tests are black box (`package <pkg>_test`). Internals go through `export_test.go` only
where data-model allows it:
- `github`: API addresses, gate clock, `runState`/`combine`, retry base, and an in-memory
  round tripper (T053);
- `gitlab`: retry base (existing);
- `scan`: slots (existing).

`task test` = `go test -count=2 -race ./...`.

**Refactor tasks** (Phase 2, forge extraction) change no behaviour. Their "failing test" is
the existing test renamed to the new API, which does not compile until the move is done.
Every 001 and 002 test must stay green afterwards.

**Organization**: grouped by user story (US1–US5 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task).
- **[Story]**: the user story the task belongs to (US1…US5).
- Citations: research decisions are G1–G16 ([research.md](./research.md)); feature 001 and
  002 decisions are R1–R12 and S1–S14; requirements are FR-xxx and SC-xxx
  ([spec.md](./spec.md)).

## Path Conventions

Go CLI layout from plan.md, module `github.com/sgaunet/vigie`:
- `cmd/vigie/`: main, exit codes, e2e tests;
- `internal/<domain>/`: logic, black-box tests and `testdata/`.

Two new domain packages:
- `internal/forge`: the shared platform contract. It imports `status` only.
- `internal/github`: the GitHub client. It imports `forge`, `token` and `status`, and has the
  test-only fake server `internal/github/githubtest`.

Only `cli` and `scan` import `gitlab` or `github`.

**E2E harness for GitHub** (research G13): the binary cannot be pointed at api.github.com, so
every e2e test that needs answers uses a GitHub Enterprise Server entry pointing at
`githubtest`:

```yaml
hosts:
  ghe.test:
    api: <githubtest URL>
    platform: github
    insecure: true
    token_env: E2E_GH_TOKEN
```

The clones use the origin `https://ghe.test/<owner>/<repo>.git`. github.com clones are used
only where no request may be sent at all, checked with the 002 `recordingProxy`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: a clean baseline and the two empty packages, with depguard allowing HTTP in them.

- [X] T001 Run `task lint` and `task test` (`Taskfile.yml`) on the current tree and confirm both pass, so every later failure is attributable to this feature
- [X] T002 Create the two package comments:
  - `internal/forge/doc.go`: `// Package forge holds what the GitLab and GitHub clients share: the client interface, the looked-up project, client options, the error sentinels every caller classifies with, refresh classification and the HTTP transport.`
  - `internal/github/doc.go`: `// Package github is a read-only client for the GitHub REST and GraphQL APIs, serving the five vigie fields for one repository.`

  In `.golangci.yml`, update the depguard `http` rule: add `"!**/internal/forge/**"` and `"!**/internal/github/**"` next to the gitlab exclusion, and change `desc` to `HTTP lives in internal/forge, internal/gitlab and internal/github`. Confirm `go build ./...` and `task lint` pass.

**Checkpoint**: the module builds with two empty packages.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared platform contract (`forge`), with `gitlab` and every consumer moved
onto it without behaviour change. Also `status.Platform`, token variables per kind of host,
and platform-aware host resolution in `config`.

Platform resolution and the Enterprise token variables are foundational, not US5: the e2e
harness of every story runs on an Enterprise Server entry, and SC-004 (no token crosses
kinds of host) must hold from the first GitHub request.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

Order:
1. forge (T003–T006), then gitlab (T007–T008), then the consumers (T009–T012);
2. in parallel with step 1: status (T013–T014) and token (T015–T016);
3. config (T017–T020) once T014 and T016 are done.

### forge: the shared contract (test → implementation pairs, in order)

- [X] T003 [P] Test sentinels, `RefreshError` and `RetryAfter` in new `internal/forge/refresh_test.go`, moving the cases of `internal/gitlab/refresh_test.go` and renaming `gitlab.ErrUnauthorized`/`gitlab.ErrNetwork` to `forge.ErrUnauthorized`/`forge.ErrNetwork`.
  - **Sentinel texts**: exactly `refusing a plain http API URL` (`ErrInsecure`), `token rejected` (`ErrUnauthorized`), `denied` (`ErrForbidden`), `not found` (`ErrNotFound`), `cannot reach` (`ErrNetwork`) and `request allowance exhausted` (`ErrRateLimit`).
  - **RefreshError**: every field failing with `fmt.Errorf("%w x: %w", forge.ErrRateLimit, forge.ErrNetwork)` counts as all-network and returns the first such error.
  - **RetryAfter**:
    - a test type with a `RetryAfter() time.Duration` method returning 3 s, wrapped twice with `%w`, gives `(3s, true)`;
    - the same type returning 0 gives `(0, false)`;
    - `errors.New("x")` and `nil` give `(0, false)`.
- [X] T004 Implement the six sentinels in new `internal/forge/errors.go` (doc comment on `ErrRateLimit`: always wrapped together with `ErrNetwork`). Implement `RefreshError(fields []status.FieldResult) error` (moved from `internal/gitlab/refresh.go`, same rule) and `RetryAfter(err error) (time.Duration, bool)` in new `internal/forge/refresh.go`; `RetryAfter` uses `errors.As` on `interface{ RetryAfter() time.Duration }` and needs a value > 0. Leave `gitlab` untouched (T008 removes its copies). Makes T003 pass.
- [X] T005 Test the shared transport in new `internal/forge/http_test.go` and new `internal/forge/tls_test.go`, all against `httptest` servers:
  - **Redirects** (moved from `TestRedirectsAreNotFollowedAcrossHosts` in `internal/gitlab/project_test.go`, now calling `forge.NewHTTPClient(forge.Options{Insecure: true})` directly):
    - a same-host redirect is followed;
    - a redirect to another host is refused;
    - an `https` → `http` redirect is refused;
    - an 11th redirect is refused;
    - no message contains `GitLab`.
  - **`forge.ParseRetryAfter`**: `"7"` → 7 s; an HTTP date 30 s ahead → about 30 s; `""`, `"-3"` and `"soon"` → 0.
  - **TLS**: `TestExtraRootsPEM`, moved from `internal/gitlab/tls_test.go`. A TLS server trusted only through `Options.ExtraRootsPEM` is reached; invalid PEM makes `NewHTTPClient` return an error. The transport's `TLSClientConfig.MinVersion` is `tls.VersionTLS12`.
- [X] T006 Implement, moving the code out of `internal/gitlab/client.go` and `internal/gitlab/tls.go` (plan G10). Makes T005 pass.
  - **New `internal/forge/client.go`**:
    - `type Client interface { Project(ctx context.Context, ref string) (Project, error); Source(p Project) status.Source }`;
    - `type Project struct { ID int64; Path, WebURL, DefaultBranch string; Archived bool }`;
    - `type Options struct { Insecure bool; ExtraRootsPEM []byte; Retry bool; OnThrottle func(); Revalidate bool }`, with the field comments of data-model "forge";
    - every field and type gets a doc comment.
  - **New `internal/forge/http.go`**: `NewHTTPClient(opts Options) (*http.Client, error)` (proxy from the environment, 5 s dial and TLS handshake, TLS ≥ 1.2, extra roots), the redirect policy (same host, no downgrade, ≤ 10; messages name the host only) and `ParseRetryAfter(v string) time.Duration`.
  - **New `internal/forge/tls.go`**: `addRoots`.

### gitlab: onto forge (refactor pair)

- [X] T007 Update the gitlab tests to the shared contract.
  - In `internal/gitlab/project_test.go`, `internal/gitlab/metrics_test.go` and `internal/gitlab/retry_test.go`, rename `gitlab.ErrInsecure`, `ErrUnauthorized`, `ErrForbidden` and `ErrNetwork` to their `forge.` names, `gitlab.ErrProjectNotFound` to `forge.ErrNotFound`, `gitlab.Options{…}` to `forge.Options{…}`, and `gitlab.Project` to `forge.Project`.
  - Delete `internal/gitlab/refresh_test.go` and `internal/gitlab/tls_test.go` (moved by T003 and T005).
  - In `TestRetryAfter`, assert `forge.RetryAfter(err) == 7*time.Second` for a 429 answered with `Retry-After: 7`.
  - Keep one cross-host redirect case through `gitlab.NewClient` as an integration check.
  - Add `TestSource`: for a project with `ID == 1`, `client.Source(p).OpenMRs(ctx)` hits `/api/v4/projects/1/merge_requests` (`gitlabtest` `Requests()`).
  - Add `var _ forge.Client = (*gitlab.Client)(nil)` in `internal/gitlab/project_test.go`.
- [X] T008 Move `gitlab` onto `forge`. Makes T007 pass; every 001 and 002 test stays green.
  - **Deleted**: `internal/gitlab/errors.go`, `internal/gitlab/refresh.go` and `internal/gitlab/tls.go`.
  - **`internal/gitlab/client.go`**:
    - `NewClient(base *url.URL, tok token.Token, opts forge.Options)` builds `hc` with `forge.NewHTTPClient(opts)` and still returns `forge.ErrInsecure` for `http://` without `Insecure`;
    - every sentinel becomes the `forge.` one;
    - `statusError` gains `func (e *statusError) RetryAfter() time.Duration`;
    - `RetryAfter`, `parseRetryAfter` and `checkRedirect` are removed, and `forge.ParseRetryAfter` is used instead.
  - **`internal/gitlab/project.go`**: the `Project` type is removed and `Client.Project` returns `forge.Project`.
  - **`internal/gitlab/metrics.go`**: `metricError` uses the forge sentinels. Add `func (c *Client) Source(p forge.Project) status.Source { return c.ForProject(p.ID) }`.

### consumers: one classification for both platforms (refactor pairs)

- [X] T009 [P] Update the scan tests to the forge sentinels (after T008).
  - In `internal/scan/classify_test.go`:
    - `forge.ErrUnauthorized` and `forge.ErrForbidden` → `FailureAuth`;
    - `forge.ErrNotFound` → `FailureNotFound`;
    - `forge.ErrNetwork` → `FailureNetwork`;
    - `forge.ErrInsecure` → `FailureError`;
    - an error wrapping both `forge.ErrRateLimit` and `forge.ErrNetwork` → `FailureNetwork`.
  - Rename every `gitlab.` sentinel in `internal/scan/fetch_test.go` to its `forge.` name.
- [X] T010 Update `internal/scan/fetch.go`. Makes T009 pass.
  - `host.client` and the result of `host.connect` become `forge.Client`, still built with `gitlab.NewClient(ep.API, tok, forge.Options{Insecure, ExtraRootsPEM, Retry: true, OnThrottle: h.throttled})`.
  - `fetchRepo` collects through `client.Source(p)` and uses `forge.RefreshError`.
  - `latches` and the `failures` table use the forge sentinels.
- [X] T011 [P] Update `TestExitCode` in `cmd/vigie/exitcode_test.go` (after T008). Each case is wrapped twice with `%w`:
  - `forge.ErrInsecure` → 2;
  - `forge.ErrUnauthorized` and `forge.ErrForbidden` → 3;
  - `forge.ErrNotFound` → 4;
  - `forge.ErrNetwork` → 5;
  - `fmt.Errorf("%w x: %w", forge.ErrRateLimit, forge.ErrNetwork)` → 5.
- [X] T012 Move the CLI wiring onto `forge`. Makes T011 pass; `task test` is green with every 001 and 002 e2e test unchanged.
  - **`cmd/vigie/exitcode.go`**: the `exitCodes` table names `forge.ErrInsecure`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound` and `ErrNetwork` instead of the `gitlab.` ones.
  - **`internal/cli/run.go`**:
    - `runner.client` becomes `forge.Client`, built with `gitlab.NewClient(ep.API, tok, forge.Options{Insecure: ep.Insecure, ExtraRootsPEM: ep.ExtraRootsPEM})`;
    - `resolve` uses `r.client.Source(p)`;
    - `once` uses `forge.RefreshError(fields)`;
    - `watch.Config.RetryAfter` becomes `forge.RetryAfter`;
    - `fatalInWatch` checks `forge.ErrUnauthorized`, `forge.ErrForbidden` and `forge.ErrNotFound`.
  - **`internal/status/collect.go`**: the `Source` doc comment says the gitlab and github clients implement it.

### status: the platform (test → implementation)

- [X] T013 [P] Test the platform type.
  - **New `internal/status/platform_test.go`**: `string(status.PlatformGitLab) == "gitlab"` and `string(status.PlatformGitHub) == "github"`.
  - **`internal/status/fingerprint_test.go`**: two snapshots that differ only in `Project.Platform` have different fingerprints.
- [X] T014 Implement the platform type. Makes T013 pass; `status` still imports no vigie package.
  - **New `internal/status/platform.go`**: `type Platform string` with the constants `PlatformGitLab = "gitlab"` and `PlatformGitHub = "github"`, and doc comments from data-model "status".
  - **`internal/status/snapshot.go`**: `Project` gains `Platform Platform`.
  - **`internal/status/fingerprint.go`**: the fingerprint includes `Project.Platform`.

### token: default variables per kind of host (test → implementation)

- [X] T015 [P] Test variables, hints and masking in `internal/token/resolve_test.go` and `internal/token/token_test.go` (research G3, FR-005–FR-008).
  - **Conventions**:
    - `token.GitLabVars` equals `[]string{"GITLAB_TOKEN"}`;
    - `token.GitHubVars` equals `{"GH_TOKEN", "GITHUB_TOKEN"}`;
    - `token.EnterpriseVars` equals `{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}`.
  - **Precedence with `Source{Vars: token.GitHubVars}`**:
    - both variables set → the `GH_TOKEN` value;
    - only `GITHUB_TOKEN` set → its value;
    - `GH_TOKEN` of spaces only counts as unset;
    - only `GITLAB_TOKEN` set → an error for which `errors.Is(err, token.ErrMissing)` holds.
  - **Missing-token texts** (contracts/cli.md "Tokens"), matched exactly:
    - `GH_TOKEN and GITHUB_TOKEN are not set: create a fine-grained token with read access to the repository and export it, or set token_command: ["gh", "auth", "token"] for this host in the config file`;
    - with `Source{Vars: token.EnterpriseVars, Host: "github.example.com"}`: `GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not set: create a token with read access to the repository and export it, or set token_env or token_command for github.example.com in the config file`;
    - `Source{}` and `Source{Vars: token.GitLabVars}`: still byte-identical to today, `GITLAB_TOKEN is not set: create a token with the read_api scope and export it`;
    - `Source{Env: "WORK_GH", Vars: token.GitHubVars}` with `WORK_GH` unset: `WORK_GH is not set: create a fine-grained token with read access to the repository and export it` (the hint follows the convention of `Vars`).
  - **Order**: `Env` beats `Command`, which beats `Vars` (unchanged).
  - **Mask**:
    - `ghp_abc123` → `ghp_****`;
    - `github_pat_11AB_xyz` → `github_pat_****`;
    - `gho_x`, `ghu_x`, `ghs_` followed by 80 characters, and `ghr_x` → their prefix + `****`;
    - `glpat-x` → `glpat-****`;
    - `gh_x` and `abc` → `****`.
  - **No leaks**: `fmt.Sprintf("%v %s %#v", tok, tok, tok)` and a `slog` JSON line of a `ghp_` token never contain its value.
- [X] T016 Implement variables and hints. Makes T015 pass; `Configured` comes in US2 (T037).
  - **`internal/token/token.go`**:
    - `Source` gains `Vars []string` (default variables in precedence order; nil means `GitLabVars`) and `Host string` (the host named in the Enterprise Server hint, set by `config.ResolveHost`);
    - exported `var GitLabVars, GitHubVars, EnterpriseVars`;
    - `Mask` keeps the first matching prefix of `glpat-`, `github_pat_`, `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, then appends `****`.
  - **`internal/token/resolve.go`**:
    - `Resolve` takes `Env`, then `Command`, then the first non-empty `Vars` entry after trimming spaces;
    - the missing error names every variable joined with ` and `, uses `is` for one variable and `are` for several, and wraps `ErrMissing` through a small unexported error type whose `Unwrap` returns `ErrMissing`, so the plural text needs no change to `ErrMissing`;
    - the hint is chosen by the first entry of `Vars`: `GITLAB_TOKEN` keeps the read_api hint, `GH_TOKEN` gets the GitHub hint, and `GH_ENTERPRISE_TOKEN` gets the Enterprise Server hint naming `Host`.

  `Source.Host` is an addition to data-model "token": the contract's Enterprise message names the host.

### config: platform-aware hosts (test → implementation pairs, after T014 and T016)

- [X] T017 Test the `platform` key in `internal/config/file_test.go` (FR-002, data-model "config").
  - **New `internal/config/testdata/platform.yaml`**, whose entries give these profiles:

    | Entry | Profile |
    |---|---|
    | `github.example.com: {platform: github, token_env: GHE_TOKEN}` | `Platform == status.PlatformGitHub`, `Token.Env == "GHE_TOKEN"` |
    | `github-work: {api: https://github.com, token_env: WORK_GH_TOKEN}` | `Platform == status.PlatformGitHub`: an `api` of `https://github.com` means github.com |
    | `gitlab.example.com: {platform: gitlab}` | GitLab |
    | `old.example.com: {token_env: OLD}` | GitLab (no `platform` means GitLab) |

  - **Errors** (each wraps `config.ErrInvalid`):
    - new `internal/config/testdata/bad_platform.yaml` (`git.corp: {platform: gitea}`) → message exactly `platform of host git.corp in config file <path>: use gitlab or github`;
    - new `internal/config/testdata/platform_github_com.yaml` (`github.com: {platform: gitlab}`) → `platform of host github.com in config file <path>: github.com is always github`;
    - the same for `www.github.com: {platform: gitlab}` and for `alias: {api: https://github.com, platform: gitlab}`;
    - new `internal/config/testdata/dup_host.yaml` with entries `github.com:` and `ssh.github.com:` → exactly `hosts github.com and ssh.github.com in config file <path> name the same host: keep one` (names sorted); the same for keys `GitLab.example.com:` and `gitlab.example.com:`.
  - **Existing configs**: every profile of the existing `internal/config/testdata/full.yaml` is GitLab (US5-4, FR-026).
- [X] T018 Implement the `platform` key in `internal/config/file.go`. Makes T017 pass.
  - `fileHost` gains ``Platform string `yaml:"platform"` ``, and `HostProfile` gains `Platform status.Platform`.
  - `fileHost.profile(key, p)` validates the value: absent → GitLab; `gitlab` or `github`; any other value → the first error of T017. It sets GitHub for `platform: github`, for a github.com-family key and for an `api` equal to `https://github.com` (scheme and host, no path, case-insensitive). `platform: gitlab` on either of the last two → the second error of T017.
  - `hostKey` maps `www.github.com`, `ssh.github.com` and `api.github.com` (any case, port stripped) to `github.com`.
  - `applyFile` rejects two keys with the same `hostKey`, with the last error of T017; no entry is ever picked by map order.
- [X] T019 Test host resolution in `internal/config/host_test.go` (after T018; research G2 and G3).
  - **First, move the 002 unknown-host fixtures off github.com**, which T020 makes a known host. Each of these tests uses github.com as "a host vigie does not know"; give them `bitbucket.org` instead, so their expectations stay as they are:
    - in `cmd/vigie/scan_test.go`, the `mainFolder` clone `github` becomes `bitbucket` with origin `https://bitbucket.org/git/git.git`; the `-v` check of `TestE2EScanTable` changes from `dir=github` to `dir=bitbucket`; the `a` clone of `TestE2EScanUnknownHostsOnly` gets the same origin. The `1 unknown host` expectations of `scan_test.go`, `scan_failures_test.go` and `scan_options_test.go` then stay unchanged;
    - in `internal/scan/select_test.go`, `github.com` becomes `bitbucket.org` in the inputs and the expected `ReasonUnknownHost` skips (lines 45 and 63);
    - in the existing `KnownHost` test of `internal/config/host_test.go`, `github.com` moves from the false list to the true list (below).
  - **`ResolveHost(s, remoteHost)`**:

    | Remote host or `--host` | `API` | Platform | Token |
    |---|---|---|---|
    | `github.com`, `www.github.com`, `ssh.github.com`, `api.github.com`, `GitHub.com:443` | `https://github.com` | GitHub | `Vars == token.GitHubVars`; `Env`/`Command` from a `github.com` profile when one exists |
    | `github-work` (profile `api: https://github.com`, `token_env: WORK_GH_TOKEN`) | `https://github.com` | GitHub | `Env == "WORK_GH_TOKEN"`, `Vars == token.GitHubVars` |
    | `github.example.com` (profile `platform: github`) | `https://github.example.com` | GitHub | `Vars == token.EnterpriseVars`, `Host == "github.example.com"`: the host as the `hosts` key names it, the remote host or the `--host` host |
    | `gitlab.com`, `git.corp` (`GITLAB_HOST`), `ssh.gitlab.example.com` | – | GitLab | `Vars == token.GitLabVars` |
    | `gist.github.com` (no profile) | – | – | error wrapping `config.ErrInvalid`, exactly `gist.github.com is not a repository host: check the git remote` |
    | `--host https://github.com`, no remote | – | GitHub | – |
    | `--host https://github.example.com`, no profile | – | GitLab (no guessing) | – |

  - **`ErrInsecureHost`**: a plain-http GitHub profile without `insecure` gives `ErrInsecureHost`, and the returned `Endpoint` still carries `Platform` (scan's failed row names it, FR-021a).
  - **`KnownHost`**: true for the four github.com-family names; false for `gist.github.com`, `github.evil.com` and `notgithub.com`.
  - **`OnHost`**: `OnHost("ssh.github.com", https://github.com)` and `OnHost("github-work", https://github.com)` are true; `OnHost("github.com", https://github.example.com)` is false.
  - **github.com takes no alias's token**: with aliases `github-work` and `github-perso`, both `api: https://github.com` (`token_env` `WORK` and `PERSO`), a `github.com` remote gets `Env == ""` and `Vars == token.GitHubVars`. A github.com-family host matches an entry by its key only, never by `api` host.
  - **Ambiguous `api` match on other hosts**: two entries `a` and `b` with `api: https://ghe.corp` and no entry keyed `ghe.corp` → for a `ghe.corp` remote, `ErrInvalid` with exactly `remote host ghe.corp matches hosts a and b in the config file: add an entry keyed ghe.corp` (names sorted). A single match keeps working as in 002.
  - **`GITLAB_HOST` on github.com** (FR-003), in `internal/config/settings_test.go`: `GITLAB_HOST=https://github.com`, and also `https://www.github.com`, makes `config.Load` fail with `ErrInvalid` and exactly `GITLAB_HOST https://github.com is a GitHub host: unset it, and pass --host https://github.com outside a clone` (resp. the `www.` URL); `GITLAB_HOST=https://gitlab.example.com` still loads.
  - **New `CheckProject(ep Endpoint, path string) error`**:
    - on a github.com endpoint, `owner/repo` and `123456` pass;
    - `a/b/c` and `a` give `ErrInvalid` with exactly `project path a/b/c on github.com: GitHub repositories are owner/repo` (resp. `a`);
    - on the Enterprise endpoint, the message says `on github.example.com`;
    - on a GitLab endpoint, `a/b/c` passes.
  - **Existing cases**: all of them pass with `Platform == status.PlatformGitLab` added.
- [X] T020 Implement resolution in `internal/config/host.go`. Makes T019 pass.
  - `Endpoint` gains `Platform status.Platform`.
  - `KnownHost` returns true when `hostKey(remoteHost) == "github.com"`.
  - **`ResolveHost`**:
    1. Pick the profile as today.
    2. Choose the platform: GitHub for the family or a GitHub profile, else GitLab. `GITLAB_HOST` always gives GitLab (FR-003).
    3. With no profile, refuse any other `*.github.com` host.
    4. Give a family host without a profile `API = https://github.com`.
    5. Set `Token.Vars` (and `Token.Host` for Enterprise Server) per T019.
    6. Return `Endpoint{Platform: p}` with `ErrInsecureHost`.
  - `CheckProject` treats a numeric path as an ID; otherwise a GitHub path must have exactly two `/` segments. The message uses `hostKey` of the endpoint host.
  - `Profile` skips the `api`-host fallback for the github.com family and reports an ambiguous match (several entries with the same `api` host, none keyed by the remote host); `ResolveHost` returns the error of T019. `KnownHost` still returns true for an ambiguous host, so a scan row fails with that error instead of being skipped.
  - `validate` in `internal/config/settings.go` refuses a `FallbackHost` whose `hostKey` is `github.com`, with the error of T019, so single and scan mode both stop before any request.
  - Doc comments: "GitLab instance" becomes "GitLab or GitHub instance".

**Checkpoint**:
- `task test` passes, and GitLab behaviour is byte-identical (SC-008).
- A GitHub remote now resolves to a GitHub endpoint with its own token variables, but `cli` and `scan` still build only GitLab clients (US1 and US2 add the switch).

---

## Phase 3: User Story 1 - See my GitHub repository's status at a glance (Priority: P1) 🎯 MVP

**Goal**: in a clone of a GitHub repository, `vigie` prints the one-line status, with the
same five fields read from GitHub, and exits 0. Missing or rejected tokens, invisible
repositories and bad paths map to the documented exit codes (FR-001, FR-003–FR-017,
FR-019–FR-021).

**Independent Test**: quickstart.md "US1" on a public github.com clone with only `GH_TOKEN`
exported; automated through the Enterprise Server harness against `githubtest`.

### E2E test for User Story 1 (write first, must fail) ⚠️

- [X] T021 [P] [US1] E2E tests for US1 in new `cmd/vigie/github_test.go`, plus `--help` assertions.
  - **Helper `newGitHubE2E(t)`**:
    - starts `githubtest.New(t, githubtest.Fixture(time.Now()))`;
    - writes the harness config (Path Conventions) with `writeConfig`;
    - creates a clone with `gitRepo(t, "https://ghe.test/octo/app.git", "main")`;
    - sets the env `E2E_GH_TOKEN=ghp_e2eGitHubToken` and no `GITLAB_TOKEN` or `GH_*`;
    - every run checks that `ghp_e2eGitHubToken` appears in neither stream and that a failed run printed nothing on stdout (`checkNoToken` pattern; generalise it to take the secret values).
  - **Cases**:
    1. **Piped line**: stdout is exactly `octo/app br 3/5 ci 1r/2p last:fail main 3m mr 5 is 12\n`, stderr is empty, exit 0. The line is plain ASCII with no `🐙` and no ESC (US1-1, US1-2, US1-3, US1-4, US3-3).
    2. **Issues disabled**: fixture `HasIssues = false` → the issues segment shows `–`, the others are unchanged, exit 0 (US1-5).
    3. **Runs refused**: `Set(githubtest.EndpointRuns, Behavior{Status: 403})` → the pipelines and last segments show `–` as in 001 `TestE2EUnavailableField`, exit 0 (US1-6).
    4. **401 on the lookup** → stderr starts with `token rejected by ` and contains `(401): check that the token is valid and not expired`, exit 3.
    5. **404** → stderr contains `repository octo/app not found on ` and `check that the token can see it`, exit 4 (US1-8).
    6. **403 with `X-GitHub-SSO: required; url=https://x`** → stderr contains `authorize the token for the organization's SAML single sign-on`, exit 3.
    7. **Remote path `https://ghe.test/a/b/c.git`** → stderr contains `project path a/b/c on ` and `GitHub repositories are owner/repo`, exit 2, and `githubtest` `Requests()` is empty (FR-004).
    8. **Outside a clone**: `-p octo/app --host <githubtest URL> -C <t.TempDir()>` → the line of case 1, with `main` as the default branch. `-p a/b/c --host <githubtest URL>` → exit 2 (FR-003).
    9. **Renamed**: fixture `MovedFrom = "octo/old"` and remote `https://ghe.test/octo/old.git` → the line starts `octo/app `, and with `-v` stderr contains `repository moved to octo/app: update the remote URL` (FR-020).
    10. **No GitHub token on github.com**: clone `https://github.com/cli/cli.git`, env `GITLAB_TOKEN=glpat-e2e` plus the `recordingProxy` variables → stdout empty; stderr exactly the GitHub missing-token line of T015 plus `\n`; exit 3; the proxy count is 0 (US1-7, SC-004).
    11. **SSH over 443**: remote `ssh://git@ssh.github.com:443/cli/cli.git` with the same env → the same message and exit 3 (recognised as github.com, not GitLab).
    12. **Gist**: remote `https://gist.github.com/abc123.git` → stderr `gist.github.com is not a repository host: check the git remote`, exit 2.
    13. **Lookup deadline** (FR-017, research G16): `Set(githubtest.EndpointRepo, Behavior{Delay: 5 * time.Second})` with `--timeout 300ms` → stdout empty, stderr contains `raise --timeout`, exit 5, all within 2 s. The same against `gitlabtest` `EndpointProject` in the 001 `newE2E` clone, since the fix covers both platforms.
  - **Help**: in `TestE2EHelp` (`cmd/vigie/main_test.go`), require `GitLab project or GitHub repository`, `GitLab or GitHub base URL`, `GH_TOKEN > GITHUB_TOKEN (github.com)`, `GH_ENTERPRISE_TOKEN > GITHUB_ENTERPRISE_TOKEN`, `GH_TOKEN, GITHUB_TOKEN` and `time budget of the project lookup`.

### github: fake server (test infrastructure)

- [X] T022 [P] [US1] Create the fake GitHub in new `internal/github/githubtest/server.go`, in the spirit of `internal/gitlab/gitlabtest/server.go`. It is test infrastructure, exercised by T023–T030; rate limits, ETags and billing come in US4 (T052).
  - **Types** (data-model "githubtest"):
    - `Repo{ID int64; FullName, DefaultBranch string; Archived, HasIssues bool; Branches []Branch; Runs []Run; OpenPRs, OpenIssues int; MovedFrom string}`;
    - `Branch{Name string; CommittedDate time.Time}`;
    - `Run{ID, WorkflowID int64; HeadBranch, HeadSHA, Event, Status, Conclusion string; UpdatedAt time.Time}`, with `Runs` newest first;
    - `Behavior{Status int; Header map[string]string; Body string; Delay time.Duration; Times int}`;
    - `type Endpoint string` with `EndpointRepo`, `EndpointRuns`, `EndpointBranches`, `EndpointPullRequests`, `EndpointIssues`;
    - `Request{Method, Path, Query, Operation, Authorization string}`.
  - **`Server` API**: `New(t, Repo) *Server` (an `httptest.Server`), `URL()`, `AddRepo(Repo)`, `SetRepo(Repo)` (replace by ID), `Set(Endpoint, Behavior)`, `SetAll(Behavior)`, `Requests() []Request`, `Tokens() []string` (the distinct `Authorization` values seen), `MaxInFlight(Endpoint) int`, and `Handler() http.Handler` (for T053).
  - **REST under `/api/v3`**:
    - `GET /repos/{owner}/{repo}` and `GET /repositories/{id}` answer `id`, `full_name`, `html_url` (`<URL>/<full_name>`), `default_branch` (`""` when there is no branch), `archived` and `has_issues`;
    - `MovedFrom` answers `301` with `Location: <URL>/api/v3/repositories/{id}`;
    - an unknown repository answers `404 {"message":"Not Found"}`;
    - `GET /repos/{o}/{r}/actions/runs` filters by `status` (one value; a comma list matches nothing, as observed in G5), `branch` (`head_branch`) and `head_sha`, honours `per_page` (default 30), and answers `{"total_count": <filtered count>, "workflow_runs": [...]}`; each run has `id`, `workflow_id`, `head_branch`, `head_sha`, `event`, `status`, `conclusion` (`null` when empty) and `updated_at` (RFC 3339).
  - **GraphQL at `POST /api/graphql`**: decodes `{query, operationName, variables}` and dispatches on `operationName`:
    - `Branches` (`owner`, `name`, `cursor`): `refs` pages of 100 with `totalCount`, `pageInfo{hasNextPage, endCursor}` and `nodes{name, target{committedDate}}`;
    - `PullRequests`: `pullRequests.totalCount = OpenPRs`;
    - `Issues`: `hasIssuesEnabled`, `issues.totalCount = OpenIssues`;
    - an unknown repository gives `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository …"}]}`;
    - an unknown operation gives 400.
  - **Behaviours**: a `Behavior` replaces the answer of its endpoint (`Status` with a `{"message":…}` body or `Body`, plus `Header`) for its first `Times` requests, or always when `Times` is 0.
  - **`Fixture(now)`** returns `octo/app` with `ID 42`, `DefaultBranch "main"` and `HasIssues true`:
    - **branches**: `main` (now−1h), `feature` (now−2d), `docs` (now−10d), `old1` (now−200d), `old2` (now−400d), so 3/5 are active over 90 days;
    - **runs**, newest first:

      | ID | Workflow | Branch | SHA | Event | Status | Conclusion | Updated |
      |---|---|---|---|---|---|---|---|
      | 110 | 2 | main | `c0ffee…` (40 hex) | push | completed | failure | now−3m |
      | 109 | 1 | main | `c0ffee…` | push | completed | success | now−5m |
      | 108 | 3 | feature | `feed…` | pull_request | in_progress | | now−1m |
      | 107 | 3 | feature | `beef…` | push | queued | | |
      | 106 | 3 | docs | `d0c5…` | push | pending | | |
      | 105 | 4 | docs | `d0c5…` | push | waiting | | |
      | 100 | 1 | main | `dead…` | push | completed | success | now−2h |

    - **counts**: `OpenPRs 5`, `OpenIssues 12`.

    So the line reads `br 3/5 ci 1r/2p last:fail main 3m mr 5 is 12`.

### github: the client (test → implementation pairs, strictly in this order)

- [X] T023 [US1] Test construction and lookup in new `internal/github/client_test.go`, with new `internal/github/export_test.go` exposing `func APIBases(c *Client) (rest, graphql string)`.
  - **API addresses**:
    - `github.NewClient(https://github.com, …)` → `https://api.github.com` and `https://api.github.com/graphql`;
    - `https://github.example.com` → `https://github.example.com/api/v3` and `https://github.example.com/api/graphql`;
    - `https://ghe.corp/sub` → `https://ghe.corp/sub/api/v3`.
  - **Plain http**: `http://127.0.0.1:1` without `Insecure` → `forge.ErrInsecure`; with `Insecure` it is accepted.
  - **Lookup**, against `githubtest`:
    - `Project(ctx, "octo/app")` returns `forge.Project{ID: 42, Path: "octo/app", WebURL: <URL>/octo/app, DefaultBranch: "main"}`;
    - `Project(ctx, "42")` uses `/api/v3/repositories/42`;
    - an archived repository has `Archived: true`;
    - an empty repository has `DefaultBranch: ""`.
  - **Request headers**: `Authorization: Bearer <token>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28` and `User-Agent: vigie`.
  - **Interface**: `var _ forge.Client = (*github.Client)(nil)`.
- [X] T024 [US1] Implement the client. Makes T023 pass.
  - **New `internal/github/client.go`**:
    - `type Client struct` with the fields of data-model "github";
    - `NewClient(base *url.URL, tok token.Token, opts forge.Options) (*Client, error)`, deriving the addresses per research G2 (github.com family → `https://api.github.com`, else `base + /api/v3` and `base + /api/graphql`) and using `forge.NewHTTPClient(opts)`;
    - `get(ctx, path string, q url.Values, out any) (http.Header, error)` for REST and `query(ctx, op, doc string, vars map[string]any, out any) error` for GraphQL (POST `{"query", "operationName", "variables"}`; decodes `data`; a non-empty `errors` array becomes an error carrying the first `type` and `message`);
    - the headers of T023, a 16 MiB body limit, and transport errors classified as in gitlab (`forge.ErrNetwork` with `raise --timeout` on a deadline; `context.Canceled` not network).
  - **New `internal/github/repo.go`**: `Project` (an all-digit ref uses `/repositories/{id}`, else `/repos/{ref}`) and `Source(p forge.Project) status.Source`, returning the `repoSource` of T030; until then, a stub type in `metrics.go` whose methods return `errors.ErrUnsupported`.
- [X] T025 [US1] Test lookup errors in new `internal/github/repo_test.go` (research G9, contracts/cli.md "Other GitHub errors"). `<host>` is the host of the base URL.

  | Answer | Error | Exact text |
  |---|---|---|
  | 401 | `forge.ErrUnauthorized` | `token rejected by <host> (401): check that the token is valid and not expired` |
  | 403 with `X-GitHub-SSO` | `forge.ErrForbidden` | `access to repository octo/app denied (403): authorize the token for the organization's SAML single sign-on` |
  | 403 with `X-Accepted-GitHub-Permissions: actions=read` | `forge.ErrForbidden` | `access to repository octo/app denied (403): the token lacks the permission "actions=read"` |
  | plain 403 | `forge.ErrForbidden` | `access to repository octo/app denied (403)` |
  | 404 | `forge.ErrNotFound` | `repository octo/gone not found on <host>: check that the token can see it (for a fine-grained token, add the repository to it; for an organization with single sign-on, authorize the token)` |
  | 429, 500, 502 | `forge.ErrNetwork` | – |
  | invalid JSON | – | starts with `malformed response from` |

  Also:
  - **Renames**: `MovedFrom: "octo/old"` and `Project(ctx, "octo/old")` → `Path == "octo/app"` (301 followed, same host).
  - **Redirects**: a redirect to another host is refused.
  - **Cancellation**: a cancelled context gives an error that wraps `context.Canceled` and not `forge.ErrNetwork`.
  - **Deadline**: a 2 s `Delay` with a 100 ms deadline → `forge.ErrNetwork`, with a message containing `raise --timeout`.
  - **Secrets**: no error text contains the token.
- [X] T026 [US1] Implement answer classification in new `internal/github/limits.go`: `classify(resp *http.Response, body []byte) error` for 401/403/404/429/5xx, with a `statusError{code int; retryAfter time.Duration; header http.Header; message string}` implementing `RetryAfter()`. Map lookup answers in `internal/github/repo.go` to the texts of T025. Makes T025 pass. Rate-limit detection is added before these rules in US4 (T057).
- [X] T027 [US1] Test the pure run functions in new `internal/github/runs_test.go`, through `export_test.go` aliases `type Run = run`, `var RunState = runState` and `var Combine = combine` (research G6, FR-011, FR-014).
  - **`RunState`**, every row of the G6 table:

    | `status` | `conclusion` | State |
    |---|---|---|
    | `in_progress` | – | `running` |
    | `queued`, `requested`, `pending` | – | `pending` |
    | `waiting` | – | `manual` |
    | `completed` | `success`, `neutral` | `success` |
    | `completed` | `failure`, `timed_out`, `startup_failure` | `failed` |
    | `completed` | `cancelled`, `stale` | `canceled` |
    | `completed` | `skipped` | `skipped` |
    | `completed` | `action_required` | `manual` |
    | `completed` | `""` or `weird` | `unknown` |
    | `weird` | – | `unknown` |

  - **`Combine(runs, "main", commitURL)`**:
    - only the runs of the newest run's `HeadSHA` count;
    - per `WorkflowID`, the highest `ID` wins: a re-run (higher ID, success) replaces a failed run of the same workflow;
    - precedence: `running` > `pending` > `manual` > `failed` > `canceled` > `unknown` > `success` > `skipped`, one case per adjacent pair;
    - `UpdatedAt` is the maximum over the counted runs;
    - `ID` is 0, `Ref` is `main`, and `WebURL` is `commitURL`;
    - an empty slice gives `Status == status.PipelineNone`;
    - the runs of an older SHA never count.
- [X] T028 [US1] Implement `run` (JSON fields of data-model "run"; `Conclusion` decodes `null` as `""`), `runState` and `combine` in new `internal/github/runs.go`, as pure functions with no I/O. Makes T027 pass.
- [X] T029 [US1] Test the five fields through `client.Source(p)` in new `internal/github/metrics_test.go`, against `githubtest` with `Fixture(now)` (data-model "repoSource", research G4–G6).
  - **Branches**:
    - `Branches(ctx, now.AddDate(0,0,-90))` → `{Active: 3, Total: 5}`;
    - 250 branches → 3 GraphQL requests with `operationName == "Branches"` and the cursor passed on;
    - 10,001 branches (101 pages) → both counts capped as in gitlab `TestBranchesCapped`;
    - an empty repository → `{0, 0}`.
  - **Pipelines**:
    - `{Running: 1, Pending: 2}` from exactly 4 run requests (`status=in_progress`, `queued`, `requested`, `pending`; never `waiting`);
    - a fixture with 1,000 queued runs → an error whose text is exactly `more than 1,000 workflow runs: GitHub does not report an exact count` (FR-016);
    - 403 and 404 on runs → an error wrapping `status.ErrUnavailable`; the 403 reason names the permission from `X-Accepted-GitHub-Permissions` when present.
  - **LastPipeline**:
    - `LastPipeline(ctx, "main")` → `failed`, `Ref "main"`, `UpdatedAt` now−3m, `WebURL <URL>/octo/app/commit/c0ffee…`, `ID 0`, from one request `branch=main&per_page=30`;
    - `"feature"` → `running` (run 108, a `pull_request` run, counts);
    - a branch with no run, and `""`, → `PipelineNone` with no request for `""`;
    - 30 runs all of one SHA → a second request `branch=B&head_sha=SHA&per_page=100`, whose runs are all combined, and never a third;
    - a newer commit with no run leaves the result of the previous one;
    - 403 → `status.ErrUnavailable`.
  - **OpenMRs**: `{N: 5}`; 10,001 → `Capped`.
  - **OpenIssues**: `{N: 12}`, not 17 (pull requests excluded, FR-013); `HasIssues: false` → `status.ErrUnavailable`; 10,001 → `Capped`; GraphQL `repository: null` → `status.ErrUnavailable`.
  - **401 on any field** → `forge.ErrUnauthorized`, so `forge.RefreshError` fails the refresh.
- [X] T030 [US1] Implement the fields. Makes T029 pass.
  - **New `internal/github/queries.go`**: the named GraphQL documents `Branches`, `PullRequests` and `Issues` (`first: 100` for refs, `first: 1` for counts, `refPrefix: "refs/heads/"`).
  - **`internal/github/metrics.go`**: replace the stub with `repoSource{c *Client; p forge.Project}` and its methods per data-model "repoSource":
    - the four `Pipelines` requests run concurrently;
    - `LastPipeline` builds `commitURL` from `p.WebURL + "/commit/" + sha`;
    - field-level 403/404 become `status.ErrUnavailable` with the reason;
    - 401 becomes `forge.ErrUnauthorized`;
    - counts above 10,000 are `status.Count{Capped: true}`.

### render: platform icon (test → implementation)

- [X] T031 [P] [US1] Test the platform icon in `internal/render/text_test.go` (FR-021, research G11).
  - Set `Project.Platform = status.PlatformGitLab` in the shared fixture.
  - Edit by hand every golden rendered with emoji (`internal/render/testdata/text_full_emoji_color.golden`, `text_full_emoji_nocolor.golden`, `width_emoji.golden`, `width_emoji_color.golden`, and any other `text_*` golden whose style has emoji), replacing `🗼` with `🦊`.
  - ASCII goldens stay byte-identical.
  - New `internal/render/testdata/text_github_emoji_nocolor.golden` (`🐙 octo/app …`) and `text_github_ascii.golden` (no label, same layout as GitLab).
  - A snapshot with `Platform == ""` still shows `🗼`.
  - Width budgets are unchanged: `CellWidth("🦊") == CellWidth("🐙") == 2`.
- [X] T032 [US1] Implement the platform icon. Makes T031 pass.
  - **`internal/render/icons.go`**: `platformLooks = map[status.Platform]look{status.PlatformGitLab: {icon: "🦊", color: bold}, status.PlatformGitHub: {icon: "🐙", color: bold}}`, with `projectLook` kept as the fallback.
  - **`internal/render/text.go`**: `projectSegment` picks its look from `p.Platform`.

### cli: wiring (makes T021 pass)

- [X] T033 [US1] Wire GitHub into single mode in `internal/cli/run.go` (depends on T024–T032).
  - **`newRunner`**:
    1. After `config.ResolveHost`, compute `ref` (`s.Project`, else `co.Remote.Path`).
    2. Call `config.CheckProject(ep, ref)` before resolving the token, so a bad path never runs a `token_command`.
    3. Resolve the token.
    4. Log the endpoint at debug with `platform`.
    5. Build the client with a six-line switch on `ep.Platform`: `status.PlatformGitHub` → `github.NewClient(ep.API, tok, opts)`, else `gitlab.NewClient(ep.API, tok, opts)`, with `opts := forge.Options{Insecure: ep.Insecure, ExtraRootsPEM: ep.ExtraRootsPEM}`.
    6. Store `ep.Platform` on the runner.
  - **`resolve`**:
    - runs `r.client.Project` under `context.WithTimeout(ctx, r.s.Timeout)` (research G16), which also bounds the watch re-resolve;
    - builds `status.Project{Path, WebURL, Archived, Platform: r.platform}`;
    - when `r.detect` and `!strings.EqualFold(p.Path, r.ref)`, logs at debug with the message `repository moved to <p.Path>: update the remote URL` (FR-020);
    - the 5-minute re-resolve of watch mode picks up a mid-session rename unchanged.
- [X] T034 [US1] Update `internal/cli/help.txt` per contracts/cli.md "`--help` additions". Together with T033, this makes T021 pass in full.
  - the first paragraph;
  - the `--host` line;
  - the `Token:` block of `Precedence:` (all three kinds of host);
  - the four GitHub variables under `Environment:`;
  - the `--timeout` line becomes `time budget of the project lookup and of one whole refresh, per repository when scanning`, with `(default 10s)` kept on its continuation line (FR-017).

**Checkpoint**: US1 is shippable as the MVP.
- A GitHub clone gets its line, and errors exit 2/3/4 as documented.
- No GitLab token reaches GitHub.
- Not yet available: scan mode skips GitHub clones as `unknown host`; JSON has no `platform`
  (US3); an exhausted allowance is reported as `denied` (exit 3) until US4.

---

## Phase 4: User Story 2 - One table for a folder of GitLab and GitHub clones (Priority: P2)

**Goal**: a folder scan lists GitLab and GitHub clones in one table with a `PLATFORM` column,
skips GitHub clones that have no token source at all, and keeps 002's per-host rules
(FR-021a, FR-024–FR-025a).

**Independent Test**: quickstart.md "US2" on `~/gh-demo`; automated with `gitlabtest` and
`githubtest` hosts.

### E2E test for User Story 2 (write first, must fail) ⚠️

- [X] T035 [P] [US2] E2E tests for US2 in new `cmd/vigie/github_scan_test.go`, plus golden and help updates.
  - **Existing golden**: edit `cmd/vigie/testdata/scan_table.golden` by hand to add the `PLATFORM` column (`gitlab` in each row), so the 002 `TestE2EScanTable` fails until T039.
  - **Mixed folder**: `gl/api` and `gl/web` on `gitlab.test` (`gitlabtest`, as in `newScanE2E`); `gh/app` (`octo/app`) and `gh/lib` (`octo/lib`, `AddRepo` copy of the fixture) on `ghe.test` (harness config); env `GITLAB_TOKEN` and `E2E_GH_TOKEN`.
    - stdout equals new `cmd/vigie/testdata/scan_mixed.golden`, written by hand from contracts/cli.md "Scan table" and the two fixtures (ASCII, tree order, `PLATFORM` between `DIR` and `PROJECT`);
    - stderr is exactly `4 repositories found: 4 listed\n`;
    - exit 0 (US2-1, SC-003).
  - **No token at all**: add `hub/one` and `hub/two` with origins `https://github.com/cli/cli.git` and `git@github.com:cli/go-gh.git`; env without `GH_TOKEN`/`GITHUB_TOKEN`, plus the `recordingProxy` variables.
    - stdout has the 4 rows above only;
    - stderr is `6 repositories found: 4 listed, 2 skipped (2 no token); run with -v to list them`;
    - `-v` adds one debug line per skipped clone naming its dir and `no token`;
    - no error line; proxy count 0; exit 0 (US2-2, FR-025a, SC-004).
  - **Only github.com clones, no token** → stdout empty, stderr exactly `no GitLab or GitHub repository found: 2 skipped (2 no token); export GH_TOKEN for github.com clones, or run with -v`, exit 2.
  - **Rejected token**: `githubtest` `SetAll(Behavior{Status: 401})` → `gh/app` and `gh/lib` show `! auth`, the GitLab rows are complete, stderr has exactly one line starting `token rejected by ` and ending ` (2 repositories)`, exit 0 (US2-3, FR-025).
  - **Failing token source**: `E2E_GH_TOKEN` unset while the entry names it → the rows show `! auth` (not skipped), with one line `E2E_GH_TOKEN is not set: …` (FR-025a).
  - **Host filter**: `--host <githubtest URL>` → only `gh/*` rows; the summary counts 2 `other host`; `gitlabtest` `Requests()` is empty (US2-4).
  - **Help**: in `TestE2EHelp`, require `gitlab.com, github.com, config "hosts", GITLAB_HOST` and `A GitHub clone needs a GitHub token, else it is skipped`.

### token: configured source (test → implementation)

- [X] T036 [P] [US2] Test `(token.Source) Configured(env map[string]string) bool` in `internal/token/resolve_test.go` (research G3):
  - true for `Source{Env: "X"}` with `X` unset (a failing source, not a missing one);
  - true for `Source{Command: []string{"gh", "auth", "token"}}`, which is never run;
  - true for `Source{Vars: GitHubVars}` with `GITHUB_TOKEN` set;
  - false for `Source{Vars: GitHubVars}` with only `GITLAB_TOKEN` and `GH_ENTERPRISE_TOKEN` set;
  - false for `Source{Vars: EnterpriseVars}` with only `GH_TOKEN` set;
  - a value of spaces counts as unset.
- [X] T037 [US2] Implement `Configured` in `internal/token/resolve.go`. Makes T036 pass.

### render: PLATFORM column (test → implementation)

- [X] T038 [P] [US2] Test the `PLATFORM` column in `internal/render/table_test.go` (FR-021a).
  - Set `Platform: status.PlatformGitLab` on the shared rows.
  - Edit by hand every `internal/render/testdata/table_*.golden` (`table_ascii`, `table_capped`, `table_emoji_color`, `table_emoji_nocolor`, `table_failures`, `table_subset`, `table_width`) to add `PLATFORM` between `DIR` and `PROJECT`: bold header with color, plain `gitlab` cells, failed rows included.
  - New `internal/render/testdata/table_mixed.golden`: one GitHub row `cli github sgaunet/vigie …`, one archived GitHub row `acme/web archived`, and one failed GitHub row (`! not found`) still showing `github`.
  - **Width**: with `Width` below the table width, `PROJECT` is cut first, then `DIR`, and `PLATFORM` is never cut.
- [X] T039 [US2] Implement the column in `internal/render/table.go`: `headPlatform = "PLATFORM"`, a cell from `r.Snapshot.Project.Platform`, and column indexes in `columnWidths`, `tableWidth` and `fit` shifted by one. Makes T038 pass; the 002 e2e golden of T035 passes too.

### github: bounded retry for scans (test → implementation, after T030)

- [X] T040 [P] [US2] Test `Options.Retry` in new `internal/github/retry_test.go`, mirroring `internal/gitlab/retry_test.go` (002 S6) and exposing `var RetryBase = &retryBase` in `internal/github/export_test.go`:
  - 502 for the first 2 requests then 200 → success, with `OnThrottle` called twice;
  - 429 with `Retry-After: 1` → the wait honours it;
  - no retry whose wait would end after the context deadline;
  - at most `maxRetries` retries;
  - only 429 and 5xx are retried, never 401/403/404;
  - a wait interrupted by cancel returns at once with `context.Canceled`;
  - `Retry: false` → no retry;
  - GraphQL requests follow the same rule.
- [X] T041 [US2] Implement the retry loop in `get` and `query` of `internal/github/client.go`, with the same constants and rule as `gitlab.Client.get`. Makes T040 pass.

### scan: GitHub clones (test → implementation pairs, in order; T043 needs T037)

- [X] T042 [P] [US2] Test recognition, the new skip reason and the "nothing listed" message.
  - **`internal/scan/select_test.go`**: `scan.Select(in, s, env)` (new `env` argument) with the profiles of T019:
    - a `github.com` clone with `GH_TOKEN` in env → listed, `Endpoint.Platform == status.PlatformGitHub`;
    - the same without any GitHub variable → `Skip{Reason: scan.ReasonNoToken}`;
    - with a `github.com` profile whose `token_env` names an unset variable → listed (configured);
    - the `platform: github` profile with only `GH_TOKEN` set → `ReasonNoToken`; with `GH_ENTERPRISE_TOKEN` set → listed;
    - a GitHub clone with path `a/b/c` → `ReasonRemoteURL`;
    - `gist.github.com` → `ReasonUnknownHost`;
    - a GitLab clone without `GITLAB_TOKEN` → listed, as in 002;
    - `--host https://github.com` keeps `ssh.github.com` and `github-work` clones and skips GitLab clones as `ReasonOtherHost`;
    - with `--host https://github.com` and no GitHub token, the github.com clones are still `ReasonNoToken` (FR-025a has no `--host` exception).
  - **`internal/scan/summary_test.go`**:
    - `ReasonNoToken.Text("origin") == "no token"`;
    - `Summary.Skips()` order is `bare, unreadable, no remote "<r>", remote URL not understood, unknown host, no token, other host`;
    - `Summary.Err(4)` wraps `scan.ErrNothingListed` with exactly `no GitLab or GitHub repository found: 2 skipped (2 no token); export GH_TOKEN for github.com clones, or run with -v`;
    - without `no token` skips, `Summary.Err(4)` gives `no GitLab or GitHub repository found: 3 skipped (1 no remote "origin", 2 unknown host); add self-hosted hosts to the config file "hosts", or run with -v`.
- [X] T043 [US2] Implement recognition and the new reason. Makes T042 pass; `task test` compiles again everywhere.
  - **`internal/scan/errors.go`**: rename `ErrNoGitLab` to `ErrNothingListed`, with text `no GitLab or GitHub repository found`.
  - **`internal/scan/outcome.go`**: `ReasonNoToken = "no token"` in `reasonOrder` before `ReasonOtherHost`; `Summary.Err` picks the hint.
  - **`internal/scan/select.go`**:
    - `Select(in []Inspected, s config.Settings, env map[string]string)`;
    - after resolving the endpoint, a GitHub endpoint with `!ep.Token.Configured(env)` → `ReasonNoToken`, and `config.CheckProject` failing → `ReasonRemoteURL`;
    - the doc comment names both platforms.
  - **Rename the callers** in `cmd/vigie/exitcode.go`, `cmd/vigie/exitcode_test.go` and `internal/cli/scan.go`, which also passes `std.Env` to `scan.Select`.
  - **Update the 002 expectations of the old message**: in `TestE2EScanUnknownHostsOnly` (`cmd/vigie/scan_test.go`) and `TestE2EScanHostFilter` (`cmd/vigie/scan_options_test.go`), `no GitLab repository found: …` becomes `no GitLab or GitHub repository found: …`; nothing else in them changes.
- [X] T044 [US2] Test GitHub hosts in `internal/scan/fetch_test.go`, with a `githubtest` host `Endpoint{API: srv URL, Insecure: true, Platform: status.PlatformGitHub, Token: token.Source{Env: "TOK_H", Vars: token.EnterpriseVars}}` next to a `gitlabtest` host:
  - the GitHub row is OK with the fixture values and `Snapshot.Project.Platform == github`, and the GitLab row has `gitlab`;
  - a 404 GitHub repository gives a `FailureNotFound` row with `Platform == github` and `WebURL == <API>/<path>`;
  - a `Repo` with `Err` set and a GitHub endpoint keeps its platform;
  - the GitHub requests carry `Bearer <TOK_H>` and `gitlabtest` never sees it;
  - two endpoints with the same API URL and token command but different `Platform` are two hosts: the counter script of 002 T032 runs twice;
  - with `Retry` on, a 502 then 200 on `githubtest` gives an OK row.
- [X] T045 [US2] Make `Fetch` platform-aware in `internal/scan/fetch.go`. Makes T044 pass.
  - **`host.connect`**: the second constructor switch on `ep.Platform` (research G1), with `forge.Options{Insecure, ExtraRootsPEM, Retry: true, OnThrottle: h.throttled, Revalidate: false}`.
  - **`hostID`**: adds `string(ep.Platform)`, `strings.Join(ep.Token.Vars, ",")` and `ep.Token.Host`.
  - **Rows**: `fetchRepo` and `failedRow` set `Project.Platform = r.Endpoint.Platform`.
  - **Doc comments**: "GitLab instance" becomes "hosting instance".

### cli: wiring (makes T035 pass)

- [X] T046 [US2] Update the `Folder of clones:` section of `internal/cli/help.txt` per contracts/cli.md (known hosts include `github.com`, plus the GitHub token sentence). With T043 and T045, this makes T035 pass.

**Checkpoint**: a mixed folder gives one table. github.com clones without a token are
skipped silently and counted. GitLab rows are unchanged apart from the `PLATFORM` column.

---

## Phase 5: User Story 3 - Same scripts and status bars for both platforms (Priority: P3)

**Goal**: every JSON record carries `platform`, validates against the published schema, and
uses the same state vocabulary on both platforms (FR-014, FR-022, SC-007).

**Independent Test**: `vigie -o json | jq -r .last.status` prints `failed` in a GitHub clone
whose last commit's CI failed (quickstart.md "US3").

### E2E test for User Story 3 (write first, must fail) ⚠️

- [X] T047 [P] [US3] E2E JSON tests.
  - **Schema helper**: extend `schemaKeys` in `cmd/vigie/scan_json_test.go` into `schema(t)`, which also returns the `required` list.
  - **New `TestE2EGitHubJSON` in `cmd/vigie/github_test.go`**, with `-o json` in the T021 clone. The raw line starts with `{"platform":"github",`. Then:
    - `project == "octo/app"`;
    - `last.status == "failed"`, `last` has no `id` key, and `last.web_url == <githubtest URL>/octo/app/commit/c0ffee…`;
    - `pipelines == {"running":1,"pending":2}`;
    - every key is in the schema, and every `required` key, `platform` included, is present (US3-1, SC-007).
  - **`TestE2EJSONOutput` in `cmd/vigie/us2_test.go`**: the GitLab record has `"platform":"gitlab"` as its first property.
  - **`TestE2EScanJSON` in `cmd/vigie/scan_json_test.go`**: every record has `platform`. On the T035 mixed folder, the `[dir, platform]` pairs are `gh/app github`, `gh/lib github`, `gl/api gitlab`, `gl/web gitlab` (US2-5).
  - **Same filter on both platforms**: with the `gitlabtest` last pipeline set to `failed`, the records whose `last.status == "failed"` include one of each platform.

### Contract and render (test → implementation)

- [X] T048 [P] [US3] Copy `specs/003-github-support/contracts/status.schema.json` over `docs/status.schema.json` unchanged; `diff` must print nothing (quickstart gate).
- [X] T049 [P] [US3] Test JSON records in `internal/render/json_test.go` (FR-022).
  - Set `Platform: status.PlatformGitLab` in the fixtures.
  - Edit by hand every `internal/render/testdata/json_*.golden` (`json_archived`, `json_capped`, `json_full`, `json_none`, `json_row_failed`, `json_row_ok`, `json_stale`, `json_subset`, `json_unavailable`) to insert `"platform":"gitlab",` as the first property.
  - New `internal/render/testdata/json_github.golden`: GitHub snapshot, `last` without `id`, `web_url` the commit.
  - New `internal/render/testdata/json_row_github_failed.golden`: a failed GitHub scan row with `"platform":"github"`.
  - New `internal/render/testdata/json_runs_capped.golden`: `"pipelines":null`, with `errors` holding `{"field":"pipelines","reason":"more than 1,000 workflow runs: GitHub does not report an exact count"}`.
- [X] T050 [US3] Implement the property in `internal/render/json.go`: ``Platform status.Platform `json:"platform"` `` as the first field of `jsonRecord`, set in `baseRecord` so `JSON` and `JSONRow` both carry it. Makes T049 and, with T048, T047 pass.

**Checkpoint**: one schema and one vocabulary for both platforms; a `jq` filter written for
GitLab works on GitHub records.

---

## Phase 6: User Story 4 - Keep watching a GitHub repository within its request allowance (Priority: P4)

**Goal**: watch mode stays under half of each hourly pool. An exhausted allowance is
throttling, never an authentication failure: watch waits, one-shot exits 5, and a scan
latches the host (FR-017, FR-018, SC-005, SC-006).

**Independent Test**: quickstart.md "US4" (10 minutes against github.com with
`gh api rate_limit`); automated with `githubtest` counters and rate-limit behaviours.

### E2E test for User Story 4 (write first, must fail) ⚠️

- [X] T051 [P] [US4] E2E tests for US4 in new `cmd/vigie/github_limits_test.go`, using the T021 harness.
  - **One-shot**: `SetAll(Behavior{RateLimited: true, Reset: now+13m})` → stdout empty; stderr matches `^\S+ request allowance exhausted until \d\d:\d\d \(in 1[23]m\): wait for the reset; it is shared by all your GitHub tokens\n$`; exit 5, never 3 (US4-3).
  - **Secondary limit**: `403` with `Retry-After: 1` and the body `You have exceeded a secondary rate limit` → exit 5.
  - **Scan**: 3 `ghe.test` clones plus 1 `gitlab.test` clone, with everything on `githubtest` rate-limited:
    - the GitHub rows show `! network` and the GitLab row is complete;
    - stderr holds exactly one line matching `request allowance exhausted until .* \(3 repositories\)$`, also with `-q`;
    - `githubtest` received at most `scan.ReposPerHost` requests;
    - exit 0 (FR-018 latch).
  - **Watch**: `-w -o json --interval 2s`, started with the `startWatch` helper of `cmd/vigie/watch_test.go` (US4-2, SC-006):
    1. After the first record, set `SetAll(Behavior{RateLimited: true, Reset: now+3s})`.
    2. The next record has `"stale":true`. Take the `Requests()` count at that moment.
    3. Clear the behaviour with `SetAll(Behavior{})`.
    4. No request reaches `githubtest` before the reset: the count is unchanged until then.
    5. A record with `"stale":false` follows within reset + 1 s margin + one interval.

### github: rate limits, revalidation, budget (strictly in this order)

- [X] T052 [P] [US4] Extend `internal/github/githubtest/server.go` (data-model "githubtest").
  - **Rate limits**:
    - `Behavior` gains `RateLimited bool`, `Reset time.Time` and `Secondary bool`;
    - normal answers carry `x-ratelimit-remaining: 4999`, `x-ratelimit-reset` and `x-ratelimit-resource` (`core` or `graphql`);
    - `RateLimited` on REST → `403` with `x-ratelimit-remaining: 0` and the reset epoch;
    - `RateLimited` on GraphQL → `200` with `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}` and `x-ratelimit-remaining: 0`;
    - `Secondary` → `403` with the secondary-limit body.
  - **ETags**: every REST 200 carries `ETag: "<hash of body>"`, and a matching `If-None-Match` answers `304` with an empty body.
  - **Billing**: `type Pool string` with `PoolCore` and `PoolGraphQL`. `Billed(Pool) int` counts REST answers other than 304, and GraphQL queries.
- [X] T053 [US4] Test the hourly budget in new `internal/github/allowance_test.go` (SC-005, research G7, G13). It fails until T055: without revalidation, REST is billed about 3,600 times.
  - **Transport hook**: add `func WithRoundTripper(c *Client, rt http.RoundTripper)` to `internal/github/export_test.go`. It serves `githubtest` `Handler()` in process, because real sockets keep a `testing/synctest` bubble from advancing time.
  - **Setup**: `TestWatchAllowance`, inside `synctest.Test`. Build a client with `forge.Options{Revalidate: true}` over a 100-branch fixture, then run `watch.Run` for one simulated hour at `Interval 5s` with all five fields and a no-op `Draw`.
  - **Static repository**: `Billed(PoolCore) <= 2500` and `Billed(PoolGraphQL) <= 2500`.
  - **Changing runs**: a run whose state changes every 2 minutes, through `SetRepo`, keeps both pools within 2,500.
- [X] T054 [US4] Test revalidation in new `internal/github/cache_test.go`:
  - with `Revalidate: true`, a second identical lookup sends `If-None-Match`, gets 304, returns an equal `forge.Project`, and `Billed(PoolCore) == 1`;
  - after `SetRepo` changes the repository, the next lookup is a 200 with the new values;
  - with `Revalidate: false`, no `If-None-Match` is sent and the count is 2;
  - a forced `304` with no cached entry (`Behavior{Status: 304}`) → an error starting `malformed response from`;
  - GraphQL queries never carry `If-None-Match`;
  - concurrent identical `get`s pass `-race`.
- [X] T055 [US4] Implement the revalidation cache in new `internal/github/cache.go`: `map[string]entry{etag string; body []byte}` under a `sync.Mutex`, keyed by the full request URL, created only when `Options.Revalidate`. `get` sends `If-None-Match` when an entry exists, stores the body of a 200 that has an `ETag`, and decodes the stored body on a 304. Nothing is written to disk. Makes T053 and T054 pass.
- [X] T056 [US4] Test limits and the gate in new `internal/github/limits_test.go` (research G8), with `var Now = &now` (the gate clock) added to `internal/github/export_test.go`.
  - **Primary limit on REST**: the error wraps `forge.ErrRateLimit` and `forge.ErrNetwork`, never `ErrUnauthorized` or `ErrForbidden`.
    - `forge.RetryAfter(err)` is `reset + 1s − Now()`;
    - the text is exactly `<host> request allowance exhausted until <reset.Local() as 15:04> (in 13m): wait for the reset; it is shared by all your GitHub tokens`;
    - a later REST request before the reset returns the same `RetryAfter` with no new server request;
    - a GraphQL field still succeeds, because the other pool is open.
  - **Primary limit on GraphQL** (200 with `errors` and remaining 0): blocks the `graphql` pool only.
  - **Secondary limit**:
    - `403` with `Retry-After: 30` → `forge.ErrNetwork` but not `ErrRateLimit`, `RetryAfter` 30 s, both pools blocked;
    - the secondary body without the header → 60 s.
  - **Reopening**: once `Now` passes the block-until time, requests are sent again.
  - **Not limits**:
    - a 429 without rate headers is plain `forge.ErrNetwork`, as in US1;
    - answers without any rate headers (Enterprise Server with limits off) are never limits;
    - a rate-limited field is never `status.ErrUnavailable`.
  - **Retry interplay** (`Retry: true`): a secondary limit is retried within the deadline; a primary limit is returned at once, never retried.
  - **Watch stale** (inside `synctest.Test`, through `WithRoundTripper`): with every field rate-limited until `now+10m`, `watch.Run` draws a stale snapshot, the server receives nothing until the reset, and fresh values are drawn within one interval after it.
- [X] T057 [US4] Implement limits and the gate in `internal/github/limits.go`. Makes T056 pass.
  - **Gate**: pools `core` and `graphql`; `gate` is `map[pool]time.Time` under a mutex, with block-until times and the stored error.
  - **`rateLimitError`**:
    - `host`, `until` and `after`;
    - `Error()` is the text of T056;
    - `RetryAfter()`;
    - `Unwrap() []error`: `{forge.ErrRateLimit, forge.ErrNetwork}` for a primary limit, `{forge.ErrNetwork}` for a secondary one.
  - **Classification**: applied first in `classify` and in `query`, in the order of G8:

    | Answer | Kind | Blocks |
    |---|---|---|
    | 403/429 or GraphQL `errors`, with `x-ratelimit-remaining: 0` | primary | that pool until `x-ratelimit-reset` + 1 s |
    | 403/429 with `retry-after`, or a body containing `secondary rate limit` | secondary | both pools for `retry-after`, else 60 s |

  - **Requests**: `get` and `query` consult the gate first and answer locally while blocked. The retry loop of T041 skips primary limits.
  - **Logging**: the client has no logger and logs nothing. The limit error reaches the user through the existing debug logging of field failures in `cli` and `scan`, and through the one-shot error line.

### watch and scan (test → implementation pairs, parallel with the github chain)

- [X] T058 [P] [US4] Test the longer retry delay in `internal/watch/backoff_test.go`: a `RetryAfter` of 50 min gives a 50 min wait (it was capped at 5 min), and 2 h is capped at `watch.MaxRetryAfter == time.Hour` (research G8, contract change 8).
- [X] T059 [US4] Set `MaxRetryAfter = time.Hour` in `internal/watch/backoff.go`, with a doc comment that GitHub's reset can be up to an hour away. Makes T058 pass.
- [X] T060 [P] [US4] Test the rate-limit latch in `internal/scan/fetch_test.go` (after T052; fails until T057 and T061):
  - **Primary limit**: 6 repositories on a `githubtest` host with everything `RateLimited`:
    - every row has `FailureNetwork` and carries the same error value, which wraps `forge.ErrRateLimit`;
    - the server receives at most `scan.ReposPerHost` lookups;
    - `scan.Latched(rows)` gives one `HostFailure` with `Rows == 6`, whose `Line()` ends `(6 repositories)`.
  - **Secondary limit**: does not latch; the rows are fetched independently.
- [X] T061 [US4] Add `forge.ErrRateLimit` to `latches` in `internal/scan/fetch.go`, and update the doc comment of `HostFailure` in `internal/scan/outcome.go`: a host that rejected the token, whose token could not be read, or whose allowance is exhausted. Makes T060 pass.

### cli: wiring (makes T051 pass)

- [X] T062 [US4] Set `Revalidate: true` in the `forge.Options` built by `newRunner` in `internal/cli/run.go` (single and watch mode only; scan keeps `false` from T045). With T057 and T059, this makes T051 pass.

**Checkpoint**:
- A one-hour watch stays within 2,500 billed calls per pool.
- An exhausted allowance makes watch stale, one-shot exit 5, and a scan latch the host.
- No limit is ever reported as an authentication error.

---

## Phase 7: User Story 5 - Use a self-hosted GitHub Enterprise Server (Priority: P5)

**Goal**: a `hosts` entry with `platform: github` makes vigie work in the clones of that
instance, in single and scan mode. Enterprise tokens never mix with github.com or GitLab
tokens, and old config files are unchanged (FR-002, FR-005, FR-006, SC-004).

**Independent Test**: quickstart.md "US5"; automated with `githubtest` as the Enterprise
Server.

Most of US5's code landed in Phase 2 (T017–T020), because every story's harness needs it.
This phase proves the story end to end and documents it. Apart from the help assertion, T063
may pass at once; that is expected, since the behaviours were test-driven in T015–T019.

### E2E test for User Story 5 (write first) ⚠️

- [X] T063 [P] [US5] E2E tests for US5 in new `cmd/vigie/github_tokens_test.go`.
  - **Servers**: `gitlabtest` for `gitlab.test`; `githubtest` A for `ghe-a.test` (`platform: github`, `insecure: true`, no token source); `githubtest` B for `ghe-b.test` (`platform: github`, `token_env: GHE_B_TOKEN`).
  - **Env**: `GITLAB_TOKEN=gl-e2e`, `GH_TOKEN=gh-e2e`, `GH_ENTERPRISE_TOKEN=ghe-e2e`, `GHE_B_TOKEN=gheb-e2e`.
  - **Token separation** (SC-004): run single mode in a clone of each server, then scan a folder holding all three.
    - `gitlabtest` saw only `gl-e2e`;
    - A's `Tokens()` is only `Bearer ghe-e2e`, and B's is only `Bearer gheb-e2e`;
    - no server ever saw `gh-e2e`;
    - no token value appears in any stdout or stderr.
  - **Missing Enterprise token**: in the A clone with `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` removed and `GH_TOKEN` still set:
    - stderr is exactly `GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not set: create a token with read access to the repository and export it, or set token_env or token_command for ghe-a.test in the config file`;
    - exit 3; A received no request (US5-3).
  - **Scan rows**: in a scan, A and B clones get rows, not `unknown host` (US5-2).
  - **Old config**: a config file without any `platform` key, the 001 `newE2E` one, gives a line byte-identical to `TestE2EStatusLine`'s expectation (US5-4).
  - **Help**: in `TestE2EHelp`, require `platform: github` and `GitHub Enterprise Server (default: gitlab)`.
- [X] T064 [US5] Add the `Config file:` sample with `platform: github      # GitHub Enterprise Server (default: gitlab)` to `internal/cli/help.txt`, per contracts/cli.md. This makes T063 pass.

**Checkpoint**: Enterprise Server works through one config entry, with its own token chain.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: documentation, the unchanged-GitLab check, the quality gates and the release
note.

- [X] T065 [P] Update `README.md` per research G14:
  - an intro naming both platforms;
  - the field mapping table of spec.md;
  - a GitHub quick start with a fine-grained token and its read permissions (Metadata, Contents, Actions, Issues, Pull requests); classic tokens need `repo` for private repositories and offer no read-only private scope;
  - the `token_command: ["gh", "auth", "token"]` recipe, with the Enterprise `--hostname` variant;
  - the Enterprise Server config entry, and the token precedence per kind of host;
  - the status-bar advice: on GitHub, use `vigie -w -o json` rather than polling one-shot runs (G7);
  - `PLATFORM` and 🦊/🐙 in the examples;
  - the v0.3.0 contract changes (contracts/cli.md, items 1–8);
  - `*.ghe.com` (data residency) out of scope;
  - `--timeout` bounds the project lookup and each refresh separately, so a one-shot run takes at most about twice `--timeout`.
- [X] T066 [P] Sweep the doc comments of `internal/config/host.go`, `internal/scan/doc.go`, `internal/scan/fetch.go`, `internal/scan/select.go`, `internal/cli/doc.go` and `internal/cli/run.go`: "GitLab instance/project" becomes "GitLab or GitHub instance/repository" where both now apply. No behaviour change; `task lint` passes.
- [X] T067 SC-008 review: `git diff main -- '*.golden'` and `git diff main -- cmd/vigie/*_test.go internal/*/*_test.go`. Confirm that every change to a GitLab expectation is one of the three allowed markers (🦊 instead of 🗼 with emoji, the `PLATFORM` column, the JSON `platform` property), plus the forge renames of Phase 2, the 002 unknown-host fixtures moved from github.com to bitbucket.org (T019), and the `no GitLab or GitHub repository found` text (T043). Any other diff is a regression to fix before merging.
- [X] T068 Run every gate in quickstart.md "Gates":
  - `task lint`, including depguard: `net/http` only in `forge`, `gitlab` and `github`;
  - `task test` (`go test -count=2 -race ./...`);
  - `go generate ./... && git diff --exit-code`;
  - `CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie`;
  - `diff docs/status.schema.json specs/003-github-support/contracts/status.schema.json`.

  Confirm that `go.mod` gained no direct dependency (plan "Technical Context").
- [ ] T069 Run quickstart.md "Scenarios" by hand against github.com and gitlab.com on `~/gh-demo`, including:
  - the observed facts (runs listed newest first, G6; a comma `status` list returns 0, G5; a `pull_request` run's `head_branch`, G4);
  - the 10-minute `gh api rate_limit` measurement for US4;
  - SC-002, a one-shot line in under 2 s on `cli/cli`.

  Record any mismatch as a spec or research amendment before release.
- [X] T070 Write the commit message for the feature. It lists the eight contract changes of contracts/cli.md as constitution gate 7 requires, plus the GitLab-visible changes listed in Notes, and names the release `v0.3.0` (research G11).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup and blocks every story.
- **US1 (Phase 3)**: after Foundational. It provides the `github` client and `githubtest`, which US2, US3 (GitHub records), US4 and US5 all use.
- **US2 (Phase 4)**: after US1 (needs `github.NewClient` and the fixture).
- **US3 (Phase 5)**: after US1. Its scan JSON assertions (T047) use the US2 mixed folder, so run it after US2, or drop the mixed-folder part until US2 lands.
- **US4 (Phase 6)**: after US1. Its scan latch (T060, T061) and scan e2e need US2's `Fetch` switch (T045).
- **US5 (Phase 7)**: after US1. Its scan check needs US2.
- **Polish (Phase 8)**: after every story included in the release.

### Within each story

E2E test first (it fails), then per-package test → implementation pairs, then the `cli`
wiring, which makes the e2e test pass.

These files are edited by several tasks, which therefore run in sequence across stories:

| File | Tasks |
|---|---|
| `internal/cli/run.go` | T012, T033, T062 |
| `internal/cli/help.txt` | T034, T046, T064 |
| `cmd/vigie/main_test.go` (`TestE2EHelp`) | T021, T035, T063 |
| `cmd/vigie/github_test.go` | T021, T047 |
| `internal/scan/fetch.go` | T010, T045, T061 |
| `internal/github/export_test.go` | T023, T027, T040, T053, T056 |
| `internal/github/githubtest/server.go` | T022, T052 |
| `cmd/vigie/exitcode.go` | T012, T043 |

### Key task dependencies

- **Phase 2**: T004 → T005 → T006 → T007 → T008 → {T009 → T010, T011 → T012}. T013 → T014 and T015 → T016 run in parallel with them. T017 needs T014 and T016; then T018 → T019 → T020.
- **US1**:
  - T023 needs T022;
  - the `github` chain T023 → T030 runs strictly in order (one test binary);
  - T031 → T032 in parallel with it;
  - T033 needs T030 and T032; T034 follows T033.
- **US2**:
  - T043 needs T037;
  - T044 needs T030 and T043;
  - T040 → T041 run in parallel with the scan chain, since they are another package;
  - T046 last.
- **US3**: T047 before T048–T050; T050 needs T049.
- **US4**:
  - T053 needs T052, and T055 makes both T053 and T054 pass;
  - the github chain T052 → T057 runs strictly in order;
  - T058 → T059 run in parallel;
  - T060 can be written after T052 but passes only after T057 and T061;
  - T062 last.
  - If US2 and US4 overlap, run the US4 `scan` pair (T060, T061) before T044 or after T045, never in between: an open `scan` test stops that package's test binary from compiling.
- **US5**: T063 → T064.

### Parallel Opportunities

- **Phase 2**: three chains at once: forge → gitlab → consumers; status (T013–T014); token (T015–T016). Then config.
- **US1**: e2e (T021), fake server then github chain (T022 → T030) and render (T031–T032) in parallel; then T033 → T034.
- **US2**: e2e (T035), token (T036–T037), render (T038–T039), github retry (T040–T041) and scan (T042 → T045) chains in parallel; then T046.
- **US3**: T048 and T049 in parallel after T047.
- **US4**: e2e (T051), the github chain (T052 → T057), watch (T058–T059) and scan (T060–T061) in parallel; then T062.
- **US5**: can run in parallel with US3 and US4 once US2 is done. It touches only its own e2e file, `cmd/vigie/main_test.go` and `internal/cli/help.txt`.
- **Polish**: T065 and T066 in parallel.

## Parallel Example: Phase 2

```bash
# Three chains at once:
Task: "T003 Test forge sentinels/RefreshError/RetryAfter in internal/forge/refresh_test.go, then T004 … T012 in order"
Task: "T013 Test status.Platform in internal/status/platform_test.go, then T014"
Task: "T015 Test token Vars, hints and Mask in internal/token/resolve_test.go, then T016"
# Then: T017 → T020 (config), needing T014 and T016
```

## Parallel Example: User Story 1

```bash
Task: "T021 E2E tests for US1 in cmd/vigie/github_test.go"
Task: "T022 Fake GitHub in internal/github/githubtest/server.go, then T023 … T030 in order"
Task: "T031 Platform icon goldens in internal/render/text_test.go, then T032"
```

## Parallel Example: User Story 4

```bash
Task: "T051 E2E tests for US4 in cmd/vigie/github_limits_test.go"
Task: "T052 githubtest rate limits/ETag/billing, then T053 … T057 in order"
Task: "T058 Test watch.MaxRetryAfter in internal/watch/backoff_test.go, then T059"
Task: "T060 Test the rate-limit latch in internal/scan/fetch_test.go, then T061 (after T057)"
```

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup, then Phase 2 Foundational. Stop and check that `task test` is green with
   GitLab unchanged: this is the riskiest refactor.
2. Phase 3 (US1). **Stop and validate** with quickstart.md "US1" on a real github.com clone.
3. Limits of the MVP until later stories land:
   - scans still skip GitHub clones as `unknown host` (US2);
   - JSON has no `platform` (US3);
   - an exhausted allowance shows as `denied` with exit 3 (US4);
   - the Enterprise Server docs are incomplete (US5).

### Incremental Delivery

US1 (GitHub line) → US2 (mixed table) → US3 (JSON platform) → US4 (allowance) → US5
(Enterprise proof and docs) → Polish. Each story opens with its e2e test and closes when the
`cli` wiring makes it pass. Release v0.3.0 only after US3 and US4: the JSON property and the
table column are contract changes best shipped together, and US4 removes the MVP's wrong exit
code for an exhausted allowance.

## Notes

- Commit after each test and implementation pair; a behaviour change lands with the test
  that fails without it.
- No new direct dependency: stdlib, `golang.org/x/term` and `go.yaml.in/yaml/v3` only.
  GraphQL is plain `net/http` + `encoding/json`.
- Goldens (`scan_mixed.golden`, the updated `scan_table.golden`, `table_*.golden`,
  `json_*.golden`, `text_*.golden`) are written by hand from the contract before the code
  exists. `-update` is only for later, reviewed changes.
- **Additions to data-model**, decided while generating these tasks:
  - `token.Source.Host` (T016), because the Enterprise missing-token message names the host;
  - `config.ResolveHost` returns `Endpoint{Platform}` together with `ErrInsecureHost` (T020), so a failed scan row still shows its platform;
  - `github` `export_test.go` gains `WithRoundTripper` (T053) for `testing/synctest`;
  - `config` rejects two `hosts` keys that name the same host (T017, T018), and an `api`-host match shared by several entries (T019, T020). github.com-family hosts match an entry by key only.
- **Changes visible on GitLab** besides the contract changes (note them in the commit
  message, T070):
  - a stalled project lookup now ends with exit 5 after `--timeout` instead of hanging (G16);
  - a config with two keys for the same host, or with several entries sharing an `api` host and no entry keyed by it, now fails with exit 2. Today one of them is picked at random;
  - a `GITLAB_HOST` naming github.com now fails with exit 2 (FR-003).

  spec.md FR-026 and contracts/cli.md ("Other changes visible on GitLab") list the same items.
