/*
===========================================================================

christmas.go - So-ok's snow flake exchange (Winter Princess and Knights)

The v1.188 server's WINTER_EVENT_2009 talk at NPC_CH_EVENT_KISAENG1
(SRO_CHRISTMAS_EVENT in mode winter; the tables are
progression/christmas_loot.go): the prompt QUEST_A offers QUEST_B,
"Receive gift with 10 snow flakes."; a click trades every ten flakes in
the bag while it has room, answering QUEST_C on a gift,
QUEST_D on a full bag and QUEST_E without ten flakes. The click's gift
count is the Snow Flake Event's report (an exchange report,
winter_missions.go). The exchange itself is the shared one
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
const ChristmasExchangeService = "QEV_WINTER_EVENT_2009:exchange"

// So-ok's winter talk rows (v1.696 textevent, imported with the flake):
// the prompt page, the exchange row, and the three answers (gift
// granted, bag full, too few flakes).
const (
	christmasTalkPrompt    = "SN_TALK_QEV_SERVER_EVENT_QUEST_A"
	christmasTalkOption    = "SN_TALK_QEV_SERVER_EVENT_QUEST_B"
	christmasTalkGranted   = "SN_TALK_QEV_SERVER_EVENT_QUEST_C"
	christmasTalkBagFull   = "SN_TALK_QEV_SERVER_EVENT_QUEST_D"
	christmasTalkNoFlakes  = "SN_TALK_QEV_SERVER_EVENT_QUEST_E"
	christmasSnowflakeHost = halloweenSoOkCodename
)

/*
================
Runtime.christmasExchange
================
*/
func (rt *Runtime) christmasExchange() *seasonalExchange {
	return &seasonalExchange{
		name: "christmas", host: christmasSnowflakeHost, token: progression.ChristmasSnowflakeCodename,
		perGift: progression.ChristmasSnowflakesPerGift, active: rt.Christmas.ActiveAt,
		gifts: progression.ChristmasGifts, service: ChristmasExchangeService,
		talk: exchangeTalk{
			prompt: christmasTalkPrompt, option: christmasTalkOption, granted: christmasTalkGranted,
			bagFull: christmasTalkBagFull, noToken: christmasTalkNoFlakes,
		},
		exchanged: rt.reportWinterExchange,
	}
}

/*
================
Runtime.ChristmasTalkOptions

So-ok's winter row while the event runs, appended after her quest rows;
outside the window, nothing.
================
*/
func (rt *Runtime) ChristmasTalkOptions(npcCodename string, character *enterworld.Character) []NpcQuestOption {
	return rt.exchangeTalkOptions(rt.christmasExchange(), npcCodename, character)
}

/*
================
Runtime.ExchangeChristmasSnowflakes

Every ten flakes in the bag buy a rolled gift at once.
================
*/
func (rt *Runtime) ExchangeChristmasSnowflakes(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	return rt.exchangeTokens(rt.christmasExchange(), character, npcCodename)
}
