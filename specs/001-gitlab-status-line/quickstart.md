# Quickstart & Validation: vigie

How to prove the feature works end to end. Contract details are in
[contracts/cli.md](./contracts/cli.md) and [contracts/status.schema.json](./contracts/status.schema.json);
types are in [data-model.md](./data-model.md).

## Prerequisites

- `mise install` (Go 1.27.2, task, golangci-lint v2, goreleaser v2), `git`, `jq`.
- A gitlab.com token with the `read_api` scope, exported as `GITLAB_TOKEN`.
- A clone of a gitlab.com project you can read, with CI pipelines. Below: `~/src/demo`.

## Gates (must all pass)

```bash
task lint                           # golangci-lint v2, architecture rules via depguard/forbidigo
task test                           # go test -count=2 -race ./...  (includes the e2e binary test)
go generate ./... && git diff --exit-code
CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie
file bin/vigie                      # Linux: "statically linked"
task snapshot                       # goreleaser: 6 archives, checksums.txt, SBOMs
```

## Scenarios

Each maps to a user story in [spec.md](./spec.md). Run them from `~/src/demo` unless noted.

### US1: one-shot status line

```bash
vigie; echo "exit=$?"
```

Expected: one line `🗼 <group/project> 🌿 a/t ⚙️ Nr/Mp <icon> <branch> <age> 🔀 N 🐛 N`,
`exit=0`; values match the GitLab web UI. Then:

```bash
time vigie >/dev/null                         # SC-001: real < 1 s
git checkout --detach && vigie; git switch -  # last-pipeline field uses the default branch
(cd /tmp && vigie; echo "exit=$?")            # stderr "not a git repository: …", exit=2
GITLAB_TOKEN= vigie; echo "exit=$?"           # stderr "GITLAB_TOKEN is not set: …", exit=3
GITLAB_TOKEN=glpat-invalid vigie; echo $?     # 3
vigie -p no/such-project --host https://gitlab.com; echo $?   # 4
vigie --host https://gitlab.invalid; echo $?  # 5
vigie -wv; echo $?                            # bundled short flags rejected, exit 2
```

### US2: scripts and pipes

```bash
vigie -o json | jq -e .pipelines.running                 # a number
vigie -o json | check-jsonschema --schemafile specs/001-gitlab-status-line/contracts/status.schema.json -
vigie | cat -v | grep -c '\^\[' ; echo                   # 0: no escape codes in a pipe
vigie 2>/dev/null | wc -l                                # 1
vigie -v 2>&1 >/dev/null | grep -c "$GITLAB_TOKEN"       # 0 (SC-003); shows glpat-****
```

### US3: watch mode

```bash
vigie -w -i 2s          # start a pipeline in the UI: running count and last status change
                        # Ctrl+C → cursor visible, prompt on a new line, echo $? → 0
vigie -w -i 2s | cat    # a new line only when values change
vigie -w -o json | jq -c '{stale, last: .last.status}'
```

Disconnect the network during `vigie -w`: the line keeps the values and shows `⚠ stale Ns`;
JSON emits one record with `"stale": true`; reconnecting brings `"stale": false` back.
Resize the terminal: the line redraws at once and the project path is cut from the left.

### US4: self-hosted and per-host tokens

```yaml
# $XDG_CONFIG_HOME/vigie/config.yaml
hosts:
  ssh.gitlab.example.com:
    api: https://gitlab.example.com
    token_command: ["pass", "show", "gitlab/example"]
```

```bash
vigie -C ~/src/selfhosted                     # resolved on gitlab.example.com
(cd /tmp && vigie -p group/app --host https://gitlab.example.com)
```

Negative checks: an `http://` API without `insecure: true` → exit 2 and no request sent;
a `token:` key in the file → exit 2.

### US5: shaping the line

```bash
vigie -f pipelines,last,mrs --no-emoji        # three ASCII segments, in that order
NO_COLOR=1 vigie | cat -v                     # no escape codes
vigie -f mrs,mrs; echo $?                     # 2, valid fields listed
```

## Automated equivalents

`task test` covers every scenario above without a network: `httptest.Server` fixtures for the
API, temporary `git init` repositories for detection, `testing/synctest` for watch timing, and
`cmd/vigie/main_test.go`, which builds the binary and checks exit codes, the stdout/stderr split
and that the token never appears in the output. A manual run of the scenarios against
gitlab.com is the pre-release smoke test.
