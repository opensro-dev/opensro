/*
===========================================================================
petbrood_test.go - egg admission, damage, hatching, lifetime and clone isolation.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"encoding/json"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

/*
================
TestCrowBroodLifecycle
================
*/
func TestCrowBroodLifecycle(t *testing.T) {
	for _, book := range []uint8{49, 50, 51} {
		rt, clock, c, _, key := tailwindFixture(t, 1)
		skill := learnPetBook(t, c, book)
		result := rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
		if len(c.Brood) != 1 || len(result.Broadcast) != 2 || result.Broadcast[1].Opcode != crowVisualOpcode || result.Broadcast[0].Payload[len(result.Broadcast[0].Payload)-1] != 0 {
			t.Fatal("cast missing", book, len(c.Brood))
		}
		egg := c.Brood[0]
		if egg.BroodStage != 1 || egg.BroodUntil != clock.NowMs()+30000 || egg.CurrentHP == 0 {
			t.Fatal(egg)
		}
		if c.ActiveCOS.Training.Rows[1].Charges != 0 {
			t.Fatal("charge not spent")
		}
		rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
		if len(c.Brood) != 1 {
			t.Fatal("cast without charge")
		}
		clone := c.Snapshot()
		clone.Brood[0].CurrentHP = 1
		if egg.CurrentHP == 1 {
			t.Fatal("snapshot aliases egg")
		}
		blob, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var saved domain.Character
		if err = json.Unmarshal(blob, &saved); err != nil || len(saved.Brood) != 0 {
			t.Fatal("temporary actor persisted", err)
		}
		clock.now = clock.now.Add(29999 * time.Millisecond)
		rt.advanceCrowBrood(key, clock.NowMs())
		if egg.BroodStage != 1 {
			t.Fatal("early hatch")
		}
		clock.now = clock.now.Add(time.Millisecond)
		rt.advanceCrowBrood(key, clock.NowMs())
		if egg.BroodStage != 2 || egg.CurrentHP != egg.BroodHatchHP || egg.BroodUntil != clock.NowMs()+600000 {
			t.Fatal("hatch", egg)
		}
		if len(egg.Training.Levels) != 15 || egg.Training.Levels != [15]uint8{} {
			t.Fatal("skills copied")
		}
		for _, def := range companion.SkillsForPet(egg) {
			if companion.Level(egg, def) != 0 {
				t.Fatal("clone learned skill")
			}
		}
		clock.now = clock.now.Add(600000 * time.Millisecond)
		rt.advanceCrowBrood(key, clock.NowMs())
		if len(c.Brood) != 0 {
			t.Fatal("clone outlived deadline")
		}
	}
}

/*
================
TestCrowBroodDestroyedEggAndCaps
================
*/
func TestCrowBroodDestroyedEggAndCaps(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	s := learnPetBook(t, c, 51)
	for range companion.BroodLimit {
		c.ActiveCOS.Training.Rows[1] = domain.PetRowCooldown{Kind: companion.ChargeKind, Capacity: 3, Charges: 3, RechargeMs: 300000}
		rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	}
	if len(c.Brood) != companion.BroodLimit {
		t.Fatal("ten live summons", len(c.Brood))
	}
	before := c.ActiveCOS.Training.Rows[1]
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	if len(c.Brood) != companion.BroodLimit || before != c.ActiveCOS.Training.Rows[1] {
		t.Fatal("live cap spent cast")
	}
	c.Brood[0].CurrentHP = 0
	clock.now = clock.now.Add(31 * time.Second)
	rt.advanceCrowBrood(key, clock.NowMs())
	if len(c.Brood) != companion.BroodLimit-1 {
		t.Fatal("dead egg hatched")
	}
	rt.AttackPetExpansion = false
	rt.advanceCrowBrood(key, clock.NowMs())
	if len(c.Brood) != 0 {
		t.Fatal("flag off left clones")
	}
	for slot := 0; slot < companion.BroodLimit; slot++ {
		if id, valid := broodObjectID(c.ID, slot); !valid || id < broodGIDBase {
			t.Fatal(id, valid)
		}
	}
	if _, valid := broodObjectID(-1, 0); valid {
		t.Fatal("invalid identity")
	}
}

