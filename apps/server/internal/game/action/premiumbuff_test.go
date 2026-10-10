/*
===========================================================================

premiumbuff_test.go - the premium tickets' APRU buff on the shipped data

ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME casts SKILL_MALL_PRE_APRU_GLOBAL_4W_01:
stri/inti 3, er/hr 5%, dru 5/5, odar 5 and the Alchemy luck words. The
stat block the use publishes (0x343C) must carry the buffed STR and INT.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestPremiumTicketBuffReachesStatsAndTheStatBlock

Every APRU word reaches the keeper, and the published 0x343C carries the
keeper's STR and INT (23, not the stored 20).
================
*/
func TestPremiumTicketBuffReachesStatsAndTheStatBlock(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	pack, ok := items.ItemRefByCodename("ITEM_MALL_PREMIUM_GLOBAL_GOLDTIME")
	if !ok {
		t.Fatal("shipped global Gold Time ticket missing")
	}
	c := testCharacter()
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 40, RefObjID: pack.RefObjID,
		Codename: pack.Codename, TypeFlags: pack.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	rt.deps.(*enterworld.Deps).Skills = enterworld.NewTextdataSkills(dir)
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	out := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(40).U16(pack.TypeFlags()).Payload())
	if len(out.Frames) == 0 || out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("ticket use = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if after.Strength != before.Strength+3 || after.Intellect != before.Intellect+3 ||
		after.PhysicalBasicRate != before.PhysicalBasicRate+5 || after.MagicalSkillRate != before.MagicalSkillRate+5 ||
		after.EvasionRate <= before.EvasionRate || after.HitRate <= before.HitRate || after.PhysicalBasicTaken >= 1 {
		t.Fatalf("keeper before %+v after %+v", before, after)
	}
	if reinforce, stone := rt.alchemyBonuses(testDivision, c); reinforce != 5 || stone != 5 {
		t.Fatalf("Alchemy luck %d/%d, want 5/5", reinforce, stone)
	}
	var block []byte
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpBaseStats {
			block = frame.Payload
		}
	}
	if len(block) != wire.BaseStatsSize {
		t.Fatalf("no stat block in %+v", out.Frames)
	}
	if str, intel := binary.LittleEndian.Uint16(block[0x20:]), binary.LittleEndian.Uint16(block[0x22:]); float64(str) != after.Strength || float64(intel) != after.Intellect {
		t.Fatalf("published STR/INT %d/%d, want the buffed %v/%v", str, intel, after.Strength, after.Intellect)
	}
}
