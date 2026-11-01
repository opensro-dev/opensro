/*
===========================================================================

block.go - the defender's block chance and the br block-rate buff

The reference server's hit-outcome routine gives every target group a block
chance byte (+0xE) before its impacts resolve, and rolls it once per impact
of an attack skill (att present): a blocked impact is a type-2 record with
no damage, no imbue share and no status roll (the block branch jumps
straight to the record writer).

===========================================================================
*/

package combat

import (
	"math"

	"opensro.online/server/internal/game/paramkeeper"
)

/*
==================
blockRateBonus

The server's block-bonus lookup: the defender's bonus for the attack's
lanes, physical first: physical basic 0x88, physical skill 0x89, magical
basic 0x8A, magical skill 0x8B. An attack selecting neither lane has none.
==================
*/
func blockRateBonus(defender Stats, attackFlags uint32) float64 {
	var id uint16
	switch {
	case attackFlags&physicalAttackFlag != 0 && attackFlags&1 != 0:
		id = 0x88
	case attackFlags&physicalAttackFlag != 0 && attackFlags&2 != 0:
		id = 0x89
	case attackFlags&physicalAttackFlag != 0:
		return 0
	case attackFlags&magicalAttackFlag != 0 && attackFlags&1 != 0:
		id = 0x8a
	case attackFlags&magicalAttackFlag != 0 && attackFlags&2 != 0:
		id = 0x8b
	default:
		return 0
	}
	v, _ := defender.Param(id)
	return float64(v)
}

/*
==================
BlockChance

The hit-outcome routine's block step: byte(defender param 0xA) plus byte(the
lane bonus), both truncated and added as bytes, then divided by (1 +
attacker param 0x38 / 100) and truncated to a byte again. A row carrying ck
(+0x248) takes no block chance at all (tested before the block step).
==================
*/
func BlockChance(attacker, defender Stats, attackFlags uint32, ck bool) uint8 {
	if ck {
		return 0
	}
	chance := uint8(int32(math.Trunc(defender.BlockRate)))
	if attackFlags != 0 {
		chance += uint8(int32(math.Trunc(blockRateBonus(defender, attackFlags))))
	}
	ignore, _ := attacker.Param(0x38)
	return uint8(int32(math.Trunc(float64(chance) / (float64(ignore)/100 + 1))))
}

/*
==================
BlockRateWrites

The server's skill-parameter installer, br branch: br {mask, value} adds
value to the flat channel of each block-rate parameter its normalized mask
selects (physical 4 / magical 8, crossed with basic 1 / skill 2).
==================
*/
func BlockRateWrites(mask, value uint32) []paramkeeper.Write {
	var writes []paramkeeper.Write
	for _, lane := range [...]struct {
		kind, attack uint32
		param        uint16
	}{{4, 1, 0x88}, {4, 2, 0x89}, {8, 1, 0x8a}, {8, 2, 0x8b}} {
		if mask&lane.kind != 0 && mask&lane.attack != 0 {
			writes = append(writes, paramkeeper.Write{Parameter: lane.param, Channel: paramkeeper.Flat, Value: float32(value)})
		}
	}
	return writes
}
