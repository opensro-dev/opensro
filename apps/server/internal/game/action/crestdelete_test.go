/*
===========================================================================

crestdelete_test.go - a crest delete use clears the stored crest

#2282 (delete half): ITEM_ETC_GUILD_CREST_DELETE clears the guild's
CrestParam and ITEM_ETC_UNION_CREST_DELETE the union's Crest, each
behind the ordinary item use (0x75BD -> 0xB5BD) with a 0x34F3 crest
update behind the answer. With no crest standing the use is refused
and the item is not spent. These tests drive the real guild store
door and the real union authority.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/union"
)

/*
================
crestDeleteOptions

The fixture states the crest gates judge: the guild's level, whether
the item holder is a plain member instead of the master, and the
union the guild stands in. The zero value is the state the success
tests want: master of a level-5 guild (past both level gates)
standing in no union.
================
*/
type crestDeleteOptions struct {
	guildLevel      uint8 // 0 = 5, past both level gates
	asMember        bool  // the item holder is a plain member, not the master
	union           bool  // seed a crested union holding the guild
	unionLedByOther bool  // another guild leads the seeded union (implies union)
}

/*
================
crestDeleteFixture

The guild-manager fixture's leader, holding one crest delete item of
the named kind in bag slot 21, with refs swapped for a mutable source.
Returns the runtime, the character and its guild's id.
================
*/
func crestDeleteFixture(t *testing.T, codename string, param1 float64) (*doorRuntime, *enterworld.Character, int64) {
	t.Helper()
	return crestDeleteFixtureWith(t, codename, param1, crestDeleteOptions{})
}

/*
================
crestDeleteFixtureWith

crestDeleteFixture under fixture options. A member holder is a second
character joined to the master's guild; the item sits in the holder's
bag either way.
================
*/
func crestDeleteFixtureWith(t *testing.T, codename string, param1 float64, opts crestDeleteOptions) (*doorRuntime, *enterworld.Character, int64) {
	t.Helper()
	d := guildManagerFixture(t)
	c := d.character
	if c.GuildID == nil {
		t.Fatal("fixture: leader has no guild")
	}
	guildID := *c.GuildID
	level := opts.guildLevel
	if level == 0 {
		level = 5
	}
	setGuildLevel(t, d, c, level)
	holder := c
	if opts.asMember {
		member := testCharacter()
		member.Name = "CrestMember"
		if err := d.authority.CreateCharacter(testDivision, "crest-member", member); err != nil {
			t.Fatal(err)
		}
		if _, refusal := d.authority.Guilds().AddGuildMemberAs(testDivision, guildID, c.ID, 0,
			domain.GuildMemberRecord{CharID: member.ID, JID: 2, Name: member.Name, Grade: guild.JoinerGrade}); refusal.Refused() {
			t.Fatalf("fixture member join: %v", refusal)
		}
		if member.GuildID == nil {
			t.Fatal("fixture: member has no guild")
		}
		holder = member
	}
	refs := testItems()
	refs[codename] = &enterworld.ItemRef{Codename: codename, RefObjID: 9269, Country: 3,
		TypeIDs:      [4]int64{3, 3, 12, 2},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": param1, "itemParam2_2a0": 2})}
	d.rt.deps.(*enterworld.Deps).Items = refs
	d.authority.UpdateCharacter(holder, "crest-delete-fixture", func() bool {
		holder.MissionInventory = append(holder.MissionInventory, enterworld.InventoryRow{Slot: 21,
			RefObjID: 9269, Codename: codename, TypeFlags: refs[codename].TypeFlags(), StackCount: 1})
		return true
	})
	if opts.union || opts.unionLedByOther {
		seedCrestUnion(t, d, guildID, opts.unionLedByOther)
	}
	return d, holder, guildID
}

/*
================
setGuildLevel

Writes the guild's stored level through its own door.
================
*/
func setGuildLevel(t *testing.T, d *doorRuntime, c *enterworld.Character, level uint8) {
	t.Helper()
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, c.ID, "crest-delete-fixture",
		domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.Level = level
			return g, m, true
		})
	if refusal.Refused() {
		t.Fatalf("seed guild level: %v", refusal)
	}
}

