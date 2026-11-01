/*
===========================================================================

petmaxhp_test.go - a pet's HP is healed and drawn against its effective
max (#1896): the unasked state push that carries the max to a default
client, and the pet heals that must not clamp below it.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petStatePush

The 0x3FB1 pushes among frames: the frame that carries the effective
max the client divides a pet's HP by.
================
*/
func petStatePush(frames []wire.Frame) (wire.Frame, bool) {
	for _, frame := range frames {
		if frame.Opcode == wire.OpPetTrainingState {
			return frame, true
		}
	}
	return wire.Frame{}, false
}

/*
================
petMaxRuntime

An owner, its summoned wolf at 100 of 1000 HP with a recovery kit in
the bag, with the attack-pet expansion in the given posture. The wolf
wears a degree-5 HP piece, so its effective max is geared, not bare.
================
*/
func petMaxRuntime(t *testing.T, expansion bool) (*Runtime, *enterworld.Character, *enterworld.CharacterCOS) {
	t.Helper()
	kit := &enterworld.ItemRef{
		RefObjID: 4001, Codename: "ITEM_ETC_COS_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 4},
		RecoveryHP: 500, Country: 3,
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1}),
	}
	source := testCosSource(testItems())
	source.staticItemSource[kit.Codename] = kit
	source.characters["COS_P_WOLF_001"] = &enterworld.CharacterRef{
		RefObjID: 2001, TidWord: petWolfTid, Codename: "COS_P_WOLF_001",
		Level: 20, MaxHP: 1000, MaxMP: 100, WalkSpeed: 20, RunSpeed: 60, Scale: 100,
	}
	c := testCharacter()
	pet := &enterworld.CharacterCOS{
		GID: 9001, RefObjID: 2001, Codename: "COS_P_WOLF_001", Level: 40,
		CurrentHP: 100, CurrentMP: 10, Summoned: true, InventorySlot: 13, Satiety: 100,
	}
	gearItem := companion.D.Gear[4].Items[0]
	_, gearSlot, ok := companion.GearByID(gearItem.ID)
	if !ok {
		t.Fatal("degree-5 gear piece has no slot")
	}
	pet.Training.Gear[gearSlot] = domain.PetGear{RefObjID: gearItem.ID}
	c.ActiveCOS = pet
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: kit.RefObjID, Codename: kit.Codename,
		TypeFlags: kit.TypeFlags(), StackCount: 2,
	}}
	rt, _ := newTestRuntime(c, source)
	rt.AttackPetExpansion = expansion
	return rt, c, pet
}

/*
================
TestAttackPetPotionPublishesEffectiveMax

Under the expansion the kit's answer carries the 0x3FB1 state with the
geared max, so a default client's bar reads the heal. Without the
expansion no state is pushed and the pet heals against the bare max,
as natively.
================
*/
func TestAttackPetPotionPublishesEffectiveMax(t *testing.T) {
	for _, expansion := range []bool{true, false} {
		rt, c, pet := petMaxRuntime(t, expansion)
		kit, _ := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_COS_HP_POTION_01")
		effective := rt.petEffectiveMaxHP(pet)
		if expansion && effective <= 1000 {
			t.Fatalf("geared max %d does not exceed the bare 1000", effective)
		}
		if !expansion && effective != 1000 {
			t.Fatalf("max without the expansion = %d, want the bare 1000", effective)
		}
		result := rt.HandleItemUse(testDivision, c, petUse(c, kit, pet.GID, -1))
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("kit refused (expansion %v): %+v", expansion, result)
		}
		if pet.CurrentHP <= 100 || pet.CurrentHP > effective {
			t.Fatalf("pet HP %d after kit, want within (100, %d]", pet.CurrentHP, effective)
		}
		push, found := petStatePush(result.Frames)
		if found != expansion {
			t.Fatalf("state push present = %v, want %v", found, expansion)
		}
		if expansion {
			want := rt.petTrainingStateFrames(pet, rt.Now().UnixMilli())
			if len(want) == 0 || push.Payload == nil || string(push.Payload) != string(want[len(want)-1].Payload) {
				t.Fatalf("pushed state %+v, want the pet's current state frame", push)
			}
		}
	}
}

