/*
===========================================================================

bridge_snapshot_bench_test.go - one tick's session snapshots, measured

A live tick takes the full scene snapshot once (the coordinator) and the
light session views once per view hook (skill objects, ground scope,
hunting points, party updates, beta silk). This bench builds that tick's
walks over a hub of in-world sessions, each publishing a realistic object
set, behind a provider shaped like the movement lane's (a cloned world
with a live ground walk and segment, and a fresh peer appearance).

===========================================================================
*/
package worldsession

import (
	"strconv"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const (
	benchSnapshotSessions  = 64  // in-world sessions on the hub
	benchPublishedPerScene = 400 // objects each scene has published
	benchViewHooksPerTick  = 5   // SnapshotSessionViews callers per tick
	benchPublishedGIDBase  = 0x10000
)

/*
================
benchProvider

The movement lane's provider shape: the world is cloned under its own lock
for every face, and the full face builds a fresh appearance.
================
*/
type benchProvider struct {
	mu    *sync.Mutex
	world *simulation.WorldState
	char  int64
}

func (p benchProvider) WorldSnapshot() simulation.SessionSnapshot {
	p.mu.Lock()
	world := simulation.CloneWorldState(*p.world)
	p.mu.Unlock()
	return simulation.SessionSnapshot{DivisionID: "DIV_A", CharacterID: p.char, World: world,
		MovementCurrent: func() bool { return world.MoveSegment != nil },
		Appearance:      &simulation.PeerAppearance{Name: "hero", ActionSpeed: 125}}
}

func (p benchProvider) WorldView() simulation.SessionView {
	p.mu.Lock()
	world := simulation.CloneWorldState(*p.world)
	p.mu.Unlock()
	return simulation.SessionView{DivisionID: "DIV_A", CharacterID: p.char, World: world}
}

/*
================
benchSnapshotBridge

A hub of benchSnapshotSessions ready scenes, each with its published set.
Every HELLO admits a distinct account, so the per-account session limit
does not evict the earlier sessions.
================
*/
func benchSnapshotBridge(b *testing.B) *Bridge {
	level := log.GetLevel()
	log.SetLevel(log.ErrorLevel)
	b.Cleanup(func() { log.SetLevel(level) })
	srv := startServer(b)
	var accounts sync.Mutex
	admitted := 0
	srv.Hub.SetHelloAuth(func([]byte) (transport.AdmissionIdentity, error) {
		accounts.Lock()
		defer accounts.Unlock()
		admitted++
		return transport.AdmissionIdentity{AccountID: "bench-account-" + strconv.Itoa(admitted), ShardID: "global-official"}, nil
	})
	for i := 0; i < benchSnapshotSessions; i++ {
		_, welcome := dialAndHello(b, srv)
		sess, ok := srv.Hub.Session(welcome.SessionID)
		if !ok {
			b.Fatal("session missing from hub")
		}
		scope := make([]transport.ObjectScopeChange, 0, benchPublishedPerScene)
		for j := 0; j < benchPublishedPerScene; j++ {
			scope = append(scope, transport.ObjectScopeChange{GID: uint32(benchPublishedGIDBase + j*7 + i), Visible: true})
		}
		if err := sess.SendSceneReset([]transport.Frame{{Opcode: transport.OpEnterWorldResult, Payload: []byte{1}, Scope: scope}}); err != nil {
			b.Fatal(err)
		}
		sess.FinishSceneReentry()
		world := simulation.DefaultWorldState(simulation.EuropeStartProfile())
		world.MoveSegment = &simulation.MoveSegment{From: world.Spawn, StartedAtMs: 1, ArrivesAtMs: 2}
		world.Ground = &simulation.GroundWalkState{At: 1, Pose: world.Spawn}
		sess.SetWorldSnapshot("DIV_A", benchProvider{mu: &sync.Mutex{}, world: &world, char: int64(i + 1)})
	}
	return New(srv.Hub)
}

/*
================
BenchmarkBridgeSnapshotTick

One tick's walks: the full snapshot and one light view per view hook.
================
*/
func BenchmarkBridgeSnapshotTick(b *testing.B) {
	bridge := benchSnapshotBridge(b)
	if got := len(bridge.SnapshotSessions()); got != benchSnapshotSessions {
		b.Fatalf("snapshot holds %d sessions, want %d", got, benchSnapshotSessions)
	}
	b.ReportAllocs()
	for b.Loop() {
		bridge.SnapshotSessions()
		for range benchViewHooksPerTick {
			bridge.SnapshotSessionViews()
		}
	}
}
