/*
===========================================================================

boundpet_guildstorage_test.go - a bound row inside a pet and the guild warehouse

The chain of #4479 end to end over the wire: a bound row moves from a
bag into a pickup pet's bag (a move native leaves open, as the pet is
the owner's own container), the pet is dismissed, and its summoner row
is deposited at the guild warehouse. The guild door refuses the direct
row already (forge_bound_test.go); the summoner row must not smuggle
the bound row past it, or any member could withdraw the pet and take
the row out. The controls prove the refusal is the bound mark alone:
the same pet loaded with an unbound row deposits, another member
withdraws it and takes the row, and the personal warehouse - the
owner's own round-trip - takes the bound-loaded pet as it always has.

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

// petBoundStoneCodename is the fixture's mall stone: an ordinary
// stackable whose only special property is the row's bound mark.
const petBoundStoneCodename = "ITEM_ETC_PETBOUND_STONE"

/*
================
boundPetGuildFixture

A store-backed pair of guild members at a guild manager: the master
holds a pickup-pet summoner and two stones, one bound; the member
waits to withdraw whatever reaches the chest.
================
*/
func boundPetGuildFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, cosTestItemSource) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)

	refs := testCosSource(testItems())
	refs.staticItemSource["SUMMON_PICKUP"] = &enterworld.ItemRef{
		Codename: "SUMMON_PICKUP", RefObjID: 901, TypeIDs: [4]int64{3, 2, 1, 2}, Country: 3,
		AssociatedCharacterCodename: "PICKUP",
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "itemParam1_29c": 60, "canBorrow": 255, "keepingFee": 0}),
	}
	refs.characters["PICKUP"] = &enterworld.CharacterRef{
		Codename: "PICKUP", RefObjID: 951, TidWord: 0x1c6 | 4<<11,
		Level: 1, MaxHP: 100, RunSpeed: 60, WalkSpeed: 20, Scale: 100, InventoryCapacity: 28,
	}
	refs.staticItemSource[petBoundStoneCodename] = &enterworld.ItemRef{
		Codename: petBoundStoneCodename, RefObjID: 11460, TypeIDs: [4]int64{3, 3, 1, 1},
	}

	master := testCharacter()
	level := int64(5)
	master.Level, master.MaxLevel = &level, &level
	master.MissionInventory = []enterworld.InventoryRow{
		{Slot: 20, RefObjID: 11460, Codename: petBoundStoneCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1, ForgeBound: true},
		{Slot: 21, RefObjID: 11460, Codename: petBoundStoneCodename,
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 1},
		{Slot: 23, RefObjID: 901, Codename: "SUMMON_PICKUP",
			TypeFlags: refs.staticItemSource["SUMMON_PICKUP"].TypeFlags(), StackCount: 1},
	}
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
	if _, err := guilds.CreateGuild(testDivision, enterworld.GuildRecord{Name: "Petbound", Level: 5, GP: 6000},
		enterworld.GuildMemberRecord{CharID: a.ID, JID: 1, Name: a.Name, Grade: 0, PermMask: 0xffffffff,
			FortressRole: guild.FortressRoleCommander}, a); err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	guildID, _ := guilds.GuildOfCharacter(testDivision, a.ID)
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, a.ID, 0, enterworld.GuildMemberRecord{
		CharID: b.ID, JID: 2, Name: b.Name, Grade: guild.JoinerGrade, PermMask: 0xffffffff}); refusal.Refused() {
		t.Fatalf("fixture join: %v", refusal)
	}

	manager := simulation.NpcDef{ObjectID: guildManagerGid, RefObjID: 7600, Codename: "NPC_EU_GUILD",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(a).Spawn}
	manager.Services = simulation.ResolveNpcServices(manager)
	keeper := simulation.NpcDef{ObjectID: 17, RefObjID: 7601, Codename: "NPC_KEEPER",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(a).Spawn}
	keeper.TalkFlags = simulation.NpcTalkFlagStorage
	rt.NpcRoster = []simulation.NpcDef{manager, keeper}
	rt.NpcSpawn.Enabled = true
	rt.BindPetSession(testDivision, a, 101)
	rt.BindPetSession(testDivision, b, 102)
	return rt, a, b, refs
}

/*
================
summonedPetGID

The fixture character's one pet, summoned or dormant.
================
*/
func summonedPetGID(t *testing.T, c *enterworld.Character) uint32 {
	t.Helper()
	pets := c.Companions()
	if len(pets) != 1 {
		t.Fatalf("%s holds %d companions", c.Name, len(pets))
	}
	return pets[0].GID
}

