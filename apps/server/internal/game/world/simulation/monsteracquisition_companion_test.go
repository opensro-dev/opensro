/*
===========================================================================

monsteracquisition_companion_test.go - companions weighed with their owner

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
TestAcquisitionWeighsOwnersCompanions

The retail server's nearest-owner-or-companion selector: the nearest of a
candidate player and its strikeable companions is the target; a tie keeps
the player, the fellow band and an ineligible
body status are never chosen, and a companion without its owner among
the candidates is never acquired.
================
*/
func TestAcquisitionWeighsOwnersCompanions(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 1000, Y: 20, Z: 1000}
	at := func(dx float64) Spawn { return Spawn{RegionID: from.RegionID, X: from.X + dx, Y: from.Y, Z: from.Z} }
	owner := playerPose{Gid: 100001, Pose: at(50)}
	pet := func(gid uint32, dx float64, band, status uint8) playerPose {
		return playerPose{Gid: gid, Pose: at(dx), OwnerGid: owner.Gid, Band: band, NativeBodyStatus: status}
	}
	cases := []struct {
		name    string
		players []playerPose
		want    uint32
	}{
		{"nearer pet", []playerPose{owner, pet(7, 20, 3, 0)}, 7},
		{"farther pet", []playerPose{owner, pet(7, 80, 3, 0)}, owner.Gid},
		{"tie keeps the owner", []playerPose{owner, pet(7, 50, 3, 0)}, owner.Gid},
		{"nearest of two", []playerPose{owner, pet(7, 30, 4, 0), pet(8, 20, 6, 0)}, 8},
		{"fellow band", []playerPose{owner, pet(7, 20, fellowCOSBand, 0)}, owner.Gid},
		{"grab pet ignored", []playerPose{owner, pet(7, 10, 4, 0)}, owner.Gid},
		{"untargetable status", []playerPose{owner, pet(7, 20, 3, 3)}, owner.Gid},
		{"orphan pet", []playerPose{pet(7, 20, 3, 0)}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := nearestEligiblePlayer(monster.Instance{}, from, c.players, 100)
			if got.Gid != c.want || ok != (c.want != 0) {
				t.Fatalf("target=%d found=%v want=%d", got.Gid, ok, c.want)
			}
		})
	}
}

/*
================
TestCompanionTargetsJoinTheTick

Companion entries follow their owner with its first-attack guard and
owner identity, and are found by gid for pursuit and attack.
================
*/
func TestCompanionTargetsJoinTheTick(t *testing.T) {
	owner := playerPose{Gid: 100001, Guard: monster.FirstAttackGuard{Mask: 1, Level: 30}}
	players := appendCompanionTargets([]playerPose{owner}, []CompanionTarget{{Gid: 7, Band: 3, BodyRadius: 10}})
	if len(players) != 2 || players[1].OwnerGid != owner.Gid || players[1].Guard != owner.Guard || players[1].Band != 3 {
		t.Fatalf("entries %+v", players)
	}
	if target, ok := eligiblePlayerByGid(monster.Instance{}, players, 7); !ok || target.OwnerGid != owner.Gid {
		t.Fatal("a companion target was not found by gid")
	}
}

