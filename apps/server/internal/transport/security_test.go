package transport

import (
	"errors"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

func TestSessionCapacityIsBounded(t *testing.T) {
	cfg := testCfg()
	cfg.MaxSessions = 1
	hub := newHub(cfg)

	first, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.createSession(); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("second session = %v, want ErrSessionCapacity", err)
	}

	hub.closeSession(first, nil)
	if _, err := hub.createSession(); err != nil {
		t.Fatalf("capacity was not released after final teardown: %v", err)
	}
}

func TestPendingHandshakeCapacityRejectsImmediately(t *testing.T) {
	cfg := testCfg()
	cfg.MaxPendingHandshakes = 1
	cfg.HelloTimeout = time.Minute
	hub := newHub(cfg)

	waiting := newFakeConn(false)
	go hub.AcceptConn(waiting)
	wait.Eventually(t, time.Second, "the first handshake to occupy its admission slot", func() bool {
		return len(hub.handshakeSlots) == 1
	})

	rejected := newFakeConn(false)
	hub.AcceptConn(rejected)
	written := rejected.written()
	if len(written) != 1 || written[0].Opcode != OpBye ||
		len(written[0].Payload) != 1 || written[0].Payload[0] != ByeReasonServerBusy {
		t.Fatalf("capacity rejection frames = %+v, want BYE(ServerBusy)", written)
	}
	select {
	case <-rejected.closed:
	default:
		t.Fatal("capacity-rejected connection remained open")
	}

	_ = waiting.Close("test complete")
}

func TestServerRefusesStartupWithoutHelloAdmissionVerifier(t *testing.T) {
	server, err := NewServer(Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err == nil || !strings.Contains(err.Error(), "admission verifier") {
		t.Fatalf("Start error = %v, want missing HELLO admission verifier", err)
	}
}

func TestServerRefusesStartupWithoutEnterWorldAuthVerifier(t *testing.T) {
	server, err := NewServer(Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
		return AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
	})
	if err := server.Start(); err == nil || !strings.Contains(err.Error(), "EnterWorld authentication verifier") {
		t.Fatalf("Start error = %v, want missing EnterWorld authentication verifier", err)
	}
}

func TestInboundFrameLimitsApplyAcrossConnImplementations(t *testing.T) {
	cfg := testCfg()
	hub := newHub(cfg)

	t.Run("oversized HELLO", func(t *testing.T) {
		conn := newFakeConn(false)
		conn.inbound <- Frame{
			Opcode:  OpHello,
			Payload: make([]byte, MaxInboundFrameBytes),
		}
		hub.AcceptConn(conn)
		written := conn.written()
		if len(written) != 1 || written[0].Opcode != OpBye ||
			len(written[0].Payload) != 1 || written[0].Payload[0] != ByeReasonProtocolErr {
			t.Fatalf("oversized HELLO response = %+v, want BYE(ProtocolErr)", written)
		}
	})

	t.Run("oversized attached frame", func(t *testing.T) {
		session, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		conn := newFakeConn(false)
		if err := session.attach(conn, false, 0); err != nil {
			t.Fatal(err)
		}
		conn.inbound <- Frame{Opcode: 0x727A, Payload: make([]byte, MaxInboundFrameBytes)}
		waitUntil(t, "oversized session teardown", func() bool {
			_, ok := hub.Session(session.ID)
			return !ok
		})
	})

	t.Run("oversized PING echo", func(t *testing.T) {
		session, err := hub.createSession()
		if err != nil {
			t.Fatal(err)
		}
		conn := newFakeConn(false)
		if err := session.attach(conn, false, 0); err != nil {
			t.Fatal(err)
		}
		conn.inbound <- Frame{Opcode: OpPing, Payload: make([]byte, MaxPingPayloadBytes+1)}
		waitUntil(t, "oversized PING teardown", func() bool {
			_, ok := hub.Session(session.ID)
			return !ok
		})
	})
}

