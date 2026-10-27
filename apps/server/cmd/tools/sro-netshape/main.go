/*
===========================================================================

sro-netshape - a local loss/latency shaper for transport measurement

A small relay that sits between a client (or probe) and a GameWorld and
impairs the traffic per direction, so WebSocket-versus-WebTransport
measurement (#1450) can run under loss on machines with no netem. Two
independent legs, each optional: a TCP relay for the WebSocket path and
a UDP relay for the QUIC/WebTransport path. Per direction (up is client
to server, down is server to client): a one-way delay, a uniform jitter
around it, and a random loss percentage, all reproducible with -seed.

Loss is a true drop only on UDP. On TCP a dropped byte would corrupt the
stream, so a failed loss draw holds the chunk for -tcp-loss-delay (a
retransmission-style penalty) instead; the hold blocks everything
behind it in that direction, which is the head-of-line stall the
measurement wants. Real TCP loss is still only a Linux netem run.

Usage:

	sro-netshape -udp-listen 127.0.0.1:18788 -udp-target 127.0.0.1:8788 \
		-tcp-listen 127.0.0.1:18789 -tcp-target 127.0.0.1:8789 \
		-up-delay 30ms -up-jitter 10ms -up-loss 3 -down-delay 30ms

Exit status: 0 on interrupt, 1 on a bind/dial failure, 2 on usage.

===========================================================================
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// udpBufferSize is the largest datagram the relay carries; a QUIC packet
// on loopback stays far below it.
const udpBufferSize = 65535

// udpSessionIdle retires a client session whose both directions have
// been silent this long.
const udpSessionIdle = 2 * time.Minute

/*
================
flagValues

The command line, before validation (buildConfig).
================
*/
type flagValues struct {
	tcpListen   string
	tcpTarget   string
	udpListen   string
	udpTarget   string
	upDelay     time.Duration
	upJitter    time.Duration
	downDelay   time.Duration
	downJitter  time.Duration
	upLoss      float64
	downLoss    float64
	tcpLossHold time.Duration
	seed        int64
	statsEvery  time.Duration
}

/*
================
main
================
*/
func main() {
	values, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sro-netshape:", err)
		os.Exit(2)
	}
	up, down := newShapers(values)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var tcp *tcpLeg
	if values.tcpListen != "" {
		tcp, err = startTCPLeg(values.tcpListen, values.tcpTarget, up, down)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sro-netshape: tcp leg:", err)
			os.Exit(1)
		}
		fmt.Printf("netshape: tcp %s -> %s (up %s, down %s)\n",
			tcp.listener.Addr(), values.tcpTarget, describe(values, true), describe(values, false))
	}
	var udp *udpLeg
	if values.udpListen != "" {
		udp, err = startUDPLeg(values.udpListen, values.udpTarget, up, down)
		if err != nil {
			fmt.Fprintln(os.Stderr, "sro-netshape: udp leg:", err)
			os.Exit(1)
		}
		fmt.Printf("netshape: udp %s -> %s (up %s, down %s)\n",
			udp.conn.LocalAddr(), values.udpTarget, describe(values, true), describe(values, false))
	}
	if values.statsEvery > 0 {
		go printStatsLoop(ctx, values.statsEvery, tcp, udp)
	}
	<-ctx.Done()
	if tcp != nil {
		tcp.close()
	}
	if udp != nil {
		udp.close()
	}
	printStats(os.Stdout, tcp, udp)
}

