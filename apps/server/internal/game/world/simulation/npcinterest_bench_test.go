/*
===========================================================================

npcinterest_bench_test.go - allocation cost of the per-tick NPC scope pass

#3291: NpcScopeFrames runs once per session per tick; the live profile
put it at 2.8 % of allocated bytes. These benchmarks pin the two shapes
that matter: the steady state (every authored NPC already admitted, the
pass must only prove nothing changed) and the entry storm (no NPC
admitted yet, every visible NPC owes a spawn row).

===========================================================================
*/
package simulation

import "testing"

// benchNpcScope is one town-sized scope problem: 200 authored NPCs all in
// interest range of the viewer, and a published list of 1200 objects of
// which the NPC gids are the minority, as in a busy town (players,
// monsters and ground items fill the rest).
const (
	benchRosterSize    = 200
	benchPublishedSize = 1200
)

/*
================
benchNpcScopeFixture

admitted=true is the steady state: every roster NPC is on the viewer's
published list, so a correct pass returns no frames.
================
*/
func benchNpcScopeFixture(admitted bool) ([]NpcDef, SessionSnapshot) {
	spawn := Spawn{RegionID: 25163, X: 1000, Y: 10, Z: 1000}
	roster := make([]NpcDef, benchRosterSize)
	for i := range roster {
		roster[i] = NpcDef{ObjectID: 200001 + uint32(i), RefObjID: 7524, AuthoredSpawn: true, Spawn: spawn}
	}
	published := make([]uint32, 0, benchPublishedSize)
	for i := 0; i < benchPublishedSize-benchRosterSize; i++ {
		published = append(published, 400001+uint32(i))
	}
	if admitted {
		for _, npc := range roster {
			published = append(published, npc.ObjectID)
		}
	}
	session := SessionSnapshot{
		SessionID:        "bench:1",
		NpcsEnabled:      true,
		PublishedObjects: published,
		World:            WorldState{Spawn: spawn},
	}
	return roster, session
}

/*
================
BenchmarkNpcScopeFramesSteady

The hot shape: nothing changed, so the only work owed is the proof.
================
*/
func BenchmarkNpcScopeFramesSteady(b *testing.B) {
	roster, session := benchNpcScopeFixture(true)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if frames := NpcScopeFrames(roster, session, 100); len(frames) != 0 {
			b.Fatalf("steady state produced %d frames", len(frames))
		}
	}
}

/*
================
BenchmarkNpcScopeFramesSteadyBuffered

The tick's steady state with its division scratch carried from call to
call, as runSessionLegs runs it: the admitted set and the frame slice
are already allocated, so the proof should allocate nothing.
================
*/
func BenchmarkNpcScopeFramesSteadyBuffered(b *testing.B) {
	roster, session := benchNpcScopeFixture(true)
	buffer := &NpcScopeBuffer{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if frames := NpcScopeFramesInto(buffer, roster, session, 100); len(frames) != 0 {
			b.Fatalf("steady state produced %d frames", len(frames))
		}
	}
}

/*
================
BenchmarkNpcScopeFramesEntry

The entry shape: every roster NPC owes a spawn row, so the payload
builds dominate and only the bookkeeping around them can be saved.
================
*/
func BenchmarkNpcScopeFramesEntry(b *testing.B) {
	roster, session := benchNpcScopeFixture(false)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if frames := NpcScopeFrames(roster, session, 100); len(frames) != benchRosterSize {
			b.Fatalf("entry produced %d frames, want %d", len(frames), benchRosterSize)
		}
	}
}

/*
================
BenchmarkNpcScopeFramesEntryBuffered

The entry shape through the tick's carried scratch: the 200 spawn
payloads are still built fresh (the session retains them), so only the
admitted-set map and the frame slice growth come out.
================
*/
func BenchmarkNpcScopeFramesEntryBuffered(b *testing.B) {
	roster, session := benchNpcScopeFixture(false)
	buffer := &NpcScopeBuffer{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if frames := NpcScopeFramesInto(buffer, roster, session, 100); len(frames) != benchRosterSize {
			b.Fatalf("entry produced %d frames, want %d", len(frames), benchRosterSize)
		}
	}
}
