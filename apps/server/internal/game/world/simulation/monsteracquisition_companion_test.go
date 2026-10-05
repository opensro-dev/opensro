/*
===========================================================================

monsteracquisition_companion_test.go - companions weighed with their owner

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestAcquisitionWeighsOwnersCompanions

5464E0: the nearest of a candidate player and its strikeable companions
is the target; a tie keeps the player, the fellow band and an ineligible
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
