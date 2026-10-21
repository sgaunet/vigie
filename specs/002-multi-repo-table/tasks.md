---

description: "Task list for vigie — Multi-Repository Status Table"
---

# Tasks: vigie — Multi-Repository Status Table

**Input**: Design documents from `specs/002-multi-repo-table/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/cli.md](./contracts/cli.md),
[contracts/status.schema.json](./contracts/status.schema.json), [quickstart.md](./quickstart.md)

**Tests**: MANDATORY (constitution Principle III, TDD). Each test task is written first and
must **fail** (a compile failure counts) before its implementation task starts. Two rules keep
that checkable in Go:

- **Pairs per package**: every `_test.go` file of a package compiles into one test binary, so
  within a package a test task is followed by the implementation that makes it pass, and the
  next test of that package starts only after that. Pairs in different packages run in
  parallel.
- **E2E first**: each story's e2e task (built binary, plus the `--help` assertions) opens its
  phase and fails; the `cli` wiring at the end of the phase makes it pass.

All tests are black box (`package <pkg>_test`). Internals go through `export_test.go` only
where research S6 and S12 allow it (`scan.slots`, the `gitlab` backoff base). `task test` =
`go test -count=2 -race ./...`.

**Organization**: grouped by user story (US1–US4 from spec.md).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: user story the task belongs to (US1…US4)
- Research decisions are cited as S1–S14 ([research.md](./research.md)), feature 001 decisions
  as R1–R12; requirements as FR-xxx and SC-xxx ([spec.md](./spec.md)).

## Path Conventions

Go CLI layout from plan.md: `cmd/vigie/` (main, exit codes, e2e tests), `internal/<domain>/`
(logic + black-box tests + `testdata/`). Module `github.com/sgaunet/vigie`. The new domain
package is `internal/scan`; it imports `config`, `repo`, `token`, `gitlab` and `status`, and
only `cli` and `cmd/vigie` import it.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: a clean baseline and the empty `scan` package.

- [X] T001 Run `task lint` and `task test` (`Taskfile.yml`) on the current tree and confirm both pass, so every later failure is attributable to this feature
- [X] T002 Create `internal/scan/doc.go` with the package comment `// Package scan finds the git clones below a folder, keeps those on a known GitLab host, and fetches their status concurrently with a bound per host.`; confirm `go build ./...` and `task lint` pass (depguard: `scan` must never import `net/http`, `os/exec` or `flag`)

**Checkpoint**: the module builds with an empty `scan` package.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the small changes in existing packages that every story builds on: the depth
setting, telling "git missing" from "not a repository", the scan row type, the shared
row-failure rule, and a fake server holding several projects.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete. The four package
pairs (config, repo, status, gitlab) run in parallel.

- [X] T003 [P] Test the depth setting in `internal/config/settings_test.go`: `config.Load` with `Overrides.Depth == nil` gives `Settings.Depth == config.DefaultDepth` (4); `Overrides.Depth = &2` gives 2; `&0` and `&-1` give an error wrapping `config.ErrInvalid` whose message is exactly `invalid --depth 0: minimum is 1` (resp. `-1`) (data-model "config", FR-017)
- [X] T004 Add `Depth *int` to `config.Overrides`, `Depth int` to `config.Settings`, `DefaultDepth = 4` to the defaults block, the default in `Load`, the override in `applyOverrides`, and the check `s.Depth < 1` → `fmt.Errorf("%w --depth %d: minimum is 1", ErrInvalid, s.Depth)` in `validate`, all in `internal/config/settings.go` (makes T003 pass). No config-file key and no environment variable; the check runs in every mode (FR-017)
- [X] T005 [P] Test `repo.ErrNoGit` in `internal/repo/checkout_test.go`: with `t.Setenv("PATH", t.TempDir())` (no `t.Parallel`), `repo.Detect` returns an error for which both `errors.Is(err, repo.ErrNoGit)` and `errors.Is(err, repo.ErrNotRepository)` hold and whose message is still `not a git repository: git is not installed, install it or pass --project and --host`; with git on PATH, a non-repository temp dir gives `ErrNotRepository` but **not** `ErrNoGit` (research S1)
- [X] T006 Add `ErrNoGit = errors.New("not installed")` to `internal/repo/errors.go` and make `notRepository` in `internal/repo/checkout.go` return `fmt.Errorf("%w: git is %w, install it or pass --project and --host", ErrNotRepository, ErrNoGit)` when `errors.Is(err, exec.ErrNotFound)`, so the message is byte-identical to today (makes T005 pass; exit code stays 2 through `ErrNotRepository`)
- [X] T007 [P] Test `status.Row` and `status.Failure` in new `internal/status/row_test.go`: `Row{}.OK()` is true; `Row{Failure: f}.OK()` is false for each of `FailureAuth`, `FailureNotFound`, `FailureNetwork`, `FailureError`; their string values are exactly `auth`, `not found`, `network`, `error` (data-model "status")
- [X] T008 Implement `type Failure string` with constants `FailureAuth = "auth"`, `FailureNotFound = "not found"`, `FailureNetwork = "network"`, `FailureError = "error"`, and `type Row struct { Dir string; Snapshot Snapshot; Failure Failure; Err error }` with `func (r Row) OK() bool { return r.Failure == "" }` in new `internal/status/row.go`, with doc comments taken from data-model "status" (makes T007 pass; `status` still imports no vigie package)
- [X] T009 [P] Test `gitlab.RefreshError(fields []status.FieldResult) error` in new `internal/gitlab/refresh_test.go`: any field with an error wrapping `gitlab.ErrUnauthorized` → that error (even if other fields succeeded); every field failing with `gitlab.ErrNetwork` → the first field's error; one success among network failures → nil; network plus `status.ErrUnavailable` → nil; an empty slice → nil (S7; same rule as today's `cli.runner.fatal`)
- [X] T010 Move `cli.runner.fatal` to `gitlab.RefreshError` in new `internal/gitlab/refresh.go` (doc comment: the error that makes a whole refresh fail: a rejected token, or a network error on every field); make `runner.once` in `internal/cli/run.go` call `gitlab.RefreshError(fields)` and delete `runner.fatal` (makes T009 pass; every 001 test in `cmd/vigie` stays green)
- [X] T011 Add `TestProjectSeveralOnOneServer` to `internal/gitlab/project_test.go` (after T010: same test binary as T009): a server started with `group/a` (id 1) plus `AddProject(group/b, id 2)` finds both by path and by ID, and an unknown path still gives `gitlab.ErrProjectNotFound`
- [X] T012 Add `func (s *Server) AddProject(p Project)` to `internal/gitlab/gitlabtest/server.go`: the server then answers for every added project (matched by path or numeric ID) plus the one given to `New`; `SetProject` still replaces the first one; `Requests()` and `MaxInFlight()` span all projects (makes T011 pass)

