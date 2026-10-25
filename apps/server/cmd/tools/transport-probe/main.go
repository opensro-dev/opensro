/*
===========================================================================

main.go - transport-probe: WebSocket-vs-WebTransport measurement rig (#1450)

This tool is the evidence rig for the transport-default question. It stands
up the production transport server (internal/transport, the same Hub the
game serves on) with a fixture admission verifier and one echo handler,
then drives it over WebSocket and over WebTransport from a client that
speaks the same hello/welcome handshake the browser client speaks.

What it measures per transport, one JSON object per line on stdout:

  handshake  - dial + hello + WELCOME, repeated (cold cost per connect)
  echo       - sequential game-frame echo RTT (0x706D -> 0xB06D, the same
               fixture opcodes the transport smoke tests use)
  datagram   - sequential echo over the unreliable lane
               (OpObjectSourceMove -> OpObjectSourceCorrection; on a
               WebSocket link the server falls back to ordered delivery,
               which is exactly what a WS client experiences)
  stream     - pipelined echo at a fixed 20 Hz send schedule; reports
               response inter-arrival, not request RTT
  burst      - server fan-out drain: one request, 200 replies written
               back to back (the world-entry spawn shape); reports
               per-reply completion time from the request

Send rates stay at or under the production inbound frame budget
(25/s sustained, burst 75): the echo scenarios are paced at the 20 Hz
cadence of the game's own movement traffic, because an unpaced loopback
echo measures the rate limiter, not the transport.

The probe client answers server keepalive pings the way the browser
client does, and every wait carries a deadline: a lost echo is counted
in the row's "dropped" field, never a hang.

Modes:

  run     - in-process fixture server on ephemeral ports, both transports
  serve   - fixture server on -ws/-wt until interrupted (for probing from
            another host; pair with -certs pointing at provisioned certs)
  measure - probe an already-running fixture server (-addr selects the
            transport's address; -transport picks the lane)

Impairment tiers (#1450): tier 1 is clean loopback (this tool as-is).
Tier 2 (added delay) and tier 3 (packet loss) belong to the Linux host
and tc netem; they are run by serving this binary on the Linux box and
measuring from a second host - never improvised on the shared dev box.

The fixture admission verifier accepts only tickets with the
probeTicketPrefix and maps each ticket to its own account, so probe
connections never trip the production per-account session limit. The
WebTransport client skips certificate verification because the fixture
server mints a throwaway self-signed pair; that skip exists only in this
tool and must never be copied into a shipping client.

===========================================================================
*/

package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/quic-go/webtransport-go"

	"opensro.online/server/internal/transport"
)

//============================================================================
// Constants
//============================================================================

const (
	probeTicketPrefix = "transport-probe-ticket"
	probeTicket       = probeTicketPrefix + "-0"

	// The transport smoke tests' fixture echo pair (smoke_test.go),
	// plus a fan-out request the probe server answers with burstRounds
	// echo responses.
	echoReqOp  uint16 = 0x706D
	echoRespOp uint16 = 0xB06D
	burstReqOp uint16 = 0x706E

	handshakeRounds  = 20
	echoRounds       = 200
	echoInterval     = 50 * time.Millisecond // 20 Hz, inside the 25/s inbound budget
	streamRateHz     = 20
	streamSeconds    = 10
	burstRounds      = 200
	echoWaitDeadline = 5 * time.Second
	dialTimeout      = 10 * time.Second
)

//============================================================================
// Sample statistics
//============================================================================

type sample struct {
	values  []float64
	dropped int
}

/*
================
add
================
*/
func (s *sample) add(d time.Duration) {
	s.values = append(s.values, float64(d)/float64(time.Millisecond))
}

