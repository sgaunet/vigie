# Implementation Plan: vigie — GitLab Project Status Line

**Branch**: `001-gitlab-status-line` (spec directory; work happens on `main` until a branch is
cut) | **Date**: 2026-10-19 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-gitlab-status-line/spec.md`, author design in
[input-design.md](./input-design.md) (superseded by the spec where they differ).

## Summary

vigie is a single-purpose Go CLI. Run in a git clone, it detects the GitLab project from a
remote and prints one status line (branches, pipelines, last pipeline, MRs, issues) as text or
JSON. With `--watch`, it redraws that line until interrupted. Approach: `git` subprocesses for
detection, stdlib `net/http` for the REST API v4 with all fields fetched concurrently under one
deadline, a pure renderer over an immutable `Snapshot`, and a watch loop with bounded backoff.
Dependencies: `golang.org/x/term` and `go.yaml.in/yaml/v3` (author approved). Decisions are in
[research.md](./research.md).

## Technical Context

**Language/Version**: Go 1.27 (`go 1.27.0`, `toolchain go1.27.2`, pinned in `mise.toml`)

**Primary Dependencies**: stdlib (`flag`, `net/http`, `encoding/json`, `log/slog`, `os/exec`,
`embed`); `golang.org/x/term`; `go.yaml.in/yaml/v3`. `golang.org/x/sync` approved but unused (R4).

**Storage**: none. Read-only, nothing written to disk (FR-012). Config file read only.

**Testing**: `go test -count=2 -race ./...` via `task test`; black-box `_test` packages;
`httptest.Server` + JSON fixtures; golden files (`-update` flag); `testing/synctest` for watch
timing; e2e test building the binary in `cmd/vigie`.

**Target Platform**: linux, darwin, windows × amd64, arm64; static binary (`CGO_ENABLED=0`);
`git` ≥ 2.7 on PATH for detection (`git remote get-url`).

**Project Type**: CLI (single binary, one default action plus `version`).

**Performance Goals**: one-shot refresh < 1 s on gitlab.com for ≤ 100 branches, p95 (SC-001);
≤ 60 API requests/min per watching instance at the default 5 s interval (SC-006).

**Constraints**: stdout = data only; token never in output, logs or files; explicit timeout on
every I/O; Ctrl+C restores the terminal within 1 s (SC-007).

**Scale/Scope**: one project per run; counts capped at 10,000 by the API (`10k+`); branch
listing bounded at 100 pages.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Source: `.specify/memory/constitution.md` v1.2.0. Pre-research and post-design results are
both shown. Re-checked after `/speckit-analyze`: the in-flight marker moved to stderr (C1);
`GITLAB_HOST` as a fallback-only variable (C2) and `token_command` (C3) are allowed by v1.2.0.

- [x] **I. Single purpose**: one job (report project status), read-only. Roadmap items (cache
      mode, `open`, notifications) are explicitly out of v1.
- [x] **II. Pipe-safe contract**: `--output=text|json` (Clarification Q2); stdout data only,
      help/version on stdout by explicit request; exit codes 0–5 in `--help` and tested
      (constitution v1.1.0); `NO_COLOR`/`--no-color`; no color, icons, cursor control or marker
      on a non-TTY, no override (Q3); `--quiet`/`--verbose`; precedence stated in `--help`
      ([contracts/cli.md](./contracts/cli.md)); no destructive action, so `--yes` is not
      needed; JSON schema versioned in `contracts/`.
- [x] **III. Test-first**: every task pairs a failing test first; black-box packages enforced
      by the `testpackage` linter; arg parsing, exit codes and stream split tested in
      `internal/cli` and `cmd/vigie`; e2e binary test in `cmd/vigie/main_test.go`.
- [x] **IV. Thin commands**: `internal/cli` only parses, validates, calls and formats; stdlib
      `flag` with a bare `version` word (constitution v1.1.1). Domain packages import no CLI
      package (`depguard`: `flag` only in `internal/cli`). Package names are domain names, with
      no `utils`. Help text embedded with `//go:embed`.
- [x] **V. Simple Go**: concrete types only, one interface (`status.Source`) as the test
      seam; `%w` wrapping with sentinel errors mapped to exit codes in one place; `log/slog` at
      a user-chosen level; `task lint` gate.
- [x] **VI. Safe to interrupt**: root context from `signal.NotifyContext(SIGINT, SIGTERM)`,
      passed to every git, token command and HTTP call; per-I/O timeouts in R9; retries only in
      the watch loop, with delay capped at 60 s (5 min for `Retry-After`).
- [x] **Build & supply chain**: `CGO_ENABLED=0 -trimpath`, pinned toolchain, goreleaser v2 with
      checksums + SBOM; no generators in v1; two direct dependencies, both approved and
      BSD/MIT/Apache-2.0; tokens only from the environment or a command; the `Token` type masks
      itself in every format.

**Result: PASS.** The author resolved three conflicts before design: exit codes (constitution →
1.1.0), the flag parser (→ 1.1.1) and the dependency approvals. Two small deviations from
the design are recorded in research.md: errgroup is not used, and `testing/synctest` replaces
the injected clock.

## Project Structure

### Documentation (this feature)

```text
specs/001-gitlab-status-line/
├── spec.md              # requirements (+ Clarifications)
├── input-design.md      # author's design, verbatim
├── plan.md              # this file
├── research.md          # Phase 0 decisions R1–R12
├── data-model.md        # Phase 1 types per package
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── cli.md           # flags, streams, text format, exit codes, messages
│   └── status.schema.json  # JSON record schema v1
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
cmd/vigie/
├── main.go              # root ctx (signals), run() → exit code mapping, panic → restore + 1
├── exitcode.go          # errors.Is table: sentinel → 0..5
├── main_test.go         # e2e: builds binary, fake GitLab (httptest) + temp git repo
└── exitcode_test.go
internal/
├── cli/                 # stdlib flag parsing, validation, help.txt (go:embed), wiring
├── config/              # Settings, HostProfile, env + YAML + precedence, validation
├── repo/                # git subprocesses (5 s timeout), ParseRemote
├── token/               # Token (self-masking), Resolve (env / command)
├── gitlab/              # Client (status.Source), Project lookup, error sentinels
├── status/              # FieldName, values, PipelineStatus, Snapshot, Collect, Fingerprint
├── render/              # Text, JSON, icons/labels/colors, width, Display (TTY redraw)
└── watch/               # loop, Backoff, branch cache, project refresh, SIGWINCH
    each package: *_test.go in package <pkg>_test, testdata/ for fixtures and goldens
go.mod  go.sum  mise.toml  Taskfile.yml  .golangci.yml  .goreleaser.yaml
.github/workflows/ci.yml  .github/workflows/release.yml
README.md                # install, usage, precedence, exit codes; links the constitution
```

**Structure Decision**: the design's layout with three changes. `internal/cli` holds flag
parsing so `config` imports no CLI package (Principle IV). `internal/token` isolates secret
handling so exactly one package touches the raw value. Fixtures move to per-package
`testdata/` (Go convention, keeps black-box tests self-contained). The dependency direction
is `cmd → cli → {config, repo, token, gitlab, watch, render} → status`; `status` imports
none of them.

## Complexity Tracking

No violations to justify.
