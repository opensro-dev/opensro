/*
===========================================================================

tomb_chain_retaliation_test.go - stage uniques keep their nest's contract

Regression coverage for #2600: the Floor 6 chain anchors spawn at
stage points, and before the fix those instances carried a nest row
with no AI fields at all, so their tactics resolved empty and even
lethal damage never drew a chase or a strike. Each stage unique must
retaliate exactly the way its authored nest spawn does.

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
tombChainTestWorld

The tomb template over a one-ref base (WithQinShiTomb leaves an
empty base untouched), chain installed, world opened: the chain
uniques are live before the first test leg runs.
================
*/
func tombChainTestWorld(t *testing.T) (*MonsterState, time.Time) {
	t.Helper()
	base := monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 59},
	}, nil)
	withTomb, err := base.WithQinShiTomb("")
	if err != nil {
		t.Fatalf("WithQinShiTomb: %v", err)
	}
	s := NewMonsterState(withTomb)
	base0 := time.UnixMilli(1_784_100_000_000)
	now := base0
	s.SetTimeSource(func() time.Time { return now })
	s.SetRandomSource(constantWord(16384))
	if err := s.InstallTombChain(); err != nil {
		t.Fatalf("InstallTombChain: %v", err)
	}
	s.StartDivision("division")
	for step := 1; step <= 5; step++ {
		s.AdvancePopulation(base0.Add(time.Duration(step) * time.Second).UnixMilli())
	}
	return s, base0
}

/*
================
tombChainFindUnique

The chain's live instance of one codename on Floor 6.
================
*/
func tombChainFindUnique(t *testing.T, s *MonsterState, codename string) monster.Instance {
	t.Helper()
	for _, in := range s.InstancesInRegions("division", []uint16{0x8002}) {
		if in.Ref.Codename == codename {
			return in
		}
	}
	t.Fatalf("no live %s on Floor 6", codename)
	return monster.Instance{}
}

/*
================
TestTombChainFloor6UniquesRetaliate

Damage each Floor 6 stage unique through the production hostility
path and run mover legs with the attacker beside it: the unique
must strike back. The Black Snake case stands the attacker off, so
the snake has to close the distance (chase) before it can strike -
the nest-spawn contract end to end.
================
*/
func TestTombChainFloor6UniquesRetaliate(t *testing.T) {
	cases := []struct {
		name      string
		codename  string
		offset    float64
		wantChase bool
	}{
		{"white snake strikes from its stage point", "MOB_TQ_WHITESNAKE", 5, false},
		{"black snake strikes from its stage point", "MOB_TQ_BLACKSNAKE", 5, false},
		{"black snake chases a distant attacker", "MOB_TQ_BLACKSNAKE", 40, true},
		{"snake general strikes from its stage point", "MOB_TQ_SNAKEGENERAL", 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, base0 := tombChainTestWorld(t)
			inst := tombChainFindUnique(t, s, tc.codename)

			attackCalls := 0
			ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0.5 }}
			ops.AttackPlan = func(got monster.Instance, requested uint32, _ AttackPick) (MonsterAttackPlan, bool) {
				return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
			}
			ops.BasicAttack = func(divisionID string, got monster.Instance, targetGid, gotSkillID uint32, _ int64) MonsterAttackResult {
				if got.Gid == inst.Gid {
					attackCalls++
				}
				return MonsterAttackResult{
					Frames:      []Frame{{Opcode: wire.OpSkillCastResult}},
					Accepted:    true,
					TargetAlive: true,
				}
			}

			nowMs := base0.Add(6 * time.Second).UnixMilli()
			if _, ok := s.ApplyDamage("division", inst.Gid, 50); !ok {
				t.Fatalf("ApplyDamage(%s) refused", tc.codename)
			}
			candidates := [3]monster.OpponentCandidate{{
				GID: PlayerObjectID(1), Eligible: true, Distance: tc.offset, ActorDistance: float32(tc.offset),
			}}
			if !s.RecordHostility("division", inst.Gid, PlayerObjectID(1), 50, 50, candidates, nowMs) {
				t.Fatalf("RecordHostility(%s) refused", tc.codename)
			}

			pose := inst.Spawn
			session := SessionSnapshot{
				SessionID:      "viewer",
				DivisionID:     "division",
				CharacterID:    1,
				WorldInstance:  0x10001,
				Population:     instance.Lease{ID: instance.Pack(1, 1), Generation: 1},
				CombatEligible: true,
				World: WorldState{Spawn: Spawn{
					RegionID: pose.RegionID, X: pose.X + tc.offset, Y: pose.Y, Z: pose.Z,
				}, SpawnSet: true},
				BodyRadius: BodyRadius(4),
			}
			push := &fakePusher{}
			recordTestMonsterBootstrap(ops, []SessionSnapshot{session}, nowMs)
			sawChase, sawAttacking := false, false
			for i := 0; i < 40; i++ {
				ops.RunMonsterLeg(nowMs+int64(i)*500, []SessionSnapshot{session}, push)
				mover, ok := s.Mover("division", inst.Gid)
				if !ok {
					break
				}
				sawChase = sawChase || mover.Mode() == monster.MoverChasing
				sawAttacking = sawAttacking || mover.Mode() == monster.MoverAttacking
				if attackCalls > 0 {
					break
				}
			}
			if attackCalls == 0 {
				t.Fatalf("%s: damaged stage unique never attacked back", tc.codename)
			}
			if !sawAttacking {
				t.Fatalf("%s: attacked without entering the attacking mode", tc.codename)
			}
			if tc.wantChase && !sawChase {
				t.Fatalf("%s: never chased the distant attacker", tc.codename)
			}
		})
	}
}