/*
================
report

Percentiles over the recorded values; jitter is the mean absolute delta
between successive values, the shape a player feels as unevenness.
================
*/
func (s *sample) report() (p50, p95, p99, max, jitter float64) {
	if len(s.values) == 0 {
		return 0, 0, 0, 0, 0
	}
	sorted := append([]float64(nil), s.values...)
	sort.Float64s(sorted)
	at := func(q float64) float64 {
		i := int(math.Ceil(q*float64(len(sorted)))) - 1
		if i < 0 {
			i = 0
		}
		return sorted[i]
	}
	for i := 1; i < len(s.values); i++ {
		jitter += math.Abs(s.values[i] - s.values[i-1])
	}
	if len(s.values) > 1 {
		jitter /= float64(len(s.values) - 1)
	}
	return at(0.50), at(0.95), at(0.99), sorted[len(sorted)-1], jitter
}

//============================================================================
// Result rows
//============================================================================

type row struct {
	Scenario  string  `json:"scenario"`
	Transport string  `json:"transport"`
	Samples   int     `json:"samples"`
	Dropped   int     `json:"dropped"`
	P50Ms     float64 `json:"p50Ms"`
	P95Ms     float64 `json:"p95Ms"`
	P99Ms     float64 `json:"p99Ms"`
	MaxMs     float64 `json:"maxMs"`
	JitterMs  float64 `json:"jitterMs"`
}

/*
================
emit
================
*/
func emit(scenario, transportName string, s *sample) {
	p50, p95, p99, max, jitter := s.report()
	_ = json.NewEncoder(os.Stdout).Encode(row{
		Scenario:  scenario,
		Transport: transportName,
		Samples:   len(s.values),
		Dropped:   s.dropped,
		P50Ms:     p50,
		P95Ms:     p95,
		P99Ms:     p99,
		MaxMs:     max,
		JitterMs:  jitter,
	})
}

//============================================================================
// Probe client links
//
// One reader goroutine per lane feeds a single frame channel. Server
// keepalive pings are answered in the reader, mirroring the browser
// client, so scenario code only ever sees frames it asked about.
//============================================================================

type probeLink interface {
	Send(op uint16, body []byte) error
	SendDatagram(op uint16, body []byte) error
	Recv() <-chan transport.Frame
	Close()
}

//----------------------------------------------------------------------------
// WebSocket link
//----------------------------------------------------------------------------

type wsLink struct {
	conn   *websocket.Conn
	frames chan transport.Frame
	done   chan struct{}
}

/*
================
dialWS
================
*/
func dialWS(ctx context.Context, addr string) (*wsLink, error) {
	d := websocket.Dialer{HandshakeTimeout: dialTimeout}
	c, _, err := d.DialContext(ctx, "ws://"+addr+transport.PathWS, nil)
	if err != nil {
		return nil, err
	}
	l := &wsLink{conn: c, frames: make(chan transport.Frame, 512), done: make(chan struct{})}
	go l.read()
	return l, nil
}

/*
================
wsLink.read
================
*/
func (l *wsLink) read() {
	defer close(l.frames)
	for {
		_, raw, err := l.conn.ReadMessage()
		if err != nil {
			return
		}
		f, err := transport.DecodeFrame(raw)
		if err != nil {
			continue
		}
		if f.Opcode == transport.OpPing {
			_ = l.Send(transport.OpPong, f.Payload)
			continue
		}
		select {
		case l.frames <- f:
		case <-l.done:
			return
		}
	}
}

// Send writes one reliable frame.
func (l *wsLink) Send(op uint16, body []byte) error {
	return l.conn.WriteMessage(websocket.BinaryMessage,
		transport.Frame{Opcode: op, Payload: body}.Encode())
}

// SendDatagram on a WebSocket link is the same ordered write; the
// datagram scenario then measures the server's ordered fallback.
func (l *wsLink) SendDatagram(op uint16, body []byte) error {
	return l.Send(op, body)
}

// Recv returns the incoming frame channel.
func (l *wsLink) Recv() <-chan transport.Frame { return l.frames }

// Close ends the link.
func (l *wsLink) Close() {
	close(l.done)
	_ = l.conn.Close()
}

