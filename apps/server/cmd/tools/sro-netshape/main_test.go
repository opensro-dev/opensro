/*
===========================================================================

main_test.go - the relay legs against loopback echo servers

Integration: a real datagram and a real byte stream cross each leg, so
the tests pin forwarding, the applied delay, the UDP drop and the TCP
loss hold (never a byte drop) end to end. Delays are short and margins
generous; the draw-level exactness lives in shape_test.go.

===========================================================================
*/
package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

/*
================
TestValidateFlags
================
*/
func TestValidateFlags(t *testing.T) {
	udpOnly := flagValues{udpListen: "127.0.0.1:0", udpTarget: "127.0.0.1:8788"}
	for _, tc := range []struct {
		name   string
		values flagValues
		ok     bool
	}{
		{"udp only", udpOnly, true},
		{"loss above 100", flagValues{udpListen: "127.0.0.1:0", udpTarget: "127.0.0.1:8788", upLoss: 101}, false},
		{"negative loss", flagValues{udpListen: "127.0.0.1:0", udpTarget: "127.0.0.1:8788", downLoss: -1}, false},
		{"negative delay", flagValues{udpListen: "127.0.0.1:0", udpTarget: "127.0.0.1:8788", upDelay: -time.Millisecond}, false},
		{"tcp listen without target", flagValues{tcpListen: "127.0.0.1:0"}, false},
		{"udp target without listen", flagValues{udpTarget: "127.0.0.1:8788"}, false},
		{"no legs", flagValues{}, false},
	} {
		err := validateFlags(tc.values)
		if tc.ok && err != nil {
			t.Fatalf("%s: rejected: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%s: accepted, want rejection", tc.name)
		}
	}
	if _, err := parseFlags([]string{"-udp-listen", "127.0.0.1:0", "-udp-target", "127.0.0.1:8788", "-up-loss", "101"}); err == nil {
		t.Fatal("parseFlags accepted a loss above 100")
	}
}

/*
================
startUDPEcho
================
*/
func startUDPEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buffer := make([]byte, udpBufferSize)
		for {
			n, client, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if _, err := conn.WriteToUDP(buffer[:n], client); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { conn.Close() })
	return conn.LocalAddr().(*net.UDPAddr)
}

/*
================
TestUDPRelayEchoThroughDelay

A datagram crosses the leg twice behind 25ms each way: the echo returns
whole, and measurably later than an unshaped round trip.
================
*/
func TestUDPRelayEchoThroughDelay(t *testing.T) {
	echo := startUDPEcho(t)
	cfg := shapeConfig{Delay: 25 * time.Millisecond}
	leg, err := startUDPLeg("127.0.0.1:0", echo.String(), newShaper(cfg, 3), newShaper(cfg, 4))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(leg.close)
	client, err := net.DialUDP("udp", nil, leg.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	start := time.Now()
	if _, err := client.Write([]byte("shape-ping")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, udpBufferSize)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := client.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buffer[:n]); got != "shape-ping" {
		t.Fatalf("echo %q, want shape-ping", got)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("echo after %v, want the two 25ms delays to show", elapsed)
	}
}

/*
================
TestUDPRelayDropsEverythingAtFullLoss

At 100% up loss the echo server is never reached: the read times out
and the leg's dropped counter says the datagram was spent here.
================
*/
func TestUDPRelayDropsEverythingAtFullLoss(t *testing.T) {
	echo := startUDPEcho(t)
	leg, err := startUDPLeg("127.0.0.1:0", echo.String(),
		newShaper(shapeConfig{LossPct: 100}, 5), newShaper(shapeConfig{}, 6))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(leg.close)
	client, err := net.DialUDP("udp", nil, leg.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("shape-ping")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, udpBufferSize)
	client.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	if _, err := client.Read(buffer); err == nil {
		t.Fatal("an echo returned through 100% loss")
	}
	deadline := time.Now().Add(2 * time.Second)
	for leg.upStats.dropped.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("dropped counter %d, want 1", leg.upStats.dropped.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if leg.upStats.forwarded.Load() != 0 {
		t.Fatalf("forwarded %d datagrams at 100%% loss", leg.upStats.forwarded.Load())
	}
}

/*
================
startTCPEcho
================
*/
func startTCPEcho(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return listener.Addr().String()
}

/*
================
TestTCPRelayEchoPreservesStream

Chunks cross the leg behind a small delay and come back byte for byte:
the pump never drops, splits or reorders stream bytes.
================
*/
func TestTCPRelayEchoPreservesStream(t *testing.T) {
	echo := startTCPEcho(t)
	cfg := shapeConfig{Delay: 5 * time.Millisecond}
	leg, err := startTCPLeg("127.0.0.1:0", echo, newShaper(cfg, 7), newShaper(cfg, 8))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(leg.close)
	client, err := net.Dial("tcp", leg.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	want := [][]byte{[]byte("alpha-"), {0, 1, 2, 250, 251}, []byte("omega")}
	total := 0
	for _, chunk := range want {
		if _, err := client.Write(chunk); err != nil {
			t.Fatal(err)
		}
		total += len(chunk)
	}
	got := make([]byte, total)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.Join(want, nil)) {
		t.Fatalf("stream came back %v, want %v", got, bytes.Join(want, nil))
	}
}

/*
================
TestTCPRelayLossHoldsInsteadOfDropping

At 100% up loss the stream still delivers every byte - each chunk is
held for the loss penalty (150ms here), never dropped.
================
*/
func TestTCPRelayLossHoldsInsteadOfDropping(t *testing.T) {
	echo := startTCPEcho(t)
	cfg := shapeConfig{LossPct: 100, LossHold: 150 * time.Millisecond}
	leg, err := startTCPLeg("127.0.0.1:0", echo, newShaper(cfg, 9), newShaper(shapeConfig{}, 10))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(leg.close)
	client, err := net.Dial("tcp", leg.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	start := time.Now()
	if _, err := client.Write([]byte("held-not-dropped")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("held-not-dropped"))
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "held-not-dropped" {
		t.Fatalf("echo %q, want held-not-dropped", got)
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Fatalf("echo after %v, want the 150ms loss hold to show", elapsed)
	}
	if leg.upStats.held.Load() == 0 {
		t.Fatal("the held counter never moved at 100% loss")
	}
}
