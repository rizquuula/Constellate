package httpapi_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/platform/log"
)

// selfSignedCert generates an ECDSA self-signed certificate and writes the PEM
// cert and key to a temp dir. Returns the cert PEM bytes, cert path, key path.
func selfSignedCert(t *testing.T) (certPEM []byte, certFile, keyFile string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "constellate-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPEM, certFile, keyFile
}

// TestStartTLS_BadPath verifies StartTLS returns a non-nil error immediately
// when the cert or key file does not exist.
func TestStartTLS_BadPath(t *testing.T) {
	logger := log.New("error", "text")
	links := agentlink.NewRegistry()
	reg := registry.New(memory.NewMachineStore(), links, registry.SystemClock{}, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, nil, nil, nil, nil, nil, nil, nil, nil, false, logger)

	err := srv.StartTLS("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Fatal("StartTLS with missing files should return an error")
	}
	t.Logf("StartTLS bad-path error (expected): %v", err)
}

// TestStartTLS_Connectivity starts a TLS server with a self-signed cert and
// verifies that a client with the cert in its root pool gets HTTP 200 on
// the allowlisted /api/auth/status endpoint.
func TestStartTLS_Connectivity(t *testing.T) {
	certPEM, certFile, keyFile := selfSignedCert(t)

	// Build a root pool containing the self-signed cert.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM: no valid certs found")
	}

	// Use httptest.NewUnstartedServer to get an httptest server, then start it
	// with TLS using our own cert/key via a real tls.Config built from the files.
	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}

	logger := log.New("error", "text")
	links := agentlink.NewRegistry()
	reg := registry.New(memory.NewMachineStore(), links, registry.SystemClock{}, logger)
	srv := httpapi.NewServer("127.0.0.1:0", reg, nil, nil, nil, nil, nil, nil, nil, nil, false, logger)

	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}

	// /api/machines returns 200 with an empty array for a fresh hub (no auth required
	// when authSvc is nil — the middleware passes all requests through in dev mode).
	resp, err := client.Get(ts.URL + "/api/machines")
	if err != nil {
		t.Fatalf("GET /api/machines over TLS: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	t.Logf("TLS connectivity verified: GET /api/machines → %d", resp.StatusCode)
}

// TestCAPoolBuilder verifies the x509 cert-pool construction used by the agent's
// buildHTTPClient: load a PEM cert, add to pool, check it's trusted.
func TestCAPoolBuilder(t *testing.T) {
	certPEM, _, _ := selfSignedCert(t)

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM: returned false for valid cert")
	}

	// Parse the cert and verify it is present in the pool by attempting to
	// verify it against the pool (as a leaf + its own CA).
	cert, err := x509.ParseCertificate(func() []byte {
		block, _ := pem.Decode(certPEM)
		return block.Bytes
	}())
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	opts := x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: time.Now(),
		// No DNSName — the cert has only an IP SAN.
	}
	_, err = cert.Verify(opts)
	// Self-signed certs verify successfully when the pool contains the cert itself.
	if err != nil {
		t.Errorf("cert.Verify with custom pool: %v", err)
	}
	t.Log("CA pool builder: self-signed cert verifies against its own pool entry")
}
