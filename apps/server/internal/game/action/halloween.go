/*
===========================================================================

halloween.go - the Halloween Day event: box drops, So-ok's exchange, cleanup

Port-only, not native (progression/halloween.go owns the switch, window
and gift table). While the window runs, ordinary monsters drop jewelry
boxes and So-ok (NPC_CH_EVENT_KISAENG1) offers extra talk rows: one
exchanges a box for a weighted random gift, and the flat-price rows sell
exact prizes — the four Elixirs to anyone, one row per helmet of
the character's sex. The exchange's shape follows the sibling event quests: refusals answer
first and remove nothing, the roll picks the first gift whose running
sum is not below it, and the box removal and gift grant commit as one
step (the older sibling grants before it removes; that order is not
copied). Boxes mint under a daily cap that counts per account (#2547),
every source counting toward it; a Gourd kill always pays its box past
the level gap, still under the cap, and pays one to every character who
damaged the Gourd, each under their own account's cap. The character
whose mint reaches the cap is told so once that day. After the window,
the first world entry takes the remaining boxes, and the GameWorld's boot
sweep (HalloweenBoxHolders) takes them from characters who never enter again.

===========================================================================
*/
package action

import (
	"fmt"
	"sort"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
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

// HalloweenPrizeServicePrefix routes one flat-price row the same way;
// the prize's codename follows the prefix, so the exact row a player
// picks is the row the sale grants.
const HalloweenPrizeServicePrefix = "QEV_CH_EVENT_KISAENG_071030:prize:"

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
	halloweenGiftsDoneBanner  = "Exchanged your Jewel Boxes: %d gift(s) received."
	halloweenGiftsLeftBanner  = "%d gift(s) received. Your bag is full, %d Jewel Box(es) left."
	halloweenPrizeDoneBanner  = "Exchanged %d Jewel Boxes for a prize."
	halloweenPrizeShortBanner = "That prize costs %d Jewel Boxes. You have %d."
)

/*
================
Runtime.halloweenExchange

So-ok's Halloween rows on the shared exchange (seasonal_exchange.go): a
jewelry box buys a rolled gift, and the flat-price rows sell the helmets
made for the character's sex and the Elixirs to anyone.
================
*/
func (rt *Runtime) halloweenExchange() *seasonalExchange {
	return &seasonalExchange{
		name: "halloween", host: halloweenSoOkCodename, token: progression.HalloweenBoxCodename,
		perGift: progression.HalloweenBoxesPerGift, active: rt.Halloween.ActiveAt,
		gifts: progression.HalloweenGifts, prizes: progression.HalloweenPrizes, byName: progression.HalloweenPrizeByCodename,
		service: HalloweenExchangeService, prefix: HalloweenPrizeServicePrefix,
		talk: exchangeTalk{
			prompt: halloweenTalkPrompt, option: halloweenTalkOption, granted: halloweenTalkGranted,
			bagFull: halloweenTalkBagFull, noToken: halloweenTalkNoBox,
		},
		banners: exchangeBanners{
			giftDone: halloweenGiftsDoneBanner, giftLeft: halloweenGiftsLeftBanner,
			saleDone: halloweenPrizeDoneBanner, short: halloweenPrizeShortBanner,
		},
	}
}

/*
================
Runtime.HalloweenTalkOptions

The rows So-ok offers while the event runs. Appended after the NPC's
quest rows, so Capture the Flag and every quest row keep their places;
the zero posture appends nothing and talk is native.
================
*/
func (rt *Runtime) HalloweenTalkOptions(npcCodename string, character *enterworld.Character) []NpcQuestOption {
	return rt.exchangeTalkOptions(rt.halloweenExchange(), npcCodename, character)
}

/*
================
Runtime.ExchangeHalloweenBox

Every box in the bag buys a rolled gift at once. Port-only, not native:
the sibling event spends one token per click; the owner wants the whole
bag (the event itself is behind its off-by-default switch).
================
*/
func (rt *Runtime) ExchangeHalloweenBox(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
	return rt.exchangeTokens(rt.halloweenExchange(), character, npcCodename)
}

