/*
===========================================================================

cure_test.go - pill and level cures, zombie potions

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func cureItem(tid4 int64, mask, chance, grade int64, levels [6]int64) *enterworld.ItemRef {
	return &enterworld.ItemRef{
		RefObjID: 900, Codename: "ITEM_CURE", Country: 3,
		TypeIDs:      [4]int64{3, 3, 2, tid4},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1}),
		CureMask:     mask, CureChance: chance, CureGradeSub: grade, CureLevels: levels,
	}
}

func useBagItem(rt *Runtime, c *enterworld.Character, ref *enterworld.ItemRef) OpResult {
	items := rt.deps.ItemReferences().(staticItemSource)
	items[ref.Codename] = ref
	rt.deps.(*enterworld.Deps).Items = items
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1,
	})
	flags := ref.TypeFlags()
	return rt.HandleItemUse(testDivision, c, []byte{21, byte(flags), byte(flags >> 8)})
}

func saw(frames []wire.Frame, opcode uint16) bool {
	for _, frame := range frames {
		if frame.Opcode == opcode {
			return true
		}
	}
	return false
}

func TestUniversalPillClearsItsMask(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.deps.Update(c, "stun", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Stun, DurationMs: 10000, Grade: 3, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	result := useBagItem(rt, c, cureItem(1, int64(abnormal.Stun.Bit()), 100, 0, [6]int64{}))
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Has(abnormal.Stun) {
		t.Fatal("pill left stun")
	}
	if !saw(result.Frames, 0x36C7) || !saw(result.Frames, 0x33A6) {
		t.Fatalf("frames %+v", result.Frames)
	}
}

func TestLevelCureRemovesAShortenedBurn(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	rt.deps.Update(c, "burn", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Burn, DurationMs: 1000, Level: 1, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	levels := [6]int64{}
	levels[abnormal.Burn] = 2
	result := useBagItem(rt, c, cureItem(2, 0, 0, 0, levels))
	if block := rt.playerAbnormal(testDivision, c.Name); block != nil && block.Has(abnormal.Burn) {
		t.Fatal("level cure left burn")
	}
	if !saw(result.Frames, 0x36C7) || !saw(result.Frames, 0x33A6) {
		t.Fatalf("frames %+v", result.Frames)
	}
}

func potion(hp, mp float64) *enterworld.ItemRef {
	return &enterworld.ItemRef{
		RefObjID: 901, Codename: "ITEM_HPMP", Country: 3,
		TypeIDs:      [4]int64{3, 3, 1, 3},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1}),
		RecoveryHP:   hp, RecoveryMP: mp,
	}
}

func TestZombiePotionDamagesHPAndRestoresMP(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	hp := int64(100)
	c.CurrentHP = &hp
	mp := int64(50)
	c.CurrentMP = &mp
	rt.deps.Update(c, "zombie", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Zombie, DurationMs: 10000, Level: 1, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	result := useBagItem(rt, c, potion(40, 30))
	// Reference server recovery at level 1, STR/INT 20: HP 40 -> 41, MP 30 -> 31.
	if c.CurrentHP == nil || *c.CurrentHP != 59 || c.CurrentMP == nil || *c.CurrentMP != 81 {
		t.Fatalf("hp %v mp %v frames %d", c.CurrentHP, c.CurrentMP, len(result.Frames))
	}
}

/*
================
TestZombiePotionStopsAtOneHPWithoutDeath

The v1.150 rule as reported by the owner: the zombie potion reverses the
heal into damage, but the damage stops at one HP and never kills. MP still
recovers as before.
================
*/
func TestZombiePotionStopsAtOneHPWithoutDeath(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	hp := int64(10)
	c.CurrentHP = &hp
	mp := int64(50)
	c.CurrentMP = &mp
	rt.deps.Update(c, "zombie", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Zombie, DurationMs: 10000, Level: 1, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	result := useBagItem(rt, c, potion(80, 30))
	// Reference server recovery at level 1, STR/INT 20: HP 80 -> 81, more
	// than the 10 HP held, so the reversed damage stops at the floor. MP
	// 30 -> 31 still recovers.
	if c.CurrentHP == nil || *c.CurrentHP != 1 {
		t.Fatalf("hp %v, want 1", c.CurrentHP)
	}
	if c.CurrentMP == nil || *c.CurrentMP != 81 {
		t.Fatalf("mp %v, want 81", c.CurrentMP)
	}
	if !enterworld.CharacterAlive(c) {
		t.Fatal("zombie potion killed the player")
	}
	if saw(result.Frames, wire.OpObjectStateRefresh) || saw(result.Frames, wire.OpDeathNotice) {
		t.Fatalf("zombie potion sent a death frame: %+v", result.Frames)
	}
}

// The reference server deals the potion's whole HP amount while zombie is set, even when a
// full gauge would absorb none of it as healing.
func TestZombiePotionAtFullHPStillDamages(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	maxHP, _, _, _ := rt.playerKeeperVitals(testDivision, c)
	c.CurrentHP = &maxHP
	rt.deps.Update(c, "zombie", func() bool {
		rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{{
			Status: abnormal.Zombie, DurationMs: 10000, Level: 1, SourceGID: m.Gid,
		}}, clock.NowMs())
		return true
	})
	useBagItem(rt, c, potion(40, 0))
	// Reference server recovery at level 1, STR 20: Param1 40 -> 41.
	if c.CurrentHP == nil || *c.CurrentHP != maxHP-41 {
		t.Fatalf("hp %v want %d", c.CurrentHP, maxHP-41)
	}
}

