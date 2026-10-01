package worldsession

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

func TestBridgeScopePublicationControlsSynchronousObservers(t *testing.T) {
	srv := startServer(t)
	c, welcome := dialAndHello(t, srv)
	session, _ := srv.Hub.Session(welcome.SessionID)
	session.BindCharacter("DIV_A", "Viewer", 0)
	bridge := New(srv.Hub)
	const gid = 81
	action := []transport.Frame{{Opcode: 0x324b}}
	srv.Hub.BroadcastObserved("DIV_A", 0, gid, action)
	bridge.PushToSession(SessionSceneID(session), []simulation.Frame{
		{ScopeGID: gid, ScopeVisible: true, Opcode: 0x30d7},
		{Opcode: 0x3122},
	})
	srv.Hub.BroadcastObserved("DIV_A", 0, gid, action)
	bridge.PushToSession(SessionSceneID(session), []simulation.Frame{{ScopeGID: gid, Opcode: 0x36ab}})
	srv.Hub.BroadcastObserved("DIV_A", 0, gid, action)
	_ = session.Send(0x7777, nil)
	for _, want := range []uint16{0x30d7, 0x3122, 0x324b, 0x36ab, 0x7777} {
		if frame := readFrame(t, c); frame.Opcode != want {
			t.Fatalf("scope publication order: got %x, want %x", frame.Opcode, want)
		}
	}
}
