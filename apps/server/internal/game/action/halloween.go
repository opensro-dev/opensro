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
copied). Boxes mint under a daily cap that counts per account (#2547),
every source counting toward it; a Gourd kill always pays its box past
the level gap, still under the cap, and pays one to every character who
damaged the Gourd, each under their own account's cap. The character
whose mint reaches the cap is told so once that day. After the window,
the first world entry takes the remaining boxes.

===========================================================================
*/
package action

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/social/chat"
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

	// The top banners. Port-only, not native; no shipped text carries them.
	halloweenGiftsDoneBanner   = "Exchanged your Jewel Boxes: %d gift(s) received."
	halloweenGiftsLeftBanner   = "%d gift(s) received. Your bag is full, %d Jewel Box(es) left."
	halloweenHelmetDoneBanner  = "Exchanged %d Jewel Boxes for a helmet."
	halloweenHelmetShortBanner = "A helmet costs %d Jewel Boxes. You have %d."
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

Every box in the bag buys a rolled gift at once. Port-only, not native:
the sibling event spends one token per click; the owner wants the whole
bag (the event itself is behind its off-by-default switch). No box
answers the no-box line; a first gift that cannot fit the bag as it
stands answers the bag-full line; later gifts stop at the first that
does not fit and leave their boxes. Removal and grants land together and
any refusal leaves every row untouched. The top banner says what moved.
================
*/
func (rt *Runtime) ExchangeHalloweenBox(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	if npcCodename != halloweenSoOkCodename || !rt.Halloween.ActiveAt(rt.Now()) {
		return nil, fmt.Errorf("halloween exchange is not offered at %s", npcCodename)
	}
	answer := ""
	committed := false
	gifts := uint32(0)
	boxes := uint32(0)
	var frames []wire.Frame
	var rollErr error
	rt.deps.Update(character, "halloween-exchange", func() bool {
		boxes = halloweenBagBoxes(character)
		if boxes < progression.HalloweenBoxesPerGift {
			answer = halloweenTalkNoBox
			return false
		}
		var rows []enterworld.InventoryRow
		answer, rows, frames, gifts, rollErr = rt.planHalloweenGifts(character, boxes/progression.HalloweenBoxesPerGift)
		if rollErr != nil || gifts == 0 {
			return false
		}
		character.MissionInventory = rows
		committed = true
		return true
	})
	if rollErr != nil || (!committed && (answer == "" || answer == halloweenTalkGranted)) {
		return nil, fmt.Errorf("halloween exchange could not commit")
	}
	if committed {
		frames = append(frames, halloweenBanner(halloweenGiftsBanner(gifts, boxes-gifts*progression.HalloweenBoxesPerGift)))
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(answer)}), nil
}

/*
================
Runtime.planHalloweenGifts

Rolls up to wanted gifts and plans them as one change, run inside the
caller's door. The first gift keeps the sibling's test against the bag as
it stands; each later one is planned with its boxes removed. It returns
the answer line, the planned rows and frames of the last gift that fit,
and how many fit.
================
*/
func (rt *Runtime) planHalloweenGifts(character *enterworld.Character, wanted uint32) (string, []enterworld.InventoryRow, []wire.Frame, uint32, error) {
	var rows []enterworld.InventoryRow
	var frames []wire.Frame
	var grant []inventory.ItemAmount
	gifts := uint32(0)
	for gifts < wanted {
		gift, rollErr := rt.rollHalloweenGift()
		if rollErr != nil {
			return "", nil, nil, 0, rollErr
		}
		next := addHalloweenGrant(grant, gift.Codename, gift.Count)
		if gifts == 0 {
			if _, _, planErr := rt.PlanQuestInventory(character, nil, next); planErr != nil {
				break
			}
		}
		consume := []inventory.ItemAmount{{Codename: progression.HalloweenBoxCodename, Count: (gifts + 1) * progression.HalloweenBoxesPerGift}}
		planned, plannedFrames, planErr := rt.PlanQuestInventory(character, consume, next)
		if planErr != nil {
			break
		}
		rows, frames, grant = planned, plannedFrames, next
		gifts++
	}
	if gifts == 0 {
		return halloweenTalkBagFull, nil, nil, 0, nil
	}
	return halloweenTalkGranted, rows, frames, gifts, nil
}