/*
================
seedCrestUnion

Stores a union carrying crest 777 that holds the fixture's guild -
led by it, or by another guild when ledByOther - and loads the
runtime's union authority from the store.
================
*/
func seedCrestUnion(t *testing.T, d *doorRuntime, guildID int64, ledByOther bool) {
	t.Helper()
	store := d.authority.Alliances()
	record := domain.AllianceRecord{Crest: 777}
	if ledByOther {
		record.AllianceID = guildID + 500
		record.Guilds[0] = guildID + 500
		record.Guilds[1] = guildID
	} else {
		record.AllianceID = guildID
		record.Guilds[0] = guildID
		record.Guilds[1] = guildID + 500
	}
	if err := store.SaveAlliance(testDivision, record, true); err != nil {
		t.Fatal(err)
	}
	d.rt.Unions = union.New()
	if err := d.rt.Unions.Restore(testDivision, store); err != nil {
		t.Fatal(err)
	}
}

/*
================
setGuildCrest

Writes the guild's stored crest through its own door.
================
*/
func setGuildCrest(t *testing.T, d *doorRuntime, c *enterworld.Character, crest uint32) {
	t.Helper()
	_, refusal := d.authority.Guilds().UpdateGuildAs(testDivision, c.ID, "crest-delete-fixture",
		domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.CrestParam = crest
			return g, m, true
		})
	if refusal.Refused() {
		t.Fatalf("seed crest: %v", refusal)
	}
}

/*
================
useCrestDelete

The ordinary 0x75BD use of the bag-slot-21 delete item.
================
*/
func useCrestDelete(t *testing.T, d *doorRuntime, c *enterworld.Character, codename string) OpResult {
	t.Helper()
	ref := d.rt.deps.ItemReferences().(staticItemSource)[codename]
	return d.rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
}

/*
================
crestUpdateFrame

The result's 0x34F3 frame payload, or nil.
================
*/
func crestUpdateFrame(result OpResult) []byte {
	for _, frame := range result.Frames {
		if frame.Opcode == guild.OpCrestUpdate {
			return frame.Payload
		}
	}
	return nil
}

/*
================
TestGuildCrestDeleteClearsTheStoredCrest
================
*/
func TestGuildCrestDeleteClearsTheStoredCrest(t *testing.T) {
	d, c, guildID := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	setGuildCrest(t, d, c, 4242)
	result := useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild crest delete refused: %+v", result)
	}
	if _, ok := inventoryRowAt(c, 21); ok {
		t.Fatal("delete item not spent")
	}
	record, _, ok := d.authority.Guilds().Guild(testDivision, guildID)
	if !ok || record.CrestParam != 0 {
		t.Fatalf("stored crest = %d (ok %t), want cleared", record.CrestParam, ok)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 7 || payload[0] != 1 || binary.LittleEndian.Uint32(payload[1:]) != uint32(guildID) {
		t.Fatalf("crest update = % X, want a guild row for %d", payload, guildID)
	}
	if got := binary.LittleEndian.Uint32(payload[len(payload)-4:]); got != 0 {
		t.Fatalf("crest update carries crest %d, want 0", got)
	}
}

/*
================
TestGuildCrestDeleteWithoutACrestSpendsNothing
================
*/
func TestGuildCrestDeleteWithoutACrestSpendsNothing(t *testing.T) {
	d, c, guildID := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	result := useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("crestless delete answered: %+v", result)
	}
	if row, ok := inventoryRowAt(c, 21); !ok || row.StackCount != 1 {
		t.Fatal("crestless delete spent the item")
	}
	if _, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok {
		t.Fatal("guild lost")
	}
	if payload := crestUpdateFrame(result); payload != nil {
		t.Fatalf("crestless delete published % X", payload)
	}
}

