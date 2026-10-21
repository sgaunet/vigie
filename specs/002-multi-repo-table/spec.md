# Feature Specification: vigie — Multi-Repository Status Table

**Feature Branch**: `002-multi-repo-table` (spec directory; no branch created)

**Created**: 2026-10-21

**Status**: Draft

**Input**: User description: "by default, vigie searches if the current directory is a git
repository... it could be great that if the current repository is not a git repository, it
searches recursively all git repositories under the current folder to print a table of all sons
repositories (ignoring if repo are not from gitlab)"

This is the "Multi-project view" item of the feature 001 roadmap
([input-design.md](../001-gitlab-status-line/input-design.md)). It builds on feature 001
([spec](../001-gitlab-status-line/spec.md)) and reuses its detection, authentication, fields,
output rules and exit codes unless stated otherwise here.

## Clarifications

### Session 2026-10-21

- Q: Which repositories count as GitLab in scan mode? → A: Known hosts only: `gitlab.com`, the
  config `hosts` map, `--host` and `GITLAB_HOST`; no name heuristic, no probing (`--host` then
  refined into a filter, below).
- Q: Does `--watch` work with the table? → A: No; out of scope for this feature, usage error
  (exit 2) with a hint.
- Q: What does `--host` do during a scan? → A: It filters: only repositories whose remote
  resolves to that host are listed; others are skipped as "other host".
- Q: What does a scan do when a host answers "too many requests"? → A: Bounded retry within the
  row's `--timeout`, honoring the server's retry delay, and lower that host's concurrency for the
  rest of the run.
- Q: What feedback while a scan runs? → A: A `· done/total` counter on stderr, redrawn in place
  and erased before the table, only when stderr is a terminal and not `--quiet` (then refined:
  stdout must be a terminal too, constitution Principle II).
- Q: (planning) When every row fails, what goes on stdout? → A: Nothing, as in feature 001
  after a fatal error; stderr names each failing host and each other failed repository.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See all my GitLab projects from a parent folder (Priority: P1)

A developer keeps their clones under a folder such as `~/src`. They run `vigie` in that folder,
which is not itself a git repository. Instead of the current "not a git repository" error, vigie
finds every git repository below the folder, keeps those whose remote points to a GitLab host,
and prints one table: one row per repository with its location, project path and the same fields
as the single-project line.

**Why this priority**: this is the feature. A single command replaces running vigie in each
clone, or opening each project's page.

**Independent Test**: create a folder holding two clones of known GitLab projects, one clone of
a non-GitLab project and one plain directory; run `vigie` in it and check the table has exactly
the two GitLab rows, with the counts shown in the GitLab web interface.

**Acceptance Scenarios**:

1. **Given** a folder that is not a git repository and contains three GitLab clones at various
   depths, **When** the user runs `vigie` there, **Then** stdout holds a header row and three
   data rows, one per clone, and the exit code is 0.
2. **Given** each row, **When** it is printed, **Then** it shows the clone's directory relative
   to the scanned folder, the project path, and one column per requested field, in `--fields`
   order, with the same values and colors the single-project line would show for that clone;
   the per-field icons and labels move to the column header (FR-011).
3. **Given** clones whose remote points to a host that is not recognised as GitLab (see FR-005),
   **When** the user runs `vigie`, **Then** those clones get no row, no request is sent to their
   host, and the number of skipped repositories is reported on stderr.
4. **Given** the user runs `vigie` inside a git repository, **When** that repository has nested
   repositories below it, **Then** behaviour is unchanged from feature 001: the single status
   line of the enclosing repository is printed.
5. **Given** `--project` is passed outside any repository, **When** the user runs `vigie`,
   **Then** behaviour is unchanged from feature 001 and no scan takes place.
6. **Given** a folder with no git repository below it, or only non-GitLab ones, **When** the
   user runs `vigie`, **Then** nothing is printed on stdout, a one-line error with a hint is
   printed on stderr, and the exit code is 2.

---

### User Story 2 - One broken repository does not hide the others (Priority: P2)

Among a dozen clones, one points to a project that was deleted, one lives on an instance whose
token has expired, and one has issues disabled. The user still gets the full table; the broken
rows say what is wrong instead of aborting the run.

**Why this priority**: with many repositories, at least one is usually in a bad state. Without
per-row failure handling the table would be unusable in practice.

**Independent Test**: in a scan folder, add a clone whose remote points to a non-existent
project on a recognised host; run `vigie` and check that row shows a "not found" marker while
every other row is complete and the exit code is 0.

