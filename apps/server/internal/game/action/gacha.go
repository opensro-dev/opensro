/*
===========================================================================

gacha.go - Magic Pop machine interaction, ticket rolls, and Gori prize exchange

Owns 0x7338 machine opening, 0x7053 ticket roll to win/loss vouchers, and
0x71BE voucher redemption through Gori (NPC_CH_GACHA_OPERATOR), including the
native equipment property roll (+1..+5 OptLevel, 5-bit white variance
+ durability, and blue magic options) and, on the v1.696 tables, the row's
[BIIV] property override (item/gacha/spec.go).

===========================================================================
*/

package action

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const gachaRollDenominator uint32 = 10000

// EnvGachaPoolV2 turns the port-only Magic Pop prize pool on (owner,
// 2026-10-20; not native). Unset is on; "off" restores the retail
// gachaitemset.txt pool byte-for-byte.
const EnvGachaPoolV2 = "SRO_GACHA_POOL_V2"

/*
================
GachaPoolV2FromEnv
================
*/
func GachaPoolV2FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGachaPoolV2))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// EnvMagicPopV1696 selects the v1.696 Magic POP tables (owner,
// 2026-11-27: the later client's Magic POP is the default). Unset is on;
// "off" keeps the v1.150 machine, with pool v2 under its own flag.
const EnvMagicPopV1696 = "SRO_MAGIC_POP_V1696"

/*
================
MagicPopV1696FromEnv
================
*/
func MagicPopV1696FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMagicPopV1696))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// Native Magic Pop refusal codes, answered [2][u8 code] on the request's
// reply opcode (0xB053 for the roll, 0xB1BE for the exchange). The reference
// server's dispatcher looks the gid up first (no such object: 0; wrong plane
// or non-adjacent sector: 3; beyond the class hit range: 4); its handlers
// then refuse in this order: no gacha service or set 0x1C0F, slot at or
// beyond the bag limit 0x1806, empty slot 0x1809, item not a ticket 0x1806,
// no win entry 2, mutation failure 5. A gid naming an object of another kind
// also reads as no such object here: the roster holds NPCs only. The native
// expired-ticket refusal 0x183D has no port trigger: bag rows carry no
// period state.
const (
	gachaRefusalNoObject   uint16 = 0
	gachaRefusalNoEntry    uint16 = 2
	gachaRefusalOutOfScope uint16 = 3
	gachaRefusalTooFar     uint16 = 4
	gachaRefusalFailed     uint16 = 5
	gachaRefusalBadSlot    uint16 = 0x1806
	gachaRefusalEmptySlot  uint16 = 0x1809
	gachaRefusalNoService  uint16 = 0x1C0F
)

/*
================
gachaRefusal

The one frame a refused roll or exchange answers with. A refusal that sent
nothing left the client in its waiting phase for good: it leaves that phase
only on the reply opcode.
================
*/
func gachaRefusal(opcode uint16, code uint16) []wire.Frame {
	return []wire.Frame{{Opcode: opcode, Payload: gacha.EncodeRefusal(code)}}
}

/*
================
gachaNpcAdmission

The native admission shared by the roll and the exchange: the gid names a
live object (else code 0), in the viewer's plane and adjacent sectors (3),
inside the NPC class hit range (4). The caller then tests its own service
mark and answers 0x1C0F. There is no selected-object test: closing the NPC
talk window releases the selection (0x74B3) while the Magic Pang window
stays open, and the reference server still answers the roll. The reason is
empty only when the NPC is admitted; the code is meaningful only beside a
reason (an admitted NPC and a missing object both carry 0).
================
*/
func (rt *Runtime) gachaNpcAdmission(
	divisionID string,
	character *enterworld.Character,
	gid uint32,
) (simulation.NpcDef, uint16, string) {
	if !rt.NpcSpawn.Enabled {
		return simulation.NpcDef{}, gachaRefusalNoObject, "the NPC roster is not live"
	}
	npc, ok := rt.npcForGID(gid)
	if !ok {
		return simulation.NpcDef{}, gachaRefusalNoObject, fmt.Sprintf("gid %d names no roster NPC", gid)
	}
	if !rt.npcVisibleTo(divisionID, character, npc) {
		return simulation.NpcDef{}, gachaRefusalOutOfScope, fmt.Sprintf("gid %d is outside the viewer's sector scope", gid)
	}
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return simulation.NpcDef{}, gachaRefusalTooFar, fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	return npc, 0, ""
}