//----------------------------------------------------------------------------
// WebTransport link
//----------------------------------------------------------------------------

type wtLink struct {
	session *webtransport.Session
	stream  *webtransport.Stream
	frames  chan transport.Frame
	done    chan struct{}
}

/*
================
dialWT
================
*/
func dialWT(ctx context.Context, addr string) (*wtLink, error) {
	d := webtransport.Transport{
		// Fixture servers mint self-signed certs; see the file banner.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // probe fixture only
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	_, session, err := d.Dial(dctx, "https://"+addr+transport.PathWT, http.Header{})
	if err != nil {
		return nil, err
	}
	stream, err := session.OpenStreamSync(dctx)
	if err != nil {
		_ = session.CloseWithError(0, "probe: open stream failed")
		return nil, err
	}
	l := &wtLink{session: session, stream: stream, frames: make(chan transport.Frame, 512), done: make(chan struct{})}
	go l.readStream()
	go l.readDatagrams()
	return l, nil
}

/*
================
wtLink.readStream
================
*/
func (l *wtLink) readStream() {
	for {
		f, err := transport.ReadStreamFrame(l.stream)
		if err != nil {
			return
		}
		if f.Opcode == transport.OpPing {
			_ = l.Send(transport.OpPong, f.Payload)
			continue
		}
		select {
		case l.frames <- f:
		case <-l.done:
			return
		default:
			// A full channel means the scenario stopped reading; the
			// frame is stale by the time anyone looks, so drop it.
		}
	}
}

/*
================
wtLink.readDatagrams
================
*/
func (l *wtLink) readDatagrams() {
	for {
		raw, err := l.session.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		f, err := transport.DecodeFrame(raw)
		if err != nil {
			continue
		}
		select {
		case l.frames <- f:
		case <-l.done:
			return
		default:
		}
	}
}

// Send writes one reliable stream frame.
func (l *wtLink) Send(op uint16, body []byte) error {
	return transport.WriteStreamFrame(l.stream, transport.Frame{Opcode: op, Payload: body})
}

// SendDatagram writes one unreliable datagram.
func (l *wtLink) SendDatagram(op uint16, body []byte) error {
	return l.session.SendDatagram(transport.Frame{Opcode: op, Payload: body}.Encode())
}

// Recv returns the incoming frame channel.
func (l *wtLink) Recv() <-chan transport.Frame { return l.frames }

// Close ends the link.
func (l *wtLink) Close() {
	close(l.done)
	_ = l.session.CloseWithError(0, "probe done")
}

//============================================================================
// Handshake
//============================================================================

/*
================
dial

Connects one link and completes hello/WELCOME. The returned duration is
connect-through-WELCOME, the cold cost a player pays per (re)connect.
================
*/
func dial(ctx context.Context, kind, addr, ticket string) (probeLink, time.Duration, error) {
	start := time.Now()
	var (
		link probeLink
		err  error
	)
	switch kind {
	case "ws":
		link, err = dialWS(ctx, addr)
	case "wt":
		link, err = dialWT(ctx, addr)
	default:
		return nil, 0, fmt.Errorf("unknown transport %q", kind)
	}
	if err != nil {
		return nil, 0, err
	}
	hello := transport.EncodeHello(transport.Hello{AdmissionToken: []byte(ticket)})
	if err := link.Send(transport.OpHello, hello); err != nil {
		link.Close()
		return nil, 0, err
	}
	for {
		select {
		case f, ok := <-link.Recv():
			if !ok {
				link.Close()
				return nil, 0, fmt.Errorf("link closed before WELCOME")
			}
			switch f.Opcode {
			case transport.OpWelcome:
				return link, time.Since(start), nil
			case transport.OpBye:
				link.Close()
				return nil, 0, fmt.Errorf("server said goodbye during hello (reason %d)", byeReason(f))
			}
		case <-time.After(dialTimeout):
			link.Close()
			return nil, 0, fmt.Errorf("no WELCOME within %s", dialTimeout)
		}
	}
}

/*
================
byeReason
================
*/
func byeReason(f transport.Frame) int {
	if len(f.Payload) == 0 {
		return -1
	}
	return int(f.Payload[0])
}

//============================================================================
// Echo plumbing
//============================================================================

/*
================
echoBody

The echo payload is a sequence number plus the send timestamp; the
server returns it verbatim, so an echo is matched and timed on arrival
with no client-side bookkeeping that could drift.
================
*/
func echoBody(seq uint32) []byte {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body, seq)
	binary.LittleEndian.PutUint64(body[4:], uint64(time.Now().UnixNano()))
	return body
}

