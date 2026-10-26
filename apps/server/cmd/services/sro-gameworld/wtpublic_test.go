/*
===========================================================================

wtpublic_test.go - the public WebTransport posture through the GameWorld
config path

The transport package tests its own server against hand-built Config
structs. This file proves the assembled thing a real process boots:
shard catalog plus TRANSPORT_* environment plus the public switch,
through loadOwnedShard and gameWorldTransportServer - the one assembly
main() boots with - all the way to a WELCOME over the UDP listener
bound on a non-loopback address. It also proves the refusals hold at
this level:
a bind change without the switch, the switch without a key file, and
the switch without allowed origins each build nothing, and a forged
admission ticket never reaches WELCOME.

Local only: ephemeral ports and a generated fixture pair. No deployment
is touched, and nothing here starts without the switch being set first.

Scope, honestly: the environment-only boot is exercised (the Nomad
deployment path; a config file sets the same viper keys). The process
guard and newGameWorldApplication's heavier boot - game data, shard
lease, stores - stay out; only the identity verifiers that boot would
install are stubbed, exactly as the operations probe's test drives the
same server package.

===========================================================================
*/
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/webtransport-go"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/transport"
)

const (
	wtPublicShardID = "wtpublic-test"
	wtPublicTicket  = "wtpublic-test-ticket"
	wtPublicOrigin  = "https://play.example.test"
)

//============================================================================

