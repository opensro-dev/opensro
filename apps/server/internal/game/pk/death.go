/*
===========================================================================

death.go - how a player's death is classified and what it costs

CGObjPC_ResolveDeathKind (4E6EC0) classifies a death by its killer
(CGObjPC_ResolvePvpKillRelation 4E6590) and dispatches the penalty:
CGObjPC_ApplyDeathPenalty (4E6980) for player, monster and guild-war
deaths, CGObjPC_ApplyJobDeathPenalty (4E6820) for a job wearer killed by
an opposing job monster. This file owns the rules; progression owns the
EXP arithmetic and the action runtime the item move.

===========================================================================
*/

package pk

/*
================
DeathKind

The byte 4E6EC0 stores at victim+0x1CB4 (the 0x3011 death notice).
================
*/
type DeathKind uint8

const (
	DeathNone         DeathKind = 0
	DeathJob          DeathKind = 1
	DeathTeam         DeathKind = 2 // free-battle capes, arena: no penalty
	DeathPlayer       DeathKind = 3
	DeathMonster      DeathKind = 4
	DeathGuildWar     DeathKind = 5
	DeathSpecialWorld DeathKind = 6
)

// deathProtectedLevel: 4E6A1B loses nothing at level 10 or below.
const deathProtectedLevel = 10

/*
================
Penalized

4E6EC0 runs 4E6980 for kinds 3, 4 and 5 only.
================
*/
func (k DeathKind) Penalized() bool {
	return k == DeathPlayer || k == DeathMonster || k == DeathGuildWar
}

/*
================
Murderer

4E69CB: the victim pays the murderer's rates when its penalty is positive
and the death is not a guild-war kill.
================
*/
func Murderer(penalty uint32, kind DeathKind) bool {
	return penalty > 0 && kind != DeathGuildWar
}

/*
================
LossRule

4E6980's rate table. Percent is the float32 constant multiplied into the
level's required EXP, CapFactor multiplies leveldata column 5, SP is the
skill points taken. A thief-monster killer halves the cap.
================
*/
type LossRule struct {
	Percent   float32
	CapFactor int64
	SP        int64
	HalveCap  bool
}

/*
================
DeathLoss

The rule for a penalized death above level 10, or false when the death
costs no EXP. killerPlayer selects the player rates; killerThief is a
thief monster (CGObj_IsThiefMonster, vtable +0x3B4).
================
*/
func DeathLoss(kind DeathKind, level uint8, penalty uint32, killerPlayer, killerThief bool) (LossRule, bool) {
	if !kind.Penalized() || level <= deathProtectedLevel {
		return LossRule{}, false
	}
	murderer := Murderer(penalty, kind)
	switch {
	case !killerPlayer && !murderer:
		return LossRule{Percent: 0.02, CapFactor: 100, HalveCap: killerThief}, true
	case !killerPlayer:
		return LossRule{Percent: 0.06, CapFactor: 300, SP: 60, HalveCap: killerThief}, true
	case !murderer:
		return LossRule{Percent: 0.004, CapFactor: 20}, true
	default:
		return LossRule{Percent: 0.02, CapFactor: 100, SP: 20}, true
	}
}

/*
================
DeathRelief

4E6D12: a murderer's death lowers the penalty by 600 (monster) or 200
(player); both lower the daily count by one.
================
*/
func DeathRelief(kind DeathKind) (penalty int32, daily int32) {
	switch kind {
	case DeathMonster:
		return -600, -1
	case DeathPlayer:
		return -200, -1
	}
	return 0, 0
}

/*
================
DropChance

CGObjPC_GetDeathItemDropChance (4E6930): the percent rolled as
rand() % 101 <= chance.
================
*/
func DropChance(penalty uint32) int32 {
	switch {
	case penalty == 0:
		return 5
	case penalty < 4000:
		return 30
	case penalty < 15000:
		return 50
	case penalty < 30000:
		return 70
	}
	return 100
}

/*
================
KillerEXPDoubled

4E69F5: the killer's PvP EXP doubles when the victim was a murderer.
================
*/
func KillerEXPDoubled(victimPenalty uint32, kind DeathKind) bool {
	return Murderer(victimPenalty, kind)
}

/*
================
DeathPenalty

What one death costs, as the action runtime resolved it for progression:
the 4E6980 rate row, or a job death (4E6820) with the killer's level, and
the victim's parameter 0x101 (the percent of the EXP loss it keeps).
================
*/
type DeathPenalty struct {
	Rule             LossRule
	Job              bool
	SpecialWorld     bool
	KillerLevel      uint8
	ReductionPercent float32
}
