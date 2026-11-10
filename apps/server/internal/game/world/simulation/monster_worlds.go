/*
===========================================================================

monster_worlds.go - population lifetime and world-scoped monster queries

World leases isolate actors; interest queries use current movement geometry.

===========================================================================
*/
package simulation

import (
	"slices"
	"sort"
	"strings"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const (
	// RefGameWorld +0x20: zero marks a world the shard opens at boot.
	permanentWorldType = 0
	// The first layer a world hands out; zero is its controller.
	firstResidentLayer = 1
	// INS_DEFAULT, the field every character starts in.
	defaultWorldDefinition instance.DefinitionID = 1
)

/*
================
CurrentTimeMillis
================
*/
func (s *MonsterState) CurrentTimeMillis() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nowMillis()
}

// AllocatePopulation binds a fresh timer/actor population to an allocated
// layer. No elapsed callback runs in this transition. All layers share the
// process GID allocator and immutable references, never live nest counters.
/*
================
AllocatePopulation
================
*/
func (s *MonsterState) AllocatePopulation(division string, id instance.ID) (instance.Lease, instance.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
	definition, found := instance.Lookup(id.Definition())
	if !found {
		return instance.Lease{}, instance.InvalidLayer
	}
	lease, status := s.worldAllocators[division].Allocate(id)
	if status != instance.Success {
		return lease, status
	}
	if s.worldPopulations == nil {
		s.worldPopulations = make(map[populationKey]*divisionMonsterState)
	}
	s.worldPopulations[populationKey{division, lease}] = s.createPopulation(lease, definition.CodeName)
	s.popKeysMemo = nil
	return lease, status
}

