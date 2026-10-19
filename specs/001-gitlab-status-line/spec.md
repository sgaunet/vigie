# Feature Specification: vigie — GitLab Project Status Line

**Feature Branch**: `001-gitlab-status-line` (spec directory; no branch created)

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "vigie — Specification" (Oct 19, 2026, @Sylvain): a CLI that, run
inside a git working copy, detects the GitLab project behind it and prints one colored status
line (branches, running pipelines, last pipeline status, open merge requests, open issues), with
a `--watch` mode that redraws the line every few seconds. The full design document is kept
verbatim in [input-design.md](./input-design.md); its API, package and dependency choices are
input for `/speckit-plan`, not requirements of this spec.

## Clarifications

### Session 2026-10-19

- Q: Keep the design's exit codes 3/4/5 despite the constitution's 0/1/2? → A: Keep 0–5;
  constitution amended (v1.1.0) to allow documented, tested tool-specific codes ≥ 3.
- Q: Which flag selects output, `--format line|plain|json` or `--output=text|json`? → A:
  `--output=text|json` (`-o`); `plain` dropped (automatic in pipes, or `--no-color --no-emoji`).
- Q: May colours/icons be forced on when stdout is not a terminal? → A: No; the override is
  dropped, text output is always plain off a terminal, and status bars use `--output json`.
- Q: In watch mode, what do JSON records do on a failed refresh, and what counts as "changed"?
  → A: Emit last good values with `"stale": true`, `stale_since` and the error; change
  detection ignores `fetched_at`, `stale_since` and ages.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See my project's status at a glance (Priority: P1)

A developer working in a local clone of a GitLab project runs `vigie` with no arguments. Without
any configuration beyond an exported access token, they get one line naming the project and
showing, in a fixed order with an icon and color per field: active/total branches, running and
pending pipelines, the status of the latest pipeline on their current branch, open merge
requests and open issues.

**Why this priority**: this is the whole product. Every other story reuses the same detection,
fetching and rendering; on its own it already replaces opening the GitLab web page.

**Independent Test**: in a clone of a known gitlab.com project, with only the token exported,
run `vigie` and compare the line with the counts shown in the GitLab web interface.

**Acceptance Scenarios**:

1. **Given** a clone whose `origin` points to a gitlab.com project and a valid token in
   `GITLAB_TOKEN`, **When** the user runs `vigie`, **Then** one line is printed with the project
   path followed by the five fields in default order, and the exit code is 0.
2. **Given** the user is on branch `main` whose latest pipeline succeeded 3 minutes ago,
   **When** they run `vigie`, **Then** the last-pipeline field shows a success marker, `main`,
   the pipeline number and `3m ago`.
3. **Given** a detached HEAD, **When** the user runs `vigie`, **Then** the last-pipeline field
   reports on the project's default branch.
4. **Given** the project has issues disabled, **When** the user runs `vigie`, **Then** the
   issues field shows `–`, every other field is shown normally, and the exit code is 0.
5. **Given** the current directory is not a git repository and no project is forced, **When**
   the user runs `vigie`, **Then** nothing is printed on stdout, a one-line error with a hint is
   printed on stderr, and the exit code is 2.
6. **Given** `GITLAB_TOKEN` is unset and no configured token source applies, **When** the user
   runs `vigie`, **Then** stderr shows `GITLAB_TOKEN is not set: create a token with the
   read_api scope and export it` and the exit code is 3.

---

### User Story 2 - Feed the status to scripts and status bars (Priority: P2)

A user wants the same information in a shell script, a tmux/Starship status bar, or piped into
`jq`. They request JSON output and get one machine-parseable object per refresh on stdout, with
nothing else mixed in.

**Why this priority**: composability through pipes is the project's first principle; JSON
output turns vigie from a display into a building block.

**Independent Test**: run `vigie --output json | jq .pipelines.running` in a clone and check the
number against GitLab; run `vigie | cat` and check the output contains no color codes or icons.

**Acceptance Scenarios**:

1. **Given** a valid setup, **When** the user requests JSON output, **Then** stdout holds exactly
   one JSON object containing project, URL, branch, each requested field, fetch time and a list
   of per-field errors (empty when none).
2. **Given** a field could not be fetched (e.g. disabled feature), **When** JSON output is
   requested, **Then** that field is `null` and the `errors` list names the field and reason,
   and the exit code is 0.
3. **Given** a count above 10,000 that the platform does not report exactly, **When** JSON is
   requested, **Then** the count is `null` with `"capped": true`; text output shows `10k+`.
