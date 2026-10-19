---

description: "Task list for vigie — GitLab Project Status Line"
---

# Tasks: vigie — GitLab Project Status Line

**Input**: Design documents from `specs/001-gitlab-status-line/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/cli.md](./contracts/cli.md),
[contracts/status.schema.json](./contracts/status.schema.json), [quickstart.md](./quickstart.md)

**Tests**: MANDATORY (constitution Principle III, TDD). Each test task is written first and
must **fail** (compile failure counts) before its implementation task starts. All tests are
black box (`package <pkg>_test`). Internals go through `export_test.go` only when unavoidable.
`task test` = `go test -count=2 -race ./...`.

**Organization**: grouped by user story (US1–US5 from spec.md) so each story is a shippable
increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: user story the task belongs to (US1…US5)
- Research decisions are cited as R1–R12 ([research.md](./research.md)); requirements as
  FR-xxx and SC-xxx ([spec.md](./spec.md)).

## Path Conventions

Go CLI layout from plan.md: `cmd/vigie/` (main, exit codes, e2e tests), `internal/<domain>/`
(logic + black-box tests + `testdata/`). Module `github.com/sgaunet/vigie`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: toolchain, task runner, lint rules, release and CI configuration.

- [X] T001 Create `go.mod` (module `github.com/sgaunet/vigie`, `go 1.27.0`, `toolchain go1.27.2`) and `mise.toml` pinning `go = "1.27.2"`, `task`, `golangci-lint = "2.14"`, `goreleaser = "2"` (R2)
- [X] T002 [P] Create `Taskfile.yml` with tasks `build` (`CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version={{.VERSION}} -X main.commit={{.COMMIT}} -X main.date={{.DATE}}" -o bin/vigie ./cmd/vigie`), `test` (`CGO_ENABLED=1 go test -count=2 -race ./...`), `lint` (`golangci-lint run`), `snapshot` (`goreleaser release --snapshot --clean`), `release` (`goreleaser release --clean`) per R12
- [X] T003 [P] Create `.golangci.yml` (golangci-lint v2 schema, `version: "2"`): standard linters plus `errorlint`, `wrapcheck`, `gosec`, `revive`, `testpackage`; `forbidigo` forbidding `fmt.Print*` outside `cmd/vigie` and `internal/render` and forbidding `InsecureSkipVerify`; `depguard` rules: `flag` only in `internal/cli` and `cmd/vigie`, `net/http` only in `internal/gitlab` (tests excepted), `os/exec` only in `internal/repo` and `internal/token`, deny any import path ending in `/utils`, `/helpers`, `/common`, `/base` (R12, Principle IV)
- [X] T004 [P] Create `.goreleaser.yaml` (v2): builds for linux/darwin/windows × amd64/arm64 with `CGO_ENABLED=0`, `-trimpath`, ldflags `main.version/commit/date`; archives; `checksum: {algorithm: sha256, name_template: checksums.txt}`; `sboms` (syft); `brews` to `sgaunet/homebrew-tools`; GitHub release (R1, R12)
- [X] T005 [P] Create `.github/workflows/ci.yml`: on push/PR, set up via `jdx/mise-action`, run `task lint`, `task test`, `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`, and `CGO_ENABLED=0 go build -trimpath ./...`
- [X] T006 [P] Create `.github/workflows/release.yml`: on tags `v*`, run `task release` with `GITHUB_TOKEN` and a `HOMEBREW_TAP_GITHUB_TOKEN` secret
- [X] T007 [P] Create `.gitignore` with `bin/`, `dist/`, `*.test`, `coverage.out`
- [X] T008 Create one `doc.go` per package with a one-sentence package comment (`internal/cli`, `internal/config`, `internal/repo`, `internal/token`, `internal/gitlab`, `internal/status`, `internal/render`, `internal/watch`) and a minimal `cmd/vigie/main.go` (`func main() {}`); verify `go build ./...` and `task lint` pass

**Checkpoint**: an empty but buildable, lintable module.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: domain types, secrets, settings, flag parsing, logging, exit codes and the e2e
harness that every story depends on.

**⚠️ CRITICAL**: no user story work can begin until this phase is complete.

### Tests (write first, must fail)

