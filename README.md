# vigie

vigie prints a one-line status of the GitLab project or GitHub repository behind your git
clone:

```text
🦊 group/sub/project 🌿 12/42 ⚙️ 2r/1p ✅ main 3m 🔀 5 🐛 17
🐙 sgaunet/vigie 🌿 3/5 ⚙️ 1r/0p 🔄 main 2m 🔀 2 🐛 4
```

Active/total branches, running and pending pipelines, the last pipeline of your current
branch, open merge requests and open issues. With `--watch` it redraws the line in place
until you press Ctrl+C. vigie is read-only: it never changes anything on GitLab, on GitHub
or in your repository.

The five fields keep their names on both platforms. On GitHub they read:

| Field | GitLab | GitHub |
|---|---|---|
| `branches` | active/total branches | active/total branches |
| `pipelines` | running/pending pipelines | running/pending GitHub Actions workflow runs |
| `last` | latest pipeline on the branch | combined result of the workflow runs of the branch's latest commit |
| `mrs` | open merge requests | open pull requests, drafts included |
| `issues` | open issues | open issues, pull requests excluded |

On GitHub, `last` combines every workflow run of the newest commit of the branch that has
any run, whatever started it (push, pull request, schedule, manual start): only the latest
run of each workflow counts, and the worst state wins (`running`, then `pending`, `manual`,
`failed`, `canceled`, `unknown`, `success`, `skipped`). Its link is the commit page.

## Install

```bash
brew install sgaunet/tools/vigie
# or
go install github.com/sgaunet/vigie/cmd/vigie@latest
```

