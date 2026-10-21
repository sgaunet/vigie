# Feature Specification: vigie — GitHub Support

**Feature Branch**: `003-github-support` (spec directory; no branch created)

**Created**: 2026-10-21

**Status**: Draft

**Input**: User description: "Make the tool github compatible"

Features 001 ([spec](../001-gitlab-status-line/spec.md)) and 002
([spec](../002-multi-repo-table/spec.md)) put GitHub explicitly out of scope: a clone of a
GitHub repository gets a "not found" or network error, and the folder scan skips it as an
"unknown host". This feature makes vigie show the same status for a GitHub repository as for
a GitLab project, in the single line, the watch mode and the folder table. It reuses the
detection, fields, output rules and exit codes of features 001 and 002 unless stated
otherwise here.

The five fields keep their names on both platforms. On GitHub they read:

| Field | GitLab (features 001, 002) | GitHub (this feature) |
|---|---|---|
| `branches` | active/total branches | active/total branches |
| `pipelines` | running/pending pipelines | running/pending GitHub Actions workflow runs |
| `last` | latest pipeline on the branch | combined result of the workflow runs of the branch's latest commit (FR-011) |
| `mrs` | open merge requests | open pull requests |
| `issues` | open issues | open issues, pull requests excluded |

## Clarifications

### Session 2026-10-21

- Q: What does the `last` field report on GitHub? → A: The combined result of all workflow
  runs of the branch's latest commit, worst state winning; not the single most recent run, and
  not GitHub's overall check result (which includes CI outside GitHub Actions).
- Q: Is GitHub Enterprise Server in scope? → A: Yes: github.com works with no configuration,
  and Enterprise Server instances are declared in the config `hosts` map with a `platform` key.
- Q: Which token goes to a GitHub Enterprise Server host? → A: GitHub CLI convention:
  `GH_TOKEN`/`GITHUB_TOKEN` only to github.com; an Enterprise Server host uses its entry's
  `token_env`/`token_command`, else `GH_ENTERPRISE_TOKEN`, else `GITHUB_ENTERPRISE_TOKEN`.
- Q: Do runs started by a pull request count toward `last`? → A: Yes: every run of that commit
  on that branch counts, whatever started it (push, pull request, schedule, manual start).
- Q: In a scan, what happens to GitHub clones when no GitHub token is set? → A: With no token
  source at all for that host, they are skipped as `no token` (counted in the summary, listed
  with `-v`, no row, no error line); a token that is set but rejected or failing still marks
  the rows `auth`.
- Q: Do runs waiting for an approval count as pending in `pipelines`? → A: No: `running` and
  `pending` count only runs whose state maps to `running` or `pending` (FR-014); approval- or
  action-waiting runs (`manual`) are not counted, as GitLab manual pipelines are not.
- Q: Should text output show a repository's platform? → A: Yes: a per-platform icon replaces
  🗼 on the project segment (🦊 GitLab, 🐙 GitHub), and the scan table gains a `PLATFORM`
  column; this changes GitLab output too and is a contract change.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - See my GitHub repository's status at a glance (Priority: P1)

A developer working in a local clone of a github.com repository runs `vigie` with no
arguments. With only a GitHub token exported, they get the same one-line status they would get
in a GitLab clone: `owner/repo`, active/total branches, running and pending workflow runs, the
CI result of their current branch, open pull requests and open issues.

**Why this priority**: this is the feature. Today vigie fails in every GitHub clone; once this
story works, a GitHub user gets all of feature 001's value.

**Independent Test**: in a clone of a known public github.com repository, with only `GH_TOKEN`
exported, run `vigie` and compare each value with the repository's GitHub web pages (branches,
Actions, pull requests, issues).

**Acceptance Scenarios**:

1. **Given** a clone whose `origin` points to `github.com/owner/repo` and a valid token in
   `GH_TOKEN` or `GITHUB_TOKEN`, **When** the user runs `vigie` on a terminal, **Then** one line
   is printed with the GitHub icon 🐙 and `owner/repo`, followed by the five fields in default
   order, and the exit code is 0.
2. **Given** the user is on branch `main` and the CI of its latest commit succeeded 3 minutes
   ago, **When** they run `vigie`, **Then** the last field shows the success marker, `main` and
   `3m`.