// The cure result is ignored: the item is consumed and the lane locks even
// with nothing to cure; a second use inside the lock is the reuse delay.
func TestCureAlwaysConsumesAndLocksItsLane(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	pill := cureItem(1, int64(abnormal.Stun.Bit()), 100, 0, [6]int64{})
	first := useBagItem(rt, c, pill)
	if !saw(first.Frames, wire.OpItemUseResponse) || len(c.MissionInventory) != 0 && c.MissionInventory[len(c.MissionInventory)-1].Codename == pill.Codename {
		t.Fatalf("empty cure was refused or not consumed: %+v", first.Frames)
	}
	if c.ItemCureCooldowns[0] != clock.NowMs()+20100 || c.ItemCureCooldowns[1] != 0 {
		t.Fatalf("pill lane %v", c.ItemCureCooldowns)
	}
	second := useBagItem(rt, c, pill)
	if len(second.Frames) != 1 || second.Frames[0].Payload[0] == 1 {
		t.Fatalf("reuse inside the lock admitted: %+v", second.Frames)
	}
	// The other cure lane is independent and locks for 1.1 s. The refused
	// pill still occupies slot 21, so the bag is emptied first.
	c.MissionInventory = nil
	useBagItem(rt, c, cureItem(2, 0, 0, 0, [6]int64{}))
	if c.ItemCureCooldowns[1] != clock.NowMs()+1100 {
		t.Fatalf("level cure lane %v", c.ItemCureCooldowns)
	}
}

// The reference server shuffles the selection before the limit: with two matching slots,
// i=1 swaps with rand()%2, so rand 0 cures the higher slot and 1 the lower.
func TestPillLimitFollowsTheNativeShuffle(t *testing.T) {
	for _, tc := range []struct {
		roll  uint32
		cured abnormal.Status
		kept  abnormal.Status
	}{{0, abnormal.Stun, abnormal.Sleep}, {1, abnormal.Sleep, abnormal.Stun}} {
		rt, clock, c, m := newCombatTestRuntime(t, 100)
		rt.deps.Update(c, "states", func() bool {
			rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{
				{Status: abnormal.Sleep, DurationMs: 10000, Grade: 2, SourceGID: m.Gid},
				{Status: abnormal.Stun, DurationMs: 10000, Grade: 2, SourceGID: m.Gid},
			}, clock.NowMs())
			return true
		})
		roll := tc.roll
		rt.CombatRoll = func() (uint32, error) { r := roll; roll = 0; return r, nil }
		useBagItem(rt, c, cureItem(1, int64(abnormal.Sleep.Bit()|abnormal.Stun.Bit()), 100, 0, [6]int64{}))
		block := rt.playerAbnormal(testDivision, c.Name)
		if block == nil || block.Has(tc.cured) || !block.Has(tc.kept) {
			t.Fatalf("roll %d: block %+v", tc.roll, block)
		}
	}
}
