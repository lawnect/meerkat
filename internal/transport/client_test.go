package transport

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCredentialIsNotForwardedToRedirect(t *testing.T) {
	var leaked atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		w.WriteHeader(200)
	}))
	defer destination.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer origin.Close()
	path := filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: origin.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := New(origin.URL, path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Configuration(context.Background(), "secret-token")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 307 {
		t.Fatal("redirect not rejected")
	}
	if leaked.Load() != 0 {
		t.Fatal("credential leaked to redirect target")
	}
}

func TestUntrustedCertificateIsRejected(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer origin.Close()
	client, err := New(origin.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Configuration(context.Background(), "secret-token"); err == nil {
		t.Fatal("untrusted outlet certificate accepted")
	}
}
