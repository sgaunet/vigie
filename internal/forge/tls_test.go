package forge_test

import (
	"crypto/tls"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sgaunet/vigie/internal/forge"
)

// caOf returns the PEM certificate a test TLS server presents.
func caOf(srv *httptest.Server) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

func TestExtraRootsPEM(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the untrusted attempt below fails its handshake
	t.Cleanup(srv.Close)

	if code, err := get(t, newHTTPClient(t, forge.Options{ExtraRootsPEM: caOf(srv)}), srv.URL); err != nil || code != http.StatusOK {
		t.Fatalf("with the CA: code %d, err %v", code, err)
	}
	if _, err := get(t, newHTTPClient(t, forge.Options{}), srv.URL); err == nil {
		t.Fatal("reached a server whose CA is not trusted")
	}
	if _, err := forge.NewHTTPClient(forge.Options{ExtraRootsPEM: []byte("junk")}); err == nil {
		t.Fatal("junk PEM accepted")
	}
}

func TestMinimumTLSVersion(t *testing.T) {
	t.Parallel()
	tr, ok := newHTTPClient(t, forge.Options{}).Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("transport = %#v", tr)
	}
}
