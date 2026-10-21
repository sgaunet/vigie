package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sgaunet/vigie/internal/forge"
)

// project is the GitLab project JSON.
type project struct {
	ID            int64  `json:"id"`
	Path          string `json:"path_with_namespace"`
	WebURL        string `json:"web_url"`
	DefaultBranch string `json:"default_branch"` // "" for an empty project
	Archived      bool   `json:"archived"`
}

// Project looks up a project by path (group/sub/name) or numeric ID.
func (c *Client) Project(ctx context.Context, ref string) (forge.Project, error) {
	id := ref
	if strings.Trim(ref, "0123456789") != "" {
		id = url.PathEscape(ref)
	}
	var p project
	_, err := c.get(ctx, "/projects/"+id, nil, &p)
	var se *statusError
	if errors.As(err, &se) {
		switch se.code {
		case http.StatusForbidden:
			return forge.Project{}, fmt.Errorf("access to project %s %w (403): check that the token has the read_api scope and that you can see the project", ref, forge.ErrForbidden)
		case http.StatusNotFound:
			return forge.Project{}, fmt.Errorf("project %s %w on %s: check the hosts mapping in the config file", ref, forge.ErrNotFound, c.base)
		}
		return forge.Project{}, c.httpError(err)
	}
	if err != nil {
		return forge.Project{}, err
	}
	return forge.Project{ID: p.ID, Path: p.Path, WebURL: p.WebURL, DefaultBranch: p.DefaultBranch, Archived: p.Archived}, nil
}