/*
================
waitEcho

Waits for the echo of one sequence number until the deadline. Frames
for other sequences are stale (their wait already expired) and dropped.
================
*/
func waitEcho(link probeLink, wantOp uint16, seq uint32) (time.Duration, bool) {
	deadline := time.Now().Add(echoWaitDeadline)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return 0, false
		}
		select {
		case f, ok := <-link.Recv():
			if !ok {
				return 0, false
			}
			if f.Opcode != wantOp || len(f.Payload) != 12 {
				continue
			}
			if binary.LittleEndian.Uint32(f.Payload) != seq {
				continue
			}
			sent := int64(binary.LittleEndian.Uint64(f.Payload[4:]))
			return time.Since(time.Unix(0, sent)), true
		case <-time.After(remain):
			return 0, false
		}
	}
}

//============================================================================
// Scenarios
//============================================================================

/*
================
scenarioHandshake
================
*/
func scenarioHandshake(ctx context.Context, kind, addr string) error {
	s := &sample{}
	for i := 0; i < handshakeRounds; i++ {
		ticket := fmt.Sprintf("%s-%d", probeTicketPrefix, i)
		link, d, err := dial(ctx, kind, addr, ticket)
		if err != nil {
			return fmt.Errorf("handshake %d: %w", i, err)
		}
		s.add(d)
		link.Close()
	}
	emit("handshake", kind, s)
	return nil
}

/*
================
scenarioEchoSeq

Sequential echo over one lane: send, wait, record. The datagram variant
differs only in which send/receive opcodes carry the body.
================
*/
func scenarioEchoSeq(ctx context.Context, kind, addr, name string, datagram bool) error {
	link, _, err := dial(ctx, kind, addr, probeTicket)
	if err != nil {
		return err
	}
	defer link.Close()
	send := link.Send
	wantOp := uint16(echoRespOp)
	if datagram {
		send = link.SendDatagram
		wantOp = transport.OpObjectSourceCorrection
	}
	reqOp := uint16(echoReqOp)
	if datagram {
		reqOp = transport.OpObjectSourceMove
	}
	s := &sample{}
	for i := 0; i < echoRounds; i++ {
		paceStart := time.Now()
		seq := uint32(i + 1)
		if err := send(reqOp, echoBody(seq)); err != nil {
			return fmt.Errorf("%s send %d: %w", name, seq, err)
		}
		if d, ok := waitEcho(link, wantOp, seq); ok {
			s.add(d)
		} else {
			s.dropped++
		}
		// Pace to echoInterval from the send instant: the sample stays a
		// true round trip, the send rate stays a game's, not a flood's.
		if remain := time.Until(paceStart.Add(echoInterval)); remain > 0 {
			time.Sleep(remain)
		}
	}
	emit(name, kind, s)
	return nil
}