/*
================
parseFlags
================
*/
func parseFlags(args []string) (flagValues, error) {
	var values flagValues
	set := flag.NewFlagSet("sro-netshape", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&values.tcpListen, "tcp-listen", "", "TCP (WebSocket) listen address, e.g. 127.0.0.1:18789")
	set.StringVar(&values.tcpTarget, "tcp-target", "", "TCP upstream address the relay dials")
	set.StringVar(&values.udpListen, "udp-listen", "", "UDP (WebTransport) listen address, e.g. 127.0.0.1:18788")
	set.StringVar(&values.udpTarget, "udp-target", "", "UDP upstream address, e.g. 127.0.0.1:8788")
	set.DurationVar(&values.upDelay, "up-delay", 0, "client->server one-way delay")
	set.DurationVar(&values.upJitter, "up-jitter", 0, "client->server jitter, applied as +/- this duration")
	set.DurationVar(&values.downDelay, "down-delay", 0, "server->client one-way delay")
	set.DurationVar(&values.downJitter, "down-jitter", 0, "server->client jitter, applied as +/- this duration")
	set.Float64Var(&values.upLoss, "up-loss", 0, "client->server loss percent, 0..100 (UDP drops; TCP holds)")
	set.Float64Var(&values.downLoss, "down-loss", 0, "server->client loss percent, 0..100 (UDP drops; TCP holds)")
	set.DurationVar(&values.tcpLossHold, "tcp-loss-delay", 200*time.Millisecond,
		"extra hold a TCP chunk takes when its loss draw fails (a proxy cannot drop stream bytes)")
	set.Int64Var(&values.seed, "seed", 0, "draw seed; 0 picks one from the clock, nonzero reproduces a run")
	set.DurationVar(&values.statsEvery, "stats", 0, "print the counters this often; 0 prints only at exit")
	if err := set.Parse(args); err != nil {
		return flagValues{}, err
	}
	if err := validateFlags(values); err != nil {
		return flagValues{}, err
	}
	return values, nil
}

/*
================
validateFlags
================
*/
func validateFlags(values flagValues) error {
	for name, loss := range map[string]float64{"up-loss": values.upLoss, "down-loss": values.downLoss} {
		if loss < 0 || loss > 100 {
			return fmt.Errorf("-%s %v is outside 0..100", name, loss)
		}
	}
	durations := map[string]time.Duration{
		"up-delay": values.upDelay, "up-jitter": values.upJitter,
		"down-delay": values.downDelay, "down-jitter": values.downJitter,
		"tcp-loss-delay": values.tcpLossHold,
	}
	for name, duration := range durations {
		if duration < 0 {
			return fmt.Errorf("-%s %v is negative", name, duration)
		}
	}
	if (values.tcpListen == "") != (values.tcpTarget == "") {
		return fmt.Errorf("-tcp-listen and -tcp-target must be given together")
	}
	if (values.udpListen == "") != (values.udpTarget == "") {
		return fmt.Errorf("-udp-listen and -udp-target must be given together")
	}
	if values.tcpListen == "" && values.udpListen == "" {
		return fmt.Errorf("nothing to relay: give -tcp-listen/-tcp-target and/or -udp-listen/-udp-target")
	}
	return nil
}

/*
================
newShapers

The two directions' shapers. A nonzero seed derives both directions
deterministically; zero seeds from the clock (delay draws shared by no
two runs matters less than the loss draws, which the seed fixes).
================
*/
func newShapers(values flagValues) (up, down *shaper) {
	seed := uint64(values.seed)
	if values.seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}
	upCfg := shapeConfig{Delay: values.upDelay, Jitter: values.upJitter, LossPct: values.upLoss, LossHold: values.tcpLossHold}
	downCfg := shapeConfig{Delay: values.downDelay, Jitter: values.downJitter, LossPct: values.downLoss, LossHold: values.tcpLossHold}
	return newShaper(upCfg, seed), newShaper(downCfg, seed^0x5deece66d)
}

/*
================
describe

One direction's configured impairment, for the startup line.
================
*/
func describe(values flagValues, up bool) string {
	if up {
		return fmt.Sprintf("delay=%s jitter=%s loss=%v%%", values.upDelay, values.upJitter, values.upLoss)
	}
	return fmt.Sprintf("delay=%s jitter=%s loss=%v%%", values.downDelay, values.downJitter, values.downLoss)
}

/*
================
printStats
================
*/
func printStats(out io.Writer, tcp *tcpLeg, udp *udpLeg) {
	if tcp != nil {
		fmt.Fprintf(out, "netshape tcp up:   %s\n", statsLine(&tcp.upStats))
		fmt.Fprintf(out, "netshape tcp down: %s\n", statsLine(&tcp.downStats))
	}
	if udp != nil {
		fmt.Fprintf(out, "netshape udp up:   %s\n", statsLine(&udp.upStats))
		fmt.Fprintf(out, "netshape udp down: %s\n", statsLine(&udp.downStats))
	}
}

/*
================
printStatsLoop
================
*/
func printStatsLoop(ctx context.Context, every time.Duration, tcp *tcpLeg, udp *udpLeg) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			printStats(os.Stdout, tcp, udp)
		case <-ctx.Done():
			return
		}
	}
}