/*
================
TestAggressiveMonsterChasesTheNearerPet

The tick's companion provider puts a pet beside its owner; an aggressive
monster acquires the nearer pet, chases it and strikes its gid.
================
*/
func TestAggressiveMonsterChasesTheNearerPet(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	const petGID = 0x02000007
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(50), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	var struck uint32
	ops.BasicAttack = func(_ string, _ monster.Instance, target, _ uint32, _ int64) MonsterAttackResult {
		struck = target
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	player := playerSessionAt(1, 1080, 1000)
	player.CombatEligible = true
	ops.Companions = func(_ string, owner uint32, _ int64) []CompanionTarget {
		if owner != PlayerObjectID(1) {
			return nil
		}
		return []CompanionTarget{{Gid: petGID, Band: 3, BodyRadius: 5,
			Pose: Spawn{RegionID: player.World.Spawn.RegionID, X: 1020, Y: player.World.Spawn.Y, Z: 1000}}}
	}
	push := &fakePusher{}
	ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
	ops.RunMonsterLeg(t0+1500, []SessionSnapshot{player}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.TargetGID() != petGID {
		t.Fatalf("target %d, want the pet", mover.TargetGID())
	}
	if struck != petGID {
		t.Fatalf("struck %d, want the pet", struck)
	}
}

/*
================
TestDungeonAcquisitionRunsSelector2

The retail server's candidate collection, entity gather, dungeon query
and nearest-selector chain: dungeon regions use the same selector dispatch
as outdoor regions, weighing companions with their owner, enforcing Fear's
source exclusion and first-attack protection, and ranking by 3D distance.
================
*/
func TestDungeonAcquisitionRunsSelector2(t *testing.T) {
	const dungeonRegion = uint16(0x8001)
	from := monster.Pose{RegionID: dungeonRegion, X: 100, Y: 10, Z: 100}
	at := func(dx, dy float64) Spawn {
		return Spawn{RegionID: dungeonRegion, X: from.X + dx, Y: from.Y + dy, Z: from.Z}
	}
	regular := monster.Instance{Ref: monster.MonsterRef{TidWord: 0x00C6, TypeID4: 1, Level: 20, MonsterType: 0}}
	owner := playerPose{Gid: 100001, Pose: at(50, 0)}
	pet := playerPose{Gid: 7, Pose: at(20, 0), OwnerGid: owner.Gid, Band: 3}

	if got, ok := nearestEligiblePlayer(regular, from, []playerPose{owner, pet}, 100); !ok || got.Gid != pet.Gid {
		t.Fatalf("dungeon nearer companion: got=%+v ok=%v, want pet %d", got, ok, pet.Gid)
	}

	fearedOfOwner := regular
	fearedOfOwner.Abnormal = &abnormal.Block{Mask: abnormal.Fear.Bit()}
	fearedOfOwner.Abnormal.Slots[abnormal.Fear].Active = true
	fearedOfOwner.Abnormal.Slots[abnormal.Fear].SourceGID = owner.Gid
	other := playerPose{Gid: 100002, Pose: at(70, 0)}
	if got, ok := nearestEligiblePlayer(fearedOfOwner, from, []playerPose{owner, pet, other}, 100); !ok || got.Gid != other.Gid {
		t.Fatalf("dungeon Fear of owner: got=%+v ok=%v, want other %d", got, ok, other.Gid)
	}

	fearedOfPet := regular
	fearedOfPet.Abnormal = &abnormal.Block{Mask: abnormal.Fear.Bit()}
	fearedOfPet.Abnormal.Slots[abnormal.Fear].Active = true
	fearedOfPet.Abnormal.Slots[abnormal.Fear].SourceGID = pet.Gid
	if got, ok := nearestEligiblePlayer(fearedOfPet, from, []playerPose{owner, pet}, 100); !ok || got.Gid != owner.Gid {
		t.Fatalf("dungeon Fear of pet: got=%+v ok=%v, want owner %d", got, ok, owner.Gid)
	}

	guardedOwner := owner
	guardedOwner.Guard = monster.FirstAttackGuard{Mask: 0x7, Level: 25}
	guardedPet := pet
	guardedPet.Guard = guardedOwner.Guard
	if got, ok := nearestEligiblePlayer(regular, from, []playerPose{guardedOwner, guardedPet}, 100); ok {
		t.Fatalf("dungeon Noise-guarded owner and companion should be skipped, got=%+v", got)
	}

	highPlayer := playerPose{Gid: 100003, Pose: at(10, 200)}
	if got, ok := nearestEligiblePlayer(regular, from, []playerPose{highPlayer}, 100); ok {
		t.Fatalf("dungeon 3D distance out of sight should be rejected, got=%+v", got)
	}
}

/*
================
TestMountedOwnerRedirectsCompanionTargetToOwner

The retail server's tactics acquisition selectors, its fixed-query
fallback and its idle wander: when the selector or parent-opponent
fallback chooses a COS whose owner answers the server's mounted check
== 1, the combat target is swapped to the owner.
================
*/
func TestMountedOwnerRedirectsCompanionTargetToOwner(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 1000, Y: 20, Z: 1000}
	at := func(dx float64) Spawn { return Spawn{RegionID: from.RegionID, X: from.X + dx, Y: from.Y, Z: from.Z} }
	owner := playerPose{Gid: 100001, Pose: at(60), Mounted: true, JobState: jobStateTrader}
	pet := playerPose{Gid: 7, Pose: at(20), OwnerGid: owner.Gid, Band: 3}

	if got, ok := nearestEligiblePlayer(monster.Instance{}, from, []playerPose{owner, pet}, 100); !ok || got.Gid != owner.Gid {
		t.Fatalf("Selector 2 mounted redirect: got=%+v ok=%v, want owner %d", got, ok, owner.Gid)
	}

	thiefHunter := monster.Instance{}
	thiefHunter.Nest.NativeTacticsFlags = 0x4
	if got, ok := nearestEligiblePlayer(thiefHunter, from, []playerPose{owner, pet}, 100); !ok || got.Gid != owner.Gid {
		t.Fatalf("Selector 3 mounted redirect: got=%+v ok=%v, want owner %d", got, ok, owner.Gid)
	}

	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok {
		t.Fatal("wave refused")
	}
	child := children[0]
	ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0 }, TacticsFor: fixedTactics(passiveTactics())}
	if !s.ArmRetaliation("summon", parent.Gid, pet.Gid) {
		t.Fatal("leader target refused")
	}
	mover, _ := s.Mover("summon", child.Gid)
	players := []playerPose{
		{Gid: owner.Gid, Pose: Spawn{RegionID: parent.Spawn.RegionID, X: 1000, Z: 100}, Mounted: true},
		{Gid: pet.Gid, Pose: Spawn{RegionID: parent.Spawn.RegionID, X: 950, Z: 100}, OwnerGid: owner.Gid, Band: 3},
	}
	if got, ok := ops.summonedCandidate("summon", child, mover, players, *now); !ok || got.Gid != owner.Gid {
		t.Fatalf("summonedCandidate parent-opponent mounted redirect: got=%+v ok=%v, want owner %d", got, ok, owner.Gid)
	}
}

/*
================
TestAggressiveMonsterSwapsNearerPetToMountedOwner

When a session has a mounted COS, an aggressive monster whose
selector picks the session's nearer companion swaps its combat target to
the mounted owner before its target is set (the retail server's tactics
acquisition selectors).
================
*/
func TestAggressiveMonsterSwapsNearerPetToMountedOwner(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	const petGID = 0x02000007
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	player := playerSessionAt(1, 1060, 1000)
	player.CombatEligible = true
	player.COS = &PeerCOS{Mounted: true}
	ops.Companions = func(_ string, owner uint32, _ int64) []CompanionTarget {
		if owner != PlayerObjectID(1) {
			return nil
		}
		return []CompanionTarget{{Gid: petGID, Band: 3, BodyRadius: 5,
			Pose: Spawn{RegionID: player.World.Spawn.RegionID, X: 1020, Y: player.World.Spawn.Y, Z: 1000}}}
	}
	push := &fakePusher{}
	ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("target %d, want mounted owner %d", mover.TargetGID(), PlayerObjectID(1))
	}
}
