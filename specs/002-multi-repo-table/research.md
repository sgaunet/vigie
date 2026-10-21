# Research: Multi-Repository Status Table

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-10-21

Feature 001's decisions ([research.md](../001-gitlab-status-line/research.md), R1–R12) still
hold. This file records only what the scan adds or changes. No new dependency is needed.

## S1. When the scan runs

- **Decision**: in `cli.Run`, when `--project` is not given, detect the checkout once with
  `repo.Detect`. If the error is `repo.ErrNotRepository` and git is installed, switch to scan
  mode; any other outcome follows feature 001 unchanged (FR-001, FR-002). `--watch` in scan
  mode is rejected with `cli.ErrUsage` before the tree is walked (FR-019). `--depth` outside
  scan mode only logs a debug note (FR-017).
- **Git missing**: `repo.Detect` already reports a missing `git` as `ErrNotRepository` with a
  different hint. A new sentinel `repo.ErrNoGit`, wrapped in that same error, lets `cli` tell
  the two apart: without git no row could be read, so the 001 error stays and nothing is
  scanned.
- **`--dir` that does not exist**: git fails with `ErrNotRepository`, so the scan starts and
  fails at once on the root with `scan.ErrRoot` (exit 2, "cannot scan …: check --dir").
- **Alternatives rejected**: a `vigie scan` subcommand, or a `--scan` flag. The spec makes the
  scan an automatic fallback, and forcing a scan from inside a clone is out of scope
  (Assumptions).

## S2. Directory walk

- **Decision**: `scan.Find(ctx, root, depth)` uses `filepath.WalkDir`. That gives stdlib
  lexical order, never follows symbolic links, and returns `SkipDir` support. For each
  directory below the root:
  - a name starting with `.`: `SkipDir` (FR-004);
  - an `os.Lstat` of `<dir>/.git` succeeds (directory, or file for worktrees and submodules):
    candidate, `SkipDir`, so nested clones and submodules are never listed (FR-004, US4-3);
  - `HEAD` file plus `objects/` and `refs/` directories: bare repository, skipped with reason
    `bare`, `SkipDir`;
  - unreadable (the walk callback receives an error): skipped with reason `unreadable`, walk
    continues;
  - at level `depth` (root = 0, its children = 1): probed but never descended into.
- **Context**: the callback checks `ctx.Err()`, so Ctrl+C stops the walk between two
  directories. A `ReadDir` or `Lstat` blocked on a hung network mount cannot be interrupted,
  so `cli` runs `Find` in a goroutine and returns on `ctx.Done()`; the abandoned walk ends
  with the process (Principle VI).
- **Termination**: the depth bound alone guarantees it, including on Windows, where Go ≥ 1.23
  reports junctions as plain directories (they are not `ModeSymlink` any more).
- **Order**: WalkDir visits depth-first in lexical order, which is tree order. Rows keep this
  order (FR-013, SC-006) and need no extra sort. In tree order, `a/b` comes before `a-b`
  because siblings are grouped; FR-013 says tree order, not plain alphabetical order (S13).
- **Performance**: one `ReadDir` plus at most four `Lstat` calls per directory, done
  sequentially. 10,000 directories take well under a second on a local SSD (SC-002). A
  parallel walk was rejected: more code, little gain on local disks.

## S3. Inspecting candidates

- **Decision**: run `repo.Detect(ctx, dir, remote, log)` (three git calls, 5 s timeout each,
  001 R9) on every candidate, 8 at a time (`scan.inspectWorkers`). Skip reasons map from its
  errors: `ErrRemoteMissing` → `no remote "<name>"`, `ErrRemoteURL` → `remote URL not
  understood`, `ErrNotRepository` (git refuses, e.g. dubious ownership or a broken worktree
  link) → `unreadable`.
- **Rationale**: this is the exact code path of single mode, including `insteadOf` rewrites
  and credential stripping (001 FR-001–FR-003). A row then shows what `vigie -C <dir>` would
  show (see S12, the equivalence test).

## S4. Host recognition and the `--host` filter

