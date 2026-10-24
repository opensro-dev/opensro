/*
===========================================================================

caravanbandit_lifecycle_test.go - a caravan bandit's working life

Pins the trader thread's item 8 against the native job-monster rule: a
bandit engages the caravan it ambushed, stays while it is landing hits,
and leaves 20 s after its last hit when it can no longer reach the
caravan. Leaving is the native idle timer (monsterlifetime.go), not a
distance cull: an outrun thief mid-chase goes on the same deadline as an
idle one.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// caravanBanditWorld builds the ambush by the production seams: the
// bandit reference the tables pick (MOB_THIEF_NPC at run speed 50, the
// shipped row's own speed), tactics row 2004, the vehicle standing on a
// battlefield, and a mover ops with an honest attack plan.
/*
================
caravanBanditWorld
================
*/
func caravanBanditWorld(t *testing.T, t0 int64) (*MonsterState, monster.Instance, *MonsterMoverOps, *int, *int64) {
	t.Helper()
	ref := monster.MonsterRef{RefObjID: 40001, Codename: "MOB_THIEF_NPC_0020", TidWord: 0x00c6, TypeID4: 2,
		Level: 20, MaxHP: 200, BodyRadius: 5, WalkSpeed: 20, RunSpeed: 50}
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	state.SetRandomSource(func() float64 { return 0.5 })
	state.StartDivision(monsterTestDivision)
	state.AdvancePopulation(t0)
	lease, exists := state.PopulationLease(monsterTestDivision, instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	tactics, ok := monster.CaravanTactics(2004)
	if !ok {
		t.Fatal("thief tactics row 2004 absent")
	}
	vehicle := Spawn{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	if !state.SpawnCaravanBandit(CaravanBanditSpawn{Division: monsterTestDivision, Population: lease, Ref: ref,
		Vehicle: vehicle, Tactics: tactics, NowMs: t0}) {
		t.Fatal("bandit admission failed")
	}
	bandit := state.MaterializedInstances(monsterTestDivision)[0]
	attacks, lastHit := new(int), new(int64)
	ops := &MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0.5 }}
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(_ string, _ monster.Instance, _, _ uint32, nowMs int64) MonsterAttackResult {
		*attacks++
		*lastHit = nowMs
		// The action owner rearms the job timer on an accepted hit.
		state.RefreshJobMonster(monsterTestDivision, bandit.Gid, nowMs)
		return MonsterAttackResult{Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: true, TargetAlive: true}
	}
	return state, bandit, ops, attacks, lastHit
}

// caravanTraderAt is the suited trader on his loaded transport, riding
// +X from the ambush point at speed units per second.
/*
================
caravanTraderAt
================
*/
func caravanTraderAt(speed, elapsedSeconds float64) SessionSnapshot {
	session := playerSessionAt(1, 1000+speed*elapsedSeconds, 1000)
	session.Appearance = &PeerAppearance{JobType: 1}
	session.COS = &PeerCOS{Row: wire.CosSpawnBand2{Gid: 200001}, Mounted: true}
	return session
}

/*
================
TestCaravanBanditStaysWhileItFights

The ambush works: the bandit runs the riding trader down, hits land,
and each hit rearms the job timer - the bandit is still in the world
25 s after the ambush even though it spawned under a 20 s clock.
================
*/
func TestCaravanBanditStaysWhileItFights(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	state, bandit, ops, attacks, _ := caravanBanditWorld(t, t0)
	push := &fakePusher{}
	for step := int64(0); step <= 250; step++ {
		now := t0 + step*100
		ops.RunMonsterLeg(now, []SessionSnapshot{caravanTraderAt(20, float64(step)/10)}, push)
		state.ExpireMonsterLifetimes(now)
		if _, alive := state.Mover(monsterTestDivision, bandit.Gid); !alive {
			t.Fatalf("bandit left at %d ms while fighting (attacks=%d)", step*100, *attacks)
		}
	}
	if *attacks == 0 {
		t.Fatal("bandit never landed a hit on the riding trader")
	}
	mover, _ := state.Mover(monsterTestDivision, bandit.Gid)
	if mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("bandit target = %d after 25 s of fighting", mover.TargetGID())
	}
}

/*
================
TestCaravanBanditLeavesTwentySecondsAfterItsLastHit

Outrun, the same thief leaves on the idle clock: once the hits stop,
the 20 s runs from the LAST accepted hit, not from the spawn - the
caravan that keeps fighting keeps its thieves.
================
*/
func TestCaravanBanditLeavesTwentySecondsAfterItsLastHit(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	state, bandit, ops, attacks, lastHit := caravanBanditWorld(t, t0)
	push := &fakePusher{}
	// Phase 1: the trader rides at 20 for 30 s; the bandit fights.
	for step := int64(0); step <= 300; step++ {
		now := t0 + step*100
		ops.RunMonsterLeg(now, []SessionSnapshot{caravanTraderAt(20, float64(step)/10)}, push)
		state.ExpireMonsterLifetimes(now)
	}
	if *attacks == 0 || *lastHit <= t0 {
		t.Fatalf("fighting phase landed no hits (attacks=%d)", *attacks)
	}
	// Phase 2: the trader rides off at 70, beyond the bandit's run 50.
	finalHit := *lastHit
	leftAt := int64(-1)
	base := finalHit
	for now := base + 100; now <= base+jobMonsterIdleMs+2000; now += 100 {
		session := playerSessionAt(1, 1600+70*float64(now-base)/1000, 1000)
		session.Appearance = &PeerAppearance{JobType: 1}
		session.COS = &PeerCOS{Row: wire.CosSpawnBand2{Gid: 200001}, Mounted: true}
		ops.RunMonsterLeg(now, []SessionSnapshot{session}, push)
		state.ExpireMonsterLifetimes(now)
		if *lastHit != finalHit {
			finalHit = *lastHit
			base = finalHit
			continue
		}
		if _, alive := state.Mover(monsterTestDivision, bandit.Gid); !alive {
			leftAt = now
			break
		}
		if now < finalHit+jobMonsterIdleMs {
			continue
		}
		t.Fatalf("bandit still in the world %d ms after its last hit", now-finalHit)
	}
	if leftAt < 0 {
		t.Fatal("bandit never left")
	}
	if leftAt < finalHit+jobMonsterIdleMs || leftAt > finalHit+jobMonsterIdleMs+200 {
		t.Fatalf("bandit left %d ms after its last hit, want the 20 s idle deadline", leftAt-finalHit)
	}
}