// gachaOperatorCodename is the exchange operator Gori (RefObj 9252).
const gachaOperatorCodename = "NPC_CH_GACHA_OPERATOR"

/*
================
ConfigureGacha

ConfigureGacha loads the authoritative v1.150 Gacha tables and card refs.
Construction fails rather than registering a ticket-consuming opcode over
incomplete data.
================
*/
func (rt *Runtime) ConfigureGacha(textdataDir string) error {
	levelCap, err := progression.LevelCapFromEnv()
	if err != nil {
		return err
	}
	rt.GachaLevelCap = levelCap
	if MagicPopV1696FromEnv() {
		if gacha.HasV1696Tables(textdataDir) {
			catalog, err := gacha.LoadCatalogV1696(textdataDir, rt.deps.ItemReferences())
			if err != nil {
				return err
			}
			if err := rt.validateGachaSpecs(catalog); err != nil {
				return err
			}
			// The v1.696 table retires pool v2, whatever its flag says.
			rt.GachaPoolV2 = false
			rt.GachaCatalog = catalog
			log.Infof("gacha: v1.696 Magic POP tables active (%s), %d prizes; pool v2 retired",
				EnvMagicPopV1696, len(catalog.Prizes()))
			return nil
		}
		log.Warnf("gacha: %s is on but %s is absent; serving the v1.150 machine", EnvMagicPopV1696, gacha.PrizeTableV1696)
	}
	catalog, err := gacha.LoadCatalog(textdataDir, rt.deps.ItemReferences())
	if err != nil {
		return err
	}
	rt.GachaPoolV2 = GachaPoolV2FromEnv()
	if rt.GachaPoolV2 {
		if err := catalog.ApplyPoolV2(rt.deps.ItemReferences()); err != nil {
			return err
		}
		log.Infof("gacha: port-only prize pool v2 active (%s); the shipped gachaitemset.txt is untouched", EnvGachaPoolV2)
	}
	rt.GachaCatalog = catalog
	return nil
}

/*
================
validateGachaSpecs

The v1.188 row loader's M check: the option exists at the token's degree
and its value lies in the option's range (see
research/findings/magic-pop-biiv-server-v1188.md). It needs the
magic option table, so it runs once the alchemy catalogue is in; a
runtime without one (unit fixtures) defers the check to the exchange,
which refuses an unresolved option.
================
*/
func (rt *Runtime) validateGachaSpecs(catalog *gacha.Catalog) error {
	if rt.Alchemy == nil {
		return nil
	}
	for _, prize := range catalog.Prizes() {
		for _, token := range prize.Spec.MagicOptions() {
			if _, err := rt.gachaSpecOption(token); err != nil {
				return fmt.Errorf("gacha: entry %d: %w", prize.EntryID, err)
			}
		}
	}
	return nil
}

/*
================
gachaSpecOption

One M token as an item magic option word: the option row's id in the low
half, the value in the high half (the same store the exchange writes for
a rolled option).
================
*/
func (rt *Runtime) gachaSpecOption(token gacha.SpecToken) (uint64, error) {
	if rt.Alchemy == nil {
		return 0, fmt.Errorf("magic option table is unavailable for %s", token.Name)
	}
	option, ok := rt.Alchemy.OptionByTag(token.Tag, int(token.Level))
	if !ok {
		return 0, fmt.Errorf("magic option %s has no degree %d row", token.Name, token.Level)
	}
	low, high, ok := alchemy.MagicValueBounds(option)
	if !ok || token.Value < int64(low) || token.Value > int64(high) {
		return 0, fmt.Errorf("magic option %s value %d is outside %d..%d", token.Name, token.Value, low, high)
	}
	return uint64(token.Value)<<32 | uint64(option.ID), nil
}

