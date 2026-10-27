/*
===========================================================================

register.go - session-bound gameplay transport registration and publication

All action lanes resolve the same authenticated character authority before
calling gameplay handlers and publish complete ordered response batches.

===========================================================================
*/
package action

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
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
	hub.Handle(opFortressInteraction, rt.hubHandler(hub, rt.HandleFortressInteraction))
	hub.Handle(opFortressReturn, rt.hubHandler(hub, rt.HandleFortressReturn))
	hub.Handle(opNpcRepairRequest, rt.hubHandler(hub, rt.HandleNpcRepair))
	hub.Handle(opJobJoinRequest, rt.hubHandler(hub, rt.HandleJobJoin))
	hub.Handle(opJobWithdrawRequest, rt.hubHandler(hub, rt.HandleJobWithdraw))
	hub.Handle(opJobAliasRequest, rt.hubHandler(hub, rt.HandleJobAlias))
	hub.Handle(opPetNameRequest, rt.hubHandler(hub, rt.HandlePetName))
	hub.Handle(opJobRankRequest, rt.hubHandler(hub, rt.HandleJobRank))
	hub.Handle(opJobOutcomeRequest, rt.hubHandler(hub, rt.HandleJobOutcome))
	hub.Handle(opJobPrevInfoRequest, rt.hubHandler(hub, rt.HandleJobPrevInfo))
	hub.Handle(opGuildLevelUpRequest, rt.hubHandler(hub, rt.HandleGuildLevelUp))
	hub.Handle(opMercenaryAttribute, rt.hubHandler(hub, rt.HandleMercenaryAttribute))
	hub.Handle(opMercenaryDismiss, rt.hubHandler(hub, rt.HandleMercenaryDismiss))
	hub.Handle(opGuildMasterLeave, rt.hubHandler(hub, rt.HandleGuildMasterLeave))
	hub.Handle(opGuildCompensation, rt.hubHandler(hub, rt.HandleGuildCompensation))
	hub.Handle(opGuildCompensationPay, rt.hubHandler(hub, rt.HandleGuildCompensationClaim))
	hub.Handle(opGuildMasterRelease, rt.hubHandler(hub, rt.HandleGuildMasterRelease))
	hub.Handle(opGuildBallot, rt.hubHandler(hub, rt.HandleGuildBallot))
	hub.Handle(opGuildVoteJoin, rt.hubHandler(hub, rt.HandleGuildVoteParticipate))
	hub.Handle(opGuildStorageOpen, rt.hubHandler(hub, rt.HandleGuildStorageOpen))
	hub.Handle(opGuildStorageList, rt.hubHandler(hub, rt.HandleGuildStorageList))
	hub.Handle(opGuildStorageClose, rt.hubHandler(hub, rt.HandleGuildStorageClose))
	hub.Handle(0x72dd, rt.hubHandler(hub, rt.HandleReturnCancel))
	hub.Handle(wire.OpCountJobUse, rt.hubHandler(hub, rt.HandleCountJobUse))
	hub.Handle(wire.OpCosBehaviorRequest, rt.hubHandler(hub, rt.HandleCosBehavior))
	hub.Handle(0x77e7, rt.hubHandler(hub, rt.HandleRetailBuyback))
	hub.Handle(opShopBuyback, rt.hubHandler(hub, rt.HandleBuyback))
	hub.Handle(wire.OpItemMoveRequest, rt.hubHandler(hub, rt.HandleItemMove))
	hub.Handle(wire.OpExchangeRequest, rt.hubHandler(hub, rt.HandleExchangeRequest))
	hub.Handle(wire.OpExchangeConfirm, rt.hubHandler(hub, rt.HandleExchangeConfirm))
	hub.Handle(wire.OpExchangeApprove, rt.hubHandler(hub, rt.HandleExchangeApprove))
	hub.Handle(wire.OpExchangeCancel, rt.hubHandler(hub, rt.HandleExchangeCancel))
	hub.Handle(wire.OpStallCreate, rt.hubHandler(hub, rt.HandleStallCreate))
	hub.Handle(wire.OpStallClose, rt.hubHandler(hub, rt.HandleStallClose))
	hub.Handle(wire.OpStallVisit, rt.hubHandler(hub, rt.HandleStallVisit))
	hub.Handle(wire.OpStallLeave, rt.hubHandler(hub, rt.HandleStallLeave))
	hub.Handle(wire.OpStallBuy, rt.hubHandler(hub, rt.HandleStallBuy))
	hub.Handle(wire.OpStallEdit, rt.hubHandler(hub, rt.HandleStallEdit))
	hub.Handle(wire.OpStallNetworkSearch, rt.hubHandler(hub, rt.HandleStallNetworkSearch))
	hub.Handle(wire.OpStallNetworkBuy, rt.hubHandler(hub, rt.HandleStallNetworkBuy))
	hub.Handle(wire.OpTargetInteract, rt.hubHandler(hub, rt.HandleTargetInteract))
	hub.Handle(wire.OpItemUseRequest, rt.hubHandler(hub, rt.HandleItemUse))
	hub.Handle(wire.OpCosCommandRequest, rt.cosCommandHubHandler(hub))
	hub.Handle(wire.OpCosRideRequest, rt.hubHandler(hub, rt.HandleCosRide))
	hub.Handle(opCosCancelRequest, rt.hubHandler(hub, rt.HandleCosCancel))
	hub.Handle(opCosTerminateRequest, rt.hubHandler(hub, rt.HandleCosTerminate))
	hub.Handle(wire.OpLocalRebirthRequest, rt.hubHandler(hub, rt.HandleLocalRebirth))
	hub.Handle(wire.OpVisualFlagsRequest, rt.hubHandler(hub, rt.HandleVisualFlags))
	// 0x745A object select/interact rides its own glue (select.go): its
	// refusals are silent - the B06D error channel below would be a wrong
	// conversation for a select.
	rt.registerObjectSelect(hub)
	// 0x7753 toggles Capture the Flag registration (flagwar.go).
	hub.Handle(wire.OpFlagWarRegisterRequest, rt.hubHandler(hub, rt.HandleFlagWarRegister))
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
		rt.registerAvatarBless(hub)
	}
}