/*
================
setWtPublicCatalog

Write the one-shard catalog this suite boots from and point the process
at it, the way Nomad points SRO_SHARD_CATALOG_PATH at an owned file.
================
*/
func setWtPublicCatalog(t *testing.T) {
	t.Helper()
	document := struct {
		Shards []shard.Definition `json:"shards"`
	}{
		Shards: []shard.Definition{
			{
				ID:             wtPublicShardID,
				Name:           "WebTransport public-posture test shard",
				NativeServerID: 1,
				NativeFarmID:   1,
				Capacity:       25,
				Default:        true,
				Test:           true,
				Enabled:        true,
				ControlURL:     "http://127.0.0.1:8797",
				TransportURL:   "https://127.0.0.1:8788",
			},
		},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shards.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(shard.EnvCatalogPath, path)
	t.Setenv("SRO_SHARD_ID", wtPublicShardID)
}

/*
================
setWtPublicEnv

Pin every environment value the assembled posture reads, so a value
leaking from the host shell cannot change what a case proves. The UDP
listener binds 0.0.0.0 - the non-loopback shape that is public the
moment the switch allows it; the TCP listener stays on loopback, as
behind a TLS edge.
================
*/
func setWtPublicEnv(t *testing.T, certFile, keyFile, allowedOrigins string, public bool) {
	t.Helper()
	setWtPublicCatalog(t)
	t.Setenv("TRANSPORT_WT_ADDR", "0.0.0.0:0")
	t.Setenv("TRANSPORT_WS_ADDR", "127.0.0.1:0")
	t.Setenv("TRANSPORT_CERT_FILE", certFile)
	t.Setenv("TRANSPORT_CERT_DIR", t.TempDir())
	t.Setenv("TRANSPORT_KEY_FILE", keyFile)
	t.Setenv("TRANSPORT_ALLOWED_ORIGINS", allowedOrigins)
	t.Setenv("TRANSPORT_PPROF", "")
	t.Setenv("SRO_GAMEWORLD_PRIVATE_NETWORK", "")
	if public {
		t.Setenv(transport.EnvWebTransportPublic, "1")
	} else {
		t.Setenv(transport.EnvWebTransportPublic, "")
	}
}

/*
================
assembleGameWorldTransport

The production assembly itself: the catalog load, then the same
gameWorldTransportServer call main() makes. No transport line is
reproduced here, so a posture line lost in server.go fails these
tests instead of passing against a copy.
================
*/
func assembleGameWorldTransport(t *testing.T) (*transport.Server, error) {
	t.Helper()
	ownedShard, err := loadOwnedShard()
	if err != nil {
		return nil, err
	}
	return gameWorldTransportServer(ownedShard)
}

/*
================
startPublicTransport

The positive fixture: a fixture pair generated in a temp dir (on a real
host the operator drops the same two files in place; either way the
server only ever reads them), the public switch on, and the server
assembled and started through the config path above.
================
*/
func startPublicTransport(t *testing.T) *transport.Server {
	t.Helper()
	certificate, err := transport.LoadCertificate("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	setWtPublicEnv(t, certificate.CertPath, certificate.KeyPath, wtPublicOrigin, true)
	srv, err := assembleGameWorldTransport(t)
	if err != nil {
		t.Fatalf("assembling the public posture: %v", err)
	}
	srv.Hub.SetHelloAuth(func(token []byte) (transport.AdmissionIdentity, error) {
		if string(token) != wtPublicTicket {
			return transport.AdmissionIdentity{}, errors.New("unexpected admission ticket")
		}
		return transport.AdmissionIdentity{AccountID: "wtpublic-account", ShardID: wtPublicShardID}, nil
	})
	// newGameWorldApplication installs the Agent-backed verifiers in
	// production. This suite's subject is the assembled posture, not
	// world entry, so the world gate admits without binding a world.
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, _ transport.EnterWorld) (bool, uint32) {
		return true, 0
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("starting the public posture: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

/*
================
advertisedCertHash

The hash the assembled server's TCP endpoint advertises for its UDP
listener - the same document a player's client would be handed.
================
*/
func advertisedCertHash(t *testing.T, srv *transport.Server) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://" + srv.WSAddr() + transport.PathCertHash)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cert-hash status %s", resp.Status)
	}
	var doc struct {
		SHA256Hex string `json:"sha256Hex"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc.SHA256Hex
}

/*
================
dialAndWelcome

One WebTransport admission against the assembled listener: normal PKI
verification against the fixture pair, narrowed by the pin to the
certificate the server was assembled with, then HELLO and the read for
WELCOME. The dial goes to the loopback interface of the 0.0.0.0 bind -
a public bind is what the posture promises, not a routable address.
================
*/
func dialAndWelcome(ctx context.Context, srv *transport.Server, ticket string) (*transport.Welcome, error) {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Cert.Leaf.Raw}))
	_, port, err := net.SplitHostPort(srv.WTAddr())
	if err != nil {
		return nil, err
	}
	dialer := webtransport.Transport{TLSClientConfig: &tls.Config{
		ServerName: "127.0.0.1",
		RootCAs:    roots,
		// Verification is never skipped: the pin only narrows what a
		// verified chain may end in, so a substituted certificate
		// fails closed too.
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("server presented no certificate")
			}
			got := sha256.Sum256(state.PeerCertificates[0].Raw)
			if got != srv.Cert.SHA256 {
				return fmt.Errorf("served certificate sha256 %x does not match the assembled pair %x", got, srv.Cert.SHA256)
			}
			return nil
		},
	}}
	defer dialer.Close()
	wtURL := "https://127.0.0.1:" + port + transport.PathWT
	rsp, sess, err := dialer.Dial(ctx, wtURL, nil)
	if err != nil {
		if rsp != nil && rsp.Body != nil {
			_ = rsp.Body.Close()
		}
		return nil, fmt.Errorf("dial %s: %w", wtURL, err)
	}
	stream, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	if err := stream.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, fmt.Errorf("stream deadline: %w", err)
	}
	err = transport.WriteStreamFrame(stream, transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(ticket)}),
	})
	if err != nil {
		return nil, fmt.Errorf("write HELLO: %w", err)
	}
	for {
		f, err := transport.ReadStreamFrame(stream)
		if err != nil {
			return nil, fmt.Errorf("read for WELCOME: %w", err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		if f.Opcode != transport.OpWelcome {
			return nil, fmt.Errorf("first frame = 0x%04X, want WELCOME", f.Opcode)
		}
		w, err := transport.DecodeWelcome(f.Payload)
		if err != nil {
			return nil, fmt.Errorf("decode WELCOME: %w", err)
		}
		_ = sess.CloseWithError(0, "wtpublic test complete")
		return &w, nil
	}
}

//============================================================================

/*
================
TestGameWorldServesPublicWebTransport

A fresh ticket earns WELCOME over the publicly bound UDP listener, and
the certificate served there is the one the TCP endpoint advertises -
everything reached through the GameWorld's own catalog and environment.
================
*/
func TestGameWorldServesPublicWebTransport(t *testing.T) {
	srv := startPublicTransport(t)
	if got, want := advertisedCertHash(t, srv), srv.Cert.SHA256Hex(); got != want {
		t.Fatalf("advertised cert hash = %s, want the assembled pair %s", got, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	welcome, err := dialAndWelcome(ctx, srv, wtPublicTicket)
	if err != nil {
		t.Fatalf("public WebTransport admission: %v", err)
	}
	if welcome.SessionID == 0 {
		t.Fatal("WELCOME carried an empty session id")
	}
}

/*
================
TestGameWorldRefusesUnsafeWebTransportPostures

Each unsafe posture is refused while assembling, with the refusal
naming the missing boundary, and a forged ticket against the good
posture never reaches WELCOME.
================
*/
func TestGameWorldRefusesUnsafeWebTransportPostures(t *testing.T) {
	certificate, err := transport.LoadCertificate("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		cert    string
		key     string
		origins string
		public  bool
		want    string
	}{
		{"public bind without the public switch", certificate.CertPath, certificate.KeyPath, wtPublicOrigin, false, "public WebTransport switch"},
		{"public switch without a key file", certificate.CertPath, "", wtPublicOrigin, true, "explicit certificate and key file"},
		{"public switch without allowed origins", certificate.CertPath, certificate.KeyPath, "", true, "requires allowed origins"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setWtPublicEnv(t, tc.cert, tc.key, tc.origins, tc.public)
			srv, err := assembleGameWorldTransport(t)
			if err == nil {
				t.Fatalf("unsafe posture assembled (server %v)", srv)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal = %q, want it to name %q", err, tc.want)
			}
		})
	}

	t.Run("forged admission ticket never reaches WELCOME", func(t *testing.T) {
		srv := startPublicTransport(t)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := dialAndWelcome(ctx, srv, "forged-ticket"); err == nil {
			t.Fatal("forged ticket reached WELCOME on the public posture")
		}
	})
}