4. **Given** stdout is a pipe or a file, **When** the user runs `vigie` with the default format,
   **Then** the output contains no ANSI color codes and no emoji icons.
5. **Given** any run, **When** logs, warnings or errors are produced, **Then** they appear only
   on stderr, never on stdout.

---

### User Story 3 - Keep watching while I work (Priority: P3)

A developer pushes a commit and wants to see the pipeline progress without leaving the terminal.
They run `vigie -w`; the line redraws in place every 5 seconds (or a chosen interval) until they
press Ctrl+C, which leaves the terminal clean.

**Why this priority**: high value during active work, but it builds entirely on Story 1.

**Independent Test**: run `vigie -w -i 2s` in a clone, start a pipeline from the GitLab UI,
watch the running count and last-pipeline status change, then press Ctrl+C and check the cursor
is visible and the prompt starts on a fresh line.

**Acceptance Scenarios**:

1. **Given** watch mode on a terminal, **When** each interval elapses, **Then** the same line is
   rewritten in place with fresh values, and a dim `·`, written to stderr and only when stderr
   is a terminal, marks a refresh in flight.
2. **Given** a refresh slower than the interval, **When** it completes, **Then** the next refresh
   starts one interval after it ended; refreshes never overlap.
3. **Given** a refresh fails (network error, timeout), **When** the line is redrawn, **Then** the
   last good values remain and a yellow `⚠ stale 15s` shows their age; watching continues.
4. **Given** the server answers "too many requests" or a server error, **When** watching
   continues, **Then** the interval doubles on each consecutive failure up to 60 s, honoring any
   server-provided retry delay, and returns to normal after a success.
5. **Given** the token is rejected or access to the project is denied during watch, **When** that
   response arrives, **Then** watching stops with an authentication error and exit code 3
   instead of retrying.
6. **Given** watch mode, **When** the user presses Ctrl+C or the process receives a termination
   signal, **Then** the cursor is restored, a final newline is printed, and the exit code is 0.
7. **Given** the terminal is resized, **When** the resize happens, **Then** the line is redrawn
   immediately for the new width.
8. **Given** watch mode with stdout not a terminal, **When** refreshes occur, **Then** a new line
   (or JSON object) is written only when the content changed.
9. **Given** watch mode with JSON output, **When** a refresh fails after a good one, **Then**
   exactly one record is written with the last good values, `"stale": true` and the error;
   further failed refreshes write nothing until the values or stale state change.

---

### User Story 4 - Use a self-hosted GitLab and per-host tokens (Priority: P4)

A user works with gitlab.com and a company's self-hosted instance, whose SSH host differs from
its web host and which needs its own token stored in a password manager. They describe this
once in an optional config file; afterwards `vigie` works in clones from either instance.

**Why this priority**: required for self-hosted users, but gitlab.com users never need it.

**Independent Test**: with a config mapping `ssh.gitlab.example.com` to
`https://gitlab.example.com` and a `token_command`, run `vigie` in a clone using the SSH remote
and check the line shows that project.

**Acceptance Scenarios**:

1. **Given** a remote on an SSH host mapped in the config to a different web host, **When** the
   user runs `vigie`, **Then** the project is resolved on the mapped web host.
2. **Given** a host entry with `token_env` or `token_command`, **When** the user runs `vigie`,
   **Then** the token comes from that source instead of `GITLAB_TOKEN`.
3. **Given** `--project` and `--host`, **When** the user runs `vigie` outside any repository,
   **Then** the status line for that project is shown.
4. **Given** a host whose address uses plain `http://` and is not marked `insecure: true`,
   **When** the user runs `vigie`, **Then** no request carrying the token is sent and a setup
   error explains how to mark the host insecure.
5. **Given** a config file that contains a literal token value, **When** vigie loads it, **Then**
   it refuses the file with a setup error, since tokens may only be referenced.

---

### User Story 5 - Shape the line to my taste (Priority: P5)

A user only cares about pipelines and merge requests, or uses a terminal font without emoji.
They choose which fields appear and in which order, and switch icons to short ASCII labels,
either per run or permanently in the config file.

**Why this priority**: comfort; the defaults already work.

**Independent Test**: run `vigie -f pipelines,last,mrs --no-emoji` and check only those three
fields appear, in that order, with ASCII labels.

**Acceptance Scenarios**:

1. **Given** `-f pipelines,last,mrs`, **When** the user runs `vigie`, **Then** only those fields
   appear, in that order, after the project.
