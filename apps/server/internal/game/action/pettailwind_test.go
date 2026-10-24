/*
===========================================================================
pettailwind_test.go - travel timing, real speed/stun publication and party isolation.
===========================================================================
*/
package action

import (
	"fmt"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tailwindFixture / chargeTailwind
================
*/
func tailwindFixture(t *testing.T, tier uint8) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance, petOwnerKey) {
	t.Helper()
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	mob := roarField(t, rt, c, clock)
	learnPetBook(t, c, 54+tier)
	step, ok := rt.elementalCompanion(testDivision, c, c.ActiveCOS.GID, "Crow", clock.NowMs())
	if !ok {
		t.Fatal("crow fixture not eligible")
	}
	rt.advanceTailwind(step.key, clock.NowMs())
	return rt, clock, c, mob, step.key
}
func chargeTailwind(rt *Runtime, clock *fakeClock, key petOwnerKey, count int) {
	for range count {
		clock.now = clock.now.Add(5 * time.Second)
		rt.advanceTailwind(key, clock.NowMs())
	}
}

/*
================
TestTailwindDoesNotRepublishUnchangedRecipients
================
*/
func TestTailwindDoesNotRepublishUnchangedRecipients(t *testing.T) {
	rt, clock, _, _, key := tailwindFixture(t, 3)
	rt.drainMonsterLegRecipients()
	for range 10 {
		rt.advanceTailwind(key, clock.NowMs())
	}
	if frames := rt.drainMonsterLegRecipients(); len(frames) != 0 {
		t.Fatal("unchanged Tailwind flooded recipient queue", len(frames))
	}
}

/*
================
TestTailwindTimingAndDamageConsumeAllTiers
================
*/
func TestTailwindTimingAndDamageConsumeAllTiers(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob, key := tailwindFixture(t, tier)
			gid := enterworld.ObjectIDForCharacter(c)
			clock.now = clock.now.Add(4999 * time.Millisecond)
			rt.advanceTailwind(key, clock.NowMs())
			if rt.tailwindView(testDivision, gid, clock.NowMs()).stacks != 0 {
				t.Fatal("early stack")
			}
			clock.now = clock.now.Add(time.Millisecond)
			rt.advanceTailwind(key, clock.NowMs())
			if got := rt.tailwindView(testDivision, gid, clock.NowMs()).stacks; got != 1 {
				t.Fatal("first stack", got)
			}
			chargeTailwind(rt, clock, key, 12)
			row := rt.tailwindView(testDivision, gid, clock.NowMs())
			if row.stacks != 10 {
				t.Fatal("stack cap", row.stacks)
			}
			rt.playerStruckFrames(testDivision, c, playerStruck{attacker: mob.Gid, impacts: []wire.SkillCastTargetImpact{{Blocked: true}}}, clock.NowMs())
			if rt.tailwindView(testDivision, gid, clock.NowMs()).stacks != 10 {
				t.Fatal("blocked hit consumed charges")
			}
			_, speed := rt.EntryMovementSpeeds(testDivision, c.Name)
			want := float32(simulation.RunSpeed) * (1 + float32(row.percent)/10)
			if math.Abs(float64(speed-want)) > .001 {
				t.Fatalf("movement speed %v want %v", speed, want)
			}
			// The shared incoming-hit publication is used by physical and magical strikes.
			rt.playerStruckFrames(testDivision, c, playerStruck{attacker: mob.Gid, impacts: []wire.SkillCastTargetImpact{{Damage: 1}}}, clock.NowMs())
			live, _ := rt.Monsters.Get(testDivision, mob.Gid)
			stunned := live.Abnormal != nil && live.Abnormal.Slots[abnormal.Stun].Active
			if stunned != (tier >= 2) {
				t.Fatal("tier stun", stunned)
			}
			if rt.tailwindView(testDivision, gid, clock.NowMs()).stacks != 0 {
				t.Fatal("damage did not consume stacks")
			}
			_, speed = rt.EntryMovementSpeeds(testDivision, c.Name)
			if speed != simulation.RunSpeed {
				t.Fatal("speed not restored", speed)
			}
			clock.now = clock.now.Add(4999 * time.Millisecond)
			rt.advanceTailwind(key, clock.NowMs())
			if rt.tailwindView(testDivision, gid, clock.NowMs()).stacks != 0 {
				t.Fatal("damage did not restart timer")
			}
		})
	}
}

