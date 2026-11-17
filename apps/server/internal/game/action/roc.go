/*
===========================================================================

roc.go - the Roc's summit (INS_ROC_TOP) and Tahomet's room (INS_ROC_FRONT)

Port-only, not native to v1.150 (SRO_ROC, #3399 step 4): the v1.150
client carries both worlds, but the server rules that run them are the
v1.188 reference server's, read from its Binary Ninja database
(research/investigations/roc-arena/NOTES.md has the evidence):

	open      the SCHEDULE_DEF_MEET_ROC job's start opens
	          INS_ROC_TOP and tells the shard UIIT_MSG_QUEST_ROC_APPEAR
	stones    the layer start places the eight seal stones
	break     all stones dead: UIIT_MSG_QUEST_ROC_BREAK_STONE
	          to the layer, the next step 10 minutes later
	entrance  step 1: entry closes, UIIT_MSG_QUEST_ROC_DISAPPEAR
	          to the shard, the Roc 42 seconds later
	roc       step 2: MOB_RM_ROC at region 26045 (1028.9, -7.29, 1769.52)
	kill      the Roc's death: an INS_QUEST_TELEPORT gate where
	          it fell, leading to GATE_RC_MOUNTAIN_GATE's spawn; the layer
	          closes 30 minutes later
	limit     an hour after the open, once the schedule window is over,
	          the layer closes whatever its step
	end       the job's end closes entry and tells the shard
	          UIIT_MSG_QUEST_ROC_DISAPPEAR

The Front (Tahomet's room, world 41) follows its own handlers: Tahomet at
region 25789 (1002.03, -12.72, 943.97) on the layer start;
his death raises a gate to GATE_RC_ROC_FRONT_GATE's spawn and,
60 seconds later, the tick sends whoever stayed there; entry
refuses once he has fallen.

Inferences, as no binary shows them:
- The Front's opener was not found. The port opens it beside the summit,
  for the same window, as one layer: gates land on layer 1
  (portal_world.go) and only a scheduled opener exists to open it.
- A server that boots inside the window opens the summit at once, as the
  schedule job is started by the executor when its row is current.
- The client's notice for these symbols is the 0x36BF talk notice: the
  v1.150 client has no 0x300C string notice (unique-notices.ts), and its
  0x36BF handler banners any text key.
- The Roc's berserk and departure countdowns (UIIT_MSG_QUEST_ROC_BERSERKER
  and _BYEBYE carry a minute count: the summit tick's step 3 and the
  escape shouts) and its timed skills are not ported: no v1.150 frame carries the
  count. The Roc stays until killed or until the layer closes.
- A missing kind or gate row (the variant build did not stage the Roc)
  spawns nothing; the window still opens and closes the worlds.

The Roc tells the whole division when it appears and when it falls
(0x3058 kinds 5 and 6, the v1.150 form of the reference server's
0x300C 0x0C05 and 0x0C06): the reference in-layer spawner asks the
monster factory to announce a unique, and both Roc world classes leave
the notices uncontrolled. So the Roc's spawn asks for the notice and
the two worlds are registered as announcing (AnnounceUniqueWorld).

Gate travellers enter without an instance return anchor, so a close and
the Roc gates move residents to the named gate's spawn themselves
(leaveRocLayer) instead of to an anchor.

Locking: the tick and the admission hold the division lock, which guards
every layer's fields; rocRuntime.mu guards only its maps and the fall
sites the kill hook records from inside a fatal hit's door.

===========================================================================
*/

package action

import (
	"log"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/roc"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// The two Roc worlds (v1150_gameworld.tsv).
	rocTopWorld   = instance.DefinitionID(40) // INS_ROC_TOP
	rocFrontWorld = instance.DefinitionID(41) // INS_ROC_FRONT

	rocCodename        = "MOB_RM_ROC"
	rocTahometCodename = "MOB_RM_TAHOMET_ROC"

	// The gates whose spawns the Roc worlds send people to.
	rocMountainGateCode = "GATE_RC_MOUNTAIN_GATE"
	rocFrontGateCode    = "GATE_RC_ROC_FRONT_GATE"

	// The summit tick's timings: the stones, the entrance, the kill.
	rocBreakStoneMs = 600_000
	rocEntranceMs   = 42_000
	rocAfterKillMs  = 1_800_000
	rocTopLimitMs   = 3_600_000
	// The Front's send-out after Tahomet falls.
	rocFrontSendOutMs = 60_000

	// portalRocEntry is 0x1C30's low byte: the summit entry check
	// refuses a character in job mode or with a vehicle.
	portalRocEntry byte = 0x30

	// rocNoticeOpcode is the talk notice (quest/capture.go's 0x36BF).
	rocNoticeOpcode uint16 = 0x36bf

	rocSymAppear     = "UIIT_MSG_QUEST_ROC_APPEAR"
	rocSymDisappear  = "UIIT_MSG_QUEST_ROC_DISAPPEAR"
	rocSymBreakStone = "UIIT_MSG_QUEST_ROC_BREAK_STONE"
)

