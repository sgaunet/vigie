# Smoke Test Record: vigie scan mode

**Feature**: [spec.md](../spec.md) · **Guide**: [quickstart.md](../quickstart.md) · **Date**: 2026-10-21

Machine: Apple M2 Max, macOS (darwin/arm64), Go 1.27.2, binary built with
`CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie`.

## SC-002: walk of 10,000 directories (T068)

`go test -run '^$' -bench Find10k -benchtime=5x -count=3 ./internal/scan`: a tree of 10,000
directories, none of them a repository, on the local SSD.

| Run | Time per walk |
|---|---|
| 1 | 510 ms |
| 2 | 503 ms |
| 3 | 499 ms |

**Result: met** (about 0.5 s, target under 2 s).

## Quickstart scenarios against gitlab.com (T070)

The demo folder of quickstart.md was built in a scratch directory instead of `~/scan-demo`,
with `git init` plus the quickstart remotes in place of shallow clones. vigie reads only the
remote URL and the branch of a clone, so the result is the same. `GITLAB_TOKEN` was a real
`read_api` token.

| Scenario | Expected | Observed |
|---|---|---|
| US1 `vigie` | rows `glab2` (not found) and `team/runner`; summary `4 … found: 2 listed, 2 skipped (1 no remote "origin", 1 unknown host)`; exit 0 | as expected |
| US1 `vigie -v` | one debug line per skipped clone, with its reason | `glab` (no remote "origin"), `github-git` (unknown host) |
| US1 inside `team/runner` | the v1 single line | `gitlab-org/gitlab-runner br 136/414 ci 8r/0p last:run main 1h mr 149 is 2573`, same values as its table row |
| US1 `-p gitlab-org/cli --host https://gitlab.com` | v1 single line, no scan | as expected |
| US1 empty folder, and `plain/` at depth 4 | `not a git repository: no clone found below it either (depth 4); …`, exit 2 | as expected |
| US2 `vigie -f last` | `glab2` marked `! not found`, runner complete, exit 0 | as expected |
| US2 `GITLAB_TOKEN=glpat-invalid vigie` | stdout empty; `token rejected by gitlab.com (401): … (2 repositories)`, then `all 2 listed repositories failed; …`; exit 3 | as expected |
| US3 `-o json \| jq -c '{dir, project, last: .last.status}'` | one object per row, tree order | as expected; `glab2` has `"last": null` |
| US3 `select(.errors \| length > 0)` | `glab2` | as expected |
| US3 `vigie \| cat -v \| grep -c '\^\['` | 0 | 0 |
| US4 `--depth 6 \| grep deep` | `plain/a/b/c/d/deep` listed | as expected (see the note below) |
| US4 `-C team` | one row, `runner` | as expected |
| US4 `--host https://gitlab.com` | same rows; github counts as `other host` | as expected |
| US4 `--host https://gitlab.example.com` | `no GitLab repository found: … other host …`, exit 2 | as expected |
| US4 `--depth 0` / `-w` | usage messages, exit 2 | as expected |
| Counter on a terminal (`script -q /dev/null vigie`) | ` · n/2` on stderr, erased before the table | ` · 0/2`, ` · 1/2`, ` · 2/2` drawn dim, then `\r ESC[2K` before the header |
| Counter with `>/dev/null`, with `-q`, with `-v` | none; the summary only without `-q` | as expected |
| Counter with `NO_COLOR=1` | not dim | as expected |
| `SIGTERM` after 0.3 s | stdout empty, exit 1 | as expected |

Note: quickstart.md said `plain/a/b/c/d/deep` was at depth 5. It is 6 levels below the
folder (depth 1 is a direct subdirectory, FR-003). The quickstart was corrected to `--depth 6`.

## SC-001: 20 gitlab.com clones in under 3 s (T070)

A folder of 20 `gitlab-org` projects (gitlab-runner, cli, gitaly, gitlab-pages, gitlab-shell,
omnibus-gitlab, charts/gitlab, gitlab-development-kit, gitlab-ui, gitlab-svgs,
release-tools, container-registry, gitlab-agent, charts/gitlab-runner,
terraform-provider-gitlab, api/client-go, labkit, gitlab-qa, step-runner, semgrep), default
`--fields`. The round trip to gitlab.com was about 234 ms (`curl` of `/api/v4/version`).

| Run | `--fields` | Wall time |
|---|---|---|
| 1 | default | 25.5 s (first run) |
| 2 | default | 10.8 s |
| 3 | default | 9.0 s |
| 4 | `pipelines,last,mrs,issues` | 5.4 s |
| 5 | `pipelines,last,mrs,issues` | 5.6 s |
| gitaly alone, single mode | default | 3.6 s |

**Result: not demonstrated.** The SC-001 premise does not hold here: the round trip is
234 ms, over the 100 ms it assumes. These projects are also among the largest on gitlab.com,
with 43 to 1,740 branches, so branch pagination dominates. A single one of them (gitaly)
takes 3.6 s in single mode. Without branches, 20 repositories take about 5.4 s: with
`ReposPerHost` = 4 they are fetched in about 5 waves, each about 1 s at this latency. The
measurement needs repeating on a network with a round trip under 100 ms. If it still misses,
the author has two options: raise `ReposPerHost` (research S5 chose 4 to keep one instance
at about 36 requests at once), or restate SC-001 for typical project sizes.

All 20 rows were complete. The `–` cells (pipelines and last on omnibus-gitlab,
charts/gitlab, release-tools and gitlab-qa) match single mode: those projects answer 403 on
pipelines to non-members.
