package enterworld

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

// BuildLoginStatBlock must place the character's STR/INT on the words the
// client APPLIES to the player (+0x20/+0x22 -> CICPlayer+0x834/+0x836),
// not on the +0x14/+0x16 pair that reads like STR/INT and is dropped.
// cmd/active-probe's former builder filled the wrong pair (corrected in
// the progression wave), so a copy of that layout would seed every player
// with STR/INT 0 - this is the guard against that.
func TestBuildLoginStatBlockPlacesStatsOnTheAppliedWords(t *testing.T) {
	character := &Character{
		Name:      "statBlock",
		Strength:  i64(77),
		Intellect: i64(88),
	}

	display := wire.BaseStats{
		PhysicalAttackMin: 10, PhysicalAttackMax: 20,
		MagicalAttackMin: 30, MagicalAttackMax: 40,
		PhysicalDefense: 50, MagicalDefense: 60,
		HitRate: 70, ParryRate: 80,
	}
	payload := BuildLoginStatBlock(character, display)

	if len(payload) != wire.BaseStatsSize {
		t.Fatalf("block = %d bytes, want 0x%02X", len(payload), wire.BaseStatsSize)
	}
	if got := binary.LittleEndian.Uint16(payload[0x20:]); got != 77 {
		t.Fatalf("STR at +0x20 = %d, want 77 (the word sub_8629f0 applies)", got)
	}
	if got := binary.LittleEndian.Uint16(payload[0x22:]); got != 88 {
		t.Fatalf("INT at +0x22 = %d, want 88 (the word sub_862a00 applies)", got)
	}
	if got := binary.LittleEndian.Uint32(payload[0x00:]); got != 10 {
		t.Fatalf("physical attack minimum at +0x00 = %d, want the derived display field", got)
	}
	if got := binary.LittleEndian.Uint16(payload[0x16:]); got != 80 {
		t.Fatalf("parry ratio at +0x16 = %d, want the derived display field", got)
	}
	// The vitals ride along because the handler applies them
	// unconditionally - and they are DERIVED from the stats the block
	// itself carries (level 1: STR 77 -> 770, INT 88 -> 880), never the
	// persistence-only values.
	if got := binary.LittleEndian.Uint32(payload[0x18:]); got != 770 {
		t.Fatalf("maxHP at +0x18 = %d, want the derived 770", got)
	}
	if got := binary.LittleEndian.Uint32(payload[0x1c:]); got != 880 {
		t.Fatalf("maxMP at +0x1c = %d, want the derived 880", got)
	}
}

// An absent stat reads as the creation base (the same fallback the equip
// gates use), so a bare record derives the retail creation vitals.
func TestBuildLoginStatBlockFallbacks(t *testing.T) {
	payload := BuildLoginStatBlock(&Character{Name: "bare"}, wire.BaseStats{})

	if got := binary.LittleEndian.Uint16(payload[0x20:]); got != uint16(BaseStat) {
		t.Fatalf("STR = %d, want the creation base %d", got, BaseStat)
	}
	if got := binary.LittleEndian.Uint16(payload[0x22:]); got != uint16(BaseStat) {
		t.Fatalf("INT = %d, want the creation base %d", got, BaseStat)
	}
	if got := binary.LittleEndian.Uint32(payload[0x18:]); got != 200 {
		t.Fatalf("maxHP = %d, want the derived creation 200 (never 0 - it empties the gauge)", got)
	}
}

// The client holds STR/INT as u16, so a value past the word must clamp
// rather than wrap.
func TestBuildLoginStatBlockClampsToTheWord(t *testing.T) {
	payload := BuildLoginStatBlock(&Character{
		Name:      "overflow",
		Strength:  i64(StatWordMax + 500),
		Intellect: i64(-5),
	}, wire.BaseStats{})

	if got := binary.LittleEndian.Uint16(payload[0x20:]); got != uint16(StatWordMax) {
		t.Fatalf("STR = %d, want it clamped at %d", got, StatWordMax)
	}
	// A negative persisted stat reads as the creation base via the shared
	// reader, so it never encodes as a wrapped word.
	if got := binary.LittleEndian.Uint16(payload[0x22:]); got != uint16(BaseStat) {
		t.Fatalf("INT = %d, want the base fallback %d", got, BaseStat)
	}
}

/*
================
TestBuildLoginStatBlockKeepsTheKeeperWords

A keeper projection's STR and INT already carry every effect (params 1 and
2, CGObjPC_SendParameterStats 4EBE30). The block keeps them instead of
the stored attributes, so a level-up or equip refresh does not drop a
buff's STR/INT from the client.
================
*/
func TestBuildLoginStatBlockKeepsTheKeeperWords(t *testing.T) {
	character := &Character{Name: "buffed", Strength: i64(20), Intellect: i64(20)}
	payload := BuildLoginStatBlock(character, wire.BaseStats{StrWord: 23, IntWord: 23})
	if str, intel := binary.LittleEndian.Uint16(payload[0x20:]), binary.LittleEndian.Uint16(payload[0x22:]); str != 23 || intel != 23 {
		t.Fatalf("STR/INT = %d/%d, want the keeper's 23/23", str, intel)
	}
}
