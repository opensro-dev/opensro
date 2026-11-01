/*
===========================================================================

halloween.go - the Halloween Day event: box drops, So-ok's exchange, cleanup

Port-only, not native (progression/halloween.go owns the switch, window
and gift table). While the window runs, ordinary monsters drop jewelry
boxes and So-ok (NPC_CH_EVENT_KISAENG1) offers extra talk rows: one
exchanges a box for a weighted random gift, and one row per helmet of
the character's sex sells that exact helmet at the flat box price. The
exchange's shape follows the sibling event quests: refusals answer
first and remove nothing, the roll picks the first gift whose running
sum is not below it, and the box removal and gift grant commit as one
step (the older sibling grants before it removes; that order is not
copied). Boxes mint under a per-character daily cap that every source
counts toward (the owner's rule, 2026-10-31); a Gourd kill always pays
its box past the level gap, still under the cap. After the window, the
first world entry takes the remaining boxes.

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// HalloweenExchangeService routes So-ok's gift row through
// NpcQuestHooks.Finish, the immediate-service shape of
// quest.ResuscitationService.
const HalloweenExchangeService = "QEV_CH_EVENT_KISAENG_071030:exchange"

// HalloweenHelmetServicePrefix routes one helmet row the same way; the
// helmet's codename follows the prefix, so the exact row a player picks
// is the row the exchange grants.
const HalloweenHelmetServicePrefix = "QEV_CH_EVENT_KISAENG_071030:helmet:"

// halloweenSoOkCodename is the event's host (character id 3861).
const halloweenSoOkCodename = "NPC_CH_EVENT_KISAENG1"

// halloweenGourdCodename is the event's field pumpkin: its kill always
// pays one box, past the level gap that admits nothing else from it.
const halloweenGourdCodename = "MOB_EV_PUMPKIN"

// So-ok's talk rows (textevent): the prompt page, the exchange row, and
// the three answers (gift granted, bag full, no box).
const (
	halloweenTalkPrompt  = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_A"
	halloweenTalkOption  = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_B"
	halloweenTalkGranted = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_C"
	halloweenTalkBagFull = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_D"
	halloweenTalkNoBox   = "SN_TALK_QEV_CH_EVENT_KISAENG_071030_E"
)

/*
================
Runtime.HalloweenTalkOptions

The rows So-ok offers while the event runs: the rolled exchange first,
then one row per helmet made for the character's sex (the exact row is
the prize; the other sex's rows are never offered). Appended after the
NPC's quest rows, so Capture the Flag and every quest row keep their
places; the zero posture appends nothing and talk is native.
================
*/
func (rt *Runtime) HalloweenTalkOptions(npcCodename string, character *enterworld.Character) []NpcQuestOption {
	if npcCodename != halloweenSoOkCodename || character == nil || !rt.Halloween.ActiveAt(rt.Now()) {
		return nil
	}
	out := []NpcQuestOption{{
		Codename: HalloweenExchangeService, TitleSymbol: halloweenTalkOption,
		PromptSymbol: halloweenTalkPrompt, Immediate: true,
	}}
	female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
	refs := rt.deps.ItemReferences()
	for _, helmet := range progression.HalloweenHelmets() {
		if helmet.Female != female {
			continue
		}
		title := "SN_" + helmet.Codename
		if ref, ok := refs.ItemRefByCodename(helmet.Codename); ok && ref != nil && ref.NameStrID != "" {
			title = ref.NameStrID
		}
		out = append(out, NpcQuestOption{
			Codename: HalloweenHelmetServicePrefix + helmet.Codename, TitleSymbol: title,
			PromptSymbol: halloweenTalkPrompt, Immediate: true,
		})
	}
	return out
}

/*
================
Runtime.ExchangeHalloweenBox

One exchange: the bag's boxes buy one rolled gift. No box answers the
no-box line; a gift that cannot fit the bag as it stands (boxes still in
it, as the sibling's refusal precedes any removal) answers the bag-full
line; only then do removal and grant commit together and the granted
line closes the talk. Any refusal leaves every row untouched.
================
*/
func (rt *Runtime) ExchangeHalloweenBox(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	if npcCodename != halloweenSoOkCodename || !rt.Halloween.ActiveAt(rt.Now()) {
		return nil, fmt.Errorf("halloween exchange is not offered at %s", npcCodename)
	}
	answer := ""
	committed := false
	var frames []wire.Frame
	rt.deps.Update(character, "halloween-exchange", func() bool {
		if halloweenBagBoxes(character) < progression.HalloweenBoxesPerGift {
			answer = halloweenTalkNoBox
			return false
		}
		gift, rollErr := rt.rollHalloweenGift()
		if rollErr != nil {
			answer = ""
			frames = nil
			return false
		}
		grant := []inventory.ItemAmount{{Codename: gift.Codename, Count: gift.Count}}
		answer, frames, committed = rt.commitHalloweenExchange(character, progression.HalloweenBoxesPerGift, grant)
		return committed
	})
	if !committed && (answer == "" || answer == halloweenTalkGranted) {
		return nil, fmt.Errorf("halloween exchange could not commit")
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(answer)}), nil
}

