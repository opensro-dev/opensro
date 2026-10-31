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
}
