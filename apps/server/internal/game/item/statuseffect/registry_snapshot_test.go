/*
===========================================================================

registry_snapshot_test.go - the published snapshot view against the rows

Snapshot republishes an owner's rows only after a writer marked the owner
stale. This differential drives the public writers in a seeded order and,
after every operation, compares the served view against an independent copy
of the authoritative rows; a missed mark shows up as a stale view at once.

===========================================================================
*/
package statuseffect

import (
	"math/rand"
	"reflect"
	"testing"
)

/*
================
TestSnapshotTracksEveryWriter
================
*/
func TestSnapshotTracksEveryWriter(t *testing.T) {
	rng := rand.New(rand.NewSource(2030))
	r := NewRegistry()
	owners := [][2]string{{"d", "anna"}, {"d", "bert"}, {"d", "cora"}}
	skill := uint32(1000)
	token := uint32(7000)

	fresh := func(owner [2]string) []Effect {
		return append([]Effect(nil), r.byOwner[ownerKey(owner[0], owner[1])]...)
	}
	check := func(step int) {
		t.Helper()
		for _, owner := range owners {
			got := r.Snapshot(owner[0], owner[1])
			if !reflect.DeepEqual(got, fresh(owner)) {
				t.Fatalf("step %d: snapshot for %v = %+v, rows %+v", step, owner, got, fresh(owner))
			}
			if len(got) > 0 {
				again := r.Snapshot(owner[0], owner[1])
				if &again[0] != &got[0] {
					t.Fatalf("step %d: unchanged snapshot for %v recopied", step, owner)
				}
			}
		}
	}

	for step := 0; step < 400; step++ {
		owner := owners[rng.Intn(len(owners))]
		switch rng.Intn(9) {
		case 0, 1, 2:
			skill++
			token++
			r.Apply(Effect{
				DivisionID: owner[0], CharacterName: owner[1],
				SkillID: skill, SkillGroup: skill,
				InstanceToken: token, State: StateActive,
				ClientCancelable: rng.Intn(2) == 0,
			})
		case 3:
			r.RequestVoluntaryStop(owner[0], owner[1], 1000+uint32(rng.Intn(60)), 0)
		case 4:
			r.Expire(int64(step) * 1000)
		case 5:
			r.DrainStopRequested()
		case 6:
			skill++
			token++
			r.Apply(Effect{
				DivisionID: owner[0], CharacterName: owner[1],
				SkillID: skill, SkillGroup: skill,
				InstanceToken: token, State: StateActive,
				Persistent: true, ExpiresAtMs: int64(step)*1000 + 600000, StartedAtMs: int64(step) * 1000,
			})
			r.AnyJobCheckpointDue(int64(step) * 1000)
			r.TakeJobCheckpoints(int64(step)*1000 + 1000)
		case 7:
			r.RetireEvent(owner[0], owner[1], 0xff)
			r.RetireInstances(owner[0], owner[1], []uint32{token})
		case 8:
			r.RetireBodyStatusesOnDeath(owner[0], owner[1])
			r.Forget(owner[0], owner[1])
		}
		check(step)
	}
}

