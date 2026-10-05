/*
===========================================================================

select.go - The 0x745A object select/interact request. It lives on the item plane
because the plane already owns every gid the request can legally name:
the shared WorldStore, the division ground registry (the other
target-shaped request, 0x72CD, lives here too), the shared monster
population and, through deps, the division character roster. Static
CITeleportGate records share the roster interest/liveness owner, but use
the native structure grant (capabilities without the CICNPC mask). Guard
and AT structures still require their own authored spawn authorities.

Wire contract: internal/game/item/wire/objectselect.go (dual-use send sites and
the 0xB45A twin's encoder). The browser client folded sub_764c60's
live-CICNPC arm REAL (npcTalkPlane.ts consumes it), retiring the old
"0xB45A is unconsumable" ruling from server-wave seq 51: the GRANT for
a live roster NPC now answers a real 0xB45A - result 1, the gid, the
capability flags its service set projects (simulation.ResolveNpcTalkFlags) -
and the NPC talk window opens from live play. A live in-scope monster
answers the same opcode with current HP and flags zero, updating the
target HUD without opening the talk window. Player and ground-drop grants
record without a frame: neither grant has a proven server response
payload, while the now-complete client fold can consume the local-player
and non-character result arms when an authority legitimately emits one.
Refusals send nothing in either direction (the historical native 0xB45A
probe crashed the retail client on uninitialized interaction scratch),
except the native out-of-range refusal [2, 4] for an NPC beyond its 4A8E10
class range (npcrange.go), which the browser client consumes silently.

===========================================================================
*/

package action

import (
	"fmt"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
SelectionStore

SelectionStore is the per-character object-selection plane: the last gid
each character validly selected (division-scoped key, the PendingTracker
key convention), and the NPC function the selection opened. Runtime-only
state, like the fixture's in-memory selection - nothing persists, a reboot
simply forgets selections.
It locks itself so cross-lane readers never need the item-operation lane.
================
*/
type SelectionStore struct {
	mu          sync.Mutex
	byCharacter map[string]selection
}

/*
================
selection

selection is one character's selected gid and its open NPC function.
510250 sets CGObjPC +0xC+6 to 5 when an in-range request opens a shop;
trades check that state, not distance. A new selection or a release ends
it, so every path that clears the selection also closes the function.
================
*/
type selection struct {
	gid          uint32
	functionOpen bool
}

/*
================
NewSelectionStore

NewSelectionStore builds an empty selection plane.
================
*/
func NewSelectionStore() *SelectionStore {
	return &SelectionStore{byCharacter: make(map[string]selection)}
}

/*
================
Set

Set records a character's selected object gid. Any open NPC function ends.
================
*/
func (s *SelectionStore) Set(divisionID, characterName string, gid uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byCharacter[selectionKey(divisionID, characterName)] = selection{gid: gid}
}

/*
================
Get

Get answers a character's selected object gid, if any.
================
*/
func (s *SelectionStore) Get(divisionID, characterName string) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.byCharacter[selectionKey(divisionID, characterName)]
	return row.gid, ok
}

/*
================
Clear

Clear forgets a character's selection and closes its NPC function.
================
*/
func (s *SelectionStore) Clear(divisionID, characterName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byCharacter, selectionKey(divisionID, characterName))
}

/*
================
OpenFunction

OpenFunction marks the NPC function of the selected gid open. It does
nothing unless gid is still the selection.
================
*/
func (s *SelectionStore) OpenFunction(divisionID, characterName string, gid uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := selectionKey(divisionID, characterName)
	if row, ok := s.byCharacter[key]; ok && row.gid == gid {
		s.byCharacter[key] = selection{gid: gid, functionOpen: true}
	}
}

/*
================
FunctionOpen

FunctionOpen answers whether gid is selected with its NPC function open.
================
*/
func (s *SelectionStore) FunctionOpen(divisionID, characterName string, gid uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.byCharacter[selectionKey(divisionID, characterName)]
	return ok && row.gid == gid && row.functionOpen
}

/*
================
selectionKey
================
*/
func selectionKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}

