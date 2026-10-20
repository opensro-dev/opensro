/*
===========================================================================
petbond_test.go - Guardian recovery, tier gates, shared budget and admission
===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
TestGuardianRecoveryRanksAndSavedCooldown
================
*/
func TestGuardianRecoveryRanksAndSavedCooldown(t *testing.T) {
	for _, rank := range []uint8{1, 5, 10} {
		t.Run(string(rune('0'+rank)), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			roarField(t, rt, c, clock)
			c.MissionInventory = nil
			rt.AttackPetExpansion = true
			skill := learnPetBook(t, c, 25)
			c.ActiveCOS.Training.Levels[skill.Index()] = rank
			wantRank, cooldown := companion.GuardianRevival(c.ActiveCOS)
			if wantRank != rank || cooldown != 600000-int64(rank-1)*300000/9 {
				t.Fatal("recovery scaling", wantRank, cooldown)
			}
			stats, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
			c.DeathKind = uint8(pk.DeathMonster)
			c.CurrentHP = testInt64(0)
			c.LastExpLoss = 1003
			var refunded int64
			rt.RefundExperience = func(_ *enterworld.Character, exp int64) ([]wire.Frame, bool) { refunded += exp; return nil, true }
			key := petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}
			state := rt.petSessionFor(key.division, key.name, key.gid)
			frames := rt.advancePetBond(key, state, clock.NowMs())
			if len(frames) == 0 || *c.CurrentHP != maxHP*int64(rank)/10 || refunded != 1003*int64(rank)/10 || c.LastExpLoss != 0 {
				t.Fatalf("revival hp=%d/%d refund=%d frames=%d", *c.CurrentHP, maxHP, refunded, len(frames))
			}
			ready := c.ActiveCOS.Training.PassiveReadyAtMs[skill.Row-companion.ActiveRowCount]
			if ready != clock.NowMs()+cooldown {
				t.Fatal("saved cooldown", ready)
			}
			// New transient combat memory must not reset the saved revival timer.
			state.bond = petBondRuntime{}
			c.DeathKind = uint8(pk.DeathMonster)
			c.CurrentHP = testInt64(0)
			if len(rt.advancePetBond(key, state, ready-1)) != 0 || *c.CurrentHP != 0 {
				t.Fatal("cooldown bypass")
			}
			if len(rt.advancePetBond(key, state, ready)) == 0 || *c.CurrentHP == 0 {
				t.Fatal("cooldown boundary")
			}
		})
	}
}

/*
================
TestGuardianSharingTiersBudgetAndParty
================
*/
func TestGuardianSharingTiersBudgetAndParty(t *testing.T) {
	for _, tier := range []uint8{1, 2, 3} {
		t.Run(string(rune('0'+tier)), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			roarField(t, rt, c, clock)
			c.MissionInventory = nil
			rt.AttackPetExpansion = true
			skill := learnPetBook(t, c, 24+tier)
			c.ActiveCOS.Training.Levels[skill.Index()] = 10
			near := nearbyCharacter(rt, c, 21, "bond-near", 50)
			near.ActiveCOS = nil
			rt.BindPetSession(testDivision, near, 2)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
			}
			plan := rt.planPetBond(testDivision, c, clock.NowMs())
			if tier == 1 {
				if plan != nil {
					t.Fatal("tier I transferred damage")
				}
				return
			}
			if plan == nil || plan.percent != float64(tier)*10 {
				t.Fatal("missing or wrong transfer", plan)
			}
			oldHP := c.ActiveCOS.CurrentHP
			damage := sharePetBond(plan, 100)
			if damage != 100-uint32(tier)*10 {
				t.Fatal("transfer percent", damage)
			}
			if len(rt.commitPetBond(plan)) != 2 || c.ActiveCOS.CurrentHP != oldHP-(100-damage) {
				t.Fatal("bear did not pay transfer")
			}
			if len(rt.commitPetBond(plan)) != 0 {
				t.Fatal("duplicate debit")
			}
			partyPlan := rt.planPetBond(testDivision, near, clock.NowMs())
			if (partyPlan != nil) != (tier == 3) {
				t.Fatal("party tier gate")
			}
			plan = rt.planPetBond(testDivision, c, clock.NowMs())
			if plan == nil {
				t.Fatal("budget disappeared")
			}
			sharePetBond(plan, 1000000)
			sharePetBond(plan, 1000000)
			if plan.moved != plan.budget {
				t.Fatal("shared budget not bounded")
			}
			rt.commitPetBond(plan)
			if rt.planPetBond(testDivision, c, clock.NowMs()) != nil {
				t.Fatal("per-second cap bypass")
			}
			if rt.planPetBond(testDivision, c, clock.NowMs()+1000) == nil {
				t.Fatal("budget not replenished")
			}
			c.ActiveCOS.CurrentHP = 2
			plan = rt.planPetBond(testDivision, c, clock.NowMs()+2000)
			sharePetBond(plan, 1000000)
			rt.commitPetBond(plan)
			if c.ActiveCOS.CurrentHP != 1 {
				t.Fatal("transfer killed bear")
			}
			rt.AttackPetExpansion = false
			if rt.planPetBond(testDivision, c, clock.NowMs()+3000) != nil {
				t.Fatal("flag-off transfer")
			}
		})
	}
}