- [X] T009 [P] Test `status.ParseFields` in `internal/status/fieldname_test.go`: valid list keeps order; default list is `branches,pipelines,last,mrs,issues`; unknown name → error whose message lists the valid names; duplicate → error; empty → error
- [X] T010 [P] Test `status.ParsePipelineStatus` in `internal/status/pipeline_test.go`: all 11 GitLab statuses map to their constant; `""` → `None`; any other string → `Unknown` (data-model.md "PipelineStatus")
- [X] T011 [P] Test `token.Token` in `internal/token/token_test.go`: `fmt` verbs `%s %v %+v %#v %q`, `json.Marshal`, `slog` text and JSON handlers all print `glpat-****` for a `glpat-` token and `****` otherwise, never the raw value; `token.Resolve` with no profile reads `GITLAB_TOKEN` from the env map; empty or unset → `token.ErrMissing`
- [X] T012 [P] Test `config.Load` in `internal/config/settings_test.go`: defaults from data-model.md "Settings"; `VIGIE_INTERVAL` overrides default; `Overrides` win over env; `NO_COLOR` non-empty disables color, empty does not; `Interval < 2s`, unparsable duration, `Timeout <= 0`, invalid `Project` → `config.ErrInvalid`
- [X] T013 [P] Test config file loading in `internal/config/file_test.go` with `fstest.MapFS` and an env map, fixtures in `internal/config/testdata/*.yaml`: path is `VIGIE_CONFIG`, else `$XDG_CONFIG_HOME/vigie/config.yaml`, else `~/.config/vigie/config.yaml`; missing default file is not an error; missing `VIGIE_CONFIG` file → `ErrInvalid`; unknown key → `ErrInvalid`; a `token` key at any depth → `ErrTokenInFile`; `token_env` and `token_command` in one entry → `ErrInvalid`; file values rank below env and flags; `hosts` entries parse into `HostProfile`
- [X] T014 [P] Test flag parsing in `internal/cli/flags_test.go`: every long/short pair in contracts/cli.md sets the same `config.Overrides` field; `-i=3s` and `-i 3s` both work; `-wv` → `cli.ErrUsage`; unknown flag, bad duration, `-o yaml`, unexpected positional → `ErrUsage`; `version` and `version -o json` recognised; `-h`/`--help` → `flag.ErrHelp`; `-v -q` together → `ErrUsage`
- [X] T015 [P] Test logger setup in `internal/cli/logging_test.go`: default level Warn, `--verbose` Debug, `--quiet` Error; output goes to the given stderr writer only; any attribute whose key contains `token` is masked by `ReplaceAttr`
- [X] T016 [P] Test exit-code mapping in `cmd/vigie/exitcode_test.go` (package `main_test` via `cmd/vigie/export_test.go` exposing `ExitCode`): `nil` → 0; each sentinel from research R7 plus `gitlab.ErrInsecure` (→ 2), wrapped twice with `%w`, maps to its code; `context.Canceled` → 1 by default; any other error → 1

### Implementation

