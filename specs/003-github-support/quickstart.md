# Quickstart & Validation: GitHub support

How to prove the feature works end to end. The contract is in
[contracts/cli.md](./contracts/cli.md) and [contracts/status.schema.json](./contracts/status.schema.json);
types are in [data-model.md](./data-model.md), decisions in [research.md](./research.md).

## Prerequisites

- Same as features 001 and 002: `mise install`, `git`, `jq`, a gitlab.com token in
  `GITLAB_TOKEN`.
- A fine-grained GitHub token with read access to Metadata, Contents, Actions, Issues and
  Pull requests on the repositories below (public repositories need no grant), exported as
  `GH_TOKEN`. Alternatively, a `token_command: ["gh", "auth", "token"]` entry for
  `github.com`.
- A mixed folder, built once:

```bash
mkdir -p ~/gh-demo && cd ~/gh-demo
git clone -q https://github.com/cli/cli.git gh-cli --depth 50
git clone -q https://github.com/sgaunet/vigie.git vigie --depth 1
git clone -q https://gitlab.com/gitlab-org/cli.git glab --depth 1
git clone -q https://github.com/cli/cli.git gh-moved --depth 1
git -C gh-moved remote set-url origin https://github.com/docker/docker.git   # renamed upstream
git clone -q https://github.com/cli/cli.git gh-ssh443 --depth 1
git -C gh-ssh443 remote set-url origin ssh://git@ssh.github.com:443/cli/cli.git
```

## Gates (must all pass)

The gates of features 001 and 002 (`task lint`, `task test`, `go generate ./... && git diff
--exit-code`, `CGO_ENABLED=0 go build -trimpath -o bin/vigie ./cmd/vigie`), plus:

```bash
diff docs/status.schema.json specs/003-github-support/contracts/status.schema.json   # no diff
golangci-lint run ./...   # depguard: net/http only in forge, gitlab, github
```

## Scenarios

### US1: a GitHub repository at a glance

```bash
cd ~/gh-demo/gh-cli && vigie; echo "exit=$?"
vigie | cat
vigie -o json | jq '{platform, project, last, mrs, issues}'
```

Expected:
- **Terminal line**: `🐙 cli/cli 🌿 a/b ⚙️ Nr/Np <status> trunk <age> 🔀 n 🐛 m`, with
  `exit=0`.
- **Values** match the GitHub pages: Branches; Actions filtered by `is:in_progress` and
  `is:queued`; Pull requests (open, drafts included); Issues (open; the PR count is not
  included).
- **Piped**: the plain line with no 🐙 and no colour.
- **JSON**: `"platform":"github"` and no `last.id`; `last.web_url` is
  `https://github.com/cli/cli/commit/<sha>`.

Combined result (FR-011): open the commit in `last.web_url`. If any of its workflow runs is in
progress, `last.status` is `running`; else if any failed, `failed`; else `success`.

**Run order (observed fact, research G6)**: the first run listed is the newest.

```bash
gh api 'repos/cli/cli/actions/runs?branch=trunk&per_page=3' --jq '.workflow_runs[].created_at'
```

Expected: the dates are in descending order.

### US1: tokens

```bash
cd ~/gh-demo/gh-cli
env -u GH_TOKEN -u GITHUB_TOKEN GITLAB_TOKEN=glpat-x vigie; echo "exit=$?"
```

Expected: stderr shows `GH_TOKEN and GITHUB_TOKEN are not set: …` with the `gh auth token`
hint, and `exit=3`. The recording proxy test (e2e) proves no connection was made.

```bash
GH_TOKEN=ghp_wrong vigie; echo "exit=$?"
```

Expected: `token rejected by github.com (401): …`, `exit=3`.

```bash
vigie -p cli/cli --host https://github.com -C /tmp -o json | jq .project      # outside a clone
vigie -p a/b/c --host https://github.com; echo "exit=$?"                       # path rule
```

Expected:
- the first command prints `"cli/cli"`;
- the second prints `project path a/b/c on github.com: GitHub repositories are owner/repo`
  and exits with `exit=2`.