/*
================
TestGuardianPartyRevivalAndLevelLimit
================
*/
func TestGuardianPartyRevivalAndLevelLimit(t *testing.T) {
	for _, tier := range []uint8{1, 2, 3} {
		t.Run(string(rune('0'+tier)), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			roarField(t, rt, c, clock)
			c.MissionInventory = nil
			rt.AttackPetExpansion = true
			learnPetBook(t, c, 24+tier)
			near := nearbyCharacter(rt, c, 21, "bond-near", 50)
			near.ActiveCOS = nil
			rt.BindPetSession(testDivision, near, 2)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
			}
			near.DeathKind = uint8(pk.DeathMonster)
			near.CurrentHP = testInt64(0)
			key := petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}
			state := rt.petSessionFor(key.division, key.name, key.gid)
			frames := rt.advancePetBond(key, state, clock.NowMs())
			if (*near.CurrentHP > 0) != (tier == 3) || (len(frames) > 0) != (tier == 3) {
				t.Fatal("party revive tier gate", *near.CurrentHP)
			}
			if tier != 3 {
				return
			}
			near.DeathKind = uint8(pk.DeathMonster)
			near.CurrentHP = testInt64(0)
			near.Level = testInt64(101)
			c.ActiveCOS.Training.PassiveReadyAtMs[1] = 0
			if len(rt.advancePetBond(key, state, clock.NowMs())) != 0 || *near.CurrentHP != 0 {
				t.Fatal("pet+10 level limit")
			}
		})
	}
}

/*
================
TestGuardianBondCommitClampsToBearCurrentHP

The bear's HP can fall between planning a transfer and committing it:
the debit, the booked budget spend and the published vitals are the
clamped amount the bear can actually pay, never the planned share
(checklist item 5's fatal over-charge at this head).
================
*/
func TestGuardianBondCommitClampsToBearCurrentHP(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 26)
	plan := rt.planPetBond(testDivision, c, clock.NowMs())
	if plan == nil {
		t.Fatal("missing transfer plan")
	}
	sharePetBond(plan, 1000000)
	if plan.moved == 0 {
		t.Fatal("nothing planned to transfer")
	}
	// The bear is mauled down to 30 HP before the commit lands.
	c.ActiveCOS.CurrentHP = 30
	if len(rt.commitPetBond(plan)) != 2 {
		t.Fatal("clamped commit published nothing")
	}
	if c.ActiveCOS.CurrentHP != 1 {
		t.Fatal("commit over-charged the bear", c.ActiveCOS.CurrentHP)
	}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state.bond.spent != 29 {
		t.Fatal("booked spend is not the clamped debit", state.bond.spent)
	}
	if len(rt.commitPetBond(plan)) != 0 {
		t.Fatal("duplicate debit after clamp")
	}
}

