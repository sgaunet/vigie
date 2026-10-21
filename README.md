# vigie

vigie prints a one-line status of the GitLab project behind your git clone:

```text
🗼 group/sub/project 🌿 12/42 ⚙️ 2r/1p ✅ main 3m 🔀 5 🐛 17
```

Active/total branches, running and pending pipelines, the last pipeline of your current
branch, open merge requests and open issues. With `--watch` it redraws the line in place
until you press Ctrl+C. vigie is read-only: it never changes anything on GitLab or in your
repository.

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

Create a GitLab token with the `read_api` scope, export it, and run vigie in a clone:

```bash
export GITLAB_TOKEN=glpat-…
cd ~/src/my-project
vigie
```

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
| `--project` | `-p` | detected | project path (`group/sub/name`) or numeric ID |
| `--host` | | detected | GitLab base URL, e.g. `https://gitlab.example.com`; when scanning, only clones on this host are listed |
| `--depth` | | `4` | when scanning, levels below `--dir` to search |
| `--fields` | `-f` | `branches,pipelines,last,mrs,issues` | fields and their order |
| `--output` | `-o` | `text` | `text` or `json` |
| `--no-color` | | off | disable colors |
| `--no-emoji` | | off | replace icons with short ASCII labels |
| `--timeout` | | `10s` | time budget of one whole refresh, per repository when scanning |
| `--verbose` | `-v` | off | debug logs on stderr |
| `--quiet` | `-q` | off | errors only on stderr |

Short flags cannot be bundled: write `-w -v`, not `-wv`.

Examples:

```bash
vigie -f pipelines,last,mrs --no-emoji      # ci 2r/1p last:ok main 3m mr 5
vigie -o json | jq .pipelines.running       # one JSON object per refresh
vigie -w -i 10s                             # keep watching
vigie -p group/app --host https://gitlab.example.com   # outside any clone
vigie --remote upstream                     # the parent project of a fork
```

### Output

- stdout carries status records only; errors, warnings and logs go to stderr.
- On a terminal, the line has colors and icons. In a pipe or a file it is plain ASCII
  (`group/app br 12/42 ci 2r/1p last:ok main 3m mr 5 is 17`), and nothing can
  turn colors back on: use `--output json` for status bars and scripts.
- A field the project does not offer (issues disabled, for example) shows `–`; the other
  fields are still shown and the run succeeds. Counts above 10,000 show `10k+`.
- `--output json` writes one object per line, described by the
  [JSON schema](docs/status.schema.json). In watch mode a record is written only when the
  content changes, and a failed refresh repeats the last values with `"stale": true`.

## Folder of clones

Run vigie in a folder that is not itself a clone, such as `~/src`, without `--project`, and
it scans the folder for clones and prints one table row per clone:

```text
DIR        PROJECT                BRANCHES  PIPELINES  LAST             MRS  ISSUES
api        backend/api            12/42     2r/1p      ✅ main 3m       5    17
legacy     group/legacy           ⚠ not found
tools/cli  tools/cli              3/3       0r/0p      ❌ fix-login 1h  0    2
web        frontend/web archived  1/1       0r/0p      ∅ main           0    –
```

- **When it scans**: `--dir` (default `.`) is not inside a clone and `--project` is not
  given. Inside a clone, vigie prints that clone's line as usual, even if other clones sit
  below it. Without `git`, nothing can be read and vigie stops with the usual error.
- **Which clones get a row**: a directory at most `--depth` levels below `--dir` (default 4)
  that holds a `.git` entry, worktrees included. vigie does not look inside a clone it found,
  and skips hidden directories, symbolic links and bare repositories. The clone's `--remote`
  must be on a known GitLab host: `gitlab.com`, a key or `api` host of the config `hosts`
  map, or the host of `GITLAB_HOST`. A self-hosted instance therefore needs a `hosts` entry
  (or `GITLAB_HOST`, or `--host`); its clones otherwise count as `unknown host`. No request,
  and so no token, is ever sent to any other host.
- **Rows**: in tree order of `DIR`, which is relative to `--dir`. A row shows what
  `vigie -C <clone>` would show, for the clone's current branch (default branch when
  detached), with the field icons moved to the header. A clone whose project cannot be
  fetched shows one marker instead of the fields: `auth`, `not found`, `network` or `error`.
  On a narrow terminal, `PROJECT` is cut from the left first, then `DIR`.
- **stderr**: one summary line after the table (`6 repositories found: 4 listed, 2 skipped
  (1 no remote "origin", 1 unknown host); run with -v to list them`), unless `--quiet`, and
  one line per host that rejected the token, even with `--quiet`. `--verbose` lists each
  skipped clone with its reason.
- **Progress**: while repositories are fetched, a ` · 12/40` counter is redrawn on stderr
  and erased before the table. It shows only when stdout and stderr are both terminals and
  neither `--quiet` nor `--verbose` is set, so `vigie | less` or `vigie >/dev/null` gets none.
- **`--host URL`** keeps only the clones on that instance (directly, or mapped to it by the
  config `hosts` map); the others count as `other host`.
- **`--timeout`** applies to each repository. At most 4 repositories are fetched at once
  per host; a host that answers "too many requests" is retried within the timeout and slowed
  down, without slowing the other hosts. A host that rejects the token gets no further
  request.
- **`--output json`** writes one record per row, in table order: the usual record plus
  `dir`. A row that failed has every requested field `null` and its error in `errors`:

  ```bash
  vigie -o json | jq -r 'select(.last.status=="failed") | .dir'
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
```

A config file never holds a token: vigie refuses a file with a `token` key. A host entry
takes its token from `token_env` or `token_command`, else from `GITLAB_TOKEN`.

### Precedence

```text
Settings: flags > environment > config file > detected from the repository > defaults.
Host:     --host > detected from the remote (via config "hosts") > GITLAB_HOST (fallback only).
Token:    host entry token_env / token_command > GITLAB_TOKEN.
```

There is no built-in `gitlab.com` default: the token is only sent to a host you named or
that your remote points to.

### Environment

| Variable | Meaning |
|---|---|
| `GITLAB_TOKEN` | access token with the `read_api` scope |
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
| 2 | local setup or usage problem |
| 3 | authentication problem |
| 4 | project not found |
| 5 | network problem in one-shot mode (DNS, TLS, timeout, throttled, server error) |

When scanning a folder of clones: 0 if any row was fetched; if every row failed, the code
the rows share, else 1.

## Development

```bash
mise install      # Go, task, golangci-lint, goreleaser at the pinned versions
task lint
task test         # go test -count=2 -race ./...
task build        # static binary in bin/vigie
```

The project follows the rules in [the constitution](.specify/memory/constitution.md):
test first, black-box tests, stdout for data only, no new dependency without approval.
The feature specifications live in [specs/001-gitlab-status-line](specs/001-gitlab-status-line/spec.md)
and [specs/002-multi-repo-table](specs/002-multi-repo-table/spec.md).

## License

MIT, see [LICENSE](LICENSE).
