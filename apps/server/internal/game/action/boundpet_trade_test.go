/*
===========================================================================

boundpet_trade_test.go - a bound row inside a pet at the trade doors

The chain of #4491 end to end over the wire: a bound row moves from a
bag into a pickup pet's bag (a move that stays open, as the pet is the
owner's own container), the pet is dismissed, and its summoner row is
put on the exchange table. Tradeable judged only the summoner row's
own marks, so the pet crossed and the buyer could summon it and take
the bound row out - the guild warehouse leak of #4479 at the
player-to-player doors. The control crosses the same table with an
unbound load and the buyer takes the row out, proving the refusal is
the bound mark alone. The chest test covers the legacy stock: a pet
that entered a guild chest before the deposit gate existed is refused
on the way out as well.

===========================================================================
*/
package action

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

// tradePetStoneCodename is the fixture's mall stone: an ordinary
// stackable whose only special property is the row's bound mark.
const tradePetStoneCodename = "ITEM_ETC_TRADEPET_STONE"

// tradePetMallCodename is a record-bound mall row (canTrade 0, #4217):
// it carries no mark of its own, and its reference row alone refuses it.
const tradePetMallCodename = "ITEM_MALL_TRADEPET_BOUND"

/*
================
tradePetRefs

The item source of the pet fixtures: the shipped test rows plus a
pickup-pet summoner, its character row and the stone.
================
*/
func tradePetRefs() cosTestItemSource {
	refs := testCosSource(testItems())
	refs.staticItemSource["SUMMON_PICKUP"] = &enterworld.ItemRef{
		Codename: "SUMMON_PICKUP", RefObjID: 901, TypeIDs: [4]int64{3, 2, 1, 2}, Country: 3,
		AssociatedCharacterCodename: "PICKUP",
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "canTrade": 1, "itemParam1_29c": 60, "canBorrow": 255, "keepingFee": 0}),
	}
	refs.characters["PICKUP"] = &enterworld.CharacterRef{
		Codename: "PICKUP", RefObjID: 951, TidWord: 0x1c6 | 4<<11,
		Level: 1, MaxHP: 100, RunSpeed: 60, WalkSpeed: 20, Scale: 100, InventoryCapacity: 28,
	}
	refs.staticItemSource[tradePetStoneCodename] = &enterworld.ItemRef{
		Codename: tradePetStoneCodename, RefObjID: 11460, TypeIDs: [4]int64{3, 3, 1, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canTrade": 1}),
	}
	refs.staticItemSource[tradePetMallCodename] = &enterworld.ItemRef{
		Codename: tradePetMallCodename, RefObjID: 11461, TypeIDs: [4]int64{3, 3, 1, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canTrade": 0}),
	}
	return refs
}

/*
================
tradePetBag

The seller's starting bag: a bound stone at 20, an unbound one at 21,
a record-bound mall row at 22 and the pickup-pet summoner at 23.
================
*/
func tradePetBag(refs cosTestItemSource) []enterworld.InventoryRow {
	return []enterworld.InventoryRow{
		{Slot: 20, RefObjID: 11460, Codename: tradePetStoneCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1, ForgeBound: true},
		{Slot: 21, RefObjID: 11460, Codename: tradePetStoneCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1},
		{Slot: 22, RefObjID: 11461, Codename: tradePetMallCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1},
		{Slot: 23, RefObjID: 901, Codename: "SUMMON_PICKUP",
			TypeFlags: refs.staticItemSource["SUMMON_PICKUP"].TypeFlags(), StackCount: 1},
	}
}

/*
================
boundPetTradeFixture

A store-backed pair of players; the first carries the pet and the
two stones.
================
*/
func boundPetTradeFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, cosTestItemSource) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)

	refs := tradePetRefs()
	level := int64(5)
	seller := testCharacter()
	seller.Level, seller.MaxLevel = &level, &level
	seller.MissionInventory = tradePetBag(refs)
	buyer := testCharacter()
	buyer.ID, buyer.Name, buyer.MissionInventory = 4, "Buyer", nil
	buyer.Level, buyer.MaxLevel = &level, &level
	if err := authority.CreateCharacter(testDivision, "seller-account", seller); err != nil {
		t.Fatalf("CreateCharacter(seller): %v", err)
	}
	if err := authority.CreateCharacter(testDivision, "buyer-account", buyer); err != nil {
		t.Fatalf("CreateCharacter(buyer): %v", err)
	}
	var a, b *enterworld.Character
	for _, c := range authority.Characters().CharactersForDivision(testDivision) {
		switch c.Name {
		case seller.Name:
			a = c
		case buyer.Name:
			b = c
		}
	}
	if a == nil || b == nil {
		t.Fatal("fixture characters not in the authority")
	}

	skills := staticSkillSource{}
	for _, id := range []uint32{1, 2, 40, 70, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970} {
		skills[id] = enterworld.SkillRow{ID: id}
	}
	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Items:      refs,
		Skills:     skills,
	}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.MutateCharacters = authority.MutateCharacters
	deps.UpdateCharacters = authority.UpdateCharacters
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	rt.BindPetSession(testDivision, a, 101)
	rt.BindPetSession(testDivision, b, 102)
	return rt, a, b, refs
}