- [X] T017 [P] Declare the sentinel errors listed in research R7, plus `gitlab.ErrInsecure`, in `internal/cli/errors.go`, `internal/config/errors.go`, `internal/repo/errors.go`, `internal/token/errors.go`, `internal/gitlab/errors.go` (one `var ( … = errors.New(…) )` block each, messages lowercase without punctuation)
- [X] T018 Implement `FieldName`, `ParseFields`, `DefaultFields`, `Count`, `Branches`, `Pipelines`, `LastPipeline`, `PipelineStatus`, `ParsePipelineStatus`, `FieldResult`, `ErrUnavailable` and `Snapshot` (no `Fingerprint` yet) in `internal/status/fieldname.go`, `internal/status/pipeline.go`, `internal/status/snapshot.go` (makes T009, T010 pass)
- [X] T019 [P] Implement `token.Token` (unexported value; `String`, `GoString`, `Format`, `MarshalJSON`, `LogValue` return the mask; `Value()` returns the raw string), `token.Source{Env string; Command []string}`, and `Resolve(ctx, src token.Source, env map[string]string)` reading `GITLAB_TOKEN` only (profile sources come in US4) in `internal/token/token.go` (makes T011 pass). `token` imports no `config` type; `config.Endpoint` carries a `token.Source`
- [X] T020 Implement `config.Settings`, `config.Overrides`, `config.Output` and `config.Load(overrides, env, fsys)` with defaults, env, validation in `internal/config/settings.go` (makes T012 pass)
- [X] T021 Implement config file discovery and strict decoding (`yaml.v3` `Decoder.KnownFields(true)`, a pre-pass over `yaml.Node` to reject any `token` key), `HostProfile`, and `Settings.Profile(host string) (HostProfile, bool)` lookup by key in `internal/config/file.go`; wire it into `Load` (makes T013 pass)
- [X] T022 Implement `cli.Parse(args []string) (Command, config.Overrides, error)` with stdlib `flag.ContinueOnError`, every long+short name bound to one variable, a second `FlagSet` for `version`, and an embedded `internal/cli/help.txt` (`//go:embed`) that reproduces contracts/cli.md synopsis, flags, precedence block, environment variables and the exit-code table, in `internal/cli/flags.go` (makes T014 pass)
- [X] T023 Implement `cli.NewLogger(stderr io.Writer, level slog.Level) *slog.Logger` in `internal/cli/logging.go` (makes T015 pass)
- [X] T024 Implement `exitCode(err error) int` with `errors.Is` over the R7 table in `cmd/vigie/exitcode.go` (makes T016 pass)
- [X] T025 Implement `cmd/vigie/main.go`: `version`, `commit`, `date` vars with `runtime/debug.ReadBuildInfo` fallback; `main` creates `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`, calls `run(ctx, os.Args[1:], os.Stdout, os.Stderr, envMap())`, and `os.Exit(exitCode(err))`; a deferred `recover` runs registered cleanup and exits 1; `run` dispatches to `cli.Run` (stub returning nil for now), prints help to stdout for `flag.ErrHelp` (exit 0), prints `vigie <version> (commit <c>, built <d>)` or the JSON form from contracts/cli.md for `version`, and prints `<error>\nRun 'vigie --help' for usage.` to stderr for `ErrUsage`
- [X] T026 Create the e2e harness in `cmd/vigie/main_test.go` (package `main_test`): `TestMain` builds the binary once with `CGO_ENABLED=0 go build -trimpath` into a temp dir; helper `runVigie(t, dir string, env []string, args ...string) (stdout, stderr string, code int)` with a clean env; first tests: `version` (stdout only, exit 0), `version -o json` (valid JSON with `version`, `commit`, `date`), `--help` (stdout, exit 0, contains each exit code 0–5 and the precedence block), `--bogus` (empty stdout, one stderr error line, exit 2)

**Checkpoint**: the binary parses flags, prints help and version, maps exit codes; the e2e
harness runs.

---

## Phase 3: User Story 1 - See my project's status at a glance (Priority: P1) 🎯 MVP

**Goal**: `vigie` in a clone prints one line with project, branches, pipelines, last pipeline,
MRs and issues, with the exit codes of FR-035.

**Independent Test**: in a clone of a known gitlab.com project with only `GITLAB_TOKEN`
exported, `vigie` prints the line and the counts match the web UI (quickstart.md "US1").

### Tests for User Story 1 (MANDATORY - write first) ⚠️