/*
================
GachaPoolReference

The published v1.696 pool (enterworld.GachaPoolReference); nil on the
v1.150 machine. Each M token's option line carries the magicoption row id
the exchange writes (gachaSpecOption), so the browser's tooltip reads the
same id the server pays.
================
*/
func (rt *Runtime) GachaPoolReference() *enterworld.GachaPoolReference {
	return rt.GachaCatalog.PublicPool(rt.GachaLevelCap, rt.gachaOptionID)
}

/*
================
gachaOptionID

The magicoption row id of one M token: the row its tag and degree resolve
to (alchemy's OptionByTag, the resolver the reference server uses), which
is the id the exchange stores in the option word's low half. A runtime
with no magic option table resolves none.
================
*/
func (rt *Runtime) gachaOptionID(token gacha.SpecToken) (uint16, bool) {
	if rt == nil || rt.Alchemy == nil {
		return 0, false
	}
	option, ok := rt.Alchemy.OptionByTag(token.Tag, int(token.Level))
	if !ok {
		return 0, false
	}
	return option.ID, true
}

/*
================
GachaRuleState

What ConfigureGacha settled, for the public rules page: v1696 is the table
generation actually loaded (false when the flag is on but the tables are
absent and the v1.150 machine is served), and poolV2 is the port-only pool
actually in force.
================
*/
func (rt *Runtime) GachaRuleState() (v1696, poolV2 bool) {
	if rt == nil {
		return false, false
	}
	return rt.GachaCatalog != nil && rt.GachaCatalog.V1696, rt.GachaPoolV2
}

/*
================
secureGachaRoll
================
*/
func secureGachaRoll() (uint32, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1<<15))
	if err != nil {
		return 0, fmt.Errorf("gacha: random source: %w", err)
	}
	// Native rand() returns 15 bits; the reference server takes %10000.
	// Keep its distribution while retaining the server-owned secure source.
	return uint32(value.Uint64()) % gachaRollDenominator, nil
}

/*
================
registerGacha
================
*/
func (rt *Runtime) registerGacha(hub *transport.Hub) {
	hub.Handle(gacha.OpRoll, func(
		session *transport.Session,
		opcode uint16,
		payload []byte,
	) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("gacha: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		frames, refusal := rt.HandleGachaRoll(divisionID, character, payload)
		if refusal != "" {
			log.Debugf("gacha: 0x7053 refused for %s: %s", character.Name, refusal)
		}
		// A refusal carries its one [2][code] frame: dropping it would strand
		// the client in its waiting phase until relog.
		sendFrames(session, frames)
	})
	hub.Handle(gacha.OpExchange, func(
		session *transport.Session,
		opcode uint16,
		payload []byte,
	) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("gacha: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		frames, refusal := rt.HandleGachaExchange(divisionID, character, payload)
		if refusal != "" {
			log.Debugf("gacha: 0x71BE refused for %s: %s", character.Name, refusal)
		}
		sendFrames(session, frames)
	})
}

/*
================
HandleGachaNpcAction

HandleGachaNpcAction accepts only the exact action-0x27 native composition:
0x7338 [boundGid][0x10000]. It must name the actor's currently selected,
live roster NPC and that NPC must occur in gachanpcmap.txt. Every refusal
answers the generic lane's 0xB338 kind-2 frame (npcFunctionRefusal), so
the client's pending function request retires; the string is only logged.
================
*/
func (rt *Runtime) HandleGachaNpcAction(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil {
		return npcFunctionRefusal(wire.ErrCodeInvalidRequest), "Gacha catalogue is unavailable"
	}
	gid, actionFlags, err := gacha.DecodeNpcAction(payload)
	if err != nil {
		return npcFunctionRefusal(wire.ErrCodeInvalidRequest), err.Error()
	}
	if actionFlags != gacha.InteractionFlagGacha {
		return npcFunctionRefusal(wire.ErrCodeInvalidRequest), fmt.Sprintf("unsupported action flags 0x%X", actionFlags)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		return npcFunctionRefusal(selectObjectNotFound), "bound NPC is not the current selected object"
	}
	// The same live world/region admission as the generic NPC owner: a
	// selected gid can survive travel, so roster presence is not enough.
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	if !ok {
		return npcFunctionRefusal(selectObjectNotFound), fmt.Sprintf("gid %d is not a live Gacha roster NPC", gid)
	}
	if !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return npcFunctionRefusal(npcFunctionNotOffered), fmt.Sprintf("NPC RefObjID %d has no v1.150 Gacha set", npc.RefObjID)
	}
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}
	return []wire.Frame{{
		Opcode:  gacha.OpInteractionState,
		Payload: gacha.EncodeOpenInteraction(),
	}}, ""
}

