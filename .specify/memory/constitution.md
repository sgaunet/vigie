<!--
Sync Impact Report
==================
Version change: 1.1.1 → 1.2.0 (2026-10-19)
Bump rationale: MINOR, two rules materially expanded (from /speckit-analyze findings C2, C3).
Modified principles / sections:
  - II. Pipe-Safe CLI Contract (Configuration): detected values rank between config file and
    defaults; an env var documented as a fallback MAY rank below detection, stated in --help.
  - Build, Release & Supply Chain (Secrets): secrets also banned from config files; they may
    come from the environment or an external command named in the config (no shell, timeout).
Templates requiring updates:
  ✅ .specify/templates/plan-template.md — Principle II and supply-chain gate wording

Version change: 1.1.0 → 1.1.1 (2026-10-19)
Bump rationale: PATCH, clarification of Principle IV: stdlib `flag` stays allowed when the
  only subcommand is a bare `version` word (author decision during /speckit-plan, feature 001).
Templates requiring updates: none.

Version change: 1.0.0 → 1.1.0 (2026-10-19)
Bump rationale: MINOR, Principle II expanded to allow tool-specific exit codes ≥ 3.
Modified principles:
  - II. Pipe-Safe CLI Contract: exit codes 0/1/2 keep their meaning; codes from 3 up are
    allowed when documented in --help, tested, and wired explicitly in main.
Trigger: /speckit-clarify on specs/001-gitlab-status-line (vigie needs 3 auth, 4 not found,
  5 network).
Templates requiring updates:
  ✅ .specify/templates/plan-template.md   — Principle II gate wording
  ✅ .specify/templates/tasks-template.md  — T004 wording

Previous change
---------------
Version change: (template, unversioned) → 1.0.0
Bump rationale: first ratification; every placeholder replaced with concrete principles.

Modified principles (template slot → new title):
  - [PRINCIPLE_1_NAME] → I. Single-Purpose, Composable Tool
  - [PRINCIPLE_2_NAME] → II. Pipe-Safe CLI Contract
  - [PRINCIPLE_3_NAME] → III. Test-First (NON-NEGOTIABLE)
  - [PRINCIPLE_4_NAME] → IV. Thin Commands, Domain Packages
  - [PRINCIPLE_5_NAME] → V. Simple, Explicit Go
Added principles:
  - VI. Safe to Interrupt
Added sections:
  - Build, Release & Supply Chain   (was [SECTION_2_NAME])
  - Development Workflow & Quality Gates   (was [SECTION_3_NAME])
Removed sections: none

Templates requiring updates:
  ✅ .specify/templates/plan-template.md   — Constitution Check gates, Go CLI layout
  ✅ .specify/templates/tasks-template.md  — tests mandatory (TDD), Go paths, CLI foundations
  ✅ .specify/templates/spec-template.md   — reviewed, no change needed
  ✅ .claude/skills/speckit-*/SKILL.md     — reviewed, no agent-specific references to fix
  ⚠ README.md / runtime guidance docs     — none exist yet; reference this file when created

Follow-up TODOs:
  - Input bullet on licences was truncated ("copyleft or unlicensed modules -"); completed as
    "MUST NOT be added". Author to confirm.
-->

# Vigie Constitution

## Core Principles

### I. Single-Purpose, Composable Tool

- Vigie is a command-line tool with one focused job per binary. A capability that is not part
  of that job MUST become a separate tool, not a new mode of this one.
- The tool MUST compose with other tools through pipes: it reads from arguments or stdin and
  writes data to stdout.

Rationale: a narrow tool is easy to reason about, test, and chain; scope creep is what turns a
CLI into an application.

### II. Pipe-Safe CLI Contract

- **Streams**: stdout MUST carry data only, machine-parseable, selectable with
  `--output=text|json`. Stderr MUST carry logs, errors, and progress.
- **Exit codes**: `0` success, `1` runtime failure, `2` usage error. A tool MAY define further
  codes from `3` up for failure classes a caller can act on differently (e.g. authentication,
  not found, network); codes `0`–`2` keep their meaning. Every code MUST be documented in
  `--help` and covered by a test. Cobra sets no exit code of its own (`Execute()` returns an
  error and the scaffolded `main` exits `1` for everything), so code `2` and any code from `3`
  up MUST be wired explicitly in `main`.
- **Terminal behaviour**: the tool MUST respect `NO_COLOR`, MUST NOT emit colour, spinners, or
  progress bars when stdout is not a TTY, and MUST support `--quiet` and `--verbose`.
- **Configuration**: precedence MUST be flags > environment > config file > defaults, and MUST
  be stated in `--help`. A value detected from the working context (e.g. the git remote) ranks
  between the config file and defaults. An environment variable documented as a *fallback* for
  such a value MAY rank below detection; `--help` MUST say so.
- **Destructive actions** MUST require `--yes` or an interactive confirmation, so a mistyped
  command cannot delete anything.
- The output format (fields, ordering, JSON schema) and exit codes are a public contract. A
  change that breaks them is a breaking change for the tool's version.

Rationale: the stdout/stderr split and stable exit codes are what make the tool safe inside a
pipe or a script.

### III. Test-First (NON-NEGOTIABLE)

- Development MUST follow TDD: write the failing test, make it pass, then refactor. A behaviour
  change MUST arrive with the test that fails without it.
- Tests MUST live in `package <pkg>_test` (black box), so they exercise the API a consumer
  actually has. Internals a test needs are exposed through an `export_test.go` inside the
  package, never by moving the test back into the package.
