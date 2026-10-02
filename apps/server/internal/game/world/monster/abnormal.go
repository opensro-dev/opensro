/*
===========================================================================

abnormal.go - monster abnormal-mask views

===========================================================================
*/

package monster

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
)

// AbnormalMask is the actor's published mask (block+04).
func (i Instance) AbnormalMask() uint32 {
	if i.Abnormal == nil {
		return 0
	}
	return i.Abnormal.Mask
}

// MovementBlocked is the move-command gate of 4B0EA0: freeze, sleep, root
// (mask C1) or stun (4000) refuse every new movement leg.
func (i Instance) MovementBlocked() bool {
	return i.AbnormalMask()&(0xc1|0x4000) != 0
}

/*
================
Fears

Fears reports an active Fear installed by gid. The v1.150 client describes
the status as "You cannot select or attack the target you fear for a fixed
time" (DE_UIIT_MSG_STATE_SKILL_CURSING_FEAR). 4A4BD0 only queues the
abandon event; without the AI selection disassembly at hand, the port
infers the rule from that text and applies it to every target selection.
================
*/
func (i Instance) Fears(gid uint32) bool {
	if i.Abnormal == nil || gid == 0 {
		return false
	}
	slot := i.Abnormal.Slots[abnormal.Fear]
	return slot.Active && slot.SourceGID == gid
}

func (i Instance) effectiveSpeed(param uint16, base float64) float64 {
	if i.Abnormal == nil || !i.Abnormal.Touches(param) {
		return base
	}
	definition, _ := paramkeeper.NativeDefinition(param)
	value, err := i.Abnormal.Evaluate(param, definition, float32(base))
	if err != nil {
		return base
	}
	return float64(value)
}

// WalkSpeed and RunSpeed are parameters 17/18 (4CEFE0 seeds them from the
// reference; frostbite and slow scale them), read by 4AA410.
func (i Instance) WalkSpeed() float64 { return i.effectiveSpeed(0x17, i.Ref.WalkSpeed) }
func (i Instance) RunSpeed() float64  { return i.effectiveSpeed(0x18, i.Ref.RunSpeed) }
