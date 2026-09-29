/*
===========================================================================

cositem_test.go - pet potions, cures and revival

Use shipped item rows to verify that pet operations change the summoned COS,
preserve the owner's player state and publish the correct wire identity.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
shippedItems

Keep retail-data coverage explicit when the source-only suite has no assets.
================
*/
func shippedItems(t *testing.T) *enterworld.TextdataItems {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	return enterworld.NewTextdataItems(dir)
}

/*
================
equipShippedPet

Install an authored COS with depleted vitals so recovery and clamping are visible.
================
*/
func equipShippedPet(t *testing.T, rt *Runtime, c *enterworld.Character, items *enterworld.TextdataItems, codename string) {
	t.Helper()
	ref, ok := items.CharacterRefByCodename(codename)
	if !ok || ref.MaxHP == 0 {
		t.Fatalf("shipped COS %s missing or has no max HP", codename)
	}
	c.ActiveCOS = &enterworld.CharacterCOS{
		GID: 9001, RefObjID: ref.RefObjID, Codename: ref.Codename,
		CurrentHP: 1, CurrentMP: 10, Summoned: true, InventorySlot: 13, Satiety: 100,
	}
	c.MissionInventory = nil
	rt.deps.(*enterworld.Deps).Items = items
}

/*
================
petUse

Encode the real item-use target: a summoned object or a revival inventory slot.
================
*/
func petUse(c *enterworld.Character, ref *enterworld.ItemRef, gid uint32, reviveSlot int) []byte {
	body := []byte{21, byte(ref.TypeFlags()), byte(ref.TypeFlags() >> 8)}
	if reviveSlot >= 0 {
		return append(body, byte(reviveSlot))
	}
	var tail [4]byte
	binary.LittleEndian.PutUint32(tail[:], gid)
	return append(body, tail[:]...)
}

/*
================
TestShippedPetPotionHealsThePetNotThePlayer

The shared item-use path must debit the item and heal only its pet target.
================
*/
func TestShippedPetPotionHealsThePetNotThePlayer(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	items := shippedItems(t)
	potion, ok := items.ItemRefByCodename("ITEM_ETC_COS_HP_POTION_01")
	if !ok || potion.TypeIDs != [4]int64{3, 3, 1, 4} || potion.RecoveryHP != 360 {
		t.Fatalf("shipped pet potion row = %+v", potion)
	}
	equipShippedPet(t, rt, c, items, "COS_P_RABBIT")
	c.Level = testInt64(1)
	c.Strength = testInt64(20)
	c.Intellect = testInt64(20)
	playerHP := *c.CurrentHP
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: potion.RefObjID, Codename: potion.Codename,
		TypeFlags: potion.TypeFlags(), StackCount: 1,
	}}
	// (20/416 + 1) * 1.02^(1-1) * 360 = 377.307, truncated to 377.
	// COS_P_RABBIT's shipped maximum is below that, so the credit clamps.
	const unclamped = 377
	petRef, _ := items.CharacterRefByCodename("COS_P_RABBIT")
	if petRef.MaxHP >= 1+unclamped {
		t.Fatalf("rabbit max %d does not exercise the pet clamp", petRef.MaxHP)
	}
	result := rt.HandleItemUse(testDivision, c, petUse(c, potion, c.ActiveCOS.GID, -1))
	if result.Frames[0].Payload[0] != 1 {
		t.Fatalf("refused %x", result.Frames[0].Payload)
	}
	if c.ActiveCOS.CurrentHP != petRef.MaxHP || *c.CurrentHP != playerHP {
		t.Fatalf("pet HP %d want %d player HP %d", c.ActiveCOS.CurrentHP, petRef.MaxHP, *c.CurrentHP)
	}
	if c.PetPotionCooldowns[0] == 0 || len(c.MissionInventory) != 0 {
		t.Fatal("potion was not consumed onto the 1.1s lane")
	}
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: potion.RefObjID, Codename: potion.Codename,
		TypeFlags: potion.TypeFlags(), StackCount: 1,
	}}
	again := rt.HandleItemUse(testDivision, c, petUse(c, potion, c.ActiveCOS.GID, -1))
	if again.Frames[0].Payload[1] != wire.ErrCodeItemReuseDelay {
		t.Fatalf("reuse %x", again.Frames[0].Payload)
	}
}

