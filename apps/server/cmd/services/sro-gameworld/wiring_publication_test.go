/*
===========================================================================

wiring_publication_test.go - production private receipt capture boundaries

Exercise captureCharacterFrames itself with the real Hub and session writer.
The channel connection replaces only socket I/O; scene admission, recipient
selection and reliable enqueue remain production code.

===========================================================================
*/
package main

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

const (
	wiringPublicationTimeout  = 5 * time.Second
	wiringPublicationDivision = "publication-fixture"
	wiringPublicationFence    = 0x7ffd
)

/*
================
wiringPublicationConn
================
*/
type wiringPublicationConn struct {
	in     chan transport.Frame
	out    chan transport.Frame
	closed chan struct{}
	once   sync.Once
}

/*
================
ReadFrame
================
*/
func (c *wiringPublicationConn) ReadFrame(ctx context.Context) (transport.Frame, error) {
	select {
	case frame := <-c.in:
		return frame, nil
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	case <-c.closed:
		return transport.Frame{}, io.EOF
	}
}

/*
================
WriteFrame
================
*/
func (c *wiringPublicationConn) WriteFrame(frame transport.Frame) error {
	frame.Payload = append([]byte(nil), frame.Payload...)
	select {
	case c.out <- frame:
		return nil
	case <-c.closed:
		return io.ErrClosedPipe
	}
}

/*
================
SupportsUnreliable
================
*/
func (*wiringPublicationConn) SupportsUnreliable() bool { return false }

/*
================
WriteUnreliable
================
*/
func (*wiringPublicationConn) WriteUnreliable(transport.Frame) error { return io.ErrClosedPipe }

/*
================
Close
================
*/
func (c *wiringPublicationConn) Close(string) error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

/*
================
Kind
================
*/
func (*wiringPublicationConn) Kind() string { return "websocket" }

/*
================
RemoteAddr
================
*/
func (*wiringPublicationConn) RemoteAddr() string { return "publication-fixture:0" }

/*
================
wiringPublicationThrough

A reliable fence proves absence without sleeps or inspecting session queues.
================
*/
func wiringPublicationThrough(t *testing.T, c *wiringPublicationConn, opcode uint16) []transport.Frame {
	t.Helper()
	timer := time.NewTimer(wiringPublicationTimeout)
	defer timer.Stop()
	var frames []transport.Frame
	for {
		select {
		case frame := <-c.out:
			if frame.Opcode == opcode {
				return frames
			}
			frames = append(frames, frame)
		case <-timer.C:
			t.Fatalf("timed out waiting for reliable fence %#x", opcode)
		}
	}
}

