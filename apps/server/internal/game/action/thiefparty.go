/*
===========================================================================

thiefparty.go - a party earns more gold and EXP for each thief in it

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_THIEF_PARTY_BONUS on, every member of a party earns +10% monster
EXP and the party's monster gold drops are +10% larger for each member
dressed as a thief, capped at +50% (five thieves; the cap is an assumption
the advisor has not ruled on). Skill EXP is unchanged. Both percents are
live tunables (game/tunables, the trade group); 10 and 50 are their
defaults. Off, the default
here, is native; the GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"math"
	"os"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/tunables"
)

// EnvThiefPartyBonus turns the bonus on ("on", "1", "true").
const EnvThiefPartyBonus = "SRO_THIEF_PARTY_BONUS"

/*
================
ThiefPartyBonusFromEnv
================
*/
func ThiefPartyBonusFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvThiefPartyBonus))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
thiefPartyPercent

The bonus a party earns: the per-thief percent for each member dressed as
a thief, capped. Zero with the flag off or without a party.
================
*/
func (rt *Runtime) thiefPartyPercent(division string, party *RewardParty, s *tunables.Settings) int {
	if !rt.ThiefPartyBonus || party == nil {
		return 0
	}
	thieves := 0
	for _, gid := range party.Members {
		if member := rt.findCharacterByGid(division, gid); member != nil && enterworld.DressedJob(member) == domain.JobThief {
			thieves++
		}
	}
	return min(thieves*int(s.Int(tunables.ThiefPartyPercentEach)), int(s.Int(tunables.ThiefPartyPercentCap)))
}

/*
================
thiefPartyExp

Monster EXP raised by the party's thief bonus.
================
*/
func thiefPartyExp(exp int64, percent int) int64 {
	if percent <= 0 || exp <= 0 {
		return exp
	}
	return exp + int64(float64(exp)*float64(percent)/100)
}

/*
================
applyThiefPartyGold

The owner's party's thief bonus on its planned gold drops.
================
*/
func (rt *Runtime) applyThiefPartyGold(division string, owner *enterworld.Character, planned []grounditem.Item, s *tunables.Settings) {
	if owner == nil {
		return
	}
	percent := rt.thiefPartyPercent(division, rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(owner)), s)
	if percent <= 0 {
		return
	}
	for i := range planned {
		if planned[i].IsGold() {
			amount := float64(planned[i].GoldAmount) * (1 + float64(percent)/100)
			planned[i].GoldAmount = uint32(min(amount, math.MaxUint32))
		}
	}
}