- **Decision**: `scan.Select` is a pure function of the checkouts and `config.Settings`:
  - **Without `--host`**, a remote host is recognised (FR-005) when it is `gitlab.com`, a key
    of the config `hosts` map, the host of a profile's `api`, or the host of `GITLAB_HOST`.
    The new method `config.Settings.KnownHost` holds this rule next to `Profile`, so host
    knowledge stays in `config`.
  - **With `--host H`**, a repository is kept (FR-005a) when its remote host equals H's host,
    or when the profile matched by the remote host has `api` equal to H (scheme, host and
    port). This is `config.Settings.OnHost`. Others are skipped with reason `other host`.
  - The endpoint of a kept repository comes from the unchanged `config.ResolveHost(s,
    remoteHost)`, exactly as in single mode. With `--host` that returns H, which is the 001
    override.
- **`gitlab.com`**: 001 R8 rules out a built-in default *host*, so the token never goes to a
  host the user did not name. Recognising `gitlab.com` in a remote the user cloned is not
  such a default: the remote names the host, as in single mode.
- **No probing, no name heuristic** (Clarifications): no request is sent before a host is
  recognised (SC-003).
- **Comparison**: host names are lowercase and compared without port, the way `hostKey`
  already handles profile lookup. The `--host` match compares the full base URL, because H
  may carry a port.

## S5. Fetch orchestration and per-host concurrency

- **Decision**: `scan.Fetch(ctx, repos, cfg) []status.Row`. Repositories are grouped into
  hosts, keyed by API base URL plus token source; two remote names mapped to one API share a
  host. Each host has:
  - a client and token, created lazily once (`sync.Once`), so a `token_command` runs at most
    once per host;
  - `slots`, a semaphore of repositories in flight that can shrink, starting at
    `ReposPerHost = 4` (FR-008);
  - an auth latch: the first `gitlab.ErrUnauthorized` (401) or token error is stored, and
    later repositories of that host fail with it without sending any request (FR-009). A
    `403` on one project stays a per-project failure and is not latched.
- **Per repository**: one goroutine. It acquires a slot (honouring ctx), checks the latch,
  then creates a context with `--timeout` covering both the project lookup and the fields
  (FR-008). It runs `client.Project`, picks the branch (current, or default when detached;
  001 FR-006), runs `status.Collect` and `gitlab.RefreshError`, then releases the slot and
  reports completion. `Collect`'s own timeout nests inside the repository deadline, so the
  earlier deadline wins.
- **Isolation**: slots are per host, so a slow or throttled host never holds back another
  host (FR-008a).
- **Why 4**: each repository issues up to 9 concurrent requests (4 branch pages, 2 pipeline
  counts, last, MRs, issues), so 4 repositories means at most about 36 requests to one
  instance. 20 clones take 5 waves of about 0.4 s on gitlab.com, under SC-001's 3 s. It is a
  constant, not a setting (Assumptions).
- **Alternatives rejected**: one global worker pool, where a slow host starves the others;
  `errgroup`, whose cancel-on-first-error is the opposite of what rows need.

## S6. Throttling: bounded retry and slow-down

- **Decision**: `gitlab.Options` gains `Retry bool` and `OnThrottle func()`. With `Retry` set
  and a `429` or `5xx` answer, `Client.get`:
  1. calls `OnThrottle`;
  2. waits the server's `Retry-After` if given, else `250 ms × 2^attempt` (capped at 4 s);
  3. retries, at most 4 times (5 attempts), and only when the wait ends before the context
     deadline. Otherwise it returns the classified error, as today (`ErrNetwork`), and the row
     shows the network marker (FR-008a).

  The wait uses a timer plus `ctx.Done()`, so Ctrl+C interrupts it.
- **Slow-down**: `OnThrottle` halves that host's slots (`limit = max(1, limit/2)`), at most
  once per second. A burst of `429` from one wave then halves once, not once per request. The
  change is logged at debug level. Repositories already in flight finish; the lower limit
  applies to the next ones (FR-008a).
