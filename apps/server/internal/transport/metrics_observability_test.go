package transport

import (
	"testing"
)

func TestTransportMetricsExposeSessionQueueAndByteGauges(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })

	if err := session.Send(0x3126, []byte{0xAA, 0xBB}); err != nil {
		t.Fatal(err)
	}
	metrics := hub.Metrics()
	if metrics.DetachedSessions != 1 || metrics.AttachedSessions != 0 {
		t.Fatalf("session gauges before attach = attached %d detached %d, want 0/1", metrics.AttachedSessions, metrics.DetachedSessions)
	}
	if metrics.OutboundQueueDepth != 1 || metrics.OutboundQueueHighWater < 1 {
		t.Fatalf("queue gauges before attach = depth %d high-water %d, want 1/>=1", metrics.OutboundQueueDepth, metrics.OutboundQueueHighWater)
	}

	connection := newFakeConn(false)
	if err := session.attach(connection, false, 0); err != nil {
		t.Fatal(err)
	}
	written := waitWritten(t, connection, 2) // WELCOME, then the queued frame.
	wantBytes := uint64(written[0].EncodedLen() + written[1].EncodedLen())
	waitUntil(t, "outbound frame accounting", func() bool {
		return hub.Metrics().FramesOut == 2
	})
	metrics = hub.Metrics()
	if metrics.AttachedSessions != 1 || metrics.DetachedSessions != 0 {
		t.Fatalf("session gauges after attach = attached %d detached %d, want 1/0", metrics.AttachedSessions, metrics.DetachedSessions)
	}
	if metrics.FramesOut != 2 || metrics.BytesOut != wantBytes {
		t.Fatalf("outbound accounting = frames %d bytes %d, want 2/%d", metrics.FramesOut, metrics.BytesOut, wantBytes)
	}
	if metrics.OutboundQueueDepth != 0 {
		t.Fatalf("queue depth after drain = %d, want 0", metrics.OutboundQueueDepth)
	}
}

func TestTransportMetricsAccountResumeAndDetach(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	first := newFakeConn(false)
	first.kind = "websocket"
	first.inbound <- Frame{
		Opcode:  OpHello,
		Payload: EncodeHello(Hello{AdmissionToken: testHelloAdmissionToken}),
	}
	hub.AcceptConn(first)
	sessions := hub.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("fresh sessions = %d, want 1", len(sessions))
	}
	session := sessions[0]
	t.Cleanup(func() { hub.closeSession(session, nil) })

	_ = first.Close("test disconnect")
	waitUntil(t, "session detach", func() bool {
		return hub.Metrics().DetachedSessions == 1
	})

	second := newFakeConn(false)
	second.kind = "websocket"
	second.inbound <- Frame{
		Opcode: OpHello,
		Payload: EncodeHello(Hello{
			ResumeToken:    session.resumeToken[:],
			AdmissionToken: testHelloAdmissionToken,
			HasPosition:    true,
		}),
	}
	hub.AcceptConn(second)

	metrics := hub.Metrics()
	if metrics.ResumeAttempted != 1 || metrics.ResumeAccepted != 1 || metrics.ResumeFresh != 0 {
		t.Fatalf("resume metrics = attempted %d accepted %d fresh %d, want 1/1/0", metrics.ResumeAttempted, metrics.ResumeAccepted, metrics.ResumeFresh)
	}
	if metrics.Detaches != 1 || metrics.DetachesOther != 1 || metrics.DetachesIdle != 0 {
		t.Fatalf("detach metrics = total %d other %d idle %d, want 1/1/0", metrics.Detaches, metrics.DetachesOther, metrics.DetachesIdle)
	}
	if metrics.HandshakeCount != 2 {
		t.Fatalf("handshake count = %d, want 2", metrics.HandshakeCount)
	}
	if metrics.AttachedSessions != 1 || metrics.DetachedSessions != 0 {
		t.Fatalf("post-resume gauges = attached %d detached %d, want 1/0", metrics.AttachedSessions, metrics.DetachedSessions)
	}
}

func TestTransportMetricsAccountUnknownResumeAsFresh(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	connection := newFakeConn(false)
	connection.kind = "websocket"
	token := make([]byte, ResumeTokenLen)
	for index := range token {
		token[index] = byte(index + 1)
	}
	connection.inbound <- Frame{
		Opcode: OpHello,
		Payload: EncodeHello(Hello{
			ResumeToken:    token,
			AdmissionToken: testHelloAdmissionToken,
		}),
	}
	hub.AcceptConn(connection)
	for _, session := range hub.Sessions() {
		defer hub.closeSession(session, nil)
	}

	metrics := hub.Metrics()
	if metrics.ResumeAttempted != 1 || metrics.ResumeAccepted != 0 || metrics.ResumeFresh != 1 {
		t.Fatalf("resume metrics = attempted %d accepted %d fresh %d, want 1/0/1", metrics.ResumeAttempted, metrics.ResumeAccepted, metrics.ResumeFresh)
	}
	if metrics.HandshakeCount != 1 {
		t.Fatalf("handshake count = %d, want 1", metrics.HandshakeCount)
	}
}

/*
================
TestTransportMetricsCountRefusedResumes

A resume HELLO with no stream position, and one whose position the ring
cannot serve, each end the old session and start a fresh one (#1466); each
is counted under its own reason (#1519).
================
*/
func TestTransportMetricsCountRefusedResumes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hasPosition bool
		lastApplied uint32
	}{
		{"no position", false, 0},
		{"beyond what was written", true, 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := newHub(testCfg())
			allowTestHelloAdmission(hub)
			first := newFakeConn(false)
			first.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{AdmissionToken: testHelloAdmissionToken})}
			hub.AcceptConn(first)
			session := hub.Sessions()[0]
			_ = first.Close("test disconnect")
			waitUntil(t, "session detach", func() bool { return hub.Metrics().DetachedSessions == 1 })

			second := newFakeConn(false)
			second.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{
				ResumeToken:    session.resumeToken[:],
				AdmissionToken: testHelloAdmissionToken,
				HasPosition:    tc.hasPosition,
				LastApplied:    tc.lastApplied,
			})}
			hub.AcceptConn(second)
			for _, s := range hub.Sessions() {
				defer hub.closeSession(s, nil)
			}
			metrics := hub.Metrics()
			wantNoPosition, wantGap := uint64(1), uint64(0)
			if tc.hasPosition {
				wantNoPosition, wantGap = 0, 1
			}
			if metrics.ResumeNoPosition != wantNoPosition || metrics.ResumeGap != wantGap || metrics.ResumeFresh != 1 {
				t.Fatalf("no-position %d gap %d fresh %d, want %d/%d/1",
					metrics.ResumeNoPosition, metrics.ResumeGap, metrics.ResumeFresh, wantNoPosition, wantGap)
			}
		})
	}
}