/*
================
HandleGachaRoll

HandleGachaRoll applies one atomic Magic Pop transaction. The request
names the machine gid, the gachaitemset entry and the ticket slot.
Admission is the native one (gachaNpcAdmission, then gacha set, bag limit,
ticket row, entry lookup); every refusal answers one B053 [2][u8 code],
the native answer the client's waiting phase ends on. On commit the ticket
row becomes a native win/loss result card; 0x3645 reaches the client before
B053 so CIFGhaCha_ApplyResult observes the new ref and reward map. A
committed win also publishes the shard-wide 0x30C0 banner (#2425).
================
*/
func (rt *Runtime) HandleGachaRoll(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil || rt.GachaRoll == nil {
		return gachaRefusal(gacha.OpResult, gachaRefusalFailed), "Gacha catalogue or random source is unavailable"
	}
	request, err := gacha.DecodeRollRequest(payload)
	if err != nil {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot), err.Error()
	}
	// The wire shape bound; the authority door checks the character's capacity.
	if request.InventorySlot < inventory.EquipmentSlotEnd ||
		request.InventorySlot >= inventory.MaxBagEnd {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d is outside the bag", request.InventorySlot)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	npc, code, reason := rt.gachaNpcAdmission(divisionID, character, request.BoundNpcGID)
	if reason != "" {
		return gachaRefusal(gacha.OpResult, code), reason
	}
	if !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return gachaRefusal(gacha.OpResult, gachaRefusalNoService),
			fmt.Sprintf("NPC RefObjID %d has no v1.150 Gacha set", npc.RefObjID)
	}
	if !inventory.IsBagSlot(request.InventorySlot, inventory.BagEnd(character)) {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d is beyond the bag limit", request.InventorySlot)
	}
	rowIndex := -1
	for index := range character.MissionInventory {
		if character.MissionInventory[index].Slot == int64(request.InventorySlot) {
			rowIndex = index
			break
		}
	}
	if rowIndex < 0 {
		return gachaRefusal(gacha.OpResult, gachaRefusalEmptySlot),
			fmt.Sprintf("inventory slot %d is empty", request.InventorySlot)
	}
	if row := &character.MissionInventory[rowIndex]; row.RefObjID != rt.GachaCatalog.Card.RefObjID ||
		row.TypeFlags != rt.GachaCatalog.Card.TypeFlags ||
		row.Codename != rt.GachaCatalog.Card.Codename {
		return gachaRefusal(gacha.OpResult, gachaRefusalBadSlot),
			fmt.Sprintf("inventory slot %d holds no Magic Pop ticket", request.InventorySlot)
	}
	prize, ok := rt.GachaCatalog.PrizeForNpc(npc.RefObjID, request.SelectedEntryID)
	if !ok {
		return gachaRefusal(gacha.OpResult, gachaRefusalNoEntry), fmt.Sprintf(
			"entry %d is not mapped to NPC RefObjID %d",
			request.SelectedEntryID,
			npc.RefObjID,
		)
	}
	// INFERENCE: the v1.696 client lists a row only while the level cap
	// reaches its column 16 (the cap byte gate in CIFGhaCha); the server
	// refuses the same rows, so a forged request cannot pick one.
	if int64(prize.MinLevelCap) > rt.GachaLevelCap {
		return gachaRefusal(gacha.OpResult, gachaRefusalNoEntry), fmt.Sprintf(
			"entry %d needs level cap %d, the server's is %d",
			request.SelectedEntryID, prize.MinLevelCap, rt.GachaLevelCap,
		)
	}

	failure := "ticket row changed before commit"
	var frames []wire.Frame
	rolledWin := false
	var rolledReward, rolledQuantity uint32
	committed := rt.deps.Update(character, "gacha-roll", func() bool {
		if character.DeletePending || !inventory.IsBagSlot(request.InventorySlot, inventory.BagEnd(character)) {
			return false
		}
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.InventorySlot) {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		row := &character.MissionInventory[rowIndex]
		if row.RefObjID != rt.GachaCatalog.Card.RefObjID ||
			row.TypeFlags != rt.GachaCatalog.Card.TypeFlags ||
			row.Codename != rt.GachaCatalog.Card.Codename {
			return false
		}

		roll, err := rt.GachaRoll()
		if err != nil {
			failure = err.Error()
			return false
		}
		if roll >= gachaRollDenominator {
			failure = "Gacha random source returned an out-of-range value"
			return false
		}
		win := roll < prize.ChancePer10000
		if !win {
			sample, err := rt.GachaRoll()
			if err != nil {
				failure = err.Error()
				return false
			}
			waste, ok := rt.GachaCatalog.WastePrizeForNpc(npc.RefObjID, sample)
			if !ok {
				failure = "Gacha waste draw is unavailable or out of range"
				return false
			}
			prize = waste
		}

		rolledWin = win
		rolledReward, rolledQuantity = prize.RewardRefObjID, prize.Quantity
		result := rt.GachaCatalog.LoseCard
		code := gacha.ResultLose
		if win {
			result = rt.GachaCatalog.WinCard
			code = gacha.ResultWin
		}
		// Natively both parameters are written for either result card.
		magicOptions := []uint64{uint64(prize.RewardRefObjID), uint64(prize.Quantity)}
		delta := gacha.EncodeResultItemDelta(request.InventorySlot, result.RefObjID, prize.RewardRefObjID, prize.Quantity)
		*row = enterworld.InventoryRow{
			Slot:         int64(request.InventorySlot),
			RefObjID:     result.RefObjID,
			Codename:     result.Codename,
			TypeFlags:    result.TypeFlags,
			VarianceBits: "0",
			StackCount:   1,
			MagicOptions: magicOptions,
		}
		frames = []wire.Frame{
			{Opcode: gacha.OpItemStateDelta, Payload: delta},
			{Opcode: gacha.OpResult, Payload: gacha.EncodeResult(code)},
		}
		return true
	})
	if !committed {
		return gachaRefusal(gacha.OpResult, gachaRefusalFailed), failure
	}
	if rt.GachaPoolV2 {
		outcome := "lose"
		if rolledWin {
			outcome = "win"
		}
		log.Infof("gacha pool v2: %s rolled entry %d at NPC %d: %s, reward %d x%d",
			character.Name, request.SelectedEntryID, npc.RefObjID, outcome, rolledReward, rolledQuantity)
	}
	if rolledWin {
		// The win is announced to the whole shard (#2425): the winner
		// reads the banner at the tail of their own burst, every other
		// session through the shard push, as for a Global Chatting line.
		// A loss announces nothing, and a refused roll never reaches
		// here, so no uncommitted win is ever published.
		announce := wire.Frame{Opcode: gacha.OpWinAnnounce,
			Payload: gacha.EncodeWinAnnounce(rolledReward, uint16(rolledQuantity))}
		frames = append(frames, announce)
		if rt.PushShardPeerFrames != nil {
			rt.PushShardPeerFrames(divisionID, character.Name, []wire.Frame{announce})
		}
		// The Event-feed line naming the winner (#2852), behind its
		// flag; the banner above goes out either way.
		rt.announceGachaWin(divisionID, character.Name, rolledReward, rolledQuantity, rt.Now().UnixMilli())
	}
	return frames, ""
}

