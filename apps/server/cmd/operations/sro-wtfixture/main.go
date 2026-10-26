/*
===========================================================================

main.go - sro-wtfixture: the loopback transport fixture for sro-wtprobe

The measurement client (cmd/operations/sro-wtprobe -measure) speaks to a
live GameWorld's transport edge. This fixture stands the same production
transport server (internal/transport, the Hub the game serves on) up on
loopback with one fixture attachment — an admission verifier that accepts
a single documented ticket — so the probe's arms can run against an
isolated server with a recorded admission budget, no live shard and no
authority store:

	go run ./cmd/operations/sro-wtfixture -certs .state/wtfixture-certs

then, with the ticket the fixture prints:

	SRO_WT_PROBE_TICKET=loopback-fixture-ticket \
	go run ./cmd/operations/sro-wtprobe \
		-url http://127.0.0.1:18087 -transport ws -measure

	SRO_WT_PROBE_TICKET=loopback-fixture-ticket \
	go run ./cmd/operations/sro-wtprobe \
		-url http://127.0.0.1:18087 -transport wt -wt-addr 127.0.0.1:18088 \
		-ca-cert .state/wtfixture-certs/wt-dev-cert.pem -measure

Fixture-only rules, deliberately unlike production: the ticket is
reusable (production tickets are Agent-minted and consumed by the
WELCOME), the account is a constant, and the certificate is the
transport's managed self-signed development pair, pinned by the probe
through the served cert-hash and trusted through -ca-cert. The fixture
answers HELLO/WELCOME and the probe's ping train; it enters no world
and runs no game logic. Loopback is the default binding; pointing it
elsewhere is a deployment decision the transport's own public-listener
guards still refuse without the production certificate and switch.

===========================================================================
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"opensro.online/server/internal/transport"
)

// The one ticket the fixture verifier accepts, and the account it maps
// to. Named in the banner and printed at startup: a measurement that
// cannot state its admission is not a measurement.
const (
	fixtureTicket  = "loopback-fixture-ticket"
	fixtureAccount = "wtprobe-fixture"
	fixtureShard   = "global-official"
)

//============================================================================
// Fixture server
//============================================================================

/*
================
newFixtureServer

The production transport server with two fixture attachments: the hello
verifier (the fixture ticket only, always the fixture account) and an
enter-world stub (any authenticated session may pass; the probe never
sends game input, so nothing downstream of enter-world is reached). No
game handlers are registered: the probe measures the transport lanes,
not gameplay.
================
*/
func newFixtureServer(certDir, wtAddr, wsAddr string) (*transport.Server, error) {
	srv, err := transport.NewServer(transport.Config{
		WTAddr:            wtAddr,
		WSAddr:            wsAddr,
		CertDir:           certDir,
		HelloTimeout:      5 * time.Second,
		GracePeriod:       10 * time.Second,
		KeepaliveInterval: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		OutboundQueue:     256,
	})
	if err != nil {
		return nil, err
	}
	srv.Hub.SetHelloAuth(func(tok []byte) (transport.AdmissionIdentity, error) {
		if string(tok) != fixtureTicket {
			return transport.AdmissionIdentity{}, fmt.Errorf("not the fixture ticket")
		}
		return transport.AdmissionIdentity{AccountID: fixtureAccount, ShardID: fixtureShard}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, ew transport.EnterWorld) (bool, uint32) {
		return len(ew.AuthToken) > 0, 0xA1
	})
	if err := srv.Start(); err != nil {
		return nil, err
	}
	return srv, nil
}

//============================================================================
// Entry point
//============================================================================

/*
================
main

Serve until interrupted. Startup states the two endpoint addresses, the
certificate file the WT arm must trust, and the ticket both arms present;
the runbook above turns those into the two probe commands.
================
*/
func main() {
	wtAddr := flag.String("wt", "127.0.0.1:18088", "WebTransport listen address")
	wsAddr := flag.String("ws", "127.0.0.1:18087", "WebSocket listen address")
	certDir := flag.String("certs", "", "certificate directory (default: a throwaway temp dir)")
	flag.Parse()

	if *certDir == "" {
		tmp, err := os.MkdirTemp("", "sro-wtfixture-certs-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "sro-wtfixture:", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmp)
		*certDir = tmp
	}

	srv, err := newFixtureServer(*certDir, *wtAddr, *wsAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sro-wtfixture:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "sro-wtfixture: ws=%s wt=%s cert=%s ticket=%s\n",
		srv.WSAddr(), srv.WTAddr(), filepath.Join(*certDir, "wt-dev-cert.pem"), fixtureTicket)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintln(os.Stderr, "sro-wtfixture:", err)
		os.Exit(1)
	}
}