/*
================
tradePetGID

The fixture character's one pet, summoned or dormant.
================
*/
func tradePetGID(t *testing.T, c *enterworld.Character) uint32 {
	t.Helper()
	pets := c.Companions()
	if len(pets) != 1 {
		t.Fatalf("%s holds %d companions", c.Name, len(pets))
	}
	return pets[0].GID
}

/*
================
loadTradePetAndDismiss

Summon the pickup pet, move the stone at bagSlot into its bag over
the wire and dismiss it: the summoner row back in the bag carries the
load in its container.
================
*/
func loadTradePetAndDismiss(t *testing.T, rt *Runtime, c *enterworld.Character, refs cosTestItemSource, bagSlot uint8) {
	t.Helper()
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_PICKUP"])
	gid := tradePetGID(t, c)
	r := trade(t, rt, c, wire.ItemMoveRequest{
		MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: bagSlot, DestSlot: 0})
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("bag-to-pet move refused: %+v", r.Frames)
	}
	res := rt.HandleCosCancel(testDivision, c, wire.NewWriter(4).U32(gid).Payload())
	if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 {
		t.Fatalf("dismiss refused: %+v", res.Frames)
	}
	row := bagRowByCodename(c, "SUMMON_PICKUP")
	if row == nil || row.Summon == nil || row.Summon.Container == nil || len(row.Summon.Container.Rows) != 1 {
		t.Fatalf("dismissed pet carries %+v", row)
	}
}

/*
================
openTradeExchange

The seller asks, the buyer consents: the table stands open.
================
*/
func openTradeExchange(t *testing.T, rt *Runtime, a, b *enterworld.Character) {
	t.Helper()
	gidB := enterworld.ObjectIDForCharacter(b)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(gidB).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	if !rt.Exchanges.Trading(testDivision, a.Name) {
		t.Fatal("the exchange never opened")
	}
}

/*
================
TestBoundRowInsideAPetStaysOffTheExchangeTable
================
*/
func TestBoundRowInsideAPetStaysOffTheExchangeTable(t *testing.T) {
	rt, a, b, refs := boundPetTradeFixture(t)

	// The bound stone rides into the pet's bag - the owner's own
	// container stays open to it - and the pet is dismissed.
	loadTradePetAndDismiss(t, rt, a, refs, 20)
	if row := bagRowByCodename(a, "SUMMON_PICKUP"); !row.Summon.Container.Rows[0].ForgeBound {
		t.Fatal("the dismissed pet does not carry the bound stone")
	}

	// The table refuses the loaded pet with the cannot-trade answer,
	// so the buyer never holds it.
	openTradeExchange(t, rt, a, b)
	put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 23})
	if len(put.Frames) != 1 || !bytes.Equal(put.Frames[0].Payload, []byte{2, wire.ExchangeErrCannotTrade}) {
		t.Fatalf("exchange put of a bound-loaded pet = %+v, want the cannot-trade refusal", put.Frames)
	}
	if bagRowByCodename(a, "SUMMON_PICKUP") == nil {
		t.Fatal("the refused put took the pet off the seller")
	}
	if bagRowByCodename(b, "SUMMON_PICKUP") != nil {
		t.Fatal("the buyer holds the pet after the refusal")
	}
}