3. **Given** the latest commit of the branch started a lint workflow that succeeded and a test
   workflow that failed, **When** the user runs `vigie`, **Then** the last field shows the
   failure marker; while either run is still in progress, it shows the running marker.
4. **Given** a repository with 12 open issues and 5 open pull requests, **When** the user runs
   `vigie`, **Then** the mrs field shows 5 and the issues field shows 12: pull requests are not
   counted as issues.
5. **Given** a repository with issues disabled, **When** the user runs `vigie`, **Then** the
   issues field shows `–`, the other fields are shown normally, and the exit code is 0.
6. **Given** a repository where GitHub refuses to list workflow runs (GitHub Actions disabled,
   or the token lacks read access to Actions), **When** the user runs `vigie`, **Then** the
   pipelines and last fields show `–`, the other fields are shown, and the exit code is 0.
   When GitHub lists runs despite Actions being disabled, the fields show what it returns.
7. **Given** no GitHub token is available from any source, even if `GITLAB_TOKEN` is set,
   **When** the user runs `vigie` in a GitHub clone, **Then** nothing is printed on stdout,
   stderr shows one line naming `GH_TOKEN` and `GITHUB_TOKEN` with a hint on creating a
   read-only token, no request carries the GitLab token, and the exit code is 3.
8. **Given** a private repository the token cannot see, **When** the user runs `vigie`,
   **Then** the exit code is 4 and the hint says the token may lack access to that repository.

---

### User Story 2 - One table for a folder of GitLab and GitHub clones (Priority: P2)

A developer keeps GitLab and GitHub clones side by side under `~/src`. They run `vigie` there
and get one table with a row per clone, whatever the platform.

**Why this priority**: the folder table of feature 002 is where mixed platforms meet. Without
this story, every GitHub clone in the folder is silently missing from the table.

**Independent Test**: build a folder with two GitLab clones and two github.com clones, export
both tokens, run `vigie` in it and check the table has four complete rows in tree order.

**Acceptance Scenarios**:

1. **Given** a folder with GitLab and github.com clones and a token for each platform, **When**
   the user runs `vigie` there, **Then** every clone gets a row, in tree order, with its
   platform in the `PLATFORM` column and the values each platform's web pages show, and the
   exit code is 0.
2. **Given** only the GitLab token is set and no GitHub token source exists, **When** the user
   runs `vigie` in that folder, **Then** the GitLab rows are complete, the github.com clones get
   no row and no error line, the stderr summary counts them as skipped (`no token`), `-v` lists
   them, and the exit code is 0.
3. **Given** a GitHub token that github.com rejects, **When** the user runs `vigie` in that
   folder, **Then** every github.com row shows the `auth` marker, stderr holds one line for
   github.com (not one per row), the GitLab rows are complete, and the exit code is 0.
4. **Given** the same folder, **When** the user runs `vigie --host https://github.com`, **Then**
   only the github.com clones get a row, no request is sent to any GitLab host, and the GitLab
   clones count as "other host" in the stderr summary.
5. **Given** the same folder, **When** the user requests JSON output, **Then** each record
   names its platform (`gitlab` or `github`) and otherwise follows the same schema.

---

### User Story 3 - Same scripts and status bars for both platforms (Priority: P3)

A user feeds `vigie -o json` to a status bar and to `jq` scripts. The same script works on a
GitLab project and on a GitHub repository, because the records share one schema and one
vocabulary of CI states.

**Why this priority**: composability is the project's first principle; one schema across
platforms means no script has to care where a repository lives.

**Independent Test**: run `vigie -o json | jq -r .last.status` in a GitLab clone and in a
GitHub clone whose CI failed; both print `failed`.

**Acceptance Scenarios**:

1. **Given** a GitHub clone, **When** JSON output is requested, **Then** the record carries the
   same properties as for a GitLab project, plus `"platform": "github"`, and validates against
   the published schema.
2. **Given** any CI state GitHub can report, **When** it is shown, **Then** it maps to one of
   the states vigie already documents (FR-014), and the JSON `last.status` vocabulary gains no
   new value.
3. **Given** a GitHub clone and stdout is a pipe, **When** the default text output is used,
   **Then** the line is plain ASCII, as for a GitLab project.

---

### User Story 4 - Keep watching a GitHub repository within its request allowance (Priority: P4)

