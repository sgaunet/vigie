# CLI Contract: GitHub support (feature 003)

This contract extends [the v1 contract](../../001-gitlab-status-line/contracts/cli.md) and
[the scan-mode contract](../../002-multi-repo-table/contracts/cli.md). Everything not stated
here is unchanged.

**Contract changes** (constitution, gate 7; FR-027). The release carrying them is **v0.3.0**:

1. A clone on a GitHub host now gets a status line (it used to fail), and in a scan a row
   (it used to be skipped as `unknown host`).
2. Text output: the project icon is 🦊 (GitLab) or 🐙 (GitHub) instead of 🗼, on a
   terminal with emoji. The plain-text line is unchanged.
3. Scan table: a new `PLATFORM` column between `DIR` and `PROJECT`, in every mode.
4. JSON: a new required property `platform` in every record
   ([status.schema.json](./status.schema.json)). On GitHub, `last.id` is absent.
5. New environment variables `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`,
   `GITHUB_ENTERPRISE_TOKEN`.
6. New config key `platform` in a `hosts` entry.
7. New scan skip reason `no token`, and the "nothing listed" message names both platforms.
8. Watch mode honours a server retry delay of up to 1 h (was 5 min).

**Other changes visible on GitLab** (not contract changes; listed in the commit message):

- A project lookup that stalls ends with exit 5 after `--timeout` instead of hanging
  (research G16).
- A config file that names one host twice, or leaves a remote host matching the `api` of
  several entries with no entry keyed by it, is a setup error (exit 2). vigie used to pick
  one entry at random.
- A `GITLAB_HOST` naming github.com is a setup error (exit 2).

## Which hosts are GitHub

| Host | Platform | API used |
|---|---|---|
| `github.com`, `www.github.com`, `ssh.github.com`, `api.github.com` | GitHub, always | `https://api.github.com` |
| a `hosts` entry with `platform: github` | GitHub Enterprise Server | `<api>/api/v3`, `<api>/api/graphql` |
| a `hosts` entry whose `api` is `https://github.com` (an SSH alias) | GitHub (github.com) | `https://api.github.com` |
| any other `*.github.com` (e.g. `gist.github.com`) | none: setup error in a single clone (exit 2), `unknown host` in a scan | – |
| anything else | GitLab, as before | – |

vigie never guesses a platform from a host name or by sending a request.

## Config file

```yaml
hosts:
  github-work:                 # SSH alias from ~/.ssh/config
    api: https://github.com    # → github.com
    token_env: WORK_GH_TOKEN
  github.example.com:
    platform: github           # GitHub Enterprise Server; default is gitlab
    token_command: ["gh", "auth", "token", "--hostname", "github.example.com"]
```

- `platform`: `gitlab` (default) or `github`. Any other value, or `platform: gitlab` on a
  github.com host, is a setup error (exit 2).
- `api` of a GitHub Enterprise Server entry is its web address; vigie adds `/api/v3` and
  `/api/graphql`.
- Two keys for one host (`github.com` and `ssh.github.com`, or names differing only in case)
  are a setup error (exit 2): `hosts github.com and ssh.github.com in config file P name the
  same host: keep one`.
- A github.com remote uses only an entry keyed by a github.com name, never an alias whose
  `api` is `https://github.com`. On other hosts, a remote host matching the `api` of several
  entries, with no entry keyed by it, is a setup error (exit 2): `remote host ghe.corp
  matches hosts a and b in the config file: add an entry keyed ghe.corp`.

## Tokens

```text
Token: host entry token_env / token_command
       > GitLab:            GITLAB_TOKEN
       > github.com:        GH_TOKEN > GITHUB_TOKEN
       > Enterprise Server: GH_ENTERPRISE_TOKEN > GITHUB_ENTERPRISE_TOKEN
```

- A token from these variables is only ever sent to hosts of that kind: `GITLAB_TOKEN` never
  to GitHub, `GH_TOKEN` never to an Enterprise Server or to GitLab.
- vigie never runs `gh` unless a `token_command` names it.
- Read access is enough. A fine-grained token needs: Metadata, Contents, Actions, Issues,
  Pull requests (read).

Missing-token errors (exit 3, one line):

```text
GH_TOKEN and GITHUB_TOKEN are not set: create a fine-grained token with read access to the repository and export it, or set token_command: ["gh", "auth", "token"] for this host in the config file
GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not set: create a token with read access to the repository and export it, or set token_env or token_command for github.example.com in the config file
```

## Text output (stdout)

Terminal with emoji (colors not shown):

```text
🐙 sgaunet/vigie 🌿 3/5 ⚙️ 1r/0p 🔄 main 2m 🔀 2 🐛 4
🦊 group/sub/project 🌿 12/42 ⚙️ 2r/1p ✅ main 3m 🔀 5 🐛 17
```

Non-TTY or `--no-emoji` (unchanged format):

```text
sgaunet/vigie br 3/5 ci 1r/0p last:run main 2m mr 2 is 4
```

On GitHub:
- `ci` counts workflow runs;
- `mr` counts pull requests;
- `last` is the combined result of the workflow runs of the latest commit that has any.

## Scan table (stdout)

