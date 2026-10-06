/*
===========================================================================

mercenary_test.go - guild-soldier consumption, ownership and cooldown

Exercise the real guild/character store door, including restart and refusal.

===========================================================================
*/
package action

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
mercenaryFixture
================
*/
func mercenaryFixture(t *testing.T) (*doorRuntime, *enterworld.ItemRef) {
	t.Helper()
	d := guildManagerFixture(t)
	c := d.character
	refs := testCosSource(testItems())
	item := &enterworld.ItemRef{Codename: "GUILD_SCROLL", RefObjID: 9900, TypeIDs: [4]int64{3, 3, 12, 1}, Country: 3,
		AssociatedCharacterCodename: "SOLDIER_ONE", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 20})}
	refs.staticItemSource[item.Codename] = item
	for i, name := range []string{"SOLDIER_ONE", "SOLDIER_TWO"} {
		ref := &enterworld.CharacterRef{Codename: name, GroupCodename: "SOLDIERS", RefObjID: uint32(9910 + i), TidWord: 0x1c6 | 5<<11,
			Level: uint8(i + 1), MaxHP: 100, MaxMP: 50, RunSpeed: 60, WalkSpeed: 20, Scale: 100}
		ref.Parameters.Country = uint8(enterworld.NativeCountryByte9C(c))
		refs.characters[name] = ref
	}
	d.rt.deps.(*enterworld.Deps).Items = refs
	d.authority.UpdateCharacter(c, "mercenary-fixture", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 23, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 2})
		return true
	})
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, c.ID, "mercenary-guild-level", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.Level = 5
			return g, m, true
		})
	if refusal.Refused() {
		t.Fatal(refusal)
	}
	d.rt.Now = func() time.Time { return time.Unix(1000, 0) }
	d.rt.BindPetSession(testDivision, c, 1)
	return d, item
}

/*
================
TestMercenarySummonAndDismissPreserveCooldown
================
*/
func TestMercenarySummonAndDismissPreserveCooldown(t *testing.T) {
	d, item := mercenaryFixture(t)
	c := d.character
	useSummonerFixture(t, d.rt, c, 23, item)
	if len(c.Mercenaries) != 6 || c.MercenarySummonUntilMs != 2200000 {
		t.Fatalf("summon state: %d, %d", len(c.Mercenaries), c.MercenarySummonUntilMs)
	}
	seen := map[uint32]bool{}
	for _, pet := range c.Mercenaries {
		if seen[pet.GID] || !c.OwnsMercenaryID(pet.GID) || pet.RentalExpiresAtUnix != 2200 {
			t.Fatalf("soldier: %+v", pet)
		}
		seen[pet.GID] = true
	}
	request := wire.NewWriter(3).U8(23).U16(item.TypeFlags()).Payload()
	if got := d.rt.HandleItemUse(testDivision, c, request); !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryAlreadySummoned).Frames[0].Payload) {
		t.Fatalf("duplicate: %+v", got)
	}
	out := d.rt.HandleMercenaryDismiss(testDivision, c, nil)
	if len(out.Frames) != 6 || len(out.Broadcast) != 6 || len(c.Mercenaries) != 0 || c.MercenarySummonUntilMs != 2200000 {
		t.Fatalf("dismiss: %+v", out)
	}
	if got := d.rt.HandleItemUse(testDivision, c, request); !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryCooldownActive).Frames[0].Payload) {
		t.Fatalf("cooldown: %+v", got)
	}
	d.rt.Now = func() time.Time { return time.Unix(2200, 0) }
	d.rt.advanceMercenaryCooldowns(2200000)
	useSummonerFixture(t, d.rt, c, 23, item)
	for _, row := range c.MissionInventory {
		if row.Slot == 23 {
			t.Fatal("two summons did not consume two scrolls")
		}
	}
}