- [X] T027 [P] [US1] Table test `repo.ParseRemote` in `internal/repo/remote_test.go`: `git@gitlab.com:group/sub/project.git`, `ssh://git@gitlab.example.com:2222/group/project.git` (port dropped), `https://user:pass@gitlab.example.com/group/project.git` (userinfo dropped), `http://` form, optional `.git`, trailing `/`, 4-level groups; invalid inputs (`file:///x`, `/local/path`, `gitlab.com`, empty, a path with one segment) → `repo.ErrRemoteURL` whose message contains no credentials
- [X] T028 [P] [US1] Test `repo.Detect(ctx, dir, remote)` in `internal/repo/checkout_test.go` using temp repos created with `git init`, `git remote add`, `git commit --allow-empty`: returns root, branch and the parsed `Remote`; a remote `https://user:s3cr3t@gitlab.test/g/p.git` yields a `Remote` without credentials and, with a debug `slog` logger attached, `s3cr3t` never appears in the log output; detached HEAD → `Branch == ""`; `url.<base>.insteadOf` rewrite applied; missing remote → `repo.ErrRemoteMissing` with existing remote names in the message; non-repo dir → `repo.ErrNotRepository`; a worktree created with `git worktree add` resolves; skip with `t.Skip` when `git` is absent
- [X] T029 [P] [US1] Create the fake GitLab server `gitlabtest.New(t, gitlabtest.Project{…}) *Server` in `internal/gitlab/gitlabtest/server.go` (test-support package, named like `httptest`): serves `GET /api/v4/projects/:id` (path-encoded or numeric) and the five metric endpoints of research R6 from fixtures in `internal/gitlab/gitlabtest/testdata/*.json`; knobs per endpoint for status code, `X-Total` presence, `X-Total-Pages`, `Retry-After`, latency; records requests (path, query, `PRIVATE-TOKEN` header) for assertions
- [X] T030 [US1] Test project lookup and the HTTP policy in `internal/gitlab/project_test.go` with `gitlabtest`: path is URL-encoded (`group%2Fsub%2Fproject`), numeric ID passed as is; `PRIVATE-TOKEN` header carries `Token.Value()`; 401 → `gitlab.ErrUnauthorized`, 403 → `ErrForbidden`, 404 → `ErrProjectNotFound`; `NewClient` with an `http://` base and `insecure=false` → `gitlab.ErrInsecure` (defence in depth behind `config.ErrInsecureHost`); a redirect to another host or from https to http is not followed and sends no token; connection refused and deadline exceeded → `ErrNetwork`; malformed JSON → an error that is none of the sentinels (exit 1)
- [X] T031 [US1] Test metrics in `internal/gitlab/metrics_test.go` with `gitlabtest`: `Branches` counts total and active (commit date within `activeSince`) across 3 pages fetched with at most 4 requests in flight; missing `X-Total-Pages` → `Total.Capped`; `Pipelines` reads `X-Total` of `status=running` and `status=pending`; `LastPipeline` queries `ref=<branch>&per_page=1&order_by=id&sort=desc`, maps status, empty list → `None`; `OpenMRs` reads `X-Total` with `state=opened&per_page=1`, missing `X-Total` on a full page → `Capped`; `OpenIssues` reads `statistics.counts.opened`; 403 or 404 on any metric → error wrapping `status.ErrUnavailable`
- [X] T032 [P] [US1] Test `status.Collect(ctx, src, fields, branch, now)` in `internal/status/collect_test.go` with a fake `Source`, inside `testing/synctest`: only requested fields are called, results keep request order; one erroring field leaves the others intact; with `--timeout` 10s and one field sleeping 20s, that field has a deadline error and the others succeed; total elapsed equals the slowest field (concurrency)
- [X] T033 [P] [US1] Golden tests for `render.Text(snapshot, style, now)` in `internal/render/text_test.go` with goldens in `internal/render/testdata/text_*.golden` and an `-update` flag: emoji + color and ASCII + no color variants of the full line; one golden per `PipelineStatus` (icon, label, color from contracts/cli.md); unavailable field shows `–`; capped count shows `10k+`; archived tag; ages `12s`, `3m`, `2h`, `4d`; dim/bold rules of FR-019 (branches total 1 dim, pipelines blue/yellow/dim, MRs/issues bold/dim)
- [X] T034 [P] [US1] Test `config.ResolveHost(settings, remoteHost string) (Endpoint, error)` in `internal/config/host_test.go`: `--host` wins; else the `hosts[remoteHost].api` mapping; else `https://<remoteHost>`; else `GITLAB_HOST`; else `config.ErrNoHost`; an `http://` result without `insecure: true` on the matching profile → `config.ErrInsecureHost`; the returned `Endpoint` carries API base URL, insecure flag and the profile's token source

### Implementation for User Story 1

