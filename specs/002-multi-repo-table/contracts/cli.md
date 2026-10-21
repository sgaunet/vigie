# CLI Contract: vigie scan mode (feature 002)

This contract extends [the v1 contract](../../001-gitlab-status-line/contracts/cli.md);
everything not stated here is unchanged. **Contract changes** (constitution, gate 7):

1. A directory that is not inside a clone now produces a scan instead of the "not a git
   repository" error (exit 2). The error remains when nothing is found below it.
2. New flag `--depth`.
3. New optional JSON property `dir` ([status.schema.json](./status.schema.json)).
4. `--host` without `--project`, outside a clone, filters the scan instead of failing.

## Modes

| Situation | Mode |
|---|---|
| `--project` given | single project (v1) |
| `--dir` (default `.`) is inside a clone | single project (v1) |
| otherwise, `git` installed | **scan** of `--dir` |
| otherwise, `git` missing | v1 error `not a git repository: git is not installed, …` (exit 2) |

## Flags

| Long | Short | Value | Default | Notes |
|---|---|---|---|---|
| `--depth` | — | integer ≥ 1 | `4` | scan mode only: how many levels below `--dir` to look for clones; ignored otherwise (debug note) |
| `--dir` | `-C` | path | `.` | the clone, **or the folder to scan** |
| `--host` | — | URL | detected | in scan mode: **only list clones on this host** |
| `--watch` | `-w` | — | off | in scan mode: usage error, exit 2 |

Every other flag applies to each row as it would in that clone: `--remote`, `--fields`,
`--output`, `--no-color`, `--no-emoji`, `--timeout` (per repository), `--verbose`,
`--quiet`.

## Which clones get a row

A directory below `--dir`, at most `--depth` levels down, holding a `.git` entry. A directory
is not searched further once a clone is found in it, and hidden directories, symbolic links
and bare repositories are skipped. The clone gets a row only if:

- without `--host`: its `--remote` host is `gitlab.com`, a key or `api` host of the config
  `hosts` map, or the host of `GITLAB_HOST`;
- with `--host URL`: its remote host is the host of URL, or is mapped to URL by the config
  `hosts` map.

No request, and so no token, ever goes to any other host.

## Text output (stdout)

```text
TTY, emoji (colors not shown):
DIR        PROJECT                BRANCHES  PIPELINES  LAST             MRS  ISSUES
api        backend/api            12/42     2r/1p      ✅ main 3m       5    17
legacy     group/legacy           ⚠ not found
tools/cli  tools/cli              3/3       0r/0p      ❌ fix-login 1h  0    2
web        frontend/web archived  1/1       0r/0p      ∅ main           0    –

Non-TTY, or --no-emoji:
DIR        PROJECT                BRANCHES  PIPELINES  LAST               MRS  ISSUES
api        backend/api            12/42     2r/1p      ok main 3m         5    17
legacy     group/legacy           ! not found
tools/cli  tools/cli              3/3       0r/0p      fail fix-login 1h  0    2
web        frontend/web archived  1/1       0r/0p      none main          0    –
```

- **Header**: always printed. `DIR`, `PROJECT`, then the `--fields` names in upper case, in
  `--fields` order. Bold when colors are on.
- **Rows**: in tree order of `DIR` (depth-first, siblings sorted by name). `DIR` is relative
  to `--dir` and `/`-separated on every platform.
- **Cells**: the value parts of the v1 segments, without the per-field icon or label. LAST
  keeps its status icon (emoji) or label (`ok`, `fail`, `run`, `wait`, `manual`, `sched`,
  `skip`, `none`, `?`), then the ref and the age. Colors, `–` and `10k+` follow v1.
- **Failed row**: `DIR`, `PROJECT` (path from the remote), then one marker: `⚠ <class>`, or
  `! <class>` without emoji, in yellow. Classes: `auth`, `not found`, `network`, `error`.
- **Layout**: columns separated by two spaces and padded to the widest cell, measured in
  terminal cells. The last cell of a line is not padded. On a terminal narrower than the
  table, `PROJECT` is cut from the left (`…/sub/app`) first, then `DIR`.
- **Printed once**, after every repository finished. Never on a non-TTY with colors or
  emoji (v1 FR-022).

## JSON output (stdout)