/*
================
TestTombChainSpawnerNestDonatesFields

The point-spawner row itself: an anchor whose codename carries an
authored nest in the region spawns with that nest's AI fields at
the stage point; a ref-only point spawner falls back to the pinned
quest-spawn shape rather than a contract-less row.
================
*/
func TestTombChainSpawnerNestDonatesFields(t *testing.T) {
	s, _ := tombChainTestWorld(t)

	var snakeDef *TombSpawnerDef
	for _, floor := range TombChainDefaultDef().Floors {
		for i := range floor.Spawners {
			if floor.Spawners[i].Codename == "MOB_TQ_WHITESNAKE" {
				snakeDef = &floor.Spawners[i]
			}
		}
	}
	if snakeDef == nil {
		t.Fatal("no MOB_TQ_WHITESNAKE spawner in the default chain")
	}
	nest, err := s.tombChainSpawnerNest(*snakeDef)
	if err != nil {
		t.Fatalf("tombChainSpawnerNest(F6_WHITE_SNAKE): %v", err)
	}
	if !nest.HasControls || !nest.PolicyPinned {
		t.Fatalf("snake spawner nest = controls %v pinned %v, want the authored Floor 6 nest", nest.HasControls, nest.PolicyPinned)
	}
	if nest.RegionID != 0x8002 || nest.X != snakeDef.X || nest.Z != snakeDef.Z {
		t.Fatalf("snake spawner nest point = (%#x, %v, %v), want the stage point (%v, %v)",
			nest.RegionID, nest.X, nest.Z, snakeDef.X, snakeDef.Z)
	}

	refOnly, err := s.tombChainSpawnerNest(TombSpawnerDef{
		Name: "TEST_REF_ONLY", Codename: "MOB_CH_MANGNYANG", Region: 0x8002, X: 10, Y: 132, Z: 20,
	})
	if err != nil {
		t.Fatalf("tombChainSpawnerNest(ref-only): %v", err)
	}
	if !refOnly.PolicyPinned || refOnly.HasControls {
		t.Fatalf("ref-only nest = pinned %v controls %v, want the pinned point shape", refOnly.PolicyPinned, refOnly.HasControls)
	}
}
