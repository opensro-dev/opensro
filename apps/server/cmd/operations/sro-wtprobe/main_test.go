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
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

const probeTestTicket = "probe-test-ticket"

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
	srv, err := transport.NewServer(transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      10 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
		OutboundQueue:     16,
	})
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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

/*
================
TestProbeLoopbackWelcome

A fresh ticket earns WELCOME and a non-empty success line over real UDP.
================
*/
func TestProbeLoopbackWelcome(t *testing.T) {
	srv := startProbeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	line, err := probe(ctx, probeConfig{
		Base:    "http://" + srv.WSAddr(),
		WTAddr:  srv.WTAddr(),
		Ticket:  probeTestTicket,
		Timeout: 10 * time.Second,
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
TestProbeLoopbackBadTicketFailsAtWelcome

A ticket the verifier refuses never reaches WELCOME; the failure names
the welcome stage so an operator can route the alert.
================
*/
func TestProbeLoopbackBadTicketFailsAtWelcome(t *testing.T) {
	srv := startProbeServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := probe(ctx, probeConfig{
		Base:    "http://" + srv.WSAddr(),
		WTAddr:  srv.WTAddr(),
		Ticket:  "forged-ticket",
		Timeout: 10 * time.Second,
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