Or download an archive for Linux, macOS or Windows (amd64, arm64) from the
[releases](https://github.com/sgaunet/vigie/releases) page and check it against
`checksums.txt`. The binary is static; only `git` is needed, for auto-detection.

## Quick start

### GitLab

Create a GitLab token with the `read_api` scope, export it, and run vigie in a clone:

```bash
export GITLAB_TOKEN=glpat-…
cd ~/src/my-project
vigie
```

### GitHub

Create a [fine-grained token](https://github.com/settings/personal-access-tokens) with read
access to **Metadata**, **Contents**, **Actions**, **Issues** and **Pull requests** on the
repositories you want to see (public repositories need no grant), export it, and run vigie
in a clone:

```bash
export GH_TOKEN=github_pat_…        # or GITHUB_TOKEN
cd ~/src/my-github-project
vigie
```

A classic token needs no scope for public repositories but the `repo` scope for private
ones: GitHub offers no read-only private scope for classic tokens, so a fine-grained token
is the better choice. A token is required even for public repositories.

If you use the GitHub CLI, let vigie ask it for its token instead of exporting one:

```yaml
# ~/.config/vigie/config.yaml
hosts:
  github.com:
    token_command: ["gh", "auth", "token"]
```

vigie never runs `gh` on its own: only a `token_command` named in the config is run.

## Usage

```text
vigie [flags]
vigie version [--output text|json]
```

| Flag | Short | Default | Meaning |
|---|---|---|---|
| `--watch` | `-w` | off | redraw the line until interrupted |
| `--interval` | `-i` | `5s` | refresh interval in watch mode (minimum `2s`) |
| `--dir` | `-C` | `.` | directory of the git clone, or folder to scan |
| `--remote` | `-r` | `origin` | git remote to read |
| `--project` | `-p` | detected | project path (`group/sub/name`, `owner/repo`) or numeric ID |
| `--host` | | detected | GitLab or GitHub base URL, e.g. `https://gitlab.example.com`; when scanning, only clones on this host are listed |
| `--depth` | | `4` | when scanning, levels below `--dir` to search |
| `--fields` | `-f` | `branches,pipelines,last,mrs,issues` | fields and their order |
| `--output` | `-o` | `text` | `text` or `json` |
| `--no-color` | | off | disable colors |
| `--no-emoji` | | off | replace icons with short ASCII labels |
| `--timeout` | | `10s` | time budget of the project lookup and of one whole refresh, per repository when scanning |
| `--verbose` | `-v` | off | debug logs on stderr |
| `--quiet` | `-q` | off | errors only on stderr |

Short flags cannot be bundled: write `-w -v`, not `-wv`. `--timeout` bounds the project
lookup and each refresh separately, so a one-shot run takes at most about twice
`--timeout`.

Examples:

```bash
vigie -f pipelines,last,mrs --no-emoji      # ci 2r/1p last:ok main 3m mr 5
vigie -o json | jq .pipelines.running       # one JSON object per refresh
vigie -w -i 10s                             # keep watching
vigie -p group/app --host https://gitlab.example.com   # outside any clone
vigie -p cli/cli --host https://github.com             # a GitHub repository, outside any clone
vigie --remote upstream                     # the parent project of a fork
```

### Output

- stdout carries status records only; errors, warnings and logs go to stderr.
- On a terminal, the line has colors and icons; the project icon is 🦊 for GitLab and 🐙 for
  GitHub. In a pipe or a file it is plain ASCII
  (`group/app br 12/42 ci 2r/1p last:ok main 3m mr 5 is 17`), and nothing can
  turn colors back on: use `--output json` for status bars and scripts.
- A field the project does not offer (issues disabled, for example) shows `–`; the other
  fields are still shown and the run succeeds. Counts above 10,000 show `10k+`. On GitHub,
  a workflow-run count of 1,000 or more shows `–`, since GitHub does not report it exactly,
  and `pipelines` and `last` show `–` when GitHub refuses to list workflow runs (Actions
  disabled, or a token without read access to Actions).
- `--output json` writes one object per line, described by the
  [JSON schema](docs/status.schema.json). Every record has a `platform` (`gitlab` or
  `github`); on GitHub, `last` has no `id`. In watch mode a record is written only when the
  content changes, and a failed refresh repeats the last values with `"stale": true`.

### Status bars and the GitHub request allowance

GitHub grants 5,000 REST requests and 5,000 GraphQL points per hour, per user, shared by all
of your tokens and tools. A watch session stays well under half of each: unchanged REST
answers are revalidated and cost nothing. A one-shot run cannot revalidate, so a status bar
that polls `vigie` every 5 seconds would use the whole REST allowance within an hour. On
GitHub, run one watch and read its records instead:

```bash
vigie -w -o json | while read -r rec; do …; done
```

When the allowance runs out anyway, vigie never treats it as an authentication failure:
watch mode keeps the last values with the stale marker and sends nothing until the reset; a
one-shot run exits 5 and names the reset time; a folder scan marks that host's rows
`network` and prints one line for the host.

## Folder of clones

Run vigie in a folder that is not itself a clone, such as `~/src`, without `--project`, and
it scans the folder for clones and prints one table row per clone, GitLab and GitHub alike:

```text
DIR        PLATFORM  PROJECT                BRANCHES  PIPELINES  LAST             MRS  ISSUES
api        gitlab    backend/api            12/42     2r/1p      ✅ main 3m       5    17
cli        github    sgaunet/vigie          3/5       1r/0p      🔄 main 2m       2    4
legacy     gitlab    group/legacy           ⚠ not found
web        github    acme/web archived      1/1       0r/0p      ∅ main           0    –
```

- **When it scans**: `--dir` (default `.`) is not inside a clone and `--project` is not
  given. Inside a clone, vigie prints that clone's line as usual, even if other clones sit
  below it. Without `git`, nothing can be read and vigie stops with the usual error.
- **Which clones get a row**: a directory at most `--depth` levels below `--dir` (default 4)
  that holds a `.git` entry, worktrees included. vigie does not look inside a clone it found,
  and skips hidden directories, symbolic links and bare repositories. The clone's `--remote`
  must be on a known host: `gitlab.com`, `github.com`, a key or `api` host of the config
  `hosts` map, or the host of `GITLAB_HOST`. A self-hosted instance therefore needs a
  `hosts` entry (or `GITLAB_HOST`, or `--host`); its clones otherwise count as
  `unknown host`. No request, and so no token, is ever sent to any other host.
- **GitHub clones** need a GitHub token: when their host has no token source at all, they
  are skipped as `no token` (counted in the summary, listed with `-v`), and no request is
  sent for them. A token that is set but rejected marks the rows `auth`.
- **Rows**: in tree order of `DIR`, which is relative to `--dir`. A row shows what
  `vigie -C <clone>` would show, for the clone's current branch (default branch when
  detached), with the field icons moved to the header. `PLATFORM` names the platform of
  every row. A clone whose project cannot be fetched shows one marker instead of the fields:
  `auth`, `not found`, `network` or `error`. On a narrow terminal, `PROJECT` is cut from the
  left first, then `DIR`; `PLATFORM` never is.
- **stderr**: one summary line after the table (`6 repositories found: 4 listed, 2 skipped
  (1 no remote "origin", 1 no token); run with -v to list them`), unless `--quiet`, and
  one line per host that rejected the token or whose allowance is exhausted, even with
  `--quiet`. `--verbose` lists each skipped clone with its reason.
- **Progress**: while repositories are fetched, a ` · 12/40` counter is redrawn on stderr
  and erased before the table. It shows only when stdout and stderr are both terminals and
  neither `--quiet` nor `--verbose` is set, so `vigie | less` or `vigie >/dev/null` gets none.
- **`--host URL`** keeps only the clones on that instance (directly, or mapped to it by the
  config `hosts` map); the others count as `other host`. `--host https://github.com` keeps
  the github.com clones.
- **`--timeout`** applies to each repository. At most 4 repositories are fetched at once
  per host; a host that answers "too many requests" is retried within the timeout and slowed
  down, without slowing the other hosts. A host that rejects the token, or whose allowance is
  exhausted, gets no further request.
- **`--output json`** writes one record per row, in table order: the usual record plus
  `dir`. A row that failed has every requested field `null` and its error in `errors`. The
  same filter works on both platforms:

  ```bash
  vigie -o json | jq -r 'select(.last.status=="failed") | [.dir, .platform] | @tsv'
  ```

- **`--watch`** needs a single clone; in a folder of clones it is a usage error.
- **Exit code**: 0 if any row was fetched. If every row failed, stdout is empty and the
  code is the one the rows share (2 local setup, 3 auth, 4 not found, 5 network), else 1.
  Nothing to list (no clone, or none on a known host) is code 2.

## Configuration

The config file is optional. vigie reads `$VIGIE_CONFIG`, else
`$XDG_CONFIG_HOME/vigie/config.yaml`, else `~/.config/vigie/config.yaml`:

```yaml
interval: 5s
fields: [branches, pipelines, last, mrs, issues]
emoji: true
active_days: 90            # a branch with a commit in the last 90 days is active
hosts:
  ssh.gitlab.example.com:  # host name of the git remote
    api: https://gitlab.example.com
    token_command: ["pass", "show", "gitlab/example"]   # argv, run without a shell
  gitlab.lan:
    api: http://gitlab.lan
    insecure: true         # required to send the token over plain http
    token_env: LAN_GITLAB_TOKEN
  github-work:             # an SSH alias of github.com from ~/.ssh/config
    api: https://github.com
    token_env: WORK_GH_TOKEN
  github.example.com:
    platform: github       # GitHub Enterprise Server; the default is gitlab
    token_command: ["gh", "auth", "token", "--hostname", "github.example.com"]
```

- **`platform`**: `gitlab` (the default) or `github`. A host is GitHub when it is
  `github.com` (or `www.`, `ssh.`, `api.github.com`), when its entry has
  `api: https://github.com`, or when its entry says `platform: github`: a GitHub Enterprise
  Server, whose `api` is its web address (vigie adds `/api/v3` and `/api/graphql`). vigie
  never guesses a platform from a host name or by probing it, so config files written for
  earlier versions keep working unchanged.
- **Tokens**: a config file never holds a token: vigie refuses a file with a `token` key. A
  host entry takes its token from `token_env` or `token_command`, else from the default
  variables of its kind of host (below).
- **Not supported**: GitHub Enterprise Cloud with data residency (`*.ghe.com`), whose API
  lives at another address, and other `*.github.com` hosts such as `gist.github.com`.

### Precedence

```text
Settings: flags > environment > config file > detected from the repository > defaults.
Host:     --host > detected from the remote (via config "hosts") > GITLAB_HOST (fallback only).
Token:    host entry token_env / token_command > GITLAB_TOKEN (GitLab),
          GH_TOKEN > GITHUB_TOKEN (github.com),
          GH_ENTERPRISE_TOKEN > GITHUB_ENTERPRISE_TOKEN (GitHub Enterprise Server).
```

A token from these variables is only ever sent to hosts of its kind: `GITLAB_TOKEN` never
to GitHub, `GH_TOKEN` never to an Enterprise Server or to GitLab. There is no built-in
default host: the token is only sent to a host you named or that your remote points to.
`GITLAB_HOST` stays GitLab-only; outside a clone, pass `--host https://github.com` for
github.com.

### Environment

| Variable | Meaning |
|---|---|
| `GITLAB_TOKEN` | GitLab access token with the `read_api` scope |
| `GH_TOKEN`, `GITHUB_TOKEN` | github.com token with read access |
| `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN` | GitHub Enterprise Server token with read access |
| `GITLAB_HOST` | GitLab base URL, used only when no host is detected |
| `VIGIE_INTERVAL` | default `--interval` |
| `VIGIE_CONFIG` | config file path |
| `NO_COLOR` | any non-empty value disables colors |
| `SSL_CERT_FILE` | extra CA certificates (PEM), on every platform |

There is no option to skip certificate verification.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success, including partial results; watch mode interrupted |
| 1 | unexpected failure (bug, malformed server response); one-shot interrupted |
| 2 | local setup or usage problem (on GitHub: a path that is not `owner/repo`, an invalid `platform`) |
| 3 | authentication problem (token missing, rejected, not authorized for single sign-on) |
| 4 | project not found (on GitHub: also a repository the token cannot see) |
| 5 | network problem in one-shot mode (DNS, TLS, timeout, throttled, server error, exhausted allowance) |

When scanning a folder of clones: 0 if any row was fetched; if every row failed, the code
the rows share, else 1.

## Changes in v0.3.0

GitHub support changes the output contract; scripts that parse the table or validate JSON
strictly should be checked:

1. A clone on a GitHub host now gets a status line (it used to fail), and in a scan a row (it
   used to be skipped as `unknown host`).
2. On a terminal with emoji, the project icon is 🦊 (GitLab) or 🐙 (GitHub) instead of 🗼.
   The plain-text line is unchanged.
3. The scan table has a new `PLATFORM` column between `DIR` and `PROJECT`, in every mode.
4. JSON records have a new required property, `platform`. On GitHub, `last.id` is absent.
5. New environment variables: `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`,
   `GITHUB_ENTERPRISE_TOKEN`.
6. New config key `platform` in a `hosts` entry.
7. New scan skip reason `no token`; the "nothing listed" message names both platforms.
8. Watch mode honours a server retry delay of up to 1 h (was 5 min).

Also visible on GitLab: a project lookup that stalls now ends with exit 5 after `--timeout`
instead of hanging; a config file that names one host twice, or leaves a remote host
matching the `api` of several entries with no entry keyed by it, is now a setup error
(exit 2) instead of a random pick; a `GITLAB_HOST` naming github.com is a setup error.

## Development

```bash
mise install      # Go, task, golangci-lint, goreleaser at the pinned versions
task lint
task test         # go test -count=2 -race ./...
task build        # static binary in bin/vigie
```

The project follows the rules in [the constitution](.specify/memory/constitution.md):
test first, black-box tests, stdout for data only, no new dependency without approval.
The feature specifications live in [specs/001-gitlab-status-line](specs/001-gitlab-status-line/spec.md),
[specs/002-multi-repo-table](specs/002-multi-repo-table/spec.md) and
[specs/003-github-support](specs/003-github-support/spec.md).

## License

MIT, see [LICENSE](LICENSE).
