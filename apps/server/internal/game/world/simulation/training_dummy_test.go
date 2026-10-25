/*
===========================================================================
training_dummy_test.go - damageable GM targets never run AI or get displaced.
===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestTrainingDummyCombatAndAI
================
*/
func TestTrainingDummyCombatAndAI(t *testing.T) {
	s, lease := gmMonsterState(t, 0)
	ref := s.template.Refs[1]
	ref.RefObjID, ref.TrainingDummy, ref.MaxHP = 5000121, true, 100000000
	s.template.Refs[ref.RefObjID] = ref
	at := Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}
	ids := s.SpawnGMMonstersTracked(GMMonsterSpawn{Division: "gm", Population: lease, RefObjID: ref.RefObjID, Count: 1, Position: at, NowMs: 1000})
	if len(ids) != 1 {
		t.Fatal("GM dummy did not spawn")
	}
	row, _ := s.Get("gm", ids[0])
	move := &MonsterKnockdownPlan{Pose: monster.Pose{RegionID: at.RegionID, X: at.X + 50, Y: at.Y, Z: at.Z}, UntilMs: 2000}
	hit := s.ApplyDamageSequence("gm", row.Gid, row.CurrentHP, []MonsterDamagePlan{{GID: row.Gid, Damage: 123, Knockback: move}})
	if len(hit) != 1 || hit[0].CurrentHP != 99999877 || hit[0].Knockback != nil || hit[0].Fatal {
		t.Fatal("damage or displacement", hit)
	}
	ops := &MonsterMoverOps{Monsters: s}
	for _, now := range []int64{2000, 60000, 600000} {
		frames, private := ops.advanceInstance("gm", hit[0].Instance, nil, now)
		if len(frames) != 0 || len(private) != 0 {
			t.Fatal("dummy ran AI", frames, private)
		}
	}
	mover, _ := s.Mover("gm", row.Gid)
	if pose := mover.LivePoseAt(600000, nil); pose.X != at.X || pose.Z != at.Z {
		t.Fatal("dummy moved", pose)
	}
	s.SetAbnormalContext(testAbnormalContext{})
	frozen := applyRecords(t, s, "gm", row.Gid, 9, 1, abnormal.Record{Status: abnormal.Freeze, Grade: 3, DurationMs: 3000, SourceGID: 9})
	if frozen.Instance.Abnormal == nil || !frozen.Instance.Abnormal.Has(abnormal.Freeze) {
		t.Fatal("dummy rejected a status")
	}
}