/*
================
OpenInstanceLayer

The retail server's world-layer open (its layer-slot check, then its layer
allocation) opens the first unallocated layer slot in 1..LayerLimit on an
instance world (NativeType != 0). Permanent worlds (NativeType == 0) and
full worlds refuse.
================
*/
func (s *MonsterState) OpenInstanceLayer(division string, defID instance.DefinitionID) (instance.Lease, bool) {
	definition, ok := instance.Lookup(defID)
	if !ok || definition.NativeType == permanentWorldType {
		return instance.Lease{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
	allocator := s.worldAllocators[division]
	for layer := uint16(firstResidentLayer); layer <= definition.LayerLimit; layer++ {
		id := instance.Pack(defID, layer)
		if _, active := allocator.Lookup(id); active {
			continue
		}
		lease, status := allocator.Allocate(id)
		if status != instance.Success {
			return instance.Lease{}, false
		}
		if s.worldPopulations == nil {
			s.worldPopulations = make(map[populationKey]*divisionMonsterState)
		}
		s.worldPopulations[populationKey{division, lease}] = s.createPopulation(lease, definition.CodeName)
		s.popKeysMemo = nil
		return lease, true
	}
	return instance.Lease{}, false
}

/*
================
FirstNestSpawnByCodename

The reference server's NPC-position-by-codename fallback: the first
authored nest anchor for a
reference codename and the world code its hive names.
================
*/
func (s *MonsterState) FirstNestSpawnByCodename(codename string) (string, Spawn, bool) {
	if codename == "" {
		return "", Spawn{}, false
	}
	for _, nest := range s.template.Nests {
		ref, ok := s.template.Refs[nest.RefObjID]
		if !ok || !strings.EqualFold(ref.Codename, codename) {
			continue
		}
		return nest.WorldCode, Spawn{
			RegionID: nest.RegionID,
			X:        nest.X,
			Y:        nest.Y,
			Z:        nest.Z,
		}, true
	}
	return "", Spawn{}, false
}

// ReleasePopulation invalidates the lease and all of its timers, live GIDs,
// contribution ledgers, summon reservations and burn sources in one owner
// transaction. A callback retaining the old lease cannot remove a replacement.
/*
================
ReleasePopulation
================
*/
func (s *MonsterState) ReleasePopulation(division string, lease instance.Lease) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := populationKey{division, lease}
	state := s.worldPopulations[key]
	if state == nil {
		return false
	}
	if !s.worldAllocators[division].Release(lease) {
		panic("population lost its allocation owner")
	}
	if state := s.worldPopulations[key]; state != nil {
		state.instances.release()
	}
	delete(s.worldPopulations, key)
	delete(s.retirementRequests, key)
	s.popKeysMemo = nil
	return true
}

/*
================
populationForLease
================
*/
func (s *MonsterState) populationForLease(division string, lease instance.Lease) *divisionMonsterState {
	if state := s.divisionState(division); state != nil && state.lease == lease {
		return state
	}
	return s.worldPopulations[populationKey{division, lease}]
}

// populationForObject follows a native GID lookup back to its population.
// Unknown GIDs return an empty lookup state; queries never allocate a world.
// Callers resolve one population's actors back to back, so the population
// the last scan answered with is remembered (popHint) and asked first. The
// hint is believed only while the registry still maps its key to that very
// population and the population's rows still hold the gid - a retired
// population's storage can outlive its registration and still answer
// contains, which the scan could never return again - and any miss falls
// through to the scan, so the hint can only ever repeat the scan's own
// answer (#2029). Callers hold s.mu, as the maps above already require.
/*
================
populationForObject
================
*/
func (s *MonsterState) populationForObject(division string, gid uint32) *divisionMonsterState {
	return s.populationForObjectAt(division, gid, behaviorNoMonsterSlot)
}

/*
================
populationForObjectAt

populationForObject through the monster slot the caller's step
carried (#2133): every candidate's membership test goes through
containsAt, which answers from the slot when it still names the
gid and delegates to the gid probe otherwise, so the candidate
order - and therefore the answer, popHint side effects included -
is populationForObject's either way. The step's membership tests
(moverStuckAt, the AI timer check) run per actor per leg; their
gid probes were the store map walk this removes.
================
*/
func (s *MonsterState) populationForObjectAt(division string, gid uint32, monsterSlot uint32) *divisionMonsterState {
	if state := s.divisionState(division); state != nil {
		if state.instances.containsAt(monsterSlot, gid) {
			return state
		}
	}
	if s.popHintState != nil {
		if s.popHintKey.division == division && s.worldPopulations[s.popHintKey] == s.popHintState &&
			s.popHintState.instances.containsAt(monsterSlot, gid) {
			return s.popHintState
		}
		s.popHintState = nil
	}
	for key, state := range s.worldPopulations {
		if key.division == division {
			if state.instances.containsAt(monsterSlot, gid) {
				s.popHintKey, s.popHintState = key, state
				return state
			}
		}
	}
	return &divisionMonsterState{}
}

/*
================
locateObject

populationForObject that also hands back the residency it found, so the
caller hydrates the actor from that slot instead of probing the population
a second time. The candidates and their order are populationForObject's,
so the same population answers; an unknown gid returns nil and allocates
nothing.
================
*/
func (s *MonsterState) locateObject(division string, gid uint32) (*divisionMonsterState, monsterSlot) {
	if state := s.divisionState(division); state != nil {
		if slot := state.instances.locate(gid); slot.residency != slotMissing {
			return state, slot
		}
	}
	for key, state := range s.worldPopulations {
		if key.division != division {
			continue
		}
		if slot := state.instances.locate(gid); slot.residency != slotMissing {
			return state, slot
		}
	}
	return nil, monsterSlot{}
}

/*
================
PopulationLease
================
*/
func (s *MonsterState) PopulationLease(division string, id instance.ID) (instance.Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	allocator := s.worldAllocators[division]
	if allocator == nil {
		return instance.Lease{}, false
	}
	return allocator.Lookup(id)
}

/*
================
CheckPopulationTransfer

The retail server's transfer check: the non-mutating door a teleport asks
before it moves a PC into another world. Nothing is reserved.
================
*/
/*
================
CheckPopulationTransfer
================
*/
func (s *MonsterState) CheckPopulationTransfer(division string, destination instance.ID, capacityBypass bool) instance.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
	return s.worldAllocators[division].CheckTransfer(destination, capacityBypass)
}