/*
================
TestAttackPetSummonPublishesEffectiveMax

A fresh summon under the expansion publishes the state with its max;
without the expansion the summon is the native one, no state frame.
================
*/
func TestAttackPetSummonPublishesEffectiveMax(t *testing.T) {
	for _, expansion := range []bool{true, false} {
		c := testCharacter()
		level := int64(5)
		c.Level = &level
		source := testCosSource(testItems())
		scroll := &enterworld.ItemRef{
			Codename: "SUMMON_WOLF", RefObjID: 900, TypeIDs: [4]int64{3, 2, 1, 1}, Country: 3,
			AssociatedCharacterCodename: "COS_P_WOLF_001",
			NativeFields:                enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 60}),
		}
		source.staticItemSource[scroll.Codename] = scroll
		source.characters["COS_P_WOLF_001"] = &enterworld.CharacterRef{
			Codename: "COS_P_WOLF_001", RefObjID: 950, TidWord: petWolfTid,
			Level: 1, MaxHP: 1000, MaxMP: 50, WalkSpeed: 20, RunSpeed: 60, Scale: 100,
		}
		c.MissionInventory = []enterworld.InventoryRow{{
			Slot: 23, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
			TypeFlags: scroll.TypeFlags(), StackCount: 1,
		}}
		rt, _ := newTestRuntime(c, source)
		rt.AttackPetExpansion = expansion
		result := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(23).U16(scroll.TypeFlags()).Payload())
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("summon refused (expansion %v): %+v", expansion, result)
		}
		pet := c.MissionInventory[0].Summon
		if pet == nil || !pet.Summoned || pet.CurrentHP != 1000 {
			t.Fatalf("summoned pet %+v", pet)
		}
		if _, found := petStatePush(result.Frames); found != expansion {
			t.Fatalf("summon state push present = %v, want %v", found, expansion)
		}
	}
}

/*
================
TestRestoredAttackPetPublishesEffectiveMax

Binding a session for a restored, summoned attack pet queues the
state for its owner; with the expansion off nothing is queued.
================
*/
func TestRestoredAttackPetPublishesEffectiveMax(t *testing.T) {
	for _, expansion := range []bool{true, false} {
		source := testCosSource(testItems())
		source.characters["COS_P_WOLF_001"] = &enterworld.CharacterRef{
			RefObjID: 2001, TidWord: petWolfTid, Codename: "COS_P_WOLF_001",
			Level: 20, MaxHP: 1000, MaxMP: 100, WalkSpeed: 20, RunSpeed: 60, Scale: 100,
		}
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 23, Summon: &enterworld.CharacterCOS{
			GID: 777001, RefObjID: 2001, Codename: "COS_P_WOLF_001", Level: 40,
			CurrentHP: 500, CurrentMP: 50, Summoned: true, Satiety: 100,
		}}}
		rt, _ := newTestRuntime(c, source)
		rt.AttackPetExpansion = expansion
		rt.BindPetSession(testDivision, c, 42)
		batches := rt.drainMonsterLegRecipients()
		if !expansion {
			if len(batches) != 0 {
				t.Fatalf("queued frames without the expansion: %+v", batches)
			}
			continue
		}
		if len(batches) != 1 || batches[0].OnlyCharacterID != c.ID {
			t.Fatalf("restore batches %+v, want one for the owner", batches)
		}
		found := false
		for _, frame := range batches[0].Frames {
			if frame.Opcode == wire.OpPetTrainingState {
				found = true
			}
		}
		if !found {
			t.Fatalf("no state push in restore batch %+v", batches[0].Frames)
		}
	}
}