/*
================
SelectOutcome

SelectOutcome is one handled 0x745A: the recorded gid on a grant, or the
refusal reason. Frames carries the typed live NPC or monster 0xB45A for
the acting session, or the out-of-range refusal; player and ground grants
and every other refusal leave it empty (see the package comment).
================
*/
type SelectOutcome struct {
	// Selected is the recorded gid; meaningful only when Refusal is empty.
	Selected uint32
	Frames   []wire.Frame
	Refusal  string
}

/*
================
refusedSelect
================
*/
func refusedSelect(reason string) SelectOutcome {
	return SelectOutcome{Refusal: reason}
}

/*
================
registerObjectSelect

registerObjectSelect wires the 0x745A handler onto the hub. The 0xB45A
answer rides live roster-NPC and monster grants (outcome.Frames); this
plane still has no typed refusal channel, so an unbound session
discards silently - answering it with a B06D item error would invent a
conversation, the progression posture.
================
*/
func (rt *Runtime) registerObjectSelect(hub *transport.Hub) {
	hub.Handle(wire.OpObjectSelectRequest, func(s *transport.Session, opcode uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			log.Debugf("action: 0x%04X from unbound session %d discarded", opcode, s.ID)
			return
		}
		outcome := rt.HandleObjectSelect(divisionID, character, payload)
		if outcome.Refusal != "" {
			// A typed refusal (out of range) answers the client; the rest stay silent.
			log.Debugf("action: 0x745A refused for %s: %s", character.Name, outcome.Refusal)
			sendFrames(s, outcome.Frames)
			return
		}
		sendFrames(s, outcome.Frames)
		log.Debugf("action: 0x745A %s selected gid %d", character.Name, outcome.Selected)
	})
}

/*
================
HandleObjectSelect

HandleObjectSelect answers a C->S 0x745A object select/interact: strict
decode (exactly [u32le gid]), then a LIVENESS gate - the gid must resolve
to an object this division's world actually contains:

  - a division character's player entity (self included: the native
    self-click and clear-marker send sites both name the local player's
    own band; peers are world objects on the peer-visibility plane);
  - a roster NPC spawned for the ACTING character (gids are derived
    per-character, simulation.NpcObjectID), only while the NPC roster is
    enabled - with spawns off those gids exist on no client and accepting
    them would widen the domain;
  - a live monster from the shared division registry, only while its
    generated region is inside the acting character's current visibility
    ring;
  - a live ground drop in the division registry.

Anything else refuses: an object-interaction acceptor on a live server
that records arbitrary gids would hand later consumers (attack/talk
lanes) attacker-chosen targets (coordinator ruling, seq 51). Every
grant records the selection on the runtime store. The roster-NPC grant
answers with the 0xB45A talk flags; the monster grant answers with its
per-instance current HP and zero flags. Player and ground grants stay
frameless (no native response bytes are proven for those outcomes).
================
*/
func (rt *Runtime) HandleObjectSelect(divisionID string, character *enterworld.Character, payload []byte) SelectOutcome {
	if character == nil {
		return refusedSelect("characterNotFound")
	}

	unlock := rt.lockDivision(divisionID)
	defer unlock()

	gid, err := wire.DecodeObjectSelectRequest(payload)
	if err != nil {
		return refusedSelect(err.Error())
	}

	// Capture the pointer list before entering the read door. The source
	// returns an owned slice; peer fields are inspected only while the door
	// is held, without recursively acquiring the store read lock.
	peers := rt.deps.CharactersForDivision(divisionID)
	var target selectedObject
	var live, deletePending, tooFar bool
	rt.deps.Read(divisionID, func() {
		deletePending = character.DeletePending
		if !deletePending {
			world := rt.Worlds.Snapshot(
				simulation.WorldKey(divisionID, character.Name),
				func() simulation.WorldState { return simulation.SeedWorldState(character) },
			)
			viewerRegion := world.LiveSpawnAt(rt.Now().UnixMilli()).RegionID
			target, live = rt.resolveLiveObject(
				divisionID,
				character,
				peers,
				gid,
				viewerRegion,
			)
			// 52B040 runs 4A8E10 on every select: an NPC beyond its class
			// range is refused with code 4 and no selection is recorded.
			tooFar = live && target.npc != nil && !rt.npcWithinHitRange(divisionID, character, *target.npc)
		}
	})
	if deletePending {
		return refusedSelect("deletePending")
	}
	if !live {
		return refusedSelect(fmt.Sprintf("gid %d resolves to no live object in division %s", gid, divisionID))
	}
	if tooFar {
		return SelectOutcome{
			Refusal: fmt.Sprintf("NPC gid %d is beyond its interaction range", gid),
			Frames: []wire.Frame{{
				Opcode:  wire.OpObjectSelectResult,
				Payload: wire.EncodeObjectSelectRefusal(hitRangeTooFar),
			}},
		}
	}
	rt.Selected.Set(divisionID, character.Name, gid)
	rt.NpcDialogs.Clear(divisionID, character.Name)
	outcome := SelectOutcome{Selected: gid}
	var flags uint32
	if target.npc != nil {
		flags = target.npc.TalkFlags | rt.reverseReturnCapability(*target.npc, character)
	}
	if target.npc != nil {
		// npcExtra 0: no forced action-0x38 menu row. Every live authored NPC
		// receives a typed result, including a legitimate zero-capability row;
		// silence used to leave the client's previous NPC binding alive.
		outcome.Frames = []wire.Frame{{
			Opcode:  wire.OpObjectSelectResult,
			Payload: wire.EncodeNpcObjectSelectResult(gid, flags, 0),
		}}
	}
	if target.npc != nil && target.npc.Teleport != nil {
		w := wire.NewWriter(11).U8(1).U32(gid).U32(flags)
		if target.npc.Teleport.FortressID != 0 {
			tax := rt.commerceTax(divisionID, target.npc.RefObjID, character)
			rate := tax.Percent
			if rate > 0 && tax.Exempt {
				rate = 0
			}
			w.U16(uint16(rate))
		}
		outcome.Frames = []wire.Frame{{Opcode: wire.OpObjectSelectResult, Payload: w.Payload()}}
	}
	if target.monster != nil {
		outcome.Frames = []wire.Frame{{
			Opcode:  wire.OpObjectSelectResult,
			Payload: wire.EncodeMonsterObjectSelectResult(gid, target.monster.CurrentHP),
		}}
	}
	return outcome
}