/*
================
AdmitPopulationPC
================
*/
func (s *MonsterState) AdmitPopulationPC(division string, lease instance.Lease, gid uint32, capacityBypass bool) instance.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return instance.InvalidLayer
	}
	return s.worldAllocators[division].AdmitPC(lease, gid, capacityBypass)
}

/*
================
LeavePopulationPC
================
*/
func (s *MonsterState) LeavePopulationPC(division string, lease instance.Lease, gid uint32) (instance.Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return instance.MissingLayer, false
	}
	status, request := s.worldAllocators[division].LeavePC(lease, gid)
	if request {
		s.requestPopulationRetirement(division, lease)
	}
	return status, request
}

/*
================
BeginPopulationRetirement
================
*/
func (s *MonsterState) BeginPopulationRetirement(division string, lease instance.Lease) ([]uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populationForLease(division, lease) == nil {
		return nil, false
	}
	residents, exists := s.worldAllocators[division].BeginRetirement(lease)
	if exists && len(residents) == 0 {
		s.requestPopulationRetirement(division, lease)
	}
	return residents, exists
}

/*
================
requestPopulationRetirement
================
*/
func (s *MonsterState) requestPopulationRetirement(division string, lease instance.Lease) {
	if s.retirementRequests == nil {
		s.retirementRequests = make(map[populationKey]struct{})
	}
	s.retirementRequests[populationKey{division, lease}] = struct{}{}
}

// PendingPopulationRetirements is the manager's native 7C14 release-request
// inbox. Reading does not acknowledge it: only releasing that exact lifetime
// removes the request, so a disconnected caller cannot lose the last leave.
/*
================
PendingPopulationRetirements
================
*/
func (s *MonsterState) PendingPopulationRetirements(division string) []instance.Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []instance.Lease
	for key := range s.retirementRequests {
		if key.division == division {
			out = append(out, key.lease)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Generation < out[j].Generation })
	return out
}

// PopulationInstances is a read: allocation, callbacks and random draws are
// separate operations. The exact lease is required, including its generation.
/*
================
PopulationInstances
================
*/
func (s *MonsterState) PopulationInstances(division string, lease instance.Lease, regions []uint16) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil
	}
	var out []monster.Instance
	seen := make(map[uint16]bool, len(regions))
	for _, region := range regions {
		if seen[region] {
			continue
		}
		seen[region] = true
		for _, gid := range state.byRegion[region] {
			out = append(out, state.instances.get(gid))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gid < out[j].Gid })
	return out
}

// PopulationInterestInstances projects one allocated lifetime under the owner
// lock. Release, pose changes and replacement allocation cannot interleave
// between candidate collection and visibility filtering.
/*
================
PopulationInterestInstances
================
*/
func (s *MonsterState) PopulationInterestInstances(division string, lease instance.Lease, viewer worldgeom.RegionXZ, nowMs int64) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil
	}
	gids := populationInterestGIDs(state, viewer, nowMs)
	out := make([]monster.Instance, 0, len(gids))
	for _, gid := range gids {
		out = append(out, state.instances.get(gid))
	}
	return out
}

/*
================
interestMemoStamp

What a memoized viewer answer depends on.
================
*/
type interestMemoStamp struct {
	blocks    uint64
	instances uint64
	statics   interestStaticsStamp
}

/*
================
interestStaticsStamp

movers is compared too: a population can be handed a fresh storage whose
counters restart.
================
*/
type interestStaticsStamp struct {
	movers   *moverStorage
	roster   uint64
	byRegion uint64
}

/*
================
interestStaticsNow
================
*/
func (state *divisionMonsterState) interestStaticsNow() interestStaticsStamp {
	stamp := interestStaticsStamp{movers: state.movers, byRegion: state.byRegionGeneration}
	if state.movers != nil {
		stamp.roster = state.movers.blocks.rosterGeneration
	}
	return stamp
}