- `task test` (`go test -count=2 -race ./...`) MUST pass. `-count=2` defeats result caching and
  surfaces state leaking between runs; `-race` is the only mechanical check for data races.
- Argument parsing, exit codes, and the stdout/stderr split MUST each be tested, and at least
  one test MUST invoke the built binary end to end.

Rationale: the tests are the executable form of the contract in Principle II; without them the
contract is a wish.

### IV. Thin Commands, Domain Packages

- Commands MUST be thin wrappers (cobra, or stdlib `flag` when there are no subcommands other
  than a bare `version` word): parse, validate, call, format. Business logic MUST live in packages that import no CLI
  package, so it stays testable without a terminal.
- Packages MUST be named for the domain they serve. `utils`, `helpers`, `common`, and `base`
  packages are forbidden, because they attract unrelated code and grow into import cycles.
- Runtime assets (templates, default configs, static files) MUST be embedded with `//go:embed`,
  so the binary never depends on files sitting next to it.

Rationale: separating the CLI shell from the domain keeps logic reusable and testable, and
keeps the binary self-contained.

### V. Simple, Explicit Go

- Code MUST prefer concrete types. A generic MUST NOT be introduced before the same concrete
  implementation exists for three or more types, because generics cost readability and most
  Go code never needs them.
- Errors MUST be wrapped with `fmt.Errorf("...: %w", err)` and never flattened into a string,
  so callers can still use `errors.Is` and `errors.As`.
- Logging MUST use `log/slog` at a level the user controls, and MUST record only what the
  reader can act on.
- `task lint` (golangci-lint v2) MUST pass. "Idiomatic" means whatever the linter accepts;
  disagreements are settled by editing `.golangci.yml`, not by arguing in review.

Rationale: a single author must be able to read and audit every line; explicitness and a
mechanical style arbiter keep that possible.

### VI. Safe to Interrupt

- Every long-running operation MUST take a `context.Context` and cancel cleanly on
  `SIGINT`/`SIGTERM`.
- All I/O MUST have an explicit timeout. Retries MUST be bounded and backed off.

Rationale: a tool used in pipes and scripts must never hang and must always be safe to stop.

## Build, Release & Supply Chain

**Build**

- The tool MUST build as a single static binary with `CGO_ENABLED=0`, so it runs on any host
  with no runtime dependency. A default build is not static: `net` and `os/user` link cgo
  unless it is disabled.
- Builds MUST pass `-trimpath` and use the toolchain pinned in `go.mod` (`go` and `toolchain`
  directives) and `mise.toml`, so rebuilding a tag produces the same binary.

**Release**

- Releases MUST go through goreleaser v2 (`task release`) and publish SHA-256 checksums and an
  SBOM. Build targets live in `.goreleaser.yml`, never in ad-hoc commands.

**Code generation**

- Generators MUST be declared as Go tool dependencies (`go get -tool <module>`, Go 1.24+ `tool`
  directive) rather than installed globally, so every checkout runs the same versions.
- Each generator MUST be invoked by a `//go:generate go tool <name> ...` directive placed next
  to the file it produces, and the whole tree MUST regenerate with `go generate ./...`.
- Generated files MUST be committed, so a clean checkout builds without running any generator.

**Dependencies**

- The standard library MUST be the first choice. Adding a direct module dependency MUST be
  proposed to the author and approved before it lands, so the dependency surface stays small
  enough for one person to audit.
- A new dependency MUST be MIT, BSD, or Apache-2.0 licensed and actively maintained; copyleft
  or unlicensed modules MUST NOT be added.

**Secrets**

- Secrets MUST NOT appear in the repository, in config files, in logs, or in error messages.
  They come from the environment or from an external command named in the config (e.g. a
  password manager), run without a shell and with a timeout.

## Development Workflow & Quality Gates

A change is mergeable only when all of the following hold:

1. The failing test was written first and now passes (Principle III).
2. `task lint` passes with the committed `.golangci.yml`.
3. `task test` (`go test -count=2 -race ./...`) passes.
4. `go generate ./...` produces no diff against the committed tree.
5. `CGO_ENABLED=0 go build -trimpath ./...` succeeds.
6. No direct dependency was added without the author's recorded approval.
7. Any change to stdout format, `--output=json` schema, flags, or exit codes is called out as a
   contract change in the spec, plan, and commit message.

Every `/speckit-plan` MUST fill its Constitution Check against these principles before design,
and re-check after design. Violations go in the plan's Complexity Tracking table with the
trade-off stated, or the plan is revised.

## Governance

- This constitution overrides the assistant's defaults and any other practice in this
  repository. A spec, plan, or patch that violates a principle MUST be revised to comply, or
  the conflict MUST be raised to the author with the trade-off stated.
- When two principles conflict, composability and a stable output contract (Principles I and
  II) win: breaking a pipe is worse than an awkward internal design.
- When a case is genuinely unaddressed here, the assistant MUST ask rather than guess, and
  SHOULD choose the simpler design, with fewer dependencies and more tests.
- **Amendments** are made by the author, through `/speckit-constitution`, in a commit that
  updates the Sync Impact Report and propagates changes to `.specify/templates/`.
- **Versioning** follows semantic versioning: MAJOR for removing or redefining a principle,
  MINOR for adding a principle or section or materially expanding guidance, PATCH for wording
  and clarifications.
- **Compliance review**: every plan's Constitution Check and every review MUST verify the
  gates in Development Workflow & Quality Gates.

**Version**: 1.2.0 | **Ratified**: 2026-10-19 | **Last Amended**: 2026-10-19
