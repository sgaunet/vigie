package github

// Named GraphQL documents: githubtest dispatches on the operation name. Connections need
// first or last, so the counts ask for one node and read only totalCount.
const (
	branchesQuery = `query Branches($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/heads/", first: 100, after: $cursor) {
      totalCount
      pageInfo { hasNextPage endCursor }
      nodes { name target { ... on Commit { committedDate } } }
    }
  }
}`

	pullRequestsQuery = `query PullRequests($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    pullRequests(states: OPEN, first: 1) { totalCount }
  }
}`

	issuesQuery = `query Issues($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    hasIssuesEnabled
    issues(states: OPEN, first: 1) { totalCount }
  }
}`
)