/*
================
TestUnionCrestDeleteClearsTheStoredCrest
================
*/
func TestUnionCrestDeleteClearsTheStoredCrest(t *testing.T) {
	d, c, guildID := crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{union: true})
	store := d.authority.Alliances()
	result := useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("union crest delete refused: %+v", result)
	}
	stored, err := store.Alliances(testDivision)
	if err != nil || len(stored) != 1 || stored[0].Crest != 0 {
		t.Fatalf("stored union = %+v (%v), want crest cleared", stored, err)
	}
	payload := crestUpdateFrame(result)
	if len(payload) < 6 || payload[0] != 2 || binary.LittleEndian.Uint32(payload[1:]) != uint32(guildID) {
		t.Fatalf("crest update = % X, want a union row for %d", payload, guildID)
	}
	if got := binary.LittleEndian.Uint32(payload[len(payload)-4:]); got != 0 {
		t.Fatalf("crest update carries crest %d, want 0", got)
	}
}

/*
================
crestRefusalCode

The 0xB5BD failure payload's notice byte, or -1 when the use was not
refused with the error arm.
================
*/
func crestRefusalCode(result OpResult) int {
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemUseResponse ||
		len(result.Frames[0].Payload) != 2 || result.Frames[0].Payload[0] != 2 {
		return -1
	}
	return int(result.Frames[0].Payload[1])
}

/*
================
assertCrestRefused

A refused delete answers with the named code, spends nothing, and
publishes no crest update.
================
*/
func assertCrestRefused(t *testing.T, result OpResult, holder *enterworld.Character, code uint8) {
	t.Helper()
	if got := crestRefusalCode(result); got != int(code) {
		t.Fatalf("refusal code = %#x, want %#x (result %+v)", got, code, result)
	}
	if row, ok := inventoryRowAt(holder, 21); !ok || row.StackCount != 1 {
		t.Fatal("refused delete spent the item")
	}
	if payload := crestUpdateFrame(result); payload != nil {
		t.Fatalf("refused delete published % X", payload)
	}
}

/*
================
TestGuildCrestDeleteGateOrder

The guild arm's gates refuse in their order - level (0xBB), then
master (0xBC), then crest (the generic byte) - and spend nothing.
================
*/
func TestGuildCrestDeleteGateOrder(t *testing.T) {
	// Level 3, held by the master: the level gate speaks first.
	d, c, guildID := crestDeleteFixtureWith(t, "ITEM_ETC_GUILD_CREST_DELETE", 1, crestDeleteOptions{guildLevel: 3})
	setGuildCrest(t, d, c, 4242)
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE"), c, wire.ErrCodeCrestGuildLevelTooLow)
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 4242 {
		t.Fatal("level-refused delete cleared the crest")
	}

	// Level 3, held by a plain member: still the level byte (level
	// precedes mastership).
	d, member, _ := crestDeleteFixtureWith(t, "ITEM_ETC_GUILD_CREST_DELETE", 1, crestDeleteOptions{guildLevel: 3, asMember: true})
	setGuildCrest(t, d, d.character, 4242)
	assertCrestRefused(t, useCrestDelete(t, d, member, "ITEM_ETC_GUILD_CREST_DELETE"), member, wire.ErrCodeCrestGuildLevelTooLow)

	// Level 5, held by a plain member: the master gate speaks.
	d, member, guildID = crestDeleteFixtureWith(t, "ITEM_ETC_GUILD_CREST_DELETE", 1, crestDeleteOptions{asMember: true})
	setGuildCrest(t, d, d.character, 4242)
	assertCrestRefused(t, useCrestDelete(t, d, member, "ITEM_ETC_GUILD_CREST_DELETE"), member, wire.ErrCodeCrestNotMaster)
	if record, _, ok := d.authority.Guilds().Guild(testDivision, guildID); !ok || record.CrestParam != 4242 {
		t.Fatal("member-refused delete cleared the crest")
	}

	// Level 5 master with no crest standing: the generic byte.
	d, c, _ = crestDeleteFixtureWith(t, "ITEM_ETC_GUILD_CREST_DELETE", 1, crestDeleteOptions{})
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE"), c, wire.ErrCodeInvalidRequest)
}