/*
================
TestShippedPetCureHitsThePetBlock

The pet cure retires its own status while preserving the owner's player block.
================
*/
func TestShippedPetCureHitsThePetBlock(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	items := shippedItems(t)
	cure, ok := items.ItemRefByCodename("ITEM_COS_P_CURE_ALL_01")
	if !ok || cure.CureLevels != [6]int64{36, 36, 36, 36, 36, 36} {
		t.Fatalf("shipped pet cure levels %+v", cure)
	}
	equipShippedPet(t, rt, c, items, "COS_P_RABBIT")
	record := abnormal.Record{Status: abnormal.Burn, DurationMs: 1000, Level: 1, SourceGID: m.Gid}
	owner := rt.newCosAbnormalOwner(testDivision, c, clock.NowMs())
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	rt.deps.Update(c, "stun", func() bool {
		owner.changed = owner.block.Apply(owner, record, clock.NowMs())
		owner.commit()
		return true
	})
	rt.deps.Update(c, "player-stun", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Stun, DurationMs: 10000, Level: 1, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: cure.RefObjID, Codename: cure.Codename,
		TypeFlags: cure.TypeFlags(), StackCount: 1,
	}}
	result := rt.HandleItemUse(testDivision, c, petUse(c, cure, c.ActiveCOS.GID, -1))
	if result.Frames[0].Payload[0] != 1 {
		t.Fatalf("refused %x", result.Frames[0].Payload)
	}
	if block := rt.cosAbnormal(testDivision, c.Name, c.ActiveCOS.GID); block != nil && block.Has(abnormal.Burn) {
		t.Fatal("pet cure left the pet burning")
	}
	if block := rt.playerAbnormal(testDivision, c.Name); block == nil || !block.Has(abnormal.Stun) {
		t.Fatal("pet cure cleared the player")
	}
	// A COS has no 0x36C7 (4A5C60 is player-only); its mask rides 33A6.
	if saw(result.Frames, 0x36C7) || !hasPetMask(result.Broadcast, c.ActiveCOS.GID) {
		t.Fatal("pet cure publication", result.Frames, result.Broadcast)
	}
}

/*
================
TestShippedPetRevivalRestoresRefVitals

Revival restores authored pet vitals once and refuses a second consumption.
================
*/
func TestShippedPetRevivalRestoresRefVitals(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	items := shippedItems(t)
	scroll, ok := items.ItemRefByCodename("ITEM_COS_P_REVIVAL")
	if !ok || scroll.TypeIDs != [4]int64{3, 3, 1, 6} {
		t.Fatal("revival row missing")
	}
	ref, ok := items.CharacterRefByCodename("COS_P_RABBIT")
	if !ok {
		t.Fatal("rabbit ref missing")
	}
	equipShippedPet(t, rt, c, items, "COS_P_RABBIT")
	c.ActiveCOS.CurrentHP = 0
	c.ActiveCOS.Summoned = false
	c.ActiveCOS.StateFlags = 0
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
		TypeFlags: scroll.TypeFlags(), StackCount: 1,
	}}
	result := rt.HandleItemUse(testDivision, c, petUse(c, scroll, 0, int(c.ActiveCOS.InventorySlot)))
	if result.Frames[0].Payload[0] != 1 {
		t.Fatalf("refused %x", result.Frames[0].Payload)
	}
	if c.ActiveCOS.CurrentHP != ref.MaxHP || c.ActiveCOS.CurrentMP != ref.MaxMP || c.ActiveCOS.Satiety != 3000 || c.ActiveCOS.StateFlags&1 == 0 {
		t.Fatalf("revived %+v want hp %d mp %d", c.ActiveCOS, ref.MaxHP, ref.MaxMP)
	}
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: 21, RefObjID: scroll.RefObjID, Codename: scroll.Codename,
		TypeFlags: scroll.TypeFlags(), StackCount: 1,
	}}
	again := rt.HandleItemUse(testDivision, c, petUse(c, scroll, 0, int(c.ActiveCOS.InventorySlot)))
	if again.Frames[0].Payload[0] == 1 {
		t.Fatal("second revival consumed another scroll")
	}
}

/*
================
TestMonsterHitRollsStatusOntoThePet

Resolve a summoned target through the real monster attack path and keep the
rolled status separate from its owning character's block.
================
*/
func TestMonsterHitRollsStatusOntoThePet(t *testing.T) {
	rt, clock, c, mon := newCombatTestRuntime(t, 100)
	c.ActiveCOS = &enterworld.CharacterCOS{
		GID: 9001, RefObjID: 1, Codename: "COS_P_RABBIT", CurrentHP: 1_000_000, Summoned: true,
	}
	mon.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	row := skills[2]
	row.Attack.Min, row.Attack.Max, row.Attack.Percent = 100, 100, 100
	burn, _ := abnormal.SourceIndex(0x6275)
	row.Abnormal.Params[burn].Present = true
	row.Abnormal.Params[burn].Args = [6]uint32{100, 100}
	skills[2] = row
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	seen := false
	for _, ch := range rt.deps.CharactersForDivision(testDivision) {
		if ch.ActiveCOS != nil && ch.ActiveCOS.GID == c.ActiveCOS.GID && ch.ActiveCOS.Summoned {
			seen = true
		}
	}
	if !seen {
		t.Fatal("division list does not contain the summoned pet")
	}
	result := rt.MonsterBasicAttack(testDivision, mon, c.ActiveCOS.GID, 2, clock.NowMs())
	if !result.Accepted || c.ActiveCOS.CurrentHP == 1_000_000 {
		t.Fatalf("hit %v refusal %d hp %d", result.Accepted, result.Refusal, c.ActiveCOS.CurrentHP)
	}
	block := rt.cosAbnormal(testDivision, c.Name, c.ActiveCOS.GID)
	if block == nil || !block.Has(abnormal.Burn) {
		t.Fatal("monster hit did not roll burn onto the pet")
	}
	if player := rt.playerAbnormal(testDivision, c.Name); player != nil && player.Has(abnormal.Burn) {
		t.Fatal("pet hit wrote the player block")
	}
}
