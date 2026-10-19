package gitlab

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// addRoots trusts the CAs in pemData in addition to the system pool. Certificate
// verification itself is never turned off.
func addRoots(cfg *tls.Config, pemData []byte) error {
	if len(pemData) == 0 {
		return nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return fmt.Errorf("%w: SSL_CERT_FILE holds no PEM certificate", ErrInsecure)
	}
	cfg.RootCAs = pool
	return nil
}