A developer pushes to a GitHub repository and runs `vigie -w` to follow the workflow runs.
GitHub grants each token a limited number of requests per hour, shared with every other tool
using it. vigie stays well within that allowance and, if it runs out anyway, waits for it
instead of failing.

**Why this priority**: watch mode is the most request-hungry use of vigie, and on GitHub the
allowance is per hour and per token; a watch that exhausts it also breaks the user's other
GitHub tools.

**Independent Test**: run `vigie -w` for one hour on a GitHub repository at the default interval
and check the requests used, as reported by GitHub for that token, stay within SC-005; then
point vigie at a test server that reports the allowance as exhausted and check the line goes
stale rather than failing.

**Acceptance Scenarios**:

1. **Given** watch mode on a GitHub repository at the default interval, **When** an hour has
   passed, **Then** vigie has used at most half of each hourly allowance pool (SC-005).
2. **Given** GitHub reports the allowance exhausted or asks the client to slow down, **When**
   the next refresh runs in watch mode, **Then** the last values stay on the line with the stale
   marker, vigie waits until the reset time or retry delay GitHub gave, and watching continues;
   this is never treated as an authentication failure.
3. **Given** the allowance is exhausted, **When** the user runs a one-shot `vigie`, **Then**
   nothing is printed on stdout, stderr names the time the allowance resets, and the exit code
   is 5.

---

### User Story 5 - Use a self-hosted GitHub Enterprise Server (Priority: P5)

A user's company runs GitHub Enterprise Server. They describe the instance once in the config
file, as they would a self-hosted GitLab, and `vigie` then works in its clones.

**Why this priority**: required for GitHub Enterprise Server users only; github.com users never
need it.

**Independent Test**: with a config entry for `github.example.com` holding `platform: github`
and its own `token_env`, run `vigie` in a clone of that instance and check the line shows the
repository.

**Acceptance Scenarios**:

1. **Given** a config host entry with `platform: github`, **When** the user runs `vigie` in
   one of its clones, **Then** the status line for that repository is shown, using that entry's
   token source.
2. **Given** the same entry, **When** the user scans a folder holding clones of that instance,
   **Then** those clones get rows instead of counting as "unknown host".
3. **Given** an entry with `platform: github` and no token source, `GH_TOKEN` set and no
   `GH_ENTERPRISE_TOKEN` or `GITHUB_ENTERPRISE_TOKEN`, **When** the user runs `vigie` in one of
   its clones, **Then** no request carries `GH_TOKEN`, stderr names the enterprise variables
   and the entry's `token_env`, and the exit code is 3.
4. **Given** a config file written for features 001 and 002 (no platform declared anywhere),
   **When** the user runs `vigie`, **Then** every configured host is still treated as GitLab
   and behaviour is unchanged.

---

### Edge Cases

- SSH over the HTTPS port: a remote on `ssh.github.com` (port 443) is a github.com remote.
- SSH host alias: a remote using an alias from the user's SSH config (e.g. `github-work`) is
  not recognised on its own; mapping the alias to `https://github.com` in the config `hosts`
  map makes it a github.com remote (feature 001, FR-004).
- Renamed or transferred repository: GitHub redirects the old path; vigie follows the redirect,
  shows the new `owner/repo`, and in verbose mode suggests updating the remote URL.
- Private repository the token cannot see: GitHub answers "not found" rather than "forbidden",
  so the exit code is 4 and the hint mentions token access (User Story 1, scenario 8).
- Organization enforcing single sign-on, token not authorized for it: authentication problem,
  exit 3, with a hint to authorize the token for that organization.
- A token without permission for one field only (e.g. a fine-grained token without read access
  to Actions): that field shows `–` with the reason, the other fields are shown, exit code 0.
- Empty repository (no commit yet): branches show `0/0` and the last field shows `∅`.
- No workflow ever ran on the current branch: the last field shows `∅` (JSON: `none`).
- CI that runs only for pull requests: a feature branch with an open pull request shows the
  result of the pull request's runs; one without a pull request shows `∅`.
- A scheduled run on the branch's latest commit counts like any other: a failed nightly run
  makes the last field show `failed` until a later run of that workflow on the same commit, or
  a newer commit with runs, replaces it.
- Workflow runs waiting for an approval (environment protection, first-time contributor): not
  counted in `pipelines` (FR-010); when they decide the last field, it shows the manual state
  (FR-014).
