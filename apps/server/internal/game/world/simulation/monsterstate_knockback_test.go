package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"time"
)

func TestKnockbackHoldsMoverAndPublishesCurrentSpawnMotion(t *testing.T) {
	registry := damageTestState()
	now := int64(1000)
	registry.clock = func() time.Time { return time.UnixMilli(now) }
	instance := firstDamageTestMonster(t, registry, "damage")
	stale, _ := registry.Mover("damage", instance.Gid)
	pose := monster.Pose{RegionID: instance.Spawn.RegionID, X: 120, Y: 20, Z: 100}
	results := registry.ApplyDamageSequence("damage", instance.Gid, instance.CurrentHP, []MonsterDamagePlan{{GID: instance.Gid, Damage: 1, Knockback: &MonsterKnockdownPlan{Pose: pose, UntilMs: 3000}}})
	if len(results) != 1 || registry.CommitMover("damage", instance.Gid, stale) {
		t.Fatal("knockback did not supersede the old movement plan")
	}
	current, _ := registry.Mover("damage", instance.Gid)
	if current.Pose != pose || registry.CommitMover("damage", instance.Gid, current) {
		t.Fatal("movement admitted during knockback")
	}
	for _, tc := range []struct {
		at     int64
		motion byte
	}{{2999, 16}, {3000, 0}} {
		now = tc.at
		live, _ := registry.Get("damage", instance.Gid)
		row := BuildMonsterCreateRow(monsterWireDefFromInstance(live, now), instance.Gid, Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
		if row[30] != tc.motion {
			t.Fatalf("spawn at %d: motion %d, want %d", now, row[30], tc.motion)
		}
	}
	if registry.CommitMover("damage", instance.Gid, stale) || !registry.CommitMover("damage", instance.Gid, current) {
		t.Fatal("recovery must admit the current mover while rejecting the displaced plan")
	}
}

/*
================
TestKnockbackWalksThroughTheMoveTest

CGObj_MoveTo (485740) moves a displaced victim through the region
manager's move query: a blocked result leaves it where it stood, a clipped
one stops it at the point the query wrote, and an open one lands it on the
push.
================
*/
func TestKnockbackWalksThroughTheMoveTest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result uint32
		rest   float64
		wantX  func(live, push float64) float64
	}{
		{"open", 0, 0, func(_, push float64) float64 { return push }},
		{"clipped", monster.NavResultClipped, 110, func(_, _ float64) float64 { return 110 }},
		{"blocked", monster.NavResultBlocked, 0, func(live, _ float64) float64 { return live }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := damageTestState()
			registry.clock = func() time.Time { return time.UnixMilli(1000) }
			instance := firstDamageTestMonster(t, registry, "damage")
			before, _ := registry.Mover("damage", instance.Gid)
			live := before.LivePoseAt(1000, nil)
			registry.SetSpawnCollisionTest(func(from, to Spawn) MonsterSpawnMove {
				rest := to
				if tc.result == monster.NavResultClipped {
					rest.X = tc.rest
				}
				if tc.result == monster.NavResultBlocked {
					rest = from
				}
				return MonsterSpawnMove{Result: tc.result, Rest: rest}
			})
			push := monster.Pose{RegionID: live.RegionID, X: live.X + 30, Y: live.Y, Z: live.Z}
			results := registry.ApplyDamageSequence("damage", instance.Gid, instance.CurrentHP, []MonsterDamagePlan{{GID: instance.Gid, Damage: 1, Knockback: &MonsterKnockdownPlan{Pose: push, UntilMs: 3000}}})
			if len(results) != 1 || results[0].Knockback == nil {
				t.Fatalf("no knockback committed: %+v", results)
			}
			want := tc.wantX(live.X, push.X)
			current, _ := registry.Mover("damage", instance.Gid)
			if current.Pose.X != want || results[0].Knockback.Pose.X != want {
				t.Fatalf("landed at %.1f (published %.1f), want %.1f", current.Pose.X, results[0].Knockback.Pose.X, want)
			}
		})
	}
}
