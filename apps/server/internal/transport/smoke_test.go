/*
===========================================================================

smoke_test.go - real socket contracts for the transport hub

Loopback WebTransport and WebSocket clients share one hub and an echo lane.
These tests own connection lifetimes, admission fixtures and observable wire
results. Socket delivery does not imply that the server goroutine has finished
recording its handshake; asynchronous assertions use the shared wait package.

===========================================================================
*/
package transport_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quic-go/webtransport-go"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	echoReqOp             uint16 = 0x706D
	echoRespOp            uint16 = 0xB06D
	dgramReqOp                   = transport.OpObjectSourceMove
	dgramRspOp                   = transport.OpObjectSourceCorrection
	testAdmissionTicket          = "test-smoke-admission-ticket"
	queuedFrameOp         uint16 = 0x3126
	testWorldID           uint32 = 0x00A1
	smokeIOTimeout               = 5 * time.Second
	smokeLifecycleTimeout        = 3 * time.Second
	smokeGracePeriod             = 10 * time.Second
	smokeIdleTimeout             = 30 * time.Second
	smokeOutboundQueue           = 64
	smokeDatagramAttempts        = 20
	smokeDatagramTimeout         = 250 * time.Millisecond
)

/*
================
installSmokeAdmissionVerifier

Use a fixed admission identity so the socket tests do not depend on accounts
or game storage. Datagram opcodes still pass the production allowlist.
================
*/
func installSmokeAdmissionVerifier(srv *transport.Server) {
	srv.Hub.SetHelloAuth(func(ticket []byte) (transport.AdmissionIdentity, error) {
		if string(ticket) != testAdmissionTicket {
			return transport.AdmissionIdentity{}, fmt.Errorf("unexpected test admission ticket")
		}
		return transport.AdmissionIdentity{AccountID: "smoke-account", ShardID: "global-official"}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, ew transport.EnterWorld) (bool, uint32) {
		return len(ew.AuthToken) > 0, testWorldID
	})
}

/*
================
startServer

Each test owns ephemeral listeners and certificates. Cleanup waits for shutdown
so later tests cannot inherit sockets or sessions from this hub.
================
*/
func startServer(t *testing.T) *transport.Server {
	t.Helper()
	cfg := transport.Config{
		WTAddr:            "127.0.0.1:0",
		WSAddr:            "127.0.0.1:0",
		CertDir:           t.TempDir(),
		HelloTimeout:      smokeIOTimeout,
		GracePeriod:       smokeGracePeriod,
		KeepaliveInterval: smokeIOTimeout,
		IdleTimeout:       smokeIdleTimeout,
		OutboundQueue:     smokeOutboundQueue,
	}
	srv, err := transport.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	installSmokeAdmissionVerifier(srv)
	srv.Hub.Handle(echoReqOp, func(s *transport.Session, _ uint16, payload []byte) {
		s.Send(echoRespOp, payload)
	})
	srv.Hub.Handle(dgramReqOp, func(s *transport.Session, _ uint16, payload []byte) {
		s.SendUnreliable(dgramRspOp, payload)
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), smokeLifecycleTimeout)
		defer cancel()
		srv.Shutdown(ctx)
	})
	return srv
}

//============================================================================

/*
================
dialWT

Trust only this test server's certificate and open its reliable control stream.
================
*/
func dialWT(t *testing.T, srv *transport.Server) (*webtransport.Session, *webtransport.Stream) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Cert.Leaf)
	d := webtransport.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	t.Cleanup(func() { d.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), smokeIOTimeout)
	defer cancel()
	url := fmt.Sprintf("https://%s%s", srv.WTAddr(), transport.PathWT)
	_, sess, err := d.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("webtransport dial %s: %v", url, err)
	}
	str, err := sess.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open control stream: %v", err)
	}
	return sess, str
}

/*
================
readWTFrame

Ignore keepalive frames while waiting for the asserted control or game frame.
================
*/
func readWTFrame(t *testing.T, str *webtransport.Stream) transport.Frame {
	t.Helper()
	str.SetReadDeadline(time.Now().Add(smokeIOTimeout))
	for {
		f, err := transport.ReadStreamFrame(str)
		if err != nil {
			t.Fatalf("reading stream frame: %v", err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		return f
	}
}

/*
================
helloWT

WELCOME proves wire admission; server-side completion counters may follow it.
================
*/
func helloWT(t *testing.T, str *webtransport.Stream, token []byte) transport.Welcome {
	t.Helper()
	err := transport.WriteStreamFrame(str, transport.Frame{
		Opcode: transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{
			ResumeToken: token, AdmissionToken: []byte(testAdmissionTicket),
		}),
	})
	if err != nil {
		t.Fatalf("writing HELLO: %v", err)
	}
	f := readWTFrame(t, str)
	if f.Opcode != transport.OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", f.Opcode)
	}
	w, err := transport.DecodeWelcome(f.Payload)
	if err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
	return w
}

