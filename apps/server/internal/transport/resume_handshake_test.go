/*
===========================================================================

resume_handshake_test.go - a resume's WELCOME is bounded even while the
zombie connection's write loop is parked on the dead peer

Issue #1453: on two production reconnects the WELCOME arrived about 8 s
after the HELLO. The only phase in AcceptConn that can eat seconds is the
zombie replacement: the session's write loop may be parked inside the old
connection's WriteFrame (the peer vanished without closing), and attaching
the new connection must not inherit that wait: closing the old connection
aborts the parked write instead of queueing behind it. This test parks a
real WebSocket write on an unread net.Pipe, resumes over a second
connection, and requires the WELCOME well inside the write's 10 s
deadline.

===========================================================================
*/
package transport

import (
	"net"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// peerHello writes one unmasked binary WebSocket message (the framing the
// client-role pipe end expects from its peer) carrying frame.
func peerHello(t *testing.T, peer net.Conn, frame Frame) {
	t.Helper()
	payload := frame.Encode()
	if len(payload) >= 126 {
		t.Fatalf("test frame too large for the one-byte length form: %d", len(payload))
	}
	wire := append([]byte{0x82, byte(len(payload))}, payload...)
	if _, err := peer.Write(wire); err != nil {
		t.Fatalf("writing HELLO to pipe: %v", err)
	}
}

/*
================
TestResumeWelcomeNotBlockedByParkedOldWrite
================
*/
func TestResumeWelcomeNotBlockedByParkedOldWrite(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)

	oldConn, peer := pipeWSConn(t)
	peerHello(t, peer, Frame{
		Opcode:  OpHello,
		Payload: EncodeHello(Hello{AdmissionToken: testHelloAdmissionToken}),
	})
	hub.AcceptConn(oldConn)

	sessions := hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	sess := sessions[0]
	t.Cleanup(func() { hub.closeSession(sess, nil) })

	// The pipe is never read, so the write loop's first write (the WELCOME)
	// parks inside WriteFrame until its deadline: exactly a session still
	// attached to a peer that vanished mid-stream. writeMu held = the
	// write loop is inside the socket write.
	wait.Eventually(t, 3*time.Second, "the old connection's write to park", func() bool {
		if oldConn.writeMu.TryLock() {
			oldConn.writeMu.Unlock()
			return false
		}
		return true
	})

	resume := helloConn(sess.resumeToken[:], testHelloAdmissionToken)
	attached := make(chan struct{})
	go func() {
		hub.AcceptConn(resume)
		close(attached)
	}()

	// The WELCOME must reach the new connection almost immediately, not
	// after the parked write's 10 s deadline.
	wait.Eventually(t, 2*time.Second, "the resume WELCOME", func() bool {
		return len(resume.written()) > 0
	})
	frames := resume.written()
	if frames[0].Opcode != OpWelcome {
		t.Fatalf("first frame = 0x%04X, want WELCOME", frames[0].Opcode)
	}
	welcome, err := DecodeWelcome(frames[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !welcome.Resumed {
		t.Fatal("WELCOME does not mark the session resumed")
	}
	select {
	case <-attached:
	case <-time.After(2 * time.Second):
		t.Fatal("resume AcceptConn did not return")
	}
}