/*
================
interestStampAt

The stamp a viewer answer for block keys on: the generations of the
blocks the answer reads (moverBlockIndex.stampBlocks), the instance
view generation and the statics stamp. The caller reconciles the
block index at the read instant first (populationInterestGIDs does;
so does the scope stamp read).
================
*/
func (state *divisionMonsterState) interestStampAt(block worldgeom.InterestBlock) interestMemoStamp {
	stamp := interestMemoStamp{instances: state.instances.viewGeneration, statics: state.interestStaticsNow()}
	if state.movers != nil {
		stamp.blocks = state.movers.blocks.stampBlocks(block)
	}
	return stamp
}

/*
================
scopeInterestStamp

The interest stamp a scope leg for this viewer keys on, sampled under
the population lock after reconciling the block index at the leg
instant - the same inputs populationInterestGIDs memoizes on, so a
leg whose block and stamp both match its last full pass provably
faces the same in-scope set (#2134).
================
*/
func (s *MonsterState) scopeInterestStamp(division string, lease instance.Lease, viewer worldgeom.RegionXZ, nowMs int64) (interestMemoStamp, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return interestMemoStamp{}, false
	}
	if state.movers != nil {
		state.movers.reconcileBlocks(nowMs)
	}
	return state.interestStampAt(worldgeom.InterestBlockAt(viewer)), true
}

/*
================
scopePopulations

One leg phase's population resolutions under a single lock hold:
each division/lease resolves once, each state reconciles once at
the phase instant, and consecutive sessions in the same
population skip the map entirely (the snapshot walks players in
place order).
================
*/
type scopePopulations struct {
	states     map[populationKey]*divisionMonsterState
	reconciled map[*divisionMonsterState]bool
	lastKey    populationKey
	lastState  *divisionMonsterState
	haveLast   bool
}

/*
================
scopeStampKey

One fold of the stamp cache: the population state and the block
whose reconciled generations the stamp summarizes. A field of
MonsterState keeps the folded stamps between phases (#3027), so
the key lives at package level.
================
*/
type scopeStampKey struct {
	state *divisionMonsterState
	block worldgeom.InterestBlock
}

/*
================
scopePopulations.resolve

The session's division state, reconciled at nowMs on first sight
this phase; nil when the population does not resolve.
================
*/
func (c *scopePopulations) resolve(s *MonsterState, session *SessionSnapshot, nowMs int64) *divisionMonsterState {
	key := populationKey{session.DivisionID, session.Population}
	var state *divisionMonsterState
	if c.haveLast && key == c.lastKey {
		state = c.lastState
	} else {
		if c.states == nil {
			c.states = make(map[populationKey]*divisionMonsterState)
		}
		resolved, cached := c.states[key]
		if cached {
			state = resolved
		} else {
			state = s.populationForLease(session.DivisionID, session.Population)
			c.states[key] = state
		}
		c.lastKey, c.lastState, c.haveLast = key, state, true
	}
	if state != nil && state.movers != nil && !c.reconciled[state] {
		if c.reconciled == nil {
			c.reconciled = make(map[*divisionMonsterState]bool)
		}
		state.movers.reconcileBlocks(nowMs)
		c.reconciled[state] = true
	}
	return state
}