/*
================
Runtime.ExchangeHalloweenHelmet

One helmet sale: the flat box price buys the exact helmet the picked
row names. Too few boxes answer the no-box line; a helmet that cannot
fit the bag as it stands answers the bag-full line; only then do
removal and grant commit together. A row for the other sex is never
offered, and asking for one anyway is refused the same as no box.
================
*/
func (rt *Runtime) ExchangeHalloweenHelmet(character *enterworld.Character, npcCodename, service string) ([]wire.Frame, error) {
	if npcCodename != halloweenSoOkCodename || !rt.Halloween.ActiveAt(rt.Now()) {
		return nil, fmt.Errorf("halloween helmet sale is not offered at %s", npcCodename)
	}
	helmet, known := progression.HalloweenHelmetByCodename(strings.TrimPrefix(service, HalloweenHelmetServicePrefix))
	if !known {
		return nil, fmt.Errorf("halloween helmet sale names no helmet: %s", service)
	}
	answer := ""
	committed := false
	var frames []wire.Frame
	rt.deps.Update(character, "halloween-helmet", func() bool {
		female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
		if helmet.Female != female || halloweenBagBoxes(character) < progression.HalloweenHelmetPrice {
			answer = halloweenTalkNoBox
			return false
		}
		grant := []inventory.ItemAmount{{Codename: helmet.Codename, Count: 1}}
		answer, frames, committed = rt.commitHalloweenExchange(character, progression.HalloweenHelmetPrice, grant)
		return committed
	})
	if !committed && (answer == "" || answer == halloweenTalkGranted) {
		return nil, fmt.Errorf("halloween helmet sale could not commit")
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogSymbol(answer)}), nil
}

/*
================
Runtime.commitHalloweenExchange

The shared commit, run inside the caller's door: the price in boxes
must be in the bag, the grant must fit the bag as it stands (boxes
still in it, as the sibling's refusal precedes any removal), and only
then do removal and grant land together.
================
*/
func (rt *Runtime) commitHalloweenExchange(character *enterworld.Character, price uint32, grant []inventory.ItemAmount) (string, []wire.Frame, bool) {
	if halloweenBagBoxes(character) < price {
		return halloweenTalkNoBox, nil, false
	}
	if _, _, planErr := rt.PlanQuestInventory(character, nil, grant); planErr != nil {
		return halloweenTalkBagFull, nil, false
	}
	consume := []inventory.ItemAmount{{Codename: progression.HalloweenBoxCodename, Count: price}}
	rows, planned, planErr := rt.PlanQuestInventory(character, consume, grant)
	if planErr != nil {
		return halloweenTalkBagFull, nil, false
	}
	character.MissionInventory = rows
	return halloweenTalkGranted, planned, true
}

