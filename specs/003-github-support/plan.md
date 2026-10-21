# Implementation Plan: vigie — GitHub Support

**Branch**: `003-github-support` (spec directory; no branch created) | **Date**: 2026-10-21 |
**Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/003-github-support/spec.md`, built on features
001 ([plan](../001-gitlab-status-line/plan.md)) and 002
([plan](../002-multi-repo-table/plan.md)).

## Summary

vigie shows the same five fields for a GitHub repository as for a GitLab project: in the
single line, in watch mode and in the folder table. github.com works with only `GH_TOKEN` (or
`GITHUB_TOKEN`) exported. A GitHub Enterprise Server instance is declared once in the config
with `platform: github`.

The work, in four parts:

1. **A shared platform contract**, the new package `internal/forge`: the client interface,
   the project lookup type, the error sentinels, refresh classification and the HTTP
   transport. Today these sit in `gitlab` and are named in five places. `gitlab` moves onto
   it without behaviour change.
2. **A GitHub client**, the new package `internal/github`:
   - REST (`2022-11-28`) for the lookup, the workflow-run counts and the CI result of the
     branch. Those requests are revalidated with ETags, so an unchanged answer costs nothing
     (G7).
   - GraphQL for branch commit dates and the open pull request and issue counts.
   - The CI result combines the latest run of each workflow on the newest commit with runs
     (G6).
   - The client stops requesting while GitHub's allowance is exhausted (G8).
3. **Platform-aware settings**, in `config` and `token`:
   - the github.com host family;
   - the `platform` host key;
   - the GitHub CLI token conventions: `GH_TOKEN`/`GITHUB_TOKEN` for github.com,
     `GH_ENTERPRISE_TOKEN`/`GITHUB_ENTERPRISE_TOKEN` for Enterprise Server;
   - the `owner/repo` path rule.
4. **Output and scan**:
   - 🦊/🐙 replace 🗼 on a terminal;
   - a `PLATFORM` table column;
   - a required JSON `platform` property;
   - the `no token` skip in scans;
   - a scan latch for an exhausted allowance.

There are no new dependencies. The decisions are in [research.md](./research.md) (G1–G16).

## Technical Context

**Language/Version**: Go 1.27 (`go 1.27.0`, `toolchain go1.27.3`, pinned in `mise.toml`),
unchanged.

**Primary Dependencies**: unchanged: stdlib, `golang.org/x/term`, `go.yaml.in/yaml/v3`.
GraphQL is plain `net/http` + `encoding/json` POSTs; no GraphQL or GitHub SDK.

**Storage**: none. The revalidation cache is in memory for the life of the process (001
FR-012).

**Testing**:
- `task test` (`go test -count=2 -race ./...`), black-box packages.
- A new `githubtest` fake server: REST plus GraphQL dispatched on `operationName`, with ETags,
  billing counters, rate-limit behaviours and a token log.
- `testing/synctest` for the one-hour allowance test.
- Render goldens.
- e2e on the built binary with an Enterprise Server entry pointing at `githubtest`, plus the
  002 recording proxy.

**Target Platform**: linux, darwin, windows × amd64, arm64; static binary; `git` for
detection. Unchanged.

**Project Type**: CLI, one binary. GitHub is a backend of the existing default command, not a
mode or subcommand.

**Performance Goals**:
- one-shot line in under 2 s p95 on github.com (SC-002); one refresh is 7 REST requests and 3
  GraphQL queries, sent concurrently per field;
- a scan of 20 mixed clones within 002's SC-001 envelope;
- watch: at most 2,500 billed REST requests and 2,500 GraphQL points per hour (SC-005, per
  pool).

**Constraints**:
- **Tokens**: never cross platforms or kinds of host (SC-004).
- **Allowance**: no request while it is exhausted (FR-018).
- **Run counts**: a count of 1,000 or more is never shown as exact.
- **Output**: GitLab output is unchanged apart from the three platform markers (SC-008).
- **Timeouts**: every request runs under the refresh or row deadline (Principle VI).

**Scale/Scope**:
- repositories up to 100 branches for the budget (more is allowed: +1 point per 100 branches
  per minute in watch);
- scans of tens to a few hundred clones, 4 in flight per host (002).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Source: `.specify/memory/constitution.md` v1.2.0. The pre-research and post-design results
are both shown.

- [x] **I. Single purpose**: same job, the status of the repository behind a clone, from a
      second hosting platform. No new mode, no subcommand, no write action. It composes
      through `-o json` with one schema for both platforms (US3).
- [x] **II. Pipe-safe contract**:
  - **Streams**: stdout carries only data. The allowance, token and latch messages go to
    stderr, one line each, with a hint.
  - **Exit codes**: still 0–5. The GitHub causes are mapped through the shared `forge`
    sentinels, listed in `--help` and tested.
  - **Terminal**: `NO_COLOR`, the non-TTY rule, `--quiet` and `--verbose` unchanged. The
    platform icon only appears where icons already do.
  - **Precedence**: unchanged for settings and host. The token chain gains one line per kind
    of host, stated in `--help` (research G3).
  - **Destructive actions**: none.
  - **Contract changes**: the eight items in [contracts/cli.md](./contracts/cli.md) are listed
    for the commit message. The table column and the required JSON property are breaking for
    text parsers and strict validators, so the release is v0.3.0 (research G11).
- [x] **III. Test-first**: every new type and function gets failing black-box tests first
      (research G13).
  - **Internals**: `github` exposes only the API-address derivation and the gate clock
    through `export_test.go`; `runState`/`combine` are tested through `export_test.go`
    aliases as pure functions.
  - **Argument parsing**: `platform` key, `--project` path rule.
  - **Exit codes**: GitHub causes → 2/3/4/5.
  - **Stream split**: allowance messages on stderr, empty stdout on failure.
  - **e2e** on the built binary: single, JSON, mixed scan, token separation, no-token proxy
    proof, exhausted allowance.
- [x] **IV. Thin commands**:
  - `cli` only resolves settings, picks the client constructor and wires `watch`/`render`.
  - Platform rules live in domain packages that import no CLI package: recognition and
    tokens in `config`/`token`, API logic in `github`, shared contract in `forge`.
  - The new packages are named for their domain. `forge` is the conventional name for a
    code-hosting service, not a grab-bag: it holds only what the two platform clients share.
  - depguard: `net/http` allowed in `forge`, `gitlab`, `github` only; `os/exec` still only in
    `repo` and `token`. The help text stays embedded.
- [x] **V. Simple Go**:
  - one interface, `forge.Client`, with two concrete implementations; no generics;
  - the client constructor switch appears twice (`cli`, `scan`), below the rule-of-three
    threshold (research G1);
  - errors wrapped with `%w`; `ErrRateLimit` is joined with `ErrNetwork` (`%w … %w`), so
    `errors.Is` finds both;
  - `log/slog` for the rename note and gate events at debug level;
  - `task lint` passes.
- [x] **VI. Safe to interrupt**:
  - every REST and GraphQL request takes the refresh or row context; in single mode the
    project lookup runs under its own `--timeout` deadline (research G16);
  - the gate answers locally, so it never sleeps;
  - waits for a reset happen in `watch` (interruptible timer, at most 1 h) or not at all
    (one-shot exits 5; scan latches);
  - retries stay bounded (002 S6). Branch paging is capped at 100 pages, run lookup at 2
    requests.
- [x] **Build & supply chain**:
  - no new dependency, no generator; `CGO_ENABLED=0 -trimpath` unchanged;
  - tokens come from the environment or a config-named command only, are masked with their
    GitHub prefixes, and are never sent across platforms;
  - `gh` is never run unless a `token_command` names it.

**Result: PASS** before research and after design. The design adds no violation. The planning
decisions that refine the spec are recorded as amendments (research G15). Research G16 closes
002's open note on the single-mode lookup deadline, which `/speckit-analyze` found to break
Principle VI.

## Project Structure

### Documentation (this feature)

```text
specs/003-github-support/
├── spec.md              # requirements (+ Clarifications, planning amendments)
├── plan.md              # this file
├── research.md          # Phase 0 decisions G1–G16
├── data-model.md        # Phase 1 types per package
├── quickstart.md        # Phase 1 validation guide
├── contracts/
│   ├── cli.md           # GitHub contract: hosts, config, tokens, output, errors, exit codes
│   └── status.schema.json  # JSON record schema v1 + "platform" (copy to docs/)
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

