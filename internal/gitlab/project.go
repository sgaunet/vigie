package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Project is the GitLab project behind the repository.
type Project struct {
	ID            int64  `json:"id"`
	Path          string `json:"path_with_namespace"`
	WebURL        string `json:"web_url"`
	DefaultBranch string `json:"default_branch"` // "" for an empty project
	Archived      bool   `json:"archived"`
}

// Project looks up a project by path (group/sub/name) or numeric ID.
func (c *Client) Project(ctx context.Context, ref string) (Project, error) {
	id := ref
	if strings.Trim(ref, "0123456789") != "" {
		id = url.PathEscape(ref)
	}
	var p Project
	_, err := c.get(ctx, "/projects/"+id, nil, &p)
	var se *statusError
	if errors.As(err, &se) {
		switch se.code {
		case http.StatusForbidden:
			return Project{}, fmt.Errorf("access to project %s %w (403): check that the token has the read_api scope and that you can see the project", ref, ErrForbidden)
		case http.StatusNotFound:
			return Project{}, fmt.Errorf("project %s %w on %s: check the hosts mapping in the config file", ref, ErrProjectNotFound, c.base)
		}
		return Project{}, c.httpError(err)
	}
	if err != nil {
		return Project{}, err
	}
	return p, nil
}
