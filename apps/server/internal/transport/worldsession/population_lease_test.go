package worldsession

import (
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func TestSnapshotRequiresAuthenticatedPopulationLifetime(t *testing.T) {
	srv := startServer(t)
	_, welcome := dialAndHello(t, srv)
	sess, _ := srv.Hub.Session(welcome.SessionID)
	id := instance.Pack(10, 1)
	sess.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
		DivisionID: "DIV_A", CharacterID: 7, WorldInstance: uint32(id),
	}})
	b := New(srv.Hub)
	lease := instance.Lease{ID: id, Generation: 2}
	valid := true
	b.PopulationLease = func(division, name string, session uint64) (instance.Lease, bool) {
		if division != "DIV_A" || name != "Alice" || session != welcome.SessionID {
			t.Fatal("wrong admission identity")
		}
		return lease, valid
	}
	if len(b.SnapshotSessions()) != 0 {
		t.Fatal("unbound transport entered population")
	}
	sess.BindCharacter("DIV_A", "Alice", 0)
	sess.SetWorldSnapshot("DIV_A", staticProvider{simulation.SessionSnapshot{
		DivisionID: "DIV_A", CharacterID: 7, WorldInstance: uint32(id),
	}})
	snapshots := b.SnapshotSessions()
	if len(snapshots) != 1 || snapshots[0].Population != lease || snapshots[0].PublishedObjects == nil {
		t.Fatal(snapshots)
	}
	valid = false // old membership after release, even if packed ID is reused
	lease.Generation++
	if len(b.SnapshotSessions()) != 0 {
		t.Fatal("retired admission adopted reused wire ID")
	}
	valid = true
	lease.ID = instance.Pack(10, 2)
	if len(b.SnapshotSessions()) != 0 {
		t.Fatal("provider and membership disagree")
	}
}