- [X] T035 [P] [US1] Implement `repo.Remote` and `repo.ParseRemote` in `internal/repo/remote.go` (makes T027 pass)
- [X] T036 [P] [US1] Implement `repo.Checkout` and `repo.Detect` running `git rev-parse --show-toplevel`, `git symbolic-ref --short -q HEAD`, `git remote get-url <remote>` and, on failure, `git remote` (to list remotes) via `exec.CommandContext` with a 5 s timeout each; `Detect` passes the URL to `ParseRemote` and discards the raw string, which is never returned or logged; in `internal/repo/checkout.go` (makes T028 pass)
- [X] T037 [US1] Implement `gitlab.Client` in `internal/gitlab/client.go`: `NewClient(base *url.URL, tok token.Token, insecure bool)`; `http.Transport` with 5 s dial and TLS handshake timeouts; `CheckRedirect` refusing host changes and scheme downgrades; a `get(ctx, path, query, out)` helper returning headers and classifying status codes and transport errors into the sentinels; plus `Project(ctx, idOrPath)` in `internal/gitlab/project.go` (makes T030 pass)
- [X] T038 [US1] Implement `Branches`, `Pipelines`, `LastPipeline`, `OpenMRs`, `OpenIssues` and `X-Total` parsing in `internal/gitlab/metrics.go`, so `*gitlab.Client` satisfies `status.Source` (branch pages concurrent, max 4 in flight, stop at 100 pages) (makes T031 pass)
- [X] T039 [US1] Implement the `status.Source` interface and `status.Collect` (`context.WithTimeout`, `sync.WaitGroup.Go`, one result slot per field) in `internal/status/collect.go` (makes T032 pass)
- [X] T040 [US1] Implement the icon/label/color table in `internal/render/icons.go`, ANSI codes in `internal/render/ansi.go`, and `render.Style`, `render.Text`, short age formatting in `internal/render/text.go` (makes T033 pass)
- [X] T041 [P] [US1] Implement Windows VT enabling with `syscall.NewLazyDLL("kernel32.dll")` `GetConsoleMode`/`SetConsoleMode` (`ENABLE_VIRTUAL_TERMINAL_PROCESSING`), returning false on failure so color is disabled, in `internal/render/vt_windows.go`, and a no-op returning true in `internal/render/vt_other.go` (`//go:build !windows`)
- [X] T042 [US1] Implement `config.Endpoint` and `config.ResolveHost` in `internal/config/host.go` (makes T034 pass)
- [X] T043 [US1] Implement `cli.Run(ctx, cmd, overrides, io)` for one-shot text output in `internal/cli/run.go`: `config.Load` → `repo.Detect` (skipped when `--project` and `--host` are both set; when only `--project` is set, `repo.ErrNotRepository` is not fatal and the host falls back to `GITLAB_HOST`) → `config.ResolveHost` → `token.Resolve` → `gitlab.NewClient` → `Project` → reporting branch (current, else default) → `status.Collect` → `render.Text` with `Style{Color, Emoji}` from settings and `term.IsTerminal(stdout)` → one `\n`-terminated line on stdout; if every requested field failed with `gitlab.ErrNetwork`, return that error (exit 5); otherwise return nil (partial result, exit 0). Wire it from `cmd/vigie/main.go` `run`
- [X] T044 [US1] E2E tests for US1 in `cmd/vigie/main_test.go` using `gitlabtest` and a temp git repo whose `origin` is `git@gitlab.test:group/app.git`, with `VIGIE_CONFIG` pointing at a temp config mapping `gitlab.test` to `api: http://127.0.0.1:<port>` and `insecure: true`: one stdout line, exit 0, values match the fixture; detached HEAD reports the default branch; issues returning 403 → `–` and exit 0; temp non-repo dir → exit 2 and `not a git repository: …`; empty `GITLAB_TOKEN` → exit 3 and the exact FR message; server 401 → 3; project 404 → 4; a second temp repo whose origin is `git@down.test:group/app.git`, with `hosts: {down.test: {api: http://127.0.0.1:1, insecure: true}}` (closed port) → 5

**Checkpoint**: US1 is shippable as the MVP (one-shot text line, exit codes 0–5).

---

## Phase 4: User Story 2 - Feed the status to scripts and status bars (Priority: P2)

**Goal**: `--output json` emits one schema-conformant object per refresh; stdout never carries
anything but data; the token never appears anywhere.

**Independent Test**: `vigie -o json | jq .pipelines.running` prints a number;
`vigie | cat -v` shows no escape codes (quickstart.md "US2").

### Tests for User Story 2 (MANDATORY - write first) ⚠️

- [X] T045 [P] [US2] Golden tests for `render.JSON(w, snapshot)` in `internal/render/json_test.go` with goldens `internal/render/testdata/json_*.golden`: full record matching the shape of contracts/status.schema.json; unavailable field → `null` plus an `errors` entry `{field, reason}`; capped count → `null` with `"capped": true`; last pipeline `none` has only `status`; fields not requested are absent; `stale` is always present, `stale_since` only when stale; output is exactly one line ending in `\n`; a structural test decodes each golden into `map[string]any` and checks required keys and types against the schema's `required` lists

### Implementation for User Story 2

- [X] T046 [US2] Implement the JSON output structs (separate from domain types) and `render.JSON` in `internal/render/json.go` (makes T045 pass)
- [X] T047 [US2] Wire `--output json` in `internal/cli/run.go`: JSON goes to stdout regardless of TTY; text output on a non-TTY uses ASCII labels and no color (FR-022)
- [X] T048 [US2] E2E tests for US2 in `cmd/vigie/main_test.go`: `-o json` stdout is one line that `json.Unmarshal`s and contains the fixture values; text output through the pipe contains no `\x1b` byte and no emoji; `-v` writes debug lines to stderr only and stdout stays one line; for every e2e scenario in this file, a shared assertion checks that the token string appears in neither stdout nor stderr (SC-003) and that `-v` shows `glpat-****`; on a fatal error (`exit != 0`), stdout is empty

**Checkpoint**: US1 + US2 make vigie usable in pipes and scripts.

---

## Phase 5: User Story 3 - Keep watching while I work (Priority: P3)

