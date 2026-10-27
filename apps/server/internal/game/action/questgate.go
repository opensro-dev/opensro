/*
===========================================================================

questgate.go - the quest instance exit gate (#1878)

Every v1.150 quest instance raises an exit gate when its boss dies: the
reference server's quest handlers spawn the INS_QUEST_TELEPORT building
(teleportbuilding ref 19076, region 0: instance-created, never in the
static roster) beside the fallen boss and give it the teleport-gate
service 0x1C with one destination. A resident who selects it reads the
gate's rows in the select answer and leaves through 0x7495 type 3.

The gate lives here for its world's lifetime: the quest programme
spawns it (SpawnQuestExitGate) and removes it when the layer closes
(RemoveQuestExitGates); the ticker's NPC scope pass publishes it to
that world's sessions only (QuestGateNpcs); select (questGateSelect)
and 0x7495 type 3 (handleQuestGate) answer for it.

Client wire, from the v1.150 client's readers:

	select answer  [u8 1][u32 gid][u32 flags 0x8000000][u8 rows]
	               then per row [u8 kind]; kind 0 carries nothing more
	               (CIFNPCTalk_AppendTeleportRows 5D32C0: kind 0 is the
	               SN_TALK_COMMON_INS_WADDUNGOT line, 1 an ascii symbol,
	               2 a u16 with UIIT_CTL_TELEPORT_FREE_RESULT)
	row click      0x7495 [u32 gid][u8 3][u8 choice]
	               (CIFNpcTalk_ExecuteMenuAction 5DA1B0 case 0x2A)

The quest gates of v1.150 carry one row of kind 0: the player's last
recall point, in the world it was recorded in.

===========================================================================
*/
package action

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// questGateCodename is the exit gate's building (teleportbuilding).
	questGateCodename = "INS_QUEST_TELEPORT"
	// The gate's gids: a band of the NPC range below the Capture the Flag
	// arena's (flagwar_match.go, NPCGIDBase+90000) and above the static
	// teleport buildings (250000 + ref; refs stay below 30000).
	questGateGIDBase = domain.NPCGIDBase + 85_000
	questGateGIDSpan = 5_000

	// gateInstanceTravel is the 0x7495 type the gate's rows send.
	gateInstanceTravel uint8 = 3
	// questGateRecallPoint is row kind 0: the last recall point.
	questGateRecallPoint uint8 = 0

	// The reference quest handlers place the gate 20 to 100 from the
	// fallen boss on each horizontal axis, facing a random direction.
	questGateOffsetMin  = 20.0
	questGateOffsetSpan = 80.0

	// errCodeGateMoveFailed is the low byte the reference case 3 answers
	// when the resolved point cannot be entered (its move failure).
	errCodeGateMoveFailed uint8 = 0x03
)

/*
================
questGateBuilding

The INS_QUEST_TELEPORT row: its identity, type word and bounds.
================
*/
type questGateBuilding struct {
	ref       uint32
	nameStrID string
	tid       uint16
	height    float64
	radius    float64
}

/*
================
questGateBuildingRow

Parses the gate's teleportbuilding row: RefObjID column 1, name symbol
column 5, TypeID columns 7-12 packed as AppendTeleportGates packs them,
height column 49 and radius column 50.
================
*/
func questGateBuildingRow(r []string) (*questGateBuilding, error) {
	if len(r) < 51 {
		return nil, fmt.Errorf("teleportbuilding %s row is truncated", questGateCodename)
	}
	ref64, e := strconv.ParseUint(r[1], 10, 32)
	ref := uint32(ref64)
	if e != nil || ref == 0 {
		return nil, fmt.Errorf("teleportbuilding %s identity", questGateCodename)
	}
	tid := uint16(0)
	for j, col := range []int{7, 8, 9, 10, 11, 12} {
		v, e := strconv.ParseUint(r[col], 10, 16)
		if e != nil {
			return nil, fmt.Errorf("teleportbuilding %s TypeID", questGateCodename)
		}
		tid |= uint16(v) << []uint{0, 1, 2, 5, 7, 11}[j]
	}
	height, e1 := strconv.ParseFloat(r[49], 64)
	radius, e2 := strconv.ParseFloat(r[50], 64)
	if e1 != nil || e2 != nil || height <= 0 || radius <= 0 {
		return nil, fmt.Errorf("teleportbuilding %s bounds", questGateCodename)
	}
	return &questGateBuilding{ref: ref, nameStrID: r[5], tid: tid, height: height, radius: radius}, nil
}

