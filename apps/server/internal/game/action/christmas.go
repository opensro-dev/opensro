/*
===========================================================================

christmas.go - Santa So-Ok's Red Santa Socks exchange

Port-only, not native (SRO_CHRISTMAS_EVENT, default off; the tables are
progression/christmas_loot.go). While the window runs So-ok
(NPC_CH_EVENT_KISAENG1, the town's event host; the shipped Santa So-Ok
row, NPC_CH_EVENT_SANTA_KISAENG1, has no place in the world data) offers
the December 2007 event's one row, "Receive present for Red Santa
Socks.": one pair buys one rolled present, and one click spends every
pair in the bag (owner, 2026-11-15; the sibling event quests take one
token per click). Only the shipped talk lines answer (textevent
QEV_CH_EVENT_KISAENG_071225_A..E): no prize rows and no banners. The exchange itself is the shared one
(seasonal_exchange.go).

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
)

// ChristmasExchangeService routes So-ok's gift row through
// NpcQuestHooks.Finish, as HalloweenExchangeService does.
const ChristmasExchangeService = "QEV_CH_EVENT_KISAENG_071225:exchange"

// So-ok's Christmas talk rows (textevent): the prompt page, the exchange
// row, and the three answers (gift granted, bag full, no socks).
const (
	christmasTalkPrompt  = "SN_TALK_QEV_CH_EVENT_KISAENG_071225_A"
	christmasTalkOption  = "SN_TALK_QEV_CH_EVENT_KISAENG_071225_B"
	christmasTalkGranted = "SN_TALK_QEV_CH_EVENT_KISAENG_071225_C"
	christmasTalkBagFull = "SN_TALK_QEV_CH_EVENT_KISAENG_071225_D"
	christmasTalkNoSocks = "SN_TALK_QEV_CH_EVENT_KISAENG_071225_E"
)

/*
================
Runtime.christmasExchange
================
*/
func (rt *Runtime) christmasExchange() *seasonalExchange {
	return &seasonalExchange{
		name: "christmas", host: halloweenSoOkCodename, token: progression.ChristmasSockCodename,
		perGift: progression.ChristmasSocksPerGift, active: rt.Christmas.ActiveAt,
		gifts: progression.ChristmasGifts, service: ChristmasExchangeService,
		talk: exchangeTalk{
			prompt: christmasTalkPrompt, option: christmasTalkOption, granted: christmasTalkGranted,
			bagFull: christmasTalkBagFull, noToken: christmasTalkNoSocks,
		},
	}
}

/*
================
Runtime.ChristmasTalkOptions

So-ok's Christmas rows while the event runs, appended after her quest
rows; outside the window, nothing.
================
*/
func (rt *Runtime) ChristmasTalkOptions(npcCodename string, character *enterworld.Character) []NpcQuestOption {
	return rt.exchangeTalkOptions(rt.christmasExchange(), npcCodename, character)
}

/*
================
Runtime.ExchangeChristmasSocks

Every pair in the bag buys a rolled present at once.
================
*/
func (rt *Runtime) ExchangeChristmasSocks(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	return rt.exchangeTokens(rt.christmasExchange(), character, npcCodename)
}