**Acceptance Scenarios**:

1. **Given** one repository's project is not found, **When** the table is printed, **Then** that
   row shows the project path and a short error marker in place of the fields, the other rows
   are complete, and the exit code is 0.
2. **Given** the token of one host is rejected, **When** the table is printed, **Then** every
   row on that host shows an authentication marker, rows on other hosts are complete, and a
   single error line per host (not per row) is printed on stderr.
3. **Given** a field the project does not offer, **When** its row is printed, **Then** that cell
   shows `–` as in feature 001.
4. **Given** every row failed, **When** the run ends, **Then** nothing is printed on stdout,
   stderr holds one line per failing host and per other failed repository, and the exit code
   reflects the failure as described in FR-016.

---

### User Story 3 - Feed the table to scripts (Priority: P3)

A user wants to find, from a script, every clone whose last pipeline failed. They request JSON
output and get one machine-parseable record per repository on stdout.

**Why this priority**: keeps the tool composable (constitution, Principle I), but the table
already delivers the main value.

**Independent Test**: run `vigie -o json | jq -r 'select(.last.status=="failed") | .dir'` in a
scan folder and check the printed directories match the failing projects.

**Acceptance Scenarios**:

1. **Given** JSON output in scan mode, **When** the run ends, **Then** stdout holds one JSON
   object per row, one per line, each following the feature 001 record schema plus the
   repository's relative directory.
2. **Given** a failed row, **When** JSON output is requested, **Then** its record carries the
   project path (when known), `null` fields and the failure in `errors`.
3. **Given** stdout is a pipe or a file, **When** the default text output is used, **Then** the
   table contains no ANSI color codes and no emoji, and columns are separated so that each row
   stays on one line.

---

### User Story 4 - Control how deep and where vigie looks (Priority: P4)

A user runs vigie from their home folder, which holds thousands of directories. They limit the
scan depth so the run stays fast, point vigie at a specific folder with `--dir`, or keep only
the clones of one GitLab instance with `--host`.

**Why this priority**: comfort and safety for large trees; the defaults already suit a typical
"folder of clones".

**Independent Test**: build a tree with clones at depths 1, 3 and 6; run `vigie --depth 3` and
check only the first two appear.

**Acceptance Scenarios**:

1. **Given** `--dir <folder>` pointing to a non-repository folder, **When** the user runs
   `vigie`, **Then** that folder is scanned and directories in the table are relative to it.
2. **Given** `--depth N`, **When** the scan runs, **Then** repositories deeper than N levels
   below the scanned folder are not listed.
3. **Given** a repository was found, **When** the scan continues, **Then** vigie does not look
   for further repositories inside it (nested clones and submodules are not listed separately).
4. **Given** a directory that cannot be read, **When** the scan reaches it, **Then** it is
   skipped, a warning appears on stderr in verbose mode, and the scan continues.
5. **Given** clones on gitlab.com and on `gitlab.example.com`, **When** the user runs
   `vigie --host https://gitlab.example.com`, **Then** only the `gitlab.example.com` clones get
   a row, no request goes to gitlab.com, and the others count as "other host" on stderr.

---

### Edge Cases

- Symbolic links to directories are not followed, so a link loop cannot make the scan run
  forever and a clone is never listed twice through a link.
- Hidden directories (name starting with `.`) are not scanned.
- A repository without the selected remote (`--remote`, default `origin`) is skipped with the
  reason `no remote "<name>"` and counted in the stderr summary; in verbose mode its directory
  and reason are listed.
- Two clones of the same project in different directories produce two rows.
- A host throttles the scan: rows on that host are retried more slowly (FR-008a); rows on other
  hosts are unaffected; only rows still throttled at the end of their time budget show the
  network marker.
- Git worktrees of a repository found by the scan are listed as their own rows when they sit
  below the scanned folder.
- A bare repository has no working copy and no current branch; it is skipped.
- Rows are in tree order of their relative directory (depth-first, siblings sorted by name, so
  `a/x` comes before `a-b`), so two runs on the same tree list rows in the same order.
- A terminal narrower than the table: the project column is truncated from the left
  (`…/sub/project`) first, then the directory column, so each row fits on one line.
- Ctrl+C during the scan or the fetches stops at once, prints nothing more on stdout, erases the
  progress counter, restores the terminal, and reports a runtime failure, as a one-shot run does in feature 001.
