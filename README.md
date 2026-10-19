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
| `--dir` | `-C` | `.` | directory of the git clone |
| `--remote` | `-r` | `origin` | git remote to read |
| `--project` | `-p` | detected | project path (`group/sub/name`) or numeric ID |
| `--host` | | detected | GitLab base URL, e.g. `https://gitlab.example.com` |
| `--fields` | `-f` | `branches,pipelines,last,mrs,issues` | fields and their order |
| `--output` | `-o` | `text` | `text` or `json` |
| `--no-color` | | off | disable colors |
| `--no-emoji` | | off | replace icons with short ASCII labels |
| `--timeout` | | `10s` | time budget of one whole refresh |
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

## Development

```bash
mise install      # Go, task, golangci-lint, goreleaser at the pinned versions
task lint
task test         # go test -count=2 -race ./...
task build        # static binary in bin/vigie
```

The project follows the rules in [the constitution](.specify/memory/constitution.md):
test first, black-box tests, stdout for data only, no new dependency without approval.
The feature specification lives in [specs/001-gitlab-status-line](specs/001-gitlab-status-line/spec.md).

## License

MIT, see [LICENSE](LICENSE).