/*
================
TestSnapshotSeesEachNarrowWriter

The seeded walk above marks every owner through Expire and
AnyJobCheckpointDue, which hides a writer that forgets its own mark. Each
writer here runs alone between two reads, so only its mark can refresh the
view.
================
*/
func TestSnapshotSeesEachNarrowWriter(t *testing.T) {
	stale := func(t *testing.T, r *Registry) {
		t.Helper()
		rows := append([]Effect(nil), r.byOwner[ownerKey("d", "anna")]...)
		if got := r.Snapshot("d", "anna"); !reflect.DeepEqual(got, rows) {
			t.Fatalf("snapshot %+v, rows %+v", got, rows)
		}
	}
	t.Run("forced target stop", func(t *testing.T) {
		r := NewRegistry()
		effect := Effect{
			DivisionID: "d", CharacterName: "anna", SkillID: 1, SkillGroup: 1,
			InstanceToken: 7, State: StateActive, ForcedTargetGID: 9,
		}
		r.Apply(effect)
		r.Snapshot("d", "anna")
		r.StopForcedTarget(effect)
		if !r.byOwner[ownerKey("d", "anna")][0].StopRequested {
			t.Fatal("the stop did not land, so the view proved nothing")
		}
		stale(t, r)
	})
	t.Run("job checkpoint", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(Effect{
			DivisionID: "d", CharacterName: "anna", SkillID: 1, SkillGroup: 1,
			InstanceToken: 7, State: StateActive, Persistent: true,
			StartedAtMs: 1000, ExpiresAtMs: 601000,
		})
		before := r.byOwner[ownerKey("d", "anna")][0].jobUpdatedAtMs
		r.Snapshot("d", "anna")
		r.TakeJobCheckpoints(2000)
		if r.byOwner[ownerKey("d", "anna")][0].jobUpdatedAtMs == before {
			t.Fatal("the checkpoint moved nothing, so the view proved nothing")
		}
		stale(t, r)
	})
	t.Run("link marks both owners", func(t *testing.T) {
		r := NewRegistry()
		link := testLink()
		r.Snapshot(link.DivisionID, link.SourceName)
		r.Snapshot(link.DivisionID, link.TargetName)
		if code := r.ApplyLink(link); code != 0 {
			t.Fatalf("link refused: %#x", code)
		}
		for _, name := range []string{link.SourceName, link.TargetName} {
			got := r.Snapshot(link.DivisionID, name)
			if len(got) != 1 {
				t.Fatalf("%s's view holds %d rows after the link, want 1", name, len(got))
			}
		}
	})
	t.Run("replacement install", func(t *testing.T) {
		r := NewRegistry()
		held := movementFixture(1, MovementHaste, 20)
		incoming := movementFixture(2, MovementHaste, 40)
		incoming.SkillGroup = 2
		r.Apply(held)
		r.Snapshot(held.DivisionID, held.CharacterName)
		// A different group retires nothing, so no stop's mark can
		// refresh the view: only the install's own mark can.
		if !r.ApplyReplacement(ReplacementApplication{Effect: incoming, Descriptors: map[uint32]ReplacementDescriptor{
			1: {Category: 3, Group: 1, Rank: 2}, 2: {Category: 3, Group: 2, Rank: 3},
		}}) {
			t.Fatal("replacement refused")
		}
		if got := r.Snapshot(held.DivisionID, held.CharacterName); len(got) != 2 {
			t.Fatalf("the view holds %d rows after the install, want 2", len(got))
		}
	})
}