- `--watch` in scan mode: usage error (exit 2), with a hint to run `vigie -w` inside one clone
  (FR-019).

## Requirements *(mandatory)*

### Functional Requirements

**Triggering the scan**

- **FR-001**: When the working directory (or `--dir`) is not inside a git repository and no
  `--project` is given, vigie MUST scan that directory for git repositories instead of failing
  with "not a git repository".
- **FR-002**: When the working directory is inside a git repository, or `--project` is given,
  vigie MUST behave exactly as in feature 001; no scan takes place.

**Scanning**

- **FR-003**: The scan MUST descend at most `--depth` levels below the scanned directory
  (default 4); a depth of 1 means only its direct subdirectories.
- **FR-004**: The scan MUST NOT descend into a repository once found, MUST NOT follow symbolic
  links, MUST skip hidden directories, bare repositories and unreadable directories, and MUST
  continue past any of them.
- **FR-005**: A repository MUST get a row only when the host of its selected remote is
  recognised as a GitLab host: `gitlab.com`, a host named in the config `hosts` map, or the
  host of `GITLAB_HOST`. No other host is recognised, whatever its name, and no host is probed.
  vigie MUST NOT send any request, and so no token, to a host that is not recognised.
- **FR-005a**: With `--host`, the scan MUST list only repositories whose remote resolves to
  that host, either directly or through the config `hosts` map (e.g. remote
  `ssh.gitlab.example.com` mapped to `--host https://gitlab.example.com`). The `--host` host is
  recognised even when it is in neither the config nor `GITLAB_HOST`. Other repositories MUST be
  skipped with the reason "other host", and no request is sent to their hosts.
- **FR-006**: When it prints the table, vigie MUST print on stderr, unless `--quiet`, a
  one-line summary of how many repositories were found and how many were skipped; when nothing
  is listed, the error line carries these counts instead. `--verbose` MUST list each skipped
  repository with its reason (`bare`, `unreadable`, `no remote "<name>"`,
  `remote URL not understood`, `unknown host`, `other host`).

**Fetching**

- **FR-007**: For each listed repository, vigie MUST fetch the same fields as feature 001, for
  that repository's current branch (or default branch when detached), using that repository's
  host profile and token.
- **FR-008**: Repositories MUST be fetched concurrently, with a bounded number in flight at once
  so a large tree does not flood a GitLab instance; the bound MUST apply per host.
  `--timeout` MUST apply to each repository's fetch separately.
- **FR-008a**: When a host answers "too many requests" or a server error, vigie MUST retry the
  request after the server-provided retry delay, or else after an exponential backoff, only
  while the repository's `--timeout` allows; the row shows the network marker if time runs out.
  vigie MUST also halve that host's number of repositories in flight, down to one, for the rest
  of the run, and log the reduction in verbose mode. Throttling of one host MUST NOT slow the
  fetches of other hosts.
- **FR-009**: When a host rejects the token or a token command fails, vigie MUST stop sending
  requests to that host for the rest of the run and mark all of its rows as authentication
  failures.

**Output**

- **FR-010**: Text output MUST be a table with a header row, then one row per listed repository:
  relative directory, project path, then one column per field in `--fields` order. Columns MUST
  be aligned.
- **FR-011**: Cell contents MUST follow feature 001 rules: icons and colors on a terminal,
  plain ASCII labels in a pipe or with `--no-emoji`, no color with `NO_COLOR` or `--no-color`,
  `–` for an unavailable field, `10k+` for a capped count. The column header names each field,
  so cells drop the per-field icon or label; the last-pipeline cell keeps its status icon or
  label.
- **FR-012**: A row whose project could not be fetched MUST show the directory, the project path
  from the remote, and one short marker naming the failure class (auth, not found, network,
  error) in place of the fields.
- **FR-013**: The table MUST be printed once, after all fetches end, with rows in tree order of
  relative directory: depth-first, siblings sorted by name.
- **FR-014**: `--output json` MUST write one JSON object per row, one per line, in the same order
  as the table. Each object MUST follow the feature 001 record schema with one added property,
  `dir`, holding the repository's directory relative to the scanned folder. The published
  schema (`docs/status.schema.json`) MUST be updated accordingly.
- **FR-013a**: While repositories are being fetched, vigie MUST show a progress counter
  (`· 12/40`, repositories done over repositories listed) on stderr, redrawn in place and erased
  before the table or the JSON records are written. The counter MUST appear only when stdout and
  stderr are both terminals (constitution Principle II: no progress when stdout is not a TTY)
  and neither `--quiet` nor `--verbose` is set (debug lines would break the redrawn line), and
  MUST NOT emit color when `NO_COLOR` or `--no-color` applies.
