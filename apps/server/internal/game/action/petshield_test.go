/*
===========================================================================
petshield_test.go - port-only absorption, tier targets, expiry and cancellation
===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetShieldAbsorption
================
*/
func TestPetShieldAbsorption(t *testing.T) {
	rt := &Runtime{AttackPetExpansion: true}
	key := petShieldKey{"one", 1}
	rt.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: 100, until: 5000}}
	if d := rt.absorbPetShield("two", 1, 30, 0); d != 30 {
		t.Fatal("cross-division absorption")
	}
	if d := rt.absorbPetShield("one", 1, 30, 0); d != 0 || rt.petShields.rows[key].amount != 70 {
		t.Fatal("partial absorption")
	}
	if d := rt.absorbPetShield("one", 1, 100, 1); d != 30 || rt.petShields.rows[key].amount != 0 {
		t.Fatal("overflow absorption")
	}
	rt.petShields.rows[key] = petShield{key: key, amount: 100, until: 5000}
	if d := rt.absorbPetShield("one", 1, 30, 5000); d != 30 {
		t.Fatal("expired shield absorbed")
	}
}

/*
================
TestPetShieldTiersAndExpiry
================
*/
func TestPetShieldTiersAndExpiry(t *testing.T) {
	for _, tier := range []uint8{1, 2, 3} {
		t.Run(string(rune('0'+tier)), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			mob := roarField(t, rt, c, clock)
			c.MissionInventory = nil
			rt.AttackPetExpansion = true
			near := nearbyCharacter(rt, c, 21, "shield-near", 50)
			far := nearbyCharacter(rt, c, 22, "shield-far", 500)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near), enterworld.ObjectIDForCharacter(far)}}}
			}
			skill := learnPetBook(t, c, 18+tier)
			result := rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs())
			if len(result.Broadcast) < 2 {
				t.Fatalf("no shields: %+v", result)
			}
			self := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(c)}
			if rt.petShields.rows[self].amount == 0 || rt.petShields.rows[petShieldKey{testDivision, c.ActiveCOS.GID}].amount == 0 {
				t.Fatal("missing owner/bear shield")
			}
			_, gotNear := rt.petShields.rows[petShieldKey{testDivision, enterworld.ObjectIDForCharacter(near)}]
			if gotNear != (tier >= 2) {
				t.Fatal("party tier gate")
			}
			if _, ok := rt.petShields.rows[petShieldKey{testDivision, enterworld.ObjectIDForCharacter(far)}]; ok {
				t.Fatal("party radius")
			}
			if rt.petShields.rows[self].burst != (tier == 3) {
				t.Fatal("burst tier gate")
			}
			before := rt.petShields.rows[self]
			// Leave 20 absorption on the owner and 40 on the bear: both bursts must deal 30 total.
			if tier == 3 {
				rt.absorbPetShield(testDivision, self.gid, before.amount-20, clock.NowMs())
				nearKey := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(near)}
				rt.absorbPetShield(testDivision, nearKey.gid, rt.petShields.rows[nearKey].amount, clock.NowMs())
				petKey := petShieldKey{testDivision, c.ActiveCOS.GID}
				rt.absorbPetShield(testDivision, petKey.gid, rt.petShields.rows[petKey].amount-40, clock.NowMs())
				before = rt.petShields.rows[self]
			}
			rt.advancePetShields(clock.NowMs() + 4999)
			if rt.petShields.rows[self].amount != before.amount {
				t.Fatal("early expiry")
			}
			frames := rt.advancePetShields(clock.NowMs() + 5000)
			if len(rt.petShields.rows) != 0 {
				t.Fatal("shield retained")
			}
			found := false
			burstGIDs := map[uint32]bool{}
			for _, batch := range frames {
				for _, f := range batch.Frames {
					if f.Opcode == petShieldOpcode && len(f.Payload) == 21 && f.Payload[20] == 1 {
						found = true
						burstGIDs[binary.LittleEndian.Uint32(f.Payload)] = true
					}
				}
			}
			if found != (tier == 3) {
				t.Fatal("expiry burst publication")
			}
			if tier == 3 && (!burstGIDs[self.gid] || !burstGIDs[c.ActiveCOS.GID] || len(burstGIDs) != 2) {
				t.Fatal("owner and bear must both publish expiry explosions")
			}
			left, _ := rt.Monsters.Get(testDivision, mob.Gid)
			want := mob.CurrentHP
			if tier == 3 {
				want -= 30
			}
			if left.CurrentHP != want {
				t.Fatalf("burst HP %d, want %d", left.CurrentHP, want)
			}
		})
	}
}