// opFunc is one transport-free runtime operation.
/*
================
opFunc
================
*/
type opFunc func(divisionID string, character *enterworld.Character, payload []byte) OpResult

/*
================
hubHandler

Adapts a runtime operation onto the hub: resolve the bound character
(never trust client-supplied names on later frames), run the op, answer
the acting session, and fan the broadcast frames to source observers -
the origin excluded, it already holds its own burst.

A request that arrives while the session's scene is loading is never
executed: its world is being rebuilt, and an answer computed against it
would land in a world that never asked (a mall projection after the
bootstrap reset the mall: "Unexpected mall projection"). Every scene
reset (entry, relocation, recall, a return) begins loading, and only the
client's game-ready (0x3012) ends it server-side - but the client retires
its loading screen on its own send, so it can already hold a pending
answer when the request gets here. The refusal therefore still answers,
with the opcode's native failure frame (sceneLoadingRefusal), so that
pending retires the way any refused request retires it; a bootstrap
later rebuilds the state the refusal left alone. Opcodes with no failure
conversation hold no pending answer and stay silent.
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

		unlockPublication := rt.LockPublication(divisionID, "requestOp")
		defer unlockPublication()
		// Checked under the publication gate: a reset another handler pushes
		// (a recall, a relocation) holds the same gate, so a request that
		// waited on it sees that reset here instead of answering into it.
		if _, live := s.SceneRevision(); !live {
			// Info, not debug: a client that sends while loading is a bug to see.
			if refusal := sceneLoadingRefusal(opcode); len(refusal) > 0 {
				sendFrames(s, refusal)
			}
			log.Infof("action: 0x%04X from %s refused: the scene is loading", opcode, character.Name)
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
		for _, route := range result.Around {
			// No session is excepted: the actor watches its own recipients.
			BroadcastObservedFrames(hub, divisionID, 0, route.SourceGID, route.Frames)
		}
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
sceneLoadingRefusal

The native failure answer for a request the loading gate refuses, or nil
when the opcode has none. The v1.150 client's 0xB06D handler retires its
item-move pending on any result frame before it parses one, and its
failure arm reads exactly [2, code]; its 0xB5BD handler likewise reads a
non-success result byte and one code byte and shows the notice - so the
generic invalid-request notice ends the client's wait the way every
other refused request does.
================
*/
func sceneLoadingRefusal(opcode uint16) []wire.Frame {
	switch opcode {
	case wire.OpItemMoveRequest:
		return failureResult(wire.ErrCodeInvalidRequest).Frames
	case wire.OpItemUseRequest:
		return itemUseFailure(wire.ErrCodeInvalidRequest).Frames
	case opNpcRepairRequest:
		// 0x746F waits on 0xB46F; its failure arm reads [2, notice] and
		// retires the repair pending (689420 parses either arm first).
		return repairRefusal(wire.ErrCodeInvalidRequest).Frames
	case 0x77e7:
		// A retail buyback waits on its own result lane (0xB7E7); answer
		// with the same [2, code] refusal the buyback itself raises.
		return []wire.Frame{{Opcode: 0xB7E7, Payload: []byte{2, wire.ErrCodeInvalidRequest}}}
	case opMallCatalog:
		// The browser's catalogue request waits on its projection lane
		// (extension opcode 15); an error projection retires the request
		// the way a failed native purchase retires its own pending lane.
		if payload, err := json.Marshal(mallProjection{Version: 1, Error: "mall is unavailable while entering"}); err == nil {
			return []wire.Frame{{Opcode: opMallCatalog, Payload: payload}}
		}
	}
	return nil
}