/*
================
MonsterState.fillScopeStamps

The scope leg's stamps, sampled together under one lock hold
into the viewers' dense slots (#2988, #2994). The reconcile is
idempotent per instant and the generations it advances are the
stamps' whole input, so one hold at leg start yields exactly the
stamp each viewer would have sampled on its own turn, and the
leg's answers all key on one instant the way the batch snapshot
keys the monster leg's. Viewers share blocks (a town crowd
stands in a handful) and the stamp is a pure function of the
reconciled generations, so each distinct block folds once per
population per leg. A population that does not resolve leaves
stampOK false, the answer scopeInterestStamp gives for it.
Commits landing mid-leg publish next leg - indistinguishable
from landing just after it.
================
*/
func (s *MonsterState) fillScopeStamps(slots []scopeViewerSlot, sessions []SessionSnapshot, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The phase caches are kept on the state and cleared here,
	// under the same hold that uses them (#3027): they answer
	// exactly this phase, so nothing resolved or folded for one
	// phase may leak into the next — the clear keeps a released
	// population's state pointer out of every later answer.
	populations := &s.scopePhase
	clear(populations.states)
	clear(populations.reconciled)
	populations.lastKey, populations.lastState, populations.haveLast = populationKey{}, nil, false
	// Made on first use: consecutive viewers usually share the
	// last fold, and a phase with one block never touches the map.
	folded := s.scopeFolded
	if folded == nil {
		folded = make(map[scopeStampKey]interestMemoStamp)
		s.scopeFolded = folded
	} else {
		clear(folded)
	}
	lastFold := scopeStampKey{}
	var lastStamp interestMemoStamp
	for i := range slots {
		slot := &slots[i]
		state := populations.resolve(s, &sessions[i], nowMs)
		if state == nil {
			continue
		}
		foldKey := scopeStampKey{state, slot.block}
		var stamp interestMemoStamp
		if foldKey == lastFold {
			stamp = lastStamp
		} else if cached, okFold := folded[foldKey]; okFold {
			stamp = cached
		} else {
			stamp = state.interestStampAt(slot.block)
			folded[foldKey] = stamp
		}
		lastFold, lastStamp = foldKey, stamp
		slot.stamp = stamp
		slot.stampOK = true
	}
}

/*
================
regionStatics

The entries of byRegion[region] that have no mover. Most generated actors
have a mover once their population is active, so this keeps a scan from
probing each of them per viewer. A duplicate listing stays duplicated,
as byRegion lists it.
================
*/
func (state *divisionMonsterState) regionStatics(region uint16) []uint32 {
	now := state.interestStaticsNow()
	if state.interestStatics == nil || state.interestStaticsStamp != now {
		state.interestStatics = make(map[uint16][]uint32, len(state.byRegion))
		state.interestStaticsStamp = now
		for key, gids := range state.byRegion {
			for _, gid := range gids {
				if !state.movers.has(gid) {
					state.interestStatics[key] = append(state.interestStatics[key], gid)
				}
			}
		}
	}
	return state.interestStatics[region]
}

/*
================
interestMemoEntry

One block's memoized viewer answer: the gid set as last computed, the
stamp it was computed under, and how it changed from the answer it
replaced. A scope leg whose viewer still holds the replaced answer
applies entered/left instead of re-reading the whole set (#2134).
Only one replacement is remembered; a viewer older than that runs
the full pass, which is always correct.
================
*/
type interestMemoEntry struct {
	gids  []uint32
	stamp interestMemoStamp
	// fromStamp is the stamp gids' predecessor was computed under;
	// entered and left bridge exactly fromStamp -> stamp.
	fromStamp interestMemoStamp
	entered   []uint32
	left      []uint32
}

/*
================
interestGidDifference

The entered and left halves of the symmetric difference of two
gid-sorted answers.
================
*/
func interestGidDifference(before, after []uint32) (entered, left []uint32) {
	i, j := 0, 0
	for i < len(before) && j < len(after) {
		switch {
		case before[i] == after[j]:
			i++
			j++
		case before[i] < after[j]:
			left = append(left, before[i])
			i++
		default:
			entered = append(entered, after[j])
			j++
		}
	}
	left = append(left, before[i:]...)
	entered = append(entered, after[j:]...)
	return entered, left
}

/*
================
populationInterestGIDs

The spawn index owns nest/lifecycle queries. Visibility instead asks the
mover block index: a pursuing actor can leave its spawn region ring.
Filter exact live blocks before any archived actor read (the retail
server's neighbour-block resolution).
Caller holds MonsterState.mu.

Static entries still use their immutable spawn position. The returned
slice is the memo's own once stored: callers read it, never mutate it
(#2134 slice 2a - the per-leg clone cost a copy per viewer per leg and
nobody consumed the isolation).

The memo is per block and rewritten in place: an entry lives until one
of its own blocks' generations moves, so churn in a far neighbourhood
never retires it (#2134). Entries are never pruned; the blocks viewers
stand in bound the count, like the per-session shown sets.
================
*/
func populationInterestGIDs(state *divisionMonsterState, viewer worldgeom.RegionXZ, nowMs int64) []uint32 {
	if state.movers == nil {
		return appendInterestStatics(state, viewer, nil)
	}
	block := worldgeom.InterestBlockAt(viewer)
	state.movers.reconcileBlocks(nowMs)
	return populationInterestGIDsFolded(state, viewer, block, state.interestStampAt(block))
}