/*
================
TestPetShieldUnsummonCancelsWithoutExplosion
================
*/
func TestPetShieldUnsummonCancelsWithoutExplosion(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	rt.AttackPetExpansion = true
	c.MissionInventory = nil
	skill := learnPetBook(t, c, 21)
	rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs())
	c.ActiveCOS.Summoned = false
	for _, b := range rt.advancePetShields(clock.NowMs() + 5000) {
		for _, f := range b.Frames {
			if f.Opcode == petShieldOpcode && f.Payload[20] != 0 {
				t.Fatal("unsummon exploded")
			}
		}
	}
}

/*
================
TestPetShieldCancellation
================
*/
func TestPetShieldCancellation(t *testing.T) {
	for _, reason := range []string{"death", "logout", "town", "disabled"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			town := *c.World.Spawn.RegionID
			roarField(t, rt, c, clock)
			rt.AttackPetExpansion = true
			c.MissionInventory = nil
			skill := learnPetBook(t, c, 21)
			rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs())
			switch reason {
			case "death":
				*c.CurrentHP = 0
			case "logout":
				rt.forgetPetSession(testDivision, c.Name)
			case "town":
				*c.World.Spawn.RegionID = town
				rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
			case "disabled":
				rt.AttackPetExpansion = false
			}
			for _, b := range rt.advancePetShields(clock.NowMs() + 5000) {
				for _, f := range b.Frames {
					if f.Opcode == petShieldOpcode && f.Payload[20] != 0 {
						t.Fatal("cancelled shield exploded")
					}
				}
			}
			if len(rt.petShields.rows) != 0 {
				t.Fatal("cancelled shield retained")
			}
		})
	}
}

/*
================
TestPetShieldRealMonsterHit

Exercise the actual monster hit doors, not only the subtraction helper.
================
*/
func TestPetShieldRealMonsterHit(t *testing.T) {
	for _, petTarget := range []bool{false, true} {
		for _, shield := range []uint32{5, 1000} {
			plain, clock, c, mob := newCombatTestRuntime(t, 100000)
			guarded, guardedClock, gc, gm := newCombatTestRuntime(t, 100000)
			guarded.AttackPetExpansion = true
			if petTarget {
				equipCombatTestPet(t, plain, c, attackPetBand)
				equipCombatTestPet(t, guarded, gc, attackPetBand)
			}
			for _, rt := range []*Runtime{plain, guarded} {
				skills := rt.deps.SkillData().(staticSkillSource)
				row := skills[2]
				row.ActionCastingTimeMs = 0
				row.Attack.Min, row.Attack.Max, row.Attack.Percent = 30, 30, 100
				skills[2] = row
			}
			mob.Ref.DefaultSkillIDs[0], gm.Ref.DefaultSkillIDs[0] = 2, 2
			*c.CurrentHP, *gc.CurrentHP = 200, 200
			gid, ggid := enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(gc)
			before := uint32(200)
			if petTarget {
				gid, ggid = c.ActiveCOS.GID, gc.ActiveCOS.GID
				c.ActiveCOS.CurrentHP, gc.ActiveCOS.CurrentHP = 200, 200
			}
			key := petShieldKey{testDivision, ggid}
			guarded.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: shield, until: guardedClock.NowMs() + 5000}}
			base := plain.MonsterBasicAttack(testDivision, mob, gid, 2, clock.NowMs())
			result := guarded.MonsterBasicAttack(testDivision, gm, ggid, 2, guardedClock.NowMs())
			if !base.Accepted || !result.Accepted {
				t.Fatal("monster hit refused")
			}
			baseHP, guardedHP := uint32(*c.CurrentHP), uint32(*gc.CurrentHP)
			if petTarget {
				baseHP, guardedHP = c.ActiveCOS.CurrentHP, gc.ActiveCOS.CurrentHP
			}
			damage := before - baseHP
			if damage == 0 {
				t.Fatal("fixture hit did not land")
			}
			absorbed := min(shield, damage)
			if guardedHP != baseHP+absorbed || guarded.petShields.rows[key].amount != shield-absorbed {
				t.Fatalf("pet=%v shield=%d: baseHP=%d guardedHP=%d damage=%d", petTarget, shield, baseHP, guardedHP, damage)
			}
		}
	}
}