- Fork: counts are the fork's own; `--remote upstream` shows the parent repository.
- Archived repository: the project segment carries the gray `archived` tag.
- Draft pull requests are counted with the others.
- Counts GitHub reports as approximate or incomplete are never shown as exact (FR-016).
- `--project` with a path that is not `owner/repo` on a GitHub host: usage error, exit 2.
- A gist remote (`gist.github.com`) is not a repository: setup error in a single clone,
  skipped as "unknown host" in a scan.
- The allowance runs out during a folder scan: the host is latched (FR-018). Its remaining
  rows show the `network` marker without any further request, stderr gets one line for the
  host, and other hosts are unaffected. A request to slow down (secondary limit) is retried
  within `--timeout`, as for any throttling host (feature 002, FR-008a).

## Requirements *(mandatory)*

### Functional Requirements

**Recognising GitHub**

- **FR-001**: vigie MUST treat `github.com` as a GitHub host in every mode with no
  configuration. The host names `www.github.com`, `ssh.github.com` and `api.github.com` MUST
  denote the same instance.
- **FR-002**: A host entry of the config `hosts` map MUST accept a `platform` key, `gitlab`
  (the default) or `github`; with `platform: github` the entry is a GitHub Enterprise Server
  instance and takes the same per-host settings as a GitLab entry (`api` as its web base URL,
  `token_env`, `token_command`, `insecure`). Any other `platform` value MUST be a setup error
  (exit 2). A host entry whose `api` is `https://github.com` MUST be treated as github.com
  without a `platform` key. Every other host stays GitLab, so existing config files behave
  unchanged. vigie MUST NOT guess a host's platform from its name or by probing it.
- **FR-003**: `--project` MUST accept `owner/repo` or a numeric repository ID on a GitHub
  host, and with `--host https://github.com` (or a GitHub host from the config) MUST work
  outside any clone, as in feature 001 (FR-007). `GITLAB_HOST` MUST stay a GitLab-only
  fallback. A `GITLAB_HOST` naming a github.com-family host MUST be a setup error (exit 2), so
  a GitLab token is never sent to github.com.
- **FR-004**: The remote URL forms of feature 001 (FR-002) MUST be accepted for GitHub
  remotes. On a GitHub host the project path MUST have exactly two segments; any other path
  MUST be a setup error (exit 2) quoting the URL without credentials.

**Authentication and secrets**

- **FR-005**: For a GitHub host, vigie MUST take the token, in order, from the matching host
  entry's `token_env` or `token_command`, else from the default variables of that host: for
  github.com `GH_TOKEN`, then `GITHUB_TOKEN`; for an Enterprise Server host
  `GH_ENTERPRISE_TOKEN`, then `GITHUB_ENTERPRISE_TOKEN` (GitHub CLI convention). A token with
  read-only access to the repository MUST suffice.