/*
================
TestAttackPetLevelUpPublishesEffectiveMax

The growth gain that changes the pet's form also publishes the new
form's effective max to the owner: the old catalog max would leave
the bar reading the pre-growth fraction.
================
*/
func TestAttackPetLevelUpPublishesEffectiveMax(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	rt.AttackPetExpansion = true
	ref, _ := rt.cosReference(pet)
	owner, _ := rt.applyAttackPetExperience(c, pet, ref, 350, 9001)
	if pet.Codename != "COS_P_WOLF_003" || rt.petEffectiveMaxHP(pet) != 600 {
		t.Fatalf("grown pet %+v max %d", pet, rt.petEffectiveMaxHP(pet))
	}
	push, found := petStatePush(owner)
	if !found {
		t.Fatalf("level-up published no state: %+v", owner)
	}
	want := rt.petTrainingStateFrames(pet, rt.Now().UnixMilli())
	if len(want) == 0 || string(push.Payload) != string(want[len(want)-1].Payload) {
		t.Fatalf("pushed state %+v, want the grown pet's state frame", push)
	}
}

/*
================
TestLifebiteHealKeepsGearedHP

A wolf wearing HP gear and slotting Lifebite is topped near its geared
max; its next landed hit leeches. The heal must cap at the geared max:
before the fix it clamped to the bare reference and the "heal" cut the
pet's HP down to it.
================
*/
func TestLifebiteHealKeepsGearedHP(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 40)
	rt.AttackPetExpansion = true
	c.ActiveCOS.Training.BonusPoints = 60
	life := learn(t, rt, c, "Lifebite", 1)
	rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, petTrainingSlot, life.ID))
	if mods := companion.ModifiersFor(c.ActiveCOS); mods.LeechPct <= 0 {
		t.Fatalf("Lifebite not slotted: %+v", mods)
	}
	bare, _ := rt.cosReference(c.ActiveCOS)
	gear := companion.D.Gear[4].Items[0]
	grantGear(t, rt, c, gear.ID)
	row, _ := bagItem(c, gear.Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	worn, _ := rt.cosReference(c.ActiveCOS)
	if worn.MaxHP <= bare.MaxHP*2 {
		t.Fatalf("gear did not raise the maximum: bare %d worn %d", bare.MaxHP, worn.MaxHP)
	}
	c.ActiveCOS.CurrentHP = worn.MaxHP - 5
	before := c.ActiveCOS.CurrentHP
	attackUntil(t, rt, clock, c, m, func() bool {
		hit, _ := rt.Monsters.Get(testDivision, m.Gid)
		return hit.CurrentHP < m.CurrentHP
	})
	if c.ActiveCOS.CurrentHP < before {
		t.Fatalf("a leech heal lowered the pet's HP from %d to %d (bare max %d, geared max %d)",
			before, c.ActiveCOS.CurrentHP, bare.MaxHP, worn.MaxHP)
	}
}

/*
================
TestPetHitWithoutLifebiteKeepsGearedHP

The control for the leech clamp: the same fight without Lifebite runs
no heal, so the geared HP survives the hit untouched.
================
*/
func TestPetHitWithoutLifebiteKeepsGearedHP(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 40)
	rt.AttackPetExpansion = true
	c.ActiveCOS.Training.BonusPoints = 60
	if mods := companion.ModifiersFor(c.ActiveCOS); mods.LeechPct != 0 {
		t.Fatalf("Lifebite unexpectedly active: %+v", mods)
	}
	gear := companion.D.Gear[4].Items[0]
	grantGear(t, rt, c, gear.ID)
	row, _ := bagItem(c, gear.Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	worn, _ := rt.cosReference(c.ActiveCOS)
	c.ActiveCOS.CurrentHP = worn.MaxHP - 5
	before := c.ActiveCOS.CurrentHP
	attackUntil(t, rt, clock, c, m, func() bool {
		hit, _ := rt.Monsters.Get(testDivision, m.Gid)
		return hit.CurrentHP < m.CurrentHP
	})
	if c.ActiveCOS.CurrentHP != before {
		t.Fatalf("control HP changed without a heal: %d -> %d", before, c.ActiveCOS.CurrentHP)
	}
}