/*
================
frameSourceGID

A ground-item despawn (0x36AB) must reach every session that published the
drop (including via the 0x3417 bootstrap before any movement tick), even if
that viewer has not published the picking character or grab pet.

A ride-state apply (0xB4B5 [1][rider u32][flag][mount u32]) goes to its
rider's observers, whoever's action caused it: the v1.150 client resolves
the rider with no null test (777FB2..777FC3) and faults on one it does not
hold (#1613: an attacker's observer got a killed rider's dismount).
================
*/
func frameSourceGID(defaultSourceGID uint32, f wire.Frame) uint32 {
	if f.Opcode == wire.OpCosRideState {
		if rider, ok := wire.CosRideStateRider(f.Payload); ok {
			return rider
		}
	}
	if f.Opcode == wire.OpObjectDespawn && len(f.Scope) == 1 && !f.Scope[0].Visible {
		gid := f.Scope[0].GID
		if gid > domain.GroundItemGIDBase && gid <= domain.GroundItemGIDLimit {
			return gid
		}
	}
	return defaultSourceGID
}

/*
================
BroadcastObservedFrames
================
*/
func BroadcastObservedFrames(hub *transport.Hub, division string, exceptSession uint64, sourceGID uint32, frames []wire.Frame) {
	if len(frames) == 0 {
		return
	}
	start := 0
	currentSource := frameSourceGID(sourceGID, frames[0])
	for i := 1; i <= len(frames); i++ {
		var nextSource uint32
		if i < len(frames) {
			nextSource = frameSourceGID(sourceGID, frames[i])
			if nextSource == currentSource {
				continue
			}
		}
		hub.BroadcastObserved(division, exceptSession, currentSource, transportFrames(frames[start:i]))
		start = i
		currentSource = nextSource
	}
}

/*
================
transportFrames

The game lane's frames as the transport queues them, scope changes included.
================
*/
func transportFrames(frames []wire.Frame) []transport.Frame {
	batch := make([]transport.Frame, len(frames))
	for i, f := range frames {
		batch[i] = transport.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: transport.ScopeChanges(f.Scope)}
	}
	return batch
}

// SendFrames publishes a complete action result, including scene replacement,
// as one transaction. Re-entry must share the initial admission's atomicity.
/*
================
SendFrames
================
*/
func SendFrames(s *transport.Session, frames []wire.Frame) {
	batch := transportFrames(frames)
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
