/*
===========================================================================

hostility.go - whether a monster may choose a target (5299E0)

The default world controller's mob decision (v1.188 604E00 -> 5298C0 ->
5299E0) as plain values: an observer, a target, and the predicate. Its last
branch is the first-attack protection the Bard's Noise (pola) installs.

===========================================================================
*/

package monster

/*
================
HostilityObserver

HostilityObserver and HostilityTarget are the inputs consumed by the
mob decision. They are values, not a shared combat-eligibility cache.
Numeric restrictions remain unnamed where their gameplay producers have
not been recovered.
================
*/
type HostilityObserver struct {
	TID             uint16
	ReferenceFlags  uint32
	Mode            uint8 // 4A98A0's temporary mode; ordinary acquisition passes 1
	Level           uint8
	Rarity          uint8
	RestrictionD34  uint32
	Restriction118C bool
	ExcludedGID     uint32
	HasBoundTarget  bool
	BoundTargetGID  uint32
}

/*
================
HostilityTarget
================
*/
type HostilityTarget struct {
	GID              uint32
	BodyStatus       uint8
	RejectedType43C  bool
	Player           bool
	RestrictionC44   bool
	RejectedType3C   bool
	ProtectionActive bool
	ProtectionMask   uint32
	ProtectionLevel  uint32
}

/*
================
AllowsHostility

The player-target projection. COS substitution at 529929 and non-default
world controllers require their own adapters. LIFE/visibility are caller
responsibilities: this function does not invent a life test or replace
the independent 540DE0 observer-status predicate.
================
*/
func AllowsHostility(actor HostilityObserver, target HostilityTarget) bool {
	if target.GID == 0 || target.RejectedType43C ||
		(target.BodyStatus >= 2 && target.BodyStatus <= 4) {
		return false
	}
	if target.BodyStatus == 6 || target.BodyStatus == 7 {
		// 529A60 jumps directly to success after detection. In particular it
		// skips the excluded-GID and bound-target checks below.
		if actor.TID&2 == 0 || actor.TID&0x1c != 4 || actor.TID&0x60 != 0x40 ||
			actor.TID&0x780 != 0x80 || actor.ReferenceFlags&0x200 == 0 {
			return false
		}
	} else {
		if actor.RestrictionD34&0x200 != 0 && actor.Restriction118C && target.GID == actor.ExcludedGID {
			return false
		}
		if actor.HasBoundTarget && target.GID != actor.BoundTargetGID {
			return false
		}
	}
	if (target.Player && target.RestrictionC44) || target.RejectedType3C {
		return false
	}
	return !FirstAttackProtected(actor, target)
}

/*
================
FirstAttackProtected

The protection branch at the end of 5299E0: a regular monster (type word
0x8C6) in a scanning mode does not choose a target whose protection names
its grade bit, unless its level is above the protection's level.
================
*/
func FirstAttackProtected(actor HostilityObserver, target HostilityTarget) bool {
	if actor.Mode == 0 || actor.TID&typeWordFlagMask != regularMonsterTypeWord || !target.ProtectionActive {
		return false
	}
	var mask uint32
	switch actor.Rarity & rarityGradeMask {
	case 0:
		mask = 1
	case 1:
		mask = 2
	case 3:
		mask = 4
	case 6:
		mask = 8
	}
	return target.ProtectionMask&mask != 0 && uint32(actor.Level) <= target.ProtectionLevel
}

/*
================
FirstAttackGuard

A player's live first-attack protection (the Bard's Noise): the grade
mask and level of its pola block. A zero mask protects nothing.
================
*/
type FirstAttackGuard struct {
	Mask, Level uint32
}

/*
================
Protect

Copy the guard onto a hostility target's protection fields.
================
*/
func (g FirstAttackGuard) Protect(target HostilityTarget) HostilityTarget {
	target.ProtectionActive = g.Mask != 0
	target.ProtectionMask, target.ProtectionLevel = g.Mask, g.Level
	return target
}

/*
================
Observer

The hostility observer an ordinary acquisition scan (mode 1) passes for
this monster: its full type word (TID4 in bits 11-15), level and grade.
================
*/
func (i Instance) Observer() HostilityObserver {
	return HostilityObserver{
		TID: NativeTypeWord(i.Ref), ReferenceFlags: i.Nest.NativeTacticsFlags, Mode: 1,
		Level: i.Ref.Level, Rarity: i.Rarity(),
	}
}