**Goal**: `--watch` redraws in place every interval, keeps last good values with a stale
marker on failure, backs off on 429/5xx, stops on auth errors and always restores the terminal.

**Independent Test**: `vigie -w -i 2s` while a pipeline runs; Ctrl+C leaves a visible cursor
and a fresh prompt line, exit 0 (quickstart.md "US3").

### Tests for User Story 3 (MANDATORY - write first) ⚠️

- [X] T049 [P] [US3] Test `Snapshot.Fingerprint()` in `internal/status/fingerprint_test.go`: equal when only `FetchedAt`, `StaleSince` or ages differ; different when any value, field error, project, branch or `Stale` differs
- [X] T050 [P] [US3] Test `watch.Backoff` in `internal/watch/backoff_test.go`: `Next()` is `Base` with no failures, doubles per consecutive failure, caps at 60 s, uses `RetryAfter` when larger (capped at 5 min), resets after `Success()`
- [X] T051 [P] [US3] Test `render.Display` in `internal/render/display_test.go`, constructed with two buffers `(out, errOut)`: TTY mode hides the cursor on first draw, redraws with `\r\x1b[2K` on `out`; while `InFlight`, a dim ` ·` is written to `errOut` only and never to `out`; appends ` ⚠ stale 15s` (or ` ! stale 15s` without emoji) for a stale snapshot, `Close()` shows the cursor and writes `\n` exactly once even if called twice; line mode (non-TTY text) and JSON mode write a record only when `Fingerprint()` changed
- [X] T052 [US3] Test `watch.Run(ctx, watch.Config{…})` in `internal/watch/loop_test.go` with `testing/synctest`, a fake `status.Source`, a fake project resolver and a recording display: the next refresh starts one interval after the previous ended, never overlapping a slow source; a failed refresh yields a stale snapshot keeping old values with `StaleSince` set once per streak and cleared on recovery; 429 with `Retry-After: 30` delays the next refresh by 30 s; three 5xx in a row give delays 10 s, 20 s, 40 s at a 5 s interval; branch counts are fetched at most once per 60 s; the project is re-resolved every 5 min and a new default branch is used on a detached HEAD; `ErrUnauthorized` or `ErrForbidden` on the project stops the loop and returns that error; cancelling `ctx` returns nil within one tick; a send on the resize channel triggers an immediate redraw without a fetch

### Implementation for User Story 3

- [X] T053 [P] [US3] Implement `Snapshot.Fingerprint()` in `internal/status/fingerprint.go` (makes T049 pass)
- [X] T054 [P] [US3] Implement `watch.Backoff` in `internal/watch/backoff.go` (makes T050 pass)
- [X] T055 [US3] Implement `render.Display` (`NewDisplay(out, errOut io.Writer, mode, style)`; TTY, line and JSON modes; `Draw(snapshot, inFlight bool)` flushes the stdout line before writing the marker to `errOut`, and only when stderr is a TTY; `Close()`) in `internal/render/display.go` (makes T051 pass)
- [X] T056 [US3] Implement `watch.Run` with `time.NewTimer`, branch cache (60 s), project refresh (5 min), stale state transitions from data-model.md, `Backoff` on throttled/server/network failures, and resize-triggered redraws in `internal/watch/loop.go` (makes T052 pass)
- [X] T057 [P] [US3] Implement `watch.ResizeSignal(ctx) <-chan struct{}` with `signal.Notify(SIGWINCH)` in `internal/watch/resize_unix.go` (`//go:build unix`) and a never-firing channel in `internal/watch/resize_windows.go`
- [X] T058 [US3] Wire `--watch` in `internal/cli/run.go`: build `render.Display` for the output mode and TTY state, defer `Close()`, register `Close` with the panic cleanup in `cmd/vigie/main.go`, run `watch.Run`; watch-mode network failures never exit; `ctx` cancellation returns nil (exit 0) in watch mode and `context.Canceled` (exit 1) in one-shot mode
- [X] T059 [US3] E2E tests for US3 in `cmd/vigie/watch_test.go` (package `main_test`, `//go:build unix`): `-w -i 2s` piped prints a new line only after the fake server's values change; `-w -o json` emits one `"stale": true` record when the server starts failing and one `"stale": false` record on recovery; SIGINT → exit 0 and stdout ends with `\n`; server switching to 401 → exit 3 within one interval

**Checkpoint**: US1–US3 cover the whole day-to-day use on gitlab.com.

---

## Phase 6: User Story 4 - Use a self-hosted GitLab and per-host tokens (Priority: P4)

