package gitlab_test

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/sgaunet/vigie/internal/gitlab"
	"github.com/sgaunet/vigie/internal/token"
)

func TestExtraRootsPEM(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"path_with_namespace":"g/p","default_branch":"main","web_url":"https://x/g/p"}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	c, err := gitlab.NewClient(u, token.New("t"), gitlab.Options{ExtraRootsPEM: ca})
	if err != nil {
		t.Fatal(err)
	}
	if p, err := c.Project(context.Background(), "g/p"); err != nil || p.ID != 1 {
		t.Fatalf("with the CA: %+v, %v", p, err)
	}

	c, err = gitlab.NewClient(u, token.New("t"), gitlab.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Project(context.Background(), "g/p"); !errors.Is(err, gitlab.ErrNetwork) {
		t.Fatalf("without the CA: got %v, want ErrNetwork", err)
	}

	if _, err := gitlab.NewClient(u, token.New("t"), gitlab.Options{ExtraRootsPEM: []byte("junk")}); err == nil {
		t.Fatal("junk PEM accepted")
	}
}