/*
================
selectedObject
================
*/
type selectedObject struct {
	npc     *simulation.NpcDef
	monster *monster.Instance
}

/*
================
resolveLiveObject

resolveLiveObject answers whether a gid names an object the division's
world contains right now and returns the typed server authority needed
to encode NPC or monster selection state. Callers hold the division
operation lock.
================
*/
func (rt *Runtime) resolveLiveObject(
	divisionID string,
	character *enterworld.Character,
	peers []*enterworld.Character,
	gid uint32,
	viewerRegion uint16,
) (selectedObject, bool) {
	// Division characters (self and peers) - the player entity band.
	for _, peer := range peers {
		if peer != nil && !peer.DeletePending && enterworld.ObjectIDForCharacter(peer) == gid {
			return selectedObject{}, true
		}
	}
	// Selection uses the same live message-block predicate as NPC publication.
	if rt.NpcSpawn.Enabled {
		viewer := rt.liveSpawn(simulation.WorldKey(divisionID, character.Name), character, rt.Now().UnixMilli())
		for index := range rt.NpcRoster {
			npc := &rt.NpcRoster[index]
			if npc.ObjectID == gid &&
				simulation.NpcVisibleAt(*npc, viewer) {
				return selectedObject{npc: &rt.NpcRoster[index]}, true
			}
		}
	}
	// Shared monsters are selectable only while they are live and inside
	// the same ring that made them visible to this character.
	if rt.Monsters != nil {
		if instance, ok := rt.characterMonster(divisionID, character, gid); ok &&
			regionInScope(instance.Spawn.RegionID, simulation.RegionScopeRing(viewerRegion)) {
			return selectedObject{monster: &instance}, true
		}
	}
	// Live ground drops.
	if rt.Ground != nil {
		if _, ok := rt.characterGround(divisionID, character, gid); ok {
			return selectedObject{}, true
		}
	}
	return selectedObject{}, false
}

/*
================
regionInScope
================
*/
func regionInScope(regionID uint16, scope []uint16) bool {
	for _, candidate := range scope {
		if candidate == regionID {
			return true
		}
	}
	return false
}
