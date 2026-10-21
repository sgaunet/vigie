# Implementation Plan: vigie — Multi-Repository Status Table

**Branch**: `002-multi-repo-table` (spec directory; no branch created) | **Date**: 2026-10-21 |
**Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/002-multi-repo-table/spec.md`, built on feature
001 ([plan](../001-gitlab-status-line/plan.md)).

## Summary

Run outside any clone and without `--project`, vigie scans `--dir` for git clones, keeps those
whose remote is on a known GitLab host, and prints one table row per clone, or one JSON line
per clone. A row shows what `vigie -C <clone>` would print.

The work is a new domain package `internal/scan` with four stages:

1. **find**: `filepath.WalkDir`, depth-bounded, no symlinks, no hidden directories;
2. **inspect**: the existing `repo.Detect`, 8 at a time;
3. **select**: host recognition and the `--host` filter, a pure function over
   `config.Settings`;
4. **fetch**: the existing project lookup and `status.Collect`, with one shrinkable semaphore
   per host, an auth latch, and bounded retry on throttling.

Around it: a table renderer and a progress counter in `render`, a `dir` property in the JSON
record, a `--depth` flag, and exit-code mapping for "every row failed" in `cmd/vigie`. There
are no new dependencies. Decisions are in [research.md](./research.md).

## Technical Context

**Language/Version**: Go 1.27 (`go 1.27.0`, `toolchain go1.27.2`, pinned in `mise.toml`),
unchanged.

**Primary Dependencies**: unchanged: stdlib, `golang.org/x/term`, `go.yaml.in/yaml/v3`. The
walk uses `path/filepath.WalkDir`; concurrency uses `sync` only.

**Storage**: none. The scan reads directories and git metadata and writes nothing (001
FR-012).

**Testing**: `task test` (`go test -count=2 -race ./...`); black-box packages; temp trees and
`git init` repos; `gitlabtest` extended with several projects per server and
`Behavior.Times`; render goldens; e2e on the built binary with a recording `HTTPS_PROXY` for
SC-003 (research S12).

**Target Platform**: linux, darwin, windows × amd64, arm64; static binary; `git` ≥ 2.7 for
detection, unchanged.

**Project Type**: CLI, one binary. The scan is a mode of the default command, not a
subcommand.

**Performance Goals**: table for 20 clones in under 3 s, p95 (SC-001); walk of 10,000
directories in under 2 s (SC-002).

**Constraints**: no request to an unrecognised host; stdout is data only and empty when every
row fails; `--timeout` per repository; at most 4 repositories in flight per host, halved on
throttling; each git call keeps its 5 s timeout.

**Scale/Scope**: tens to a few hundred clones, `--depth` 4 by default. Throughput is bounded
by per-host concurrency, not by the walk.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Source: `.specify/memory/constitution.md` v1.2.0. Pre-research and post-design results are
both shown.

- [x] **I. Single purpose**: it is the same job, the status of GitLab projects, for the clones
      below a folder. It is the "multi-project view" item of the 001 roadmap. It adds no new
      kind of output and no write action, and it composes through `-o json`.
- [x] **II. Pipe-safe contract**:
  - **Streams**: stdout carries only the table or JSON lines, and nothing when every row
    fails (author decision, research S8). The counter, summary and host errors go to stderr.
  - **Exit codes**: 0–5 only, documented in `--help` and tested. "Every row failed" maps to
    the common code of the row errors in `cmd/vigie`, explicitly (S7).
  - **Terminal**: `NO_COLOR` and `--no-color` apply; a non-TTY stdout gets no colour, emoji
    or cursor control, and no counter: it appears only when stdout and stderr are both
    terminals.
  - **Logging**: `--quiet` and `--verbose` apply.
  - **Precedence**: unchanged. `--depth` is a flag only.
  - **Destructive actions**: none.
  - **Contract changes**: listed in [contracts/cli.md](./contracts/cli.md) (FR-018), to be
    repeated in the commit message.
- [x] **III. Test-first**: every new type and function gets failing black-box tests first
      (S12). `scan` uses `export_test.go` only for `slots` and the backoff base. Argument
      parsing (`--depth`, `--watch` + scan), exit codes (the `AllFailedError` table) and the
      stream split (stdout empty on all-failed, counter on stderr) are each tested. The e2e
      suite runs the built binary on a folder of clones.
- [x] **IV. Thin commands**: `cli` parses `--depth`, picks the mode and calls the `scan`
      stages, then `render` and stderr formatting. Walk, recognition, concurrency,
      classification and outcome live in `internal/scan`, which imports no CLI package and is
      named for its domain. depguard still holds: `net/http` only in `gitlab`, `os/exec` only in
      `repo` and `token`. The help text stays embedded.
- [x] **V. Simple Go**: concrete types; no generics; one small shrinkable semaphore
      (`scan.slots`, mutex plus wake channel) is the only new concurrency primitive. Errors
      are wrapped with `%w`. `AllFailedError` deliberately has no `Unwrap`, and the exit-code
      table handles it with `errors.As`. Logging is `log/slog`; field warnings drop to debug in
      scan mode. `task lint` passes.
- [x] **VI. Safe to interrupt**: the signal context reaches the walk (checked per directory, and
      `cli` stops waiting for a walk blocked on a hung mount, research S2),
      the git calls, the slot waits, the retry timers and every HTTP call. Every repository
      fetch runs under `--timeout`. Retries are bounded: at most 4, backed off, and only
      within the deadline (S6). The counter is cleared on every exit path.
- [x] **Build & supply chain**: no new dependency and no generator. `CGO_ENABLED=0
      -trimpath` unchanged. A token is resolved at most once per host, never logged, and
      never sent to an unrecognised host (SC-003, proved by the proxy recorder).

**Result: PASS** before research and after design. One author decision was taken during
planning (all-failed → empty stdout). The other refinements are recorded as spec amendments
in research S13. Research S14 notes a pre-existing single-mode gap (project lookup without a
deadline) for the author, outside this feature.

## Project Structure

### Documentation (this feature)

```text
specs/002-multi-repo-table/
├── spec.md              # requirements (+ Clarifications, planning amendments)
├── plan.md              # this file
├── research.md          # Phase 0 decisions S1–S14
├── data-model.md        # Phase 1 types per package
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── cli.md           # scan-mode contract: modes, --depth, table, stderr, exit codes
│   └── status.schema.json  # JSON record schema v1 + "dir" (copy to docs/)
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

