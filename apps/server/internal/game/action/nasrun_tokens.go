/*
===========================================================================

nasrun_tokens.go - Devil's Spirit tokens paid in a unique's settlement

Port-only, not native (#3456 step 3a), on with SRO_NASRUN. The rules are
game/nasrun/tokens.go; this file decides who earns and writes the award.
Off, the default, is native: no kill pays a token and nothing is sent.

It runs inside the fatal hit's UpdateMany door (settleMonsterInsideDoor),
whose participants are every roster character, so the ledger write lands
in the same transaction as the kill's experience and loot.

===========================================================================
*/
package action

import (
	"fmt"
	"sort"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/world/simulation"
)

// The notices travel as the native subtype-7 chat notice, through
// sookBanner, which keeps its 96-unit length contract.
const (
	nasrunTokenNotice    = "Devil's Spirit tokens +%d (balance %d, today %d/%d)."
	nasrunTokenCapNotice = "You have earned today's %d Devil's Spirit tokens; the count restarts at 04:00 UTC."
	// nasrunTokenSpentNotice tells a token shop purchase (step 3b).
	nasrunTokenSpentNotice = "Devil's Spirit tokens -%d (balance %d)."
)

/*
================
nasrunTokenEarner

One contributor who earns: the account's top contributor and its
credited damage.
================
*/
type nasrunTokenEarner struct {
	gid       uint32
	character *enterworld.Character
	damage    uint32
}

/*
================
nasrunTokenEarners

The eligible contributors of a unique's kill, one per account, in a
stable order (damage, then GID). Inferred, as no source has the rule (the
owner's "present near the unique and dealt damage, one award per account
per kill, within the unique's level band"):

  - dealt damage: a row of the native contribution table, the one the EXP
    and loot split read, with pets credited to their owner;
  - present: in the roster, alive, in the unique's world and within the
    party share range of where it died (withinPartyRewardRange);
  - one per account: an account with several characters in the kill earns
    once, on its top contributor;
  - the band: nasrun.WithinTokenBand of the unique's level.

================
*/
func (rt *Runtime) nasrunTokenEarners(division string, roster rewardRoster, impact simulation.MonsterDamageResult, at simulation.Spawn, now int64) []nasrunTokenEarner {
	world := uint32(impact.Population.ID)
	damage := make(map[uint32]uint32, len(impact.Contributions))
	for _, row := range impact.Contributions {
		damage[row.CreditGID] += row.Damage
	}
	best := make(map[string]nasrunTokenEarner)
	for gid, dealt := range damage {
		a, ok := roster.actors[gid]
		if !ok || dealt == 0 || a.world != world || !enterworld.CharacterAlive(a.character) {
			continue
		}
		c := a.character
		if !nasrun.WithinTokenBand(rewardLevel(c), int64(impact.Instance.Ref.Level)) {
			continue
		}
		if !withinPartyRewardRange(at, rt.rewardActorPose(division, a, now)) {
			continue
		}
		key := c.AccountID
		if key == "" {
			// A character with no account is its own earner.
			key = fmt.Sprintf("char:%d", c.ID)
		}
		earner := nasrunTokenEarner{gid: gid, character: c, damage: dealt}
		if held, ok := best[key]; !ok || earner.damage > held.damage || (earner.damage == held.damage && gid < held.gid) {
			best[key] = earner
		}
	}
	earners := make([]nasrunTokenEarner, 0, len(best))
	for _, earner := range best {
		earners = append(earners, earner)
	}
	sort.Slice(earners, func(i, j int) bool {
		if earners[i].damage != earners[j].damage {
			return earners[i].damage > earners[j].damage
		}
		return earners[i].gid < earners[j].gid
	})
	return earners
}

/*
================
payNasrunTokensInsideDoor

Pays each eligible contributor of a fatal unique hit and returns the
notices: the actor's travel on the reply, the others' as recipient
frames. A GM-spawned unique is conjured, not hunted, and pays nothing,
as for the seal board; an arena kill pays nothing either.
================
*/
func (rt *Runtime) payNasrunTokensInsideDoor(division string, actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult, at simulation.Spawn, arena bool, now int64) ([]wire.Frame, []RecipientFrames) {
	if !rt.NasrunTokens || arena || !impact.Fatal || impact.Instance.GMSpawned || impact.Instance.Rarity()&15 != uniqueRarityGrade {
		return nil, nil
	}
	award := nasrun.UniqueTokens(impact.Instance.Ref.Codename)
	if award == 0 {
		return nil, nil
	}
	today := nasrun.TokenDay(time.UnixMilli(now).UTC())
	var actorFrames []wire.Frame
	var others []RecipientFrames
	for _, earner := range rt.nasrunTokenEarners(division, roster, impact, at, now) {
		c := earner.character
		grant := nasrun.GrantTokens(c.NasrunTokens, c.NasrunTokenDay, c.NasrunTokenDayCount, award, today)
		if grant.Granted == 0 {
			continue
		}
		c.NasrunTokens, c.NasrunTokenDay, c.NasrunTokenDayCount = grant.Balance, grant.Day, grant.DayCount
		frames := []wire.Frame{sookBanner(fmt.Sprintf(nasrunTokenNotice, grant.Granted, grant.Balance, grant.DayCount, nasrun.TokenDailyCap))}
		if grant.Reached {
			frames = append(frames, sookBanner(fmt.Sprintf(nasrunTokenCapNotice, nasrun.TokenDailyCap)))
		}
		if c == actor {
			actorFrames = append(actorFrames, frames...)
		} else {
			others = append(others, RecipientFrames{CharacterID: c.ID, Frames: frames})
		}
	}
	return actorFrames, others
}