/*
================
questGate

One raised gate: where it stands, the world it stands in and its rows.
================
*/
type questGate struct {
	gid   uint32
	world instance.ID
	spawn simulation.Spawn
	rows  []uint8
}

/*
================
questGateBook

Every standing gate, by gid. The ticker reads it from its shard workers,
so it keeps its own lock.
================
*/
type questGateBook struct {
	mu    sync.Mutex
	next  uint32
	byGID map[uint32]questGate
	// roll is the placement's random source in [0, 1); nil uses
	// math/rand/v2.
	roll func() float64
}

/*
================
SpawnQuestExitGate

Raises the exit gate in a world beside the point a boss fell: 20 to 100
along each horizontal axis, a random heading, the height of the fall.
False when the data lacks the gate or the gid band is full.
================
*/
func (rt *Runtime) SpawnQuestExitGate(world instance.ID, fell simulation.Spawn) (uint32, bool) {
	if rt.portals == nil || rt.portals.questGate == nil {
		return 0, false
	}
	book := &rt.questGates
	book.mu.Lock()
	defer book.mu.Unlock()
	roll := book.roll
	if roll == nil {
		roll = rand.Float64
	}
	if book.byGID == nil {
		book.byGID = map[uint32]questGate{}
	}
	if len(book.byGID) >= questGateGIDSpan {
		return 0, false
	}
	gid := uint32(0)
	for gid == 0 {
		candidate := questGateGIDBase + book.next%questGateGIDSpan
		book.next++
		if _, used := book.byGID[candidate]; !used {
			gid = candidate
		}
	}
	spawn := fell
	spawn.X += roll()*questGateOffsetSpan + questGateOffsetMin
	spawn.Z += roll()*questGateOffsetSpan + questGateOffsetMin
	spawn.Angle = uint16(roll() * 65536)
	book.byGID[gid] = questGate{gid: gid, world: world, spawn: spawn, rows: []uint8{questGateRecallPoint}}
	return gid, true
}

/*
================
RemoveQuestExitGates

Takes down every gate standing in a world (its layer closed).
================
*/
func (rt *Runtime) RemoveQuestExitGates(world instance.ID) {
	book := &rt.questGates
	book.mu.Lock()
	defer book.mu.Unlock()
	for gid, gate := range book.byGID {
		if gate.world == world {
			delete(book.byGID, gid)
		}
	}
}

/*
================
QuestGateNpcs

The gates standing in one world, as the NPC defs the ticker's scope pass
publishes to that world's sessions.
================
*/
func (rt *Runtime) QuestGateNpcs(world uint32) []simulation.NpcDef {
	book := &rt.questGates
	book.mu.Lock()
	defer book.mu.Unlock()
	var out []simulation.NpcDef
	for _, gate := range book.byGID {
		if uint32(gate.world) == world {
			out = append(out, rt.questGateDef(gate))
		}
	}
	return out
}

/*
================
questGateDef

A gate as an NPC def: a teleport-gate object holding service 0x1C.
================
*/
func (rt *Runtime) questGateDef(gate questGate) simulation.NpcDef {
	building := rt.portals.questGate
	return simulation.NpcDef{
		ObjectID:      gate.gid,
		RefObjID:      building.ref,
		TidWord:       building.tid,
		Codename:      questGateCodename,
		NameStrID:     building.nameStrID,
		AuthoredSpawn: true,
		Spawn:         gate.spawn,
		Teleport:      &simulation.TeleportGateBounds{Radius: building.radius, Height: building.height},
		TalkFlags:     simulation.NpcTalkFlagTeleportGate,
		Services:      simulation.NpcServices(0).With(simulation.NpcServiceTeleportGate),
	}
}

