/*
===========================================================================

gacha.go - Package action.

===========================================================================
*/

package action

import (
	"crypto/rand"
	"fmt"
	"math/big"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const gachaRollDenominator uint32 = 10000

/*
================
ConfigureGacha

ConfigureGacha loads the authoritative v1.150 Gacha tables and card refs.
Construction fails rather than registering a ticket-consuming opcode over
incomplete data.
================
*/
func (rt *Runtime) ConfigureGacha(textdataDir string) error {
	catalog, err := gacha.LoadCatalog(textdataDir, rt.deps.ItemReferences())
	if err != nil {
		return err
	}
	rt.GachaCatalog = catalog
	return nil
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
	// Native 9DD338 returns 15 bits; 4C8010..4C8015 takes %10000.
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
			return
		}
		sendFrames(session, frames)
	})
}

/*
================
HandleGachaNpcAction

HandleGachaNpcAction accepts only the exact action-0x27 native composition:
0x7338 [boundGid][0x10000]. It must name the actor's currently selected,
live roster NPC and that NPC must occur in gachanpcmap.txt.
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
		return nil, "Gacha catalogue is unavailable"
	}
	gid, actionFlags, err := gacha.DecodeNpcAction(payload)
	if err != nil {
		return nil, err.Error()
	}
	if actionFlags != gacha.InteractionFlagGacha {
		return nil, fmt.Sprintf("unsupported action flags 0x%X", actionFlags)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		return nil, "bound NPC is not the current selected object"
	}
	npc, ok := rt.gachaNpcForGID(divisionID, character, gid)
	if !ok {
		return nil, fmt.Sprintf("gid %d is not a live Gacha roster NPC", gid)
	}
	if !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return nil, fmt.Sprintf("NPC RefObjID %d has no v1.150 Gacha set", npc.RefObjID)
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

HandleGachaRoll applies one atomic Magic Pop transaction. The exact
v1.150 request binds all three authorities: selected machine gid, selected
gachaitemset entry id and ticket inventory slot. On commit the ticket row
becomes a native win/loss result card; 0x3645 reaches the client before
B053 so CIFGhaCha_ApplyResult observes the new ref and reward map.
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
		return nil, "Gacha catalogue or random source is unavailable"
	}
	request, err := gacha.DecodeRollRequest(payload)
	if err != nil {
		return nil, err.Error()
	}
	if request.InventorySlot < inventory.EquipmentSlotEnd ||
		request.InventorySlot >= inventory.BagSlotEnd {
		return nil, fmt.Sprintf("inventory slot %d is outside the bag", request.InventorySlot)
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok ||
		selected != request.BoundNpcGID {
		return nil, "bound NPC is not the current selected object"
	}
	npc, ok := rt.gachaNpcForGID(divisionID, character, request.BoundNpcGID)
	if !ok {
		return nil, fmt.Sprintf("gid %d is not a live Gacha roster NPC", request.BoundNpcGID)
	}
	prize, ok := rt.GachaCatalog.PrizeForNpc(npc.RefObjID, request.SelectedEntryID)
	if !ok {
		return nil, fmt.Sprintf(
			"entry %d is not mapped to NPC RefObjID %d",
			request.SelectedEntryID,
			npc.RefObjID,
		)
	}

	failure := "ticket row changed before commit"
	var frames []wire.Frame
	committed := rt.deps.Update(character, "gacha-roll", func() bool {
		if character.DeletePending {
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

		result := rt.GachaCatalog.LoseCard
		code := gacha.ResultLose
		if win {
			result = rt.GachaCatalog.WinCard
			code = gacha.ResultWin
		}
		// 4EDE1B..4EDE77 writes both parameters for either result card.
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
		return nil, failure
	}
	return frames, ""
}

/*
================
gachaNpcForGID
================
*/
func (rt *Runtime) gachaNpcForGID(
	divisionID string,
	character *enterworld.Character,
	gid uint32,
) (simulation.NpcDef, bool) {
	// Use the same live world/region admission as the generic NPC owner.
	// A selected GID can survive travel; roster presence alone is insufficient.
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	if !ok || !rt.GachaCatalog.HasNpc(npc.RefObjID) {
		return simulation.NpcDef{}, false
	}
	return npc, true
}
