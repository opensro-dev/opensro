package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestMonsterAttackTransitionFacesLiveTargetBeforeB245(t *testing.T) {
	const nowMs = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{
			Frames:      []Frame{{Opcode: wire.OpSkillCastResult}},
			Accepted:    true,
			TargetAlive: true,
		}
	}
	push := &fakePusher{}
	recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, 1005, 1000)}, nowMs)
	ops.RunMonsterLeg(nowMs, []SessionSnapshot{playerSessionAt(1, 1005, 1000)}, push)
	frames := monsterFrames(push)
	if len(frames) != 2 || frames[0].Opcode != wire.OpObjectSourceCorrection ||
		frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("attack transition frames = %+v, want facing correction before B245", frames)
	}
	correction, err := wire.DecodeObjectSourceCorrection(frames[0].Payload)
	if err != nil {
		t.Fatalf("decode attack-facing correction: %v", err)
	}
	want := headingWordFromDelta(5, 0)
	if correction.Heading != want {
		t.Fatalf("attack-facing heading = %#04x, want live target bearing %#04x", correction.Heading, want)
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Pose.Heading != want {
		t.Fatalf("authoritative mover heading = %#04x, want committed %#04x", mover.Pose.Heading, want)
	}
}

func TestMonsterAttackTransitionUsesRegionAwareTargetBearing(t *testing.T) {
	const nowMs = int64(1_784_000_000_000)
	tactics := aggressiveTactics()
	tactics.ChaseLeash = 2000
	ops, instance := monsterLegFixture(t, tactics)
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(10), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: true, TargetAlive: true}
	}

	// The target is five world units east across the adjacent-region seam.
	from, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	from.Pose.X = NativeRegionSize - 2
	if !ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, from) {
		t.Fatal("commit cross-region attacker pose")
	}
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
		t.Fatal("arm cross-region retaliation target")
	}
	player := playerSessionAt(1, 3, 1000)
	player.World.Spawn.RegionID = RegionIDForSectors(SectorX(instance.Spawn.RegionID)+1, SectorY(instance.Spawn.RegionID))
	want, ok := HeadingFromMovement(poseToSpawn(from.Pose), player.World.Spawn)
	if !ok {
		t.Fatal("cross-region target bearing unexpectedly degenerate")
	}
	push := &fakePusher{}
	recordTestMonsterBootstrap(ops, []SessionSnapshot{player}, nowMs)
	ops.RunMonsterLeg(nowMs, []SessionSnapshot{player}, push)
	frames := monsterFrames(push)
	if len(frames) != 2 || frames[0].Opcode != wire.OpObjectSourceCorrection || frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("cross-region attack frames = %+v, want correction before B245", frames)
	}
	correction, err := wire.DecodeObjectSourceCorrection(frames[0].Payload)
	if err != nil {
		t.Fatalf("decode cross-region correction: %v", err)
	}
	if correction.Heading != want {
		t.Fatalf("cross-region attack heading = %#04x, want %#04x", correction.Heading, want)
	}
}