/*
================
questGateFor

The gate a gid names, when it stands in the character's own world. The
second result reports a gate gid at all (a gate in another world is
still the gate band's, never another owner's object).
================
*/
func (rt *Runtime) questGateFor(c *enterworld.Character, gid uint32) (questGate, bool, bool) {
	book := &rt.questGates
	book.mu.Lock()
	gate, ok := book.byGID[gid]
	book.mu.Unlock()
	if !ok {
		return questGate{}, false, false
	}
	return gate, uint32(gate.world) == domain.CharacterWorldInstance(c), true
}

/*
================
questGateSelect

Answers a select of a gate: the hit range first, as every select, then
the teleport-gate answer with its rows. Unhandled for any other gid. The
caller holds the division lock.
================
*/
func (rt *Runtime) questGateSelect(divisionID string, c *enterworld.Character, gid uint32) (SelectOutcome, bool) {
	gate, here, isGate := rt.questGateFor(c, gid)
	if !isGate {
		return SelectOutcome{}, false
	}
	if !here || rt.portals == nil || rt.portals.questGate == nil {
		return SelectOutcome{
			Refusal: fmt.Sprintf("gate gid %d stands in another world", gid),
			Frames:  []wire.Frame{{Opcode: wire.OpObjectSelectResult, Payload: wire.EncodeObjectSelectRefusal(selectObjectNotFound)}},
		}, true
	}
	def := rt.questGateDef(gate)
	if !rt.npcWithinHitRange(divisionID, c, def) {
		return SelectOutcome{
			Refusal: fmt.Sprintf("gate gid %d is beyond its interaction range", gid),
			Frames:  []wire.Frame{{Opcode: wire.OpObjectSelectResult, Payload: wire.EncodeObjectSelectRefusal(hitRangeTooFar)}},
		}, true
	}
	rt.Selected.Set(divisionID, c.Name, gid)
	rt.NpcDialogs.Clear(divisionID, c.Name)
	w := wire.NewWriter(11 + len(gate.rows)).U8(1).U32(gid).U32(def.TalkFlags).U8(uint8(len(gate.rows)))
	for _, kind := range gate.rows {
		w.U8(kind)
	}
	return SelectOutcome{Selected: gid, Frames: []wire.Frame{{Opcode: wire.OpObjectSelectResult, Payload: w.Payload()}}}, true
}

/*
================
handleQuestGate

0x7495 type 3 from a selected gate, in the reference server's order: the
shared prologue (a teleport already under way 0x1C14, the hit range, the
teleport service), then case 3 (the quest operation bit 0x1C17, a
summoned transport 0x1C10, the gate's own requirement - the quest gates
carry none - the choice in range, else 2), then the move to the resolved
point, leaving the instance like the layer's own return does.
================
*/
func (rt *Runtime) handleQuestGate(division string, c *enterworld.Character, gid uint32, choice uint8) OpResult {
	if c == nil {
		return portalFailure(2)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return portalFailure(2)
	}
	gate, here, _ := rt.questGateFor(c, gid)
	if !here {
		return portalFailure(2)
	}
	def := rt.questGateDef(gate)
	if c.NativeTeleportMode != 0 {
		return portalFailure(errCodeTeleportState)
	}
	if !rt.npcWithinHitRange(division, c, def) {
		return portalFailure(hitRangeTooFar)
	}
	if rt.QuestTravelBlocks != nil && rt.QuestTravelBlocks(c)&operationMaskQuestTravel != 0 {
		return portalFailure(errCodeGuideQuestBlock)
	}
	if rt.hasSummonedTransportCOS(c) {
		return portalFailure(errCodeGuideTransport)
	}
	if int(choice) >= len(gate.rows) {
		return portalFailure(2)
	}
	// Row kind 0 is the last recall point; the v1.150 quest gates carry
	// no other kind.
	point, refusal := reverseReturnPoint(c, reverseReturnLastRecall)
	if refusal != 0 {
		return portalFailure(errCodeGateMoveFailed)
	}
	if rt.returnCharacterFromInstanceTo(division, c, point, "quest-gate") != instance.Success {
		return portalFailure(errCodeGateMoveFailed)
	}
	return OpResult{}
}
