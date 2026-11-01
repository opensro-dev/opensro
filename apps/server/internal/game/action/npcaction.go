/*
===========================================================================

npcaction.go - the NPC function request: talk, shop and service admission

One 0x7338 names an NPC and a function mask. This module binds the
request to the still-selected NPC, runs the native hit-range gate and
admits the function the NPC grants. Every refusal answers with the
native 0xB338 kind-2 refusal, so the client's pending function request
always retires; silence would leave it pending forever.

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
		// While the scene loads the request cannot execute (its first state
		// op retires the player's hide), and the client still holds the
		// function request pending: answer with the native 0xB338 kind-2
		// refusal the way every refused function request retires it.
		if _, live := session.SceneRevision(); !live {
			sendFrames(session, []wire.Frame{{
				Opcode:  wire.OpNpcInteractionAck,
				Payload: wire.EncodeNpcInteractionRefusal(wire.ErrCodeInvalidRequest),
			}})
			log.Infof("npcaction: 0x7338 mask 0x%X from %s refused: the scene is loading", mask, character.Name)
			return
		}
		frames, refusal := rt.routeNpcAction(divisionID, character, mask, payload)
		if refusal != "" {
			// The frames carry the refusal's 0xB338 kind-2 answer; the
			// string is only logged.
			log.Debugf("npcaction: 0x7338 mask 0x%X refused for %s: %s", mask, character.Name, refusal)
		}
		sendFrames(session, frames)
	})
}

/*
================
routeNpcAction

Routes one decoded 0x7338 by its mask: the gacha flag to the Magic Pop
machine, everything else to the ordinary function handler. With no
gacha catalogue configured the machine cannot open; the click answers
the generic invalid-request refusal like every other unconfigured
service, so the client's pending function request retires instead of
hanging on silence.
================
*/
func (rt *Runtime) routeNpcAction(divisionID string, character *enterworld.Character, mask uint32, payload []byte) ([]wire.Frame, string) {
	if mask == gacha.InteractionFlagGacha {
		if rt.GachaCatalog == nil {
			return npcFunctionRefusal(wire.ErrCodeInvalidRequest), "Gacha catalogue is unavailable"
		}
		return rt.HandleGachaNpcAction(divisionID, character, payload)
	}
	return rt.HandleNpcAction(divisionID, character, payload)
}