**Checkpoint**: `task test` passes; single mode behaves exactly as before (SC-007).

---

## Phase 3: User Story 1 - See all my GitLab projects from a parent folder (Priority: P1) 🎯 MVP

**Goal**: `vigie` run in a folder that is not a clone (and without `--project`) finds the
clones below it, keeps those on a known GitLab host, and prints one aligned table row per
clone, with a summary on stderr and exit 0 (FR-001–FR-008, FR-010–FR-013a, FR-015, FR-019).

**Independent Test**: a folder holding two clones of known GitLab projects, one clone of a
non-GitLab project and one plain directory; `vigie` there prints a header and exactly the two
GitLab rows, with the counts the GitLab web UI shows (quickstart.md "US1").

### E2E test for User Story 1 (write first, must fail) ⚠️

- [X] T013 [P] [US1] E2E tests for US1 in new `cmd/vigie/scan_test.go`, plus `--help` assertions. Shared helpers for every scan e2e: `cloneAt(t, root, rel, origin, branch)` (a `gitRepo` at `root/rel`), and `newScanE2E(t)` starting two `gitlabtest` servers for `gitlab.test` and `other.test`, each project added with `AddProject` as a copy of `gitlabtest.Fixture(now)` with its own path and ID (so every row shows the fixture values), a config mapping both hosts (`api: <srv URL>`, `insecure: true`, same token source for both) and `GITLAB_TOKEN=e2eToken`; every run goes through `checkNoToken`. Main folder: `api` (gitlab.test), `team/tools/cli` (other.test), `team/web` (gitlab.test, with a nested clone `team/web/vendor/lib` that must not be listed), `.hidden/x` (not listed), `github` (origin `https://github.com/git/git.git`) and a plain directory → stdout equals `cmd/vigie/testdata/scan_table.golden`, **written by hand** from contracts/cli.md "Text output" and the fixture values (non-TTY: ASCII, no color, header plus three rows in tree order; an `-update` flag may rewrite it later), stderr is exactly `4 repositories found: 3 listed, 1 skipped (1 unknown host); run with -v to list them\n`, exit 0; two runs give identical stdout (SC-006); `-q` drops the summary line; `-v` prints one debug line per skipped clone. Each of the following in its own folder: clones on `github.com` and `unknown.test` only, with a recording proxy (`httptest.Server` counting every request, `CONNECT` included) as `HTTPS_PROXY` and `HTTP_PROXY` → zero proxy requests (SC-003; loopback hosts bypass the proxy), `no GitLab repository found: …`, exit 2; an empty folder → stdout empty, the one-line `not a git repository: no clone found below it either (depth 4); …` message, exit 2 (the 001 `TestE2ENotARepository` still passes unchanged); two clones of the same project → two rows; a `git worktree add` worktree of a clone → its own row; a server delaying every answer 5 s and `SIGTERM` sent after 300 ms → stdout empty, exit 1 (Principle VI). Mode checks: `-w` in the main folder → exit 2 with the `--watch needs a single repository` hint and zero requests on both servers (FR-019); inside the `newE2E` clone with a nested clone below it → the 001 single line, unchanged (US1-4); `-p group/app --host <srv URL>` in the main folder → the single line, no scan (US1-5). In `TestE2EHelp` in `cmd/vigie/main_test.go`, require `Folder of clones:`, `or folder to scan` and `per repository when scanning`

### config: host recognition (test → implementation)

- [X] T014 [P] [US1] Test `(config.Settings) KnownHost(remoteHost string) bool` in `internal/config/host_test.go` with `Hosts{"ssh.gitlab.example.com": {API: https://gitlab.example.com}}` and `FallbackHost https://git.corp:8443`: true for `gitlab.com`, `GitLab.com`, `gitlab.com:22`, `ssh.gitlab.example.com`, `gitlab.example.com` (host of a profile `api`) and `git.corp` (host of `GITLAB_HOST`); false for `github.com`, `gitlab.evil.com` and `gitlab.example.org` (no name heuristic, FR-005, S4)
- [X] T015 [US1] Implement `func (s Settings) KnownHost(remoteHost string) bool` in `internal/config/host.go`: `hostKey(remoteHost)` is `gitlab.com`, or `s.Profile(remoteHost)` finds a profile (key or `api` host), or `s.FallbackHost != nil && hostKey(s.FallbackHost.Host) == key` (makes T014 pass)

### render: table and progress counter (test → implementation pairs, in order)

- [X] T016 [P] [US1] Golden tests for `render.Table(rows []status.Row, fields []status.FieldName, st render.Style, now time.Time) string` in new `internal/render/table_test.go`, goldens `internal/render/testdata/table_*.golden` written by hand from contracts/cli.md "Text output" (reuse the `-update` flag and `golden` helper of `text_test.go` for later changes): the four-row example in emoji + color, emoji + no color and ASCII + no color; header `DIR  PROJECT` then the `--fields` names in upper case, bold with color; a `--fields last,mrs` subset keeps that order; a failed row of each class shows `⚠ <class>` (emoji) or `! <class>` (ASCII) in yellow in place of the field cells, unpadded; `–` for an unavailable field and `10k+` for a capped count; the ` archived` tag in the PROJECT cell; with `Width` set below the table width, PROJECT is cut from the left (`…/sub/app`) first, then DIR, other columns never; columns are separated by two spaces and padded to the widest cell measured with `render.CellWidth` (emoji count 2); no line has trailing spaces; every line ends with `\n`
- [X] T017 [US1] Implement `render.Table` in new `internal/render/table.go`: first extract the value part of each field segment in `internal/render/text.go` into helpers returning the value text and its `sgr` (branches `12/42`, pipelines `2r/1p`, last `<status icon or label> <ref> <age>`, counts `5`, `–` when unavailable), and make `Text` call them so its output is byte-identical (every `text_*.golden` and `width_*.golden` passes without `-update`); then build the header and cells, pad with `cellWidth`, leave the last cell of each line unpadded, render failed rows as `⚠ <failure>` / `! <failure>` in `yellow`, and when `st.Width > 0` cut PROJECT then DIR with `truncateLeft` (makes T016 pass; research S10)
- [X] T018 [US1] Test `render.Progress` in new `internal/render/progress_test.go`: `render.NewProgress(&buf, false)` then `Update(12, 40)` writes exactly `"\r\x1b[2K · 12/40"`; with color the ` · 12/40` part is wrapped in the dim SGR of `ansi.go`; `Clear()` writes `"\r\x1b[2K"` after a draw and nothing when nothing was drawn; a failing writer makes `Update` return its error; `render.ShowProgress(stdoutTTY, stderrTTY, quiet, verbose bool) bool` is true for `(true, true, false, false)` and false for each of the other 15 combinations (FR-013a, constitution Principle II, S9)
- [X] T019 [US1] Implement `render.ShowProgress` and `render.Progress` with `NewProgress(w io.Writer, color bool) *Progress`, `Update(done, total int) error` and `Clear() error` in new `internal/render/progress.go` (makes T018 pass)