**Goal**: per-host API mapping, per-host token sources, explicit project/host outside a repo,
and refusal of plain HTTP and literal tokens.

**Independent Test**: with a config mapping `ssh.gitlab.example.com` to
`https://gitlab.example.com` and a `token_command`, `vigie` works in a clone using the SSH
remote (quickstart.md "US4").

### Tests for User Story 4 (MANDATORY - write first) ⚠️

- [X] T060 [P] [US4] Test `token.Resolve` with a `token.Source` in `internal/token/resolve_test.go`: `Env` set → reads that variable, ignoring `GITLAB_TOKEN`; empty variable → `ErrMissing` naming the variable; `Command` → stdout trimmed of whitespace; non-zero exit → `ErrCommand` whose message contains neither the command's stdout nor its stderr; a command running past 5 s is killed and returns `ErrCommand` (use the re-exec helper-process pattern with `os.Args[0]` and an env switch)
- [X] T061 [P] [US4] Extend `internal/config/host_test.go`: `--host` and `GITLAB_HOST` match a profile by its key or by its `api` host, so the profile's token source and insecure flag apply; outside a repo with `--project` but no host and no `GITLAB_HOST` → `ErrNoHost`
- [X] T062 [P] [US4] Test TLS roots: in `internal/gitlab/tls_test.go`, `gitlab.NewClient` given `ExtraRootsPEM` holding the certificate of an `httptest.NewTLSServer` trusts that server (run on macOS and Linux at least), and without it the call fails with an error wrapping `gitlab.ErrNetwork`; in `internal/config/host_test.go`, `SSL_CERT_FILE` in the env map fills `Endpoint.ExtraRootsPEM` with the file content, and an unreadable file or one without a PEM block → `config.ErrInvalid`

### Implementation for User Story 4

- [X] T063 [US4] Implement `token_env` and `token_command` resolution (argv exec, no shell, 5 s `exec.CommandContext`, stderr discarded) in `internal/token/resolve.go` (makes T060 pass)
- [X] T064 [US4] Extend profile matching by `api` host in `internal/config/host.go` and `Settings.Profile` in `internal/config/file.go` (makes T061 pass)
- [X] T065 [US4] Implement `SSL_CERT_FILE` support: `config.ResolveHost` reads the file into `Endpoint.ExtraRootsPEM` in `internal/config/host.go`; `gitlab.NewClient` gains an `ExtraRootsPEM []byte` option and builds its roots from `x509.SystemCertPool()` plus `AppendCertsFromPEM` in `internal/gitlab/tls.go` (makes T062 pass)
- [X] T066 [US4] E2E tests for US4 in `cmd/vigie/main_test.go`: from a non-repo temp dir, `-p group/app --host http://127.0.0.1:<port>` with an insecure profile keyed by that host → status line, exit 0; a remote on `ssh.gitlab.test` mapped via `api` → resolved on the fake server; `token_command: ["sh", "-c", "echo glpat-fromcmd"]` (unix) → the fake server records that token; from a non-repo temp dir, `-p group/app` with `GITLAB_HOST=http://127.0.0.1:<port>` (insecure profile) → status line, exit 0; config containing `token:` → exit 2; `http://` API without `insecure: true` → exit 2 and the fake server recorded zero requests

**Checkpoint**: self-hosted users are covered.

---

## Phase 7: User Story 5 - Shape the line to my taste (Priority: P5)

**Goal**: field selection and order, ASCII labels, `NO_COLOR`, left truncation to the terminal
width, and the same options from the config file.

**Independent Test**: `vigie -f pipelines,last,mrs --no-emoji` shows exactly those three ASCII
segments in that order (quickstart.md "US5").

### Tests for User Story 5 (MANDATORY - write first) ⚠️

- [X] T067 [P] [US5] Golden tests in `internal/render/width_test.go`: display width counts emoji as 2 cells and `…` as 1; at widths 120, 60 and 40 the project path is cut from the left (`…/sub/project`) until the line fits and no other segment is cut; width 0 means no truncation; field subsets render in the requested order
- [X] T068 [P] [US5] Extend `internal/config/file_test.go`: `fields`, `emoji: false` and `active_days` from the file apply; `--fields` and `--no-emoji` override them; invalid `fields` in the file → `ErrInvalid` naming the valid fields

### Implementation for User Story 5