/*
================
populationInterestGIDsFolded

populationInterestGIDs with the reconcile done and the stamp
already folded by the caller: the scope leg samples both once per
leg (#2988) instead of once per viewer per query. The stamp is a
pure function of the reconciled generations and the memo keys on
its value, so the answers are the same.
================
*/
func populationInterestGIDsFolded(state *divisionMonsterState, viewer worldgeom.RegionXZ, block worldgeom.InterestBlock, stamp interestMemoStamp) []uint32 {
	if state.movers == nil {
		return appendInterestStatics(state, viewer, nil)
	}
	if state.interestMemo == nil {
		state.interestMemo = make(map[worldgeom.InterestBlock]*interestMemoEntry)
	}
	entry, ok := state.interestMemo[block]
	if ok && entry.stamp == stamp {
		return entry.gids
	}
	members := state.movers.blockMembers(block, nil)
	out := members[:0]
	for _, gid := range members {
		if state.instances.contains(gid) {
			out = append(out, gid)
		}
	}
	out = appendInterestStatics(state, viewer, out)
	if !ok {
		entry = &interestMemoEntry{}
		state.interestMemo[block] = entry
	}
	entry.entered, entry.left = interestGidDifference(entry.gids, out)
	entry.fromStamp, entry.gids, entry.stamp = entry.stamp, out, stamp
	return out
}

/*
================
MonsterState.resolveScopeDeltas

The scope leg's fast path (#2134): the viewer's shown set still equals
the block answer computed under base, and the memo remembers how that
answer changed, so the leg applies only the difference. entered is
filtered to gids the viewer does not show, with their snapshots; left
to gids it does. total is the current answer's size, for the scope
budget read. A viewer the memo cannot bridge from its memo stamp
keeps bridged=false - the leg then runs its full pass. The stamps
ride the leg's leg-start slots (#2988): the memo keys on the stamp
value, so answering from it is the answer a fresh reconcile and
fold would give, and the answers are the ones the per-viewer
populationInterestBlockDelta computed before #2994 - same memo
reads, same entered/left filters, one hold instead of one per
viewer.
================
*/
func (s *MonsterState) resolveScopeDeltas(sessions []SessionSnapshot, slots []scopeViewerSlot, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var populations scopePopulations
	// Consecutive viewers usually stand in the same block of the
	// same population, where the stamp - and so the memo entry -
	// is the same one the last viewer resolved.
	var lastState *divisionMonsterState
	var lastBlock worldgeom.InterestBlock
	var lastEntry *interestMemoEntry
	for i := range slots {
		slot := &slots[i]
		// An unchanged viewer (stamp == memo stamp) can never
		// bridge: the memo entry's fromStamp is the stamp its last
		// recompute replaced, never the stamp it now holds. The
		// leg skips it outright, so resolving it would be waste.
		if slot.state == nil || !slot.stampOK || !slot.sameScope || slot.stamp == slot.state.memo.stamp {
			continue
		}
		state := populations.resolve(s, &sessions[i], nowMs)
		if state == nil || state.movers == nil {
			continue
		}
		var entry *interestMemoEntry
		if state == lastState && slot.block == lastBlock && lastEntry != nil {
			entry = lastEntry
		} else {
			populationInterestGIDsFolded(state, slot.viewer, slot.block, slot.stamp)
			entry = state.interestMemo[slot.block]
			lastState, lastBlock, lastEntry = state, slot.block, entry
		}
		if entry == nil || entry.fromStamp != slot.state.memo.stamp {
			continue
		}
		for _, gid := range entry.entered {
			if slot.shown[gid] {
				continue
			}
			slot.entered = append(slot.entered, gid)
			if slot.snapshots == nil {
				slot.snapshots = make(map[uint32]monster.Instance, len(entry.entered))
			}
			slot.snapshots[gid] = state.instances.get(gid)
		}
		for _, gid := range entry.left {
			if slot.shown[gid] {
				slot.left = append(slot.left, gid)
			}
		}
		slot.total = len(entry.gids)
		slot.bridged = true
	}
}

