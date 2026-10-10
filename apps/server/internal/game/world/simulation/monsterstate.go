/*
===========================================================================

monsterstate.go - the monster population owner

===========================================================================
*/

package simulation

import (
	"math/rand"
	"sort"
	"sync"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
divisionMonsterState
================
*/
type divisionMonsterState struct {
	archiveQueue    []uint32
	archiveQueued   map[uint32]struct{}
	dormant         map[uint32]uint16
	dormantRegions  map[uint16]map[uint32]struct{}
	keepAwakeUntil  map[uint16]int64
	approachActors  map[uint32]uint32
	approachTargets map[uint32]*monsterApproachSquad
	behavior        behaviorQueue
	lease           instance.Lease
	lastDensityMs   int64
	// Boot fill bookkeeping (PopulationSettled). spawns counts placed nest
	// spawns; fillPending holds every materialized nest outside a hive and
	// every hive until its first callback visit that places nothing.
	populationTicked bool
	spawns           uint64
	fillPending      map[populationFillKey]struct{}
	contributions    map[uint32]map[uint32]uint32 // victim -> credited actor -> damage argument
	abnormalActive   map[uint32]struct{}          // active-only abnormal tick index; blocks live on instances
	aiEvents         map[uint32][]monsterAIEvent  // fear/confusion tactics events awaiting the behavior step
	pendingSummons   map[uint32]pendingMonsterSummon
	instances        monsterStorage
	uniqueNotices    []Frame
	uniqueDeaths     map[uint32]bool
	// uniqueTimes is the public tracker's spawn and death record
	// (unique_states.go, port-only).
	uniqueTimes map[uint32]*uniqueTimeline
	// lifetimes holds the CGObjMob tick timers (monsterlifetime.go).
	lifetimes map[uint32]monsterLifetime
	// byRegion indexes generated spawn regions for nest/lifecycle queries
	// and non-mover interest fallback. Movers own their live spatial index.
	byRegion map[uint16][]uint32
	// materialized indexes all regions whose nest timers belong to this world.
	materialized map[uint16]bool
	// movers holds per-instance movement state and is seeded at activation.
	movers         *moverStorage
	aiTimers       map[uint32]*monster.AITimeManager
	storedAITimers map[uint32]monster.StoredAITimers
	sparseMapPeaks [3]int
	// nests is the per-world CAIHive nest timer table (+1C, 0x20-byte
	// entries) plus CNest +30, keyed by catalog nest index. Every template
	// nest has an entry; lifecycle is independent of transient entity gids.
	nests    map[int]*nestRuntime
	gidNests map[uint32]int
	// hives owns shared callback phases; overwrite live counts are maintained
	// incrementally rather than recomputed by scanning monsters.
	hives map[string]*hiveRuntime
	// due schedules the next hive callback of every group with work pending.
	due spawnQueue
}

/*
==================
MonsterState

MonsterState is the sole mutable authority for division-keyed monster
populations. The monster package supplies immutable catalogs and pure
behavior policy; bootstrap, action, and tick lanes receive value snapshots
and commit changes only through this state boundary.
==================
*/
type MonsterState struct {
	// abnormalContext resolves casters, rolls and parameters for the
	// abnormal-state engine; see MonsterAbnormalContext.
	abnormalContext MonsterAbnormalContext
	archive         *monsterArchive
	regionDormancy  bool
	// Installed before listeners. Called outside the population mutex so
	// character snapshots cannot invert the character -> population lock order.
	players              func(string, int64) []PopulationPlayer
	mu                   sync.Mutex
	template             monster.Template
	counter              uint32
	divs                 map[string]*divisionMonsterState
	worldAllocators      map[string]*instance.Registry
	worldPopulations     map[populationKey]*divisionMonsterState
	retirementRequests   map[populationKey]struct{}
	ground               MonsterSpawnGroundResolver
	collide              MonsterSpawnCollisionTest
	spawnRegionAvailable func(uint16) bool
	random               func() float64
	clock                func() time.Time
	// objectLists holds bootstrap object-list gids until the first scope tick.
	objectLists map[monsterObjectListKey][]uint32
	// bandits is built once from the immutable template (BanditTables).
	banditsOnce sync.Once
	bandits     *monster.BanditTables
}

/*
==================
MonsterSpawnGroundResolver

MonsterSpawnGroundResolver resolves one candidate population point against
the world surface nearest the authored Y. The authored Y is required because
Silkroad can stack terrain and object-nav decks at the same XZ.
==================
*/
type MonsterSpawnGroundResolver func(regionID uint16, x, authoredY, z float64) (float64, bool)

/*
==================
MonsterSpawnCollisionTest

MonsterSpawnCollisionTest is the navmesh move test creation 5F6EB0 runs
from the nest centre to a generated candidate (region manager vtable +0x30).
Result carries the native bits (monster.NavResultClipped / NavResultBlocked;
zero admits the candidate). Rest is where the walk came to rest, on the
surface it walked: 5F6EB0 creates the monster at that written position.
==================
*/
type MonsterSpawnCollisionTest func(from, to Spawn) MonsterSpawnMove

/*
==================
MonsterSpawnMove
==================
*/
type MonsterSpawnMove struct {
	Result uint32
	Rest   Spawn
}

// NewMonsterState builds empty authoritative state over an immutable catalog.
/*
================
NewMonsterState
================
*/
func NewMonsterState(template monster.Template) *MonsterState {
	return &MonsterState{
		template: template,
		divs:     make(map[string]*divisionMonsterState),
		random:   rand.Float64,
		clock:    time.Now,
	}
}

/*
==================
SetSpawnGroundResolver

SetSpawnGroundResolver installs the authoritative world-surface sampler
used for every population candidate. Wiring calls this before listeners
start.
==================
*/
func (s *MonsterState) SetSpawnGroundResolver(resolver MonsterSpawnGroundResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ground = resolver
}

/*
==================
SetSpawnCollisionTest

SetSpawnCollisionTest installs the navmesh move test every generated
population candidate must pass. Wiring calls this before listeners start;
without one, candidates are admitted untested.
==================
*/
func (s *MonsterState) SetSpawnCollisionTest(test MonsterSpawnCollisionTest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.collide = test
}

/*
================
SetSpawnRegionAvailability

Installed before listeners start, alongside collision and height admission.
================
*/
func (s *MonsterState) SetSpawnRegionAvailability(available func(uint16) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawnRegionAvailable = available
}

// TemplateSize reports how many monster nest rows the catalog carries.
/*
================
int
================
*/
func (s *MonsterState) TemplateSize() int { return len(s.template.Nests) }

// SpawnableRefs exposes the catalog's spawnable reference roster.
/*
================
SpawnableRefs
================
*/
func (s *MonsterState) SpawnableRefs() []monster.MonsterRef {
	return s.template.SpawnableRefs()
}

/*
================
CreatableRefs
================
*/
func (s *MonsterState) CreatableRefs() []monster.MonsterRef {
	return s.template.CreatableRefs()
}

// Reference reads the immutable catalog, including rows without a nest.
/*
================
Reference
================
*/
func (s *MonsterState) Reference(id uint32) (monster.MonsterRef, bool) {
	ref, ok := s.template.Refs[id]
	return ref, ok
}

// ReferenceByCodename reads only immutable references, including quest-only
// actors without a world nest. It never touches active or archived actors.
/*
================
ReferenceByCodename
================
*/
func (s *MonsterState) ReferenceByCodename(code string) (monster.MonsterRef, bool) {
	var found monster.MonsterRef
	for _, ref := range s.template.Refs {
		if ref.Codename != code {
			continue
		}
		if found.RefObjID != 0 {
			return monster.MonsterRef{}, false
		}
		found = ref
	}
	return found, found.RefObjID != 0
}

/*
================
division

Opens a division the way the shard boots. SR_ShardManager 65BD60 runs
_GameWorldInitialize, then walks every RefGameWorld and opens the first
layer (65C200 -> world vtable +0x18) of each record whose type byte (+0x20)
is zero; the GameServer builds that layer when the job arrives (5F7BD0 ->
CGameWorld_AllocateLayer). INS_DEFAULT and the INS_FORT_* siege worlds are
the shipped type-0 rows. One process plays both roles here, so the
division owner opens them directly, in authored row order.
================
*/
func (s *MonsterState) division(divisionID string) *divisionMonsterState {
	if state, ok := s.divs[divisionID]; ok {
		return state
	}
	if s.worldAllocators == nil {
		s.worldAllocators = make(map[string]*instance.Registry)
	}
	if s.worldPopulations == nil {
		s.worldPopulations = make(map[populationKey]*divisionMonsterState)
	}
	definitions := instance.Shipped()
	allocator := instance.NewRegistry(definitions)
	s.worldAllocators[divisionID] = allocator
	var state *divisionMonsterState
	for _, definition := range definitions {
		if definition.NativeType != permanentWorldType {
			continue
		}
		lease, status := allocator.Allocate(instance.Pack(definition.ID, firstResidentLayer))
		if status != instance.Success {
			panic("permanent world allocation failed: " + definition.CodeName)
		}
		population := s.createPopulation(lease, definition.CodeName)
		if definition.ID == defaultWorldDefinition {
			state = population
			continue
		}
		s.worldPopulations[populationKey{divisionID, lease}] = population
	}
	if state == nil {
		panic("catalog has no default world")
	}
	s.divs[divisionID] = state
	return state
}

/*
================
createPopulation
================
*/
func (s *MonsterState) createPopulation(lease instance.Lease, worldCode string) *divisionMonsterState {
	state := &divisionMonsterState{
		lease:         lease,
		lastDensityMs: s.nowMillis(),
		instances:     newMonsterStorage(nil),
		byRegion:      make(map[uint16][]uint32),
		materialized:  make(map[uint16]bool),
		nests:         make(map[int]*nestRuntime),
		gidNests:      make(map[uint32]int),
		hives:         make(map[string]*hiveRuntime),
	}
	state.instances.archive = s.archive
	// The world owns all timers, including regions without viewers.
	for index, nest := range s.template.Nests {
		// Empty codes are explicit in-memory fixtures. Published rows carry
		// the joined world name; a foreign definition never enters this owner.
		code := nest.WorldCode
		if code == "" {
			code = "INS_DEFAULT"
		}
		if code != worldCode {
			continue
		}
		state.materialized[nest.RegionID] = true
		if nest.HiveKey != "" {
			s.materializeHive(state, nest.HiveKey, s.nowMillis())
		} else {
			s.materializeNest(state, index, s.nowMillis())
		}
	}
	return state
}

// StartDivision creates the population owner before world admission. It does
// not manufacture a pre-filled world or advance elapsed callbacks.
/*
================
StartDivision
================
*/
func (s *MonsterState) StartDivision(division string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.division(division)
}

// AdvancePopulation also runs when a world has no connected viewers.
/*
================
AdvancePopulation
================
*/
func (s *MonsterState) AdvancePopulation(nowMs int64) {
	s.mu.Lock()
	keys := s.populationKeys()
	players := s.players
	s.mu.Unlock()
	// The snapshot covers every world of a division and the sampler filters
	// by world, so each division is queried once however many layers it has.
	snapshots := make(map[string][]PopulationPlayer)
	for _, key := range keys {
		s.mu.Lock()
		state := s.populationForLease(key.division, key.lease)
		if state == nil {
			s.mu.Unlock()
			continue
		}
		due := nowMs-state.lastDensityMs >= 30000
		s.mu.Unlock()
		var snapshot []PopulationPlayer
		if due && players != nil {
			cached, queried := snapshots[key.division]
			if !queried {
				cached = players(key.division, nowMs)
				snapshots[key.division] = cached
			}
			snapshot = cached
		}
		s.mu.Lock()
		state = s.populationForLease(key.division, key.lease)
		if state == nil {
			s.mu.Unlock()
			continue
		}
		if due && players != nil && nowMs-state.lastDensityMs >= 30000 {
			s.sampleHiveDensity(state, snapshot, nowMs)
		}
		s.runDueHiveTicks(state, nowMs)
		state.populationTicked = true
		s.mu.Unlock()
	}
}

/*
================
MonsterState.PopulationSettled

The division's boot fill is over: every population of it has run a
population pass, and every nest and hive materialized so far has had a
callback visit that placed nothing (it was at its target, its timer was
still waiting, or its placement was rejected). The test is per callback
unit and sticky, so the respawn churn of a live world, which places
something somewhere in nearly every nest tick, cannot hold it open. A
division with no population is settled.
================
*/
func (s *MonsterState) PopulationSettled(division string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.populationKeys() {
		if key.division != division {
			continue
		}
		state := s.populationForLease(key.division, key.lease)
		if state == nil {
			continue
		}
		if !state.populationTicked || len(state.fillPending) > 0 {
			return false
		}
	}
	return true
}

// InstancesInRegions reads the default population. Startup and clock events
// are explicit owner commands; observing a region never allocates or spawns.
/*
================
InstancesInRegions
================
*/
func (s *MonsterState) InstancesInRegions(divisionID string, regions []uint16) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.divs[divisionID]
	if state == nil {
		return nil
	}
	var out []monster.Instance
	for _, regionID := range regions {
		for _, gid := range state.byRegion[regionID] {
			out = append(out, state.instances.get(gid))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gid < out[j].Gid })
	return out
}