- **FR-006**: A token taken from default variables MUST only be sent to the hosts those
  variables serve: `GITLAB_TOKEN` only to GitLab hosts, `GH_TOKEN` and `GITHUB_TOKEN` only to
  github.com, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` only to Enterprise Server
  hosts. A token from a host entry MUST only be sent to that host.
- **FR-007**: The secret rules of feature 001 (FR-009 to FR-012) MUST apply to GitHub tokens:
  never shown or logged (verbose output MAY show a masked prefix such as `ghp_****` or
  `github_pat_****`), never sent over plain HTTP unless the host is marked `insecure: true`,
  never held in the config file, and only read-only operations.
- **FR-008**: When no GitHub token is available, the one-line error MUST name the variables
  that host would accept (`GH_TOKEN` and `GITHUB_TOKEN` for github.com; `GH_ENTERPRISE_TOKEN`,
  `GITHUB_ENTERPRISE_TOKEN` and the entry's `token_env` for Enterprise Server), say how to
  create a read-only token, and mention that GitHub CLI users can set
  `token_command: ["gh", "auth", "token"]` in the config. vigie MUST NOT run any command that
  the config does not name.

**Fields on GitHub**

- **FR-009**: `branches` MUST count all branches and those whose latest commit is within
  `active_days`, as in feature 001.
- **FR-010**: `pipelines` MUST count GitHub Actions workflow runs whose state maps (FR-014) to
  `running` (in progress) and to `pending` (queued, requested, pending). Runs waiting for an
  approval or an action (`manual`) MUST NOT be counted, as GitLab pipelines waiting for a
  manual action are not.
- **FR-011**: `last` MUST report, for the reporting branch (feature 001, FR-006), the combined
  result of the workflow runs of the branch's latest commit that has any run, taken as the
  commit of the newest run on the branch (re-runs keep their creation time). Every run of that
  commit on that branch counts, whatever started it: a push, a pull request, a schedule or a
  manual start. A commit that started no run (skipped CI, path filters) leaves the previous
  result shown, as a push without a pipeline does on GitLab. For each workflow, only its most recent run of that commit counts,
  so a re-run replaces the run it repeats. Each run's state maps per FR-014, and the combined
  state is the first of these that any counted run has: `running`, `pending`, `manual`,
  `failed`, `canceled`, `unknown`, `success`, `skipped`. The field shows the combined state, the
  branch, the time of the most recent update among the counted runs, and a link to the commit's
  runs on GitHub. The JSON `last.id` is omitted on GitHub, since the result has no single
  number.
- **FR-012**: `mrs` MUST count open pull requests, drafts included.
- **FR-013**: `issues` MUST count open issues only; pull requests MUST NOT be counted, so the
  number matches the repository's Issues tab.
- **FR-014**: Every state and result GitHub reports for a workflow run MUST be shown as one of
  the existing states of feature 001 (FR-017), so the JSON `last.status` vocabulary is
  unchanged:

  | GitHub state or result | vigie state |
  |---|---|
  | queued, requested, pending | `pending` |
  | in progress | `running` |
  | waiting (for an approval), action required | `manual` |
  | success, neutral | `success` |
  | failure, timed out, startup failure | `failed` |
  | cancelled, stale | `canceled` |
  | skipped | `skipped` |
  | no run | `none` |
  | anything else | `unknown` |

- **FR-015**: A field unavailable because GitHub refuses it (issues disabled, workflow runs
  not listable) or because the token cannot read it MUST show `–` and MUST NOT hide or fail
  the other fields; the run still succeeds (feature 001, FR-015). A read-only token cannot
  tell whether GitHub Actions is disabled, so the Actions-based fields show `–` only when
  GitHub refuses the listing.
- **FR-016**: vigie MUST never show a count as exact when GitHub reports it as approximate or
  incomplete. GitHub caps filtered workflow-run listings at 1,000, so a run count of 1,000 or
  more MUST show `–` with that reason. Other counts above 10,000 MUST show as `10k+` (JSON
  `null` with `"capped": true`), as in feature 001 (FR-016).
- **FR-017**: The fetching rules of feature 001 MUST apply: all fields of one refresh fetched
  concurrently within one `--timeout` budget (FR-014), branch counts reused for up to 60 s in
  watch mode (FR-018). The project lookup MUST also finish within one `--timeout` budget, in
  single and watch mode as in a scan (constitution Principle VI); a lookup that runs out of
  time is a network failure (exit 5).

**Request allowance and errors**

- **FR-018**: When GitHub reports the token's request allowance exhausted, or asks the client
  to slow down, vigie MUST treat it as throttling and never as an authentication failure. In
  watch mode it MUST keep the last values with the stale marker and not send the next request
  before the reset time or retry delay GitHub gave; in one-shot mode it MUST exit 5 with a hint
  naming the reset time; in a scan the throttling rules of feature 002 (FR-008a) apply, and
  a host whose allowance is exhausted is latched: its remaining rows show the `network`
  marker without any further request, and stderr gets one line for the host, even with
  `--quiet`.
- **FR-019**: Error classes MUST map to the exit codes of feature 001 (FR-035): token missing,
  rejected, or not authorized for the organization's single sign-on → 3; repository not found
  or not visible to the token → 4, with a hint mentioning token access; network failures and
  an exhausted allowance in one-shot mode → 5.
- **FR-020**: When GitHub reports that a repository moved, vigie MUST follow it to the new
  location and report the new `owner/repo`; in verbose mode it MUST suggest updating the
  remote URL.

**Output**

- **FR-021**: Text output for a GitHub repository MUST follow feature 001 (FR-019 to FR-025):
  same segments, field icons, ASCII labels, colors and field names, with `owner/repo` as the
  project path. The project segment's icon MUST name the platform on both platforms: 🦊 for
  GitLab and 🐙 for GitHub, replacing 🗼. Where icons are not shown (`--no-emoji`, stdout not a
  terminal), the project segment stays without a label, as today.
- **FR-021a**: The scan table (feature 002, FR-010) MUST gain a `PLATFORM` column between `DIR`
  and `PROJECT`, holding `gitlab` or `github` in every mode, failed rows included. The column
  is never truncated; the narrow-terminal rule of feature 002 (`PROJECT` first, then `DIR`) is
  unchanged.
- **FR-022**: Every JSON record, in every mode and for both platforms, MUST carry a `platform`
  property, `gitlab` or `github`. The published schema (`docs/status.schema.json`) MUST be
  updated accordingly; no other property changes.
- **FR-023**: `--help` and the README MUST describe GitHub support, the field mapping above, the
  token variables `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and
  `GITHUB_ENTERPRISE_TOKEN`, and the token precedence for each kind of host.

