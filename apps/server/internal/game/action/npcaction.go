/*
===========================================================================

npcaction.go - Package action.

===========================================================================
*/

package action

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
registerNpcAction

registerNpcAction owns the shared 0x7338 registration. Feature children
return frames through this dispatcher; none may overwrite the hub handler.
================
*/
func (rt *Runtime) registerNpcAction(hub *transport.Hub) {
	hub.Handle(wire.OpNpcActionRequest, func(session *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, session)
		if !bound {
			log.Debugf("npcaction: 0x%04X from unbound session %d discarded", opcode, session.ID)
			return
		}
		_, mask, err := wire.DecodeNpcActionRequest(payload)
		if err != nil {
			log.Debugf("npcaction: malformed 0x7338 from %s: %v", character.Name, err)
			return
		}
		var frames []wire.Frame
		var refusal string
		if mask == gacha.InteractionFlagGacha {
			if rt.GachaCatalog == nil {
				refusal = "Gacha catalogue is unavailable"
			} else {
				frames, refusal = rt.HandleGachaNpcAction(divisionID, character, payload)
			}
		} else {
			frames, refusal = rt.HandleNpcAction(divisionID, character, payload)
		}
		if refusal != "" {
			// A typed refusal (0xB338 kind 2) still answers the client.
			log.Debugf("npcaction: 0x7338 mask 0x%X refused for %s: %s", mask, character.Name, refusal)
		}
		sendFrames(session, frames)
	})
}

/*
================
HandleNpcAction

HandleNpcAction applies the ordinary talk/shop subset whose server and
client contracts are both closed. Other masks fail closed; a visible menu
action is never acknowledged by an unrelated feature.
================
*/
func (rt *Runtime) HandleNpcAction(divisionID string, character *enterworld.Character, payload []byte) ([]wire.Frame, string) {
	if character == nil {
		return nil, "characterNotFound"
	}
	gid, mask, err := wire.DecodeNpcActionRequest(payload)
	if err != nil {
		return nil, err.Error()
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()
	// 510262: an NPC function request ends the requester's hide first,
	// whatever becomes of the request.
	rt.deps.Update(character, "npc-action-hide", func() bool {
		return rt.retireHide(divisionID, character, rt.Now().UnixMilli())
	})
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		return nil, "bound NPC is not the current selected object"
	}
	// A warehouse ticket's room (storage.go) opens without an NPC.
	if mask == simulation.NpcTalkFlagStorage && rt.storageAuthority != nil && rt.remoteStorageOpen(divisionID, character, gid) {
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	}
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	if !ok {
		return nil, fmt.Sprintf("gid %d is not a live in-scope NPC", gid)
	}
	if npc.TalkFlags&mask == 0 {
		return nil, fmt.Sprintf("NPC %s does not grant action mask 0x%X", npc.Codename, mask)
	}
	// 510250 runs 4A8E10 before any NPC function; a dialog the client kept
	// open while walking away is refused here with the "too far" notice.
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}

	switch mask {
	case simulation.NpcTalkFlagTalk:
		if npc.BaseSpeechSymbol == "" {
			return nil, fmt.Sprintf("NPC %s has no npcchat base-speech symbol", npc.Codename)
		}
		if rt.NpcQuests.Options != nil {
			options := rt.NpcQuests.Options(divisionID, character, npc.Codename)
			if len(options) != 0 {
				symbols := make([]string, 0, len(options))
				for _, option := range options {
					if option.Codename == "" || option.TitleSymbol == "" || option.PromptSymbol == "" {
						return nil, fmt.Sprintf("NPC %s quest option has an incomplete symbol contract", npc.Codename)
					}
					symbols = append(symbols, option.TitleSymbol)
				}
				prompt := npc.QuestSpeechSymbol
				if prompt == "" {
					prompt = npc.BaseSpeechSymbol
				}
				rt.NpcDialogs.Put(divisionID, character.Name, npcDialogSession{
					NpcGID: gid, NpcCode: npc.Codename, DefaultSymbol: npc.BaseSpeechSymbol,
					Stage: npcDialogOptions, Options: options,
				})
				return []wire.Frame{{
					Opcode: wire.OpNpcDialog, Payload: wire.EncodeNpcDialogOptions(prompt, symbols),
				}}, ""
			}
		}
		rt.NpcDialogs.Clear(divisionID, character.Name)
		return []wire.Frame{{
			Opcode:  wire.OpNpcDialog,
			Payload: wire.EncodeNpcDialogSymbol(npc.BaseSpeechSymbol),
		}}, ""
	case simulation.NpcTalkFlagShop, simulation.NpcTalkFlagSpecialTrade:
		ack := wire.EncodeNpcInteractionAck(mask)
		if mask == simulation.NpcTalkFlagSpecialTrade {
			// Client 75AF59 reads a mode byte after the special-trade mask.
			// Server 510500 publishes +42405: the ordinary shard mode is 0.
			ack = append(ack, 0)
		}
		frames := []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: ack}}
		if rt.Commerce != nil {
			// 510250 sets the shop function state (5); trades check it.
			rt.Selected.OpenFunction(divisionID, character.Name, gid)
			frames = append(frames, rt.shopCatalog(divisionID, character, gid))
		}
		return frames, ""
	case simulation.NpcTalkFlagStorage:
		// The room list (0x72C3) already reached the client; B338 [1][4]
		// opens it beside the inventory (SetNpcShopVisible( 5 )).
		if rt.storageAuthority == nil {
			return nil, "storage authority is not configured"
		}
		// The storage function state, which warehouse moves check.
		rt.Selected.OpenFunction(divisionID, character.Name, gid)
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	case simulation.NpcTalkFlagMagicOption:
		// B338 lock 0x80000000 opens the avatar grant window beside the
		// inventory; 0x361A (avatarbless.go) checks this function state.
		if rt.Alchemy == nil {
			return nil, "alchemy catalogue is not configured"
		}
		rt.Selected.OpenFunction(divisionID, character.Name, gid)
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	case guildStorageFunction:
		// B338 lock 0x4000 makes the client ask for the guild warehouse
		// (0x7515, npcguildstorage.go); the admission runs there.
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	default:
		return nil, fmt.Sprintf("action mask 0x%X has no reconstructed gameplay owner", mask)
	}
}

/*
================
npcForCurrentViewer
================
*/
func (rt *Runtime) npcForCurrentViewer(divisionID string, character *enterworld.Character, gid uint32) (simulation.NpcDef, bool) {
	if character == nil || !rt.NpcSpawn.Enabled {
		return simulation.NpcDef{}, false
	}
	world := rt.Worlds.Snapshot(
		simulation.WorldKey(divisionID, character.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
	)
	viewer := world.LiveSpawnAt(rt.Now().UnixMilli())
	for _, npc := range rt.NpcRoster {
		if npc.ObjectID != gid {
			continue
		}
		if !simulation.NpcVisibleAt(npc, viewer) {
			return simulation.NpcDef{}, false
		}
		return npc, true
	}
	return simulation.NpcDef{}, false
}