/*
================
TestRecordBoundRowInsideAPetStaysOffTheExchangeTable

A mall row the reference refuses (canTrade 0) carries no mark of its
own; the pet carrying it is refused by the same judgment as the row.
================
*/
func TestRecordBoundRowInsideAPetStaysOffTheExchangeTable(t *testing.T) {
	rt, a, b, refs := boundPetTradeFixture(t)

	loadTradePetAndDismiss(t, rt, a, refs, 22)
	openTradeExchange(t, rt, a, b)
	put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 23})
	if len(put.Frames) != 1 || !bytes.Equal(put.Frames[0].Payload, []byte{2, wire.ExchangeErrCannotTrade}) {
		t.Fatalf("exchange put of a pet carrying a record-bound row = %+v, want the cannot-trade refusal", put.Frames)
	}
	if bagRowByCodename(b, "SUMMON_PICKUP") != nil {
		t.Fatal("the buyer holds the pet after the refusal")
	}
}

/*
================
TestUnboundRowInsideAPetCrossesTheExchangeTable

The same chain with an unbound stone: the pet crosses, the buyer
summons it and takes the row out. The refusal above is the bound
mark alone.
================
*/
func TestUnboundRowInsideAPetCrossesTheExchangeTable(t *testing.T) {
	rt, a, b, refs := boundPetTradeFixture(t)

	loadTradePetAndDismiss(t, rt, a, refs, 21)
	openTradeExchange(t, rt, a, b)
	put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 23})
	if len(put.Frames) != 1 || put.Frames[0].Payload[0] != 1 {
		t.Fatalf("exchange put of an unbound-loaded pet = %+v", put.Frames)
	}
	rt.HandleExchangeConfirm(testDivision, a, nil)
	rt.HandleExchangeConfirm(testDivision, b, nil)
	rt.HandleExchangeApprove(testDivision, a, nil)
	rt.HandleExchangeApprove(testDivision, b, nil)
	pet := bagRowByCodename(b, "SUMMON_PICKUP")
	if pet == nil {
		t.Fatalf("the buyer holds no pet after the swap: %+v", b.MissionInventory)
	}

	// The buyer summons the pet and takes the row out of its bag.
	useSummonerFixture(t, rt, b, uint8(pet.Slot), refs.staticItemSource["SUMMON_PICKUP"])
	r := trade(t, rt, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeCosToPlayer, CosGID: tradePetGID(t, b), SourceSlot: 0, DestSlot: 20})
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("buyer taking the row out of the pet = %+v", r.Frames)
	}
	if row := bagRowByCodename(b, tradePetStoneCodename); row == nil || row.ForgeBound {
		t.Fatalf("the buyer's bag holds %+v", b.MissionInventory)
	}
}

/*
================
boundPetChestFixture

A store-backed pair of guild members at a guild manager, with two
pets already in the chest - legacy stock from before the deposit
gate: one carries a bound stone, one an unbound stone, one a
record-bound mall row.
================
*/
func boundPetChestFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)

	refs := tradePetRefs()
	level := int64(5)
	master := testCharacter()
	master.Level, master.MaxLevel = &level, &level
	master.MissionInventory = nil
	member := testCharacter()
	member.ID, member.Name, member.MissionInventory = 4, "Member", nil
	member.Level, member.MaxLevel = &level, &level
	if err := authority.CreateCharacter(testDivision, "master-account", master); err != nil {
		t.Fatalf("CreateCharacter(master): %v", err)
	}
	if err := authority.CreateCharacter(testDivision, "member-account", member); err != nil {
		t.Fatalf("CreateCharacter(member): %v", err)
	}
	var a, b *enterworld.Character
	for _, c := range authority.Characters().CharactersForDivision(testDivision) {
		switch c.Name {
		case master.Name:
			a = c
		case member.Name:
			b = c
		}
	}
	if a == nil || b == nil {
		t.Fatal("fixture characters not in the authority")
	}

	skills := staticSkillSource{}
	for _, id := range []uint32{1, 2, 40, 70, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970} {
		skills[id] = enterworld.SkillRow{ID: id}
	}
	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Items:      refs,
		Skills:     skills,
		Guilds:     authority.Guilds(),
	}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
	}
	deps.MutateCharacters = authority.MutateCharacters
	deps.UpdateCharacters = authority.UpdateCharacters
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())

	guilds := authority.Guilds()
	if _, err := guilds.CreateGuild(testDivision, enterworld.GuildRecord{Name: "Tradebound", Level: 5, GP: 6000},
		enterworld.GuildMemberRecord{CharID: a.ID, JID: 1, Name: a.Name, Grade: 0, PermMask: 0xffffffff,
			FortressRole: guild.FortressRoleCommander}, a); err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	guildID, _ := guilds.GuildOfCharacter(testDivision, a.ID)
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, a.ID, 0, enterworld.GuildMemberRecord{
		CharID: b.ID, JID: 2, Name: b.Name, Grade: guild.JoinerGrade, PermMask: 0xffffffff}); refusal.Refused() {
		t.Fatalf("fixture join: %v", refusal)
	}

	// The legacy stock, written as a pre-gate deposit left it.
	chestPet := func(slot int64, gid uint32, codename string, bound bool) domain.InventoryRow {
		return domain.InventoryRow{
			Slot: slot, RefObjID: 901, Codename: "SUMMON_PICKUP",
			TypeFlags: refs.staticItemSource["SUMMON_PICKUP"].TypeFlags(), StackCount: 1,
			Summon: &domain.CharacterCOS{
				GID: gid, RefObjID: 951, Codename: "PICKUP",
				Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{
					{Slot: 0, RefObjID: refs.staticItemSource[codename].RefObjID, Codename: codename,
						TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1, ForgeBound: bound},
				}},
			},
		}
	}
	if _, refusal, err := guilds.TransactGuildStorageAs(testDivision, a.ID,
		func(_ *domain.Character, chest *domain.AccountStorage) error {
			chest.Rows = append(chest.Rows, chestPet(0, 991, tradePetStoneCodename, true),
				chestPet(1, 992, tradePetStoneCodename, false), chestPet(2, 993, tradePetMallCodename, false))
			return nil
		}); err != nil || refusal.Refused() {
		t.Fatalf("seed the chest: refusal %v, err %v", refusal, err)
	}

	manager := simulation.NpcDef{ObjectID: guildManagerGid, RefObjID: 7600, Codename: "NPC_EU_GUILD",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(a).Spawn}
	manager.Services = simulation.ResolveNpcServices(manager)
	rt.NpcRoster = []simulation.NpcDef{manager}
	rt.NpcSpawn.Enabled = true
	return rt, a, b
}