/*
================
loadPetAndDismiss

Summon the pickup pet, move the stone at bagSlot into its bag over
the wire and dismiss it: the summoner row back in the bag carries the
load in its container.
================
*/
func loadPetAndDismiss(t *testing.T, rt *Runtime, c *enterworld.Character, refs cosTestItemSource, bagSlot uint8) {
	t.Helper()
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_PICKUP"])
	gid := summonedPetGID(t, c)
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
openBoundPetGuildWarehouse

Open the guild warehouse for a member at the fixture manager.
================
*/
func openBoundPetGuildWarehouse(t *testing.T, rt *Runtime, c *enterworld.Character) {
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
TestBoundRowInsideAPetNeverReachesTheGuildWarehouse
================
*/
func TestBoundRowInsideAPetNeverReachesTheGuildWarehouse(t *testing.T) {
	rt, a, b, refs := boundPetGuildFixture(t)

	// The bound stone rides into the pet's bag and the pet is dismissed.
	loadPetAndDismiss(t, rt, a, refs, 20)
	if row := bagRowByCodename(a, "SUMMON_PICKUP"); !row.Summon.Container.Rows[0].ForgeBound {
		t.Fatal("the dismissed pet does not carry the bound stone")
	}

	// The guild door refuses the loaded pet, with its own code.
	openBoundPetGuildWarehouse(t, rt, a)
	res := rt.applyGuildStorageMove(testDivision, a, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageDeposit, SourceSlot: 23, DestSlot: 0, NpcGID: guildManagerGid})
	if len(res.Frames) != 1 || !bytes.Equal(res.Frames[0].Payload, []byte{2, errCodeInvalidStorageTarget}) {
		t.Fatalf("guild deposit of a bound-loaded pet = %+v, want the storage-target refusal", res.Frames)
	}
	if bagRowByCodename(a, "SUMMON_PICKUP") == nil {
		t.Fatal("the refused deposit took the pet out of the master's bag")
	}
	list := rt.HandleGuildStorageList(testDivision, a, wire.NewWriter(4).U32(guildManagerGid).Payload())
	for _, frame := range list.Frames {
		if frame.Opcode == wire.OpGuildStorageList && (len(frame.Payload) < 2 || frame.Payload[1] != 0) {
			t.Fatalf("the guild chest holds rows after the refusal: %x", frame.Payload)
		}
	}

	// The personal warehouse takes the same pet: the owner's own
	// round-trip stays open to a bound load (done-when, #4479).
	room := &memoryStorage{room: domain.NewAccountStorage()}
	rt.ConfigureStorage(room)
	openStorageRoom(t, rt, a, 17)
	r := trade(t, rt, a, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeStorageDeposit, SourceSlot: 23, DestSlot: 0, NpcGID: 17})
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 || len(room.room.Rows) != 1 {
		t.Fatalf("personal deposit of a bound-loaded pet = %+v, room %+v", r.Frames, room.room.Rows)
	}
	if !room.room.Rows[0].Summon.Container.Rows[0].ForgeBound {
		t.Fatal("the personal warehouse lost the bound stone")
	}
	r = trade(t, rt, a, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeStorageWithdraw, SourceSlot: 0, DestSlot: 23, NpcGID: 17})
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 || bagRowByCodename(a, "SUMMON_PICKUP") == nil {
		t.Fatalf("personal withdraw = %+v", r.Frames)
	}

	// Control: the unbound stone loads the same way and the guild door
	// admits the pet, so the refusal above is the bound mark alone. The
	// keeper visit took the one storage session, so the room is opened
	// again first, as a player's second visit would.
	loadPetAndDismiss(t, rt, a, refs, 21)
	openBoundPetGuildWarehouse(t, rt, a)
	res = rt.applyGuildStorageMove(testDivision, a, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageDeposit, SourceSlot: 23, DestSlot: 0, NpcGID: guildManagerGid})
	if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild deposit of an unbound-loaded pet = %+v", res.Frames)
	}
	if bagRowByCodename(a, "SUMMON_PICKUP") != nil {
		t.Fatal("the admitted deposit left the pet in the master's bag")
	}

	// Another member withdraws the pet and takes the row out of its bag.
	rt.HandleGuildStorageClose(testDivision, a, wire.NewWriter(4).U32(guildManagerGid).Payload())
	openBoundPetGuildWarehouse(t, rt, b)
	res = rt.applyGuildStorageMove(testDivision, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGuildStorageWithdraw, SourceSlot: 0, DestSlot: 20, NpcGID: guildManagerGid})
	if len(res.Frames) == 0 || res.Frames[0].Payload[0] != 1 {
		t.Fatalf("member withdraw of the pet = %+v", res.Frames)
	}
	useSummonerFixture(t, rt, b, 20, refs.staticItemSource["SUMMON_PICKUP"])
	r = trade(t, rt, b, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeCosToPlayer, CosGID: summonedPetGID(t, b), SourceSlot: 0, DestSlot: 21})
	if len(r.Frames) == 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("member taking the row out of the pet = %+v", r.Frames)
	}
	if row := bagRowByCodename(b, petBoundStoneCodename); row == nil || row.ForgeBound {
		t.Fatalf("the member's bag holds %+v", b.MissionInventory)
	}
}
