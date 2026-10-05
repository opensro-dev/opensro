/*
===========================================================================

runtimefixture_test.go - shared action runtime, item and clock fixtures

Keep deterministic setup and wire assertions separate from pickup scenarios.
These fixtures are used by the action package's behavior tests.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

const testDivision = "global-official"

// staticItemSource is a test enterworld.ItemRefSource.
type staticItemSource map[string]*enterworld.ItemRef

/*
================
ItemRefByCodename
================
*/
func (s staticItemSource) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := s[codename]
	return ref, ok
}

/*
================
testItems
================
*/
func testItems() staticItemSource {
	return staticItemSource{
		"ITEM_ETC_GOLD_02": {
			RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", TypeIDs: [4]int64{3, 3, 5, 2},
		},
		"ITEM_ETC_HP_POTION_01": {
			RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1}), RecoveryHP: 120,
		},
		"ITEM_CH_SWORD_01_A_RARE": {
			RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
			Combat: &enterworld.ItemCombatRef{
				PhysicalAttack: enterworld.ItemAttackRange{
					Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4},
					Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4},
				},
				MagicalAttack: enterworld.ItemAttackRange{
					Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.1},
					Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.1},
				},
				HitRate: enterworld.ItemStatRange{Min: 24, Max: 30},
			},
		},
		"ITEM_CH_SWORD_02_A_RARE": {
			RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
			Combat: &enterworld.ItemCombatRef{},
		},
	}
}

/*
================
testCharacter
================
*/
func testCharacter() *enterworld.Character {
	gold := int64(5000)
	level := int64(1)
	base := int64(enterworld.BaseStat)
	return &enterworld.Character{
		ID:            3,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Gold:          &gold,
		Level:         &level,
		MaxLevel:      &level,
		Strength:      &base,
		Intellect:     &base,
		MissionInventory: []enterworld.InventoryRow{
			{Slot: 20, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
				TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "9223372036854775808",
				Durability: 96, StackCount: 1},
		},
	}
}

/*
================
newTestRuntime
================
*/
func newTestRuntime(character *enterworld.Character, items enterworld.ItemRefSource) (*Runtime, *fakeClock) {
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      items,
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	// 99 % 100 + 1 = 100: equipment wears only where a test asks it to.
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	rt.Now = clock.Now
	return rt, clock
}

/*
================
fakeClock
================
*/
type fakeClock struct{ now time.Time }

/*
================
Now
================
*/
func (c *fakeClock) Now() time.Time { return c.now }

/*
================
Advance
================
*/
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

/*
================
NowMs
================
*/
func (c *fakeClock) NowMs() int64 { return c.now.UnixMilli() }

/*
================
At
================
*/
func (c *fakeClock) At(offset time.Duration) time.Time { return c.now.Add(offset) }

// installMidMove puts the character halfway through a 100u eastward run:
// from X=960 toward X=1060 over 2s, sampled at +1s => live X ~1010.
/*
================
installMidMove
================
*/
func installMidMove(rt *Runtime, character *enterworld.Character, clock *fakeClock) (goal simulation.Spawn) {
	from := simulation.Spawn{RegionID: 0x62A8, X: 960, Y: 20, Z: 458, Angle: 300}
	goal = simulation.Spawn{RegionID: 0x62A8, X: 1060, Y: 20, Z: 458, Angle: 300}
	startedAtMs := clock.NowMs() - 1000

	segment := simulation.MoveSegmentForTravel(from, goal, simulation.RunMode, startedAtMs)
	key := simulation.WorldKey(testDivision, character.Name)
	rt.Worlds.Update(key,
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
		func(world *simulation.WorldState) {
			world.Spawn = goal
			world.MoveSegment = segment
			world.MovementMode = simulation.RunMode
			world.SpawnSet = true
			world.MovementSourceSeeded = true
		})
	return goal
}

/*
================
opcodesOf
================
*/
func opcodesOf(frames []wire.Frame) []uint16 {
	out := make([]uint16, len(frames))
	for index, frame := range frames {
		out[index] = frame.Opcode
	}
	return out
}

/*
================
assertOpcodes
================
*/
func assertOpcodes(t *testing.T, frames []wire.Frame, want ...uint16) {
	t.Helper()
	got := opcodesOf(frames)
	if len(got) != len(want) {
		t.Fatalf("frames = %04X, want %04X", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("frames = %04X, want %04X", got, want)
		}
	}
}

/*
================
encodeMove
================
*/
func encodeMove(t *testing.T, request wire.ItemMoveRequest) []byte {
	t.Helper()
	payload, err := request.Encode()
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return payload
}