// rocSealStones are the eight stones the layer start places: its table reads
// a region then x, y, z per stone, the codenames in the same order.
var rocSealStones = []struct {
	codename string
	spawn    simulation.Spawn
}{
	{"MOB_RM_SEALSTONE", simulation.Spawn{RegionID: 26301, X: 1031.79, Y: 17.90, Z: 165.58}},
	{"MOB_RM_SEALSTONE_2", simulation.Spawn{RegionID: 26045, X: 467.65, Y: 8.04, Z: 1805.65}},
	{"MOB_RM_SEALSTONE_3", simulation.Spawn{RegionID: 26045, X: 171.01, Y: -0.95, Z: 1311.78}},
	{"MOB_RM_SEALSTONE_4", simulation.Spawn{RegionID: 26045, X: 436.02, Y: -4.36, Z: 779.72}},
	{"MOB_RM_SEALSTONE_5", simulation.Spawn{RegionID: 26045, X: 999.71, Y: -4.05, Z: 550.84}},
	{"MOB_RM_SEALSTONE_6", simulation.Spawn{RegionID: 26045, X: 1586.43, Y: -13.06, Z: 728.75}},
	{"MOB_RM_SEALSTONE_7", simulation.Spawn{RegionID: 26045, X: 1582.73, Y: 17.09, Z: 1795.80}},
	{"MOB_RM_SEALSTONE_8", simulation.Spawn{RegionID: 26045, X: 1858.34, Y: 26.25, Z: 1241.77}},
}

var (
	// rocSpawn is where step 2 places the Roc.
	rocSpawn = simulation.Spawn{RegionID: 26045, X: 1028.9, Y: -7.29, Z: 1769.52}
	// rocTahometSpawn is where the Front's start places Tahomet.
	rocTahometSpawn = simulation.Spawn{RegionID: 25789, X: 1002.03, Y: -12.72, Z: 943.97}
)

/*
================
rocStep

The summit's steps, numbered as the summit tick numbers them.
================
*/
type rocStep uint8

const (
	rocStepStones    rocStep = 0
	rocStepEntrance  rocStep = 1
	rocStepRoc       rocStep = 2
	rocStepFight     rocStep = 3
	rocStepAfterKill rocStep = 5
)

/*
================
rocSummit

One division's open summit layer.
================
*/
type rocSummit struct {
	lease       instance.Lease
	openedMs    int64
	step        rocStep
	nextMs      int64
	stones      []uint32
	roc         uint32
	entryClosed bool
}

/*
================
rocFront

One division's open Front layer. fallenMs is zero while Tahomet lives.
================
*/
type rocFront struct {
	lease    instance.Lease
	tahomet  uint32
	fallenMs int64
	sentOut  bool
}

/*
================
rocRuntime

The Roc owner, present only with SRO_ROC on. spawn and alive are the
monster owner's (seams for tests); loc is the schedule's clock zone.
================
*/
type rocRuntime struct {
	mu     sync.Mutex
	loc    *time.Location
	top    map[string]*rocSummit
	front  map[string]*rocFront
	opened map[string]int64 // the window start each division last opened for
	fell   map[uint32]simulation.Spawn

	spawn func(request simulation.QuestMonsterSpawn) (uint32, bool)
	alive func(division string, gid uint32) bool
}