- **Single mode is unchanged**: `Retry` is false there, so one-shot still fails fast and watch
  keeps its own backoff (001 R9). The R9 table gains one row:

  | I/O | Timeout | Retries |
  |---|---|---|
  | Scan, one repository (lookup + fields) | `--timeout` from slot acquisition | per request: ≤ 4 retries on `429`/`5xx`, `Retry-After` or 250 ms × 2ⁿ (≤ 4 s), only within the deadline |

- **Test seam**: the backoff base is a package variable that `export_test.go` sets to 1 ms,
  the same pattern as `token.commandTimeout`.
- **Alternatives rejected**: retrying a whole repository, which re-fetches fields that had
  already succeeded; a token-bucket rate limiter, or `golang.org/x/time/rate`, a new
  dependency for a limit nobody could tune correctly.

## S7. Row outcome, failure classes, exit codes

- **Row failure**: a row fails when the project lookup fails, or when
  `gitlab.RefreshError(fields)` is non-nil, which happens with a 401 on a field or when every
  field failed on the network. That is the rule one-shot already applies, in
  `cli.runner.fatal`. It moves to the `gitlab` package so both modes share it.
- **Classes** (`status.Failure`, shown in the row marker, FR-012), assigned by
  `scan.Classify`:

  | Class | Errors |
  |---|---|
  | `auth` | `token.ErrMissing`, `token.ErrCommand`, `gitlab.ErrUnauthorized`, `gitlab.ErrForbidden` |
  | `not found` | `gitlab.ErrProjectNotFound` |
  | `network` | `gitlab.ErrNetwork` |
  | `error` | anything else (http host not marked insecure, malformed answer, refused redirect) |

- **Exit code** (FR-016): if at least one row succeeded, the run returns nil and exits 0. If
  every row failed, `scan.Outcome` returns `*scan.AllFailedError`, holding the row errors and
  no `Unwrap`, so the sentinel table cannot pick one at random. `cmd/vigie/exitcode.go`
  matches it with `errors.As`, maps each row error with the existing table, and returns the
  common code, or 1 when the codes differ. That gives 3, 4 or 5 as the spec states, and 2
  when every row failed on local setup. FR-016 is amended to name code 2 (S13).
- **Nothing to list** (exit 2):
  - when no candidate is found at all, the error wraps `repo.ErrNotRepository`. Its message
    keeps the 001 prefix `not a git repository: `, so 001's `TestE2ENotARepository` still
    holds;
  - when candidates exist but none was listed: `scan.ErrNoGitLab`, whose message carries the
    skip counts.
- **Interruption**: same as one-shot. Exit 1, nothing on stdout, counter erased.

## S8. Streams and stderr lines

- **At least one row succeeded**: the counter is erased, then stdout gets the table or JSON
  lines. After that, stderr gets the scan summary (unless `--quiet`; FR-006) and one line per
  latched host, which is an error, so it is shown even with `--quiet` (US2-2). Rows that failed
  for another reason are visible in the table; `--verbose` also logs each failure and each
  skipped repository with its reason.
- **Every row failed** (author decision during planning): stdout gets nothing, as 001 says for
  a fatal error. Stderr gets one line per latched host, one `<dir>: <error>` line per other
  failed row, then the final `AllFailedError` line printed by `main`.
- **Field warnings**: single mode logs a failed field at warn level. In scan mode that moves
  to debug, because the table already shows `–` and 40 rows would otherwise flood stderr.
- **Ordering**: stderr lines come after stdout, so on a terminal they stay just above the
  prompt instead of scrolling away above the table.