/*
================
appendInterestStatics

Add the viewer's visible static entries and sort the result by gid.
================
*/
func appendInterestStatics(state *divisionMonsterState, viewer worldgeom.RegionXZ, out []uint32) []uint32 {
	for _, region := range RegionScopeRing(viewer.RegionID) {
		for _, gid := range state.regionStatics(region) {
			actor := state.instances.get(gid)
			position := worldgeom.RegionXZ{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z}
			if worldgeom.InterestVisible(viewer, position) {
				out = append(out, gid)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Visibility is a delta stream. Copy full actors only for new/unseeded viewers,
// not every already-published monster on every viewer tick. Both the identity
// set and required snapshots are captured under the same population lock.
/*
================
populationInterestDelta
================
*/
func (s *MonsterState) populationInterestDelta(division string, lease instance.Lease, viewer worldgeom.RegionXZ, nowMs int64, known map[uint32]bool) ([]uint32, map[uint32]monster.Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil, nil
	}
	return populationInterestDeltaOf(state, populationInterestGIDs(state, viewer, nowMs), known)
}

/*
================
MonsterState.populationInterestDeltaSampled

The scope leg's full pass (#2988): the populationInterestDelta
answer keyed on the leg's leg-start sample instead of a fresh
reconcile and fold per viewer.
================
*/
func (s *MonsterState) populationInterestDeltaSampled(division string, lease instance.Lease, viewer worldgeom.RegionXZ, block worldgeom.InterestBlock, stamp interestMemoStamp, known map[uint32]bool) ([]uint32, map[uint32]monster.Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return nil, nil
	}
	return populationInterestDeltaOf(state, populationInterestGIDsFolded(state, viewer, block, stamp), known)
}

/*
================
populationInterestDeltaOf

The gids among gids the viewer does not know yet, with snapshots.
================
*/
func populationInterestDeltaOf(state *divisionMonsterState, gids []uint32, known map[uint32]bool) ([]uint32, map[uint32]monster.Instance) {
	var snapshots map[uint32]monster.Instance
	for _, gid := range gids {
		if known[gid] {
			continue
		}
		if snapshots == nil {
			snapshots = make(map[uint32]monster.Instance)
		}
		snapshots[gid] = state.instances.get(gid)
	}
	return gids, snapshots
}

/*
================
ObjectPopulation
================
*/
func (s *MonsterState) ObjectPopulation(division string, gid uint32) (instance.Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	return state.lease, state.instances.contains(gid)
}

// GetInWorld is the actor-facing lookup. A GID remains globally resolvable
// for internal callbacks, but a resident cannot use it across a world boundary.
/*
================
GetInWorld
================
*/
func (s *MonsterState) GetInWorld(division string, world instance.ID, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, slot := s.locateObject(division, gid)
	if state == nil || state.lease.ID != world {
		return monster.Instance{}, false
	}
	actor := state.instances.slotValue(gid, &slot)
	if clearExpiredSummonAction(&actor, s.nowMillis()) {
		state.instances.set(gid, actor)
	}
	return actor, true
}

/*
================
GetInPopulation
================
*/
func (s *MonsterState) GetInPopulation(division string, lease instance.Lease, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(division, lease)
	if state == nil {
		return monster.Instance{}, false
	}
	actor, exists := state.instances.lookup(gid)
	if !exists {
		return actor, false
	}
	if clearExpiredSummonAction(&actor, s.nowMillis()) {
		state.instances.set(gid, actor)
	}
	return actor, true
}