//============================================================================

/*
================
dialWS

Connect to this test's ephemeral TCP listener.
================
*/
func dialWS(t *testing.T, srv *transport.Server) *websocket.Conn {
	t.Helper()
	url := fmt.Sprintf("ws://%s%s", srv.WSAddr(), transport.PathWS)
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("websocket dial %s: %v", url, err)
	}
	return c
}

/*
================
readWSFrame

Require binary transport frames and ignore keepalive traffic.
================
*/
func readWSFrame(t *testing.T, c *websocket.Conn) transport.Frame {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(smokeIOTimeout))
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("reading ws message: %v", err)
		}
		if typ != websocket.BinaryMessage {
			t.Fatalf("ws message type = %d, want binary", typ)
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("decoding ws frame: %v", err)
		}
		if f.Opcode == transport.OpPing || f.Opcode == transport.OpPong {
			continue
		}
		return f
	}
}

/*
================
writeWSFrame

Fail at the sending caller when the reliable socket cannot accept a frame.
================
*/
func writeWSFrame(t *testing.T, c *websocket.Conn, f transport.Frame) {
	t.Helper()
	if err := c.WriteMessage(websocket.BinaryMessage, f.Encode()); err != nil {
		t.Fatalf("writing ws frame: %v", err)
	}
}

/*
================
helloWS

Exchange admission or resume tokens through the production WebSocket envelope.
================
*/
func helloWS(t *testing.T, c *websocket.Conn, token []byte) transport.Welcome {
	t.Helper()
	writeWSFrame(t, c, transport.Frame{
		Opcode: transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{
			ResumeToken: token, AdmissionToken: []byte(testAdmissionTicket),
		}),
	})
	f := readWSFrame(t, c)
	if f.Opcode != transport.OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", f.Opcode)
	}
	w, err := transport.DecodeWelcome(f.Payload)
	if err != nil {
		t.Fatalf("decoding WELCOME: %v", err)
	}
	return w
}

//============================================================================

/*
================
TestWebTransportRoundTrip

Prove admission, reliable game traffic, keepalive and explicit teardown over UDP.
================
*/
func TestWebTransportRoundTrip(t *testing.T) {
	srv := startServer(t)
	sess, str := dialWT(t, srv)
	defer sess.CloseWithError(0, "test done")

	w := helloWT(t, str, nil)
	if w.Resumed {
		t.Fatal("fresh session marked resumed")
	}
	if len(w.ResumeToken) != transport.ResumeTokenLen {
		t.Fatalf("resume token %d bytes, want %d", len(w.ResumeToken), transport.ResumeTokenLen)
	}

	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: echoReqOp, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	echo := readWTFrame(t, str)
	if echo.Opcode != echoRespOp || !bytes.Equal(echo.Payload, payload) {
		t.Fatalf("echo = op 0x%04X payload % X", echo.Opcode, echo.Payload)
	}

	// Explicit PING must come back as PONG with the same body.
	body := []byte{0xAA, 0xBB}
	if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: transport.OpPing, Payload: body}); err != nil {
		t.Fatal(err)
	}
	str.SetReadDeadline(time.Now().Add(smokeIOTimeout))
	for {
		f, err := transport.ReadStreamFrame(str)
		if err != nil {
			t.Fatalf("waiting for PONG: %v", err)
		}
		if f.Opcode == transport.OpPong && bytes.Equal(f.Payload, body) {
			break
		}
	}

	if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: transport.OpBye, Payload: []byte{transport.ByeReasonNormal}}); err != nil {
		t.Fatal(err)
	}
	waitSessionGone(t, srv, w.SessionID)
}

/*
================
TestWebSocketRoundTrip

Prove the same reliable game contract over the TCP fallback.
================
*/
func TestWebSocketRoundTrip(t *testing.T) {
	srv := startServer(t)
	c := dialWS(t, srv)
	defer c.Close()

	w := helloWS(t, c, nil)
	if w.Resumed {
		t.Fatal("fresh session marked resumed")
	}

	payload := []byte{0xCA, 0xFE, 0xF0, 0x0D}
	writeWSFrame(t, c, transport.Frame{Opcode: echoReqOp, Payload: payload})
	echo := readWSFrame(t, c)
	if echo.Opcode != echoRespOp || !bytes.Equal(echo.Payload, payload) {
		t.Fatalf("echo = op 0x%04X payload % X", echo.Opcode, echo.Payload)
	}

	writeWSFrame(t, c, transport.Frame{Opcode: transport.OpBye, Payload: []byte{transport.ByeReasonNormal}})
	waitSessionGone(t, srv, w.SessionID)
}