/*
================
statsLine
================
*/
func statsLine(stats *directionStats) string {
	return fmt.Sprintf("received=%d forwarded=%d dropped=%d held=%d bytes=%d",
		stats.received.Load(), stats.forwarded.Load(), stats.dropped.Load(), stats.held.Load(), stats.bytes.Load())
}

//============================================================================
// TCP leg
//============================================================================

/*
================
tcpLeg

The WebSocket path's relay: each accepted connection dials the target
and pumps both directions. A pump sleeps in line before writing, so a
delayed chunk blocks the ones behind it - the head-of-line behaviour
being measured - and byte order can never break.
================
*/
type tcpLeg struct {
	listener  net.Listener
	target    string
	up        *shaper
	down      *shaper
	upStats   directionStats
	downStats directionStats
	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
}

/*
================
startTCPLeg
================
*/
func startTCPLeg(listenAddr, target string, up, down *shaper) (*tcpLeg, error) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	leg := &tcpLeg{listener: listener, target: target, up: up, down: down, conns: map[net.Conn]struct{}{}}
	leg.wg.Add(1)
	go leg.serve()
	return leg, nil
}

/*
================
serve
================
*/
func (l *tcpLeg) serve() {
	defer l.wg.Done()
	for {
		client, err := l.listener.Accept()
		if err != nil {
			return
		}
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handle(client)
		}()
	}
}

/*
================
handle

One connection: dial the target, pump both ways, and close both ends
when either pump ends (a measurement proxy does not half-close).
================
*/
func (l *tcpLeg) handle(client net.Conn) {
	server, err := net.Dial("tcp", l.target)
	if err != nil {
		client.Close()
		return
	}
	l.track(client)
	l.track(server)
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			client.Close()
			server.Close()
		})
	}
	var pumps sync.WaitGroup
	pumps.Add(2)
	go func() {
		defer pumps.Done()
		l.pump(server, client, l.up, &l.upStats)
		closeBoth()
	}()
	go func() {
		defer pumps.Done()
		l.pump(client, server, l.down, &l.downStats)
		closeBoth()
	}()
	pumps.Wait()
	l.untrack(client)
	l.untrack(server)
}

/*
================
pump

Moves one direction of one connection: draw, sleep the drawn delay (plus
the loss hold when the loss draw fails), write the chunk whole.
================
*/
func (l *tcpLeg) pump(dst, src net.Conn, sh *shaper, stats *directionStats) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			stats.received.Add(1)
			admitted, delay := sh.pass()
			if !admitted {
				delay += sh.cfg.LossHold
				stats.held.Add(1)
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			if writeAll(dst, buffer[:n]) != nil {
				return
			}
			stats.forwarded.Add(1)
			stats.bytes.Add(uint64(n))
		}
		if err != nil {
			return
		}
	}
}

/*
================
track
================
*/
func (l *tcpLeg) track(conn net.Conn) {
	l.mu.Lock()
	l.conns[conn] = struct{}{}
	l.mu.Unlock()
}

/*
================
untrack
================
*/
func (l *tcpLeg) untrack(conn net.Conn) {
	l.mu.Lock()
	delete(l.conns, conn)
	l.mu.Unlock()
}

/*
================
close
================
*/
func (l *tcpLeg) close() {
	l.listener.Close()
	l.mu.Lock()
	for conn := range l.conns {
		conn.Close()
	}
	l.mu.Unlock()
	l.wg.Wait()
}

/*
================
writeAll
================
*/
func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

//============================================================================
// UDP leg
//============================================================================

/*
================
udpLeg

The WebTransport path's relay. Each client address gets a session: its
own upstream socket to the target, its traffic shaped by the direction
shapers through the delay queues. Sessions idle past udpSessionIdle are
retired by the sweep.
================
*/
type udpLeg struct {
	conn      *net.UDPConn
	target    *net.UDPAddr
	up        *shaper
	down      *shaper
	upStats   directionStats
	downStats directionStats
	upQueue   *delayQueue
	downQueue *delayQueue
	mu        sync.Mutex
	sessions  map[string]*udpSession
	stop      chan struct{}
	wg        sync.WaitGroup
}

/*
================
udpSession
================
*/
type udpSession struct {
	client   *net.UDPAddr
	upstream *net.UDPConn
	lastSeen atomic.Int64
}