2. **Given** `--no-emoji`, **When** the user runs `vigie`, **Then** icons are replaced by short
   labels (`br 12/42  ci 2r/1p  last:ok main #48213  mr 5  is 17`).
3. **Given** `NO_COLOR` is set to any non-empty value or `--no-color` is passed, **When** the
   user runs `vigie`, **Then** no ANSI color code is emitted.
4. **Given** an unknown or duplicated field name in `--fields`, **When** the user runs `vigie`,
   **Then** it exits with a usage error naming the valid fields.
5. **Given** a terminal narrower than the line, **When** the line is drawn, **Then** the project
   path is truncated from the left (`…/sub/project`) so the line fits.

---

### Edge Cases

- Empty project (no commit yet): branches show `0/0` and the last-pipeline field shows `∅`.
- No pipeline ever ran on the current branch: last-pipeline field shows `∅` (JSON: `none`).
- Fork: counts are the fork's own; `--remote upstream` shows the parent project.
- Git worktrees and submodules: detection follows git, so a submodule directory shows the
  submodule's project.
- Archived project: the project segment carries a gray `archived` tag.
- Draft merge requests are counted with the others.
- Requested remote missing: setup error listing the remotes that do exist.
- Remote URL with embedded credentials: credentials are stripped and never shown or logged.
- Unparsable remote URL: setup error quoting the URL without credentials.
- Project not found on the host (wrong host mapping, or no access): exit code 4, with a hint
  about the host mapping.
- Self-signed certificate: the system trust store is used; `SSL_CERT_FILE` adds a custom CA on
  every platform;
  there is no way to skip certificate verification.
- `--interval` below 2 s or not a valid duration: usage error.
- Default branch renamed during a long watch session: picked up within 5 minutes.
- Very large projects: listing branches is the only costly field; in watch mode its value is
  refreshed at most once a minute.
- Network failure in one-shot mode (DNS, TLS, timeout): nothing on stdout, a one-line error on
  stderr, exit code 5. In watch mode the same failure only marks the values stale.

## Requirements *(mandatory)*

### Functional Requirements

**Project detection**

- **FR-001**: vigie MUST find the repository containing the working directory (or `--dir`) and
  read the URL of the selected remote (`--remote`, default `origin`), honoring the user's git
  configuration including URL rewrites.
- **FR-002**: vigie MUST parse scp-like SSH (`git@host:group/sub/project.git`), `ssh://` URLs
  with optional port, and `https://` URLs, with optional `.git` suffix and trailing slash, into a
  host and a project path of any group depth.
- **FR-003**: vigie MUST strip credentials embedded in a remote URL before any use, display or
  log of that URL.
- **FR-004**: vigie MUST translate a remote host to a web host through the optional `hosts` map
  in the config file, and otherwise assume `https://<host>`.
- **FR-005**: vigie MUST resolve the project once at startup to obtain its identity and default
  branch, and in watch mode MUST re-resolve it every 5 minutes.
- **FR-006**: vigie MUST report on the current branch, or on the default branch when HEAD is
  detached.
- **FR-007**: `--project` (path or numeric ID) and `--host` MUST override detection and MUST
  allow running outside any repository; `GITLAB_HOST` MUST provide the host when it cannot be
  derived otherwise.

**Authentication and secrets**

- **FR-008**: vigie MUST authenticate with an access token taken, in order, from the matching
  host entry's `token_env` or `token_command`, else from `GITLAB_TOKEN`; read-only API scope
  MUST suffice.
- **FR-009**: The token MUST NOT appear on stdout, stderr, in logs, in error messages or in any
  file; verbose output MAY show only a masked form such as `glpat-****`.
- **FR-010**: vigie MUST refuse to send the token over plain HTTP unless that host is explicitly
  marked `insecure: true` in the config.
- **FR-011**: The config file MUST NOT hold a token value; it MAY only reference an environment
  variable or a command that prints the token.
- **FR-012**: vigie MUST perform read-only operations only; it MUST NOT modify anything on the
  GitLab server or in the repository, and MUST NOT write to disk.

**Metrics**

- **FR-013**: vigie MUST report, for the resolved project: active/total branches (active = last
  commit within `active_days`, default 90), running and pending pipeline counts, the latest
  pipeline on the reporting branch (status, number, last update time, link), open merge request
  count, and open issue count.
- **FR-014**: All fields of one refresh MUST be fetched concurrently and share a single time
  budget (`--timeout`, default 10 s).
- **FR-015**: A field unavailable because the feature is disabled or forbidden on the project
  MUST display `–` and MUST NOT hide or fail the other fields; the run still succeeds.
