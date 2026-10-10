/*
===========================================================================

worldstore.go - shared accepted movement state and lifecycle

===========================================================================
*/
package simulation

import (
	"math"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

/*
================
WorldStore

WorldStore is the runtime home of each character's live-position plane:
the WorldState that action, pickup, and movement handlers
read and write. Keys fold the character name to lower case, matching the
fixture's case-insensitive character matching.

It lives beside WorldState so simulation remains the sole owner of mutable
runtime world/entity state. Feature lanes can transact through this store
but cannot establish a competing live-position plane.
================
*/
type WorldStore struct {
	mu            sync.Mutex
	states        map[string]*WorldState
	groundConfig  GroundWalkConfig
	groundUpdates map[string]GroundUpdate
	tethers       map[string]Tether
}

/*
================
NewWorldStore

NewWorldStore returns an empty WorldStore.
================
*/
func NewWorldStore() *WorldStore {
	return &WorldStore{states: make(map[string]*WorldState)}
}

/*
================
WorldKey

WorldKey is the store key for one character.
================
*/
func WorldKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}

/*
================
state

state returns the character's state, seeding it on first touch. Callers
hold the lock. A seed made without a character record answers this call
only: keeping it would pin the race start as that name's live position
until the next Forget, while its record says otherwise.
================
*/
func (st *WorldStore) state(key string, seed func() WorldState) *WorldState {
	if state, ok := st.states[key]; ok {
		return state
	}
	seeded := seed()
	seeded.Normalize()
	if seeded.unbound {
		log.WithField("key", key).Warn("simulation: world state seeded without a character record; not kept")
		return &seeded
	}
	st.states[key] = &seeded
	return &seeded
}

/*
================
Snapshot

Snapshot returns a safe copy of the character's state, seeding from seed
on first touch. The MoveSegment pointer is deep-copied so a concurrent
resteer cannot race the caller's read.
================
*/
func (st *WorldStore) Snapshot(key string, seed func() WorldState) WorldState {
	st.mu.Lock()
	defer st.mu.Unlock()

	state := st.state(key, seed)
	st.advanceGroundWalk(key, state)
	return CloneWorldState(*state)
}

/*
================
MovementCurrent

MovementCurrent validates a queued movement at delivery, without seeding a
disconnected actor. The reliable writer orders this decision with LIFE and
correction packets; a pre-death snapshot cannot publish after revival.
================
*/
func (st *WorldStore) MovementCurrent(key string, expected WorldState) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	w, ok := st.states[key]
	if !ok || w.LifeRevision != expected.LifeRevision || w.Spawn != expected.Spawn || w.MovementMode != expected.MovementMode {
		return false
	}
	if w.MoveSegment == nil || expected.MoveSegment == nil {
		return w.MoveSegment == expected.MoveSegment
	}
	return *w.MoveSegment == *expected.MoveSegment
}

