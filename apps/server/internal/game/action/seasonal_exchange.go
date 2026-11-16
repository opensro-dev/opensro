/*
===========================================================================

seasonal_exchange.go - So-ok's seasonal token exchange, one shape per event

Port-only, not native. The seasonal events (Halloween's jewelry boxes,
Christmas's Red Santa Socks) share one exchange at the town's event host:
a rolled row that spends tokens on weighted gifts (every token in the bag
at once, or the native one gift per click), then any flat-price prize
rows. The shipped talk text gives each event its own
prompt, row and answers (textevent QEV_CH_EVENT_KISAENG_<date>_A..E); the
rules are the sibling event quests', first written for Halloween
(halloween.go has the provenance): refusals answer first and remove
nothing, the roll picks the first gift whose running sum is not below it,
and removal and grant commit as one step inside the character's door.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/social/chat"
)

/*
================
seasonalExchange

One event's exchange: its host, its token, its tables, its talk symbols
and services, and its banners. The banners are port-only; no shipped
text carries them.
================
*/
type seasonalExchange struct {
	name    string // the door's operation label prefix
	host    string // the NPC codename that offers the rows
	token   string // the item the rows spend
	perGift uint32 // tokens one rolled gift costs
	oneGift bool   // one gift per click, as the sibling event; else the whole bag
	active  func(time.Time) bool
	gifts   func() []progression.HalloweenGift
	prizes  func() []progression.HalloweenPrize             // nil: no prize rows
	byName  func(string) (progression.HalloweenPrize, bool) // nil with prizes
	service string                                          // the rolled row's service codename
	prefix  string                                          // a prize row's service prefix; the prize follows
	talk    exchangeTalk
	banners exchangeBanners
}

/*
================
exchangeTalk

The shipped talk symbols: the prompt page, the exchange row, and the
three answers (granted, bag full, no token).
================
*/
type exchangeTalk struct {
	prompt, option, granted, bagFull, noToken string
}

/*
================
exchangeBanners

The top banners: gifts received (%d), gifts with tokens left (%d, %d),
a sale (%d tokens) and a short purse (%d price, %d held). The zero value
sends none: the talk lines alone answer, as natively.
================
*/
type exchangeBanners struct {
	giftDone, giftLeft, saleDone, short string
}

/*
================
Runtime.exchangeTalkOptions

The rows the host offers while the event runs: the rolled exchange
first, then the flat-price prize rows (a sex-bound prize only for that
sex). Appended after the NPC's quest rows; outside the window nothing.
================
*/
func (rt *Runtime) exchangeTalkOptions(ex *seasonalExchange, npcCodename string, character *enterworld.Character) []NpcQuestOption {
	if npcCodename != ex.host || character == nil || !ex.active(rt.Now()) {
		return nil
	}
	out := []NpcQuestOption{{Codename: ex.service, TitleSymbol: ex.talk.option, PromptSymbol: ex.talk.prompt, Immediate: true}}
	if ex.prizes == nil {
		return out
	}
	female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
	refs := rt.deps.ItemReferences()
	for _, prize := range ex.prizes() {
		if prize.Helmet && prize.Female != female {
			continue
		}
		title := "SN_" + prize.Codename
		if ref, ok := refs.ItemRefByCodename(prize.Codename); ok && ref != nil && ref.NameStrID != "" {
			title = ref.NameStrID
		}
		out = append(out, NpcQuestOption{Codename: ex.prefix + prize.Codename, TitleSymbol: title, PromptSymbol: ex.talk.prompt, Immediate: true})
	}
	return out
}