**Folder scan (feature 002)**

- **FR-024**: In a scan, clones on a GitHub host (FR-001, FR-002) MUST be recognised and get a
  row, fetched with that platform's token; they no longer count as "unknown host".
- **FR-025**: The scan rules of feature 002 MUST apply per host unchanged: bounded requests in
  flight per host (FR-008), throttling (FR-008a), and a rejected token or failing token source
  marking every row of that host `auth` with one stderr line for the host (FR-009). `--host
  https://github.com` MUST keep only github.com clones (FR-005a).
- **FR-025a**: When a GitHub host has no token source at all (no `token_env` or
  `token_command` in its entry, and none of its default variables set, FR-005), its clones
  MUST be skipped with the reason `no token`: no row, no request, no error line. They MUST be
  counted in the stderr summary and listed with `--verbose`, as the other skip reasons of
  feature 002 (FR-006). A `token_env` naming an unset variable is a failing source (FR-025),
  not a missing one. GitLab hosts keep the feature 002 behaviour. If no row is left, the run
  ends as feature 002 does when nothing is listed (exit 2).

**Compatibility and contract**

- **FR-026**: For GitLab projects, every behaviour of features 001 and 002 MUST stay unchanged,
  except:
  - the added JSON `platform` property, the 🦊 project icon (FR-021) and the `PLATFORM` table
    column (FR-021a);
  - GitHub clones now listed in a scan, and the "nothing listed" message naming both platforms
    (`no GitLab or GitHub repository found`);
  - a server retry delay honoured up to 1 h in watch mode (was 5 min);
  - a project lookup that stalls ends with exit 5 after `--timeout` (FR-017) instead of hanging;
  - a `GITLAB_HOST` naming github.com is a setup error (FR-003).

  Existing config files MUST keep working without edits, except a file that names one host
  twice (e.g. `GitLab.example.com` and `gitlab.example.com`), or that leaves a remote host
  matching the `api` of several entries with no entry keyed by that host. vigie picks one of
  those entries at random today; this becomes a setup error (exit 2).