func TestPublicTransportRequiresSecureBoundaries(t *testing.T) {
	t.Run("plaintext TCP listener is loopback only", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr:  "127.0.0.1:0",
			WSAddr:  "0.0.0.0:0",
			CertDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("public plaintext WebSocket and HTTP listener accepted")
		}
	})

	t.Run("explicit private network permits a TLS-edge listener", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr:         "127.0.0.1:0",
			WSAddr:         "0.0.0.0:0",
			CertDir:        t.TempDir(),
			PrivateNetwork: true,
		})
		if err != nil {
			t.Fatalf("private-network WebSocket and HTTP listener: %v", err)
		}
	})

	t.Run("pprof cannot ride a public listener", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr:         "127.0.0.1:0",
			WSAddr:         "0.0.0.0:0",
			CertDir:        t.TempDir(),
			EnablePprof:    true,
			PrivateNetwork: true,
		})
		if err == nil {
			t.Fatal("public pprof listener accepted")
		}
	})

	t.Run("public WebTransport refuses development certificates", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr:  "0.0.0.0:0",
			WSAddr:  "127.0.0.1:0",
			CertDir: t.TempDir(),
		})
		if err == nil {
			t.Fatal("public WebTransport accepted a managed development certificate")
		}
	})

	t.Run("public WebTransport refuses self-asserted identity", func(t *testing.T) {
		certificate, err := LoadCertificate("", "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		server, err := NewServer(Config{
			WTAddr:             "0.0.0.0:0",
			WSAddr:             "127.0.0.1:0",
			CertFile:           certificate.CertPath,
			KeyFile:            certificate.KeyPath,
			WebTransportPublic: true,
			AllowedOrigins:     []string{"https://play.example.test"},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.Hub.SetHelloAuth(func([]byte) (AdmissionIdentity, error) {
			return AdmissionIdentity{AccountID: "test-account", ShardID: "global-official"}, nil
		})
		if err := server.Start(); err == nil {
			t.Fatal("public WebTransport started without EnterWorld authentication")
		}
	})
}

func TestWebTransportPublicSwitch(t *testing.T) {
	certificate, err := LoadCertificate("", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origins := []string{"https://play.example.test"}

	t.Run("public bind without the switch is refused", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr: "0.0.0.0:0", WSAddr: "127.0.0.1:0",
			CertFile: certificate.CertPath, KeyFile: certificate.KeyPath,
			AllowedOrigins: origins,
		})
		if err == nil {
			t.Fatal("public WebTransport bind accepted without the public switch")
		}
	})

	t.Run("the switch refuses the managed development certificate", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr: "127.0.0.1:0", WSAddr: "127.0.0.1:0", CertDir: t.TempDir(),
			WebTransportPublic: true,
			AllowedOrigins:     origins,
		})
		if err == nil {
			t.Fatal("public WebTransport accepted the managed development certificate")
		}
	})

	t.Run("the switch refuses an empty origin list", func(t *testing.T) {
		_, err := NewServer(Config{
			WTAddr: "127.0.0.1:0", WSAddr: "127.0.0.1:0",
			CertFile: certificate.CertPath, KeyFile: certificate.KeyPath,
			WebTransportPublic: true,
		})
		if err == nil {
			t.Fatal("public WebTransport accepted the loopback-only origin posture")
		}
	})

	t.Run("the switch admits a public bind with the full posture", func(t *testing.T) {
		if _, err := NewServer(Config{
			WTAddr: "0.0.0.0:0", WSAddr: "127.0.0.1:0",
			CertFile: certificate.CertPath, KeyFile: certificate.KeyPath,
			WebTransportPublic: true,
			AllowedOrigins:     origins,
		}); err != nil {
			t.Fatalf("public WebTransport posture refused: %v", err)
		}
	})
}

func TestHTTPServersCarryResourceLimits(t *testing.T) {
	server, err := NewServer(Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.httpServer.ReadHeaderTimeout <= 0 ||
		server.httpServer.ReadTimeout <= 0 ||
		server.httpServer.WriteTimeout <= 0 ||
		server.httpServer.IdleTimeout <= 0 ||
		server.httpServer.MaxHeaderBytes <= 0 {
		t.Fatalf("TCP HTTP limits incomplete: %+v", server.httpServer)
	}
	if server.wtServer.H3.MaxHeaderBytes <= 0 || server.wtServer.H3.IdleTimeout <= 0 {
		t.Fatalf("HTTP/3 limits incomplete: %+v", server.wtServer.H3)
	}
	quic := server.wtServer.H3.QUICConfig
	if quic == nil ||
		quic.HandshakeIdleTimeout <= 0 ||
		quic.MaxIdleTimeout <= 0 ||
		quic.MaxStreamReceiveWindow <= 0 ||
		quic.MaxConnectionReceiveWindow <= 0 ||
		quic.MaxIncomingStreams <= 0 ||
		quic.MaxIncomingUniStreams <= 0 {
		t.Fatalf("QUIC limits incomplete: %+v", quic)
	}
}