One object per listed repository, one per line, in table order. Each is a v1 status record
plus `"dir"`. A failed row (only in a run where another row succeeded) has every requested
field `null` and `"errors": [{"field": "refresh", "reason": "…"}]`.

```json
{"project":"backend/api","url":"https://gitlab.com/backend/api","archived":false,"branch":"main","dir":"api","branches":{"active":12,"total":42},"pipelines":{"running":2,"pending":1},"last":{"status":"success","id":48213,"ref":"main","updated_at":"2026-10-21T09:57:00Z","web_url":"https://gitlab.com/backend/api/-/pipelines/48213"},"mrs":{"opened":5},"issues":{"opened":17},"fetched_at":"2026-10-21T10:00:00Z","stale":false,"errors":[]}
{"project":"group/legacy","url":"https://gitlab.com/group/legacy","archived":false,"branch":"main","dir":"legacy","branches":null,"pipelines":null,"last":null,"mrs":null,"issues":null,"fetched_at":"2026-10-21T10:00:00Z","stale":false,"errors":[{"field":"refresh","reason":"project group/legacy not found on https://gitlab.com: check the hosts mapping in the config file"}]}
```

## Stderr in scan mode

Progress, only when stdout and stderr are both terminals and neither `--quiet` nor
`--verbose` is set: one line redrawn in place and erased before stdout is written. Piped or
redirected output (`vigie | less`, `vigie -o json | jq`, `vigie >/dev/null`) gets none.

```text
 · 12/40
```

After stdout, when at least one row succeeded:

```text
6 repositories found: 4 listed, 2 skipped (1 no remote "origin", 1 unknown host); run with -v to list them
token rejected by gitlab.example.com (401): check that the token is valid and not expired (2 repositories)
```

- **Summary line**: always, unless `--quiet`. The skip part appears only when something was
  skipped. Reasons, in this order: `bare`, `unreadable`, `no remote "<name>"`,
  `remote URL not understood`, `unknown host`, `other host`.
- **Latched host**: one line per host that rejected the token or whose token could not be
  read (v1 message plus the count, `(1 repository)` for one). Errors, so shown even with
  `--quiet`.
- **`--verbose`**: debug lines for each skipped repository (`dir`, `reason`), each failed
  row, field failures, and slow-down events (`host`, `limit`).

When every listed row failed, stdout is empty and stderr holds:

```text
token rejected by gitlab.example.com (401): check that the token is valid and not expired (2 repositories)
legacy: project group/legacy not found on https://gitlab.com: check the hosts mapping in the config file
all 3 listed repositories failed; 1 skipped (1 unknown host); run with -v to list them
```

## Error messages (examples)

```text
not a git repository: no clone found below it either (depth 4); run vigie in a clone or a folder of clones, or pass --project and --host
no GitLab repository found: 3 skipped (1 no remote "origin", 2 unknown host); add self-hosted hosts to the config file "hosts", or run with -v
cannot scan /no/such/dir: check --dir
invalid usage: --watch needs a single repository: run it inside a clone or pass --project
invalid --depth 0: minimum is 1
```

## Exit codes in scan mode

| Code | When |
|---|---|
| 0 | at least one row was fetched (fully, or with unavailable fields) |
| 1 | interrupted; or every row failed and their exit codes differ; or every row failed with an unexpected error |
| 2 | nothing listed (no clone, or no clone on a recognised host); `--watch`; invalid `--depth`; unreadable `--dir`; every row failed on local setup |
| 3 / 4 / 5 | every row failed, all with that code (auth / not found / network) |

## `--help` additions (verbatim intent)

```text
  -C, --dir PATH          directory of the git clone, or folder to scan (default .)
      --host URL          GitLab base URL, e.g. https://gitlab.example.com (default: detected);
                          when scanning, only clones on this host are listed
      --depth N           when scanning, levels below --dir to search (default 4)
      --timeout DURATION  time budget of one whole refresh, per repository when scanning
                          (default 10s)

Folder of clones:
  Outside any clone and without --project, vigie scans --dir for clones whose remote is on
  a known GitLab host (gitlab.com, config "hosts", GITLAB_HOST) and prints one table row per
  clone. No request is sent to any other host. --watch needs a single clone.

Exit codes:
  … (v1 lines)
  When scanning: 0 if any row was fetched; if all failed, their common code, else 1.
```