/*
================
openTradePetChest

Open the guild warehouse for a member at the fixture manager.
================
*/
func openTradePetChest(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	rt.characterAdmissions.Store(simulation.WorldKey(testDivision, c.Name),
		populationAdmission{division: testDivision, name: c.Name})
	rt.Selected.Set(testDivision, c.Name, guildManagerGid)
	out := rt.HandleGuildStorageOpen(testDivision, c, wire.NewWriter(4).U32(guildManagerGid).Payload())
	if len(out.Frames) == 0 || !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("%s could not open the guild warehouse: %+v", c.Name, out.Frames)
	}
}

/*
================
TestBoundLoadedPetStaysInTheGuildChest
================
*/
func TestBoundLoadedPetStaysInTheGuildChest(t *testing.T) {
	rt, _, b := boundPetChestFixture(t)

	// A member who never deposited the pet asks for it: the chest
	// refuses with the deposit door's own code, and nothing moves.
	openTradePetChest(t, rt, b)
	res := rt.applyGuildStorageMove(testDivision, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageWithdraw, SourceSlot: 0, DestSlot: 20, NpcGID: guildManagerGid})
	if len(res.Frames) != 1 || !bytes.Equal(res.Frames[0].Payload, []byte{2, errCodeInvalidStorageTarget}) {
		t.Fatalf("guild withdraw of a bound-loaded pet = %+v, want the storage-target refusal", res.Frames)
	}
	if bagRowByCodename(b, "SUMMON_PICKUP") != nil {
		t.Fatal("the refused withdraw took the pet out of the chest")
	}
	// The record-bound mall row stays in the chest the same way.
	res = rt.applyGuildStorageMove(testDivision, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageWithdraw, SourceSlot: 2, DestSlot: 20, NpcGID: guildManagerGid})
	if len(res.Frames) != 1 || !bytes.Equal(res.Frames[0].Payload, []byte{2, errCodeInvalidStorageTarget}) {
		t.Fatalf("guild withdraw of a pet carrying a record-bound row = %+v, want the storage-target refusal", res.Frames)
	}
	if bagRowByCodename(b, "SUMMON_PICKUP") != nil {
		t.Fatal("the refused withdraw took the record-bound pet out of the chest")
	}

	// Control: the unbound-loaded pet beside it withdraws as always.
	res = rt.applyGuildStorageMove(testDivision, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageWithdraw, SourceSlot: 1, DestSlot: 20, NpcGID: guildManagerGid})
	if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild withdraw of an unbound-loaded pet = %+v", res.Frames)
	}
	if bagRowByCodename(b, "SUMMON_PICKUP") == nil {
		t.Fatal("the admitted withdraw left the pet in the chest")
	}
}
