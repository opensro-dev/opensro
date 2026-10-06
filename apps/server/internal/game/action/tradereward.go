/*
===========================================================================

tradereward.go - transaction roster and native trade reward recipients

The division lock stabilizes party membership; UpdateMany commits every
recipient's balance and progression alongside removal of the seller's cargo.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tradeRoster
================
*/
type tradeRoster struct {
	members []*enterworld.Character
	party   bool
}

/*
================
tradePayout
================
*/
type tradePayout struct {
	character *enterworld.Character
	commerce.TradeReward
}

/*
================
commerceRoster
================
*/
func (rt *Runtime) commerceRoster(division string, c *enterworld.Character) tradeRoster {
	roster := tradeRoster{members: []*enterworld.Character{c}}
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(c))
	if party == nil {
		return roster
	}
	roster.party = true
	seen := map[int64]bool{c.ID: true}
	for _, gid := range party.Members {
		member := rt.findCharacterByGid(division, gid)
		if member != nil && !seen[member.ID] && (rt.RewardActorPresent == nil || rt.RewardActorPresent(division, member.Name)) {
			seen[member.ID] = true
			roster.members = append(roster.members, member)
		}
	}
	return roster
}

/*
================
tradeRewards

5BCFF0: only living members of the requested job, in the seller's world,
within 1000 planar units. The seller remains the first transaction owner.
================
*/
func (rt *Runtime) tradeRewards(division string, c *enterworld.Character, roster tradeRoster, value commerce.TradeRewardInput) []tradePayout {
	now := rt.Now().UnixMilli()
	origin := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	eligible := make([]*enterworld.Character, 0, len(roster.members))
	value.Job = enterworld.DressedJob(c)
	value.Party = roster.party
	value.OneStar = caravan.DifficultyTier(rt.caravanCargoValue(c)) == 1
	for _, member := range roster.members {
		if member.ID == c.ID {
			member = c
		}
		if member.DeletePending || !enterworld.CharacterAlive(member) || domain.CharacterWorldInstance(member) != domain.CharacterWorldInstance(c) {
			continue
		}
		if !withinPartyRewardRange(origin, rt.liveSpawn(simulation.WorldKey(division, member.Name), member, now)) {
			continue
		}
		switch enterworld.DressedJob(member) {
		case domain.JobTrader:
			value.Traders++
		case domain.JobThief:
			value.Thieves++
		case domain.JobHunter:
			value.Hunters++
		default:
			continue
		}
		eligible = append(eligible, member)
	}
	distribution := commerce.DistributeTradeReward(value)
	out := []tradePayout{{c, distribution.Actor}}
	if !roster.party || value.Profit <= 0 {
		return out
	}
	for _, member := range eligible {
		if member.ID == c.ID {
			continue
		}
		if value.Job == domain.JobTrader && enterworld.DressedJob(member) == domain.JobHunter {
			out = append(out, tradePayout{member, distribution.Hunter})
		}
		if value.Job == domain.JobThief && enterworld.DressedJob(member) == domain.JobThief {
			out = append(out, tradePayout{member, distribution.Thief})
		}
	}
	return out
}
