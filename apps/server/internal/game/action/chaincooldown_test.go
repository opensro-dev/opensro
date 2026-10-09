/*
===========================================================================

chaincooldown_test.go - a chain stage is not refused by its root's cooldown

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestWindlessSpearSecondHitIgnoresTheRootCooldown

SKILL_CH_SPEAR_FRONTAREA_D_01 (Windless Spear) has a 3000 ms cooldown and
chains to SKILL_CH_SPEAR_FRONTAREA_D2_01 in the same group 709. The root
installs the group cooldown when it fires; the linked stage is admitted
without it (4AECE7), so its cost check must skip it too, or the second hit
is refused 0x3005.
================
*/
func TestWindlessSpearSecondHitIgnoresTheRootCooldown(t *testing.T) {
	rt, targets := areaFixture(t, 100000)
	c := rt.findCharacter(testDivision, "asd2")
	root := shippedOffense(t, "SKILL_CH_SPEAR_FRONTAREA_D_01")
	second := shippedOffense(t, "SKILL_CH_SPEAR_FRONTAREA_D2_01")
	if root.ChainNext != second.ID || root.Group != second.Group || root.CoolTimeMs == 0 {
		t.Fatalf("not a cooled chain in one group: %+v %+v", root, second)
	}
	skills := rt.deps.SkillData().(staticSkillSource)
	skills[root.ID], skills[second.ID] = root, second
	c.Skills = append(c.Skills, root.ID)
	c.Masteries = append(c.Masteries, enterworld.CharacterMastery{ID: 258, Level: 90})
	c.Level, c.MaxLevel = testInt64(90), testInt64(90)
	c.Strength, c.Intellect = testInt64(2000), testInt64(2000)
	c.CurrentMP = testInt64(10000)
	items := rt.deps.ItemReferences().(staticItemSource)
	spear := *items[c.MissionInventory[0].Codename]
	spear.Codename, spear.RefObjID, spear.TypeIDs[3] = "ITEM_CH_SPEAR_01_A", 73, 4
	items[spear.Codename] = &spear
	c.MissionInventory[0].Codename, c.MissionInventory[0].RefObjID, c.MissionInventory[0].TypeFlags = spear.Codename, spear.RefObjID, spear.TypeFlags()

	start := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	if len(start.Frames) == 0 || start.Frames[0].Opcode != wire.OpSkillCastResult || start.Frames[0].Payload[0] != 1 {
		t.Fatalf("root refused %+v", start)
	}
	base := rt.Now()
	staged, refused := false, false
	for tick := 1; tick <= 40; tick++ {
		for _, burst := range rt.TickHook()(base.Add(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range burst.Frames {
				if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) < 2 {
					continue
				}
				if frame.Payload[0] == 2 {
					refused = true
				} else if len(frame.Payload) >= 6 && binary.LittleEndian.Uint32(frame.Payload[2:6]) == second.ID {
					staged = true
				}
			}
		}
	}
	if refused || !staged {
		t.Fatalf("second hit staged=%v refused=%v, want it cast", staged, refused)
	}
}