/*
================
ConfigureRoc

Turns the Roc worlds on (SRO_ROC). Off leaves rt.roc nil: no world
opens, no entry rule runs, and every hook returns at once.
================
*/
func (rt *Runtime) ConfigureRoc(enabled bool) {
	if rt.Monsters != nil {
		// The two Roc world classes answer no control over the unique
		// notices, so their layers tell the division of the Roc.
		rt.Monsters.AnnounceUniqueWorld(rocTopWorld, enabled)
		rt.Monsters.AnnounceUniqueWorld(rocFrontWorld, enabled)
	}
	if !enabled || rt.Monsters == nil {
		rt.roc = nil
		return
	}
	rt.roc = &rocRuntime{
		loc:    time.Local,
		top:    map[string]*rocSummit{},
		front:  map[string]*rocFront{},
		opened: map[string]int64{},
		fell:   map[uint32]simulation.Spawn{},
		spawn:  rt.Monsters.SpawnQuestMonsterGID,
		alive:  rt.Monsters.QuestMonsterAlive,
	}
}

/*
================
rocRuntime.layers

A division's open layers; either may be nil.
================
*/
func (r *rocRuntime) layers(division string) (*rocSummit, *rocFront) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.top[division], r.front[division]
}

/*
================
rocRuntime.takeFell

Where gid fell, once, when the kill hook recorded it.
================
*/
func (r *rocRuntime) takeFell(gid uint32) (simulation.Spawn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	site, ok := r.fell[gid]
	delete(r.fell, gid)
	return site, ok
}

/*
================
noteRocKill

The kill hook: records where the Roc or Tahomet fell, for the gate the
next tick raises. Records only; runs inside the fatal hit's door.
================
*/
func (rt *Runtime) noteRocKill(codename string, gid uint32, pose monster.Pose) {
	r := rt.roc
	if r == nil || codename != rocCodename && codename != rocTahometCodename {
		return
	}
	r.mu.Lock()
	r.fell[gid] = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	r.mu.Unlock()
}

/*
================
rocEntry

The Roc worlds' half of a gate's admission (portalWorldAdmission). The
summit refuses a rider or a summoned vehicle (the entry check reads the
character's riding dword at byte +0x1D18, which walk-in ports as
mountedOnCOS, then its active vehicle) and, once entry closed, every
traveller; job mode is no refusal there. The Front refuses once Tahomet
fell (its entry check). The caller holds the division lock.
================
*/
func (rt *Runtime) rocEntry(division string, c *enterworld.Character, world instance.DefinitionID) byte {
	r := rt.roc
	if r == nil || world != rocTopWorld && world != rocFrontWorld {
		return 0
	}
	top, front := r.layers(division)
	if world == rocTopWorld {
		if mountedOnCOS(c) || rt.hasSummonedTransportCOS(c) {
			return portalRocEntry
		}
		if top != nil && top.entryClosed {
			return portalTransferError(instance.MissingLayer)
		}
		return 0
	}
	if front != nil && front.fallenMs != 0 {
		return portalTransferError(instance.MissingLayer)
	}
	return 0
}

/*
================
advanceRoc

The tick step: per division, opens both worlds when the window starts,
runs their steps, and closes them. The shard notices come back as
division frames; the layer notices go to residents directly.
================
*/
func (rt *Runtime) advanceRoc(nowMs int64) []simulation.DivisionFrames {
	r := rt.roc
	if r == nil || rt.Monsters == nil {
		return nil
	}
	start, _, in := roc.SummitWindow().Current(rt.Now(), r.loc)
	var out []simulation.DivisionFrames
	for _, population := range rt.Monsters.OpenWorldPopulations() {
		division := population.Division
		unlock := rt.lockDivision(division)
		var notices []string
		if in && rt.openRoc(division, start.UnixMilli(), nowMs) {
			notices = append(notices, rocSymAppear)
		}
		top, front := r.layers(division)
		if top != nil {
			notices = append(notices, rt.advanceRocTop(division, top, in, nowMs)...)
		}
		if front != nil {
			rt.advanceRocFront(division, front, top == nil, nowMs)
		}
		unlock()
		for _, symbol := range notices {
			out = append(out, simulation.DivisionFrames{DivisionID: division, Frames: []simulation.Frame{rocNotice(symbol)}})
		}
	}
	return out
}

