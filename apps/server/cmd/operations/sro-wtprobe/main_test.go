/*
===========================================================================

main_test.go - the probe against a real loopback transport server

The probe's contract is end-to-end (cert-hash over TCP, HELLO/WELCOME
over UDP with the served certificate pinned), so the test stands up the
production transport server on loopback with a development certificate
and drives the same probe function the command runs. This is also the
local and LAN half of the one-code-path promise; the cloud half differs
only in the -url and -wt-addr values.

===========================================================================
*/

package main

import (
	"context"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

const (
	probeTestTicket          = "probe-test-ticket"
	probeTestIOTimeout       = 10 * time.Second
	probeTestConnectTimeout  = 15 * time.Second
	probeTestShutdownTimeout = 3 * time.Second
	probeTestIdleTimeout     = 30 * time.Second
	probeTestOutboundQueue   = 16
	probeTestCAFileMode      = 0o600
)

//============================================================================

/*
================
startProbeServer

One production transport server whose admission stub accepts exactly
probeTestTicket, on ephemeral loopback TCP and UDP ports.
================
*/
func startProbeServer(t *testing.T) *transport.Server {
	t.Helper()
	return startProbeServerWithConfig(t, probeTestServerConfig(t))
}

/*
================
probeTestServerConfig
================
*/
func probeTestServerConfig(t *testing.T) transport.Config {
	t.Helper()
	return transport.Config{
		WTAddr: "127.0.0.1:0", WSAddr: "127.0.0.1:0", CertDir: t.TempDir(),
		HelloTimeout: probeTestIOTimeout, GracePeriod: probeTestIOTimeout,
		KeepaliveInterval: probeTestIOTimeout, IdleTimeout: probeTestIdleTimeout, OutboundQueue: probeTestOutboundQueue,
	}
}

/*
================
startProbeServerWithConfig

Measurement tests declare their socket and admission budgets explicitly;
the ordinary smoke fixture continues using its original settings.
================
*/
func startProbeServerWithConfig(t *testing.T, cfg transport.Config) *transport.Server {
	t.Helper()
	srv, err := transport.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.Hub.SetHelloAuth(func(token []byte) (transport.AdmissionIdentity, error) {
		if string(token) != probeTestTicket {
			return transport.AdmissionIdentity{}, errors.New("unexpected probe ticket")
		}
		return transport.AdmissionIdentity{AccountID: "probe", ShardID: "global-official"}, nil
	})
	// The probe never binds a world; the gate only needs to exist.
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, _ transport.EnterWorld) (bool, uint32) {
		return true, 0
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), probeTestShutdownTimeout)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

/*
================
caCertFile

Write the server's self-signed certificate as the PEM bundle a local
probe would be handed with -ca-cert.
================
*/
func caCertFile(t *testing.T, srv *transport.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Cert.Leaf.Raw})
	if err := os.WriteFile(path, raw, probeTestCAFileMode); err != nil {
		t.Fatal(err)
	}
	return path
}

/*
================
TestProbeLoopbackWelcome

A fresh ticket earns WELCOME and a non-empty success line over real UDP.
================
*/
func TestProbeLoopbackWelcome(t *testing.T) {
	srv := startProbeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	line, err := probe(ctx, probeConfig{
		Base:    "http://" + srv.WSAddr(),
		WTAddr:  srv.WTAddr(),
		Ticket:  probeTestTicket,
		Timeout: probeTestIOTimeout,
		CACert:  caCertFile(t, srv),
	})
	if err != nil {
		t.Fatal(err)
	}
	if line == "" {
		t.Fatal("probe returned an empty success line")
	}
	t.Log(line)
}

/*
================
TestProbeSubstitutedCertFailsPin

Both loopback servers' certificates are trusted, but the probe pins the
one the TCP endpoint advertised: dialing the other server must fail at
the dial stage with the pin mismatch named.
================
*/
func TestProbeSubstitutedCertFailsPin(t *testing.T) {
	advertised := startProbeServer(t)
	other := startProbeServer(t)
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: advertised.Cert.Leaf.Raw})
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: other.Cert.Leaf.Raw})...)
	if err := os.WriteFile(bundle, raw, probeTestCAFileMode); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	_, err := probe(ctx, probeConfig{
		Base:    "http://" + advertised.WSAddr(),
		WTAddr:  other.WTAddr(),
		Ticket:  probeTestTicket,
		Timeout: probeTestIOTimeout,
		CACert:  bundle,
	})
	if err == nil {
		t.Fatal("probe with substituted certificate succeeded")
	}
	var se *stageError
	if !errors.As(err, &se) {
		t.Fatalf("probe error type = %T, want *stageError (%v)", err, err)
	}
	if se.stage != "dial" {
		t.Fatalf("probe error stage = %q, want dial (%v)", se.stage, err)
	}
}

/*
================
TestProbeLoopbackBadTicketFailsAtWelcome

A ticket the verifier refuses never reaches WELCOME; the failure names
the welcome stage so an operator can route the alert.
================
*/
func TestProbeLoopbackBadTicketFailsAtWelcome(t *testing.T) {
	srv := startProbeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	_, err := probe(ctx, probeConfig{
		Base:    "http://" + srv.WSAddr(),
		WTAddr:  srv.WTAddr(),
		Ticket:  "forged-ticket",
		Timeout: probeTestIOTimeout,
		CACert:  caCertFile(t, srv),
	})
	if err == nil {
		t.Fatal("probe with forged ticket succeeded")
	}
	var se *stageError
	if !errors.As(err, &se) {
		t.Fatalf("probe error type = %T, want *stageError (%v)", err, err)
	}
	if se.stage != "welcome" {
		t.Fatalf("probe error stage = %q, want welcome (%v)", se.stage, err)
	}
}
