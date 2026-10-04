/*
===========================================================================

register.go - session-bound gameplay transport registration and publication

All action lanes resolve the same authenticated character authority before
calling gameplay handlers and publish complete ordered response batches.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// Register wires this runtime's handlers onto the hub: 0x706D item
// operations, 0x72CD ground-item interact, 0x75BD selected-item use,
// 0x745A object select, and its 0x74B3 selected-target release.
// The runtime's deps MUST
// be the same instance enterworld.Register ran with - both lanes resolve
// characters through it, and separate instances would hold separate
// *Character pointers whose mutations diverge.
//
// The Runtime carries the division ground registry; hand its TickHook to
// the simulation Ticker for the TTL sweep and its GroundRefItemCodenames to
// Deps.ExtraRefItemCodenames. When the codename seam matters (bootstrap
// must name the division drops), build the Runtime and attach its seam to
// the shared deps pointer - and register the ONE
// runtime everything else (AttachGround, the tick hook) was built around;
// a second NewRuntime would fork the ground-item plane:
//
//	textdata, _ := enterworld.LoadTextdataCatalogs(textdataDir)
//	deps, _ := enterworld.NewDevDeps(enterworld.DevPaths{RosterPath: characterAuthorityCatalog}, textdata)
//	actions := action.NewRuntime(deps, deps.MonsterState)
//	deps.ExtraRefItemCodenames = actions.GroundRefItemCodenames
//	enterworld.Register(ts.Hub, deps)
//	actions.Register(ts.Hub)
/*
================
Register
================
*/
func (rt *Runtime) Register(hub *transport.Hub) {
	rt.registerDeparture(hub)
	rt.registerMall(hub)
	hub.Handle(0x7341, rt.hubHandler(hub, rt.HandleBerserk))
	hub.Handle(0x7495, rt.hubHandler(hub, rt.HandlePortal))
	hub.Handle(opNpcRepairRequest, rt.hubHandler(hub, rt.HandleNpcRepair))
	hub.Handle(0x72dd, rt.hubHandler(hub, rt.HandleReturnCancel))
	hub.Handle(wire.OpCosBehaviorRequest, rt.hubHandler(hub, rt.HandleCosBehavior))
	hub.Handle(0x77e7, rt.hubHandler(hub, rt.HandleRetailBuyback))
	hub.Handle(opShopBuyback, rt.hubHandler(hub, rt.HandleBuyback))
	hub.Handle(wire.OpItemMoveRequest, rt.hubHandler(hub, rt.HandleItemMove))
	hub.Handle(wire.OpTargetInteract, rt.hubHandler(hub, rt.HandleTargetInteract))
	hub.Handle(wire.OpItemUseRequest, rt.hubHandler(hub, rt.HandleItemUse))
	hub.Handle(wire.OpCosCommandRequest, rt.hubHandler(hub, rt.HandleCosCommand))
	hub.Handle(wire.OpCosRideRequest, rt.hubHandler(hub, rt.HandleCosRide))
	hub.Handle(opCosCancelRequest, rt.hubHandler(hub, rt.HandleCosCancel))
	hub.Handle(opCosTerminateRequest, rt.hubHandler(hub, rt.HandleCosTerminate))
	hub.Handle(wire.OpLocalRebirthRequest, rt.hubHandler(hub, rt.HandleLocalRebirth))
	hub.Handle(wire.OpVisualFlagsRequest, rt.hubHandler(hub, rt.HandleVisualFlags))
	// 0x745A object select/interact rides its own glue (select.go): its
	// refusals are silent - the B06D error channel below would be a wrong
	// conversation for a select.
	rt.registerObjectSelect(hub)
	// 0x74B3 releases only the exact selection recorded by 0x745A and
	// answers retail's 0xB4B3 mode-1 close sweep.
	rt.registerTargetRelease(hub)
	// 0x7338 is the generic CIFNPCTalk action multiplexer. It must be
	// registered exactly once even when Gacha content is unavailable: talk,
	// shop and storage are independent actions on the same opcode.
	rt.registerNpcAction(hub)
	rt.registerStorage(hub)
	rt.registerNpcDialogResponse(hub)
	hub.Handle(wire.OpRebirthPointAppointRequest, rt.hubHandler(hub, rt.HandleRebirthPointAppointment))
	// Gacha's roll request shares this runtime's selected NPC, inventory
	// authority and per-division operation lock. Generic 0x7338 is registered
	// above by the NPC owner, never conditionally by this child feature.
	// Detached/unit runtimes may intentionally omit reference data. Production
	// composition calls ConfigureGacha and therefore admits both opcodes.
	if rt.GachaCatalog != nil {
		rt.registerGacha(hub)
	}
	if rt.Alchemy != nil {
		rt.registerAlchemy(hub)
	}
}

// opFunc is one transport-free runtime operation.
/*
================
opFunc
================
*/
type opFunc func(divisionID string, character *enterworld.Character, payload []byte) OpResult

// hubHandler adapts a runtime operation onto the hub: resolve the bound
// character (never trust client-supplied names on later frames), run the op,
// answer the acting session, and fan the broadcast frames to source observers
// - the origin excluded, it already holds its own burst.
/*
================
hubHandler
================
*/
func (rt *Runtime) hubHandler(hub *transport.Hub, op opFunc) transport.HandlerFunc {
	return func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			// This adapter also handles portals, pets and rebirth. Without
			// a bound actor it cannot fabricate an inventory B06D reply
			// for those unrelated conversations. Reject before dispatch.
			log.Debugf("action: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}

		result := op(divisionID, character, payload)
		if result.DiagnosticRefusal != "" {
			log.Debugf(
				"action: 0x%04X refused for %s: %s",
				opcode, character.Name, result.DiagnosticRefusal,
			)
		}
		sendFrames(s, result.Frames)

		BroadcastObservedFrames(hub, divisionID, s.ID, enterworld.ObjectIDForCharacter(character), result.Broadcast)
		for _, recipient := range result.Recipients {
			for _, peer := range hub.SessionsInDivision(divisionID) {
				c, d, ok := enterworld.SessionCharacter(rt.deps, peer)
				if ok && d == divisionID && c.ID == recipient.CharacterID && peer.WorldReady() {
					sendFrames(peer, recipient.Frames)
				}
			}
		}
	}
}

/*
================
BroadcastObservedFrames
================
*/
func BroadcastObservedFrames(hub *transport.Hub, division string, exceptSession uint64, sourceGID uint32, frames []wire.Frame) {
	batch := make([]transport.Frame, len(frames))
	for i, f := range frames {
		batch[i] = transport.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: transport.ScopeChanges(f.Scope)}
	}
	hub.BroadcastObserved(division, exceptSession, sourceGID, batch)
}

// SendFrames publishes a complete action result, including scene replacement,
// as one transaction. Re-entry must share the initial admission's atomicity.
/*
================
SendFrames
================
*/
func SendFrames(s *transport.Session, frames []wire.Frame) {
	batch := make([]transport.Frame, len(frames))
	for i, f := range frames {
		batch[i] = transport.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: transport.ScopeChanges(f.Scope)}
	}
	var err error
	if len(batch) > 0 && (batch[0].Opcode == enterworld.OpcodeResetClient || batch[0].Opcode == 0x366a) {
		err = s.SendSceneReset(batch)
	} else {
		err = s.SendBatch(batch)
	}
	if err != nil {
		log.Debugf("action: send batch to session %d failed: %v", s.ID, err)
	}
}

/*
================
sendFrames
================
*/
func sendFrames(s *transport.Session, frames []wire.Frame) { SendFrames(s, frames) }