/*
================
openRoc

Opens the summit and the Front once per window. The summit must land on
layer 1, where the gates arrive; any other layer is closed again. True
when the summit opened.
================
*/
func (rt *Runtime) openRoc(division string, windowStart, nowMs int64) bool {
	r := rt.roc
	r.mu.Lock()
	already := r.opened[division] == windowStart || r.top[division] != nil
	r.mu.Unlock()
	if already {
		return false
	}
	lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: division, Definition: rocTopWorld})
	if status != instance.Success {
		log.Printf("roc: division %s: the summit did not open (status %d)", division, status)
		return false
	}
	if lease.ID != instance.Pack(rocTopWorld, portalWorldLayer) {
		log.Printf("roc: division %s: the summit opened on %v, not the gates' layer", division, lease.ID)
		rt.closeInstanceWorld(division, lease)
		return false
	}
	top := &rocSummit{lease: lease, openedMs: nowMs, step: rocStepStones}
	for _, stone := range rocSealStones {
		if gid, ok := rt.spawnRocMonster(division, lease, stone.codename, stone.spawn, nowMs); ok {
			top.stones = append(top.stones, gid)
		}
	}
	var front *rocFront
	if lease, status := rt.OpenInstanceWorld(InstanceOpenRequest{Division: division, Definition: rocFrontWorld}); status == instance.Success {
		if lease.ID == instance.Pack(rocFrontWorld, portalWorldLayer) {
			front = &rocFront{lease: lease}
			front.tahomet, _ = rt.spawnRocMonster(division, lease, rocTahometCodename, rocTahometSpawn, nowMs)
		} else {
			rt.closeInstanceWorld(division, lease)
		}
	}
	r.mu.Lock()
	r.opened[division] = windowStart
	r.top[division] = top
	if front != nil {
		r.front[division] = front
	}
	r.mu.Unlock()
	return true
}

/*
================
spawnRocMonster

One fixed-point monster in a Roc layer. A kind the variant did not
stage spawns nothing.
================
*/
func (rt *Runtime) spawnRocMonster(division string, lease instance.Lease, codename string, at simulation.Spawn, nowMs int64) (uint32, bool) {
	gid, ok := rt.roc.spawn(simulation.QuestMonsterSpawn{Division: division, Population: lease, Codename: codename,
		Position: at, FixedPosition: true, NowMs: nowMs, AnnounceUnique: true})
	if !ok {
		log.Printf("roc: division %s: %s did not spawn", division, codename)
	}
	return gid, ok
}

/*
================
advanceRocTop

One summit layer's step (the summit tick and its death and stone handlers).
Answers the shard notices it raised. The caller holds the division lock.
================
*/
func (rt *Runtime) advanceRocTop(division string, top *rocSummit, in bool, nowMs int64) []string {
	var notices []string
	if !in && !top.entryClosed {
		// The job's end closes entry and says so.
		top.entryClosed = true
		notices = append(notices, rocSymDisappear)
	}
	if !in && nowMs-top.openedMs >= rocTopLimitMs {
		rt.closeRocTop(division, top)
		return notices
	}
	switch top.step {
	case rocStepStones:
		for _, gid := range top.stones {
			if rt.roc.alive(division, gid) {
				return notices
			}
		}
		rt.rocLayerNotice(division, top.lease, rocSymBreakStone)
		top.step, top.nextMs = rocStepEntrance, nowMs+rocBreakStoneMs
	case rocStepEntrance:
		if nowMs < top.nextMs {
			return notices
		}
		if !top.entryClosed {
			top.entryClosed = true
			notices = append(notices, rocSymDisappear)
		}
		top.step, top.nextMs = rocStepRoc, nowMs+rocEntranceMs
	case rocStepRoc:
		if nowMs < top.nextMs {
			return notices
		}
		// Native: a failed spawn dumps; the port waits for the close.
		top.roc, _ = rt.spawnRocMonster(division, top.lease, rocCodename, rocSpawn, nowMs)
		top.step = rocStepFight
	case rocStepFight:
		if top.roc == 0 || rt.roc.alive(division, top.roc) {
			return notices
		}
		fell, known := rt.roc.takeFell(top.roc)
		if !known {
			fell = rocSpawn
		}
		if exit, ok := rt.rocGateSpawn(rocMountainGateCode); ok {
			rt.spawnExitGate(top.lease.ID, fell, &exit)
		}
		top.step, top.nextMs = rocStepAfterKill, nowMs+rocAfterKillMs
	case rocStepAfterKill:
		if nowMs >= top.nextMs {
			rt.closeRocTop(division, top)
		}
	}
	return notices
}