/*
================
TestCrowBroodNativeDamageAndWindBurst
================
*/
func TestCrowBroodNativeDamageAndWindBurst(t *testing.T) {
	for _, test := range []struct {
		book   uint8
		collar uint32
		slowed bool
	}{{50, 0, false}, {51, 0, false}, {51, 30101, false}, {51, 30109, false}, {51, 30139, false}, {51, 0, true}} {
		book := test.book
		rt, clock, c, mob, key := tailwindFixture(t, 1)
		c.ActiveCOS.Training.Gear[0] = domain.PetGear{RefObjID: test.collar}
		skill := learnPetBook(t, c, book)
		rt.castCrowBrood(testDivision, c, key.gid, skill, clock.NowMs())
		egg := c.Brood[0]
		ref, _ := rt.cosReference(egg)
		block := rt.newCosAbnormalOwnerForPet(testDivision, c, egg, clock.NowMs())
		hit := cosHit{monsterSource: true, owner: c, pet: egg, ref: ref, block: block, formulas: []combat.Result{{Damage: 1}}, displaceAt: -1}
		hp := egg.CurrentHP
		if !rt.applyCOSHitInDoor(testDivision, &hit, clock.NowMs()) || egg.CurrentHP != hp-1 {
			t.Fatal("egg rejects native hit")
		}
		clock.now = clock.now.Add(30 * time.Second)
		rt.advanceCrowBrood(key, clock.NowMs())
		child := rt.petSessionFor(key.division, key.name, egg.GID)
		// Put the explosion beside a valid, surviving target using the real world plane.
		step, _ := rt.elementalCompanion(testDivision, c, key.gid, "Crow", clock.NowMs())
		target, ok := rt.resolvePetCombatTarget(step, mob.Gid)
		if !ok {
			t.Fatal("missing monster")
		}
		child.broodAt = target.at
		child.follower = simulation.NewPetFollower(egg.GID, child.broodAt)
		if test.slowed {
			// A slowed target already gets its HP in the strike's abnormal vitals frame.
			seedBroodTargetSlow(t, rt, c, mob.Gid)
		}
		cloneStep := petCombatStep{key: petOwnerKey{division: key.division, name: key.name, gid: egg.GID}, state: child, snapshot: rt.characterSnapshot(key.division, c), pet: egg, ref: ref, nowMs: clock.NowMs(), percent: fullAreaPercent}
		attack, found := rt.petAttackSkill(cloneStep, &petCombatIntent{}, simulation.AttackTarget{})
		if !found {
			t.Fatal("missing clone attack")
		}
		attack, cloneStep.trained = rt.petTrainedDecide(cloneStep, attack, target)
		normal, committed := rt.strikePetTarget(cloneStep, target, attack)
		if !committed || normal.hpDamage == 0 {
			t.Fatal("normal clone attack failed")
		}
		before, _ := rt.Monsters.Get(testDivision, mob.Gid)
		rt.drainMonsterLegRecipients()
		egg.CurrentHP = 0
		rt.advanceCrowBrood(key, clock.NowMs())
		visuals := 0
		var shown, hpRefresh, refreshes uint32
		for _, batch := range rt.drainMonsterLegRecipients() {
			for _, frame := range batch.Frames {
				if frame.Opcode == 0x3128 && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid {
					if batch.SourceGID != key.gid {
						t.Fatal("wind feedback routed through retired clone", batch.SourceGID)
					}
					shown += binary.LittleEndian.Uint32(frame.Payload[4:])
				}
				if frame.Opcode == simulation.OpVitalsUpdate && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid {
					hpRefresh = binary.LittleEndian.Uint32(frame.Payload[7:])
					refreshes++
				}
				if frame.Opcode == broodWindOpcode {
					visuals++
					if len(frame.Payload) != 18 {
						t.Fatal("wind position payload", len(frame.Payload))
					}
				}
			}
		}
		if (visuals == 1) != (book == 51) {
			t.Fatal("wind visual tier", book, visuals)
		}
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if (after.CurrentHP < before.CurrentHP) != (book == 51) {
			t.Fatal("death burst tier", book, mob.CurrentHP, after.CurrentHP)
		}
		if book == 51 {
			damage := before.CurrentHP - after.CurrentHP
			// Each impact floors once, so five times its unrounded damage can add up to four.
			rounding := uint64(len(normal.strike.impacts)) * 4
			if uint64(damage) < normal.hpDamage*5 || uint64(damage) > normal.hpDamage*5+rounding {
				t.Fatal("wind must deal 500% of the same normal roll", test.collar, normal.hpDamage, damage)
			}
			if shown != damage || hpRefresh != after.CurrentHP {
				t.Fatal("wind damage/HP feedback missing", damage, shown, after.CurrentHP, hpRefresh)
			}
			// One HP refresh per target: the strike's own abnormal vitals frame
			// and the burst's refresh must never both reach the client.
			if refreshes != 1 {
				t.Fatal("wind HP refresh count", test.collar, refreshes)
			}
		}
		if len(c.Brood) != 0 {
			t.Fatal("dead clone retained")
		}
	}
}

/*
================
TestCrowBroodHatchesThroughWorldTick
================
*/
func TestCrowBroodHatchesThroughWorldTick(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	s := learnPetBook(t, c, 51)
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	tickPetCombat(t, rt, clock, 400, func() bool { return len(c.Brood) == 1 && c.Brood[0].BroodStage == 2 })
}

