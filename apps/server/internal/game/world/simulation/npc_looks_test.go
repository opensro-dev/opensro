/*
===========================================================================

npc_looks_test.go - a seasonal look dresses its NPC for its window

===========================================================================
*/
package simulation

import (
	"testing"
	"time"
)

/*
================
lookTestRoster

So-ok and a smith; the look is Santa So-Ok's reference.
================
*/
func lookTestRoster() ([]NpcDef, NpcLook) {
	roster := []NpcDef{
		{ObjectID: 200001, RefObjID: 3861, Codename: "NPC_CH_EVENT_KISAENG1", Name: "Event So-Ok", AuthoredSpawn: true, Spawn: Spawn{RegionID: 25000, X: 900, Z: 900}},
		{ObjectID: 200002, RefObjID: 2001, Codename: "NPC_CH_SMITH", Name: "Smith", AuthoredSpawn: true, Spawn: Spawn{RegionID: 25000, X: 910, Z: 900}},
	}
	look := NpcLook{
		Codename: "NPC_CH_EVENT_KISAENG1",
		Look:     NpcDef{RefObjID: 3656, Codename: "NPC_CH_EVENT_SANTA_KISAENG1", NameStrID: "SN_NPC_CH_EVENT_SANTA_KISAENG1", Name: "Santa So-Ok"},
		StartsAt: time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2027, 1, 9, 0, 0, 0, 0, time.UTC),
	}
	return roster, look
}

/*
================
TestNpcLookDressesOnlyInsideItsWindow

Before the window the base roster; at each edge a changeover with the
NPC hidden; inside the window So-ok spawns under Santa So-Ok's reference
with her own object id and codename; the smith never changes; the base
roster is never written.
================
*/
func TestNpcLookDressesOnlyInsideItsWindow(t *testing.T) {
	base, look := lookTestRoster()
	rosters := NewNpcLookRosters(base, look)
	if ref, ok := rosters.Look(); !ok || ref.RefObjID != 3656 {
		t.Fatal("the look reference", ref, ok)
	}
	check := func(at time.Time, ref uint32, hidden bool) {
		t.Helper()
		roster := rosters.At(at)
		if roster[0].RefObjID != ref || roster[0].Hidden != hidden || roster[0].ObjectID != 200001 || roster[0].Codename != "NPC_CH_EVENT_KISAENG1" {
			t.Fatalf("at %v: So-ok %+v", at, roster[0])
		}
		if roster[1].RefObjID != 2001 || roster[1].Hidden {
			t.Fatalf("at %v: the smith changed", at)
		}
	}
	check(look.StartsAt.Add(-time.Second), 3861, false)
	check(look.StartsAt, 3861, true)
	check(look.StartsAt.Add(lookChangeover), 3656, false)
	check(look.EndsAt.Add(-time.Second), 3656, false)
	check(look.EndsAt, 3861, true)
	check(look.EndsAt.Add(lookChangeover), 3861, false)
	if base[0].RefObjID != 3861 || base[0].Hidden {
		t.Fatal("the base roster was written")
	}
	if empty := NewNpcLookRosters(base, NpcLook{Codename: "NPC_NOBODY", Look: look.Look, StartsAt: look.StartsAt, EndsAt: look.EndsAt}); &empty.At(look.StartsAt.Add(time.Hour))[0] != &base[0] {
		t.Fatal("a look naming no placed NPC must answer the base roster")
	}
}

/*
================
TestNpcLookChangeoverRespawnsTheNewLook

A viewer who sees So-ok in her own look: the changeover's scope pass
despawns her, and the first pass inside the dressed window spawns her
again under Santa So-Ok's reference.
================
*/
func TestNpcLookChangeoverRespawnsTheNewLook(t *testing.T) {
	base, look := lookTestRoster()
	rosters := NewNpcLookRosters(base, look)
	session := SessionSnapshot{NpcsEnabled: true, PublishedObjects: []uint32{200001, 200002}}
	session.World.Spawn = Spawn{RegionID: 25000, X: 905, Z: 900}

	frames := NpcScopeFrames(rosters.At(look.StartsAt.Add(time.Second)), session, 0)
	if len(frames) != 1 || frames[0].ScopeGID != 200001 || frames[0].ScopeVisible {
		t.Fatalf("changeover: %+v", frames)
	}
	session.PublishedObjects = []uint32{200002}
	frames = NpcScopeFrames(rosters.At(look.StartsAt.Add(time.Hour)), session, 0)
	if len(frames) != 1 || frames[0].ScopeGID != 200001 || !frames[0].ScopeVisible {
		t.Fatalf("dressed spawn: %+v", frames)
	}
	if ref := uint32(frames[0].Payload[0]) | uint32(frames[0].Payload[1])<<8 | uint32(frames[0].Payload[2])<<16 | uint32(frames[0].Payload[3])<<24; ref != 3656 {
		t.Fatalf("So-ok spawned under ref %d, want Santa So-Ok's 3656", ref)
	}
}