New files are marked `+`; everything else is an existing file that changes.

```text
cmd/vigie/
├── exitcode.go          # scan.ErrRoot, scan.ErrNoGitLab → 2; *scan.AllFailedError → common code, else 1
├── exitcode_test.go     # AllFailedError cases (same code, mixed codes)
├── scan_test.go       + # e2e US1: table golden, summary, empty folder, --watch, proxy
│                       #   recorder, SIGTERM, determinism, worktree, same project twice
├── scan_failures_test.go + # e2e US2: not found, latched host, all failed, throttling
├── scan_json_test.go  + # e2e US3: JSON lines, schema keys, equivalence with -C
├── scan_options_test.go + # e2e US4: --depth, --dir, --host, unreadable directory
└── testdata/scan_table.golden + # expected table, written by hand
internal/
├── cli/
│   ├── flags.go         # --depth
│   ├── help.txt         # "Folder of clones" section, --dir/--host/--depth/--timeout lines, exit rule
│   ├── run.go           # detect once; pick mode; --watch + scan → ErrUsage
│   └── scan.go        + # runScan: find → inspect → select → fetch → print; stderr lines; counter
├── config/
│   ├── settings.go      # Depth, DefaultDepth, validation
│   └── host.go          # KnownHost, OnHost
├── repo/
│   ├── errors.go        # ErrNoGit
│   └── checkout.go      # wrap ErrNoGit with ErrNotRepository when git is missing
├── gitlab/
│   ├── client.go        # Options.Retry, Options.OnThrottle; bounded retry in get
│   ├── refresh.go     + # RefreshError (moved from cli.runner.fatal)
│   ├── export_test.go + # backoff base for tests
│   └── gitlabtest/server.go  # AddProject, Behavior.Times
├── scan/              + # new domain package
│   ├── doc.go
│   ├── errors.go        # ErrRoot, ErrNoGitLab, AllFailedError
│   ├── find.go          # Find (WalkDir, depth, hidden, symlink, bare)
│   ├── inspect.go       # Inspect (repo.Detect × 8)
│   ├── select.go        # Select (KnownHost / OnHost, ResolveHost)
│   ├── fetch.go         # Fetch, host (once, latch, slots), Classify
│   ├── slots.go         # shrinkable per-host semaphore
│   ├── outcome.go       # Reason, Skip, Summary, Outcome, HostFailure, Latched
│   ├── export_test.go   # slots internals
│   └── *_test.go        # package scan_test
├── status/
│   └── row.go         + # Row, Failure
└── render/
    ├── table.go       + # Table
    ├── progress.go    + # Progress, ShowProgress
    ├── json.go          # JSONRow, "dir"
    └── testdata/        # table_*.golden, json_row_*.golden
docs/status.schema.json  # "dir" (from contracts/)
README.md                # "Folder of clones" section, --depth, scan exit codes
```

**Structure Decision**: one new domain package, `internal/scan`. It is named for what it does
and holds every rule the spec adds: walk, recognition, concurrency, failure classes, outcome.
`cli` stays a thin wiring layer, with `runScan` in its own file so `run.go` keeps the
single-project path readable. `render` stays free of `scan` by rendering `status.Row`, and
`status` still imports no other vigie package. The dependency direction becomes
`cmd → cli → {config, repo, token, gitlab, scan, render} → status`, with `scan →
{config, repo, token, gitlab, status}`.

## Complexity Tracking

No violations to justify.