/*
================
startUDPLeg
================
*/
func startUDPLeg(listenAddr, targetAddr string, up, down *shaper) (*udpLeg, error) {
	listen, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}
	target, err := net.ResolveUDPAddr("udp", targetAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", listen)
	if err != nil {
		return nil, err
	}
	leg := &udpLeg{
		conn: conn, target: target, up: up, down: down,
		upQueue: newDelayQueue(), downQueue: newDelayQueue(),
		sessions: map[string]*udpSession{},
		stop:     make(chan struct{}),
	}
	leg.wg.Add(4)
	go func() { defer leg.wg.Done(); leg.upQueue.run() }()
	go func() { defer leg.wg.Done(); leg.downQueue.run() }()
	go func() { defer leg.wg.Done(); leg.serve() }()
	go func() { defer leg.wg.Done(); leg.sweep() }()
	return leg, nil
}

/*
================
serve

Reads client datagrams and queues each behind its drawn delay (or drops
it) toward the session's upstream socket.
================
*/
func (l *udpLeg) serve() {
	buffer := make([]byte, udpBufferSize)
	for {
		n, client, err := l.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		session, err := l.sessionFor(client)
		if err != nil {
			continue
		}
		data := append([]byte(nil), buffer[:n]...)
		session.lastSeen.Store(time.Now().UnixNano())
		l.upStats.received.Add(1)
		admitted, delay := l.up.pass()
		if !admitted {
			l.upStats.dropped.Add(1)
			continue
		}
		l.upQueue.submit(time.Now().Add(delay), data, func(payload []byte) error {
			_, writeErr := session.upstream.Write(payload)
			if writeErr == nil {
				l.upStats.forwarded.Add(1)
				l.upStats.bytes.Add(uint64(len(payload)))
			}
			return writeErr
		})
	}
}

/*
================
sessionFor

The client's session, created on first sight: a dedicated upstream
socket and its reader goroutine.
================
*/
func (l *udpLeg) sessionFor(client *net.UDPAddr) (*udpSession, error) {
	key := client.String()
	l.mu.Lock()
	defer l.mu.Unlock()
	if session := l.sessions[key]; session != nil {
		return session, nil
	}
	upstream, err := net.DialUDP("udp", nil, l.target)
	if err != nil {
		return nil, err
	}
	session := &udpSession{client: client, upstream: upstream}
	session.lastSeen.Store(time.Now().UnixNano())
	l.sessions[key] = session
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.readSession(session)
	}()
	return session, nil
}

/*
================
readSession

Reads the target's replies for one session and queues each toward the
client behind its drawn down delay (or drops it). Ends when the session
socket closes; the sweep may then forget the session.
================
*/
func (l *udpLeg) readSession(session *udpSession) {
	buffer := make([]byte, udpBufferSize)
	for {
		n, err := session.upstream.Read(buffer)
		if err != nil {
			return
		}
		data := append([]byte(nil), buffer[:n]...)
		session.lastSeen.Store(time.Now().UnixNano())
		l.downStats.received.Add(1)
		admitted, delay := l.down.pass()
		if !admitted {
			l.downStats.dropped.Add(1)
			continue
		}
		l.downQueue.submit(time.Now().Add(delay), data, func(payload []byte) error {
			_, writeErr := l.conn.WriteToUDP(payload, session.client)
			if writeErr == nil {
				l.downStats.forwarded.Add(1)
				l.downStats.bytes.Add(uint64(len(payload)))
			}
			return writeErr
		})
	}
}

/*
================
sweep

Retires sessions idle past udpSessionIdle until the leg closes.
================
*/
func (l *udpLeg) sweep() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-udpSessionIdle).UnixNano()
			l.mu.Lock()
			for key, session := range l.sessions {
				if session.lastSeen.Load() < cutoff {
					session.upstream.Close()
					delete(l.sessions, key)
				}
			}
			l.mu.Unlock()
		case <-l.stop:
			return
		}
	}
}

/*
================
close
================
*/
func (l *udpLeg) close() {
	close(l.stop)
	l.conn.Close()
	l.mu.Lock()
	for key, session := range l.sessions {
		session.upstream.Close()
		delete(l.sessions, key)
	}
	l.mu.Unlock()
	l.upQueue.close()
	l.downQueue.close()
	l.wg.Wait()
}