/*
================
scenarioStream

Pipelined sends on a fixed 20 Hz schedule for streamSeconds while a
reader records response arrivals; the sample is inter-arrival time, the
smoothness a moving player sees, not request RTT.
================
*/
func scenarioStream(ctx context.Context, kind, addr string) error {
	link, _, err := dial(ctx, kind, addr, probeTicket)
	if err != nil {
		return err
	}
	defer link.Close()

	total := streamRateHz * streamSeconds
	arrivals := make(chan time.Time, total*2)
	readerStop := make(chan struct{})
	go func() {
		seen := make(map[uint32]bool, total)
		for {
			select {
			case f, ok := <-link.Recv():
				if !ok {
					return
				}
				if f.Opcode != echoRespOp || len(f.Payload) != 12 {
					continue
				}
				seq := binary.LittleEndian.Uint32(f.Payload)
				if seq >= 1 && seq <= uint32(total) && !seen[seq] {
					seen[seq] = true
					arrivals <- time.Now()
				}
			case <-readerStop:
				return
			}
		}
	}()

	ticker := time.NewTicker(time.Second / streamRateHz)
	defer ticker.Stop()
	sendDeadline := time.Now().Add(time.Duration(streamSeconds) * time.Second)
	seq := uint32(0)
sendLoop:
	for {
		select {
		case <-ticker.C:
			seq++
			if err := link.Send(echoReqOp, echoBody(seq)); err != nil {
				break sendLoop
			}
			if seq >= uint32(total) || time.Now().After(sendDeadline) {
				break sendLoop
			}
		case <-ctx.Done():
			break sendLoop
		}
	}

	s := &sample{}
	received := 0
	var last time.Time
	wait := time.NewTimer(echoWaitDeadline)
collect:
	for received < int(seq) {
		select {
		case at := <-arrivals:
			if received > 0 {
				s.add(at.Sub(last))
			}
			last = at
			received++
			if !wait.Stop() {
				<-wait.C
			}
			wait.Reset(echoWaitDeadline)
		case <-wait.C:
			break collect
		}
	}
	close(readerStop)
	s.dropped = int(seq) - received
	emit("stream", kind, s)
	return nil
}

/*
================
scenarioBurst

One burstReqOp asks the fixture server to write burstRounds echo
responses back to back - the world-entry shape, hundreds of spawn
frames at once. The sample is each reply's completion time from the
request, so p99/max read as drain time. A single inbound frame keeps
the scenario inside the rate budget by construction; what drains is
the server's outbound queue over this transport.
================
*/
func scenarioBurst(ctx context.Context, kind, addr string) error {
	link, _, err := dial(ctx, kind, addr, probeTicket)
	if err != nil {
		return err
	}
	defer link.Close()
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, burstRounds)
	start := time.Now()
	if err := link.Send(burstReqOp, body); err != nil {
		return fmt.Errorf("burst request: %w", err)
	}
	s := &sample{}
	seen := make(map[uint32]bool, burstRounds)
	received := 0
	gap := time.NewTimer(echoWaitDeadline)
	defer gap.Stop()
	for received < burstRounds {
		select {
		case f, ok := <-link.Recv():
			if !ok {
				s.dropped = burstRounds - received
				emit("burst", kind, s)
				return nil
			}
			if f.Opcode != echoRespOp || len(f.Payload) != 12 {
				continue
			}
			seq := binary.LittleEndian.Uint32(f.Payload)
			if seq >= 1 && seq <= burstRounds && !seen[seq] {
				seen[seq] = true
				received++
				s.add(time.Since(start))
				if !gap.Stop() {
					<-gap.C
				}
				gap.Reset(echoWaitDeadline)
			}
		case <-gap.C:
			s.dropped = burstRounds - received
			emit("burst", kind, s)
			return nil
		case <-ctx.Done():
			s.dropped = burstRounds - received
			emit("burst", kind, s)
			return nil
		}
	}
	emit("burst", kind, s)
	return nil
}

/*
================
runScenarios
================
*/
func runScenarios(ctx context.Context, kind, addr string) error {
	if err := scenarioHandshake(ctx, kind, addr); err != nil {
		return err
	}
	if err := scenarioEchoSeq(ctx, kind, addr, "echo", false); err != nil {
		return err
	}
	if err := scenarioEchoSeq(ctx, kind, addr, "datagram", true); err != nil {
		return err
	}
	if err := scenarioStream(ctx, kind, addr); err != nil {
		return err
	}
	return scenarioBurst(ctx, kind, addr)
}

