/*
===========================================================================

wt_degrade_test.go - a public pair failing at boot disables WebTransport only

Owner 2026-10-27 (#1447): with public WebTransport on, a TLS pair that
fails at boot (expired, missing, unreadable) must not take WebSocket down
with it. The server starts without its UDP listener, says why in its
metrics, answers the cert-hash bootstrap with 503 (the client's dial then
fails against no UDP listener and it falls back to WebSocket), and shuts
down cleanly. Without the public switch a
broken explicit pair still refuses the boot.

===========================================================================
*/

package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const wtDegradeShutdown = 5 * time.Second

/*
================
expiredPair

A self-signed pair whose validity ended weeks ago: the development
generator with its clock set back past its whole validity.
================
*/
func expiredPair(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	if _, err := generateDevelopmentCertificate(certPath, keyPath, filepath.Join(dir, "hash.json"), time.Now().Add(-3*devCertValidity)); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

/*
================
publicConfig
================
*/
func publicConfig(certPath, keyPath string) Config {
	return Config{
		WTAddr: "127.0.0.1:0", WSAddr: "127.0.0.1:0",
		CertFile: certPath, KeyFile: keyPath,
		WebTransportPublic: true,
		AllowedOrigins:     []string{"https://play.example.test"},
	}
}

/*
================
getStatus
================
*/
func getStatus(t *testing.T, url string) (int, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

/*
================
TestPublicPairFailingAtBootDisablesOnlyWebTransport
================
*/
func TestPublicPairFailingAtBootDisablesOnlyWebTransport(t *testing.T) {
	expiredCert, expiredKey := expiredPair(t)
	missing := filepath.Join(t.TempDir(), "absent.pem")
	for _, tc := range []struct {
		name, cert, key, reason string
	}{
		{"expired", expiredCert, expiredKey, "not valid at"},
		{"missing", missing, missing, "loading TLS certificate pair"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := NewServer(publicConfig(tc.cert, tc.key))
			if err != nil {
				t.Fatalf("a failing public pair refused the boot: %v", err)
			}
			if server.Cert != nil || !strings.Contains(server.WebTransportDisabled(), tc.reason) {
				t.Fatalf("cert %v, disabled %q; want no cert and a reason naming %q", server.Cert, server.WebTransportDisabled(), tc.reason)
			}
			// A public start requires both admission gates (security_test.go).
			server.Hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
				return AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
			})
			server.Hub.SetEnterWorldAuth(func(*Session, EnterWorld) (bool, uint32) { return true, 0 })
			if err := server.Start(); err != nil {
				t.Fatalf("start without WebTransport: %v", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), wtDegradeShutdown)
				defer cancel()
				if err := server.Shutdown(ctx); err != nil {
					t.Errorf("shutdown: %v", err)
				}
			}()
			if addr := server.WTAddr(); addr != "" {
				t.Fatalf("a UDP listener is bound at %s with WebTransport disabled", addr)
			}
			base := "http://" + server.WSAddr()
			if status, _ := getStatus(t, base+PathHealth); status != http.StatusOK {
				t.Fatalf("health over the WebSocket listener = %d, want 200", status)
			}
			if status, _ := getStatus(t, base+PathCertHash); status != http.StatusServiceUnavailable {
				t.Fatalf("cert-hash = %d, want 503 with WebTransport disabled", status)
			}
			status, body := getStatus(t, base+PathMetrics)
			var metrics struct {
				WebTransportDisabled        string `json:"webtransport_disabled"`
				CertificateSecondsRemaining int64  `json:"certificate_seconds_remaining"`
			}
			if status != http.StatusOK || json.Unmarshal([]byte(body), &metrics) != nil {
				t.Fatalf("metrics = %d %q", status, body)
			}
			if !strings.Contains(metrics.WebTransportDisabled, tc.reason) || metrics.CertificateSecondsRemaining != 0 {
				t.Fatalf("metrics disabled %q remaining %d; want the reason and 0", metrics.WebTransportDisabled, metrics.CertificateSecondsRemaining)
			}
		})
	}
}

/*
================
TestExplicitPairFailingWithoutTheSwitchStillRefusesBoot

Only the public posture degrades: a broken explicit pair on a loopback,
non-public deployment is a configuration error and stays fatal.
================
*/
func TestExplicitPairFailingWithoutTheSwitchStillRefusesBoot(t *testing.T) {
	certPath, keyPath := expiredPair(t)
	config := publicConfig(certPath, keyPath)
	config.WebTransportPublic = false
	config.AllowedOrigins = nil
	if _, err := NewServer(config); err == nil || !strings.Contains(err.Error(), "not valid at") {
		t.Fatalf("NewServer = %v, want the expired-pair refusal", err)
	}
}
