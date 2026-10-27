/*
===========================================================================

callbacks.go - per-status start / update / end callbacks (the server's callback table)

===========================================================================
*/

package abnormal

/*
==================
Owner

Owner is the native character surface the per-status callbacks use. Adapters
(monster and player) implement reads directly and record the side effects
for their owning transaction.
==================
*/
type Owner interface {
	Alive() bool       // vfunc F8 == 1
	IsPlayer() bool    // vfunc 1C
	IsMonster() bool   // vfunc 28
	CurrentHP() uint32 // vfunc 108
	MaxHP() uint32     // vfunc 110
	MaxMP() uint32     // vfunc 114
	// Param is the keeper value including the block's own modifiers.
	Param(id uint16) float32
	// SourceExists is ObjMgr_FindByID(+4C) plus vfunc 18.
	SourceExists(gid uint32) bool
	// SourceDead is the source's life state 2 or 3.
	SourceDead(gid uint32) bool
	// Roll is the owner's CZoeZoeRnd stream.
	Roll(key uint32, chance int32) bool
	// Now is the owner's GetTickCount-equivalent in milliseconds.
	Now() int64

	// Side effects, recorded by the adapter.
	ParamsChanged(speed bool)                   // +4E8/+4EC or +538
	SetMotion(state, next uint8, delay float32) // +55C
	CancelActions(all bool)                     // +570
	StopMove()                                  // +4C0 / +4B0
	AIEvent(event, kind uint8, source uint32)   // the AI tactics' current state
	// Hit is vfunc 4FC with reason 2 (damage over time) or 1 (time bomb).
	// Credited is false when the tick is uncredited (dead source, lethal).
	Hit(source uint32, credited bool, damage uint32, reason uint8, status Status)
	ConsumeResources(hp, mp int32, reason uint8) // +308
	Detonate(slot Slot)                          // the time bomb's detonation
}

// Native event argument of the block+E0C table.
const (
	eventStart  = 0
	eventUpdate = 1
	eventEnd    = 2
)

const abnormalSource uint32 = 5

