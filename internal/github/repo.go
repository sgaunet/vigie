package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sgaunet/vigie/internal/forge"
	"github.com/sgaunet/vigie/internal/status"
)

// repository is the REST repository JSON.
type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
}

// Project looks up a repository by path (owner/repo) or numeric ID. A renamed or
// transferred repository is followed: Path is its new full name.
func (c *Client) Project(ctx context.Context, ref string) (forge.Project, error) {
	path := "/repositories/" + ref
	if strings.Trim(ref, "0123456789") != "" {
		path = "/repos/" + escapePath(ref)
	}
	var r repository
	if _, err := c.get(ctx, path, nil, &r); err != nil {
		return forge.Project{}, c.lookupError(ref, err)
	}
	return forge.Project{ID: r.ID, Path: r.FullName, WebURL: r.HTMLURL, DefaultBranch: r.DefaultBranch, Archived: r.Archived}, nil
}

// lookupError gives a refused lookup its hint: GitHub answers 404 for a repository the
// token cannot see, and 403 for single sign-on or a missing permission it names.
func (c *Client) lookupError(ref string, err error) error {
	var se *statusError
	if !errors.As(err, &se) {
		return err
	}
	switch se.code {
	case http.StatusForbidden:
		switch perm := se.header.Get("X-Accepted-GitHub-Permissions"); {
		case se.header.Get("X-GitHub-SSO") != "":
			return fmt.Errorf("access to repository %s %w (403): authorize the token for the organization's SAML single sign-on", ref, forge.ErrForbidden)
		case perm != "":
			return fmt.Errorf("access to repository %s %w (403): the token lacks the permission %q", ref, forge.ErrForbidden, perm)
		}
		return fmt.Errorf("access to repository %s %w (403)", ref, forge.ErrForbidden)
	case http.StatusNotFound:
		return fmt.Errorf("repository %s %w on %s: check that the token can see it (for a fine-grained token, add the repository to it; for an organization with single sign-on, authorize the token)",
			ref, forge.ErrNotFound, c.host)
	}
	return c.httpError(err)
}

// Source returns the metrics source of p; it implements forge.Client.
func (c *Client) Source(p forge.Project) status.Source { return &repoSource{c: c, p: p} }

// escapePath escapes each segment of owner/repo.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
