/*
===========================================================================

avatarbless_test.go - the smith's avatar magic option grant

The wire follows CIFGrantMagicAttributeWnd_OnConfirm (6EBB10) and
CPSMission_OnAvatarMagicOptionAdd0x32D9 (770140).

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
avatarBlessFixture

The merchant fixture's NPC as a smith, and an avatar hat in bag slot 14.
================
*/
func avatarBlessFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].TalkFlags = simulation.NpcTalkFlagMagicOption
	hat := wire.PackTypeFlags(3, 1, 13, 1)
	rt.Alchemy = &alchemy.Catalog{
		Items: map[string]alchemy.Reference{"HAT": {ID: 9001, Name: "HAT", Flags: hat, Class: 1, MaxMagic: 2}},
		Magic: map[uint16]alchemy.Magic{
			246: {ID: 246, Name: "MATTR_AVATAR_STR", Degree: 1, Tag: 0x73747261, Params: [3]uint32{1, 1, 1}},
			248: {ID: 248, Name: "MATTR_AVATAR_HR", Degree: 1, Tag: 0x687261, Params: [3]uint32{6, 5, 5}},
		},
		AvatarOptions: map[uint8][]string{1: {"MATTR_AVATAR_STR"}},
	}
	rt.AlchemyRoll = func() (uint32, error) { t.Fatal("a single-value option drew"); return 0, nil }
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 14, RefObjID: 9001,
		Codename: "HAT", TypeFlags: hat, StackCount: 1, VarianceBits: "0"})
	return rt, c
}

/*
================
avatarBlessRequest
================
*/
func avatarBlessRequest(slot uint8, codename string) []byte {
	return wire.NewWriter(8).U8(slot).Str(codename).Payload()
}

/*
================
TestAvatarBlessNeedsTheSmithsOpenRow
================
*/
func TestAvatarBlessNeedsTheSmithsOpenRow(t *testing.T) {
	rt, c := avatarBlessFixture(t)
	// A fresh selection: the smith is chosen but no row is open yet.
	rt.Selected.Set(testDivision, c.Name, 17)
	if frames := rt.HandleAvatarBless(testDivision, c, avatarBlessRequest(14, "MATTR_AVATAR_STR")); frames != nil {
		t.Fatalf("a grant without the open row answered %+v", frames)
	}
	open, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(17).U32(simulation.NpcTalkFlagMagicOption).Payload())
	if refusal != "" || len(open) != 1 || open[0].Opcode != wire.OpNpcInteractionAck {
		t.Fatalf("open = %+v / %q", open, refusal)
	}
	frames := rt.HandleAvatarBless(testDivision, c, avatarBlessRequest(14, "MATTR_AVATAR_STR"))
	if len(frames) != 1 || frames[0].Opcode != opAvatarMagicOptionAddAck || !bytes.HasPrefix(frames[0].Payload, []byte{1, 1, 14}) {
		t.Fatalf("grant = %+v", frames)
	}
	row := c.MissionInventory[len(c.MissionInventory)-1]
	if row.Slot != 14 || len(row.MagicOptions) != 1 || row.MagicOptions[0] != 1<<32|246 {
		t.Fatalf("hat row = %+v", row)
	}
	frames = rt.HandleAvatarBless(testDivision, c, avatarBlessRequest(14, "MATTR_AVATAR_HR"))
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, []byte{2, byte(alchemy.AvatarWrong)}) {
		t.Fatalf("an unassigned option answered %+v", frames)
	}
}