/*
================
TestMercenaryRecordOmitsPetTail
================
*/
func TestMercenaryRecordOmitsPetTail(t *testing.T) {
	ref := &enterworld.CharacterRef{RefObjID: 9, TidWord: 0x1c6 | 5<<11}
	pet := &enterworld.CharacterCOS{GID: 7, RefObjID: 9, CurrentHP: 100, CurrentMP: 50}
	p, err := enterworld.BuildCOSRecord(pet, ref, nil)
	want := wire.NewWriter(17).U32(7).U32(9).U32(100).U32(50).U8(0).Payload()
	if err != nil || !bytes.Equal(p, want) {
		t.Fatalf("record %x: %v", p, err)
	}
}

/*
================
TestMercenaryQuestRestrictionPreservesInventory

49AFE0 gives the quest restriction precedence over the guild and cooldown.
================
*/
func TestMercenaryQuestRestrictionPreservesInventory(t *testing.T) {
	d, item := mercenaryFixture(t)
	d.rt.QuestTravelBlocks = func(*enterworld.Character) uint32 { return mercenaryQuestOperationMask }
	before := append([]enterworld.InventoryRow(nil), d.character.MissionInventory...)
	request := wire.NewWriter(3).U8(23).U16(item.TypeFlags()).Payload()
	got := d.rt.HandleItemUse(testDivision, d.character, request)
	if len(got.Frames) != 1 || !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryQuestRestricted).Frames[0].Payload) {
		t.Fatalf("quest refusal: %+v", got)
	}
	if len(d.character.Mercenaries) != 0 || d.character.MercenarySummonUntilMs != 0 {
		t.Fatal("refusal created soldier state")
	}
	if !reflect.DeepEqual(before, d.character.MissionInventory) {
		t.Fatal("refusal consumed inventory")
	}
}

/*
================
TestMercenaryAcquiresMonsterWithoutOwnerOrder
================
*/
func TestMercenaryAcquiresMonsterWithoutOwnerOrder(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 1000000, domain.MercenaryBand)
	before, _ := rt.Monsters.Get(testDivision, m.Gid)
	tickPetCombat(t, rt, clock, 100, func() bool {
		live, _ := rt.Monsters.Get(testDivision, m.Gid)
		return live.CurrentHP < before.CurrentHP
	})
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if state == nil || state.combat == nil || state.combat.target != m.Gid {
		t.Fatal("soldier did not retain acquired enemy")
	}
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	if live.Opponents[0].GID != c.ActiveCOS.GID {
		t.Fatalf("retaliation targeted %d instead of soldier %d", live.Opponents[0].GID, c.ActiveCOS.GID)
	}

}

/*
================
TestMercenaryNormalEnemyUsesTargetStateAndBothLevelFloors
================
*/
func TestMercenaryNormalEnemyUsesTargetStateAndBothLevelFloors(t *testing.T) {
	rt, _, owner, _ := newCombatTestRuntime(t, 1000000)
	level := int64(20)
	owner.Level = &level
	target := owner.Snapshot()
	target.ID++
	if rt.mercenaryNormalEnemy(testDivision, owner, target) {
		t.Fatal("neutral bystander acquired")
	}
	owner.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(target): 20}
	if rt.mercenaryNormalEnemy(testDivision, owner, target) {
		t.Fatal("owner aggression authorized a neutral target")
	}
	target.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(owner): 1}
	if !rt.mercenaryNormalEnemy(testDivision, owner, target) {
		t.Fatal("target aggression record ignored")
	}
	young := int64(19)
	target.Level = &young
	if rt.mercenaryNormalEnemy(testDivision, owner, target) {
		t.Fatal("underlevel target acquired")
	}
	target.Level, owner.Level = &level, &young
	if rt.mercenaryNormalEnemy(testDivision, owner, target) {
		t.Fatal("underlevel owner acquired player")
	}
}