/*
================
Runtime.ExchangeHalloweenPrize
================
*/
func (rt *Runtime) ExchangeHalloweenPrize(character *enterworld.Character, npcCodename, service string) ([]wire.Frame, error) {
	return rt.exchangePrize(rt.halloweenExchange(), character, npcCodename, service)
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
halloweenGourdDayCount

The account's Gourd-box count on now's day, under its own cap
(#2950): a record still holding an older day counts as zero.
================
*/
func halloweenGourdDayCount(account *domain.HalloweenBoxAccount, now time.Time) int64 {
	if account == nil || account.Day != progression.HalloweenDayKey(now) {
		return 0
	}
	return account.GourdCount
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
	if account == nil {
		return grounditem.Item{}, false
	}
	// A Gourd kill draws on the Gourd allowance, an ordinary kill on
	// the ordinary one (#2950): the caps gate separately.
	if defeated.Ref.Codename == halloweenGourdCodename {
		if halloweenGourdDayCount(account, now) >= progression.HalloweenGourdBoxDailyCap {
			return grounditem.Item{}, false
		}
	} else if halloweenBoxDayCount(account, now) >= progression.HalloweenBoxDailyCap {
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

// halloweenCapReach names one owner whose mint reached a daily cap
// with this settlement, and which of the two caps it was (#2950).
type halloweenCapReach struct {
	owner *enterworld.Character
	gourd bool
}

/*
================
Runtime.countHalloweenBoxes

Counts the boxes one committed kill settlement minted, each against
its owner's account (#2547): a minted box carries its owner's JID, and
owners resolves those JIDs to the settlement's characters. A Gourd
kill's boxes land on the Gourd allowance, an ordinary kill's on the
ordinary one (#2950); the settlement is one kill, so gourd names the
source of every box in drops. Under the 04:00-UTC day key. Runs
inside the settlement's door, so the account records move directly
and the door commits them; a GM-spawned kill is conjured, not hunted,
and its caller never counts it. A day that has rolled over restarts
both counts before this kill's boxes land. Returns the owners whose
account reached the cap with this settlement (an account is named
once per cap), for the once-a-day cap message.
================
*/
func (rt *Runtime) countHalloweenBoxes(owners map[uint32]*enterworld.Character, drops []grounditem.Item, nowMs int64, gourd bool) []halloweenCapReach {
	if rt == nil || len(drops) == 0 {
		return nil
	}
	now := time.UnixMilli(nowMs).UTC()
	if !rt.Halloween.ActiveAt(now) {
		return nil
	}
	key := progression.HalloweenDayKey(now)
	cap := int64(progression.HalloweenBoxDailyCap)
	if gourd {
		cap = progression.HalloweenGourdBoxDailyCap
	}
	var reached []halloweenCapReach
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
			account.Day, account.Count, account.GourdCount = key, 0, 0
		}
		count := &account.Count
		if gourd {
			count = &account.GourdCount
		}
		before := *count
		*count++
		if before < cap && *count >= cap && !announced[account] {
			announced[account] = true
			reached = append(reached, halloweenCapReach{owner: owner, gourd: gourd})
		}
	}
	return reached
}

// halloweenBoxCapBanner is the once-a-day word to a character whose
// mint reached the account's daily cap (#2547): the cap stops being
// silent, and the reset time is in the line. The Gourd line names its
// own limit (#2950): a wave hunter capped on Gourd boxes is not told
// the ordinary limit is what stopped the pay.
const halloweenBoxCapBanner = "You have reached today's Jewel Box limit (%d). It resets at 04:00 UTC."
const halloweenGourdBoxCapBanner = "You have reached today's Gourd Jewel Box limit (%d). It resets at 04:00 UTC."

/*
================
halloweenBoxCapFrame

The cap message for the cap that was reached, in the event banner's
own shape (exchangeBanner).
================
*/
func halloweenBoxCapFrame(gourd bool) wire.Frame {
	if gourd {
		return exchangeBanner(fmt.Sprintf(halloweenGourdBoxCapBanner, progression.HalloweenGourdBoxDailyCap))
	}
	return exchangeBanner(fmt.Sprintf(halloweenBoxCapBanner, progression.HalloweenBoxDailyCap))
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
		if account == nil || halloweenGourdDayCount(account, now)+promised[account] >= progression.HalloweenGourdBoxDailyCap {
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
Runtime.RemoveExpiredEventItems

The entry cleanup the enterworld door calls: a finished Halloween
window's boxes and, whenever the Christmas event does not run, its Red
Santa Socks. True only when a row actually left, so the door writes
nothing for anyone else.
================
*/
func (rt *Runtime) RemoveExpiredEventItems(character *enterworld.Character) bool {
	removed := rt.RemoveExpiredHalloweenBoxes(character)
	if character != nil && rt.Christmas.SocksExpireAt(rt.Now()) && removeEventItems(character, progression.ChristmasSockCodename) {
		removed = true
	}
	return removed
}

/*
================
Runtime.RemoveExpiredHalloweenBoxes

Once a configured window has ended, every remaining box leaves the
character (removeEventItems). So-Ok's Christmas Gift trades the same box
(port-only, sook_christmas.go): while either event runs the boxes stay,
and the end of either window retires them once neither runs.
================
*/
func (rt *Runtime) RemoveExpiredHalloweenBoxes(character *enterworld.Character) bool {
	now := rt.Now()
	if character == nil || rt.Halloween.ActiveAt(now) || rt.SoOkChristmas.ActiveAt(now) {
		return false
	}
	if !rt.Halloween.CleanupDueAt(now) && !rt.SoOkChristmas.BoxesExpireAt(now) {
		return false
	}
	return removeEventItems(character, progression.HalloweenBoxCodename)
}

/*
================
Runtime.HalloweenBoxHolders

The boot sweep's picker: the characters of roster that still hold a
Halloween box once the window has ended, so a character who never
enters again does not keep one in the stored record (#3587). It only
reads; the caller removes the boxes through RemoveExpiredHalloweenBoxes
inside the store's multi-record door, the same single owner and filter
the entry cleanup uses. Nil while the window has not ended, so a sweep
before the close, or with the event off, picks nobody.
================
*/
func (rt *Runtime) HalloweenBoxHolders(roster []*enterworld.Character) []*enterworld.Character {
	if !rt.Halloween.CleanupDueAt(rt.Now()) {
		return nil
	}
	var holders []*enterworld.Character
	for _, character := range roster {
		if character != nil && holdsEventItem(character, progression.HalloweenBoxCodename) {
			holders = append(holders, character)
		}
	}
	return holders
}

/*
================
holdsEventItem

Whether any row of codename sits in a container removeEventItems
sweeps. The two walk the same containers: a container added to one
belongs in the other.
================
*/
func holdsEventItem(character *enterworld.Character, codename string) bool {
	if rowsHoldEventItem(character.MissionInventory, codename) {
		return true
	}
	if character.AvatarInventory != nil && rowsHoldEventItem(character.AvatarInventory.Rows, codename) {
		return true
	}
	if cosHoldsEventItem(character.ActiveCOS, codename) {
		return true
	}
	for _, mercenary := range character.Mercenaries {
		if cosHoldsEventItem(mercenary, codename) {
			return true
		}
	}
	return false
}

/*
================
cosHoldsEventItem
================
*/
func cosHoldsEventItem(pet *enterworld.CharacterCOS, codename string) bool {
	return pet != nil && pet.Container != nil && rowsHoldEventItem(pet.Container.Rows, codename)
}

/*
================
rowsHoldEventItem

Follows each summon row into the bag it carries, as removeEventRows does.
================
*/
func rowsHoldEventItem(rows []enterworld.InventoryRow, codename string) bool {
	for _, row := range rows {
		if row.Codename == codename || cosHoldsEventItem(row.Summon, codename) {
			return true
		}
	}
	return false
}

/*
================
removeEventItems

Every row of codename leaves every container the character record
carries: the bag, the avatar rows, the active companion, each mercenary,
and the bags that ride inside summon rows (a dismissed pet's bag). The
warehouses need no sweep: an event token's row is not storable
(canBorrow 0, storableItem). True only when a row actually left.
================
*/
func removeEventItems(character *enterworld.Character, codename string) bool {
	removed := removeEventRows(&character.MissionInventory, codename)
	if character.AvatarInventory != nil && removeEventRows(&character.AvatarInventory.Rows, codename) {
		removed = true
	}
	if removeEventRowsFromCOS(character.ActiveCOS, codename) {
		removed = true
	}
	for _, mercenary := range character.Mercenaries {
		if removeEventRowsFromCOS(mercenary, codename) {
			removed = true
		}
	}
	return removed
}

/*
================
removeEventRowsFromCOS

Sweeps one companion's bag. A nil companion or bagless one has nothing
to give up.
================
*/
func removeEventRowsFromCOS(pet *enterworld.CharacterCOS, codename string) bool {
	if pet == nil || pet.Container == nil {
		return false
	}
	return removeEventRows(&pet.Container.Rows, codename)
}

/*
================
removeEventRows

Filters codename's rows out of one row list in place and follows each
kept summon row into the bag it carries.
================
*/
func removeEventRows(rows *[]enterworld.InventoryRow, codename string) bool {
	source := *rows
	kept := source[:0]
	removed := false
	for _, row := range source {
		if row.Codename == codename {
			removed = true
			continue
		}
		if removeEventRowsFromCOS(row.Summon, codename) {
			removed = true
		}
		kept = append(kept, row)
	}
	// Rows past the kept prefix still alias the old tail; clear them so
	// no stale row survives in the backing array.
	for i := len(kept); i < len(source); i++ {
		source[i] = enterworld.InventoryRow{}
	}
	*rows = kept
	return removed
}
