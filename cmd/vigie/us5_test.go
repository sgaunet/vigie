package main_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestE2EFieldSelection(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t, "-f", "pipelines,last,mrs", "--no-emoji")
	if code != 0 || out != "group/app ci 2r/1p last:ok main 3m mr 5\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

func TestE2EInvalidFields(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, errOut, code := e.run(t, "-f", "mrs,mrs")
	if code != 2 || out != "" || !strings.Contains(errOut, "branches, pipelines, last, mrs, issues") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if n := len(e.srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent despite the usage error", n)
	}
}

func TestE2ENoColor(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	out, _, code := checkNoToken(t)(runVigie(t, e.repo, append(e.env, "NO_COLOR=1")))
	if code != 0 || strings.ContainsRune(out, '\x1b') {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
}

func TestE2EConfigFields(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	cfg := writeConfig(t, fmt.Sprintf("fields: [mrs]\nhosts:\n  gitlab.test:\n    api: %s\n    insecure: true\n", e.srv.URL()))
	out, errOut, code := checkNoToken(t)(runVigie(t, e.repo, []string{cfg, "GITLAB_TOKEN=" + e2eToken}))
	if code != 0 || out != "group/app mr 5\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}