- [X] T069 [US5] Implement cell-width measurement and left truncation of the project segment in `internal/render/width.go` and apply it in `internal/render/text.go` (makes T067 pass)
- [X] T070 [US5] Apply `fields`, `emoji`, `active_days` file keys in `internal/config/file.go` (makes T068 pass), and pass `term.GetSize` width into `render.Style` (TTY only, re-read on each draw) in `internal/cli/run.go`
- [X] T071 [US5] E2E tests for US5 in `cmd/vigie/main_test.go`: `-f pipelines,last,mrs --no-emoji` → three segments in that order; `-f mrs,mrs` → exit 2 and stderr lists the valid fields; `NO_COLOR=1` → no `\x1b`; config `fields: [mrs]` → only the MRs segment

**Checkpoint**: all five user stories are complete.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T072 [P] Write `README.md`: install (`brew install sgaunet/tools/vigie`, `go install github.com/sgaunet/vigie/cmd/vigie@latest`, release download), usage examples from contracts/cli.md, config file reference, precedence, exit codes, JSON schema link, link to `.specify/memory/constitution.md`
- [X] T073 [P] Copy `specs/001-gitlab-status-line/contracts/status.schema.json` to `docs/status.schema.json` and link it from `README.md` and `internal/cli/help.txt`
- [X] T074 Run the gates from quickstart.md: `task lint`, `task test`, `go generate ./... && git diff --exit-code`, `CGO_ENABLED=0 go build -trimpath ./...`, `task snapshot`; fix every finding (settle lint disagreements in `.golangci.yml`, not with `//nolint`)
- [ ] T075 Manual smoke test against a real gitlab.com project following every scenario in `specs/001-gitlab-status-line/quickstart.md`, including `time vigie` < 1 s (SC-001) and Ctrl+C restore (SC-007); record results in `specs/001-gitlab-status-line/checklists/smoke.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none.
- **Foundational (Phase 2)**: after Setup; blocks every story.
- **US1 (Phase 3)**: after Foundational. It is the MVP and builds the data path every later
  story reuses (`repo`, `gitlab`, `status.Collect`, `render.Text`, `cli.Run`).
- **US2 (Phase 4)**: after US1 (adds a renderer to `cli.Run`).
- **US3 (Phase 5)**: after US1. Independent of US2 except T059's JSON scenario, which needs T046.
- **US4 (Phase 6)**: after US1; independent of US2 and US3.
- **US5 (Phase 7)**: after US1; independent of US2, US3 and US4.
- **Polish (Phase 8)**: after the stories you intend to ship.

### Within Each Story

Test tasks → (fail) → implementation tasks in listed order → story e2e task last. Tasks that
touch `internal/cli/run.go` (T043, T047, T058, T070) are sequential across stories.

### Parallel Opportunities

- Setup: T002–T007 together after T001.
- Foundational tests T009–T016 all in parallel; then T017, T019 in parallel with T018.
- US1 tests T027, T028, T029, T032, T033, T034 in parallel; T030/T031 after T029.
  Implementation T035, T036, T041 in parallel.
- After US1, US3, US4 and US5 can proceed in parallel (different packages), joining only at
  `internal/cli/run.go`.

## Parallel Example: User Story 1

```bash
# All independent US1 tests at once:
Task: "T027 Table test repo.ParseRemote in internal/repo/remote_test.go"
Task: "T028 Test repo.Detect in internal/repo/checkout_test.go"
Task: "T029 Fake GitLab server in internal/gitlab/gitlabtest/server.go"
Task: "T032 Test status.Collect in internal/status/collect_test.go"
Task: "T033 Golden tests for render.Text in internal/render/text_test.go"
Task: "T034 Test config.ResolveHost in internal/config/host_test.go"

# Then independent implementations:
Task: "T035 repo.ParseRemote in internal/repo/remote.go"
Task: "T036 repo.Detect in internal/repo/checkout.go"
Task: "T041 Windows VT enabling in internal/render/vt_windows.go"
```

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 Setup → Phase 2 Foundational.
2. Phase 3 (US1) → **stop and validate** with quickstart.md "US1" against gitlab.com.
3. `task snapshot`; tag `v0.1.0` if releasing the MVP.

### Incremental Delivery

US1 (MVP) → US2 (pipes/JSON) → US3 (watch) → US4 (self-hosted) → US5 (shaping) → Polish. Each
story ends with its own e2e task, so `task test` proves the increment before the next starts.

## Notes

- Commit after each test+implementation pair; a behaviour change lands with the test that fails
  without it.
- Any new direct dependency beyond `golang.org/x/term` and `go.yaml.in/yaml/v3` must be proposed
  to the author first (`golang.org/x/sync` is approved but deliberately unused, R4).
- The JSON schema and contracts/cli.md are the output contract: changing them is a breaking
  change and must be called out.