/*
================
advanceRocFront

One Front layer's step (its death handler and tick). It closes with the summit
(topGone). The caller holds the division lock.
================
*/
func (rt *Runtime) advanceRocFront(division string, front *rocFront, topGone bool, nowMs int64) {
	exit, haveExit := rt.rocGateSpawn(rocFrontGateCode)
	if topGone {
		rt.closeRocLayer(division, front.lease, exit, haveExit)
		rt.roc.mu.Lock()
		delete(rt.roc.front, division)
		rt.roc.mu.Unlock()
		return
	}
	if front.fallenMs == 0 {
		if front.tahomet == 0 || rt.roc.alive(division, front.tahomet) {
			return
		}
		fell, known := rt.roc.takeFell(front.tahomet)
		if !known {
			fell = rocTahometSpawn
		}
		if haveExit {
			rt.spawnExitGate(front.lease.ID, fell, &exit)
		}
		front.fallenMs = nowMs
		return
	}
	if !front.sentOut && haveExit && nowMs-front.fallenMs >= rocFrontSendOutMs {
		front.sentOut = true
		for _, name := range rt.instanceResidentNames(division, front.lease) {
			if c := rt.findCharacter(division, name); c != nil {
				rt.leaveRocLayer(division, c, exit, "roc-front-send-out", rt.tickDoors())
			}
		}
	}
}

/*
================
closeRocTop

Ends the summit: residents go to the mountain gate, then the layer
closes. The Front follows on the next pass.
================
*/
func (rt *Runtime) closeRocTop(division string, top *rocSummit) {
	exit, ok := rt.rocGateSpawn(rocMountainGateCode)
	rt.closeRocLayer(division, top.lease, exit, ok)
	rt.roc.mu.Lock()
	delete(rt.roc.top, division)
	rt.roc.mu.Unlock()
}

/*
================
closeRocLayer

Moves every resident to exit (when the gate row exists), takes the
layer's gates down and closes it. A resident who could not be moved
keeps the layer retiring until they leave (closeInstanceWorld).
================
*/
func (rt *Runtime) closeRocLayer(division string, lease instance.Lease, exit travelPoint, haveExit bool) {
	if haveExit {
		for _, name := range rt.instanceResidentNames(division, lease) {
			if c := rt.findCharacter(division, name); c != nil {
				rt.leaveRocLayer(division, c, exit, "roc-close", rt.tickDoors())
			}
		}
	}
	rt.RemoveQuestExitGates(lease.ID)
	rt.closeInstanceWorld(division, lease)
}

/*
================
leaveRocLayer

Moves one character out of a Roc layer to a gate's spawn: through the
instance return when it holds an anchor, else as a plain relocation (a
gate traveller has none).
================
*/
func (rt *Runtime) leaveRocLayer(division string, c *enterworld.Character, to travelPoint, label string, doors characterDoors) bool {
	if c.World != nil && c.World.InstanceReturn != nil {
		return rt.returnCharacterFromInstanceTo(division, c, to, label, doors) == instance.Success
	}
	leaving := instance.ID(domain.CharacterWorldInstance(c))
	if !rt.relocateCharacter(division, c, label, func() (travelPoint, bool) { return to, true }) {
		return false
	}
	rt.instanceEvent(InstanceEventLeave, division, leaving.Definition(), leaving, c, false)
	return true
}

/*
================
rocGateSpawn

A gate's arrival point, by its teleport code, in the world and layer a
gate arrival lands in.
================
*/
func (rt *Runtime) rocGateSpawn(code string) (travelPoint, bool) {
	if rt.portals == nil {
		return travelPoint{}, false
	}
	for _, destination := range rt.portals.destinations {
		if destination.code == code {
			return travelPoint{spawn: destination.spawn, world: portalWorld(destination)}, true
		}
	}
	return travelPoint{}, false
}

/*
================
rocLayerNotice

One notice to every resident of a layer.
================
*/
func (rt *Runtime) rocLayerNotice(division string, lease instance.Lease, symbol string) {
	if rt.PushCharacterFrames == nil {
		return
	}
	notice := rocNotice(symbol)
	for _, name := range rt.instanceResidentNames(division, lease) {
		rt.PushCharacterFrames(division, name, []wire.Frame{{Opcode: notice.Opcode, Payload: notice.Payload}})
	}
}

/*
================
rocNotice

The 0x36BF talk notice: [u16 length][text key].
================
*/
func rocNotice(symbol string) simulation.Frame {
	return simulation.Frame{Opcode: rocNoticeOpcode,
		Payload: wire.NewWriter(len(symbol) + 2).U16(uint16(len(symbol))).Bytes([]byte(symbol)).Payload()}
}