- **Message formats**: see [contracts/cli.md](./contracts/cli.md#stderr-in-scan-mode).

## S9. Progress counter

- **Decision**: `render.Progress` writes `\r ESC[2K · 12/40` (dim when colour is on) to stderr
  each time a repository completes; `Clear` writes `\r ESC[2K`. It is shown only when stdout and
  stderr are both terminals and the log level is the default, so neither `--quiet` nor
  `--verbose` (FR-013a). The stdout condition follows constitution Principle II (no progress
  when stdout is not a TTY): `vigie | less`, `vigie -o json | jq` and `vigie >/dev/null` get
  no counter. In verbose mode, debug lines stream on stderr and would tear the redrawn line.
  Both conditions are spec amendments (S13). `render.ShowProgress` holds the rule as a pure
  function, so a unit test covers it without a terminal.
- **Concurrency**: `scan.Fetch` calls `OnDone(done, total)` under its own mutex, so updates
  arrive in order and are never concurrent.
- **Cleanup**: `Clear` is deferred, so an interrupt or an error also erases the counter.

## S10. Table rendering

- **Decision**: `render.Table(rows, fields, style, now) string`, a pure function with golden
  tests, like `render.Text`.
- **Header**: `DIR  PROJECT` followed by the requested field names in upper case
  (`BRANCHES PIPELINES LAST MRS ISSUES`), bold when colour is on. This follows the CLI table
  convention (`docker ps`, `kubectl get`), reads the same with or without emoji, and uses the
  `--fields` vocabulary. It replaces the spec assumption that the header uses the `--no-emoji`
  labels (S13).
- **Cells**: the header names each column, so cells drop the per-field icon or label of the
  line. Values, colours, `–` and `10k+` are unchanged (FR-011):

  | Field | Cell (emoji) | Cell (ASCII) |
  |---|---|---|
  | branches | `12/42` | `12/42` |
  | pipelines | `2r/1p` | `2r/1p` |
  | last | `✅ main 3m` | `ok main 3m` |
  | mrs, issues | `5` | `5` |

  The last-pipeline status keeps its icon or label, because that is a value. An archived
  project gets the ` archived` tag in the PROJECT cell.
- **Failed row**: `DIR`, the `PROJECT` path from the remote, then one marker in place of the
  field cells, left unpadded: `⚠ not found`, or `! not found` without emoji. The classes are
  `auth`, `not found`, `network` and `error`, drawn in the stale colour (yellow).
- **Layout**: two spaces between columns. Each column is padded to its widest cell, measured
  in display cells with `cellWidth`, so emoji count as 2. The last column is never padded, so
  no line has trailing spaces. When a terminal width is known and the table is wider, PROJECT
  is cut from the left with `truncateLeft` (`…/sub/app`), then DIR, each down to `…`. Other
  columns are never cut.
- **Off a terminal**: no colour and no emoji, as on the line (001 FR-022), still aligned.

## S11. JSON records

- **Decision**: one record per row, one per line, in table order (FR-014). Each is the 001
  record plus `dir`, the slash-separated path relative to the scan root, the same on every
  platform. `render.JSONRow(w, row)` sets it, and `render.JSON` is unchanged, so single mode
  never emits `dir`.
- **Schema**: [contracts/status.schema.json](./contracts/status.schema.json), copied to
  `docs/status.schema.json`. The change is additive: `dir` is an optional property and `$id`
  keeps `v1`. It is still a contract change and is flagged (FR-018).
- **Failed row in a mixed run**:
  - `project`: the remote path;
  - `url`: the endpoint base + `/` + path;
  - `archived`: `false`;
  - `branch`: the checkout's branch (`""` when detached);
  - every requested field: `null`;
  - `errors`: one item, `{"field":"refresh","reason":…}`, a field value the schema already
    allows;
  - `fetched_at`: the time of the attempt;
  - `stale`: `false`.
- **Alternatives rejected**: a JSON array, which would break one object per line and
  `jq -c` streaming. A `failure` property: FR-014 allows one added property, and the reason
  plus the exit code already tell the class. It can be added later without breaking anything.

## S12. Testing strategy (Principle III)

| Package | What | How |
|---|---|---|
| `scan` | Find: depth, hidden, symlink, bare, nested, unreadable, cancel | `t.TempDir()` trees with fake `.git` dirs and files, no git needed; chmod-based cases skipped on Windows and as root |
| `scan` | Inspect: skip reasons, detached HEAD | real `git init` repos, as in `repo` tests |
| `scan` | Select: recognition, `--host` filter, ports, `GITLAB_HOST` | pure table test over `config.Settings` |
| `scan` | Fetch: per-host bound, auth latch, retry then success, isolation, slots halving | `gitlabtest` with two servers as two hosts; `MaxInFlight`; request counts; `slots` through `export_test.go` |
| `gitlab` | retry: `Retry-After` honoured, budget exceeded → `ErrNetwork`, `OnThrottle` called, `Retry=false` unchanged | `gitlabtest` `Behavior.Times` |
| `render` | Table goldens (emoji, ASCII, colour, failed rows, archived, truncation); Progress bytes; JSONRow goldens | `-update` goldens, as in 001 |
| `render` | every JSON key is declared in the schema and required keys are present | decode `docs/status.schema.json` with stdlib; no validator dependency |
| `cli` | `--depth` parse and validation; `--watch` + scan → usage | black box |
| `cmd/vigie` | e2e against the built binary (below) | `gitRepo` helpers, `checkNoToken` |

`gitlabtest` gains two things: `Server.AddProject`, so one host can serve several projects, and
`Behavior.Times`, which applies a behaviour to the first N requests only.

End-to-end scenarios:
- a folder holding clones on two hosts, an unknown host, a plain directory, a nested clone and
  a hidden clone gives the golden table;
- `-o json` gives one line per row;
- one not-found row still exits 0;
- one host answering 401 marks its rows `auth` and prints one stderr line;
- when every row fails, stdout is empty and the exit code is 3, or 1 when the codes are mixed;
- an empty folder prints the 001 message and exits 2;
- `--watch` in scan mode exits 2;
- `--host` filters the rows;
- an unknown host is never contacted. The proof: `HTTPS_PROXY` points at a recording proxy,
  which must see zero connections. Go never proxies loopback, so the known test hosts on
  127.0.0.1 bypass it (SC-003).
- **Equivalence**: each row's JSON record, without `dir`, equals `vigie -C <clone> -o json` for
  that clone, apart from `fetched_at`. This keeps "a row is what single mode shows" true over
  time.

## S13. Spec amendments made while planning

These are recorded in [spec.md](./spec.md):

1. **Every row failed** (author answer): nothing on stdout; stderr lists the latched hosts and
   the failed rows (Clarifications, US2-4, FR-016).
2. **FR-016**: the exit code is the common exit code of the row errors, which adds `2` for
   local setup, else 1.
3. **FR-006**: the summary is printed with the table. When nothing is listed, the error line
   carries the counts instead.
4. **FR-011**: field icons and labels move to the column header; status icons stay in the
   LAST cell.
5. **FR-013a**: no counter with `--verbose`, and none when stdout is not a terminal
   (constitution Principle II).
6. **Assumptions**: the header uses upper-case field names (S10).

Items 7–12 come from `/speckit-analyze`:

7. **FR-013**, Edge Cases: rows are in tree order, not plain alphabetical order (S2).
8. **FR-006**, Edge Cases, Assumptions: skip reasons use the contract names
   (`no remote "<name>"`, `unknown host`, …), including `remote URL not understood`.
9. **US1-2**: field icons move to the header, following amendment 4.
10. **FR-017**: an invalid `--depth` is a usage error in every mode.
11. **SC-001**: measured on 20 gitlab.com clones, default `--fields`, round trip under
    100 ms.
12. **FR-018**: lists the fourth contract change, `--host` filtering the scan.

## S14. Noticed in feature 001, not changed here

`runner.resolve` calls `client.Project(ctx, …)` with the signal context only. The startup
lookup and the 5-minute re-resolve in watch mode therefore have no deadline beyond dial and
TLS handshake. A server that accepts the connection and never answers keeps one-shot waiting
until Ctrl+C, which falls short of Principle VI ("All I/O MUST have an explicit timeout"). Scan
mode avoids it, because each repository's `--timeout` covers the lookup. The fix for single
mode is a one-line `context.WithTimeout` and belongs in its own change. It is left to the
author.