/*
================
prepareGachaRewardItem

Natively the voucher mutates to the prize item and the server's special
equipment property roll runs with the row's override spec
(research/findings/magic-pop-biiv-server-v1188.md). v1.150 rows
carry none (all 26 rows have param1..4 == 0), so their equipment rolls a
low-biased +1..+5 enhancement level (minimum of two 1..5 draws), 5-bit
white-stat variance + tier-0 durability, and blue magic options (100%
entry for Rarity == 2 Seal of Star, 31/101 for normal gear). Each token of
a v1.696 [BIIV] spec replaces its part without drawing: O the plus level,
V the variance word (durability from its first field), D the durability
with no variance, M the blue options. Non-equipment returns immediately
without drawing from DropRoll.
================
*/
func (rt *Runtime) prepareGachaRewardItem(reward *enterworld.ItemRef, quantity uint64, spec gacha.Spec) (inventory.Item, error) {
	if wire.IsGoldHeap(reward.TypeFlags()) {
		return inventory.Item{}, fmt.Errorf("ground-only item %s cannot enter an inventory", reward.Codename)
	}
	item := inventory.Item{
		RefObjID:  reward.RefObjID,
		Codename:  reward.Codename,
		TypeFlags: reward.TypeFlags(),
		Quantity:  uint16(quantity),
	}
	if reward.TypeIDs[0] != 3 || reward.TypeIDs[1] != 1 {
		return item, nil
	}
	plus, ok := spec.Plus()
	if !ok {
		plus = 5
		for i := 0; i < 2; i++ {
			value, ok := rt.rollUniqueCount(1, 5)
			if !ok {
				return inventory.Item{}, fmt.Errorf("gacha equipment plus roll failed for %s", reward.Codename)
			}
			plus = min(plus, uint8(value))
		}
	}
	variance, durability, err := rt.gachaRewardVariance(reward, spec)
	if err != nil {
		return inventory.Item{}, err
	}
	magic, err := rt.gachaRewardMagic(reward, spec)
	if err != nil {
		return inventory.Item{}, err
	}
	item.Plus = plus
	item.VarianceBits = variance
	item.Durability = durability
	item.MagicOptions = magic
	return item, nil
}

