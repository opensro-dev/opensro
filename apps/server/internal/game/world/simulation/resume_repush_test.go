/*
===========================================================================

resume_repush_test.go - the resumed viewer's mover re-push (#1818)

===========================================================================
*/

package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// resumeSourceStub serves fixed snapshots and drains each resume mark once,
// the bridge's contract in miniature.
type resumeSourceStub struct {
	sessions []SessionSnapshot
	drained  []string
}

func (s *resumeSourceStub) SnapshotSessions() []SessionSnapshot {
	return append([]SessionSnapshot(nil), s.sessions...)
}

func (s *resumeSourceStub) DrainResumedSessions() []string {
	out := s.drained
	s.drained = nil
	return out
}

func correctionsFor(frames []Frame, gid uint32) []Frame {
	var out []Frame
	for _, frame := range frames {
		if frame.Opcode != wire.OpObjectSourceCorrection {
			continue
		}
		if decoded, err := wire.DecodeObjectSourceCorrection(frame.Payload); err == nil && decoded.Gid == gid {
			out = append(out, frame)
		}
	}
	return out
}

// A monster walks to a spot and stops while the viewer is detached; its
// final (lossy) correction never arrives. On the tick that serves the
// resume, the resumed viewer - and only that viewer - is re-sent the
// monster's current position on the ordered lane, exactly once.
func TestResumedViewerGetsSettledMonsterPosition(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	registry := ops.Monsters
	mover, _ := registry.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = math.MaxInt64 // stand still: no wander, no chase
	registry.CommitMover(monsterTestDivision, instance.Gid, mover)

	viewer := playerSessionAt(7, 1000, 1000)
	other := playerSessionAt(8, 1000, 1000)
	other.SessionID = "other"
	source := &resumeSourceStub{sessions: []SessionSnapshot{viewer, other}}
	push := &fakePusher{}
	ticker := NewTicker(source, push)
	ticker.Monsters = ops

	now := registry.CurrentTimeMillis()
	ticker.RunTick(now)
	if len(correctionsFor(sessionFrames(push, "viewer"), instance.Gid)) != 0 {
		t.Fatal("a settled monster produced a correction before any resume")
	}

	// The monster settles somewhere new while the viewer is away. Pose
	// replacement is the settled end state the lost correction carried.
	mover, _ = registry.Mover(monsterTestDivision, instance.Gid)
	mover.Pose = monster.Pose{RegionID: monsterTestRegion, X: 1150, Y: 20, Z: 1000, Heading: mover.Pose.Heading}
	registry.CommitMover(monsterTestDivision, instance.Gid, mover)

	push.toSession = nil
	source.drained = []string{"viewer"}
	ticker.RunTick(now + 100)

	frames := correctionsFor(sessionFrames(push, "viewer"), instance.Gid)
	if len(frames) != 1 {
		t.Fatalf("resumed viewer got %d corrections for the settled monster, want 1", len(frames))
	}
	decoded, err := wire.DecodeObjectSourceCorrection(frames[0].Payload)
	if err != nil {
		t.Fatalf("correction decode: %v", err)
	}
	if decoded.Position.X != 1150 || decoded.Position.Z != 1000 {
		t.Fatalf("correction position = (%v, %v), want the settled (1150, 1000)", decoded.Position.X, decoded.Position.Z)
	}
	if frames[0].Current == nil || !frames[0].Current() {
		t.Fatal("re-push frame must carry the ordered-lane admission")
	}
	if got := correctionsFor(sessionFrames(push, "other"), instance.Gid); len(got) != 0 {
		t.Fatalf("a viewer that never resumed got %d corrections, want none", len(got))
	}

	// One-shot: the next tick re-pushes nothing.
	push.toSession = nil
	ticker.RunTick(now + 200)
	if got := correctionsFor(sessionFrames(push, "viewer"), instance.Gid); len(got) != 0 {
		t.Fatalf("second tick re-pushed %d corrections, want none", len(got))
	}
}

