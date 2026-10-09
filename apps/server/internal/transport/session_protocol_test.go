/*
===========================================================================

session_protocol_test.go - the release protocol the EnterWorld gate records

The gate (wiring_runtime.go enterWorldVerifierForShard) records the
protocol its verified token binds before the game handler runs; the handler
reads it back from the same session to pick its protocol's encodings.

===========================================================================
*/
package transport

import (
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
TestTheGateHandsTheHandlerItsReleaseProtocol
================
*/
func TestTheGateHandsTheHandlerItsReleaseProtocol(t *testing.T) {
	hub, _, fc := attachedSession(t, false)
	hub.SetEnterWorldAuth(func(s *Session, _ EnterWorld) (bool, uint32) {
		if s.ReleaseProtocol() != 0 {
			t.Errorf("protocol %d before the gate recorded one", s.ReleaseProtocol())
		}
		s.SetReleaseProtocol(5)
		return true, 0
	})
	seen := make(chan int, 1)
	hub.Handle(OpEnterWorld, func(s *Session, _ uint16, _ []byte) {
		seen <- s.ReleaseProtocol()
	})
	fc.inbound <- Frame{Opcode: OpEnterWorld, Payload: EncodeEnterWorld(EnterWorld{
		Division: "d1", CharName: "cg", AuthToken: []byte("token"),
	})}
	var got int
	wait.Eventually(t, 3*time.Second, "the EnterWorld handler to run", func() bool {
		select {
		case got = <-seen:
			return true
		default:
			return false
		}
	})
	if got != 5 {
		t.Fatalf("handler read protocol %d, want the gate's 5", got)
	}
}