/*
================
gachaRewardVariance

The reference server's per-class branches (weapons and armour): V sets the
variance word and derives durability from its first 5-bit field; D
alone sets the durability, clamped to the row's range, and leaves the
variance zero; otherwise the native roll. A row whose Dur_U is 0 (an
avatar attach, flags 3.1.13.4, has none) keeps 0: DurabilityFromVariance's
floor of one would invent a durability the row does not have.
================
*/
func (rt *Runtime) gachaRewardVariance(reward *enterworld.ItemRef, spec gacha.Spec) (uint64, uint32, error) {
	variance, durability, err := rt.gachaRewardVarianceRoll(reward, spec)
	if err != nil {
		return 0, 0, err
	}
	if reward.MaxDurability == 0 {
		durability = 0
	}
	return variance, durability, nil
}

/*
================
gachaRewardVarianceRoll

The spec overrides and the native roll of gachaRewardVariance, before the
zero-durability row rule.
================
*/
func (rt *Runtime) gachaRewardVarianceRoll(reward *enterworld.ItemRef, spec gacha.Spec) (uint64, uint32, error) {
	if bits, ok := spec.Variance(); ok {
		return bits, combat.DurabilityFromVariance(reward, uint8(bits&gachaVarianceFieldMask)), nil
	}
	if value, ok := spec.Durability(); ok {
		low := int64(combat.DurabilityFromVariance(reward, 0))
		high := int64(combat.DurabilityFromVariance(reward, gachaVarianceFieldMask))
		return 0, uint32(min(max(value, low), high)), nil
	}
	variance, durability, ok := rt.rollDroppedEquipmentVariance(reward)
	if !ok {
		return 0, 0, fmt.Errorf("gacha equipment variance roll failed for %s", reward.Codename)
	}
	return variance, durability, nil
}

// gachaVarianceFieldMask is one 5-bit white-stat variance field.
const gachaVarianceFieldMask = 0x1f

