package scan

import (
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/repo"
)

// Repo is a listed repository: a row of the table.
type Repo struct {
	Rel      string
	Checkout repo.Checkout
	Endpoint config.Endpoint
	Err      error // set instead of Endpoint when it cannot be resolved; the row fails without a request
}

// Select keeps the repositories whose remote host is a known GitLab instance, or with
// --host only those on that instance, and resolves their endpoint as single mode would.
// It does no I/O; both results keep the input order.
func Select(in []Inspected, s config.Settings) ([]Repo, []Skip) {
	var (
		repos []Repo
		skips []Skip
	)
	for _, i := range in {
		host := i.Checkout.Remote.Host
		switch {
		case s.Host != nil && !s.OnHost(host, s.Host):
			skips = append(skips, Skip{Rel: i.Rel, Reason: ReasonOtherHost})
			continue
		case s.Host == nil && !s.KnownHost(host):
			skips = append(skips, Skip{Rel: i.Rel, Reason: ReasonUnknownHost})
			continue
		}
		ep, err := config.ResolveHost(s, host)
		repos = append(repos, Repo{Rel: i.Rel, Checkout: i.Checkout, Endpoint: ep, Err: err})
	}
	return repos, skips
}
