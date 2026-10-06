/*
===========================================================================

mercenary.go - guild-soldier attribute contributions (4D9850)

The four advertised attributes write native parameter IDs. Defense's 84..87
writes are retained as authored, independently of AE..B1 absorption; those
are distinct parameters and must not be silently substituted.

===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
)

const (
	mercenaryAttributeAmount float32 = 35
	mercenaryAttributeSource uint32  = 0x03000000
	MercenaryDefense         uint8   = 1
	MercenaryAttack          uint8   = 2
	MercenaryAccuracy        uint8   = 4
	MercenaryHealth          uint8   = 8
)

/*
================
mercenaryModifier

4D9850 has no parameter write for attribute bit 16.
================
*/
func mercenaryModifier(id uint16, flags uint8) (paramkeeper.Channel, bool) {
	switch {
	case flags&MercenaryDefense != 0 && id >= 0x84 && id <= 0x87,
		flags&MercenaryAttack != 0 && id >= 0x80 && id <= 0x83:
		return paramkeeper.Flat, true
	case flags&MercenaryAccuracy != 0 && (id == 9 || id == 11),
		flags&MercenaryHealth != 0 && (id == 3 || id == 0x19):
		return paramkeeper.PercentSum, true
	}
	return paramkeeper.Flat, false
}

/*
================
MercenaryParameter
================
*/
func MercenaryParameter(id uint16, base float32, flags uint8, block *abnormal.Block) (float32, error) {
	return projectParameter(parameterInput{id: id, base: base, satiety: MaximumSatiety, attributes: flags, block: block})
}