/*
================
gachaRewardMagic

With M tokens the reference server skips its 31/101 entry gate and writes
exactly the listed options (flag 2). Without them the native random roll
runs from
the client property table; an imported row that table does not know has
no option pool, so it gets none (INFERENCE: the native pool is the
magicoptionassign join the property table was built from, and no later
prize without M tokens is equipment the port can join).
================
*/
func (rt *Runtime) gachaRewardMagic(reward *enterworld.ItemRef, spec gacha.Spec) ([]uint64, error) {
	if tokens := spec.MagicOptions(); len(tokens) > 0 {
		options := make([]uint64, 0, len(tokens))
		for _, token := range tokens {
			option, err := rt.gachaSpecOption(token)
			if err != nil {
				return nil, fmt.Errorf("gacha spec for %s: %w", reward.Codename, err)
			}
			options = append(options, option)
		}
		return options, nil
	}
	if !loot.HasEquipmentProperties(reward.Codename) {
		return nil, nil
	}
	magic, _, err := loot.EquipmentMagic(reward.Codename, rt.DropRoll)
	if err != nil {
		return nil, fmt.Errorf("gacha equipment magic roll failed for %s: %w", reward.Codename, err)
	}
	return magic, nil
}

/*
================
HandleGachaExchange

HandleGachaExchange answers 0x71BE [operatorGid] from the client's talk row 5DB67B
sends once 642270 found a voucher in the bag. Per the reference server's exchange,
every win or loss voucher in the bag is redeemed in one request, walking the
bag in slot order (redeemGachaVoucher pays each). A card the exchange cannot
pay (a stacked row, a pair that is not an enabled prize row, so the exchange
can only hand out what a roll could have written, or a reward that cannot
be made: ground-only, or an option its spec cannot resolve) is passed by,
and the first one passed by names the refusal; a reward that
cannot be granted (a full bag) stops the walk, keeps the vouchers already
redeemed and answers 5. Evidence: ../research/findings/magic-pop-biiv-server-v1188.md
("Gori's exchange takes every result card in one talk").
================
*/
func (rt *Runtime) HandleGachaExchange(
	divisionID string,
	character *enterworld.Character,
	payload []byte,
) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	if rt.GachaCatalog == nil {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalFailed), "Gacha catalogue is unavailable"
	}
	gid, err := gacha.DecodeExchangeRequest(payload)
	if err != nil {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalBadSlot), err.Error()
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	// The native admission (gachaNpcAdmission), then the operator service:
	// natively an NPC without it refuses 0x1C0F and one with no exchangeable
	// voucher refuses 5, each answered [2][u8 code] on 0xB1BE.
	npc, code, reason := rt.gachaNpcAdmission(divisionID, character, gid)
	if reason != "" {
		return gachaRefusal(gacha.OpExchangeResult, code), reason
	}
	if npc.Codename != gachaOperatorCodename {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalNoService),
			fmt.Sprintf("gid %d is not a live Gacha exchange operator", gid)
	}

	failure := "no exchangeable voucher"
	var frames []wire.Frame
	stopped, passedBy := false, false
	committed := rt.deps.Update(character, "gacha-exchange", func() bool {
		if character.DeletePending || rt.deps.ItemReferences() == nil {
			return false
		}
		for _, slot := range rt.gachaVoucherSlots(character) {
			redeemed, skip, err := rt.redeemGachaVoucher(character, slot)
			if err != nil && skip {
				// Not an exchangeable card (stacked, an invalid pair, an
				// unresolved or unpayable reward): the native loop passes
				// the slot by. The first card passed by names the refusal.
				if !passedBy {
					failure, passedBy = err.Error(), true
				}
				continue
			}
			if err != nil {
				// The reward could not be granted (a full bag): the native
				// loop stops here and answers 5, keeping what it paid.
				failure = err.Error()
				stopped = true
				break
			}
			frames = append(frames, redeemed...)
		}
		return len(frames) > 0
	})
	if !committed {
		return gachaRefusal(gacha.OpExchangeResult, gachaRefusalFailed), failure
	}
	if stopped {
		return append(frames, gachaRefusal(gacha.OpExchangeResult, gachaRefusalFailed)...), failure
	}
	return frames, ""
}