/*
================
TestPetShieldAbsorbsBeforeManaRedirect
================
*/
func TestPetShieldAbsorbsBeforeManaRedirect(t *testing.T) {
	rt, clock, c, mob := damageToMPFixture(t, 19592)
	rt.AttackPetExpansion = true
	mob.Ref.DefaultSkillIDs[0] = 2
	*c.CurrentHP, *c.CurrentMP = 200, 200
	key := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(c)}
	rt.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: 10000, until: clock.NowMs() + 5000}}
	result := rt.MonsterBasicAttack(testDivision, mob, key.gid, 2, clock.NowMs())
	if !result.Accepted || *c.CurrentHP != 200 || *c.CurrentMP != 200 || rt.petShields.rows[key].amount == 10000 {
		t.Fatalf("shield/mana ordering: accepted=%v HP=%d MP=%d", result.Accepted, *c.CurrentHP, *c.CurrentMP)
	}
}

/*
================
TestPetShieldPartyDepartureCancels
================
*/
func TestPetShieldPartyDepartureCancels(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	near := nearbyCharacter(rt, c, 21, "shield-party", 50)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)}}}
	}
	skill := learnPetBook(t, c, 21)
	rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs())
	key := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(near)}
	if rt.petShields.rows[key].amount == 0 {
		t.Fatal("party shield not cast")
	}
	rt.RewardParties = func(string) []RewardParty { return nil }
	for _, b := range rt.advancePetShields(clock.NowMs() + 5000) {
		for _, f := range b.Frames {
			if f.Opcode == petShieldOpcode && binary.LittleEndian.Uint32(f.Payload) == key.gid && f.Payload[20] != 0 {
				t.Fatal("former party member exploded")
			}
		}
	}
	if _, ok := rt.petShields.rows[key]; ok {
		t.Fatal("former party shield retained")
	}
}

/*
================
TestPetShieldPlayerSourceBypassesAbsorption
================
*/
func TestPetShieldPlayerSourceBypassesAbsorption(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100000)
	rt.AttackPetExpansion = true
	*c.CurrentHP = 200
	key := petShieldKey{testDivision, enterworld.ObjectIDForCharacter(c)}
	rt.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: 100, until: clock.NowMs() + 5000}}
	owner := rt.newPlayerAbnormalOwner(testDivision, c, clock.NowMs())
	owner.sources = map[uint32]abnormalSourceState{999: {killer: deathKiller{player: c}}}
	owner.Hit(999, true, 10, 0, 0)
	if *c.CurrentHP != 190 || rt.petShields.rows[key].amount != 100 {
		t.Fatal("player DoT consumed shield")
	}
	hit := playerStrike{division: testDivision, victim: c, killer: deathKiller{player: c}, owner: owner, now: clock.NowMs(), formulas: []combat.Result{{Damage: 10, ResultFlags: 1}}}
	rt.strikePlayerInDoor(hit)
	if *c.CurrentHP != 180 || rt.petShields.rows[key].amount != 100 {
		t.Fatal("player strike consumed shield")
	}
	rt.AttackPetExpansion = false
	if rt.absorbPetShield(testDivision, key.gid, 10, clock.NowMs()) != 10 {
		t.Fatal("disabled shield absorbed")
	}
}

