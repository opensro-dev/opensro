/*
===========================================================================

loginstats.go - the login stat block

===========================================================================
*/

package enterworld

import (
	"opensro.online/server/internal/game/item/wire"
)

// StatWordMax is the display/storage ceiling of the native STR/INT words:
// the client holds them as u16 at CICPlayer+0x834/+0x836, so the server
// clamps on the way out rather than truncating silently.
const StatWordMax int64 = 0xffff

/*
==================
BuildLoginStatBlock

BuildLoginStatBlock builds the 0x343C base-stat block for a character.

This is the ONLY channel that moves the live STR/INT words the equip
gates and the item-tooltip requirement rows read (char-data never
carries them), so the stat plane emits it after a successful allocation
to make the new value visible mid-session.

Consumed by the client (sub_75be90, CPSMission base stats):
MaxHP/MaxMP onto CICPlayer+0x448/+0x44c, and the block's +0x20/+0x22
words onto +0x834/+0x836. The first eight fields stage into the singleton
at 0xced160 and CIFPlayerInfo::UpdateStats (sub_59ffa0) reads them as the
physical/magical attack ranges, defences, hit ratio and parry ratio.

TRAP: the block ALSO has +0x14/+0x16 words that read like STR/INT and
are not. cmd/active-probe's enter-world builder used to fill those and
leave +0x20/+0x22 zero - a layout that seeds a player with STR/INT 0 -
until the progression wave pointed it at wire.BaseStats; never
resurrect the old layout.

MaxHP/MaxMP ride as the DERIVED maxima (charactervitals/vitals.go: trunc of
1.02^(level-1) * STR/INT * 10, the retail formula the adopt-wave
landed; the v1.150 client never derives HP/MP itself - its 0x343C
handler applies MaxHP/MaxMP verbatim, sub_75be90: block +0x18 ->
vtable+0x98, +0x1c -> +0x9c). Because the stat plane re-encodes this
block inside its commit door after every granted allocation, a
mid-session +STR/+INT now MOVES the player's HP/MP gauges - the
user-visible payoff of deriving instead of echoing persisted values.

The client ships no player HP/MP growth data; the maxima come from a closed
form matched against era-exact observations, so the persisted maxHp/maxMp
fields are non-authoritative history. This block remains the single carrier of the maxima either way - 0x343C is
the only packet that can ever move them.

CURRENT-HP POLICY when a max changes mid-session: LEAVE current
untouched. The client never reconciles (its 0x33A6 handler stores
current verbatim with no clamp, and its display getter sub_856740
clamps at read: min(current, max)); this server clamps current into
[0, max] at every emission (wire.go coerceInt). Auto-filling or
proportionally scaling current on a raise would be invented gameplay.
==================
*/
func BuildLoginStatBlock(c *Character, stats wire.BaseStats) []byte {
	// A keeper projection (combat.PlayerBaseStatsWithModifiers) already
	// stores params 3 and 4 here, including item options and abnormal
	// factors. A zero means the caller had no keeper, so the closed form
	// still fills the gauge. The two agree when those contributions are absent.
	if stats.MaxHP == 0 {
		stats.MaxHP = uint32(DerivedMaxHP(c))
	}
	if stats.MaxMP == 0 {
		stats.MaxMP = uint32(DerivedMaxMP(c))
	}
	// The keeper projection carries STR and INT with every effect applied
	// (params 1 and 2); replacing them with the stored attributes dropped a
	// buff's STR/INT from every level-up, equip or recall refresh. Only a
	// caller without a keeper gets the stored attributes.
	if stats.StrWord == 0 && stats.IntWord == 0 {
		stats.StrWord = uint16(clampStatWord(CharacterStrength(c)))
		stats.IntWord = uint16(clampStatWord(CharacterIntellect(c)))
	}
	return stats.Encode()
}

// clampStatWord holds a stat inside the native u16 word.
func clampStatWord(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > StatWordMax {
		return StatWordMax
	}
	return value
}