/*
================
gachaVoucherSlots

The bag slots holding a win or loss voucher with its recorded pair, in
slot order: the native exchange walks the bag from slot 13 upward.
================
*/
func (rt *Runtime) gachaVoucherSlots(character *enterworld.Character) []uint8 {
	var slots []uint8
	for _, row := range character.MissionInventory {
		// The stored slot is an int64: InBag range-checks it before any
		// narrowing, so a slot of 256 or more never aliases a low bag slot.
		if !inventory.InBag(character, row.Slot) ||
			!rt.GachaCatalog.IsResultCard(row.RefObjID) || len(row.MagicOptions) != 2 {
			continue
		}
		slots = append(slots, uint8(row.Slot))
	}
	slices.Sort(slots)
	return slots
}

/*
================
redeemGachaVoucher

Exchanges the voucher in one bag slot for its recorded pair, reference id
then quantity, with the native roll giving +1..+5 OptLevel, 5-bit white
variance + durability, and blue magic options on equipment rewards. The
character's bag changes only on success. skip reports a card the exchange
passes by (stacked, an invalid pair, an unresolved reward) as opposed to a
grant that failed.

The voucher leaves by a 0x3645 quantity of zero rather than a row delete,
because that is the update client 7654B0 turns into UIIT_MSG_GACHA_EXCHANGE,
and it goes first: the freed slot is the first free one, so the reward
usually lands in it, and the client must still find the voucher there. The
removal is pinned to the slot whose pair was read, never to the codename.
================
*/
func (rt *Runtime) redeemGachaVoucher(character *enterworld.Character, slot uint8) (frames []wire.Frame, skip bool, err error) {
	index := -1
	for i, row := range character.MissionInventory {
		if row.Slot == int64(slot) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, true, fmt.Errorf("voucher at slot %d is gone", slot)
	}
	row := character.MissionInventory[index]
	// The walk listed this slot for a result card with its pair; the row
	// found here must still be that card before its pair is read.
	if !rt.GachaCatalog.IsResultCard(row.RefObjID) || len(row.MagicOptions) != 2 {
		return nil, true, fmt.Errorf("slot %d no longer holds a result card", slot)
	}
	if row.StackCount != 1 {
		return nil, true, fmt.Errorf("voucher row at slot %d is stacked (%d)", row.Slot, row.StackCount)
	}
	rewardRef, quantity := uint32(row.MagicOptions[0]), row.MagicOptions[1]
	if quantity < 1 || quantity > 0xFFFF || uint64(rewardRef) != row.MagicOptions[0] ||
		!rt.GachaCatalog.IsPrizePair(rewardRef, quantity) {
		return nil, true, fmt.Errorf("voucher at slot %d carries an invalid reward pair", slot)
	}
	reward, found := rt.deps.ItemReferences().ItemRefByID(rewardRef)
	if !found || reward == nil {
		return nil, true, fmt.Errorf("voucher reward %d is unresolved", rewardRef)
	}
	before := invItemsFromBag(character)
	inv := inventory.New(before, inventory.BagEnd(character))
	if _, fault := inv.DropQuantity(slot, 1); fault != nil {
		return nil, true, fmt.Errorf("voucher at slot %d: %v", slot, fault)
	}
	rewardItem, err := rt.prepareGachaRewardItem(reward, quantity, rt.GachaCatalog.SpecForPair(rewardRef, quantity))
	if err != nil {
		// A fault of this card's reward (ground-only, an option its spec
		// cannot resolve), not of the bag: pass the card by, or it would
		// block every card after it on every talk.
		return nil, true, err
	}
	rows, planned, err := rt.planInventoryItemsGrant(
		character,
		before,
		inv,
		[]inventory.Item{rewardItem},
		map[uint8]bool{slot: true},
	)
	if err != nil {
		return nil, false, err
	}
	character.MissionInventory = rows
	quantityFrame := wire.Frame{
		Opcode:  gacha.OpItemStateDelta,
		Payload: wire.NewWriter(4).U8(slot).U8(8).U16(0).Payload(),
	}
	return append([]wire.Frame{quantityFrame}, planned...), false, nil
}