/*
================
Runtime.exchangeTokens

Every token in the bag buys a rolled gift at once, or one gift only
when the exchange keeps the native click (oneGift). No token answers the
no-token line; a first gift that cannot fit the bag as it stands answers
the bag-full line; later gifts stop at the first that does not fit and
leave their tokens. The top banner says what moved.
================
*/
func (rt *Runtime) exchangeTokens(ex *seasonalExchange, character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	if npcCodename != ex.host || !ex.active(rt.Now()) {
		return nil, fmt.Errorf("%s exchange is not offered at %s", ex.name, npcCodename)
	}
	answer := ""
	committed := false
	gifts := uint32(0)
	tokens := uint32(0)
	var frames []wire.Frame
	var rollErr error
	rt.deps.Update(character, ex.name+"-exchange", func() bool {
		tokens = bagTokens(character, ex.token)
		if tokens < ex.perGift {
			answer = ex.talk.noToken
			return false
		}
		var rows []enterworld.InventoryRow
		wanted := tokens / ex.perGift
		if ex.oneGift {
			wanted = 1
		}
		answer, rows, frames, gifts, rollErr = rt.planExchangeGifts(ex, character, wanted)
		if rollErr != nil || gifts == 0 {
			return false
		}
		character.MissionInventory = rows
		committed = true
		return true
	})
	if rollErr != nil || (!committed && (answer == "" || answer == ex.talk.granted)) {
		return nil, fmt.Errorf("%s exchange could not commit", ex.name)
	}
	if committed && ex.banners.giftDone != "" {
		frames = append(frames, exchangeBanner(ex.giftsBanner(gifts, tokens-gifts*ex.perGift)))
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(answer)}), nil
}

/*
================
Runtime.planExchangeGifts

Rolls up to wanted gifts and plans them as one change, run inside the
caller's door. The first gift keeps the sibling's test against the bag as
it stands; each later one is planned with its tokens removed. It returns
the answer line, the planned rows and frames of the last gift that fit,
and how many fit.
================
*/
func (rt *Runtime) planExchangeGifts(ex *seasonalExchange, character *enterworld.Character, wanted uint32) (string, []enterworld.InventoryRow, []wire.Frame, uint32, error) {
	var rows []enterworld.InventoryRow
	var frames []wire.Frame
	var grant []inventory.ItemAmount
	female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
	gifts := uint32(0)
	for gifts < wanted {
		gift, rollErr := rt.rollExchangeGift(ex.gifts())
		if rollErr != nil {
			return "", nil, nil, 0, rollErr
		}
		codename := gift.Codename
		if female && gift.FemaleCodename != "" {
			codename = gift.FemaleCodename
		}
		next := addExchangeGrant(grant, codename, gift.Count)
		if gifts == 0 {
			if _, _, planErr := rt.PlanQuestInventory(character, nil, next); planErr != nil {
				break
			}
		}
		consume := []inventory.ItemAmount{{Codename: ex.token, Count: (gifts + 1) * ex.perGift}}
		planned, plannedFrames, planErr := rt.PlanQuestInventory(character, consume, next)
		if planErr != nil {
			break
		}
		rows, frames, grant = planned, plannedFrames, next
		gifts++
	}
	if gifts == 0 {
		return ex.talk.bagFull, nil, nil, 0, nil
	}
	return ex.talk.granted, rows, frames, gifts, nil
}

/*
================
addExchangeGrant

One more gift on a copy of the grant list, same codenames merged so the
plan fills one stack rather than opening a row per gift.
================
*/
func addExchangeGrant(grant []inventory.ItemAmount, codename string, count uint32) []inventory.ItemAmount {
	next := append([]inventory.ItemAmount(nil), grant...)
	for i := range next {
		if next[i].Codename == codename {
			next[i].Count += count
			return next
		}
	}
	return append(next, inventory.ItemAmount{Codename: codename, Count: count})
}