- **FR-016**: When the platform does not report an exact count (above 10,000), vigie MUST show
  `10k+` on the line and `null` with `"capped": true` in JSON, never a wrong number.
- **FR-017**: The last-pipeline field MUST recognise every GitLab pipeline status: `created`,
  `waiting_for_resource`, `preparing`, `pending`, `running`, `success`, `failed`, `canceled`,
  `skipped`, `manual`, `scheduled`, plus "no pipeline".
- **FR-018**: In watch mode the branch counts MUST be reused for up to 60 s between refreshes.

**Output**

- **FR-019**: Text output MUST print the project path then one segment per requested field,
  in `--fields` order (default `branches,pipelines,last,mrs,issues`), separated by two spaces,
  with the icons, texts and color rules of the author's design (see
  [input-design.md](./input-design.md), "Output format"), including the per-status icon and color
  table for the last-pipeline field.
- **FR-020**: Ages MUST be relative and short: `12s`, `3m`, `2h`, `4d`.
- **FR-021**: `--no-emoji` (or `emoji: false`) MUST replace icons with short ASCII labels.
- **FR-022**: vigie MUST NOT emit ANSI colors when `NO_COLOR` is set or `--no-color` is passed,
  and MUST NOT emit colors, icons, cursor control or in-flight markers when stdout is not a
  terminal. No flag, environment variable or config setting can force them on a non-terminal
  stdout; status bars and scripts use `--output json` instead.
- **FR-023**: Output MUST be selected with `--output=text|json` (short `-o`, default `text`).
  `text` is the status line (FR-019), with colors and icons subject to FR-022; `json` is one
  JSON object per refresh (one per line in watch mode). There is no separate plain format:
  plain text is what `text` produces in a pipe, or with `--no-color --no-emoji` on a terminal.
  Any other value MUST be a usage error.
- **FR-024**: Stdout MUST carry only the status data; logs, warnings, errors and debug output
  MUST go to stderr. Error messages MUST fit on one line and include a hint.
- **FR-025**: `--verbose` MUST print debug information to stderr; `--quiet` MUST suppress all
  stderr output except errors; passing both MUST be a usage error.

**Watch mode**

- **FR-026**: `--watch` MUST refresh every `--interval` (default 5 s, minimum 2 s), the next
  refresh starting one interval after the previous one ended.
- **FR-027**: On a terminal, vigie MUST redraw the line in place, hide the cursor while
  watching, and redraw immediately on terminal resize. The in-flight marker MUST go to stderr
  (only when stderr is a terminal); stdout carries only the status line.
- **FR-028**: On interrupt, termination signal or crash, vigie MUST restore the cursor and print
  a final newline before exiting; interrupting watch mode MUST exit with success.
- **FR-029**: A failed refresh MUST keep the last good values and show a stale marker with their
  age; "too many requests" and server errors MUST trigger exponential backoff (interval × 2 per
  consecutive failure, capped at 60 s, honoring a server-provided retry delay).
- **FR-030**: A rejected token or denied access to the project MUST stop watch mode at once.
- **FR-031**: In JSON output, and in text output when stdout is not a terminal, watch mode MUST
  write a new record only when the content changed. "Changed" compares the field values, field
  errors, project and branch, and the stale flag; it ignores `fetched_at`, `stale_since` and the
  displayed ages.
- **FR-031a**: Every JSON record MUST carry `"stale"`. After a failed refresh, the JSON record
  MUST repeat the last good values with their original `fetched_at`, set `"stale": true`, set
  `"stale_since"` to the time of the first failed refresh in the current failure streak, and
  list the failure in `errors`. The first successful refresh afterwards MUST emit a record with
  `"stale": false` and no `stale_since`.

**CLI contract, configuration and errors**

- **FR-032**: vigie MUST accept the flags `--watch/-w`, `--interval/-i`, `--dir/-C`,
  `--remote/-r`, `--project/-p`, `--host`, `--fields/-f`, `--output/-o` (FR-023),
  `--no-color`, `--no-emoji`, `--timeout`, `--verbose/-v`, `--quiet/-q`, and a `version`
  subcommand printing version, commit and build date.