func clamp100(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func callback(b *Block, o Owner, event int, slot *Slot) {
	switch slot.Status {
	case Freeze: // cancel actions, hold the freeze motion
		switch event {
		case eventStart:
			o.CancelActions(false)
			o.SetMotion(0xa, 0xff, 0)
		case eventEnd:
			o.SetMotion(0xa, 0, 1.5)
		}
	case Frostbite: // owns the movement factors
		switch event {
		case eventStart:
			b.applyModifier(0x17, 3, abnormalSource, 50)
			b.applyModifier(0x18, 3, abnormalSource, 50)
			b.applyModifier(0x8c, 0, 0, 200)
			o.ParamsChanged(true)
			b.SpeedOwner = 1
		case eventEnd:
			b.applyModifier(0x17, 3, abnormalSource, 100)
			b.applyModifier(0x18, 3, abnormalSource, 100)
			b.applyModifier(0x8c, 0, 0, 100)
			o.ParamsChanged(true)
		}
	case ElectricShock: // scales parameter 9
		switch event {
		case eventStart:
			b.applyModifier(9, 3, abnormalSource, clamp100(float32(uint32(100-slot.Param28))))
			o.ParamsChanged(false)
		case eventEnd:
			b.applyModifier(9, 3, abnormalSource, 100)
			o.ParamsChanged(false)
		}
	case Burn: // rate-scaled damage tick
		if event == eventUpdate {
			damageOverTime(o, slot, func() uint32 {
				rate := float64(float32(slot.Rate24)) / (float64(o.Param(8))/100 + 1)
				value := float64(slot.Scale20) * float64(float32(rate))
				if !(value > 0) {
					return 0
				}
				return uint32(ftol(float64(float32(value))))
			}, 2000, true)
		}
	case Poison: // HP-relative damage tick
		if event == eventUpdate {
			damageOverTime(o, slot, func() uint32 {
				hp := int64(o.CurrentHP())
				rest := hp - int64(slot.Param38)
				if rest <= 0 {
					rest = 1
				}
				if hp-rest <= 0 {
					return 0
				}
				return uint32(hp - rest)
			}, 2000, false)
		}
	case Sleep: // cancel actions, sleep motion
		switch event {
		case eventStart:
			o.CancelActions(false)
			o.SetMotion(0x13, 0xff, 0)
		case eventEnd:
			o.SetMotion(0, 0xff, 0)
		}
	case Root: // stop moving
		if event == eventStart {
			o.StopMove()
		}
	case Slow: // movement factors unless frostbite owns them
		switch {
		case event == eventStart || event == eventUpdate && b.SpeedOwner == 1:
			if b.Mask&Frostbite.Bit() == 0 {
				b.applyModifier(0x17, 3, abnormalSource, 75)
				b.applyModifier(0x18, 3, abnormalSource, 75)
				b.applyModifier(0x8c, 3, abnormalSource, 125)
				o.ParamsChanged(true)
				b.SpeedOwner = 8
			}
		case event == eventEnd && b.SpeedOwner == 8:
			b.applyModifier(0x17, 3, abnormalSource, 100)
			b.applyModifier(0x18, 3, abnormalSource, 100)
			b.applyModifier(0x8c, 3, abnormalSource, 100)
			o.ParamsChanged(true)
		}
	case Fear: // monster AI events 0x14 / 0x15
		switch {
		case event == eventStart && o.IsMonster() && o.Alive():
			o.AIEvent(0x14, 9, slot.SourceGID)
		case event == eventEnd && !slot.Retired && o.IsMonster() && o.Alive():
			o.AIEvent(0x15, 9, 0)
		}
	case Myopia: // flat parameter 0xB7
		switch event {
		case eventStart:
			b.applyModifier(0xb7, 0, abnormalSource, float32(slot.Param3C))
		case eventEnd:
			b.removeModifier(0xb7, abnormalSource)
		}
	case Bleeding: // defense cut plus damage tick
		switch event {
		case eventStart:
			b.applyModifier(5, 1, abnormalSource, -float32(slot.Param40))
			b.applyModifier(6, 1, abnormalSource, -float32(slot.Param40))
			o.ParamsChanged(false)
		case eventUpdate:
			if slot.Param38 > 0 {
				damageOverTime(o, slot, func() uint32 { return slot.Param38 }, slot.PeriodMs, true)
			} else if elapsed(o.Now(), slot.LastTickAt) > slot.PeriodMs {
				slot.LastTickAt = o.Now()
			}
		case eventEnd:
			b.removeModifier(5, abnormalSource)
			b.removeModifier(6, abnormalSource)
			o.ParamsChanged(false)
		}
	case Dark: // hit rate cut
		switch event {
		case eventStart:
			b.applyModifier(0xb, 1, abnormalSource, -float32(slot.Param48))
			o.ParamsChanged(false)
		case eventEnd:
			b.removeModifier(0xb, abnormalSource)
			o.ParamsChanged(false)
		}
	case Stun: // cancel actions, stun motion
		switch event {
		case eventStart:
			o.CancelActions(true)
			o.SetMotion(9, 0xff, 0)
		case eventEnd:
			o.SetMotion(0, 0xff, 0)
		}
	case Disease: // flat parameter 0xA9
		switch event {
		case eventStart:
			b.applyModifier(0xa9, 0, abnormalSource, float32(slot.Param44))
		case eventEnd:
			b.applyModifier(0xa9, 0, abnormalSource, 0)
		}
	case Confusion: // monster AI events 0x14 / 0x15
		switch {
		case event == eventStart && o.IsMonster() && o.Alive():
			o.AIEvent(0x14, 0x10, 0)
		case event == eventEnd && !slot.Retired && o.IsMonster() && o.Alive():
			o.AIEvent(0x15, 0x10, 0)
		}
	case Decay, Weaken: // capped defense cut
		param := uint16(5)
		if slot.Status == Weaken {
			param = 6
		}
		switch event {
		case eventStart:
			b.removeModifier(param, abnormalSource)
			current := float64(ftol(float64(o.Param(param))))
			limit := ftol(current - 0.29999999999999999*current)
			cut := int32(slot.Param34)
			if cut > limit {
				cut = limit
			}
			b.applyModifier(param, 0, abnormalSource, float32(-cut))
			o.ParamsChanged(false)
		case eventEnd:
			b.removeModifier(param, abnormalSource)
			o.ParamsChanged(false)
		}
	case Impotent, Division: // percent-product damage factors
		// Impotent lowers outgoing damage (B2/B3); Division raises incoming
		// damage (B4/B5) by the unsigned magnitude.
		first, second, magnitude := uint16(0xb2), uint16(0xb3), -float32(slot.Param38)
		if slot.Status == Division {
			first, second, magnitude = 0xb4, 0xb5, float32(slot.Param38)
		}
		switch event {
		case eventStart:
			b.applyModifier(first, 2, abnormalSource, magnitude)
			b.applyModifier(second, 2, abnormalSource, magnitude)
		case eventEnd:
			b.removeModifier(first, abnormalSource)
			b.removeModifier(second, abnormalSource)
		}
	case Panic, Combustion: // maximum, recovery and drain
		maximum, recovery := uint16(3), uint16(0x8f)
		if slot.Status == Combustion {
			maximum, recovery = 4, 0x90
		}
		switch event {
		case eventStart:
			factor := int32(100) - int32(slot.Param2C)
			if factor <= 0 {
				factor = 1
			}
			b.applyModifier(maximum, 3, abnormalSource, float32(factor))
			b.applyModifier(recovery, 0, abnormalSource, float32(slot.Param34))
			limit := o.MaxHP()
			if slot.Status == Combustion {
				limit = o.MaxMP()
			}
			slot.Drain = uint16(ftol(float64(limit) * float64(slot.Param38) / 100))
			o.ParamsChanged(false)
		case eventUpdate:
			if elapsed(o.Now(), slot.LastTickAt) > 2000 {
				slot.LastTickAt = o.Now()
				o.ConsumeResources(0, int32(slot.Drain), 2)
			}
		case eventEnd:
			b.applyModifier(maximum, 3, abnormalSource, 100)
			b.removeModifier(recovery, abnormalSource)
			o.ParamsChanged(false)
		}
	case TimeBomb: // detonates on a natural end
		if event == eventEnd && !slot.Retired && o.Alive() {
			o.Detonate(*slot)
		}
	}
}

/*
==================
damageOverTime

damageOverTime is the shared tick of the burn, poison and bleeding callbacks. The
latch +6C records a dead source. A latched burn/bleeding tick keeps its
source only when it is lethal; a latched poison tick never does.
==================
*/
func damageOverTime(o Owner, slot *Slot, damage func() uint32, period uint32, lethalCheck bool) {
	now := o.Now()
	if elapsed(now, slot.LastTickAt) <= period {
		return
	}
	slot.LastTickAt = now
	value := damage()
	if value == 0 {
		return
	}
	credited := o.SourceExists(slot.SourceGID)
	if slot.SourceDied || credited && o.SourceDead(slot.SourceGID) {
		slot.SourceDied = true
		credited = lethalCheck && value >= o.CurrentHP()
	}
	o.Hit(slot.SourceGID, credited, value, 2, slot.Status)
}