- **FR-015**: Stdout MUST carry only the table or the JSON records; the scan summary, warnings
  and per-host errors MUST go to stderr, one line each, with a hint.

**Exit codes**

- **FR-016**: The scan mode MUST use the feature 001 exit codes as follows: `0` when at least
  one row was fetched successfully (fully or partially); `2` when no GitLab repository was found,
  or for a usage error such as an invalid `--depth`; when every row failed, nothing on stdout
  and the exit code all row errors share (`2` local setup, `3` auth, `4` not found, `5`
  network), or `1` when they differ. `--help` MUST describe the scan mode and these rules.

**CLI contract**

- **FR-017**: vigie MUST accept a new flag `--depth` (positive integer, default 4); a value below
  1 or not an integer MUST be a usage error (exit 2) in every mode. A valid `--depth` MUST be
  ignored, with a verbose note, when no scan takes place.
- **FR-018**: The change from "not a git repository" (exit 2) to the scan, the new `--depth`
  flag, the new JSON `dir` property, and `--host` filtering the scan when given without
  `--project` outside a clone MUST be recorded as a contract change in the plan and the commit
  message (constitution, Development Workflow gate 7).
- **FR-019**: `--watch` together with a scan MUST be a usage error (exit 2) whose hint says
  watch mode needs a single repository or `--project`; no repository is fetched.

### Key Entities

- **Scan root**: the directory being scanned; the working directory or `--dir`.
- **Discovered repository**: a git working copy found below the scan root; relative directory,
  selected remote URL, current branch, and whether it was listed or skipped (with reason).
- **Row**: one listed repository with its project, host profile and snapshot (feature 001), or
  its failure class.
- **Scan summary**: counts of repositories found, listed and skipped by reason.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In a folder holding 20 clones of existing gitlab.com projects, with the default
  `--fields` and a round-trip time to gitlab.com under 100 ms, the table is printed in under
  3 seconds in at least 95% of runs.
- **SC-002**: Scanning a tree of 10,000 directories without any repository finishes in under
  2 seconds on a local disk.
- **SC-003**: Across every documented scenario, no request is sent to a host that is not
  recognised as GitLab, and the token string appears in none of stdout, stderr or files written.
- **SC-004**: In 100% of scenarios where at least one repository is healthy, the run prints a
  row for every listed repository and exits with success.
- **SC-004a**: When a host throttles the scan with a retry delay shorter than `--timeout`, every
  row on that host is still fetched successfully, in 100% of test runs against a throttling test
  server.
- **SC-005**: Every JSON line emitted in scan mode parses with a standard JSON parser and
  validates against the published schema.
- **SC-006**: Running vigie twice on an unchanged tree lists the same rows in the same order.
- **SC-007**: Inside a git repository, the output of every feature 001 scenario is unchanged.

## Assumptions

- The scan is only a fallback for "not a git repository"; there is no flag to force a scan from
  inside a repository in this feature.
- Default depth 4 covers the common layouts `~/src/<project>` and
  `~/src/<host>/<group>/<sub>/<project>`; deeper layouts use `--depth`.
- Hidden directories are skipped because they mostly hold tool caches; a clone inside one is
  reached with `--dir`.
- The limit on concurrent repositories (FR-008) is an internal constant chosen at plan time, not
  a user setting.
- The table needs a column header (feature 001 has none) because rows are only readable with
  column names; the header is `DIR`, `PROJECT` and the `--fields` names in upper case, in every
  mode, following the usual CLI table convention.
- Branch counts (the costly field) are fetched like the other fields; with many large projects
  users can drop the field with `--fields`.
- A self-hosted GitLab instance is listed only once it appears in the config `hosts` map (or
  in `GITLAB_HOST`, or is selected with `--host`); its clones otherwise show up in the "unknown host" count,
  which tells the user what to configure.
- A scan has no overall time limit: each row has its own `--timeout`, rows wait for a free slot
  on their host, and the user can stop a long scan with Ctrl+C.
- Watch mode over the table is a possible follow-up feature, not part of this one.
- Repositories hosted on GitHub, Gitea or Bitbucket stay out of scope, as in feature 001; they
  are skipped, not reported as errors.
- `vigie` stays read-only: the scan only reads directories and git metadata and writes nothing
  to disk (feature 001, FR-012).
