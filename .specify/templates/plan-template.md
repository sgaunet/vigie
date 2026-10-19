# Implementation Plan: [FEATURE]

**Branch**: `[###-feature-name]` | **Date**: [DATE] | **Spec**: [link]

**Input**: Feature specification from `/specs/[###-feature-name]/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

[Extract from feature spec: primary requirement + technical approach from research]

## Technical Context

<!--
  ACTION REQUIRED: Replace the content in this section with the technical details
  for the project. The structure here is presented in advisory capacity to guide
  the iteration process.
-->

**Language/Version**: [e.g., Python 3.11, Swift 5.9, Rust 1.75 or NEEDS CLARIFICATION]

**Primary Dependencies**: [e.g., FastAPI, UIKit, LLVM or NEEDS CLARIFICATION]

**Storage**: [if applicable, e.g., PostgreSQL, CoreData, files or N/A]

**Testing**: [e.g., pytest, XCTest, cargo test or NEEDS CLARIFICATION]

**Target Platform**: [e.g., Linux server, iOS 15+, WASM or NEEDS CLARIFICATION]

**Project Type**: [e.g., library/cli/web-service/mobile-app/compiler/desktop-app or NEEDS CLARIFICATION]

**Performance Goals**: [domain-specific, e.g., 1000 req/s, 10k lines/sec, 60 fps or NEEDS CLARIFICATION]

**Constraints**: [domain-specific, e.g., <200ms p95, <100MB memory, offline-capable or NEEDS CLARIFICATION]

**Scale/Scope**: [domain-specific, e.g., 10k users, 1M LOC, 50 screens or NEEDS CLARIFICATION]

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Source: `.specify/memory/constitution.md`. Mark each gate ✅, or ❌ with an entry in
Complexity Tracking.

- [ ] **I. Single purpose**: the feature serves the tool's one job; no new unrelated mode.
- [ ] **II. Pipe-safe contract**: data on stdout only (`--output=text|json`), logs/errors/progress
      on stderr; exit codes 0/1/2 (plus any tool-specific code ≥ 3) documented in `--help` and
      tested; `NO_COLOR`, no TTY effects when stdout is not a TTY, `--quiet`/`--verbose`;
      precedence flags > env > config > detected > defaults stated in `--help` (fallback-only
      env vars below detection called out); destructive actions gated by `--yes` or confirmation; any output/exit-code change
      flagged as a contract change.
- [ ] **III. Test-first**: failing tests planned before implementation; black-box
      `package <pkg>_test` (+ `export_test.go` if needed); arg parsing, exit codes, and stream
      split tested; at least one end-to-end test against the built binary.
- [ ] **IV. Thin commands**: commands only parse/validate/call/format; logic in domain packages
      importing no CLI package; no `utils`/`helpers`/`common`/`base`; assets via `//go:embed`.
- [ ] **V. Simple Go**: concrete types (no generic before 3 concrete copies); errors wrapped with
      `%w`; `log/slog` at a user-controlled level; `task lint` passes.
- [ ] **VI. Safe to interrupt**: long operations take `context.Context` and cancel on
      `SIGINT`/`SIGTERM`; every I/O has a timeout; retries bounded with backoff.
- [ ] **Build & supply chain**: `CGO_ENABLED=0`, `-trimpath`, pinned toolchain; generators as
      `go tool` deps with committed output; no new direct dependency without author approval
      (MIT/BSD/Apache-2.0, maintained); no secrets in repo, config files, logs, or errors; secrets
      only from the environment or a config-named command (no shell, timeout).

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit-plan command output)
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)
<!--
  ACTION REQUIRED: Replace the placeholder tree below with the concrete layout
  for this feature. Delete unused options and expand the chosen structure with
  real paths (e.g., apps/admin, packages/something). The delivered plan must
  not include Option labels.
-->

```text
# [REMOVE IF UNUSED] Option 1: Go CLI (DEFAULT, per constitution)
cmd/vigie/
├── main.go              # exit-code wiring (0/1/2), signal context
└── main_test.go         # end-to-end test against the built binary
internal/
├── cli/                 # thin cobra/flag commands: parse, validate, call, format
└── [domain]/            # business logic, named for its domain; imports no CLI package
    ├── [domain].go
    ├── [domain]_test.go # package [domain]_test (black box)
    └── export_test.go   # only if tests need internals
go.mod                   # go + toolchain directives
mise.toml
Taskfile.yml             # task lint / task test / task release
.golangci.yml
.goreleaser.yml

# [REMOVE IF UNUSED] Option 2: Web application (when "frontend" + "backend" detected)
backend/
├── src/
│   ├── models/
│   ├── services/
│   └── api/
└── tests/

frontend/
├── src/
│   ├── components/
│   ├── pages/
│   └── services/
└── tests/

# [REMOVE IF UNUSED] Option 3: Mobile + API (when "iOS/Android" detected)
api/
└── [same as backend above]

ios/ or android/
└── [platform-specific structure: feature modules, UI flows, platform tests]
```

**Structure Decision**: [Document the selected structure and reference the real
directories captured above]

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
