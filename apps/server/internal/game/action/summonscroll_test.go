/*
===========================================================================

summonscroll_test.go - the mall summon scrolls through the item-use door

Port-only, not native (issue #3257): with SRO_MALL_CATEGORIES on, a
scroll in the bag stands its count of guild soldiers beside the owner
for the native 1,200 s and is spent; a second use is refused while
the soldiers stand, dismissal frees the owner but keeps the summon
cooldown, and with the setting off the scroll is dead stock.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
)

// The suffix a soldier group's rows carry past its base row.
const summonScrollSoldierLevels = 3

/*
================
summonScrollFixture

A level-1 owner holding the three scrolls, facing a three-row soldier
group for their own country plus the other country's base row (which
the owner must never draw from).
================
*/
func summonScrollFixture(t *testing.T) (*Runtime, *enterworld.Character, map[string]*enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	refs := testCosSource(testItems())
	country := enterworld.NativeCountryByte9C(c)
	base := &enterworld.CharacterRef{Codename: "COS_GUILD_CH_SOLDIER1_001", GroupCodename: "SOLDIERS",
		RefObjID: 9910, TidWord: 0x1c6 | domain.MercenaryBand<<11,
		Level: 1, MaxHP: 100, MaxMP: 50, RunSpeed: 60, WalkSpeed: 20, Scale: 100}
	base.Parameters.Country = uint8(country)
	refs.characters[base.Codename] = base
	for level := 2; level <= summonScrollSoldierLevels; level++ {
		row := *base
		row.Codename = "COS_GUILD_CH_SOLDIER1_00" + string(rune('0'+level))
		row.RefObjID++
		row.Level = uint8(level)
		refs.characters[row.Codename] = &row
	}
	other := *base
	other.Codename = "COS_GUILD_EU_SOLDIER1_001"
	other.GroupCodename = "SOLDIERS_EU"
	other.Parameters.Country = uint8(country) + 1
	refs.characters[other.Codename] = &other

	scrolls := map[string]*enterworld.ItemRef{}
	for i, code := range []string{SummonScrollItem, SummonScrollOneItem, SummonScrollTwoItem} {
		ref := &enterworld.ItemRef{Codename: code, RefObjID: uint32(9950 + i), TypeIDs: [4]int64{3, 3, 9, 0}, Country: 3,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
		refs.staticItemSource[code] = ref
		scrolls[code] = ref
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: int64(23 + i), RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: 2})
	}
	rt, _ := newTestRuntime(c, refs)
	rt.BindPetSession(testDivision, c, 1)
	return rt, c, scrolls
}

/*
================
summonScrollRowLeft
================
*/
func summonScrollRowLeft(c *enterworld.Character, slot int64) int64 {
	for _, row := range c.MissionInventory {
		if row.Slot == slot {
			return row.StackCount
		}
	}
	return 0
}

/*
================
TestSummonScrollsSummonTheirSoldiers

ONE and the plain scroll stand one soldier, TWO stands two; each use
spends one scroll, stamps the 1,200 s expiry and cooldown, and a
further use is refused while the soldiers stand.
================
*/
func TestSummonScrollsSummonTheirSoldiers(t *testing.T) {
	t.Setenv(commerce.EnvMallCategories, "on")
	for _, tc := range []struct {
		code string
		slot int64
		want int
	}{
		{SummonScrollOneItem, 24, 1},
		{SummonScrollItem, 23, 1},
		{SummonScrollTwoItem, 25, 2},
	} {
		rt, c, scrolls := summonScrollFixture(t)
		nowSec := rt.Now().Unix()
		request := wire.NewWriter(3).U8(uint8(tc.slot)).U16(scrolls[tc.code].TypeFlags()).Payload()
		result := rt.HandleItemUse(testDivision, c, request)
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("%s refused: %+v", tc.code, result)
		}
		if len(c.Mercenaries) != tc.want {
			t.Fatalf("%s summoned %d soldiers, want %d", tc.code, len(c.Mercenaries), tc.want)
		}
		seen := map[uint32]bool{}
		for _, pet := range c.Mercenaries {
			if seen[pet.GID] || !c.OwnsMercenaryID(pet.GID) {
				t.Fatalf("soldier identity: %+v", pet)
			}
			if pet.RentalExpiresAtUnix != nowSec+domain.MercenarySummonSeconds {
				t.Fatalf("soldier expiry %d, want %d", pet.RentalExpiresAtUnix, nowSec+domain.MercenarySummonSeconds)
			}
			seen[pet.GID] = true
		}
		if want := (nowSec + domain.MercenarySummonSeconds) * 1000; c.MercenarySummonUntilMs != want {
			t.Fatalf("cooldown stamp %d, want %d", c.MercenarySummonUntilMs, want)
		}
		if left := summonScrollRowLeft(c, tc.slot); left != 1 {
			t.Fatalf("%s: %d scrolls left, want 1", tc.code, left)
		}
		// A second scroll while the soldiers stand is refused and kept.
		again := rt.HandleItemUse(testDivision, c, request)
		if !bytes.Equal(again.Frames[0].Payload, itemUseFailure(mercenaryAlreadySummoned).Frames[0].Payload) {
			t.Fatalf("second %s answered %+v, want the already-summoned refusal", tc.code, again)
		}
		if left := summonScrollRowLeft(c, tc.slot); left != 1 {
			t.Fatalf("refused use consumed a scroll: %d left", left)
		}
		// Dismissal releases the soldiers; the cooldown stands.
		rt.HandleMercenaryDismiss(testDivision, c, nil)
		if len(c.Mercenaries) != 0 || c.MercenarySummonUntilMs == 0 {
			t.Fatalf("dismiss: %d soldiers, cooldown %d", len(c.Mercenaries), c.MercenarySummonUntilMs)
		}
	}
}

/*
================
TestSummonScrollOffIsDeadStock

With the setting off (or garbage), the scroll keeps the native
answer: no soldiers, no consumption.
================
*/
func TestSummonScrollOffIsDeadStock(t *testing.T) {
	for _, setting := range []string{"off", "", "later"} {
		t.Setenv(commerce.EnvMallCategories, setting)
		rt, c, scrolls := summonScrollFixture(t)
		ref := scrolls[SummonScrollTwoItem]
		request := wire.NewWriter(3).U8(25).U16(ref.TypeFlags()).Payload()
		result := rt.HandleItemUse(testDivision, c, request)
		if len(c.Mercenaries) != 0 {
			t.Fatalf("setting %q summoned %d soldiers", setting, len(c.Mercenaries))
		}
		if result.DiagnosticRefusal == "" {
			t.Fatalf("setting %q: use answered %+v, want the off refusal", setting, result)
		}
		if left := summonScrollRowLeft(c, 25); left != 2 {
			t.Fatalf("setting %q consumed a scroll: %d left", setting, left)
		}
	}
}