/*
================
Runtime.exchangePrize

One flat-price sale: the row's stated price in tokens buys the exact
prize the picked row names. Too few tokens answer the no-token line and
the banner names the price and what the bag holds; a prize that cannot
fit the bag as it stands answers the bag-full line; only then do removal
and grant commit together. A sex-bound row for the other sex is never
offered, and asking for one anyway is refused the same as no token.
================
*/
func (rt *Runtime) exchangePrize(ex *seasonalExchange, character *enterworld.Character, npcCodename, service string) ([]wire.Frame, error) {
	if npcCodename != ex.host || !ex.active(rt.Now()) || ex.byName == nil {
		return nil, fmt.Errorf("%s prize sale is not offered at %s", ex.name, npcCodename)
	}
	prize, known := ex.byName(strings.TrimPrefix(service, ex.prefix))
	if !known {
		return nil, fmt.Errorf("%s prize sale names no prize: %s", ex.name, service)
	}
	answer := ""
	committed := false
	banner := ""
	var frames []wire.Frame
	rt.deps.Update(character, ex.name+"-prize", func() bool {
		female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
		if prize.Helmet && prize.Female != female {
			answer = ex.talk.noToken
			return false
		}
		if held := bagTokens(character, ex.token); held < prize.Price {
			answer = ex.talk.noToken
			banner = fmt.Sprintf(ex.banners.short, prize.Price, held)
			return false
		}
		grant := []inventory.ItemAmount{{Codename: prize.Codename, Count: 1}}
		answer, frames, committed = rt.commitSeasonalExchange(ex, character, prize.Price, grant)
		return committed
	})
	if !committed && (answer == "" || answer == ex.talk.granted) {
		return nil, fmt.Errorf("%s prize sale could not commit", ex.name)
	}
	if committed {
		banner = fmt.Sprintf(ex.banners.saleDone, prize.Price)
	}
	if banner != "" {
		frames = append(frames, exchangeBanner(banner))
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(answer)}), nil
}

/*
================
Runtime.commitSeasonalExchange

The shared commit, run inside the caller's door: the price in tokens
must be in the bag, the grant must fit the bag as it stands (tokens
still in it, as the sibling's refusal precedes any removal), and only
then do removal and grant land together.
================
*/
func (rt *Runtime) commitSeasonalExchange(ex *seasonalExchange, character *enterworld.Character, price uint32, grant []inventory.ItemAmount) (string, []wire.Frame, bool) {
	if bagTokens(character, ex.token) < price {
		return ex.talk.noToken, nil, false
	}
	if _, _, planErr := rt.PlanQuestInventory(character, nil, grant); planErr != nil {
		return ex.talk.bagFull, nil, false
	}
	consume := []inventory.ItemAmount{{Codename: ex.token, Count: price}}
	rows, planned, planErr := rt.PlanQuestInventory(character, consume, grant)
	if planErr != nil {
		return ex.talk.bagFull, nil, false
	}
	character.MissionInventory = rows
	return ex.talk.granted, planned, true
}

/*
================
exchangeBanner

The top banner for one exchange. Port-only, not native: the sibling
event's lines stay in the talk pane; the owner wants the result on screen.
================
*/
func exchangeBanner(text string) wire.Frame {
	return wire.Frame{Opcode: chat.OpChatBroadcast, Payload: chat.EncodeChatNotice(text)}
}

/*
================
seasonalExchange.giftsBanner
================
*/
func (ex *seasonalExchange) giftsBanner(gifts, tokensLeft uint32) string {
	if tokensLeft >= ex.perGift {
		return fmt.Sprintf(ex.banners.giftLeft, gifts, tokensLeft)
	}
	return fmt.Sprintf(ex.banners.giftDone, gifts)
}

/*
================
Runtime.rollExchangeGift

The sibling roll: rand/32767 over the gift table's own total weight,
the first row whose running sum is not below the roll, so the sibling
ratios hold exactly on the kept rows. A float sum can end a hair under
the total; the last row then takes the top of the range.
================
*/
func (rt *Runtime) rollExchangeGift(gifts []progression.HalloweenGift) (progression.HalloweenGift, error) {
	if rt.DropRoll == nil || len(gifts) == 0 {
		return progression.HalloweenGift{}, fmt.Errorf("no gift roll source")
	}
	draw, err := rt.DropRoll()
	if err != nil {
		return progression.HalloweenGift{}, err
	}
	total := 0.0
	for _, gift := range gifts {
		total += gift.Weight
	}
	roll := float64(draw&32767) / 32767.0 * total
	sum := 0.0
	for _, gift := range gifts {
		sum += gift.Weight
		if sum >= roll {
			return gift, nil
		}
	}
	return gifts[len(gifts)-1], nil
}

/*
================
bagTokens

Tokens in the bag band of the inventory (the rows an exchange may take).
================
*/
func bagTokens(character *enterworld.Character, token string) uint32 {
	var tokens uint32
	for _, row := range character.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) && row.Codename == token {
			tokens += uint32(row.StackCount)
		}
	}
	return tokens
}
