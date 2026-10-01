package worldsession

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// Exercise the production adapter and socket writer: dropping Current while
// converting frames would silently restore pre-death movement to peer clients.
func TestBridgePreservesMovementDeliveryFence(t *testing.T) {
	srv := startServer(t)
	c, welcome := dialAndHello(t, srv)
	session, _ := srv.Hub.Session(welcome.SessionID)
	session.BindCharacter("DIV_A", "Viewer", 0)
	bridge := New(srv.Hub)
	stale := simulation.Frame{Opcode: transport.OpObjectSourceMove, Payload: make([]byte, 20), Current: func() bool { return false }}
	current := simulation.Frame{Opcode: transport.OpObjectSourceCorrection, Payload: make([]byte, 20), Current: func() bool { return true }}
	current.Payload[0] = 17
	life := simulation.Frame{Opcode: wire.OpObjectStateRefresh, Payload: []byte{17, 0, 0, 0, 0, 2}}
	bridge.PushToDivision("DIV_A", []simulation.Frame{stale, current, life}, "")
	for _, want := range []simulation.Frame{current, life} {
		got := readFrame(t, c)
		if got.Opcode != want.Opcode || !bytes.Equal(got.Payload, want.Payload) {
			t.Fatalf("movement lifecycle delivery: got %#x %x, want %#x %x", got.Opcode, got.Payload, want.Opcode, want.Payload)
		}
	}
}