/*
================
TestPetShieldRetributionStoresOnlyHPDamage
================
*/
func TestPetShieldRetributionStoresOnlyHPDamage(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	mob := roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	state.trained.retribution = clock.NowMs() + 10000
	key := petShieldKey{testDivision, c.ActiveCOS.GID}
	rt.petShields.rows = map[petShieldKey]petShield{key: {key: key, amount: 100000, until: clock.NowMs() + 5000}}
	mob.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	row := skills[2]
	row.ActionCastingTimeMs = 0
	row.ActionRange = 1000
	skills[2] = row
	result := rt.MonsterBasicAttack(testDivision, mob, key.gid, 2, clock.NowMs())
	if !result.Accepted {
		t.Fatalf("monster strike refused: %+v", result)
	}
	if rt.petShields.rows[key].amount == 100000 {
		t.Fatal("fixture hit did not consume shield")
	}
	if state.trained.retribStored != 0 {
		t.Fatal("absorbed damage charged retribution")
	}
}

/*
================
TestPetShieldDisconnectCancelsRecipientAndCaster
================
*/
func TestPetShieldDisconnectCancelsRecipientAndCaster(t *testing.T) {
	rt := &Runtime{AttackPetExpansion: true}
	rt.petShields.rows = map[petShieldKey]petShield{}
	for _, shield := range []petShield{
		{key: petShieldKey{"one", 1}, name: "member", source: petOwnerKey{name: "caster"}},
		{key: petShieldKey{"one", 2}, name: "other", source: petOwnerKey{name: "member"}},
		{key: petShieldKey{"two", 1}, name: "member", source: petOwnerKey{name: "caster"}},
	} {
		shield.amount, shield.until, shield.burst = 100, 5000, true
		rt.petShields.rows[shield.key] = shield
	}
	rt.retirePetShields("one", "MEMBER")
	for key, shield := range rt.petShields.rows {
		if key.division == "one" && (shield.amount != 0 || shield.burst) {
			t.Fatal("disconnected shield retained")
		}
		if key.division == "two" && shield.amount != 100 {
			t.Fatal("cross-division teardown")
		}
	}
}

/*
================
TestPetShieldBurstKillCreditsNoQuest

A tier-3 shield's expiry burst that kills a monster is a companion kill:
the burst goes through its real caller (advancePetShields ->
burstPetShield) and no quest kill is credited, though the monster dies.
================
*/
func TestPetShieldBurstKillCreditsNoQuest(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	mob := roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	quested := 0
	rt.UpdateQuestKill = func(*enterworld.Character, string, uint8) ([]wire.Frame, bool) {
		quested++
		return nil, true
	}
	skill := learnPetBook(t, c, 21)
	if result := rt.castPetShield(testDivision, c, c, c.ActiveCOS, skill, clock.NowMs()); len(result.Broadcast) < 2 {
		t.Fatalf("no shields: %+v", result)
	}
	if _, ok := rt.Monsters.ApplyDamage(testDivision, mob.Gid, mob.CurrentHP-1); !ok {
		t.Fatal("could not wound the monster")
	}
	rt.advancePetShields(clock.NowMs() + 5000)
	left, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if left.CurrentHP != 0 {
		t.Fatalf("the burst left the monster at %d HP", left.CurrentHP)
	}
	if quested != 0 {
		t.Fatalf("the shield burst kill credited %d quest kills, want 0", quested)
	}
}