### US1: renamed repository, SSH over 443

```bash
cd ~/gh-demo/gh-moved && vigie -v -o json 2>&1 >/dev/null | grep moved
cd ~/gh-demo/gh-ssh443 && vigie -o json | jq -r .project
```

Expected:
- **Renamed** (`gh-moved`): the record's project is `moby/moby`, and stderr has
  `repository moved to moby/moby: update the remote URL`.
- **SSH over 443** (`gh-ssh443`): prints `cli/cli`.

### US2: one table for both platforms

```bash
cd ~/gh-demo && vigie; echo "exit=$?"
env -u GH_TOKEN -u GITHUB_TOKEN vigie; echo "exit=$?"
vigie --host https://github.com
vigie -o json | jq -r '[.dir, .platform] | @tsv'
```

Expected:
- **First run**: a header `DIR  PLATFORM  PROJECT  BRANCHES  …`, rows for `gh-cli`, `gh-moved`
  and `gh-ssh443` (`github`), `glab` (`gitlab`) and `vigie` (`github`), and `exit=0`.
- **Without a GitHub token**: only the `glab` row. Stderr has `… 4 skipped (4 no token); run
  with -v to list them`; no auth error line; `exit=0`.
- **`--host https://github.com`**: only GitHub rows; `glab` counts as `other host`.
- **JSON**: each line carries its platform.

### US3: the same scripts on both platforms

```bash
cd ~/gh-demo && vigie -o json | jq -r 'select(.last.status=="failed") | .dir'
```

Expected: the clones whose last result failed, on either platform. Every line validates
against the schema (an e2e test checks keys; any 2020-12 validator can be used by hand).

### US4: watch within the allowance

```bash
cd ~/gh-demo/vigie
before=$(gh api rate_limit --jq '[.resources.core.used, .resources.graphql.used] | @tsv')
timeout 600 vigie -w -o json >/dev/null
after=$(gh api rate_limit --jq '[.resources.core.used, .resources.graphql.used] | @tsv')
echo "$before → $after"
```

Expected over 10 minutes at 5 s: REST grows by well under 400 and GraphQL by about 250
(1,500 an hour). That stays below SC-005's 2,500 an hour per pool. The automated test
`TestWatchAllowance` checks a simulated hour exactly (research G13).

**Exhausted allowance**: the e2e test points at `githubtest` with `RateLimited` set.

Expected:
- **One-shot**: `exit=5`, with the reset time on stderr.
- **Watch**: stale line, no request before the reset.
- **Scan**: one stderr line for the host, rows `⚠ network`.

### US5: GitHub Enterprise Server

```yaml
# $VIGIE_CONFIG
hosts:
  github.example.com:
    platform: github
    token_env: GHE_TOKEN
```

```bash
cd ~/src/some-ghes-clone && GHE_TOKEN=… vigie; echo "exit=$?"
env -u GH_ENTERPRISE_TOKEN -u GITHUB_ENTERPRISE_TOKEN GH_TOKEN=ghp_x vigie; echo "exit=$?"   # entry without token_env
```

Expected:
- **With the token**: the status line, `exit=0`.
- **Entry without `token_env`**: `GH_ENTERPRISE_TOKEN and GITHUB_ENTERPRISE_TOKEN are not
  set: …`, `exit=3`, and `GH_TOKEN` never sent. The e2e token-separation test proves the
  last point against `githubtest`.

Without an Enterprise Server at hand, the e2e suite runs the same scenarios against
`githubtest` through such an entry (`api: http://127.0.0.1:PORT`, `insecure: true`).

### SC-008: GitLab unchanged

```bash
cd ~/gh-demo/glab && vigie | cat; vigie -o json | jq -c 'del(.platform, .fetched_at)'
```

Expected: the plain line is byte-identical to v0.2.0's. The JSON record, without `platform`
and `fetched_at`, matches v0.2.0's.

## Cleanup

```bash
rm -rf ~/gh-demo
```
