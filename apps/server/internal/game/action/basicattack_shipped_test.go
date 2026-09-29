package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

// TestShippedEuropeanSwordBaseAttackResolves is the production-data seam that
// the synthetic combat fixtures cannot cover. Character creation, itemdata,
// skilldata and combat must agree on the race and weapon identity or a native
// 0x72CD engage is otherwise reduced to a silent no-op at the action owner.
func TestShippedEuropeanSwordBaseAttackResolves(t *testing.T) {
	t.Parallel()

	textdataDir := gamedatatest.TextdataDir(t)
	character := &enterworld.Character{
		Name:          "Test2",
		RaceIndex:     testInt64(0),
		ModelCodename: "CHAR_EU_MAN_NOBLE",
		Level:         testInt64(1),
		Strength:      testInt64(20),
		Intellect:     testInt64(20),
		Skills:        []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970},
		MissionInventory: []enterworld.InventoryRow{
			{
				Slot: 6, RefObjID: 10730, Codename: "ITEM_EU_SWORD_01_A_DEF",
				TypeFlags: 15148, VarianceBits: "0", Durability: 47, StackCount: 1,
			},
			{
				Slot: 1, RefObjID: 11459, Codename: "ITEM_EU_M_HEAVY_01_BA_A_DEF",
				TypeFlags: 7596, VarianceBits: "0", Durability: 45, StackCount: 1,
			},
			{
				Slot: 4, RefObjID: 11460, Codename: "ITEM_EU_M_HEAVY_01_LA_A_DEF",
				TypeFlags: 9644, VarianceBits: "0", Durability: 45, StackCount: 1,
			},
			{
				Slot: 5, RefObjID: 11461, Codename: "ITEM_EU_M_HEAVY_01_FA_A_DEF",
				TypeFlags: 13740, VarianceBits: "0", Durability: 45, StackCount: 1,
			},
		},
	}
	deps := &enterworld.Deps{
		Items:  enterworld.NewTextdataItems(textdataDir),
		Skills: enterworld.NewTextdataSkills(textdataDir),
	}
	runtime := NewRuntime(deps, nil)

	skill, loadout, refusal := runtime.resolveBasicAttack(character)
	if refusal != "" {
		t.Fatalf(
			"shipped EU sword base attack did not resolve (%s; race=%s, weapon=%d, range=%g, learned=%v)",
			refusal, enterworld.ResolveCharacterRaceKey(character), loadout.WeaponKind,
			skillActionReach(skill, loadout, combat.Stats{}), character.Skills,
		)
	}
	if skill.ID != 7127 || skill.Codename != "SKILL_EU_SWORD_BASE_01" {
		t.Fatalf("resolved skill = %d/%s, want shipped EU sword base row 7127", skill.ID, skill.Codename)
	}
	if !loadout.HasWeapon || loadout.WeaponKind != 7 || loadout.ActionRange <= 0 {
		t.Fatalf("resolved loadout = %+v, want a positive-range weapon kind 7", loadout)
	}
}
