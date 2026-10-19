package repo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sgaunet/vigie/internal/repo"
)

func TestParseRemote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in         string
		host, path string
	}{
		{"git@gitlab.com:group/sub/project.git", "gitlab.com", "group/sub/project"},
		{"git@gitlab.com:group/project", "gitlab.com", "group/project"},
		{"gitlab.com:group/project.git", "gitlab.com", "group/project"},
		{"ssh://git@gitlab.example.com:2222/group/project.git", "gitlab.example.com", "group/project"},
		{"ssh://gitlab.example.com/group/project/", "gitlab.example.com", "group/project"},
		{"https://user:pass@gitlab.example.com/group/project.git", "gitlab.example.com", "group/project"},
		{"https://gitlab.example.com:8443/group/project.git/", "gitlab.example.com", "group/project"},
		{"http://gitlab.lan/group/project", "gitlab.lan", "group/project"},
		{"https://GitLab.com/a/b/c/d/project.git", "gitlab.com", "a/b/c/d/project"},
		{"git@gitlab.com:a/b/c/d/project.git/", "gitlab.com", "a/b/c/d/project"},
	}
	for _, c := range cases {
		r, err := repo.ParseRemote(c.in)
		if err != nil {
			t.Errorf("ParseRemote(%q): %v", c.in, err)
			continue
		}
		if r.Host != c.host || r.Path != c.path {
			t.Errorf("ParseRemote(%q) = %+v, want %s %s", c.in, r, c.host, c.path)
		}
	}
}

func TestParseRemoteInvalid(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"file:///x/project.git",
		"/local/path",
		"gitlab.com",
		"",
		"git@gitlab.com:project.git",
		"https://gitlab.com/project",
		"https://user:s3cr3t@gitlab.com/project",
		"ftp://gitlab.com/group/project",
		"https://gitlab.com//project",
		"git@:group/project",
	} {
		_, err := repo.ParseRemote(in)
		if !errors.Is(err, repo.ErrRemoteURL) {
			t.Errorf("ParseRemote(%q): got %v, want ErrRemoteURL", in, err)
			continue
		}
		if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "user:") {
			t.Errorf("ParseRemote(%q): message leaks credentials: %q", in, err)
		}
	}
}
