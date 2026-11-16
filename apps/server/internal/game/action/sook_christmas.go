/*
===========================================================================

sook_christmas.go - So-Ok's Christmas Gift: the Jewel Box drop and Santa
So-Ok's exchange

Port-only, not native (SRO_CHRISTMAS_EVENT in mode so_ok_2006, default
off; the rules are progression/sook_christmas.go). While the window runs
an ordinary monster's kill may leave one Jewel Box, and So-ok
(NPC_CH_EVENT_KISAENG1, dressed as Santa So-Ok for the window,
npc_looks.go) offers the December 2006 event's one row, "Receive a gift
box as a gift.": one box buys one rolled gift. Only the shipped talk
lines answer (textevent QEV_CH_EVENT_KISAENG_061220_A..E); the exchange
itself is the shared one (seasonal_exchange.go), so its refusals answer
first and remove nothing, and removal and grant commit as one step.

The box is Halloween's token too. Its entry cleanup (halloween.go
RemoveExpiredEventItems) keeps the boxes while either event runs and
takes them once both are over.

===========================================================================
*/
package action

import (
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// SoOkChristmasExchangeService routes the gift row through
// NpcQuestHooks.Finish, as HalloweenExchangeService does.
const SoOkChristmasExchangeService = "QEV_CH_EVENT_KISAENG_061220:exchange"

// The 2006 event's talk rows (textevent): the prompt page, the exchange
// row, and the three answers (gift granted, bag full, no box).
const (
	sookChristmasTalkPrompt  = "SN_TALK_QEV_CH_EVENT_KISAENG_061220_A"
	sookChristmasTalkOption  = "SN_TALK_QEV_CH_EVENT_KISAENG_061220_B"
	sookChristmasTalkGranted = "SN_TALK_QEV_CH_EVENT_KISAENG_061220_C"
	sookChristmasTalkBagFull = "SN_TALK_QEV_CH_EVENT_KISAENG_061220_D"
	sookChristmasTalkNoBox   = "SN_TALK_QEV_CH_EVENT_KISAENG_061220_E"

	// The top banners, as Halloween's. Port-only, not native.
	sookChristmasGiftsDoneBanner = "Exchanged your Jewel Boxes: %d gift(s) received."
	sookChristmasGiftsLeftBanner = "%d gift(s) received. Your bag is full, %d Jewel Box(es) left."
)

// sookChristmasChanceScale is the drop roll's domain (0..32767, as every
// other drop's 15-bit draw).
const sookChristmasChanceScale = 32768

/*
================
Runtime.sookChristmasExchange
================
*/
func (rt *Runtime) sookChristmasExchange() *seasonalExchange {
	return &seasonalExchange{
		name: "sook-christmas", host: progression.SoOkChristmasHostCodename, token: progression.SoOkChristmasBoxCodename,
		perGift: progression.SoOkChristmasBoxesPerGift, active: rt.SoOkChristmas.ActiveAt,
		gifts: rt.SoOkChristmas.Gifts, service: SoOkChristmasExchangeService,
		talk: exchangeTalk{
			prompt: sookChristmasTalkPrompt, option: sookChristmasTalkOption, granted: sookChristmasTalkGranted,
			bagFull: sookChristmasTalkBagFull, noToken: sookChristmasTalkNoBox,
		},
		banners: exchangeBanners{giftDone: sookChristmasGiftsDoneBanner, giftLeft: sookChristmasGiftsLeftBanner},
	}
}

/*
================
Runtime.SoOkChristmasTalkOptions

So-ok's 2006 row while the event runs, appended after her quest rows;
outside the window, nothing.
================
*/
func (rt *Runtime) SoOkChristmasTalkOptions(npcCodename string, character *enterworld.Character) []NpcQuestOption {
	return rt.exchangeTalkOptions(rt.sookChristmasExchange(), npcCodename, character)
}

/*
================
Runtime.ExchangeSoOkChristmasBoxes

Every box in the bag buys a rolled gift at once, as the other seasonal
exchanges do (the owner's whole-bag click); the gifts that do not fit
leave their boxes in the bag.
================
*/
func (rt *Runtime) ExchangeSoOkChristmasBoxes(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	return rt.exchangeTokens(rt.sookChristmasExchange(), character, npcCodename)
}

/*
================
Runtime.sookChristmasBoxDrop

The kill's event drop: at most one Jewel Box, rolled once per kill (a
party kill plans once, reward_group.go), while the window runs. Only an
ordinary monster pays: no unique and no GM-conjured kill. The box passes
the native level-gap admission (admitMonsterDrop) like any ordinary
drop, so a hunter far above the monster loots nothing, and its chance is
the event's own, outside the shard's drop rates and the drop cap; the
caller appends it after both.
================
*/
func (rt *Runtime) sookChristmasBoxDrop(winner *enterworld.Character, defeated monster.Instance, at simulation.Spawn, now time.Time) (grounditem.Item, bool) {
	if !rt.SoOkChristmas.ActiveAt(now) || winner == nil || winner.Level == nil || rt.DropRoll == nil {
		return grounditem.Item{}, false
	}
	if defeated.GMSpawned || RollsUniqueDrops(defeated.Rarity()) {
		return grounditem.Item{}, false
	}
	draw, err := rt.DropRoll()
	if err != nil || float64(draw&(sookChristmasChanceScale-1)) >= rt.SoOkChristmas.BoxChancePercent()/100*sookChristmasChanceScale {
		return grounditem.Item{}, false
	}
	if !rt.admitMonsterDrop(uint8(*winner.Level), defeated) {
		return grounditem.Item{}, false
	}
	return rt.prepareSelectedDrop(loot.DropItem{Codename: progression.SoOkChristmasBoxCodename, Count: 1, Assigned: true}, at, winner.Name, now)
}