```text
DIR        PLATFORM  PROJECT                BRANCHES  PIPELINES  LAST             MRS  ISSUES
api        gitlab    backend/api            12/42     2r/1p      ✅ main 3m       5    17
cli        github    sgaunet/vigie          3/5       1r/0p      🔄 main 2m       2    4
legacy     gitlab    group/legacy           ⚠ not found
web        github    acme/web archived      1/1       0r/0p      ∅ main           0    –
```

- **`PLATFORM`**: `gitlab` or `github`, plain text in every mode. Failed rows show it too.
- **Truncation**: the column is never truncated; `PROJECT`, then `DIR`, are cut as before.
- Header and column rules are otherwise those of the 002 contract.

## JSON (stdout)

```json
{"platform":"github","project":"sgaunet/vigie","url":"https://github.com/sgaunet/vigie","archived":false,"branch":"main","branches":{"active":3,"total":5},"pipelines":{"running":1,"pending":0},"last":{"status":"running","ref":"main","updated_at":"2026-10-21T09:58:00Z","web_url":"https://github.com/sgaunet/vigie/commit/4f2a9c1e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a39"},"mrs":{"opened":2},"issues":{"opened":4},"fetched_at":"2026-10-21T10:00:00Z","stale":false,"errors":[]}
```

- **`platform`**: required, `gitlab` or `github`, in single mode, watch mode and scan mode.
- **`last` on GitHub**:
  - no `id`;
  - `web_url` is the commit page;
  - `status` uses the existing vocabulary (FR-014).
- **Unexact run count** (1,000 or more):

  ```json
  "pipelines":null, … "errors":[{"field":"pipelines","reason":"more than 1,000 workflow runs: GitHub does not report an exact count"}]
  ```

## Request allowance (stderr, exit codes)

One-shot, allowance exhausted (exit 5):

```text
github.com request allowance exhausted until 14:05 (in 13m): wait for the reset; it is shared by all your GitHub tokens
```

- **Watch**: the line goes stale (`⚠ stale 2m`), and no request is sent before the reset or
  retry delay. Watching continues; it is never an authentication error.
- **Scan**: the host is latched. Its rows show `⚠ network`, no further request is sent to it,
  and stderr gets one line for the host, even with `--quiet`:

  ```text
  github.com request allowance exhausted until 14:05 (in 13m): wait for the reset; it is shared by all your GitHub tokens (4 repositories)
  ```

## Other GitHub errors (stderr, one line, exit code)

```text
token rejected by github.com (401): check that the token is valid and not expired                                   → 3
access to repository acme/secret denied (403): authorize the token for the organization's SAML single sign-on      → 3
access to repository acme/app denied (403): the token lacks the permission "actions=read"                          → 3 (lookup only; on a field: "–")
repository acme/gone not found on github.com: check that the token can see it (for a fine-grained token, add the repository to it; for an organization with single sign-on, authorize the token)  → 4
project path a/b/c on github.com: GitHub repositories are owner/repo                                              → 2
gist.github.com is not a repository host: check the git remote                                                     → 2
GITLAB_HOST https://github.com is a GitHub host: unset it, and pass --host https://github.com outside a clone       → 2
```

## Scan stderr

```text
9 repositories found: 6 listed, 3 skipped (1 no remote "origin", 2 no token); run with -v to list them
no GitLab or GitHub repository found: 2 skipped (2 no token); export GH_TOKEN for github.com clones, or run with -v
```

`--verbose` lists each `no token` clone with its directory, like the other skip reasons.

## Exit codes

Unchanged (0–5). The GitHub mapping:

| Code | GitHub cause |
|---|---|
| 2 | `platform` key invalid; path not `owner/repo`; `*.github.com` non-repository host; `GITLAB_HOST` on github.com; a host named twice, or an ambiguous `api` match, in the config |
| 3 | token missing, rejected (401), denied (403, incl. single sign-on) |
| 4 | repository not found or invisible to the token (404) |
| 5 | network, 5xx, secondary limit, exhausted allowance (one-shot) |

## `--help` additions (verbatim intent)

```text
vigie prints a one-line status of the GitLab project or GitHub repository behind a git
clone: branches, pipelines (GitHub: workflow runs), the last pipeline of the current branch,
open merge requests (GitHub: pull requests) and open issues.

      --host URL          GitLab or GitHub base URL, e.g. https://gitlab.example.com
                          (default: detected); when scanning, only clones on this host are listed
      --timeout DURATION  time budget of the project lookup and of one whole refresh, per
                          repository when scanning (default 10s)

Folder of clones:
  Outside any clone and without --project, vigie scans --dir for clones whose remote is on
  a known host (gitlab.com, github.com, config "hosts", GITLAB_HOST) and prints one table row
  per clone. A GitHub clone needs a GitHub token, else it is skipped. No request is sent to
  any other host. --watch needs a single clone.

Precedence:
  Token:    host entry token_env / token_command > GITLAB_TOKEN (GitLab),
            GH_TOKEN > GITHUB_TOKEN (github.com),
            GH_ENTERPRISE_TOKEN > GITHUB_ENTERPRISE_TOKEN (GitHub Enterprise Server).

Environment:
  GH_TOKEN, GITHUB_TOKEN    github.com token with read access
  GH_ENTERPRISE_TOKEN, GITHUB_ENTERPRISE_TOKEN
                            GitHub Enterprise Server token with read access

Config file:
  hosts:
    github.example.com:
      platform: github      # GitHub Enterprise Server (default: gitlab)
```