### scan: the pipeline (test → implementation pairs, in this order)

- [X] T020 [P] [US1] Test the summary in new `internal/scan/summary_test.go`: `scan.Summarize(listed int, skips []scan.Skip, remote string)` counts per reason, with `Found = listed + len(skips)`; `Summary.Line()` gives `6 repositories found: 4 listed, 2 skipped (1 no remote "origin", 1 unknown host); run with -v to list them`, `4 repositories found: 4 listed` without skips, and `1 repository found: 1 listed`; `Summary.Skips()` lists reasons in the fixed order `bare`, `unreadable`, `no remote "<remote>"`, `remote URL not understood`, `unknown host`, `other host`; `Summary.Err(depth)` is nil when `Listed > 0`, wraps `repo.ErrNotRepository` with message `not a git repository: no clone found below it either (depth 4); run vigie in a clone or a folder of clones, or pass --project and --host` when `Found == 0`, and otherwise wraps `scan.ErrNoGitLab` with message `no GitLab repository found: 3 skipped (1 no remote "origin", 2 unknown host); add self-hosted hosts to the config file "hosts", or run with -v` (FR-006, S7, data-model "Summary")
- [X] T021 [US1] Declare the shared scan types: `ErrRoot = errors.New("cannot scan")` and `ErrNoGitLab = errors.New("no GitLab repository found")` in new `internal/scan/errors.go`; `type Reason string` with constants `ReasonBare`, `ReasonUnreadable`, `ReasonNoRemote`, `ReasonRemoteURL`, `ReasonUnknownHost`, `ReasonOtherHost` and `func (r Reason) Text(remote string) string`, `type Skip struct { Rel string; Reason Reason; Err error }`, `type Summary struct { Found, Listed int; Skipped map[Reason]int; Remote string }`, `Summarize`, `Summary.Skips`, `Summary.Line` and `Summary.Err` in new `internal/scan/outcome.go` (makes T020 pass; one formatter feeds every message that carries skip counts)
- [X] T022 [US1] Test `scan.Find(ctx, root string, depth int) ([]scan.Candidate, []scan.Skip, error)` in new `internal/scan/find_test.go`, on `t.TempDir()` trees with fake `.git` entries (no git needed): clones at depths 1, 2, 4 and 5 with `depth` 4 → depths 1, 2, 4 only, and with `depth` 1 → depth 1 only; a `.git` **file** (worktree, submodule) counts as a clone; a clone `a` holding `a/sub/.git` yields only `a` (no descent into a found repository); `.cache/x/.git` is neither listed nor skipped, but a root that is itself named `.hidden` is scanned; a symlink `link → clone` is not listed and a loop `loop → .` terminates; `b.git` holding a `HEAD` file plus `objects/` and `refs/` directories → `Skip{Rel: "b.git", Reason: scan.ReasonBare}` and nothing below it is listed; a `chmod 000` directory → `Skip{Reason: scan.ReasonUnreadable}` and the walk continues (skip this case on Windows and when `os.Getuid() == 0`); clones `a-b`, `a/x`, `b` come back as `a/x`, `a-b`, `b` (tree order, FR-013, S2); `Rel` is `/`-separated and `Path` is absolute; a root reached through a symlink to a directory is scanned; a missing root and a root that is a regular file → error wrapping `scan.ErrRoot` with message `cannot scan <root>: check --dir`; a cancelled context → error wrapping `context.Canceled`
- [X] T023 [US1] Implement `type Candidate struct { Path, Rel string }` and `scan.Find` in new `internal/scan/find.go`: resolve the root with `filepath.Abs` then `filepath.EvalSymlinks`, and return `fmt.Errorf("%w %s: check --dir", ErrRoot, root)` when that fails or the root is not a directory; `filepath.WalkDir` from it; for every directory below the root: check `ctx.Err()` first; a name starting with `.` → `SkipDir`; `os.Lstat(<dir>/.git)` succeeds → candidate, `SkipDir`; `HEAD` file plus `objects/` and `refs/` directories → `Skip{ReasonBare}`, `SkipDir`; a walk error → `Skip{ReasonUnreadable, Err}` and continue; at level `depth` (root 0, children 1) probe but return `SkipDir`; non-directory entries, symlinks included, are ignored; `Rel` through `filepath.ToSlash` (makes T022 pass; research S2)
- [X] T024 [US1] Test `scan.Inspect(ctx, cands []scan.Candidate, remote string, log *slog.Logger) ([]scan.Inspected, []scan.Skip)` in new `internal/scan/inspect_test.go` with real `git init` repos (helper isolated from the user's git config with `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`; `t.Skip` when git is absent): a clone on branch `main` with `origin` `git@gitlab.test:group/app.git` → `Inspected{Rel, Checkout}` with `Branch "main"`, `Remote.Host "gitlab.test"`, `Remote.Path "group/app"`; detached HEAD → `Branch ""`; no `origin` → `Skip` with `scan.ReasonNoRemote`; remote `file:///x` → `scan.ReasonRemoteURL`; a candidate git refuses (an empty `.git` directory) → `scan.ReasonUnreadable` and `Skip.Err` wraps `repo.ErrNotRepository`; `remote` `upstream` is honoured; a worktree made with `git worktree add` is inspected with its own branch; 12 candidates come back in input order (8 workers, S3)
- [X] T025 [US1] Implement `type Inspected struct { Rel string; Checkout repo.Checkout }` and `scan.Inspect` in new `internal/scan/inspect.go`: `const inspectWorkers = 8`; a `sync.WaitGroup` with a buffered-channel semaphore writing into a results slice indexed by input position; `repo.Detect(ctx, c.Path, remote, log)`; `repo.ErrRemoteMissing` → `ReasonNoRemote`, `repo.ErrRemoteURL` → `ReasonRemoteURL`, any other error (`repo.ErrNotRepository`) → `ReasonUnreadable`, each with `Err` set (makes T024 pass; research S3)
- [X] T026 [US1] Test `scan.Select(in []scan.Inspected, s config.Settings) ([]scan.Repo, []scan.Skip)` without `--host` in new `internal/scan/select_test.go` (pure table test, no git): with the settings of T014 plus `Hosts{"plain.test": {API: http://plain.test}}` (not insecure), remotes on `gitlab.com`, `ssh.gitlab.example.com`, `git.corp`, `github.com`, `gitlab.evil.com` and `plain.test` give, in input order: `gitlab.com` listed with `Endpoint.API https://gitlab.com`; `ssh.gitlab.example.com` listed with `https://gitlab.example.com`; `git.corp` listed; `github.com` and `gitlab.evil.com` skipped with `scan.ReasonUnknownHost`; `plain.test` listed with `Repo.Err` wrapping `config.ErrInsecureHost` (it gets a failed row, no request)
- [X] T027 [US1] Implement `type Repo struct { Rel string; Checkout repo.Checkout; Endpoint config.Endpoint; Err error }` and `scan.Select` in new `internal/scan/select.go` (depends on T015): keep input order; `s.KnownHost(co.Remote.Host)` false → `Skip{ReasonUnknownHost}`; otherwise `config.ResolveHost(s, co.Remote.Host)` gives `Endpoint`, or `Err` when it fails; pure, no I/O. The `--host` filter is added by T064 (US4) (makes T026 pass)
- [X] T028 [US1] Test the per-host semaphore in new `internal/scan/slots_test.go`, through new `internal/scan/export_test.go` exposing `var NewSlots = newSlots` and `func (s *slots) Limit() int`: inside `testing/synctest`, with `NewSlots(2)` two `Acquire(ctx)` calls return at once, a third blocks (`synctest.Wait`, still blocked) until one `Release()`; `Acquire` on a cancelled context returns an error wrapping `context.Canceled` and takes no slot
- [X] T029 [US1] Implement `type slots struct` (mutex, `limit`, `inUse`, a wake channel closed and replaced on every `Release`) with `newSlots(n int) *slots`, `Acquire(ctx context.Context) error` (waits on the wake channel or `ctx.Done()`, returns `fmt.Errorf("wait for a free slot: %w", ctx.Err())`) and `Release()` in new `internal/scan/slots.go` (makes T028 pass; `Halve` comes in T044)
- [X] T030 [US1] Test `scan.Classify(err error) status.Failure` in new `internal/scan/classify_test.go`: `token.ErrMissing`, `token.ErrCommand`, `gitlab.ErrUnauthorized`, `gitlab.ErrForbidden` → `FailureAuth`; `gitlab.ErrProjectNotFound` → `FailureNotFound`; `gitlab.ErrNetwork` → `FailureNetwork`; `config.ErrInsecureHost`, `gitlab.ErrInsecure` and `errors.New("boom")` → `FailureError`; each sentinel wrapped twice with `%w` classifies the same; `nil` → `""` (S7)
- [X] T031 [US1] Implement `scan.Classify` with `errors.Is` over the table of data-model "Failure" in new `internal/scan/fetch.go` (makes T030 pass)
- [X] T032 [US1] Test `scan.Fetch(ctx, repos []scan.Repo, cfg scan.FetchConfig) []status.Row` in new `internal/scan/fetch_test.go` with two `gitlabtest` servers acting as hosts A and B (`Endpoint{API: srv URL, Insecure: true, Token: token.Source{Env: "TOK_A"}}`, `cfg.Env` holding both tokens, projects added with `AddProject`): 10 repos on A with a 50 ms `Delay` on `EndpointProject` → `A.MaxInFlight(EndpointProject) <= scan.ReposPerHost` (4); host isolation: 8 repos on A with every endpoint delayed 300 ms and 1 repo on B answering at once → B's row `FetchedAt` is before the earliest `FetchedAt` of A's rows (relative order, no wall-clock threshold; FR-008); rows come back in input order whatever the completion order; the `last` request uses `ref=<checkout branch>`, and the project's default branch when `Branch == ""`; each request carries its own host's token; with `cfg.Timeout` 200 ms and one project delayed 2 s, that row has `Failure == FailureNetwork`, the others are OK, and `Fetch` returns in under 1 s (deadline per repository); a `Repo` with `Err` set → `FailureError`, `Row.Err` is that error, and no request mentions its path; a 404 project → `FailureNotFound` with `Snapshot.Project.Path` = the remote path, `Snapshot.Project.WebURL` = API base + `/` + path, `Snapshot.Branch` = the checkout branch, `Snapshot.Fields` = the requested names with no values, `FetchedAt` set; `cfg.OnDone` is called `len(repos)` times with `done` 1, 2, … and `total == len(repos)`; a token command (a script that appends a line to a counter file and prints a token; skipped on Windows) runs exactly once for 3 repos on one host; a field answering 403 shows as unavailable and a logger at Warn level records nothing (field failures are debug in scan mode, S8)
- [X] T033 [US1] Implement `const ReposPerHost = 4`, `type FetchConfig struct { Fields []status.FieldName; Timeout time.Duration; ActiveDays int; Env map[string]string; Log *slog.Logger; OnDone func(done, total int) }` and `scan.Fetch` in `internal/scan/fetch.go`: group repos into hosts keyed by `Endpoint.API.String()` plus the token source; each host holds `sync.Once`, `*gitlab.Client`, `initErr` (from `token.Resolve` or `gitlab.NewClient` with `gitlab.Options{Insecure, ExtraRootsPEM}`) and `slots` of `ReposPerHost`; one goroutine per repo: `Repo.Err` set → failed row without any request; else acquire a slot, derive `context.WithTimeout(ctx, cfg.Timeout)` covering lookup and fields, init the host once, `client.Project(rctx, co.Remote.Path)`, branch = `co.Branch` or `p.DefaultBranch`, `status.Collect(rctx, client.ForProject(p.ID), status.Request{Fields, Branch, ActiveSince: now.AddDate(0, 0, -ActiveDays), Timeout})`, `gitlab.RefreshError(fields)`; log failed and unavailable fields at debug; build the `status.Row` (OK snapshot as in `runner.once`, or the failed snapshot of data-model "Row" with `Failure = Classify(err)`); release the slot; call `cfg.OnDone` under a mutex; return rows in input order (makes T032 pass; research S5)

### cmd/vigie: exit codes (test → implementation)

- [X] T034 [US1] Extend `TestExitCode` in `cmd/vigie/exitcode_test.go` (after T021, so the `cmd/vigie` test binary compiles): `scan.ErrRoot` and `scan.ErrNoGitLab`, wrapped twice, map to 2; an error wrapping both `repo.ErrNotRepository` and `repo.ErrNoGit` maps to 2
- [X] T035 [US1] Add `{scan.ErrRoot, exitSetup}` and `{scan.ErrNoGitLab, exitSetup}` to the `exitCodes` table in `cmd/vigie/exitcode.go` (makes T034 pass)

### cli: wiring (makes T013 pass)

- [X] T036 [US1] Implement `runScan(ctx, s config.Settings, std IO, log *slog.Logger) error` in new `internal/cli/scan.go` (depends on T017–T035): run `scan.Find(ctx, s.Dir, s.Depth)` in a goroutine and `select` on its result or `ctx.Done()`, so a `ReadDir` blocked on a hung mount never blocks Ctrl+C (research S2, Principle VI); then `scan.Inspect(ctx, cands, s.Remote, log)` → `scan.Select(inspected, s)`; after each stage return `fmt.Errorf("interrupted: %w", err)` if `ctx.Err() != nil`; log each skip at debug (`dir`, `reason` via `Reason.Text(s.Remote)`, `err`); `sum := scan.Summarize(len(repos), skips, s.Remote)`; return `sum.Err(s.Depth)` when non-nil; only when `render.ShowProgress(stdoutTTY, stderrTTY, s.LogLevel == slog.LevelError, s.LogLevel == slog.LevelDebug)` holds, with both TTY states from `terminal(std.Stdout)` and `terminal(std.Stderr)`, create `render.NewProgress(std.Stderr, s.Color && render.EnableVT(fd))` with a deferred `Clear` and pass `Update` as `OnDone` (FR-013a, constitution Principle II); `scan.Fetch` with `FetchConfig{s.Fields, s.Timeout, s.ActiveDays, std.Env, log, onDone}`; on interrupt return as above; `Clear`, then write `render.Table(rows, s.Fields, textStyle(s, std.Stdout), time.Now())` to stdout; then, unless `s.LogLevel == slog.LevelError` (`--quiet`), write `sum.Line()` plus `\n` to stderr directly (not through slog); return nil (research S8, S9)
- [X] T037 [US1] Pick the mode in `cli.Run` in `internal/cli/run.go` (depends on T036, T006): when `s.Project == "" || s.Host == nil`, run `repo.Detect(ctx, s.Dir, s.Remote, log)` once there and pass the checkout and its error to `newRunner` (which keeps its current switch and no longer calls `Detect` itself); when `s.Project == ""` and the error wraps `repo.ErrNotRepository` but not `repo.ErrNoGit`, switch to scan mode: with `s.Watch` return `fmt.Errorf("%w: --watch needs a single repository: run it inside a clone or pass --project", ErrUsage)` before walking anything (FR-019), else `return runScan(ctx, s, std, log)`; every other case follows feature 001 unchanged (FR-001, FR-002, research S1)
- [X] T038 [US1] Update `internal/cli/help.txt`: the `-C, --dir PATH` line becomes `directory of the git clone, or folder to scan (default .)`; the `--timeout` line becomes `time budget of one whole refresh, per repository when scanning` with `(default 10s)` on a continuation line; add the `Folder of clones:` section of contracts/cli.md "`--help` additions" after the flags (makes the help part of T013 pass; T013 passes in full)

**Checkpoint**: US1 is shippable as the MVP: a table of every recognised clone below a folder,
exit 0, the summary on stderr, and no request to any other host.

---

## Phase 4: User Story 2 - One broken repository does not hide the others (Priority: P2)

**Goal**: per-row failures stay in their row (US1 already classifies and draws them); a host
that rejects the token is contacted no further and gets one stderr line; a throttling host is
retried and slowed down without slowing the others; when every row fails, stdout is empty and
the exit code is the rows' common code (FR-008a, FR-009, FR-012, FR-016).

**Independent Test**: in a scan folder, a clone whose remote names a non-existent project on a
recognised host shows `not found` while every other row is complete, exit 0; with an invalid
token every row fails, stdout is empty and the exit code is 3 (quickstart.md "US2").

### E2E test for User Story 2 (write first, must fail) ⚠️

- [X] T039 [US2] Add `Times int` to `gitlabtest.Behavior` in `internal/gitlab/gitlabtest/server.go`: when non-zero the behaviour applies to the first `Times` requests of that endpoint only, then the endpoint answers normally (count kept per endpoint under the server mutex); zero keeps today's "always" meaning (test support for T040, T041, T047)
- [X] T040 [US2] E2E tests for US2 in new `cmd/vigie/scan_failures_test.go` (helpers from T013): one clone pointing at a missing project on `gitlab.test` → its row reads `! not found`, the other rows are complete, exit 0 (US2-1); `other.test` answering 401 → its row reads `! auth`, `gitlab.test` rows are complete, stderr holds the summary line and exactly one `token rejected by … (401): … (1 repository)` line, and with `-q` only that host line, exit 0 (US2-2); an issues endpoint answering 403 → `–` in that cell, exit 0 (US2-3); both servers answering 401 → stdout empty, one line per host, then `all 3 listed repositories failed; 1 skipped (1 unknown host); run with -v to list them`, exit 3; a 401 host plus a 404 project and nothing healthy → stdout empty, the host line, a `<dir>: project … not found …` line, exit 1 (US2-4, FR-016); `gitlab.test` answering 429 with `Retry-After: 0` for its first 2 project requests (`Behavior.Times`) → every row complete, exit 0 (SC-004a). In `TestE2EHelp` in `cmd/vigie/main_test.go`, require `When scanning:`

### gitlab: bounded retry (test → implementation)

- [X] T041 [P] [US2] Test bounded retry in new `internal/gitlab/retry_test.go`, with the backoff base set to 1 ms through new `internal/gitlab/export_test.go` (`var RetryBase = &retryBase`, same pattern as `token.CommandTimeout`): with `Options{Retry: true}` and `Behavior{Status: 429, Times: 2}` on `EndpointProject`, `Project` succeeds after 3 requests and `OnThrottle` was called twice; `Status: 503` behaves the same; `RetryAfter: "1"` makes the retry wait at least 1 s; `RetryAfter: "5"` with a 200 ms context deadline returns an error wrapping `gitlab.ErrNetwork` in well under 1 s (no wait past the deadline); `Times: 10` stops after 5 requests (4 retries) with `ErrNetwork`; 401 and 404 are never retried; `Options{Retry: false}` sends exactly 1 request (single mode unchanged); cancelling the context during a wait returns at once (research S6)
- [X] T042 [US2] Add `Retry bool` and `OnThrottle func()` to `gitlab.Options`, keep them on `Client`, and loop in `Client.get` in `internal/gitlab/client.go`: on a `429` or `5xx` answer with `Retry` set, call `OnThrottle` if non-nil, compute the wait (`retryAfter` of the answer, else `retryBase << attempt` capped at 4 s, with `var retryBase = 250 * time.Millisecond`), give up with the classified answer when `attempt == 4` or when the wait would end after `ctx.Deadline()`, else wait on a `time.NewTimer` or `ctx.Done()` and send the request again; with `Retry` false nothing changes (makes T041 pass)

### scan: slow-down, outcome, latch (test → implementation pairs, in this order)

- [X] T043 [P] [US2] Test `Halve()` in `internal/scan/slots_test.go` inside `testing/synctest`: `NewSlots(4)`, `Halve()` → `Limit() == 2`; a second `Halve()` within the same second leaves 2; after `time.Sleep(time.Second)` it gives 1, and it never goes below 1; with 4 slots in use and the limit halved to 2, the next `Acquire` waits until only 1 is in use (in-flight repositories finish, research S6)
- [X] T044 [US2] Implement `Halve()` in `internal/scan/slots.go`: under the mutex, `limit = max(1, limit/2)` unless the last halving was less than a second ago (`time.Now()`, so synctest controls it); `Acquire` admits only while `inUse < limit` (makes T043 pass)
- [X] T045 [US2] Test `scan.Outcome(rows []status.Row, sum scan.Summary) error` in new `internal/scan/outcome_test.go`: nil when any row is OK (even if all others failed); otherwise a `*scan.AllFailedError` (found with `errors.As`) holding every row's `Err` in row order and the summary; `errors.Unwrap` of it is nil; its message is `all 3 listed repositories failed; 1 skipped (1 unknown host); run with -v to list them`, `all 3 listed repositories failed` without skips, and `1 listed repository failed` for one row
- [X] T046 [US2] Implement `type AllFailedError struct { Errs []error; Skipped Summary }` with `Error()` (no `Unwrap`, research S7) in `internal/scan/errors.go` and `scan.Outcome` in `internal/scan/outcome.go` (makes T045 pass)
- [X] T047 [US2] Extend `internal/scan/fetch_test.go`: host A answers 401 on `EndpointProject` for its 10 repos → every A row has `FailureAuth`, every A row's `Err` is the **same error value**, and A received at most `ReposPerHost` project requests (the latch, FR-009); B's rows are complete; a 403 on one A project is a per-row `FailureAuth` that does not stop A's other rows; `Behavior{Status: 429, Times: 2}` on A's project endpoint → every A row ends OK (SC-004a) and a captured debug log holds one slow-down record with attributes `host` and `limit` 2; `scan.Latched(rows)` returns one `scan.HostFailure{Err, Rows: 10}` for the 401 host and, as the second result, the failed rows not covered by any host failure (the 403 and 404 rows)
- [X] T048 [US2] Add the auth latch and throttling to `internal/scan/fetch.go` (depends on T042, T044): each host gets a latched error set with compare-and-swap by the first `gitlab.ErrUnauthorized` from the lookup or from `gitlab.RefreshError`, and by `initErr` when it comes from `token.ErrMissing` or `token.ErrCommand`; after acquiring a slot a repository checks the latch and fails with the latched value without sending anything; a repository whose own request got a 401 also stores the latched value, so every row of the host carries the same error; create clients with `gitlab.Options{…, Retry: true, OnThrottle: h.throttled}` where `throttled` calls `slots.Halve()` and logs at debug `slowing down` with `host` and `limit`; add `type HostFailure struct { Err error; Rows int }` and `scan.Latched(rows []status.Row) ([]HostFailure, []status.Row)` in `internal/scan/outcome.go`, grouping failed rows whose `Err` wraps `gitlab.ErrUnauthorized`, `token.ErrMissing` or `token.ErrCommand` by identical error value, in first-appearance order (makes T047 pass)

### cmd/vigie: exit codes (test → implementation)

- [X] T049 [US2] Extend `TestExitCode` in `cmd/vigie/exitcode_test.go` (after T046): `&scan.AllFailedError{Errs: [401 error, token.ErrMissing]}` → 3; `[ErrProjectNotFound, ErrProjectNotFound]` → 4; `[ErrNetwork]` → 5; `[config.ErrInsecureHost, gitlab.ErrInsecure]` → 2; `[ErrProjectNotFound, ErrUnauthorized]` → 1; `[errors.New("boom")]` → 1; an `AllFailedError` wrapped with `%w` is still matched (research S7)
- [X] T050 [US2] Map `*scan.AllFailedError` in `exitCode` in `cmd/vigie/exitcode.go`: before the sentinel table, `errors.As` it, map every `Errs` item with `exitCode`, and return their common code, or `exitFailure` when they differ or the list is empty (makes T049 pass)

### cli: wiring (makes T040 pass)

- [X] T051 [US2] Wire failure output into `runScan` in `internal/cli/scan.go` (depends on T046, T048): after `Fetch` and the interrupt check, `hosts, others := scan.Latched(rows)`; if `scan.Outcome(rows, sum)` is non-nil, `Clear` the counter, write nothing to stdout, write one line per host failure to stderr (`<err> (<n> repositories)`, `(1 repository)` for one), then `<dir>: <err>` for every row in `others`, and return the error (main prints its line last); otherwise print the table, then the summary line (unless `--quiet`), then the host-failure lines (always, they are errors); log every failed row at debug (`dir`, `failure`, `reason`) (research S8, contracts/cli.md "Stderr in scan mode")
- [X] T052 [US2] Add `When scanning: 0 if any row was fetched; if all failed, their common code, else 1.` under `Exit codes:` in `internal/cli/help.txt` (FR-016; T040 passes in full)

**Checkpoint**: US1 and US2 work; broken rows never abort a scan, and the exit code follows
FR-016.

---

## Phase 5: User Story 3 - Feed the table to scripts (Priority: P3)

**Goal**: `-o json` in scan mode writes one schema-valid record per row, with `dir`, in table
order; piped text output stays plain (FR-014, SC-005).

**Independent Test**: `vigie -o json | jq -r 'select(.last.status=="failed") | .dir'` in a
scan folder prints the directories of the failing projects (quickstart.md "US3").

### E2E test for User Story 3 (write first, must fail) ⚠️

- [X] T053 [P] [US3] E2E tests for US3 in new `cmd/vigie/scan_json_test.go` (helpers from T013): `-o json` prints one line per listed row, each parsing with `encoding/json`, in table order, with `dir` set and only keys declared in `docs/status.schema.json` (SC-005); the failed row has `null` fields and one `refresh` error (US3-2); filtering records on `last.status == "failed"` yields exactly the failing directories (US3 independent test, done in Go instead of jq); piped text output contains no `\x1b` and no emoji, and every row is one line (US3-3); equivalence: for every row, the record without `dir` equals `vigie -C <clone> -o json` for that clone, `fetched_at` aside (research S12)

### render: JSON record (test → implementation)

- [X] T054 [P] [US3] Copy `specs/002-multi-repo-table/contracts/status.schema.json` over `docs/status.schema.json` (adds the optional `dir` property and the scan-mode description; `$id` stays v1); `diff docs/status.schema.json specs/002-multi-repo-table/contracts/status.schema.json` prints nothing, and the existing `TestJSONMatchesSchema` still passes
- [X] T055 [US3] Golden tests for `render.JSONRow(w io.Writer, r status.Row) error` in `internal/render/json_test.go` (after T054), goldens `internal/render/testdata/json_row_ok.golden` and `json_row_failed.golden` written by hand from contracts/cli.md "JSON output": an OK row is the 001 record plus `"dir"` right after `"branch"`; a failed row has `project` and `url` from the remote, `archived: false`, the checkout branch, every requested field `null`, `errors: [{"field":"refresh","reason":"<Row.Err>"}]`, `fetched_at` of the attempt and `stale: false` (research S11); `render.JSON` output never contains `"dir"` (every existing `json_*.golden` passes without `-update`); `TestJSONMatchesSchema` also validates every `json_row_*.golden` against `docs/status.schema.json`
- [X] T056 [US3] Implement `render.JSONRow` in `internal/render/json.go`: add `Dir string \`json:"dir,omitempty"\`` to `jsonRecord` right after `Branch`; factor the body of `JSON` into a record builder shared by both; for a failed row set every requested field to `jsonNull` and `Errors` to the single `refresh` item (makes T055 pass)

### cli: wiring (makes T053 pass)

- [X] T057 [US3] Add the JSON branch to `runScan` in `internal/cli/scan.go`: after `Clear`, when `s.Output == config.OutputJSON`, call `render.JSONRow(std.Stdout, row)` for every row in order instead of printing the table; the all-failed path of T051 still writes nothing to stdout

**Checkpoint**: US1–US3 work; scripts can consume a scan.

---

## Phase 6: User Story 4 - Control how deep and where vigie looks (Priority: P4)

**Goal**: `--depth N` bounds the walk, `--dir` picks the folder, and `--host` keeps only the
clones of one instance without contacting any other (FR-003, FR-005a, FR-017).

**Independent Test**: a tree with clones at depths 1, 3 and 6; `vigie --depth 3` lists only
the first two (quickstart.md "US4").

### E2E test for User Story 4 (write first, must fail) ⚠️

- [X] T058 [P] [US4] E2E tests for US4 in new `cmd/vigie/scan_options_test.go` (helpers from T013): clones at depths 1, 3 and 6 with `--depth 3` → only the first two rows (US4 independent test, US4-2); `--depth 0` → `invalid --depth 0: minimum is 1`, exit 2, also inside a clone (FR-017); `--depth x` → usage error, exit 2; `-C <root>/team` from another folder → rows relative to `team/` (US4-1); `-C /no/such/dir` → `cannot scan /no/such/dir: check --dir`, exit 2; `--host <gitlab.test server URL>` → only `gitlab.test` rows, zero requests on the `other.test` server, summary counts them as `other host` (US4-5; both profiles share one token source and `insecure: true`, because `Profile` ignores ports and both servers listen on 127.0.0.1); `--host https://gitlab.example.com` with no clone on it → `no GitLab repository found: … other host …`, exit 2; a `chmod 000` directory → still exit 0, and `-v` prints a debug line naming it with reason `unreadable` (US4-4; skipped on Windows and as root); `--depth 2 -v` inside a clone → the single line and the `--depth ignored` debug line (FR-017). In `TestE2EHelp` in `cmd/vigie/main_test.go`, require `--depth N` and `only clones on this host`

### cli, config, scan (test → implementation pairs; the three packages in parallel)

- [X] T059 [P] [US4] Extend `internal/cli/flags_test.go`: `--depth 3` and `--depth=3` set `Overrides.Depth` to 3; no `--depth` leaves it nil; `--depth 0` parses (range checked by `config`, T003); `--depth x` and `--depth 1.5` → `cli.ErrUsage`
- [X] T060 [US4] Add `--depth` in `cli.Parse` in `internal/cli/flags.go`: `fs.IntVar(&depth, "depth", 0, "")` (no short name), and `ov.Depth = &depth` when `set["depth"]` (makes T059 pass)
- [X] T061 [P] [US4] Test `(config.Settings) OnHost(remoteHost string, base *url.URL) bool` in `internal/config/host_test.go`: true when `hostKey(remoteHost) == hostKey(base.Host)` (`gitlab.example.com` vs `https://gitlab.example.com`); true for `ssh.gitlab.example.com` when its profile has `api: https://gitlab.example.com` and base is that URL; false when the profile `api` differs in scheme, port or path (`https://gitlab.example.com:8443`, `http://…`, `https://gitlab.example.com/sub`); false for `gitlab.com` against `https://gitlab.example.com` (FR-005a, S4)
- [X] T062 [US4] Implement `func (s Settings) OnHost(remoteHost string, base *url.URL) bool` in `internal/config/host.go`: key match, or `s.Profile(remoteHost)` with a non-nil `API` whose scheme, host (with port) and path equal `base`'s (makes T061 pass)
- [X] T063 [P] [US4] Extend `internal/scan/select_test.go` with `--host`: `s.Host = https://gitlab.example.com` keeps remotes on `gitlab.example.com` and `ssh.gitlab.example.com` (mapped), with `Endpoint.API` equal to `s.Host`; `gitlab.com` and `git.corp` → `Skip{ReasonOtherHost}`; a remote on the `--host` host is kept even when that host is in neither `Hosts` nor `FallbackHost`; `github.com` → `ReasonOtherHost` too (with `--host`, every non-matching remote is "other host")
- [X] T064 [US4] Add the filter to `scan.Select` in `internal/scan/select.go` (depends on T062): when `s.Host != nil`, keep a repository iff `s.OnHost(co.Remote.Host, s.Host)`, else `Skip{ReasonOtherHost}`, and skip the `KnownHost` check (makes T063 pass)

### cli: wiring (makes T058 pass)

- [X] T065 [US4] In `cli.Run` in `internal/cli/run.go`, when the single-project path is taken and `ov.Depth != nil`, log at debug `--depth ignored: not scanning` (FR-017)
- [X] T066 [US4] Update `internal/cli/help.txt`: append `when scanning, only clones on this host are listed` as a continuation line under `--host`, and add `      --depth N           when scanning, levels below --dir to search (default 4)` after it (contracts/cli.md "`--help` additions"; T058 passes in full)

**Checkpoint**: every user story works and is covered end to end.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: documentation, performance and the merge gates.

- [X] T067 [P] Add a "Folder of clones" section to `README.md`: when the scan runs (contracts/cli.md "Modes"), which clones get a row, the table and JSON examples, `--depth`, `--dir`, `--host` and `--timeout` in scan mode, when the progress counter shows, the scan exit codes, and the note that self-hosted instances must be in the config `hosts` map (or `GITLAB_HOST`, or `--host`)
- [X] T068 [P] Add `BenchmarkFind10k` to `internal/scan/find_test.go` (a tree of 10,000 directories without any repository, built once in `b.TempDir()`); run `go test -run '^$' -bench Find10k ./internal/scan` and record the result in `specs/002-multi-repo-table/checklists/smoke.md` (SC-002: under 2 s)
- [X] T069 Run the gates from quickstart.md: `task lint`, `task test`, `go generate ./... && git diff --exit-code`, `CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie`, and the schema `diff` of T054; every 001 test in `cmd/vigie` passes unchanged (SC-007); fix every finding (lint disagreements are settled in `.golangci.yml`, not with `//nolint`)
- [X] T070 Manual smoke test on `~/scan-demo` against gitlab.com following every scenario of `specs/002-multi-repo-table/quickstart.md`, including the ` · n/2` counter on a terminal and its absence with `>/dev/null`, `time vigie` under 3 s in a folder of 20 gitlab.com clones (SC-001) and the `SIGTERM` run; record the results in `specs/002-multi-repo-table/checklists/smoke.md`
- [X] T071 Write the commit message with a "Contract changes" section listing the four changes of contracts/cli.md (scan instead of "not a git repository", `--depth`, JSON `dir`, `--host` filtering outside a clone) (FR-018, constitution gate 7)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none.
- **Foundational (Phase 2)**: after Setup; blocks every story.
- **US1 (Phase 3)**: after Foundational. The MVP; it builds the whole scan pipeline
  (`Find` → `Inspect` → `Select` → `Fetch` → `Table`) and the mode switch in `cli.Run`.
- **US2 (Phase 4)**: after US1 (extends `Fetch`, `runScan` and the exit-code table).
- **US3 (Phase 5)**: after US1. Independent of US2, except that T057 keeps the all-failed path
  of T051 when US2 is already in.
- **US4 (Phase 6)**: after US1; independent of US2 and US3.
- **Polish (Phase 7)**: after the stories you intend to ship.

### Within Each Story

E2E test first (fails) → per-package test → implementation pairs → `cli` wiring, which makes
the e2e test pass. Tasks that touch `internal/cli/scan.go` (T036, T051, T057),
`internal/cli/run.go` (T037, T065), `internal/cli/help.txt` (T038, T052, T066),
`cmd/vigie/main_test.go` (T013, T040, T058), `internal/scan/fetch.go` (T031, T033, T048) or
`cmd/vigie/exitcode.go` (T035, T050) are sequential across stories.

### Key task dependencies

- Phase 2: T011 after T010 (same `gitlab` test binary); T012 after T011.
- US1: the scan chain T020 → T033 runs strictly in order; T027 also needs T015; T034 needs
  T021; T036 needs T017–T035; T037 needs T036.
- US2: T039 before T040, T041 and T047; T048 needs T042 and T044; T049 needs T046; T051 needs
  T046 and T048.
- US3: T054 before T055 (the schema test reads `docs/status.schema.json`).
- US4: T064 needs T062. If US2 and US4 overlap, the US4 `scan` pair (T063–T064) runs before
  T043 or after T048, never in between: an open US2 `scan` test stops the package's test
  binary from compiling.

### Parallel Opportunities

- Foundational: the config (T003–T004), repo (T005–T006), status (T007–T008) and gitlab
  (T009–T012) pairs in parallel.
- US1: four chains in parallel: e2e (T013), config (T014–T015), render (T016–T019), scan
  (T020–T033, T027 waiting for T015); then T034–T035; then T036 → T037 → T038.
- US2: after T039, the e2e (T040), gitlab (T041–T042) and scan (T043–T048) chains in
  parallel; then T049–T050; then T051 → T052.
- US3: T053 and T054 in parallel, then T055 → T056 → T057.
- US4: the e2e (T058), cli (T059–T060), config (T061–T062) and scan (T063, then T064 after
  T062) chains in parallel; then T065 → T066.
- After US1, US3 and US4 can proceed in parallel with US2, joining at the `internal/cli/` and
  `cmd/vigie/` files listed above. US3 shares no package test binary with US2; US4 shares
  `scan` with US2, so keep T063–T064 outside T043–T048 (Key task dependencies).
- Polish: T067 and T068 in parallel.

## Parallel Example: User Story 1

```bash
# Four chains at once, each strictly test → implementation inside its package:
Task: "T013 E2E tests for US1 in cmd/vigie/scan_test.go"
Task: "T014 Test config.KnownHost in internal/config/host_test.go, then T015"
Task: "T016 Golden tests for render.Table in internal/render/table_test.go, then T017, T018, T019"
Task: "T020 Test scan.Summary in internal/scan/summary_test.go, then T021 … T033 in order"
```

## Parallel Example: User Story 2

```bash
# After T039 (gitlabtest Behavior.Times):
Task: "T040 E2E tests for US2 in cmd/vigie/scan_failures_test.go"
Task: "T041 Test bounded retry in internal/gitlab/retry_test.go, then T042"
Task: "T043 Test slots.Halve in internal/scan/slots_test.go, then T044 … T048 in order"
```

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup → Phase 2 Foundational.
2. Phase 3 (US1) → **stop and validate** with quickstart.md "US1" on `~/scan-demo`.
3. MVP limits until later stories land: with every row failing the table is still printed
   with exit 0 (US2 changes that), `-o json` in scan mode prints the table (US3), and
   `--depth`/`--host` filtering are not available (US4).

### Incremental Delivery

US1 (MVP table) → US2 (failure isolation, exit codes, throttling) → US3 (JSON) → US4 (depth,
dir, host filter) → Polish. Each story opens with its e2e test and closes when the `cli`
wiring makes it pass, so `task test` proves the increment before the next starts. Contract
changes (T071) are listed in the commit that ships them.

## Notes

- Commit after each test+implementation pair; a behaviour change lands with the test that fails
  without it.
- No new direct dependency: stdlib, `golang.org/x/term` and `go.yaml.in/yaml/v3` only (plan
  "Technical Context").
- Goldens (`cmd/vigie/testdata/scan_table.golden`, `table_*.golden`, `json_row_*.golden`) are
  written by hand from the contract before the code exists; `-update` is only for later,
  reviewed changes.
- The progress counter needs stdout **and** stderr on a terminal (FR-013a, constitution
  Principle II). The rule is unit-tested through `render.ShowProgress` (T018); the drawn
  counter itself is checked by hand in T070, since e2e tests never run on a terminal.
- Research S14 (single-mode project lookup without a deadline) is deliberately out of scope;
  it belongs in its own change, at the author's decision.
- The JSON schema and contracts/cli.md are the output contract: the four changes listed in
  T071 must be called out in the commit message.