/*
================
Runtime.rollHalloweenGift

The sibling roll: rand/32767 over the gift table's own total weight,
the first row whose running sum is not below the roll, so the sibling
ratios hold exactly on the kept rows. A float sum can end a hair under
the total; the last row then takes the top of the range.
================
*/
func (rt *Runtime) rollHalloweenGift() (progression.HalloweenGift, error) {
	gifts := progression.HalloweenGifts()
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
halloweenBagBoxes

Boxes in the bag band of the inventory (the rows an exchange may take).
================
*/
func halloweenBagBoxes(character *enterworld.Character) uint32 {
	var boxes uint32
	for _, row := range character.MissionInventory {
		if row.Slot >= int64(inventory.EquipmentSlotEnd) && row.Codename == progression.HalloweenBoxCodename {
			boxes += uint32(row.StackCount)
		}
	}
	return boxes
}

/*
================
Runtime.halloweenBoxDrop

The kill's event drop: one box while the window runs and the winner's
day still has room under the cap, rolled on the same 15-bit draw as
every other drop. A Gourd kill skips the draw and pays past the level
gap; the cap still gates it. The caller appends the drop after
admission and the drop cap, like the unique ticket, so neither can
take it. The winner snapshot's count is the gate: kills settle one at
a time, and countHalloweenBoxes lands each minted box on the live
character before the next kill plans.
================
*/
func (rt *Runtime) halloweenBoxDrop(winner *enterworld.Character, defeated monster.Instance, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	if !rt.Halloween.ActiveAt(now) || winner == nil {
		return grounditem.Item{}, false
	}
	if winner.HalloweenBoxDay == progression.HalloweenDayKey(now) &&
		winner.HalloweenBoxCount >= progression.HalloweenBoxDailyCap {
		return grounditem.Item{}, false
	}
	if defeated.Ref.Codename != halloweenGourdCodename {
		if RollsUniqueDrops(defeated.Rarity()) || rt.DropRoll == nil {
			return grounditem.Item{}, false
		}
		draw, err := rt.DropRoll()
		if err != nil || int64(draw&32767)*100 >= progression.HalloweenBoxDropChancePercent*32768 {
			return grounditem.Item{}, false
		}
	}
	return rt.prepareSelectedDrop(loot.DropItem{Codename: progression.HalloweenBoxCodename, Count: 1, Assigned: true}, at, owner, now)
}

/*
================
Runtime.countHalloweenBoxes

Counts the boxes one committed kill settlement minted for its winner,
from every source (the rolled drop and the Gourd alike), under the
04:00-UTC day key. Runs inside the settlement's door, so the winner's
fields move directly and the door commits them; a GM-spawned kill is
conjured, not hunted, and its caller never counts it. A day that has
rolled over restarts the count before this kill's boxes land.
================
*/
func (rt *Runtime) countHalloweenBoxes(winner *enterworld.Character, drops []grounditem.Item, nowMs int64) {
	if rt == nil || winner == nil || winner.ID <= 0 {
		return
	}
	now := time.UnixMilli(nowMs).UTC()
	if !rt.Halloween.ActiveAt(now) {
		return
	}
	var boxes int64
	for _, drop := range drops {
		if drop.Codename == progression.HalloweenBoxCodename {
			boxes++
		}
	}
	if boxes == 0 {
		return
	}
	if key := progression.HalloweenDayKey(now); winner.HalloweenBoxDay != key {
		winner.HalloweenBoxDay = key
		winner.HalloweenBoxCount = 0
	}
	winner.HalloweenBoxCount += boxes
}

/*
================
Runtime.RemoveExpiredHalloweenBoxes

The entry cleanup the enterworld door calls once a configured window
has ended: every remaining box row leaves every container the character
record carries, the bag, the avatar rows, the active companion, each
mercenary, and the bags that ride inside summon rows (a dismissed pet's
bag). True only when a row actually left, so the door writes nothing for
anyone else.
================
*/
func (rt *Runtime) RemoveExpiredHalloweenBoxes(character *enterworld.Character) bool {
	if character == nil || !rt.Halloween.CleanupDueAt(rt.Now()) {
		return false
	}
	removed := removeHalloweenBoxRows(&character.MissionInventory)
	if character.AvatarInventory != nil && removeHalloweenBoxRows(&character.AvatarInventory.Rows) {
		removed = true
	}
	if removeHalloweenBoxesFromCOS(character.ActiveCOS) {
		removed = true
	}
	for _, mercenary := range character.Mercenaries {
		if removeHalloweenBoxesFromCOS(mercenary) {
			removed = true
		}
	}
	return removed
}

/*
================
removeHalloweenBoxesFromCOS

Sweeps one companion's bag. A nil companion or bagless one has nothing
to give up.
================
*/
func removeHalloweenBoxesFromCOS(pet *enterworld.CharacterCOS) bool {
	if pet == nil || pet.Container == nil {
		return false
	}
	return removeHalloweenBoxRows(&pet.Container.Rows)
}

/*
================
removeHalloweenBoxRows

Filters box rows out of one row list in place and follows each kept
summon row into the bag it carries.
================
*/
func removeHalloweenBoxRows(rows *[]enterworld.InventoryRow) bool {
	source := *rows
	kept := source[:0]
	removed := false
	for _, row := range source {
		if row.Codename == progression.HalloweenBoxCodename {
			removed = true
			continue
		}
		if removeHalloweenBoxesFromCOS(row.Summon) {
			removed = true
		}
		kept = append(kept, row)
	}
	// Rows past the kept prefix still alias the old tail; clear them so
	// no stale box row survives in the backing array.
	for i := len(kept); i < len(source); i++ {
		source[i] = enterworld.InventoryRow{}
	}
	*rows = kept
	return removed
}