// The NPC half of the bound: only patrolling NPCs inside the viewer's
// publication set are corrected, onto the canonical patrol track.
func TestResumedNpcFramesOnlyPublishedPatrol(t *testing.T) {
	roster := DefaultNpcRoster()
	var patrol, still *NpcDef
	for i := range roster {
		if roster[i].Patrol && patrol == nil {
			patrol = &roster[i]
		}
		if !roster[i].Patrol && still == nil {
			still = &roster[i]
		}
	}
	if patrol == nil {
		t.Fatal("default roster has no patrolling NPC to correct")
	}
	anchor := NpcShopSpawn()
	session := SessionSnapshot{
		SessionID: "viewer", DivisionID: monsterTestDivision, NpcsEnabled: true,
		NpcAnchor:        anchor,
		PublishedObjects: []uint32{patrol.ObjectID},
	}
	frames := resumedNpcFrames(roster, session, 7)
	if len(frames) != 1 {
		t.Fatalf("resumed NPC frames = %d, want 1 (the published patrol NPC)", len(frames))
	}
	decoded, err := wire.DecodeObjectSourceCorrection(frames[0].Payload)
	if err != nil || decoded.Gid != patrol.ObjectID {
		t.Fatalf("NPC correction = %+v (%v), want gid %d", decoded, err, patrol.ObjectID)
	}
	wantAnchor := anchor
	wantAnchor.X += patrol.SpawnOffsetX
	wantAnchor.Z += patrol.SpawnOffsetZ
	want := ComputeNpcPatrolState(wantAnchor, 7)
	if decoded.Position.X != float32(want.X) || decoded.Position.Z != float32(want.Z) {
		t.Fatalf("NPC correction = (%v, %v), want patrol state (%v, %v)",
			decoded.Position.X, decoded.Position.Z, want.X, want.Z)
	}

	if still != nil {
		session.PublishedObjects = []uint32{still.ObjectID}
		if frames := resumedNpcFrames(roster, session, 7); len(frames) != 0 {
			t.Fatalf("static NPC produced %d corrections, want none", len(frames))
		}
	}
	session.PublishedObjects = []uint32{uint32(domain.MonsterGIDBase) + 5}
	if frames := resumedNpcFrames(roster, session, 7); len(frames) != 0 {
		t.Fatalf("unpublished NPC produced %d corrections, want none", len(frames))
	}
}

// The companion half: a shown peer pet and the viewer's own pet are both
// re-projected; an unknown gid in the shown set is skipped.
func TestResumedCompanionFramesCoverShownAndOwnPets(t *testing.T) {
	world := WorldState{Spawn: Spawn{RegionID: monsterTestRegion, X: 1100, Y: 20, Z: 1000}, SpawnSet: true}
	peerPet := &PeerCOS{Row: wire.CosSpawnBand2{Gid: 5001}, World: world}
	ownPet := &PeerCOS{Row: wire.CosSpawnBand2{Gid: 5002}, World: world}
	owner := SessionSnapshot{SessionID: "owner", DivisionID: monsterTestDivision, CharacterID: 9, COS: peerPet}
	viewer := SessionSnapshot{SessionID: "viewer", DivisionID: monsterTestDivision, CharacterID: 7, COS: ownPet}
	state := &divisionTickState{shownCOS: map[string]map[uint32]shownCOS{
		"viewer": {5001: {}, 9999: {}},
	}}
	work := divisionTickWork{divisionID: monsterTestDivision, sessions: []SessionSnapshot{owner, viewer}, state: state}

	frames := resumedCompanionFrames(work, viewer, 50_000)
	var gids []uint32
	for _, frame := range frames {
		decoded, err := wire.DecodeObjectSourceCorrection(frame.Payload)
		if err != nil {
			t.Fatalf("companion frame %#04x is not a correction: %v", frame.Opcode, err)
		}
		gids = append(gids, decoded.Gid)
	}
	if len(gids) != 2 || gids[0] != 5001 || gids[1] != 5002 {
		t.Fatalf("companion corrections = %v, want [5001 5002] (shown peer pet, own pet)", gids)
	}
}
