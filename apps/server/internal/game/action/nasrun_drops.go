/*
===========================================================================

nasrun_drops.go - Devil's Spirit lucky drops paid in a unique's settlement

Port-only, not native (#3456 step 3d), on with SRO_NASRUN, beside the
token award (nasrun_tokens.go). The owner picked the odds (2026-11-16,
#3456): on a unique kill, each eligible contributor rolls for a Devil's
Spirit (0.25 % easy, 0.5 % hard) and for Sabakun's jewel (2 %), under
normal loot rights. The odds are game/nasrun/drops.go; this file rolls
them and puts what was won in the winner's bag.

It runs inside the same door as the tokens (settleMonsterInsideDoor),
on the same roster (nasrunTokenEarners), so a contributor the token
award skips - a second character of the account, a kill past the level
band, a GM-spawned or arena kill - cannot roll either. Nothing is
saved: each roll is independent, there is no cap, and a winner whose
bag cannot hold the prize keeps neither (planInventoryGrant refuses a
full bag as for quest rewards). Off, the default, is native.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/world/simulation"
)

// The prizes, the rows scripts/nasrun_import.py stages: the plain
// A grade Spirits the token shop sells in both sexes, and Sabakun's
// jewel. Inferred: the owner's table names "the Spirit" as one prize,
// and the six A rows share one price, so the drop grants the plain row
// of the winner's sex - a usable Spirit at the same value - and leaves
// the colours to the shop.
const (
	nasrunDropSpiritMale   = "ITEM_MALL_AVATAR_M_NASRUN"
	nasrunDropSpiritFemale = "ITEM_MALL_AVATAR_W_NASRUN"
	nasrunDropJewel        = "ITEM_ETC_NASRUN_ARCHEMY_UPGRADE_STONE"
)

// The win notices travel as the native subtype-7 chat notice, through
// sookBanner, like the token award's.
const (
	nasrunSpiritDropNotice = "Lucky drop: a Devil's Spirit is in your bag."
	nasrunJewelDropNotice  = "Lucky drop: Sabakun's Jewel is in your bag."
)

/*
================
payNasrunLuckyDropsInsideDoor

Rolls each eligible contributor's lucky drops for a fatal unique hit
and grants what was won: the winner's grant frames and notice on the
reply, the others' as recipient frames. Same door, same roster and
same exclusions as payNasrunTokensInsideDoor.
================
*/
func (rt *Runtime) payNasrunLuckyDropsInsideDoor(division string, actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult, at simulation.Spawn, arena bool, now int64) ([]wire.Frame, []RecipientFrames) {
	if !rt.NasrunTokens || arena || !impact.Fatal || impact.Instance.GMSpawned || !impact.Instance.IsUnique() {
		return nil, nil
	}
	spiritBps, jewelBps, ok := nasrun.LuckyDropOdds(impact.Instance.Ref.BaseCodename())
	if !ok {
		return nil, nil
	}
	roll := dropRoller{rt}
	var actorFrames []wire.Frame
	var others []RecipientFrames
	for _, earner := range rt.nasrunTokenEarners(division, roster, impact, at, now) {
		c := earner.character
		var grant []inventory.ItemAmount
		var notices []string
		if roll.Float() < float64(spiritBps)/float64(nasrun.DropBps) {
			grant = append(grant, inventory.ItemAmount{Codename: nasrunSpiritFor(c), Count: 1})
			notices = append(notices, nasrunSpiritDropNotice)
		}
		if roll.Float() < float64(jewelBps)/float64(nasrun.DropBps) {
			grant = append(grant, inventory.ItemAmount{Codename: nasrunDropJewel, Count: 1})
			notices = append(notices, nasrunJewelDropNotice)
		}
		if len(grant) == 0 {
			continue
		}
		rows, frames, err := rt.PlanQuestInventory(c, nil, grant)
		if err != nil {
			// A full bag (or, with the import off, an unresolved row)
			// keeps the prize unrolled-for: the notice never travels
			// for an item that did not land.
			log.WithField("character", c.Name).WithField("monster", impact.Instance.Ref.Codename).
				Info("nasrun lucky drop: grant refused")
			continue
		}
		c.MissionInventory = rows
		for _, notice := range notices {
			frames = append(frames, sookBanner(notice))
		}
		if c == actor {
			actorFrames = append(actorFrames, frames...)
		} else {
			others = append(others, RecipientFrames{CharacterID: c.ID, Frames: frames})
		}
	}
	return actorFrames, others
}

/*
================
nasrunSpiritFor

The plain A grade Spirit of the winner's sex.
================
*/
func nasrunSpiritFor(c *enterworld.Character) string {
	if enterworld.ResolveCharacterGenderIndex(c) == enterworld.GenderFemale {
		return nasrunDropSpiritFemale
	}
	return nasrunDropSpiritMale
}