//============================================================================
// Fixture server
//============================================================================

/*
================
newProbeServer

The production transport server with three fixture attachments: the
hello verifier (probe tickets only, one account per ticket), an
enter-world stub (any authenticated session may enter; the probe never
sends game input, so nothing downstream of enter-world is reached), and
the echo handlers the scenarios measure.
================
*/
func newProbeServer(certDir, wtAddr, wsAddr string) (*transport.Server, error) {
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
		ticket := string(tok)
		if !strings.HasPrefix(ticket, probeTicketPrefix) {
			return transport.AdmissionIdentity{}, fmt.Errorf("not a probe ticket")
		}
		return transport.AdmissionIdentity{AccountID: ticket, ShardID: "global-official"}, nil
	})
	srv.Hub.SetEnterWorldAuth(func(_ *transport.Session, ew transport.EnterWorld) (bool, uint32) {
		return len(ew.AuthToken) > 0, 0xA1
	})
	srv.Hub.Handle(echoReqOp, func(s *transport.Session, _ uint16, payload []byte) {
		_ = s.Send(echoRespOp, payload)
	})
	srv.Hub.Handle(burstReqOp, func(s *transport.Session, _ uint16, payload []byte) {
		if len(payload) != 4 {
			return
		}
		count := binary.LittleEndian.Uint32(payload)
		if count > 1000 {
			count = 1000
		}
		for i := uint32(1); i <= count; i++ {
			if err := s.Send(echoRespOp, echoBody(i)); err != nil {
				return
			}
		}
	})
	srv.Hub.Handle(transport.OpObjectSourceMove, func(s *transport.Session, _ uint16, payload []byte) {
		_ = s.SendUnreliable(transport.OpObjectSourceCorrection, payload)
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
runServe
================
*/
func runServe(certDir, wtAddr, wsAddr string) error {
	srv, err := newProbeServer(certDir, wtAddr, wsAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "transport-probe: serving ws=%s wt=%s\n", srv.WSAddr(), srv.WTAddr())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

/*
================
runBoth

In-process server on ephemeral ports, then both transports in turn.
================
*/
func runBoth(ctx context.Context, certDir string) error {
	srv, err := newProbeServer(certDir, "127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := runScenarios(ctx, "ws", srv.WSAddr()); err != nil {
		return fmt.Errorf("ws: %w", err)
	}
	if err := runScenarios(ctx, "wt", srv.WTAddr()); err != nil {
		return fmt.Errorf("wt: %w", err)
	}
	return nil
}

/*
================
main
================
*/
func main() {
	mode := flag.String("mode", "run", "run | serve | measure")
	kind := flag.String("transport", "ws", "ws | wt (measure mode)")
	addr := flag.String("addr", "", "server address for measure mode (host:port of the chosen transport)")
	wtAddr := flag.String("wt", "127.0.0.1:18088", "WebTransport listen address (serve mode)")
	wsAddr := flag.String("ws", "127.0.0.1:18087", "WebSocket listen address (serve mode)")
	certDir := flag.String("certs", "", "certificate directory (default: a throwaway temp dir)")
	flag.Parse()

	if *certDir == "" {
		tmp, err := os.MkdirTemp("", "transport-probe-certs-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "transport-probe:", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmp)
		*certDir = tmp
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch *mode {
	case "serve":
		err = runServe(*certDir, *wtAddr, *wsAddr)
	case "measure":
		if *addr == "" {
			err = fmt.Errorf("-addr is required in measure mode")
		} else {
			err = runScenarios(ctx, *kind, *addr)
		}
	case "run":
		err = runBoth(ctx, *certDir)
	default:
		err = fmt.Errorf("unknown -mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "transport-probe:", err)
		os.Exit(1)
	}
}