/*
================
wiringPublicationServer
================
*/
func wiringPublicationServer(t *testing.T) *transport.Server {
	t.Helper()
	server, err := transport.NewServer(transport.Config{
		CertDir: t.TempDir(), GracePeriod: time.Minute,
		KeepaliveInterval: time.Hour, IdleTimeout: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Hub.SetHelloAuth(func(token []byte) (transport.AdmissionIdentity, error) {
		return transport.AdmissionIdentity{AccountID: string(token), ShardID: wiringPublicationDivision}, nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), wiringPublicationTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return server
}

/*
================
wiringPublicationSession

Start at the post-entry binding seam. This test targets the production
capture adapter, not bootstrap construction or authentication policy.
================
*/
func wiringPublicationSession(t *testing.T, server *transport.Server, account string) (*transport.Session, *wiringPublicationConn) {
	t.Helper()
	c := &wiringPublicationConn{
		in: make(chan transport.Frame, 1), out: make(chan transport.Frame, 32), closed: make(chan struct{}),
	}
	c.in <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(account)})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Hub.AcceptConn(c)
	}()
	t.Cleanup(func() {
		_ = c.Close("test complete")
		select {
		case <-done:
		case <-time.After(wiringPublicationTimeout):
			t.Error("connection admission did not finish")
		}
	})
	var frame transport.Frame
	select {
	case frame = <-c.out:
	case <-time.After(wiringPublicationTimeout):
		t.Fatal("missing welcome")
	}
	if frame.Opcode != transport.OpWelcome {
		t.Fatalf("first frame=%#x, want welcome", frame.Opcode)
	}
	welcome, err := transport.DecodeWelcome(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := server.Hub.Session(welcome.SessionID)
	if !ok {
		t.Fatal("welcomed session is absent")
	}
	session.BindCharacter(wiringPublicationDivision, "Alice", 1)
	if !session.TryMarkWorldReady() {
		t.Fatal("initial world ready failed")
	}
	return session, c
}

/*
================
wiringPublicationFlush
================
*/
func wiringPublicationFlush(t *testing.T, session *transport.Session, c *wiringPublicationConn) []transport.Frame {
	t.Helper()
	if err := session.Send(wiringPublicationFence, nil); err != nil {
		t.Fatal(err)
	}
	return wiringPublicationThrough(t, c, wiringPublicationFence)
}

/*
================
TestCaptureCharacterFramesDeliversWhileGMSceneLoads
================
*/
func TestCaptureCharacterFramesDeliversWhileGMSceneLoads(t *testing.T) {
	server := wiringPublicationServer(t)
	session, conn := wiringPublicationSession(t, server, "original")
	bootstrap := []byte("replacement bootstrap")
	if err := session.SendSceneReset([]transport.Frame{{Opcode: transport.OpEnterWorldResult, Payload: bootstrap}}); err != nil {
		t.Fatal(err)
	}
	revision, active := session.SceneRevision()
	if active || !session.WorldReady() {
		t.Fatal("GM replacement must be loading while WorldReady remains true")
	}
	publish := captureCharacterFrames(server.Hub, wiringPublicationDivision, "aLiCe")
	if publish == nil {
		t.Fatal("loading scene was excluded from private receipt capture")
	}
	if err := session.SendSceneBatch(revision, []transport.Frame{{Opcode: wire.OpSingleObjectSpawn, Payload: []byte("suppressed visibility")}}); err != nil {
		t.Fatal(err)
	}
	receipt := []byte{1, 15, 49, 0, 0xec, 8}
	publish([]wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: receipt}})
	frames := wiringPublicationFlush(t, session, conn)
	if len(frames) != 2 || frames[0].Opcode != transport.OpEnterWorldResult || !bytes.Equal(frames[0].Payload, bootstrap) || frames[1].Opcode != wire.OpItemUseResponse || !bytes.Equal(frames[1].Payload, receipt) {
		t.Fatalf("loading publication=%v, want bootstrap then private receipt only", frames)
	}
	if current, active := session.SceneRevision(); current != revision || active || !session.WorldReady() {
		t.Fatal("private publication changed scene admission state")
	}
}

/*
================
TestCaptureCharacterFramesRejectsStaleRevisionDuringAndAfterLoading
================
*/
func TestCaptureCharacterFramesRejectsStaleRevisionDuringAndAfterLoading(t *testing.T) {
	server := wiringPublicationServer(t)
	session, conn := wiringPublicationSession(t, server, "original")
	publish := captureCharacterFrames(server.Hub, wiringPublicationDivision, "Alice")
	if publish == nil {
		t.Fatal("active capture missing")
	}
	if err := session.SendSceneReset([]transport.Frame{{Opcode: transport.OpEnterWorldResult}}); err != nil {
		t.Fatal(err)
	}
	publish([]wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: []byte{1}}})
	frames := wiringPublicationFlush(t, session, conn)
	if len(frames) != 1 || frames[0].Opcode != transport.OpEnterWorldResult {
		t.Fatalf("stale receipt reached loading scene: %v", frames)
	}
	if !session.FinishSceneReentry() {
		t.Fatal("replacement scene did not become ready")
	}
	publish([]wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: []byte{2}}})
	if frames := wiringPublicationFlush(t, session, conn); len(frames) != 0 {
		t.Fatalf("ready revived a stale capture: %v", frames)
	}
}

/*
================
TestCaptureCharacterFramesDoesNotAdoptLaterRecipients
================
*/
func TestCaptureCharacterFramesDoesNotAdoptLaterRecipients(t *testing.T) {
	server := wiringPublicationServer(t)
	original, originalConn := wiringPublicationSession(t, server, "original")
	publish := captureCharacterFrames(server.Hub, wiringPublicationDivision, "Alice")
	if publish == nil {
		t.Fatal("original capture missing")
	}
	later, laterConn := wiringPublicationSession(t, server, "later")
	receipt := []byte{1, 15, 49, 0, 0xec, 8}
	publish([]wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: receipt}})
	frames := wiringPublicationFlush(t, original, originalConn)
	if len(frames) != 1 || frames[0].Opcode != wire.OpItemUseResponse || !bytes.Equal(frames[0].Payload, receipt) {
		t.Fatalf("original recipient lost its captured receipt: %v", frames)
	}
	if frames := wiringPublicationFlush(t, later, laterConn); len(frames) != 0 {
		t.Fatalf("capture adopted a later bound recipient: %v", frames)
	}
}