/*
================
TestGuardianPartyRevivalRoutingAndOfferDrop

A tier-III party revival sends the private set plus pulse to the revived
member only, broadcasts peers plus pulse to the owner and observers
exactly once (never through the owner-excluding public lane), and drops
any pending resurrection offer for the revived member (checklist item 4).
================
*/
func TestGuardianPartyRevivalRoutingAndOfferDrop(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 27)
	near := nearbyCharacter(rt, c, 21, "bond-near", 50)
	near.ActiveCOS = nil
	rt.BindPetSession(testDivision, near, 2)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
	}
	near.DeathKind = uint8(pk.DeathMonster)
	near.CurrentHP = testInt64(0)
	rt.resurrections.put(testDivision, near.Name, resurrectionOffer{expiresMs: clock.NowMs() + resurrectionAnswerWindowMs})
	key := petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}
	state := rt.petSessionFor(key.division, key.name, key.gid)
	rt.advancePetBond(key, state, clock.NowMs())
	if *near.CurrentHP == 0 {
		t.Fatal("party member not revived")
	}
	if rt.resurrections.pending(testDivision, near.Name, clock.NowMs()) {
		t.Fatal("stale resurrection offer survived the bond revival")
	}
	if len(state.routed) != 1 || state.routed[0].ExceptCharacterID != near.ID || len(state.routed[0].Frames) == 0 {
		t.Fatal("peer broadcast not routed past the revived member", state.routed)
	}
	if len(state.public) != 0 {
		t.Fatal("party revival leaked into the owner-excluding public lane", len(state.public))
	}
	found := false
	for _, other := range state.others {
		if other.CharacterID == near.ID && len(other.Frames) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("revived member missed its private frame set")
	}
}

/*
================
TestGuardianMonsterDamageAndFatalPublication
================
*/
func TestGuardianMonsterDamageAndFatalPublication(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		t.Run(map[bool]string{false: "surviving", true: "fatal"}[fatal], func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			mob := roarField(t, rt, c, clock)
			c.MissionInventory = nil
			rt.AttackPetExpansion = true
			learnPetBook(t, c, 26)
			mob.Ref.DefaultSkillIDs[0] = 2
			skills := rt.deps.SkillData().(staticSkillSource)
			row := skills[2]
			row.ActionCastingTimeMs = 0
			row.ActionRange = 1000
			skills[2] = row
			if fatal {
				c.CurrentHP = testInt64(1)
			} else {
				c.CurrentHP = testInt64(200)
			}
			oldHP := c.ActiveCOS.CurrentHP
			hit := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !hit.Accepted || c.ActiveCOS.CurrentHP >= oldHP {
				t.Fatalf("monster hit did not debit bear %+v", hit)
			}
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			if state.trained.counterHits != [2]int{} || state.trained.retribStored != 0 {
				t.Fatal("transfer charged hit counters")
			}
			if fatal {
				if *c.CurrentHP != 0 {
					t.Fatal("fatal fixture did not die")
				}
				clock.now = clock.now.Add(time.Second)
				if len(rt.TickHook()(clock.NowMs())) == 0 || *c.CurrentHP == 0 {
					t.Fatal("real monster death did not auto-revive")
				}
			}
		})
	}
}

/*
================
TestGuardianWaitsThroughDeadOwnerTicks
================
*/
func TestGuardianWaitsThroughDeadOwnerTicks(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 25)
	c.CurrentHP = testInt64(0)
	c.DeathKind = uint8(pk.DeathMonster)
	c.LastExpLoss = 1000
	c.ActiveCOS.Training.PassiveReadyAtMs[1] = clock.NowMs() + 2000
	rt.TickHook()(clock.NowMs())
	if *c.CurrentHP != 0 {
		t.Fatal("revived before cooldown")
	}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state.follower == nil {
		t.Fatal("living guardian lost its stationary anchor while owner awaited revival")
	}
	clock.now = clock.now.Add(2 * time.Second)
	rt.TickHook()(clock.NowMs())
	if *c.CurrentHP == 0 {
		t.Fatal("guardian failed to revive owner when cooldown expired")
	}
}
