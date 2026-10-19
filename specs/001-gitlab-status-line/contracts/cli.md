# CLI Contract: vigie v1

This is the public contract (Constitution Principle II). Changing flag names, output bytes,
the JSON schema or exit codes is a breaking change.

## Synopsis

```text
vigie [flags]
vigie version [--output text|json]
```

Any other positional argument is a usage error (exit 2).

## Flags

| Long | Short | Value | Default | Notes |
|---|---|---|---|---|
| `--watch` | `-w` | — | off | redraw until interrupted |
| `--interval` | `-i` | duration | `5s` | min `2s`; ignored without `--watch` |
| `--dir` | `-C` | path | `.` | |
| `--remote` | `-r` | name | `origin` | |
| `--project` | `-p` | `group/…/name` or ID | detected | |
| `--host` | — | URL | detected | e.g. `https://gitlab.example.com` |
| `--fields` | `-f` | list | `branches,pipelines,last,mrs,issues` | order = display order |
| `--output` | `-o` | `text`\|`json` | `text` | |
| `--no-color` | — | — | off | |
| `--no-emoji` | — | — | off | |
| `--timeout` | — | duration | `10s` | one whole refresh |
| `--verbose` | `-v` | — | off | debug logs to stderr |
| `--quiet` | `-q` | — | off | errors only on stderr; exclusive with `-v` |
| `--help` | `-h` | — | — | help on stdout, exit 0 |

Short flags cannot be bundled (`-w -v`, not `-wv`). `-x=value` and `-x value` both work.

## Precedence (stated verbatim in `--help`)

```text
Settings: flags > environment > config file > detected from the repository > defaults.
Host:     --host > detected from the remote (via config "hosts") > GITLAB_HOST (fallback only).
Token:    host entry token_env / token_command > GITLAB_TOKEN.
```

## Environment

`GITLAB_TOKEN`, `GITLAB_HOST`, `VIGIE_INTERVAL`, `VIGIE_CONFIG`, `NO_COLOR` (non-empty =
on), `SSL_CERT_FILE` (extra CA, all platforms). `GITLAB_HOST` is fallback-only: it never
overrides a host detected from the remote.

## Streams

- **stdout**: status records only: text lines, or JSON objects (one per line). `--help` and
  `version` output also go to stdout.
- **stderr**: errors (one line, `<what>: <hint>`), warnings, debug logs. Never the token.
- On a fatal error, stdout receives nothing.

## Text output

```text
TTY, emoji:      🗼 group/sub/project 🌿 12/42 ⚙️ 2r/1p ✅ main 3m 🔀 5 🐛 17
TTY, --no-emoji: group/sub/project br 12/42 ci 2r/1p last:ok main 3m mr 5 is 17
Non-TTY:         same as --no-emoji, without color
```

- Separator: one space. Unavailable field: its icon or label followed by `–`.
- Capped count: `10k+`. Archived project: ` archived` tag after the path.
- Watch on a TTY: line rewritten with `\r` + `ESC[2K`, cursor hidden, a dim ` ·` on **stderr**
  while refreshing (stderr TTY only),
  ` ⚠ stale 15s` (` ! stale 15s` without emoji) when values are stale.
- Watch on a non-TTY: one `\n`-terminated line per changed snapshot.
- ASCII labels for `last`: `ok` success, `fail` failed, `run` running, `wait`
  pending/created/preparing/waiting_for_resource, `manual`, `sched` scheduled, `skip`
  canceled/skipped, `none`, `?` unknown.

## JSON output

One object per line, schema in [status.schema.json](./status.schema.json). In watch mode, a
record is written only when its fingerprint changes (data, errors, project, branch, `stale`).

`vigie version --output json` → `{"version":"1.0.0","commit":"abc1234","date":"2026-10-19T09:00:00Z"}`.

## Exit codes (listed in `--help`)

| Code | Meaning |
|---|---|
| 0 | success, including partial results; watch mode interrupted |
| 1 | unexpected failure (bug, malformed server response); one-shot interrupted |
| 2 | local setup or usage problem |
| 3 | authentication problem |
| 4 | project not found |
| 5 | network problem in one-shot mode (DNS, TLS, timeout, throttled, server error) |

## Error messages (examples, stderr)

```text
not a git repository: run vigie inside a clone, or pass --project and --host
remote "origin" not found: existing remotes are upstream, fork
GITLAB_TOKEN is not set: create a token with the read_api scope and export it
token rejected by gitlab.example.com (401): check that the token is valid and not expired
project group/app not found on https://gitlab.example.com: check the hosts mapping in the config file
refusing to send the token over http://gitlab.lan: set insecure: true for this host in the config file
config file ~/.config/vigie/config.yaml contains a token: use token_env or token_command instead
cannot reach gitlab.com: dial tcp: lookup gitlab.com: no such host
invalid --interval 1s: minimum is 2s
```