/*
================
TestTailwindPartyIsolationRangeAndRetirement
================
*/
func TestTailwindPartyIsolationRangeAndRetirement(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 3)
	member := nearbyCharacter(rt, c, 21, "tailwind-party", 50)
	far := nearbyCharacter(rt, c, 22, "tailwind-far", 201)
	memberGID := enterworld.ObjectIDForCharacter(member)
	rt.petSessions[petOwnerKey{division: testDivision, name: member.Name}] = &petSession{character: member, ready: true}
	rt.petSessions[petOwnerKey{division: testDivision, name: far.Name}] = &petSession{character: far, ready: true}
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), memberGID, enterworld.ObjectIDForCharacter(far)}}}
	}
	rt.advanceTailwind(key, clock.NowMs())
	chargeTailwind(rt, clock, key, 10)
	if rt.tailwindView(testDivision, memberGID, clock.NowMs()).stacks != 10 {
		t.Fatal("party did not charge")
	}
	if rt.tailwindView(testDivision, enterworld.ObjectIDForCharacter(far), clock.NowMs()).book != 0 {
		t.Fatal("far party member received buff")
	}
	frames := rt.tailwindDamage(testDivision, member, mob.Gid, clock.NowMs())
	if len(frames) == 0 {
		t.Fatal("party outgoing hit did not stun")
	}
	if rt.tailwindView(testDivision, memberGID, clock.NowMs()).stacks != 0 || rt.tailwindView(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs()).stacks != 10 {
		t.Fatal("party counters were shared")
	}
	rt.RewardParties = nil
	rt.advanceTailwind(key, clock.NowMs())
	if rt.tailwindView(testDivision, memberGID, clock.NowMs()).book != 0 {
		t.Fatal("party departure retained buff")
	}
	c.ActiveCOS.Summoned = false
	rt.advanceTailwind(key, clock.NowMs())
	if len(rt.tailwinds.sources) != 0 {
		t.Fatal("unsummon retained grants")
	}
	_, speed := rt.EntryMovementSpeeds(testDivision, c.Name)
	if speed != simulation.RunSpeed {
		t.Fatal("unsummon retained speed")
	}
}

/*
================
TestTailwindFlagOffBookChangeAndPartialCharges
================
*/
func TestTailwindFlagOffBookChangeAndPartialCharges(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 2)
	chargeTailwind(rt, clock, key, 9)
	if frames := rt.tailwindDamage(testDivision, c, mob.Gid, clock.NowMs()); len(frames) != 0 {
		t.Fatal("partial charges stunned")
	}
	chargeTailwind(rt, clock, key, 10)
	c.ActiveCOS.Training.Levels[10] = 0
	rt.advanceTailwind(key, clock.NowMs())
	if rt.tailwindView(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs()).stacks != 0 {
		t.Fatal("book change retained charges")
	}
	rt.AttackPetExpansion = false
	rt.advanceTailwind(key, clock.NowMs())
	if len(rt.tailwinds.sources) != 0 {
		t.Fatal("disabled feature retained grant")
	}
}

/*
================
TestTailwindSafeZoneOwnerAndParty
================
*/
func TestTailwindSafeZoneOwnerAndParty(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, _, key := tailwindFixture(t, tier)
			c.World.Spawn.RegionID = testInt64(23687)
			rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
			rt.petSessionFor(testDivision, c.Name, key.gid).follower = simulation.NewPetFollower(key.gid, rt.LiveSpawnFor(testDivision, c))
			member := nearbyCharacter(rt, c, 21, "town-party", 50)
			memberGID := enterworld.ObjectIDForCharacter(member)
			rt.petSessions[petOwnerKey{division: testDivision, name: member.Name}] = &petSession{character: member, ready: true}
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), memberGID}}}
			}
			if !rt.inTown(testDivision, c) || !rt.inTown(testDivision, member) {
				t.Fatal("fixture not in town")
			}
			if _, allowed := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs()); allowed {
				t.Fatal("combat town gate opened")
			}
			rt.advanceTailwind(key, clock.NowMs())
			chargeTailwind(rt, clock, key, 10)
			if got := rt.tailwindView(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs()).stacks; got != 10 {
				t.Fatal("town owner stacks", got)
			}
			if got := rt.tailwindSpeedFactor(testDivision, c, clock.NowMs()); got != 1.5 {
				t.Fatal("town owner speed", got)
			}
			want := uint8(0)
			if tier == 3 {
				want = 10
			}
			if got := rt.tailwindView(testDivision, memberGID, clock.NowMs()).stacks; got != want {
				t.Fatal("town party stacks", got)
			}
			if tier == 3 && rt.tailwindSpeedFactor(testDivision, member, clock.NowMs()) != 1.5 {
				t.Fatal("town party speed")
			}
			rt.AttackPetExpansion = false
			rt.advanceTailwind(key, clock.NowMs())
			if rt.tailwindSpeedFactor(testDivision, c, clock.NowMs()) != 1 || len(rt.tailwinds.sources) != 0 {
				t.Fatal("disabled feature retained town speed")
			}
		})
	}
}

/*
================
TestPetPassiveStateIgnoresSavedLowerTier
================
*/
func TestPetPassiveStateIgnoresSavedLowerTier(t *testing.T) {
	rt, clock, c, _, _ := tailwindFixture(t, 1)
	learnPetBook(t, c, 54)
	c.ActiveCOS.Training.Slotted[0] = 1
	frames := rt.petTrainingStateFrames(c.ActiveCOS, clock.NowMs())
	p := frames[len(frames)-1].Payload
	at := 9
	for range companion.ActiveRowCount {
		at += 7 + int(p[at+6])
	}
	if p[at] != 3 {
		t.Fatal("wire state retained lower passive tier", p[at])
	}
	at += 3
	count := int(p[at])
	at++
	selected := 0
	for range count {
		id, flags := p[at], p[at+4]
		if id >= 52 && id <= 54 && flags&wire.PetSkillSlotted != 0 {
			selected++
			if id != 54 {
				t.Fatal("lower tier highlighted", id)
			}
		}
		at += 9
	}
	if selected != 1 {
		t.Fatal("expected exactly one active tier", selected)
	}
	rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, petTrainingSlot, 52))
	skill, _, ok := companion.Passive(c.ActiveCOS, "Mana Spring", companion.ModifiersFor(c.ActiveCOS))
	if !ok || skill.ID != 54 {
		t.Fatal("old client downgraded passive")
	}
}