/*
================
TestMercenaryCooldownPresenceSurvivesDeadlineUntilPoll
================
*/
func TestMercenaryCooldownPresenceSurvivesDeadlineUntilPoll(t *testing.T) {
	d, item := mercenaryFixture(t)
	useSummonerFixture(t, d.rt, d.character, 23, item)
	d.rt.HandleMercenaryDismiss(testDivision, d.character, nil)
	d.rt.forgetPetSession(testDivision, d.character.Name)
	d.rt.Now = func() time.Time { return time.Unix(2201, 0) }
	d.rt.BindPetSession(testDivision, d.character, 2)
	request := wire.NewWriter(3).U8(23).U16(item.TypeFlags()).Payload()
	for _, now := range []int64{2201000, 2210999} {
		d.rt.advanceMercenaryCooldowns(now)
		got := d.rt.HandleItemUse(testDivision, d.character, request)
		if !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryCooldownActive).Frames[0].Payload) {
			t.Fatalf("expired job vanished before poll at %d", now)
		}
	}
	d.rt.advanceMercenaryCooldowns(2211000)
	if d.character.MercenarySummonUntilMs != 0 {
		t.Fatal("expired job survived native poll")
	}
}

/*
================
TestMercenaryStoreRestartAndExpiredEntry
================
*/
func TestMercenaryStoreRestartAndExpiredEntry(t *testing.T) {
	d, item := mercenaryFixture(t)
	useSummonerFixture(t, d.rt, d.character, 23, item)
	refs := d.rt.deps.ItemReferences()
	before := d.character.Snapshot()
	restarted := d.reboot(t)
	if !reflect.DeepEqual(before.Mercenaries, restarted.character.Mercenaries) || restarted.character.MercenarySummonUntilMs != 2200000 {
		t.Fatal("restart lost soldiers or penalty")
	}
	if row := bagRowByCodename(restarted.character, item.Codename); row == nil || row.StackCount != 1 {
		t.Fatal("restart lost scroll debit")
	}
	restarted.rt.deps.(*enterworld.Deps).Items = refs
	restarted.rt.Now = func() time.Time { return time.Unix(2201, 0) }
	restarted.rt.restoreCharacterCOS(testDivision, restarted.character.Name)
	if len(restarted.character.Mercenaries) != 0 || restarted.character.MercenarySummonUntilMs != 2200000 {
		t.Fatal("expired entry must retire soldiers while retaining the timed job until its poll")
	}
	entry := MercenaryCooldownFrames(restarted.character, 2201000)
	if len(entry) != 1 || !bytes.Equal(entry[0].Payload, []byte{2, 4, 255, 255, 255, 255}) {
		t.Fatalf("signed expired entry penalty: %+v", entry)
	}
}

/*
================
TestMercenaryCountTables
================
*/
func TestMercenaryCountTables(t *testing.T) {
	for level, normal := range []int{0, 0, 0, 1, 3, 6, 0} {
		union := []int{0, 0, 0, 1, 5, 10, 0}[level]
		if domain.MercenaryCount(uint8(level), false) != normal || domain.MercenaryCount(uint8(level), true) != union {
			t.Fatalf("guild level %d", level)
		}
	}
}

/*
================
TestMercenaryPenaltyBlocksMasterTransferBeforeHeirLookup
================
*/
func TestMercenaryPenaltyBlocksMasterTransferBeforeHeirLookup(t *testing.T) {
	d, item := mercenaryFixture(t)
	useSummonerFixture(t, d.rt, d.character, 23, item)
	d.rt.HandleMercenaryDismiss(testDivision, d.character, nil)
	for _, step := range []struct {
		target uint32
		code   uint8
	}{{1, guildNpcRefused}, {99, guildMasterMercenaryCooldown}} {
		out := d.rt.HandleGuildMasterLeave(testDivision, d.character, wire.NewWriter(8).U32(guildManagerGid).U32(step.target).Payload())
		if !bytes.Equal(out.Frames[0].Payload, []byte{2, step.code}) {
			t.Fatalf("target %d: %+v", step.target, out)
		}
	}
}