/*
================
TestSnapshotSeesTheOtherWriters

The remaining writers, each alone between two reads like the narrow pair
above. Every subtest first proves the writer actually landed (a row or
clock fact changed), so a passing view comparison is never vacuous: with
the writer's mark removed, the stale view and the rows part ways at once.
================
*/
func TestSnapshotSeesTheOtherWriters(t *testing.T) {
	viewMatches := func(t *testing.T, r *Registry, division, name string) {
		t.Helper()
		rows := append([]Effect(nil), r.byOwner[ownerKey(division, name)]...)
		if got := r.Snapshot(division, name); !reflect.DeepEqual(got, rows) {
			t.Fatalf("snapshot for %s/%s = %+v, rows %+v", division, name, got, rows)
		}
	}
	plain := Effect{
		DivisionID: "d", CharacterName: "anna", SkillID: 10, SkillGroup: 10,
		InstanceToken: 3, State: StateActive, ClientCancelable: true,
	}

	t.Run("voluntary stop", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		r.Snapshot("d", "anna")
		if _, ok := r.RequestVoluntaryStop("d", "anna", 10, 0); !ok {
			t.Fatal("the stop did not land, so the view proved nothing")
		}
		if !r.byOwner[ownerKey("d", "anna")][0].StopRequested {
			t.Fatal("the stop did not land, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("drain", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		if _, ok := r.RequestVoluntaryStop("d", "anna", 10, 0); !ok {
			t.Fatal("the stop did not land, so the view proved nothing")
		}
		r.Snapshot("d", "anna")
		if batches := r.DrainStopRequested(); len(batches) != 1 {
			t.Fatalf("the drain returned %d batches, so the view proved nothing", len(batches))
		}
		if len(r.byOwner[ownerKey("d", "anna")]) != 0 {
			t.Fatal("the drain removed nothing, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("death retirement", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		r.Snapshot("d", "anna")
		if ended := r.RetireBodyStatusesOnDeath("d", "anna"); len(ended) != 1 {
			t.Fatalf("death retired %d rows, so the view proved nothing", len(ended))
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("forget", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		r.Snapshot("d", "anna")
		r.Forget("d", "anna")
		if len(r.byOwner[ownerKey("d", "anna")]) != 0 {
			t.Fatal("forget removed nothing, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("event retirement", func(t *testing.T) {
		r := NewRegistry()
		masked := plain
		masked.EventCancelMask = 0x4
		r.Apply(masked)
		r.Snapshot("d", "anna")
		if ended := r.RetireEvent("d", "anna", 0x4); len(ended) != 1 {
			t.Fatalf("the event retired %d rows, so the view proved nothing", len(ended))
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("instance retirement", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		r.Snapshot("d", "anna")
		if ended := r.RetireInstances("d", "anna", []uint32{3}); len(ended) != 1 {
			t.Fatalf("the retirement ended %d rows, so the view proved nothing", len(ended))
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("any job checkpoint due", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(Effect{
			DivisionID: "d", CharacterName: "anna", SkillID: 1, SkillGroup: 1,
			InstanceToken: 7, State: StateActive, Persistent: true,
			StartedAtMs: 1000, ExpiresAtMs: 601000,
		})
		before := r.byOwner[ownerKey("d", "anna")][0].jobUpdatedAtMs
		r.Snapshot("d", "anna")
		r.AnyJobCheckpointDue(2000)
		if r.byOwner[ownerKey("d", "anna")][0].jobUpdatedAtMs == before {
			t.Fatal("the clock moved nothing, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("quiet sweeps keep the view", func(t *testing.T) {
		r := NewRegistry()
		r.Apply(plain)
		got := r.Snapshot("d", "anna")
		// Neither per-tick sweep moves a plain row: Expire finds no
		// deadline and no job clock, and the checkpoint probe finds
		// no clock either. The published view must stand.
		r.Expire(5000)
		r.AnyJobCheckpointDue(5000)
		again := r.Snapshot("d", "anna")
		if len(again) != 1 || &again[0] != &got[0] {
			t.Fatal("a sweep over unchanged rows republished the view")
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("an expiry flip marks the view", func(t *testing.T) {
		r := NewRegistry()
		timed := plain
		timed.ExpiresAtMs = 2000
		r.Apply(timed)
		r.Snapshot("d", "anna")
		// The flip is this owner's only movement this tick: no job clock
		// runs, so the sweep's flip branch alone must mark it.
		r.Expire(3000)
		if rows := r.Snapshot("d", "anna"); len(rows) != 1 || !rows[0].StopRequested {
			t.Fatal("the expiry flip left the published view stale", rows)
		}
		viewMatches(t, r, "d", "anna")
	})
	t.Run("link install", func(t *testing.T) {
		r := NewRegistry()
		r.Snapshot("d", "anna")
		r.Snapshot("d", "bert")
		link := Link{
			DivisionID: "d", SourceName: "anna", TargetName: "bert",
			SourceGID: 11, TargetGID: 22, SourceToken: 101, TargetToken: 202,
			SkillID: 5, SkillGroup: 5, StartedAtMs: 0, ExpiresAtMs: 100000,
		}
		if code := r.ApplyLink(link); code != 0 {
			t.Fatalf("the link refused with %#x, so the view proved nothing", code)
		}
		if len(r.byOwner[ownerKey("d", "anna")]) != 1 || len(r.byOwner[ownerKey("d", "bert")]) != 1 {
			t.Fatal("the link installed nothing, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
		viewMatches(t, r, "d", "bert")
	})
	t.Run("replacement install", func(t *testing.T) {
		r := NewRegistry()
		r.Snapshot("d", "anna")
		applied := r.ApplyReplacement(ReplacementApplication{
			Effect: Effect{
				DivisionID: "d", CharacterName: "anna", SkillID: 7, SkillGroup: 3,
				InstanceToken: 9, State: StateActive,
			},
			Descriptors: map[uint32]ReplacementDescriptor{7: {Group: 3}},
		})
		if !applied {
			t.Fatal("the replacement did not land, so the view proved nothing")
		}
		if len(r.byOwner[ownerKey("d", "anna")]) != 1 {
			t.Fatal("the replacement installed nothing, so the view proved nothing")
		}
		viewMatches(t, r, "d", "anna")
	})
}