New files are marked `+`; every other file listed already exists and changes.

```text
cmd/vigie/
├── exitcode.go          # forge sentinels; scan.ErrNothingListed
├── exitcode_test.go     # GitHub causes → 2/3/4/5
├── github_test.go     + # e2e US1/US3: GHES entry → githubtest; line, JSON, 401, 404, path rule
├── github_scan_test.go + # e2e US2: mixed folder golden, no-token skip, --host github.com, latch
├── github_tokens_test.go + # e2e SC-004: token separation across three servers; no-token proxy proof
├── github_limits_test.go + # e2e US4: exhausted allowance one-shot (exit 5) and scan latch
└── testdata/scan_mixed.golden + # expected mixed table, written by hand
internal/
├── forge/             + # shared platform contract (research G1, G10)
│   ├── doc.go
│   ├── client.go        # Client, Project, Options
│   ├── errors.go        # ErrInsecure, ErrUnauthorized, ErrForbidden, ErrNotFound, ErrNetwork, ErrRateLimit
│   ├── refresh.go       # RefreshError, RetryAfter (moved from gitlab)
│   ├── http.go          # NewHTTPClient, redirect policy, ParseRetryAfter (moved from gitlab)
│   ├── tls.go           # extra roots (moved from gitlab)
│   └── *_test.go        # package forge_test (moved and new)
├── github/            + # GitHub REST + GraphQL client (research G4–G9)
│   ├── doc.go
│   ├── client.go        # NewClient, API addresses, get (REST), query (GraphQL), headers
│   ├── cache.go         # revalidation cache (ETag / 304)
│   ├── limits.go        # gate, rateLimitError, classification of 401/403/404/429/5xx
│   ├── repo.go          # Project lookup (path, ID, rename)
│   ├── metrics.go       # repoSource: Branches, Pipelines, LastPipeline, OpenMRs, OpenIssues
│   ├── runs.go          # run, runState, combine (pure)
│   ├── queries.go       # named GraphQL documents: Branches, PullRequests, Issues
│   ├── export_test.go   # API addresses, gate clock, runState/combine
│   ├── *_test.go        # package github_test
│   └── githubtest/    + # fake GitHub server (REST /api/v3, GraphQL /api/graphql)
│       └── server.go
├── gitlab/
│   ├── client.go        # forge transport, forge sentinels, forge.Options
│   ├── project.go       # returns forge.Project; Source(p)
│   ├── metrics.go       # forge sentinels in metricError
│   ├── errors.go        # removed (sentinels moved to forge)
│   ├── refresh.go       # removed (moved to forge)
│   ├── tls.go           # removed (moved to forge)
│   └── *_test.go        # sentinel names updated
├── config/
│   ├── file.go          # platform key, validation
│   ├── host.go          # github.com family in hostKey/KnownHost; Platform and Token.Vars in ResolveHost; *.github.com refusal; CheckProject
│   └── *_test.go        # family, platform key, Vars, CheckProject
├── token/
│   ├── token.go         # Source.Vars, GitLabVars/GitHubVars/EnterpriseVars, Mask prefixes
│   ├── resolve.go       # Vars precedence, hints, Configured
│   └── *_test.go
├── status/
│   ├── platform.go    + # Platform
│   ├── snapshot.go      # Project.Platform
│   └── fingerprint.go   # includes Platform
├── scan/
│   ├── select.go        # env argument, GitHub recognition, ReasonNoToken, path rule
│   ├── outcome.go       # ReasonNoToken, ErrNothingListed hint
│   ├── errors.go        # ErrNoGitLab → ErrNothingListed
│   ├── fetch.go         # constructor switch, hostID, latches, Classify on forge sentinels, Platform on rows
│   └── *_test.go        # githubtest hosts, no-token skip, rate-limit latch
├── render/
│   ├── icons.go         # platformLooks 🦊 🐙
│   ├── text.go          # project icon by platform
│   ├── table.go         # PLATFORM column
│   ├── json.go          # "platform"
│   └── testdata/        # updated goldens (🦊, PLATFORM, platform) + GitHub goldens
├── watch/
│   └── backoff.go       # MaxRetryAfter 1 h
└── cli/
    ├── run.go           # forge.Client, constructor switch, CheckProject, Platform, rename note, forge sentinels
    ├── scan.go          # env to Select
    └── help.txt         # research G14
.golangci.yml            # depguard http rule: forge, gitlab, github
docs/status.schema.json  # from contracts/
README.md                # GitHub sections (research G14)
```

**Structure Decision**: two new domain packages.
- `internal/forge` holds the contract both platforms satisfy, so every consumer (exit codes,
  scan classes and latch, watch fatal errors) classifies errors once.
- `internal/github` holds every GitHub API rule, tested against its own fake server.

`gitlab` shrinks to its API logic. `cli` and `scan` each pick a client constructor by
`Endpoint.Platform` and otherwise see only `forge.Client`. `render` and `status` learn the
platform as plain data. The dependency direction is in
[data-model.md](./data-model.md); `status` and `forge` stay leaves below the clients.

## Complexity Tracking

No violations to justify.
