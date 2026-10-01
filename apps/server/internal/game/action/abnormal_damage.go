/*
===========================================================================

abnormal_damage.go - periodic hit records and private damage presentation

Player, monster and COS victims share the native damage-number body. Keep
raw damage separate from the saturated HP debit and route it after commit.

===========================================================================
*/

package action

import "opensro.online/server/internal/game/item/wire"

/*
================
abnormalHit

Defer damage credit and wire publication until the HP transaction commits.
================
*/
type abnormalHit struct {
	source   uint32
	credited bool
	damage   uint32
	reason   uint8
}

/*
================
abnormalDamageFrame

52A33D/52A38E use the authored damage, even when it exceeds remaining HP.
The v1.150 74FE80 handler consumes this body through opcode 3128.
================
*/
func abnormalDamageFrame(gid, damage uint32) wire.Frame {
	return wire.Frame{
		Opcode:  abnormalDamageCreditOpcode,
		Payload: wire.NewWriter(abnormalDamageCreditBytes).U32(gid).U32(damage).Payload(),
	}
}
