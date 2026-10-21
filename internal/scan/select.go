package scan

import (
	"github.com/sgaunet/vigie/internal/config"
	"github.com/sgaunet/vigie/internal/repo"
	"github.com/sgaunet/vigie/internal/status"
)

// Repo is a listed repository: a row of the table.
type Repo struct {
	Rel      string
	Checkout repo.Checkout
	Endpoint config.Endpoint
	Err      error // set instead of Endpoint when it cannot be resolved; the row fails without a request
}

// Select keeps the repositories whose remote host is a known GitLab or GitHub instance, or
// with --host only those on that instance, and resolves their endpoint as single mode
// would. A GitHub repository whose host has no token source at all in env, or whose path is
// not owner/repo, is skipped. It does no I/O; both results keep the input order.
func Select(in []Inspected, s config.Settings, env map[string]string) ([]Repo, []Skip) {
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
		if err == nil && ep.Platform == status.PlatformGitHub {
			switch perr := config.CheckProject(ep, i.Checkout.Remote.Path); {
			case !ep.Token.Configured(env):
				skips = append(skips, Skip{Rel: i.Rel, Reason: ReasonNoToken})
				continue
			case perr != nil:
				skips = append(skips, Skip{Rel: i.Rel, Reason: ReasonRemoteURL, Err: perr})
				continue
			}
		}
		repos = append(repos, Repo{Rel: i.Rel, Checkout: i.Checkout, Endpoint: ep, Err: err})
	}
	return repos, skips
}