/*
================
TestCrowBroodHatchWithDurableAuthority
================
*/
func TestCrowBroodHatchWithDurableAuthority(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	st, err := store.Open(t.TempDir(), store.Options{Now: clock.Now, DefaultSkills: func(string, []uint32) ([]uint32, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err = st.CreateCharacter(testDivision, "brood-test", c); err != nil {
		t.Fatal(err)
	}
	deps := rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = st.UpdateCharacter
	deps.UpdateCharacterDeferred = st.UpdateCharacterDeferred
	s := learnPetBook(t, c, 51)
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	tickPetCombat(t, rt, clock, 400, func() bool { return len(c.Brood) == 1 && c.Brood[0].BroodStage == 2 })
}

/*
================
TestCrowBroodIgnoresDormantLegacyPetAlias
================
*/
func TestCrowBroodIgnoresDormantLegacyPetAlias(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	s := learnPetBook(t, c, 51)
	crow := c.ActiveCOS
	dormant := domain.CloneCOS(crow)
	dormant.Codename = "COS_P_WOLF_090"
	dormant.Summoned = false
	c.ActiveCOS = dormant
	c.MissionInventory = []domain.InventoryRow{{Slot: 22, Summon: crow}}

	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	if len(c.Brood) != 1 {
		t.Fatal("cast missing")
	}
	tickPetCombat(t, rt, clock, 400, func() bool { return len(c.Brood) == 1 && c.Brood[0].BroodStage == 2 })
}

/*
================
TestCrowBroodCloneFightsSourceTarget
================
*/
func TestCrowBroodCloneFightsSourceTarget(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 1)
	s := learnPetBook(t, c, 51)
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	rt.HandleCosCommand(testDivision, c, petAttackOrder(key.gid, mob.Gid))
	gid := c.Brood[0].GID
	tickPetCombat(t, rt, clock, 650, func() bool {
		state := rt.petSessionFor(key.division, key.name, gid)
		return state != nil && state.trained.attacks > 0
	})
	if c.Brood[0].BroodStage != 2 {
		t.Fatal("egg attacked before hatching")
	}
	child := rt.petSessionFor(key.division, key.name, gid)
	if child.combat == nil || child.combat.target != mob.Gid {
		t.Fatal("clone did not inherit target")
	}
}

/*
================
TestCrowBroodSlotReuseChangesSummonGeneration
================
*/
func TestCrowBroodSlotReuseChangesSummonGeneration(t *testing.T) {
	rt, clock, c, _, key := tailwindFixture(t, 1)
	s := learnPetBook(t, c, 51)
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	first := *c.Brood[0]
	c.Brood[0].CurrentHP = 0
	rt.advanceCrowBrood(key, clock.NowMs())
	c.ActiveCOS.Training.Rows[1].Charges = 1
	rt.castCrowBrood(testDivision, c, key.gid, s, clock.NowMs())
	if len(c.Brood) != 1 || c.Brood[0].GID != first.GID || c.Brood[0].SummonGeneration == first.SummonGeneration {
		t.Fatal("stale hit could target replacement egg")
	}
}

/*
================
seedBroodTargetSlow

Puts a long Slow on the burst's target, so every strike on it publishes the
abnormal vitals frame that already carries its HP.
================
*/
func seedBroodTargetSlow(t *testing.T, rt *Runtime, c *enterworld.Character, gid uint32) {
	t.Helper()
	record := abnormal.Record{Status: abnormal.Slow, Grade: 1, DurationMs: 600000, SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}
	current, _ := rt.Monsters.Get(testDivision, gid)
	plan := simulation.MonsterDamagePlan{GID: gid, CreditGID: record.SourceGID, Abnormal: []abnormal.Record{record}, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{record})}
	if r := rt.Monsters.ApplyDamageSequence(testDivision, gid, current.CurrentHP, []simulation.MonsterDamagePlan{plan}); len(r) != 1 || r[0].Instance.Abnormal == nil {
		t.Fatal("slow admission")
	}
}

/*
================
TestBroodVitalsFrameNeedsHPBit

Only a vitals update that carries the target's HP stands in for the death
burst's own refresh; an update for another object or without the HP bit
leaves the refresh to the burst.
================
*/
func TestBroodVitalsFrameNeedsHPBit(t *testing.T) {
	const gid uint32 = 0x1234
	withHP := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, 0, 99)}
	noHP := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: wire.NewWriter(7).U32(gid).U16(0x100).U8(0x04).Payload()}
	other := wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid+1, 0, 99)}
	for _, test := range []struct {
		frames []wire.Frame
		want   bool
	}{{[]wire.Frame{withHP}, true}, {[]wire.Frame{noHP}, false}, {[]wire.Frame{other}, false}, {[]wire.Frame{noHP, withHP}, true}, {nil, false}} {
		if got := hasVitalsFrameFor(test.frames, gid); got != test.want {
			t.Fatal("vitals frame match", len(test.frames), got, test.want)
		}
	}
}