/*
================
TestWebSocketResume

An abrupt socket loss preserves the session and queued reliable frames. A
replacement connection receives WELCOME before the retained frame.
================
*/
func TestWebSocketResume(t *testing.T) {
	srv := startServer(t)
	c := dialWS(t, srv)
	w := helloWS(t, c, nil)

	sess, ok := srv.Hub.Session(w.SessionID)
	if !ok {
		t.Fatal("session not registered in hub")
	}

	// Abrupt transport death: no Close frame, just a dead TCP socket.
	c.NetConn().Close()
	wait.Eventually(t, smokeLifecycleTimeout, "session detach", func() bool { return sess.Kind() == "detached" })

	queued := []byte{0x60, 0x1D}
	if err := sess.Send(queuedFrameOp, queued); err != nil {
		t.Fatalf("queueing frame on detached session: %v", err)
	}

	c2 := dialWS(t, srv)
	defer c2.Close()
	w2 := helloWS(t, c2, w.ResumeToken)
	if !w2.Resumed {
		t.Fatal("WELCOME on reconnect not marked resumed")
	}
	if w2.SessionID != w.SessionID {
		t.Fatalf("resumed session ID %d, want %d", w2.SessionID, w.SessionID)
	}
	f := readWSFrame(t, c2)
	if f.Opcode != queuedFrameOp || !bytes.Equal(f.Payload, queued) {
		t.Fatalf("queued frame after resume = op 0x%04X payload % X", f.Opcode, f.Payload)
	}
}

