package client_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youthesame/elport/internal/client"
	"github.com/youthesame/elport/internal/config"
	"github.com/youthesame/elport/internal/plan"
)

func tlsClientForServer(t *testing.T, srv *httptest.Server, apiKey string, opts ...client.Option) *client.Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	allOpts := append([]client.Option{client.WithRootCAs(pool)}, opts...)
	c, err := client.New(srv.URL, config.NewAPIKey(apiKey), true, allOpts...)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c
}

func TestClientRequiresHTTPS(t *testing.T) {
	if _, err := client.New("http://example.test", config.NewAPIKey("k"), true); err == nil {
		t.Fatal("expected error for plain http base_url")
	}
}

func TestClientRedirectBlocksCrossOriginAndHTTPDowngrade(t *testing.T) {
	// Never forward Authorization across HTTPS origin/port boundaries or to plain HTTP.
	var evilHit atomic.Bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	var otherOriginHit atomic.Bool
	otherTLS := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherOriginHit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer otherTLS.Close()

	var sameOriginAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/experiments/1":
			http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
		case "/api/v2/experiments/2":
			http.Redirect(w, r, otherTLS.URL+"/steal", http.StatusFound)
		case "/api/v2/experiments/3":
			http.Redirect(w, r, "/api/v2/experiments/30", http.StatusFound)
		case "/api/v2/experiments/30":
			sameOriginAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"id":30,"body":null}`))
		}
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "42-secret")
	if _, err := c.Get(context.Background(), "experiments", 1); err == nil {
		t.Fatal("expected HTTP downgrade redirect to fail")
	}
	if evilHit.Load() {
		t.Fatal("redirect hit plain HTTP server")
	}
	if _, err := c.Get(context.Background(), "experiments", 2); err == nil {
		t.Fatal("expected cross-origin HTTPS redirect to fail")
	}
	if otherOriginHit.Load() {
		t.Fatal("redirect hit different HTTPS origin")
	}

	got, err := c.Get(context.Background(), "experiments", 3)
	if err != nil {
		t.Fatalf("same-origin redirect failed: %v", err)
	}
	if sameOriginAuth != "42-secret" {
		t.Fatalf("same-origin redirect auth = %q, want 42-secret", sameOriginAuth)
	}
	if got["body"] != "" {
		t.Fatalf("null body not normalized to empty string: %#v", got["body"])
	}
}

func TestClientIdleTimeoutAllowsTrickleAndAbortsStall(t *testing.T) {
	// Active transfers that keep sending bytes must outlive the per-read idle
	// timeout, while stalled downloads must abort instead of hanging forever.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		switch r.URL.Path {
		case "/api/v2/experiments/1/uploads/10":
			w.WriteHeader(http.StatusOK)
			for i := range 6 {
				_, _ = fmt.Fprintf(w, "chunk-%d\n", i)
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(25 * time.Millisecond)
			}
		case "/api/v2/experiments/1/uploads/11":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "1-key", client.WithTimeouts(time.Second, 80*time.Millisecond))

	data, err := c.Download(context.Background(), "experiments", 1, 10)
	if err != nil {
		t.Fatalf("trickling download should succeed (>80ms total, 25ms between chunks): %v", err)
	}
	if !strings.Contains(string(data), "chunk-5") {
		t.Fatalf("incomplete trickled body: %q", string(data))
	}

	if _, err := c.Download(context.Background(), "experiments", 1, 11); err == nil {
		t.Fatal("stalled download should time out")
	}
}

func TestClientEndpointsCachingErrorsAndJSONContentType(t *testing.T) {
	var meCalls, infoCalls int32
	var linkContentType string
	var createdPayload map[string]any

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/users/me":
			atomic.AddInt32(&meCalls, 1)
			_, _ = w.Write([]byte(`{"userid":7,"fullname":"Ada"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
			atomic.AddInt32(&infoCalls, 1)
			_, _ = w.Write([]byte(`{"elabftw_version":"6.0.9"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments":
			_ = json.NewDecoder(r.Body).Decode(&createdPayload)
			w.Header().Set("Location", "/api/v2/experiments/99")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/experiments/99/items_links/5":
			// eLabFTW returns 400 without application/json Content-Type even for empty body.
			linkContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/404":
			http.Error(w, "missing", http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/experiments/403":
			http.Error(w, "denied", http.StatusForbidden)
		}
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "123-secret-token")
	if got := c.KeyIDPrefix(); got != "123" {
		t.Fatalf("KeyIDPrefix = %q, want 123", got)
	}

	_, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me #1: %v", err)
	}
	_, err = c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me #2: %v", err)
	}
	if meCalls != 1 {
		t.Errorf("Me called server %d times, want 1", meCalls)
	}

	_, _ = c.Info(context.Background())
	_, _ = c.Info(context.Background())
	if infoCalls != 1 {
		t.Errorf("Info called server %d times, want 1", infoCalls)
	}

	created, err := c.Create(context.Background(), "experiments", "New Exp")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created["id"] != "99" || createdPayload["content_type"] != float64(2) {
		t.Fatalf("Create result=%#v payload=%#v", created, createdPayload)
	}

	if err := c.AddLink(context.Background(), "experiments", 99, "items", 5); err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if linkContentType != "application/json" {
		t.Fatalf("AddLink Content-Type = %q, want application/json", linkContentType)
	}

	_, err = c.Get(context.Background(), "experiments", 404)
	var notFound client.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError on 404, got %T: %v", err, err)
	}

	_, err = c.Get(context.Background(), "experiments", 403)
	if err == nil || !strings.Contains(err.Error(), "this API key may not GET experiments/403:") {
		t.Fatalf("unexpected 403 error: %v", err)
	}

	var zeroApproved plan.ApprovedUploadPath
	if _, err := c.Upload(context.Background(), "experiments", 99, zeroApproved); err == nil {
		t.Fatal("expected Upload to reject zero ApprovedUploadPath")
	}
}

func TestClientCreateJSONNullBodyWithLocationDoesNotPanic(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/api/v2/experiments/77")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("null"))
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "1-key")
	got, err := c.Create(context.Background(), "experiments", "T")
	if err != nil {
		t.Fatalf("Create with null body: %v", err)
	}
	if got["id"] != "77" {
		t.Fatalf("Create id = %#v, want 77", got["id"])
	}
}

func TestClientIDNAHostRedirectRejectedAndStalledRedirectBodyTimesOut(t *testing.T) {
	// IDNA homograph İ.example (xn--i-9bb.example) must not match i.example,
	// and a stalled redirect response body must abort via the conn idle deadline.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		switch r.URL.Path {
		case "/api/v2/experiments/1":
			http.Redirect(w, r, "https://\u0130.example/steal", http.StatusFound)
		case "/api/v2/experiments/2":
			w.Header().Set("Location", "/api/v2/experiments/20")
			w.WriteHeader(http.StatusFound)
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "1-key", client.WithTimeouts(time.Second, 80*time.Millisecond))
	if _, err := c.Get(context.Background(), "experiments", 1); err == nil {
		t.Fatal("expected IDNA cross-origin redirect to fail")
	}
	if _, err := c.Get(context.Background(), "experiments", 2); err == nil {
		t.Fatal("expected stalled redirect body to time out")
	}
}

func TestClientHonorsSSLCertFileEnv(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"elabftw_version":"6.0.0"}`))
	}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", caPath)

	c, err := client.New(srv.URL, config.NewAPIKey("1-key"), true)
	if err != nil {
		t.Fatalf("client.New with SSL_CERT_FILE: %v", err)
	}
	info, err := c.Info(context.Background())
	if err != nil || info["elabftw_version"] != "6.0.0" {
		t.Fatalf("Info via SSL_CERT_FILE = %v, err=%v", info, err)
	}

	// REQUESTS_CA_BUNDLE fallback keeps Python-configured environments working.
	t.Setenv("SSL_CERT_FILE", "")
	t.Setenv("REQUESTS_CA_BUNDLE", caPath)
	c2, err := client.New(srv.URL, config.NewAPIKey("1-key"), true)
	if err != nil {
		t.Fatalf("client.New with REQUESTS_CA_BUNDLE: %v", err)
	}
	info2, err := c2.Info(context.Background())
	if err != nil || info2["elabftw_version"] != "6.0.0" {
		t.Fatalf("Info via REQUESTS_CA_BUNDLE = %v, err=%v", info2, err)
	}
}

func TestClientUploadRefusesPostApprovalSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privilege on Windows")
	}
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.csv")
	secretPath := filepath.Join(dir, ".secret")
	if err := os.WriteFile(dataPath, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := plan.Approve("[d](data.csv)\n", dir, nil, nil, nil, nil, nil)
	if err != nil || len(res.Files) != 1 {
		t.Fatalf("Approve: %v", err)
	}
	if err := os.Remove(dataPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, dataPath); err != nil {
		t.Fatal(err)
	}

	uploaded := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploaded = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := tlsClientForServer(t, srv, "1-key")
	if _, err := c.Upload(context.Background(), "experiments", 1, res.Files[0]); err == nil {
		t.Fatal("expected Upload to refuse post-approval symlink swap")
	}
	if uploaded {
		t.Fatal("server must not receive any upload request after symlink swap")
	}
}