/*
================
GroundPathCurrent

An untagged approach path is replaceable state, unlike a prediction-ID
receipt. Never start an already retired path after its terminal correction.
Retiming preserves the command revision and must not erase its only path.
================
*/
func (st *WorldStore) GroundPathCurrent(key string, expected WorldState) bool {
	if expected.groundRevision == 0 {
		return st.MovementCurrent(key, expected)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	state, ok := st.states[key]
	return ok && state.LifeRevision == expected.LifeRevision && state.groundRevision == expected.groundRevision && state.groundActive()
}

/*
================
AdmissionCurrent

A ground command acknowledgement is not a replaceable pose. Its accepted
intent remains valid after progression or collision, so the ordered writer
delivers it before that intent's stop correction. New commands and LIFE
changes still fence obsolete acknowledgements.
================
*/
func (st *WorldStore) AdmissionCurrent(key string, expected WorldState) bool {
	if expected.groundRevision == 0 {
		return st.MovementCurrent(key, expected)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	state, ok := st.states[key]
	return ok && state.LifeRevision == expected.LifeRevision && state.groundRevision == expected.groundRevision
}

/*
================
Update

Update runs fn on the character's state under the lock and returns a safe
copy of the result. fn must REPLACE MoveSegment, never mutate it in place
(the tick loop's snapshot contract).
================
*/
func (st *WorldStore) Update(key string, seed func() WorldState, fn func(*WorldState)) WorldState {
	st.mu.Lock()
	defer st.mu.Unlock()

	state := st.state(key, seed)
	st.advanceGroundWalk(key, state)
	previous := state.MoveSegment
	previousSpawn, previousLife := state.Spawn, state.LifeRevision
	owner := state.GoalOwner()
	if state.groundActive() {
		owner = state.Ground.owner
	}
	fn(state)
	if previous == state.MoveSegment && (previousLife != state.LifeRevision || previousSpawn != state.Spawn) {
		state.groundRevision++
		state.Ground = nil
		delete(st.groundUpdates, key)
	}
	st.bindGroundWalk(key, state, previous, owner)
	return CloneWorldState(*state)
}

/*
================
Forget

Forget drops a character's live-position entry. Session close routes
here (Runtime.ForgetCharacter) so a departed character does not hold a
world entry for the life of the process; the goal plane already
persisted on the record (writeBackWorld on every accepted op), so the
next touch simply re-seeds from it. Idempotent on an absent key.
================
*/
func (st *WorldStore) Forget(key string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	delete(st.states, key)
	delete(st.groundUpdates, key)
}

/*
================
SeedWorldState

SeedWorldState builds the first-touch world state for a character: the
persisted world record when it carries a complete spawn, else the race
start profile - the same fallback the fixture's missionWorldStateForCharacter
applies. The spawn is taken whole or not at all: mixing the start region
with saved coordinates places the character in a region it never stood
in. The persisted record has no segment plane (nothing is in flight
across a login).
================
*/
func SeedWorldState(character *domain.Character) WorldState {
	profile := ChinaStartProfile()
	if character != nil && strings.HasPrefix(character.ModelCodename, "CHAR_EU") {
		profile = EuropeStartProfile()
	}
	state := DefaultWorldState(profile)

	if character == nil {
		state.unbound = true
		return state
	}
	if character.World == nil {
		return state
	}
	world := character.World
	if world.MovementMode != nil {
		state.MovementMode = CoerceRunWalkMode(uint8(*world.MovementMode), RunMode)
	}
	state.SpawnSet = world.SpawnSet
	if world.SpawnSet {
		state.MovementSourceSeeded = true
	}
	if world.Spawn == nil {
		return state
	}
	spawn := world.Spawn
	if !CompleteWorldSpawn(spawn) {
		log.WithField("character", character.Name).Warn("simulation: saved spawn is incomplete; seeding the race start")
		return state
	}
	state.Spawn.RegionID = uint16(*spawn.RegionID)
	state.Spawn.X, state.Spawn.Y, state.Spawn.Z = *spawn.X, *spawn.Y, *spawn.Z
	if spawn.Angle != nil {
		state.Spawn.Angle = uint16(*spawn.Angle & 0xFFFF)
	}
	// Heal records persisted before goal-frame normalization existed: a spawn
	// saved as enter-region + multi-sector overflow re-seeds in its canonical
	// frame, so settled reads (drops, tick corrections, the next move's
	// source block) stop shipping the stale region word.
	state.Spawn = NormalizeSpawnFrame(state.Spawn)
	return state
}

/*
================
CompleteWorldSpawn

Whether a saved spawn names a position by itself: a region inside the
16-bit region space and three finite coordinates. The angle may be absent.
================
*/
func CompleteWorldSpawn(spawn *domain.WorldSpawn) bool {
	if spawn == nil || spawn.RegionID == nil || *spawn.RegionID <= 0 || *spawn.RegionID > 0xffff {
		return false
	}
	for _, value := range []*float64{spawn.X, spawn.Y, spawn.Z} {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return false
		}
	}
	return true
}
