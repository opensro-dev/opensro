/*
===========================================================================

skillreqi_test.go - tests for the reqi walk (combat/equipmentreqi.go)

===========================================================================
*/

package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// reqiKit equips synthetic items by TID. Expected codes are read off
// the native equipment check, not computed with reqiRefusal's own helpers.
type reqiKit struct {
	items staticItemSource
	c     *enterworld.Character
}

func newReqiKit() *reqiKit {
	return &reqiKit{items: staticItemSource{}, c: &enterworld.Character{}}
}

func (k *reqiKit) equip(slot int64, tid [4]int64, durability int64) {
	code := fmt.Sprintf("REQI_%d_%v", slot, tid)
	ref := &enterworld.ItemRef{Codename: code, TypeIDs: tid}
	k.items[code] = ref
	k.c.MissionInventory = append(k.c.MissionInventory, domain.InventoryRow{
		Slot: slot, Codename: code, TypeFlags: ref.TypeFlags(), Durability: durability,
	})
}

func (k *reqiKit) refusal(req enterworld.SkillReqi) uint16 {
	return reqiRefusal(k.c, k.items, req)
}

func reqi(all bool, pairs ...enterworld.SkillReqiPair) enterworld.SkillReqi {
	r := enterworld.SkillReqi{Present: true, All: all, Count: len(pairs)}
	copy(r.Pairs[:], pairs)
	return r
}

func pair(kind, value uint32) enterworld.SkillReqiPair {
	return enterworld.SkillReqiPair{Kind: kind, Value: value}
}

var (
	weapon = func(tid4 int64) [4]int64 { return [4]int64{3, 1, 6, tid4} }
	armour = func(family int64) [4]int64 { return [4]int64{3, 1, family, 1} }
)

// Shipped shape (6,7 6,8 6,9): kind 6 is the primary weapon's TID4 (case 2);
// any pair admits.
func TestReqiPrimaryWeaponAnyPair(t *testing.T) {
	req := reqi(false, pair(6, 7), pair(6, 8), pair(6, 9))
	for _, tc := range []struct {
		name  string
		tid4  int64
		dur   int64
		equip bool
		want  uint16
	}{
		{"matching", 8, 10, true, 0},
		{"other kind", 10, 10, true, 0x300d},
		{"bare hand", 0, 0, false, 0x300d},
		// Natively a broken weapon's TID reads zero, so it no longer matches.
		{"broken", 8, 0, true, 0x300d},
	} {
		k := newReqiKit()
		if tc.equip {
			k.equip(6, weapon(tc.tid4), tc.dur)
		}
		if got := k.refusal(req); got != tc.want {
			t.Errorf("%s: %#x want %#x", tc.name, got, tc.want)
		}
	}
}

// Kind 4 is the secondary slot (case 1): the shipped shield rows (4,2).
func TestReqiSecondarySlot(t *testing.T) {
	k := newReqiKit()
	if got := k.refusal(reqi(false, pair(4, 2))); got != 0x300d {
		t.Fatalf("no shield %#x", got)
	}
	k.equip(7, [4]int64{3, 1, 4, 2}, 10)
	if got := k.refusal(reqi(false, pair(4, 2))); got != 0 {
		t.Fatalf("shield %#x", got)
	}
}

/*
==================
TestReqiSecondarySlotWantsAShield

Port-only, not native: ammunition in slot 7 shares the shield's TID4
(Chinese arrows 3.3.4.1, European bolts 3.3.4.2), and the native check
compares only that. The port admits a shield pair for a shield alone.
==================
*/
func TestReqiSecondarySlotWantsAShield(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value uint32
		ammo  [4]int64
	}{{"arrows for a Chinese shield", 1, [4]int64{3, 3, 4, 1}}, {"bolts for a European shield", 2, [4]int64{3, 3, 4, 2}}} {
		k := newReqiKit()
		k.equip(6, weapon(6), 10)
		k.equip(7, tc.ammo, 10)
		if got := k.refusal(reqi(false, pair(4, tc.value))); got != 0x300d {
			t.Errorf("%s: %#x want 0x300d", tc.name, got)
		}
	}
}

// Shipped shape (10,0 6,15) with reqn: armour family 10 on the set AND a
// TID4 15 weapon.
func TestReqiArmourSetWithReqn(t *testing.T) {
	req := reqi(true, pair(10, 0), pair(6, 15))
	full := func(family int64, tid4 int64) *reqiKit {
		k := newReqiKit()
		for slot := int64(0); slot <= 5; slot++ {
			k.equip(slot, armour(family), 10)
		}
		k.equip(6, weapon(tid4), 10)
		return k
	}
	if got := full(10, 15).refusal(req); got != 0 {
		t.Fatalf("full set %#x", got)
	}
	if got := full(10, 14).refusal(req); got != 0x300d {
		t.Fatalf("wrong weapon under reqn %#x", got)
	}
	if got := full(9, 15).refusal(req); got != 0x300d {
		t.Fatalf("wrong armour family %#x", got)
	}
	// Without reqn the weapon pair alone admits.
	if got := full(9, 15).refusal(reqi(false, pair(10, 0), pair(6, 15))); got != 0 {
		t.Fatalf("any-pair weapon %#x", got)
	}
}

/*
==================
TestReqiArmourWalkKeepsEarlierMatch

Natively the armour walk stops at the first mismatch but keeps a match
already made, so only armour index 1 (head, slot 0) has to carry the
family.
==================
*/
func TestReqiArmourWalkKeepsEarlierMatch(t *testing.T) {
	k := newReqiKit()
	k.equip(0, armour(10), 10)
	k.equip(2, armour(9), 10)
	if got := k.refusal(reqi(false, pair(10, 0))); got != 0 {
		t.Fatalf("head match %#x", got)
	}
	k = newReqiKit()
	k.equip(2, armour(10), 10)
	if got := k.refusal(reqi(false, pair(10, 0))); got != 0x300d {
		t.Fatalf("head empty %#x", got)
	}
}

// Natively pair (6,0) matches a zeroed TID, so a broken weapon
// reaches the usability test and records 0x300F.
func TestReqiBrokenWeaponRecords300F(t *testing.T) {
	k := newReqiKit()
	k.equip(6, weapon(8), 0)
	if got := k.refusal(reqi(false, pair(6, 0))); got != 0x300f {
		t.Fatalf("broken %#x", got)
	}
	k = newReqiKit()
	if got := k.refusal(reqi(false, pair(6, 0))); got != 0 {
		t.Fatalf("empty slot passes 5D0 %#x", got)
	}
}

// A full five-pair walk under reqn skips the native count test and
// fails even when every pair matched.
func TestReqiFivePairReqnFails(t *testing.T) {
	k := newReqiKit()
	k.equip(6, weapon(15), 10)
	p := pair(6, 15)
	if got := k.refusal(reqi(true, p, p, p, p)); got != 0 {
		t.Fatalf("four pairs %#x", got)
	}
	if got := k.refusal(reqi(true, p, p, p, p, p)); got != 0x300d {
		t.Fatalf("five pairs %#x", got)
	}
}

// Kinds outside the table, and kind 14 (avatar socket 4, absent in v1.150),
// never match.
func TestReqiUnmatchedKinds(t *testing.T) {
	k := newReqiKit()
	k.equip(6, weapon(1), 10)
	for _, kind := range []uint32{0, 5, 7, 8, 12, 13, 14, 15} {
		if got := k.refusal(reqi(false, pair(kind, 1))); got != 0x300d {
			t.Errorf("kind %d: %#x", kind, got)
		}
	}
}
