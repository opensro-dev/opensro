/*
===========================================================================

conn_ws_close_test.go - a parked WebSocket write must not hold up Close

Issue #1453: a resume's attach() closes the zombie connection synchronously,
and wsConn.Close used to queue behind the session write loop's in-flight
WriteFrame on the shared write mutex, putting the new connection's WELCOME
behind whatever remained of that write's 10 s deadline (~8 s observed).
Here the peer never reads (net.Pipe, unbuffered), so the first write parks
until its own deadline; Close must still complete almost immediately.

===========================================================================
*/
package transport

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"opensro.online/server/internal/testsupport/wait"
)

// pipeWSConn builds a wsConn over an unbuffered net.Pipe: the caller owns
// the peer end and simply never reads it, so any write parks. The client
// handshake is answered by hand: gorilla's Dial writes an HTTP request
// and waits for a 101 response, which would deadlock the unbuffered pipe.
func pipeWSConn(t *testing.T) (*wsConn, net.Conn) {
	t.Helper()
	serverEnd, peerEnd := net.Pipe()
	dialed := make(chan *websocket.Conn, 1)
	go func() {
		dialer := websocket.Dialer{
			NetDial: func(network, addr string) (net.Conn, error) { return serverEnd, nil },
		}
		gc, _, err := dialer.Dial((&url.URL{Scheme: "ws", Host: "pipe"}).String(), nil)
		if err != nil {
			dialed <- nil
			return
		}
		dialed <- gc
	}()
	// Read the upgrade request up to the blank line, then answer 101.
	request := make([]byte, 0, 512)
	buf := make([]byte, 1)
	for !bytes.HasSuffix(request, []byte("\r\n\r\n")) {
		if _, err := peerEnd.Read(buf); err != nil {
			t.Fatalf("reading upgrade request: %v", err)
		}
		request = append(request, buf[0])
	}
	keyLine := []byte("Sec-WebSocket-Key: ")
	start := bytes.Index(request, keyLine)
	if start < 0 {
		t.Fatalf("upgrade request lacks a Sec-WebSocket-Key:\n%s", request)
	}
	key := string(request[start+len(keyLine) : start+len(keyLine)+24])
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n"
	if _, err := peerEnd.Write([]byte(response)); err != nil {
		t.Fatalf("writing upgrade response: %v", err)
	}
	gc := <-dialed
	if gc == nil {
		t.Fatal("pipe websocket dial failed")
	}
	t.Cleanup(func() { _ = peerEnd.Close() })
	return newWSConn(gc), peerEnd
}

/*
================
TestWSCloseDoesNotWaitBehindParkedWrite
================
*/
func TestWSCloseDoesNotWaitBehindParkedWrite(t *testing.T) {
	conn, _ := pipeWSConn(t)

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- conn.WriteFrame(Frame{Opcode: OpPing, Payload: []byte{1}})
	}()
	// Wait until the write is parked inside the socket (it holds writeMu).
	wait.Eventually(t, 3*time.Second, "the write to park", func() bool {
		if conn.writeMu.TryLock() {
			conn.writeMu.Unlock()
			return false
		}
		return true
	})

	closeDone := make(chan error, 1)
	go func() { closeDone <- conn.Close("test close") }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close errored: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited behind a parked write (up to the 10 s write deadline)")
	}

	// The parked write must have been aborted, not left to its deadline.
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("parked write succeeded against a closed connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parked write still blocked after Close")
	}
}

/*
================
TestWSWriteAfterCloseRefuses
================
*/
func TestWSWriteAfterCloseRefuses(t *testing.T) {
	conn, _ := pipeWSConn(t)
	if err := conn.Close("test close"); err != nil {
		t.Fatalf("Close errored: %v", err)
	}
	if err := conn.WriteFrame(Frame{Opcode: OpPing, Payload: []byte{1}}); err == nil {
		t.Fatal("write after Close succeeded")
	}
}