- **FR-027**: The following MUST be recorded as contract changes in the plan and the commit
  message (constitution, Development Workflow gate 7): the JSON `platform` property, the
  per-platform project icon replacing 🗼, the `PLATFORM` table column, GitHub clones listed in
  a scan, the new `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and
  `GITHUB_ENTERPRISE_TOKEN` variables, the new `platform` key of a config host entry, the new
  `no token` skip reason in a scan, the "nothing listed" message naming both platforms, the
  omitted `last.id` on GitHub records, and the watch-mode retry delay of up to 1 h.

### Key Entities

- **Platform**: the kind of service hosting a repository, GitLab or GitHub. It decides how the
  fields are read, which default token variables apply, and how errors are classified.
- **Host profile** (feature 001): gains a platform; GitLab unless the entry sets
  `platform: github` or the host is github.com.
- **Project**: on GitHub, a repository identified by `owner/repo` or a numeric ID, with web URL,
  default branch and archived flag.
- **Workflow run**: one run of a GitHub Actions workflow, with its branch, commit, state,
  result, last update time and link; what the `pipelines` and `last` fields are made of on
  GitHub.
- **Request allowance**: the number of requests a user may still send to a GitHub host in the
  current hour, in two pools (REST requests, GraphQL points) shared by all of the user's
  tokens, and when each resets.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A github.com user goes from installed binary to a correct status line by exporting
  one token, with zero other configuration steps.
- **SC-002**: In a clone of a typical github.com repository (up to 100 branches), a one-shot run
  prints the status line in under 2 seconds in at least 95% of runs on a normal connection.
- **SC-003**: In a folder holding 10 GitLab and 10 github.com clones, with both tokens exported,
  one command prints 20 rows whose values match each platform's web pages.
- **SC-004**: Across every documented scenario, no GitLab token reaches a GitHub host, no GitHub
  token reaches a GitLab host, no github.com token from default variables reaches an Enterprise
  Server host, and no token string appears in stdout, stderr or files written (verified on
  recorded requests and captured output).
- **SC-005**: A one-hour watch session on a GitHub repository with up to 100 branches, at the
  default interval, uses at most 2,500 billed requests of each GitHub allowance pool (REST
  requests and GraphQL points), half of each pool's hourly allowance for an authenticated
  user; the allowance is per user, shared by all of the user's tokens.
- **SC-006**: When the allowance is exhausted, no run reports an authentication error, and watch
  mode shows fresh values again within one interval after the allowance resets.
- **SC-007**: Every JSON record emitted for a GitHub repository validates against the published
  schema, and a `jq` filter written for GitLab records (e.g. `select(.last.status=="failed")`)
  gives the right answer on GitHub records without change.
- **SC-008**: For GitLab projects, the output of every feature 001 and 002 scenario is unchanged,
  apart from the changes FR-026 allows.

## Assumptions

- A token is required for every GitHub request, public repositories included: the allowance
  GitHub grants without a token is too small for watch mode or a folder scan, and requiring one
  keeps behaviour identical on both platforms.
- vigie does not read the GitHub CLI's stored login on its own; users reuse it by naming
  `gh auth token` as a `token_command` (constitution: only commands named in the config run).
- `GH_TOKEN` ranks above `GITHUB_TOKEN`, and `GH_ENTERPRISE_TOKEN` above
  `GITHUB_ENTERPRISE_TOKEN`, following the GitHub CLI convention, so a user's existing setup
  works as is.
- Field names, JSON keys, field icons, ASCII labels and field column headers stay the same on
  both platforms (`mrs` holds pull requests, `pipelines` holds workflow runs): they are a
  public contract, and a scan table shows both platforms in one column. Only the project icon
  and the `PLATFORM` column tell the platforms apart. The README explains the mapping.
- Only GitHub Actions counts as CI for the `pipelines` field; CI services outside GitHub do not
  create workflow runs.
- Gitea, Forgejo, Bitbucket and other platforms stay out of scope; their clones keep being
  skipped as "unknown host" in a scan.
- vigie stays read-only and writes nothing to disk (feature 001, FR-012); GitHub support adds no
  write action.
- No GitHub counterpart to `GITLAB_HOST` (such as `GH_HOST`) is added; outside a clone, a GitHub
  instance is selected with `--host`.
- Whether GitHub support needs a new direct dependency is a plan-time decision, subject to the
  author's approval (constitution, Dependencies). Planning settled it: none is needed.
- GitHub Enterprise Cloud with data residency (`*.ghe.com`) uses a different API address
  layout and is out of scope for this feature.
- Watch mode honours a server-provided retry delay of up to one hour (feature 001 capped it at
  five minutes), on both platforms, so GitHub's reset time is respected.

### Planning amendments (2026-10-21)

Made during `/speckit-plan` ([research.md](./research.md), G15): US1-6 and FR-015 (Actions
disabled is only visible when GitHub refuses the listing), FR-016 (run counts of 1,000 or
more), FR-011 (newest run's commit), FR-018 (scan latch on an exhausted allowance), SC-005
(per allowance pool, up to 100 branches), and the last two assumptions above.

Made after `/speckit-analyze` (2026-10-21): FR-017 (the project lookup runs under
`--timeout`, closing research G16), the edge case of an allowance running out during a scan
(aligned with FR-018's latch), FR-003 (`GITLAB_HOST` on github.com refused), FR-026 (the
complete list of GitLab-visible changes, and the two ambiguous config files that become
errors), FR-027 (aligned with the eight contract changes), and SC-008 (refers to FR-026).
