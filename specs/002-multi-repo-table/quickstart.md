# Quickstart & Validation: vigie scan mode

How to prove the feature works end to end. The contract is in
[contracts/cli.md](./contracts/cli.md) and [contracts/status.schema.json](./contracts/status.schema.json);
types are in [data-model.md](./data-model.md), decisions in [research.md](./research.md).

## Prerequisites

- Same as feature 001: `mise install`, `git`, `jq`, a gitlab.com token with `read_api` in
  `GITLAB_TOKEN`.
- A scratch folder of clones, built once:

```bash
mkdir -p ~/scan-demo/team ~/scan-demo/.cache ~/scan-demo/plain/a/b/c/d
cd ~/scan-demo
git clone -q https://gitlab.com/gitlab-org/gitlab-runner.git team/runner --depth 1
git clone -q https://gitlab.com/gitlab-org/cli.git glab --depth 1
git clone -q https://github.com/git/git.git github-git --depth 1          # not GitLab
git clone -q https://gitlab.com/gitlab-org/cli.git .cache/hidden --depth 1 # hidden: ignored
git clone -q https://gitlab.com/gitlab-org/cli.git plain/a/b/c/d/deep --depth 1  # depth 6
git -C glab remote rename origin upstream                                  # no "origin"
git clone -q https://gitlab.com/gitlab-org/cli.git glab2 --depth 1
git -C glab2 remote set-url origin git@gitlab.com:no-such-group/no-such-project.git
```

## Gates (must all pass)

The same gates as 001: `task lint`, `task test`, `go generate ./... && git diff --exit-code`,
`CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie`. Also:

```bash
diff docs/status.schema.json specs/002-multi-repo-table/contracts/status.schema.json   # no diff
```

## Scenarios

Run them from `~/scan-demo` unless noted.

### US1: a table of GitLab clones

```bash
vigie; echo "exit=$?"
```

Expected on stdout: a header, then rows for `glab2` (`⚠ not found`) and `team/runner` with
values that match the GitLab web UI. There are no rows for `github-git` (unknown host),
`glab` (no `origin`), `.cache/hidden` (hidden) or `plain/a/b/c/d/deep` (depth 6 > 4). Stderr
holds `… found: 2 listed, 2 skipped (1 no remote "origin", 1 unknown host); run with -v …`.
`exit=0`.

```bash
vigie -v 2>&1 >/dev/null | grep skipped   # one debug line per skipped clone, with its reason
(cd ~/scan-demo/team/runner && vigie)     # inside a clone: the v1 single line, unchanged (SC-007)
vigie -p gitlab-org/cli --host https://gitlab.com   # --project: v1, no scan
(mkdir -p /tmp/empty-scan && cd /tmp/empty-scan && vigie; echo "exit=$?")
#   stderr "not a git repository: no clone found below it either (depth 4); …", exit=2
(cd ~/scan-demo/plain && vigie; echo "exit=$?")      # depth 4 < 5: same message, exit=2
```

Feature 001's quickstart ran `(cd /tmp && vigie)` to show "not a git repository". That now
scans `/tmp`; use an empty folder as above.

### US2: one broken repository does not hide the others

```bash
vigie -f last; echo "exit=$?"                 # glab2 shows "⚠ not found", runner is complete, exit=0
GITLAB_TOKEN=glpat-invalid vigie; echo "exit=$?"
#   stdout empty; stderr "token rejected by gitlab.com (401): … (2 repositories)" then
#   "all 2 listed repositories failed; …", exit=3
```

### US3: scripts

```bash
vigie -o json | jq -c '{dir, project, last: .last.status}'   # one object per row, tree order
vigie -o json | jq -r 'select(.errors | length > 0) | .dir'  # glab2
vigie | cat -v | grep -c '\^\['                              # 0: no escape sequences in a pipe
```

### US4: depth, folder and host

```bash
vigie --depth 6 | grep deep                        # plain/a/b/c/d/deep now listed
vigie -C ~/scan-demo/team                          # rows relative to team/: "runner"
vigie --host https://gitlab.com                    # same rows: every listed clone is on gitlab.com
vigie --host https://gitlab.example.com; echo $?   # nothing on that host: "no GitLab repository found: … other host …", exit=2
vigie --depth 0; echo $?                           # "invalid --depth 0: minimum is 1", exit=2
vigie -w; echo $?                                  # "--watch needs a single repository …", exit=2
```

### Progress, interrupt, timing

```bash
vigie                              # on a terminal: " · n/2" on stderr, erased before the table
vigie >/dev/null                   # no counter (stdout is not a terminal); summary still printed
vigie -q                           # no counter, no summary
vigie & sleep 0.3; kill -TERM $!; wait $!; echo "exit=$?"  # stdout empty, exit=1, terminal clean
#   (TERM, not INT: a shell may start background jobs with SIGINT ignored)
time vigie >/dev/null              # SC-001, folder of 20 gitlab.com clones: real < 3 s
```

## Automated equivalents

`task test` covers every scenario above without a network ([research.md S12](./research.md#s12-testing-strategy-principle-iii)):

- temporary trees with fake `.git` entries for the walk, and real `git init` clones for
  inspection;
- two `gitlabtest` servers acting as two GitLab hosts, with several projects each;
- `Behavior.Times` for throttling, then recovery;
- a recording `HTTPS_PROXY` proving that an unknown host is never contacted;
- the built binary run against a folder of clones, with golden output;
- an equivalence check: each row equals `vigie -C <clone> -o json` for that clone.

A manual run on `~/scan-demo` against gitlab.com is the pre-release smoke test.