/*
================
TestUnionCrestDeleteGateOrder

The union arm's gates refuse in their order - guild level (0xBB),
guild master (0xBC), no union (0xBD), not the union's master guild
(0xBC), no crest (the generic byte) - and spend nothing.
================
*/
func TestUnionCrestDeleteGateOrder(t *testing.T) {
	storedCrest := func(t *testing.T, d *doorRuntime) uint32 {
		t.Helper()
		stored, err := d.authority.Alliances().Alliances(testDivision)
		if err != nil || len(stored) != 1 {
			t.Fatalf("stored unions = %+v (%v)", stored, err)
		}
		return stored[0].Crest
	}

	// Level 4 (past the guild gate, below the union one), master.
	d, c, _ := crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{guildLevel: 4, union: true})
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE"), c, wire.ErrCodeCrestGuildLevelTooLow)
	if got := storedCrest(t, d); got != 777 {
		t.Fatalf("level-refused delete left union crest %d", got)
	}

	// Level 5, plain member of the leading guild: the master gate
	// speaks before any union gate.
	d, member, _ := crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{asMember: true, union: true})
	assertCrestRefused(t, useCrestDelete(t, d, member, "ITEM_ETC_UNION_CREST_DELETE"), member, wire.ErrCodeCrestNotMaster)
	if got := storedCrest(t, d); got != 777 {
		t.Fatalf("member-refused delete left union crest %d", got)
	}

	// Level 5 master standing in no union at all.
	d, c, _ = crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{})
	d.rt.Unions = union.New()
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE"), c, wire.ErrCodeCrestNoUnion)

	// Level 5 master whose guild follows another guild's union.
	d, c, _ = crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{unionLedByOther: true})
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE"), c, wire.ErrCodeCrestNotMaster)
	if got := storedCrest(t, d); got != 777 {
		t.Fatalf("follower-refused delete left union crest %d", got)
	}

	// Level 5 master leading a union with no crest standing.
	d, c, _ = crestDeleteFixtureWith(t, "ITEM_ETC_UNION_CREST_DELETE", 2, crestDeleteOptions{union: true})
	store := d.authority.Alliances()
	stored, err := store.Alliances(testDivision)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored unions = %+v (%v)", stored, err)
	}
	stored[0].Crest = 0
	if err := store.SaveAlliance(testDivision, stored[0], true); err != nil {
		t.Fatal(err)
	}
	d.rt.Unions = union.New()
	if err := d.rt.Unions.Restore(testDivision, store); err != nil {
		t.Fatal(err)
	}
	assertCrestRefused(t, useCrestDelete(t, d, c, "ITEM_ETC_UNION_CREST_DELETE"), c, wire.ErrCodeInvalidRequest)
}