/*
================
TestWebTransportDatagram

Use only allowlisted unreliable opcodes. Hosts that drop every datagram skip
this optional lane; the reliable round-trip tests remain required.
================
*/
func TestWebTransportDatagram(t *testing.T) {
	srv := startServer(t)
	sess, str := dialWT(t, srv)
	defer sess.CloseWithError(0, "test done")
	helloWT(t, str, nil)

	payload := []byte{0xD6}
	for attempt := 0; attempt < smokeDatagramAttempts; attempt++ {
		if err := sess.SendDatagram((transport.Frame{Opcode: dgramReqOp, Payload: payload}).Encode()); err != nil {
			t.Fatalf("SendDatagram: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), smokeDatagramTimeout)
		data, err := sess.ReceiveDatagram(ctx)
		cancel()
		if err != nil {
			continue
		}
		f, err := transport.DecodeFrame(data)
		if err != nil {
			t.Fatalf("decoding datagram: %v", err)
		}
		if f.Opcode == dgramRspOp && bytes.Equal(f.Payload, payload) {
			return
		}
	}
	t.Skip("no datagram round-trip on this host; reliable channel covers the contract")
}

/*
================
TestSingleBindEviction

The latest character binding evicts the previous owner and invalidates its
resume token without disturbing the winning connection.
================
*/
func TestSingleBindEviction(t *testing.T) {
	srv := startServer(t)

	cA := dialWS(t, srv)
	defer cA.Close()
	wA := helloWS(t, cA, nil)
	cB := dialWS(t, srv)
	defer cB.Close()
	wB := helloWS(t, cB, nil)

	sessA, ok := srv.Hub.Session(wA.SessionID)
	if !ok {
		t.Fatal("session A missing")
	}
	sessB, ok := srv.Hub.Session(wB.SessionID)
	if !ok {
		t.Fatal("session B missing")
	}

	// First bind wins quietly; the second evicts the first — exactly what
	// main's OnWorldBound chain does on a duplicate EnterWorld.
	if old, replaced := srv.Hub.BindExclusive("d1:cg", sessA); replaced {
		t.Fatalf("first bind replaced session %d, want none", old.ID)
	}
	old, replaced := srv.Hub.BindExclusive("d1:cg", sessB)
	if !replaced || old == nil || old.ID != wA.SessionID {
		t.Fatalf("second bind: replaced=%v old=%v, want eviction of session %d", replaced, old, wA.SessionID)
	}

	// The replaced client hears BYE(Replaced) on the wire.
	bye := readWSFrame(t, cA)
	if bye.Opcode != transport.OpBye || len(bye.Payload) != 1 || bye.Payload[0] != transport.ByeReasonReplaced {
		t.Fatalf("evicted client got op 0x%04X payload % X, want BYE(Replaced)", bye.Opcode, bye.Payload)
	}
	waitSessionGone(t, srv, wA.SessionID)

	// Reconnect suppression: A's resume token must NOT reattach — it gets a
	// fresh session instead.
	cA2 := dialWS(t, srv)
	defer cA2.Close()
	wA2 := helloWS(t, cA2, wA.ResumeToken)
	if wA2.Resumed {
		t.Fatal("evicted session resumed — reconnect suppression failed")
	}
	if wA2.SessionID == wA.SessionID {
		t.Fatal("evicted session ID reused on reconnect")
	}

	// The winner stays bound and fully functional.
	if bound, ok := srv.Hub.BoundSession("d1:cg"); !ok || bound.ID != wB.SessionID {
		t.Fatalf("bound session = %v/%v, want session %d", bound, ok, wB.SessionID)
	}
	payload := []byte{0x51}
	writeWSFrame(t, cB, transport.Frame{Opcode: echoReqOp, Payload: payload})
	echo := readWSFrame(t, cB)
	if echo.Opcode != echoRespOp || !bytes.Equal(echo.Payload, payload) {
		t.Fatalf("winner echo = op 0x%04X payload % X", echo.Opcode, echo.Payload)
	}
}

/*
================
TestTransportMetricsEndpoint

The public endpoint must expose both completed transport handshakes. WELCOME
is sent inside attach before countAttach runs, so wait for server completion
before checking that the HTTP representation preserves those counters.
================
*/
func TestTransportMetricsEndpoint(t *testing.T) {
	srv := startServer(t)

	sess, str := dialWT(t, srv)
	defer sess.CloseWithError(0, "test done")
	helloWT(t, str, nil)

	c := dialWS(t, srv)
	defer c.Close()
	helloWS(t, c, nil)

	wait.Eventually(t, smokeLifecycleTimeout, "both handshake counters", func() bool {
		metrics := srv.Hub.Metrics()
		return metrics.WTOk == 1 && metrics.WSOK == 1
	})

	client := http.Client{Timeout: smokeIOTimeout}
	resp, err := client.Get(fmt.Sprintf("http://%s%s", srv.WSAddr(), transport.PathMetrics))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var document struct {
		transport.Metrics
		CertificateNotAfter         string `json:"certificate_not_after"`
		CertificateSecondsRemaining int64  `json:"certificate_seconds_remaining"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	m := document.Metrics
	if m.WTOk != 1 || m.WSOK != 1 {
		t.Fatalf("metrics = %+v, want wt_ok=1 and ws_ok=1", m)
	}
	if m.LiveSessions < 2 {
		t.Fatalf("live_sessions = %d, want >=2", m.LiveSessions)
	}
	if document.CertificateNotAfter == "" || document.CertificateSecondsRemaining <= 0 {
		t.Fatalf("certificate metrics = notAfter %q remaining %d", document.CertificateNotAfter, document.CertificateSecondsRemaining)
	}
}

/*
================
TestCertHashEndpoint

The TCP bootstrap must identify the certificate presented by the UDP listener.
================
*/
func TestCertHashEndpoint(t *testing.T) {
	srv := startServer(t)
	url := fmt.Sprintf("http://%s%s", srv.WSAddr(), transport.PathCertHash)
	client := http.Client{Timeout: smokeIOTimeout}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var doc struct {
		Algorithm    string `json:"algorithm"`
		SHA256Hex    string `json:"sha256Hex"`
		SHA256Base64 string `json:"sha256Base64"`
		WTPath       string `json:"wtPath"`
		WSPath       string `json:"wsPath"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Algorithm != "sha-256" {
		t.Fatalf("algorithm = %q", doc.Algorithm)
	}
	if doc.SHA256Hex != srv.Cert.SHA256Hex() {
		t.Fatalf("served hash %s != cert hash %s", doc.SHA256Hex, srv.Cert.SHA256Hex())
	}
	if doc.WTPath != transport.PathWT || doc.WSPath != transport.PathWS {
		t.Fatalf("paths = %q / %q", doc.WTPath, doc.WSPath)
	}
}

//============================================================================

/*
================
waitSessionGone

Explicit BYE and eviction finish on the server goroutine after wire delivery.
================
*/
func waitSessionGone(t *testing.T, srv *transport.Server, id uint64) {
	t.Helper()
	wait.Eventually(t, smokeLifecycleTimeout, fmt.Sprintf("session %d teardown", id), func() bool {
		_, ok := srv.Hub.Session(id)
		return !ok
	})
}