- **FR-033**: Precedence MUST follow three chains, each stated in `--help` with the environment
  variables and the exit codes:
  - Settings: flags > environment (`VIGIE_INTERVAL`, `VIGIE_CONFIG`, `NO_COLOR`) > config file
    (`$VIGIE_CONFIG`, else `$XDG_CONFIG_HOME/vigie/config.yaml`, else
    `~/.config/vigie/config.yaml`) > values detected from the repository > built-in defaults.
  - Host: `--host` > host detected from the remote (translated through the config `hosts` map)
    > `GITLAB_HOST`, a fallback-only variable (constitution v1.2.0, Principle II).
  - Token: the matching host entry's `token_env` or `token_command` > `GITLAB_TOKEN`.
- **FR-034**: The config file MUST be optional and MAY set `interval`, `fields`, `emoji`,
  `active_days` and per-host `api`, `token_env`, `token_command`, `insecure`.
- **FR-035**: Exit codes MUST be: `0` success, including partial results; `1` unexpected
  failure (bug, malformed server response); `2` local setup or usage problem (not a repository,
  remote missing or unparsable, invalid flag, insecure host refused, token in config file);
  `3` authentication problem (no token, token rejected, access to the project denied, token
  command failed); `4` project not found; `5` network problem in one-shot mode (DNS, TLS,
  timeout). Each code MUST be listed in `--help` and covered by a test.

### Key Entities

- **Project**: the GitLab project behind the repository; path (`group/sub/project`), numeric
  identity, web URL, default branch, archived flag, host it lives on.
- **Host profile**: how to reach one GitLab instance; remote host name, web/API address, token
  source (environment variable or command), insecure flag.
- **Snapshot**: the values of one refresh; per-field values or per-field error, fetch time,
  reporting branch, stale flag and stale-since time. Rendered identically in every output
  format.
- **Field**: one metric (`branches`, `pipelines`, `last`, `mrs`, `issues`) with its value, an
  "unavailable" state, and an optional "capped" flag.
- **Pipeline status**: one of the GitLab statuses listed in FR-017, or "none", each mapped to an
  icon, an ASCII label and a color.
- **Configuration**: the merged result of flags, environment, config file, detected values and
  defaults.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a clone of a typical gitlab.com project (up to 100 branches), a one-shot run
  prints the status line in under 1 second in at least 95% of runs on a normal connection.
- **SC-002**: A gitlab.com user goes from installed binary to a correct status line by exporting
  one token, with zero other configuration steps.
- **SC-003**: Across every documented scenario, the token string is found in none of stdout,
  stderr, or files written (verified by searching all captured output).
- **SC-004**: In 100% of partial-failure scenarios (any single field unavailable), all other
  fields are shown and the run reports success.
- **SC-005**: Every JSON record emitted, one-shot or watch, parses with a standard JSON parser,
  and stdout never contains a non-data byte when piped.
- **SC-006**: A one-hour watch session at the default interval averages no more than 60 API
  requests per minute, with no overlapping refreshes.
- **SC-007**: After Ctrl+C in watch mode, the process ends within 1 second and the terminal
  shows a visible cursor and a prompt on a fresh line in 100% of trials.
- **SC-008**: The same release runs without any installed runtime on Linux, macOS and Windows,
  on both x86-64 and ARM64; the only external requirement is `git` for auto-detection.

## Assumptions

- Users have `git` installed; auto-detection depends on it, `--project`/`--host` do not.
- GitLab REST API v4 is the only backend in v1; GitHub, Gitea and Bitbucket are out of scope.
- v1 is read-only and one line only: no write actions (retry, merge, issue creation), no
  full-screen interface. vigie has no destructive action, so no `--yes` confirmation is needed.
- One-shot runs interrupted by Ctrl+C stop at once, print nothing on stdout and report a runtime
  failure; only watch mode treats interruption as success, as the design states.
- An empty `NO_COLOR` is ignored, per the no-color.org convention.
- "Values detected from the repository" sit between the config file and built-in defaults in
  the precedence chain; this extends, without contradicting, the constitution's order.
- Roadmap items (`--cache` for prompts, personal filters, draft MR counts, notifications,
  `vigie open`, GraphQL, ETags, multi-project view) are out of scope for v1.
- Distribution (static binary for linux/darwin/windows × amd64/arm64, checksums, SBOM, GitLab
  Releases, Homebrew tap `sgaunet/homebrew-tools`, `go install`) follows the constitution's build
  and release rules and is detailed in the plan.
- The repository is already MIT-licensed (`LICENSE`); the module path (GitLab or GitHub hosting)
  remains open and is a plan-time decision that does not affect user-visible behaviour.
- Dependencies and the flag parser were settled with the author at plan time (research.md R3,
  R4): stdlib `flag`, plus `golang.org/x/term` and `go.yaml.in/yaml/v3`.