/*
==================
MaterializedInstances

MaterializedInstances returns value snapshots of every live instance in a
division, ordered by gid. Lifecycle state remains materialized so leaving a
region cannot reset defeated monsters or respawn timers.
==================
*/
func (s *MonsterState) MaterializedInstances(divisionID string) []monster.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.divs[divisionID]
	if state == nil {
		return nil
	}
	out := make([]monster.Instance, 0, state.instances.len())
	for _, instance := range state.instances.values() {
		out = append(out, instance)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Gid < out[j].Gid })
	return out
}

// Get returns a value snapshot of one live instance.
/*
================
Get
================
*/
func (s *MonsterState) Get(divisionID string, gid uint32) (monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	instance, ok := state.instances.lookup(gid)
	if ok {
		instance = finishSummonAction(instance, s.nowMillis())
	}
	return instance, ok
}

/*
================
resolveSpawnGround
================
*/
func (s *MonsterState) resolveSpawnGround(spawn monster.SpawnPoint, authoredY float64) (monster.SpawnPoint, bool) {
	if s.ground == nil {
		return spawn, true
	}
	y, ok := s.ground(spawn.RegionID, spawn.X, authoredY, spawn.Z)
	if !ok {
		return monster.SpawnPoint{}, false
	}
	spawn.Y = y
	return spawn, true
}

/*
================
normalizeGeneratedMonsterSpawn
================
*/
func normalizeGeneratedMonsterSpawn(spawn monster.SpawnPoint) monster.SpawnPoint {
	position := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{
		RegionID: spawn.RegionID,
		X:        spawn.X,
		Z:        spawn.Z,
	})
	spawn.RegionID = position.RegionID
	spawn.X = position.X
	spawn.Z = position.Z
	return spawn
}