/*
================
TestCrowPlumeHealKeepsGearedHP

The Crow's plume heals its own pet toward the effective max: on a
geared crow near its max, the heal must not cut HP to the bare max.
(The plume's step already carries the effective reference; this pins
the contract the #1896 investigators flagged from the bare-ref step
the strike lane uses.)
================
*/
func TestCrowPlumeHealKeepsGearedHP(t *testing.T) {
	rt, _, c, _, _ := tailwindFixture(t, 1)
	skill := learnPetBook(t, c, 47)
	rt.AttackPetExpansion = true
	gear := companion.D.Gear[4].Items[0]
	_, gearSlot, ok := companion.GearByID(gear.ID)
	if !ok {
		t.Fatal("degree-5 gear piece has no slot")
	}
	c.ActiveCOS.Training.Gear[gearSlot] = domain.PetGear{RefObjID: gear.ID}
	geared := rt.petEffectiveMaxHP(c.ActiveCOS)
	bareRef, _ := rt.deps.ItemReferences().(enterworld.CharacterRefSource).CharacterRefByCodename(c.ActiveCOS.Codename)
	if geared <= bareRef.MaxHP {
		t.Fatalf("geared max %d does not exceed bare %d", geared, bareRef.MaxHP)
	}
	c.ActiveCOS.CurrentHP = geared - 1
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c)}}}
	}
	rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID))
	if c.ActiveCOS.CurrentHP == geared-1 {
		t.Fatal("the plume never healed the crow: the party walk visited nobody")
	}
	if c.ActiveCOS.CurrentHP < geared-1 {
		t.Fatalf("plume heal lowered the crow's HP from %d to %d (bare max %d, geared max %d)",
			geared-1, c.ActiveCOS.CurrentHP, bareRef.MaxHP, geared)
	}
}

/*
================
TestWearPetGearSwapClampsHP

Swapping an HP piece for a plain one lowers the pet's effective max:
the worn HP clamps to the new max and the owner is told, exactly as
removing the piece does.
================
*/
func TestWearPetGearSwapClampsHP(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 40)
	rt.AttackPetExpansion = true
	hpItem := companion.D.Gear[4].Items[0]
	_, hpSlot, ok := companion.GearByID(hpItem.ID)
	if !ok {
		t.Fatal("degree-5 gear piece has no slot")
	}
	var plainItem companion.GearItem
	for _, degree := range companion.D.Gear {
		for _, item := range degree.Items {
			if _, slot, ok := companion.GearByID(item.ID); ok && slot == hpSlot &&
				item.BaseStats.MaxHPPercent < hpItem.BaseStats.MaxHPPercent &&
				(plainItem.ID == 0 || item.BaseStats.MaxHPPercent < plainItem.BaseStats.MaxHPPercent) {
				plainItem = item
			}
		}
	}
	if plainItem.ID == 0 {
		t.Fatal("no plain same-slot gear piece to swap to")
	}
	grantGear(t, rt, c, hpItem.ID)
	row, _ := bagItem(c, hpItem.Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	geared := rt.petEffectiveMaxHP(c.ActiveCOS)
	c.ActiveCOS.CurrentHP = geared
	grantGear(t, rt, c, plainItem.ID)
	row, _ = bagItem(c, plainItem.Codename)
	result := train(rt, c, petTrainingEquip, uint8(row.Slot))
	plain := rt.petEffectiveMaxHP(c.ActiveCOS)
	if plain == 0 || plain >= geared {
		t.Fatalf("swap did not lower the max: %d -> %d", geared, plain)
	}
	if c.ActiveCOS.CurrentHP != plain {
		t.Fatalf("HP %d after swap, want clamped to %d", c.ActiveCOS.CurrentHP, plain)
	}
	clamped := false
	for _, frame := range result.Frames {
		if frame.Opcode == simulation.OpVitalsUpdate && len(frame.Payload) >= 11 &&
			binary.LittleEndian.Uint32(frame.Payload) == c.ActiveCOS.GID &&
			binary.LittleEndian.Uint32(frame.Payload[7:]) == plain {
			clamped = true
		}
	}
	if !clamped {
		t.Fatal("swap published no HP refresh at the clamped value")
	}
}
