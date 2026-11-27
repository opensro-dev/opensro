/*
===========================================================================

transportbag_test.go - a transport's cargo bag grows with its owner

#4490 U11-01: the level transition grows an active transport's bag to
the owner's new native size and publishes a fresh 0x3158 record; no
crossing, no growth, and a bag already past the native size keeps it.

===========================================================================
*/

package progression

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// transportBagLevels is a two-row curve with the sizing rule's gold
// basis: level 1 minimum 28, level 3 minimum 35.
type transportBagLevels struct{}

func (transportBagLevels) SkillPointCost(int64) (int64, bool) { return 1, true }

func (transportBagLevels) ExpRequired(level int64) (int64, bool) {
	required, ok := map[int64]int64{1: 118, 2: 470, 3: 1058}[level]
	return required, ok
}

func (transportBagLevels) MonsterExpBasis(int64) (int64, bool) { return 0, false }

func (transportBagLevels) WithdrawalGoldBasis(level int64) (int64, bool) {
	basis, ok := map[int64]int64{1: 28, 3: 35}[level]
	return basis, ok
}

// transportBagRefs answers the fixture transport's character row: a
// band-2 horse authoring 101 places at authored run 40.
type transportBagRefs struct{}

func (transportBagRefs) ItemRefByCodename(string) (*enterworld.ItemRef, bool) {
	return nil, false
}

func (transportBagRefs) ItemRefByID(uint32) (*enterworld.ItemRef, bool) {
	return nil, false
}

func (transportBagRefs) CharacterRefByCodename(codename string) (*enterworld.CharacterRef, bool) {
	if codename != "COS_T_DHORSE3" {
		return nil, false
	}
	return &enterworld.CharacterRef{
		RefObjID: 3914, TidWord: 0x11C6, Codename: "COS_T_DHORSE3", Name: "Red Horse",
		RunSpeed: 40, InventoryCapacity: 101, MaxHP: 87829,
	}, true
}

func (transportBagRefs) SummonableCharacterRefs() []enterworld.CharacterRef { return nil }

/*
================
transportBagCharacter

A level-1 owner with the fixture transport out at a given bag size.
================
*/
func transportBagCharacter(capacity uint8) *enterworld.Character {
	c := levelupTestCharacter()
	c.ActiveCOS = &domain.CharacterCOS{
		GID: 0xC00001, RefObjID: 3914, Codename: "COS_T_DHORSE3", Name: "Red Horse",
		CurrentHP: 100, Summoned: true,
		Container: &domain.COSContainer{Capacity: capacity},
	}
	return c
}

func transportBagRuntime(character *enterworld.Character) *Runtime {
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      transportBagRefs{},
		Levels:     transportBagLevels{},
	}
	return NewRuntime(deps)
}

/*
================
TestTransportBagGrowsAtTheOwnersLevelUp

From level 1 (bag 7) a grant crossing two levels lands at level 3,
whose native size is 8: the bag grows and the burst closes with the
0x3158 record carrying it. Without a crossing nothing grows, and a bag
already past the native size is never shrunk.
================
*/
func TestTransportBagGrowsAtTheOwnersLevelUp(t *testing.T) {
	c := transportBagCharacter(7)
	rt := transportBagRuntime(c)
	result := rt.GrantExperience(c, 118+470, 0, 0)
	if c.ActiveCOS.Container.Capacity != 8 {
		t.Fatalf("the bag after two level-ups is %d places, want the level-3 size 8", c.ActiveCOS.Container.Capacity)
	}
	last := result.Frames[len(result.Frames)-1]
	if last.Opcode != wire.OpCosRecordCreate {
		t.Fatalf("the burst closes on 0x%X, want the 0x3158 record", last.Opcode)
	}

	still := transportBagCharacter(7)
	stillRT := transportBagRuntime(still)
	stillResult := stillRT.GrantExperience(still, 50, 0, 0)
	if still.ActiveCOS.Container.Capacity != 7 {
		t.Fatalf("a grant without a crossing grew the bag to %d", still.ActiveCOS.Container.Capacity)
	}
	for _, frame := range stillResult.Frames {
		if frame.Opcode == wire.OpCosRecordCreate {
			t.Fatal("a grant without a crossing published a COS record")
		}
	}

	wide := transportBagCharacter(24)
	wideRT := transportBagRuntime(wide)
	wideRT.GrantExperience(wide, 118+470, 0, 0)
	if wide.ActiveCOS.Container.Capacity != 24 {
		t.Fatalf("a bag past the native size shrank to %d", wide.ActiveCOS.Container.Capacity)
	}
}