/*
================
TestGuildCrestDeleteReachesMembersAndViewers

Without the division seam, the cleared crest's update rides the
result broadcast (the user's observers) and goes by name to the
other guild members wherever they stand - never to the user, whose
copy rides the success burst.
================
*/
func TestGuildCrestDeleteReachesMembersAndViewers(t *testing.T) {
	d, _, _ := crestDeleteFixtureWith(t, "ITEM_ETC_GUILD_CREST_DELETE", 1, crestDeleteOptions{asMember: true})
	master := d.character
	setGuildCrest(t, d, master, 4242)
	pushed := map[string][]wire.Frame{}
	d.rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		pushed[name] = append(pushed[name], frames...)
	}
	// The fixture parked the delete item in the member's bag; the
	// master needs his own to spend.
	refs := d.rt.deps.ItemReferences().(staticItemSource)
	d.authority.UpdateCharacter(master, "crest-delete-fanout", func() bool {
		master.MissionInventory = append(master.MissionInventory, enterworld.InventoryRow{Slot: 22,
			RefObjID: 9269, Codename: "ITEM_ETC_GUILD_CREST_DELETE", TypeFlags: refs["ITEM_ETC_GUILD_CREST_DELETE"].TypeFlags(), StackCount: 1})
		return true
	})
	ref := refs["ITEM_ETC_GUILD_CREST_DELETE"]
	result := d.rt.HandleItemUse(testDivision, master, wire.NewWriter(3).U8(22).U16(ref.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild crest delete refused: %+v", result)
	}
	update := crestUpdateFrame(result)
	if update == nil {
		t.Fatal("user's burst carries no crest update")
	}
	found := false
	for _, frame := range result.Broadcast {
		if frame.Opcode == guild.OpCrestUpdate {
			found = true
		}
	}
	if !found {
		t.Fatal("observers' broadcast carries no crest update")
	}
	memberFrames := pushed["CrestMember"]
	if len(memberFrames) != 1 || memberFrames[0].Opcode != guild.OpCrestUpdate {
		t.Fatalf("member elsewhere received %v, want the crest update", memberFrames)
	}
	if len(pushed[master.Name]) != 0 {
		t.Fatal("the user was pushed a second copy")
	}
}

/*
================
TestGuildCrestDeleteDivisionFanout

With the division seam wired, one division-wide send carries the
update (the user excluded - their copy rides the burst) and no
member is pushed by name.
================
*/
func TestGuildCrestDeleteDivisionFanout(t *testing.T) {
	d, c, _ := crestDeleteFixture(t, "ITEM_ETC_GUILD_CREST_DELETE", 1)
	setGuildCrest(t, d, c, 4242)
	type call struct {
		except string
		frames []wire.Frame
	}
	var calls []call
	d.rt.PushShardPeerFrames = func(_ string, except string, frames []wire.Frame) {
		calls = append(calls, call{except, frames})
	}
	named := 0
	d.rt.PushCharacterFrames = func(string, string, []wire.Frame) { named++ }
	result := useCrestDelete(t, d, c, "ITEM_ETC_GUILD_CREST_DELETE")
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("guild crest delete refused: %+v", result)
	}
	if len(calls) != 1 || calls[0].except != c.Name || len(calls[0].frames) != 1 ||
		calls[0].frames[0].Opcode != guild.OpCrestUpdate {
		t.Fatalf("division fan-out = %+v, want one crest update excluding the user", calls)
	}
	if named != 0 {
		t.Fatal("members were pushed by name beside the division send")
	}
}

/*
================
TestCrestRegisterItemStaysUnusable

The register twins (param2 1) keep the unsupported answer: their flow
is the owner-blocked half of #2282 (#2272), not this slice.
================
*/
func TestCrestRegisterItemStaysUnusable(t *testing.T) {
	d := guildManagerFixture(t)
	c := d.character
	refs := testItems()
	refs["ITEM_ETC_GUILD_CREST_USER"] = &enterworld.ItemRef{Codename: "ITEM_ETC_GUILD_CREST_USER", RefObjID: 9268, Country: 3,
		TypeIDs:      [4]int64{3, 3, 12, 2},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 1, "itemParam2_2a0": 1})}
	d.rt.deps.(*enterworld.Deps).Items = refs
	d.authority.UpdateCharacter(c, "crest-delete-fixture", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 21,
			RefObjID: 9268, Codename: "ITEM_ETC_GUILD_CREST_USER", TypeFlags: refs["ITEM_ETC_GUILD_CREST_USER"].TypeFlags(), StackCount: 1})
		return true
	})
	ref := refs["ITEM_ETC_GUILD_CREST_USER"]
	result := d.rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(21).U16(ref.TypeFlags()).Payload())
	if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 2 {
		t.Fatalf("register use answered: %+v", result)
	}
	if _, ok := inventoryRowAt(c, 21); !ok {
		t.Fatal("register use spent the item")
	}
}