/*
================
HandleNpcAction

HandleNpcAction applies the ordinary talk/shop subset whose server and
client contracts are both closed. Other masks fail closed; a visible menu
action is never acknowledged by an unrelated feature. A refusal always
carries its 0xB338 kind-2 frame (see npcFunctionRefusal); the returned
string is only the server's log line.
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
	// Natively an NPC function request ends the requester's hide first,
	// whatever becomes of the request.
	rt.deps.Update(character, "npc-action-hide", func() bool {
		return rt.retireHide(divisionID, character, rt.Now().UnixMilli())
	})
	if selected, ok := rt.Selected.Get(divisionID, character.Name); !ok || selected != gid {
		// The named object is not the bound one, so the request resolves
		// to no object: the native no-object answer (code 3).
		return npcFunctionRefusal(selectObjectNotFound), "bound NPC is not the current selected object"
	}
	// A warehouse ticket's room (storage.go) opens without an NPC.
	if mask == simulation.NpcTalkFlagStorage && rt.storageAuthority != nil && rt.remoteStorageOpen(divisionID, character, gid) {
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	}
	npc, ok := rt.npcForCurrentViewer(divisionID, character, gid)
	if !ok {
		return npcFunctionRefusal(selectObjectNotFound), fmt.Sprintf("gid %d is not a live in-scope NPC", gid)
	}
	if npc.TalkFlags&mask == 0 {
		return npcFunctionRefusal(npcFunctionNotOffered), fmt.Sprintf("NPC %s does not grant action mask 0x%X", npc.Codename, mask)
	}
	// The native NPC function request runs the hit-range check before any NPC function; a dialog the client kept
	// open while walking away is refused here with the "too far" notice.
	if !rt.npcWithinHitRange(divisionID, character, npc) {
		return npcFunctionTooFar(), fmt.Sprintf("NPC %s is beyond its interaction range", npc.Codename)
	}

	switch mask {
	case simulation.NpcTalkFlagTalk:
		if npc.BaseSpeechSymbol == "" {
			// The talk entry resolves to no dialog: the native no-object
			// answer (code 3) retires the request.
			return npcFunctionRefusal(selectObjectNotFound), fmt.Sprintf("NPC %s has no npcchat base-speech symbol", npc.Codename)
		}
		if rt.NpcQuests.Options != nil {
			options := rt.NpcQuests.Options(divisionID, character, npc.Codename)
			if len(options) != 0 {
				symbols := make([]string, 0, len(options))
				for _, option := range options {
					if option.Codename == "" || option.TitleSymbol == "" || option.PromptSymbol == "" {
						return npcFunctionRefusal(wire.ErrCodeInvalidRequest), fmt.Sprintf("NPC %s quest option has an incomplete symbol contract", npc.Codename)
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
			// The reference server publishes +42405: the ordinary shard mode is 0.
			ack = append(ack, 0)
		}
		frames := []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: ack}}
		// 7046 writes interaction state 5 for a shop over the warehouse's 6:
		// one slot, so an open warehouse ends (the warehouse move refuses 0x1834).
		rt.endStorageInteraction(divisionID, character.Name)
		if rt.Commerce != nil {
			// The native request sets the shop function state (5); trades check it.
			rt.Selected.OpenFunction(divisionID, character.Name, gid)
			frames = append(frames, rt.shopCatalog(divisionID, character, gid))
		}
		return frames, ""
	case simulation.NpcTalkFlagStorage:
		// The room list (0x72C3) already reached the client; B338 [1][4]
		// opens it beside the inventory (SetNpcShopVisible( 5 )).
		if rt.storageAuthority == nil {
			return npcFunctionRefusal(wire.ErrCodeInvalidRequest), "storage authority is not configured"
		}
		// The storage function state, and the warehouse session the moves
		// check (storage.go), which outlives later selections.
		rt.Selected.OpenFunction(divisionID, character.Name, gid)
		rt.openStorageSession(divisionID, character.Name, gid)
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	case simulation.NpcTalkFlagMagicOption:
		// B338 lock 0x80000000 opens the avatar grant window beside the
		// inventory; 0x361A (avatarbless.go) checks this function state.
		if rt.Alchemy == nil {
			return npcFunctionRefusal(wire.ErrCodeInvalidRequest), "alchemy catalogue is not configured"
		}
		rt.Selected.OpenFunction(divisionID, character.Name, gid)
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	case guildStorageFunction:
		// B338 lock 0x4000 makes the client ask for the guild warehouse
		// (0x7515, npcguildstorage.go); the admission runs there.
		return []wire.Frame{{Opcode: wire.OpNpcInteractionAck, Payload: wire.EncodeNpcInteractionAck(mask)}}, ""
	default:
		return npcFunctionRefusal(npcFunctionNotOffered), fmt.Sprintf("action mask 0x%X has no reconstructed gameplay owner", mask)
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

/*
================
npcForGID

The roster NPC with this object id, before any viewer test. The gacha
handlers answer a gid that names nothing (native code 0) differently from
one outside the viewer's sector scope (code 3), so they look the roster up
in two steps where npcForCurrentViewer folds them.
================
*/
func (rt *Runtime) npcForGID(gid uint32) (simulation.NpcDef, bool) {
	for _, npc := range rt.NpcRoster {
		if npc.ObjectID == gid {
			return npc, true
		}
	}
	return simulation.NpcDef{}, false
}

/*
================
npcVisibleTo

True while the NPC stands in the viewer's plane and sector scope.
================
*/
func (rt *Runtime) npcVisibleTo(divisionID string, character *enterworld.Character, npc simulation.NpcDef) bool {
	world := rt.Worlds.Snapshot(
		simulation.WorldKey(divisionID, character.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
	)
	return simulation.NpcVisibleAt(npc, world.LiveSpawnAt(rt.Now().UnixMilli()))
}