/*
================
addHalloweenGrant

One more gift on a copy of the grant list, same codenames merged so the
plan fills one stack rather than opening a row per gift.
================
*/
func addHalloweenGrant(grant []inventory.ItemAmount, codename string, count uint32) []inventory.ItemAmount {
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
Runtime.ExchangeHalloweenHelmet

One helmet sale: the flat box price buys the exact helmet the picked
row names. Too few boxes answer the no-box line and the banner names the
price and what the bag holds, which no native line carries; a helmet that
cannot fit the bag as it stands answers the bag-full line; only then do
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
	banner := ""
	var frames []wire.Frame
	rt.deps.Update(character, "halloween-helmet", func() bool {
		female := enterworld.ResolveCharacterGenderIndex(character) == enterworld.GenderFemale
		if helmet.Female != female {
			answer = halloweenTalkNoBox
			return false
		}
		if held := halloweenBagBoxes(character); held < progression.HalloweenHelmetPrice {
			answer = halloweenTalkNoBox
			banner = fmt.Sprintf(halloweenHelmetShortBanner, progression.HalloweenHelmetPrice, held)
			return false
		}
		grant := []inventory.ItemAmount{{Codename: helmet.Codename, Count: 1}}
		answer, frames, committed = rt.commitHalloweenExchange(character, progression.HalloweenHelmetPrice, grant)
		return committed
	})
	if !committed && (answer == "" || answer == halloweenTalkGranted) {
		return nil, fmt.Errorf("halloween helmet sale could not commit")
	}
	if committed {
		banner = fmt.Sprintf(halloweenHelmetDoneBanner, progression.HalloweenHelmetPrice)
	}
	if banner != "" {
		frames = append(frames, halloweenBanner(banner))
	}
	return append(frames, wire.Frame{Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogAnswer(answer)}), nil
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
halloweenBanner

The top banner for one exchange. Port-only, not native: the sibling
event's lines stay in the talk pane; the owner wants the result on screen.
================
*/
func halloweenBanner(text string) wire.Frame {
	return wire.Frame{Opcode: chat.OpChatBroadcast, Payload: chat.EncodeChatNotice(text)}
}

/*
================
halloweenGiftsBanner
================
*/
func halloweenGiftsBanner(gifts, boxesLeft uint32) string {
	if boxesLeft >= progression.HalloweenBoxesPerGift {
		return fmt.Sprintf(halloweenGiftsLeftBanner, gifts, boxesLeft)
	}
	return fmt.Sprintf(halloweenGiftsDoneBanner, gifts)
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
Runtime.halloweenBoxAccount

The account record a character mints against (#2547): every character
of the account shares the one daily count, so an alt adds no boxes.
Only inside a settlement door; a runtime without the account store
wired mints nothing (the event is inert without its deps, as a nil
DropRoll never rolls).
================
*/
func (rt *Runtime) halloweenBoxAccount(character *enterworld.Character) *domain.HalloweenBoxAccount {
	if rt == nil || rt.deps == nil || character == nil {
		return nil
	}
	return rt.deps.HalloweenAccount(character.AccountID)
}

/*
================
halloweenBoxDayCount

The account's count on now's 04:00-UTC day: a record still holding an
older day counts as zero (the day restarts when the next box lands, in
countHalloweenBoxes).
================
*/
func halloweenBoxDayCount(account *domain.HalloweenBoxAccount, now time.Time) int64 {
	if account == nil || account.Day != progression.HalloweenDayKey(now) {
		return 0
	}
	return account.Count
}

/*
================
Runtime.halloweenBoxDrop

The kill's event drop: one box while the window runs and the winner's
account still has room under the daily cap, rolled on the same 15-bit
draw as every other drop. A Gourd kill skips the draw and pays past the
level gap; the cap still gates it. The caller appends the drop after
admission and the drop cap, like the unique ticket, so neither can
take it. The account record is the gate: kills settle one at a time,
and countHalloweenBoxes lands each minted box on the account before
the next kill plans.
================
*/
func (rt *Runtime) halloweenBoxDrop(winner *enterworld.Character, defeated monster.Instance, at simulation.Spawn, owner string, now time.Time) (grounditem.Item, bool) {
	if !rt.Halloween.ActiveAt(now) || winner == nil {
		return grounditem.Item{}, false
	}
	account := rt.halloweenBoxAccount(winner)
	if account == nil || halloweenBoxDayCount(account, now) >= progression.HalloweenBoxDailyCap {
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

Counts the boxes one committed kill settlement minted, each against
its owner's account (#2547): a minted box carries its owner's JID, and
owners resolves those JIDs to the settlement's characters. From every
source (the rolled drop and the Gourd alike), under the 04:00-UTC day
key. Runs inside the settlement's door, so the account records move
directly and the door commits them; a GM-spawned kill is conjured, not
hunted, and its caller never counts it. A day that has rolled over
restarts the count before this kill's boxes land. Returns the owners
whose account reached the cap with this settlement (an account is
named once), for the once-a-day cap message.
================
*/
func (rt *Runtime) countHalloweenBoxes(owners map[uint32]*enterworld.Character, drops []grounditem.Item, nowMs int64) []*enterworld.Character {
	if rt == nil || len(drops) == 0 {
		return nil
	}
	now := time.UnixMilli(nowMs).UTC()
	if !rt.Halloween.ActiveAt(now) {
		return nil
	}
	key := progression.HalloweenDayKey(now)
	var reached []*enterworld.Character
	announced := map[*domain.HalloweenBoxAccount]bool{}
	for _, drop := range drops {
		if drop.Codename != progression.HalloweenBoxCodename {
			continue
		}
		owner := owners[drop.OwnerJID]
		account := rt.halloweenBoxAccount(owner)
		if account == nil {
			continue
		}
		if account.Day != key {
			account.Day, account.Count = key, 0
		}
		before := account.Count
		account.Count++
		if before < progression.HalloweenBoxDailyCap &&
			account.Count >= progression.HalloweenBoxDailyCap && !announced[account] {
			announced[account] = true
			reached = append(reached, owner)
		}
	}
	return reached
}

// halloweenBoxCapBanner is the once-a-day word to a character whose
// mint reached the account's daily cap (#2547): the cap stops being
// silent, and the reset time is in the line.
const halloweenBoxCapBanner = "You have reached today's Jewel Box limit (%d). It resets at 04:00 UTC."

/*
================
halloweenBoxCapFrame

The cap message, in the event banner's own shape (halloweenBanner).
================
*/
func halloweenBoxCapFrame() wire.Frame {
	return halloweenBanner(fmt.Sprintf(halloweenBoxCapBanner, progression.HalloweenBoxDailyCap))
}

/*
================
Runtime.halloweenGourdBoxes

Port-only, not native (#2547): a Gourd's box goes to every character
who damaged it, not only the killer - one area attack used to clear a
field's Gourds and pay every box to the killing blow. One assigned box
per damager still under their account's daily cap, prepared exactly
like the winner's own box so the caller can append them to the plan
after admission and the drop cap, where neither can take them. The
winner is skipped: its box came from halloweenBoxDrop. Boxes the plan
already carries count against their account here, so two characters
of one account cannot both be promised the account's last box in one
kill. Contributions are visited in CreditGID order (the reward
groups' own order), which makes that race deterministic. A GM-spawned
Gourd pays nobody: a conjured kill is not hunted, the same rule the
winner's counting follows.
================
*/
func (rt *Runtime) halloweenGourdBoxes(winner *enterworld.Character, defeated monster.Instance, planned []grounditem.Item, contributions []simulation.MonsterContribution, actors map[uint32]rewardActor, at simulation.Spawn, now time.Time) []grounditem.Item {
	if !rt.Halloween.ActiveAt(now) || defeated.GMSpawned || defeated.Ref.Codename != halloweenGourdCodename {
		return nil
	}
	promised := map[*domain.HalloweenBoxAccount]int64{}
	for _, plannedDrop := range planned {
		if plannedDrop.Codename != progression.HalloweenBoxCodename {
			continue
		}
		if account := rt.halloweenBoxAccount(ownersCharacter(actors, plannedDrop.OwnerJID)); account != nil {
			promised[account]++
		}
	}
	rows := append([]simulation.MonsterContribution(nil), contributions...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreditGID < rows[j].CreditGID })
	winnerGID := enterworld.ObjectIDForCharacter(winner)
	var boxes []grounditem.Item
	for _, row := range rows {
		if row.Damage == 0 || row.CreditGID == winnerGID {
			continue
		}
		a, ok := actors[row.CreditGID]
		if !ok || a.character == nil || a.character.DeletePending {
			continue
		}
		account := rt.halloweenBoxAccount(a.character)
		if account == nil || halloweenBoxDayCount(account, now)+promised[account] >= progression.HalloweenBoxDailyCap {
			continue
		}
		box, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: progression.HalloweenBoxCodename, Count: 1, Assigned: true}, at, a.character.Name, now)
		if !ok {
			continue
		}
		box.OwnerJID = enterworld.ObjectIDForCharacter(a.character)
		promised[account]++
		boxes = append(boxes, rt.scatterMonsterDrop(box, at, defeated.Rarity()))
	}
	return boxes
}

/*
================
ownersCharacter

The roster character behind one ground item's owner JID, or nil.
================
*/
func ownersCharacter(actors map[uint32]rewardActor, gid uint32) *enterworld.Character {
	if a, ok := actors[gid]; ok {
		return a.character
	}
	return nil
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
