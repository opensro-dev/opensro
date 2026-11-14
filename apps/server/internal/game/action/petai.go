/*
===========================================================================

petai.go - session-owned summoned-pet movement, pickup and peer presentation

===========================================================================
*/

package action

import (
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"sort"
	"strings"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
petOwnerKey
================
*/
type petOwnerKey struct {
	division, name string
	gid            uint32
}

/*
================
petSessionEntry

One row of the tick's session walk (orderedPetSessions): a session
key and the session pointer the map held for it when the walk was
built. The pointer is a capture, not a claim: advancePetInDivision
revalidates it against the map under the division lock, so a forget
or rebind since the build is judged on the live entry exactly as
when the sweep fetched each pointer itself (#3126).
================
*/
type petSessionEntry struct {
	key   petOwnerKey
	state *petSession
}

/*
================
petSession
================
*/
type petSession struct {
	formationBattle  bool
	formationActive  bool
	relocatedAtMs    int64
	formationSlots   monster.ApproachSlots
	formationSlot    int
	formationTimers  *monster.AITimeManager
	displacement     *playerDisplacement
	mercenaryPenalty mercenaryPenaltyClock
	satiety          companion.SatietyClock
	// naturalRecoveryAtMs is the companion's next native recovery pulse
	// (petnaturalrecovery.go), bound with its session.
	naturalRecoveryAtMs int64
	session             uint64
	ready               bool
	character           *enterworld.Character
	refObjID            uint32
	// mover is the companion's own pose - its follower, or where it
	// stands parked (simulation/companionmover.go); while ridden the
	// rider's world moves it instead (cosmount.go).
	mover simulation.CompanionMover
	// judgedPet is the judged tick's scratch pet view (petjudged.go).
	judgedPet enterworld.CharacterCOS
	// tickFacts is the judged tick's scratch fact read
	// (readPetTickFactsInto, runtime_position.go), zeroed per tick:
	// the settled follow tick judges without allocating.
	tickFacts petTickFacts
	// judgedOwnerWorld is the judged tick's retained owner-world slot:
	// WorldStore.SnapshotExistingInto refills it per tick and the tick
	// consumes it before returning, so the follow pays no world clone.
	judgedOwnerWorld simulation.WorldState
	// judgedStuckAnswer is the judged tick's lazy stuck answer, built
	// once per session. It reads the two cells below, which every judged
	// tick refreshes before installing it, so installing the answer
	// allocates nothing; advancePet's deferred call runs it as before.
	judgedStuckAnswer  func() []simulation.Frame
	judgedStuckNowMs   int64
	judgedStuckTidWord uint16
	// judgedConstraint is the judged tick's movement constraint, built
	// once per session; it reads the session's character at call time,
	// exactly as the per-tick closure did.
	judgedConstraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	// judgedCosKey is the judged tick's folded COS abnormal map key
	// (cosabnormal.go), folded once for the binding judgedCosKeyFrom
	// names and folded again only when that binding changes, so the
	// tick's two abnormal reads allocate nothing (#2033).
	judgedCosKey     cosAbnormalKey
	judgedCosKeyFrom petOwnerKey
	judgedCosKeySet  bool
	// broodRecharge is the crow session's recharge verdict memo
	// (petbrood.go): the still verdict and the record fields it
	// was derived from, so an idle crow tick answers without
	// re-walking the skill table and the modifiers graph (#2033).
	broodRecharge petBroodRechargeMemo
	// boundCOS is the companion record this session was bound for
	// (bindCompanionSession); a replaced record is re-admitted.
	boundCOS *enterworld.CharacterCOS
	// tether is the parked trade transport's hold on its owner this tick
	// (simulation/tether.go); nil for every other companion state.
	tether     *simulation.Tether
	generation uint64
	// The spawn row last published for this companion and the exact
	// inputs it was derived from (companionPresentation): while they
	// stand, the kept row and vitals are handed out again instead of
	// re-deriving the speeds, the scale and the abnormal payload on
	// every snapshot (#3100).
	presentationPrimed      bool
	presentationFingerprint companionRowFingerprint
	presentationRow         wire.CosSpawnBand2
	presentationVitals      []byte
	// summonedAtMs marks a pet just called out of its item (not restored at
	// entry): its first publication carries spawn sub-state 1.
	summonedAtMs int64
	// arrivedAtMs marks a pet that just travelled with its owner
	// (relocateReturningPet). At the destination the pet appears, it does
	// not merely become visible, so its first publication there carries
	// sub-state 1 like a summon. A login restore never stamps it.
	arrivedAtMs    int64
	pickup         *wire.ItemMoveRequest
	pickupDeadline int64
	pickupCommand  bool
	public         []wire.Frame
	// trained is the attack-pet expansion's unsaved combat memory
	// (pettrained.go): attack counter and running self buffs.
	counterView [22]byte
	counterSent bool
	wolf        wolfKitState
	penguin     penguinRuntime
	crow        crowKit
	wolfBook    uint8
	wolfHits    [2]uint8
	trained     petTrainedRuntime
	// bond's spent budget is read by planPetBond and written by
	// commitPetBond under rt.petMu: concurrent victim hits in one
	// division share the bear's budget (checklist item 5).
	bond    petBondRuntime
	broodAt simulation.Spawn
	plume   *crowPlumeCast
	roar    *petRoarCast
	// combat is the attack pet's BATTLE state (petcombat.go); nil follows.
	combat *petCombatIntent
	// others holds kill-settlement frames for other characters (a party's
	// shared experience), drained with public by advancePets.
	others []RecipientFrames
	// routed holds pre-routed division frames (a bond revival's peer set
	// broadcast except its revived target), drained with others.
	routed []simulation.DivisionFrames
}

// BindPetSession admits a logical authenticated owner, not every character in
// storage. Duplicate EnterWorld/resume preserves the live pet movement plane.
/*
================
BindPetSession
================
*/
func (rt *Runtime) BindPetSession(division string, c *enterworld.Character, session uint64) {
	if c == nil || session == 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.bindPetSession(division, c, session, true)
}

/*
================
bindPetSession

Admission already owns the division lock. Bind before bootstrap so its
object list and subsequent peer publication use the same actor positions.
================
*/
func (rt *Runtime) bindPetSession(division string, c *enterworld.Character, session uint64, ready bool) {
	key := petOwnerKey{division: division, name: strings.ToLower(c.Name)}
	rt.petMu.Lock()
	old := rt.petSessions[key]
	if old != nil && old.session == session && old.character == c {
		if ready {
			for childKey, child := range rt.petSessions {
				if childKey.division == division && childKey.name == key.name {
					child.ready = true
				}
			}
		}
		rt.petMu.Unlock()
		return
	}
	if rt.petSessions == nil {
		rt.petSessions = make(map[petOwnerKey]*petSession)
	}
	state := &petSession{session: session, character: c, ready: ready, mercenaryPenalty: mercenaryPenaltyClock{lastMs: rt.Now().UnixMilli()}}
	rt.petSessions[key] = state
	rt.petSessionKeys = nil
	rt.petMu.Unlock()
	var companions []*enterworld.CharacterCOS
	rt.deps.Read(division, func() { companions = c.Companions() })
	for _, cos := range companions {
		if cos.Summoned {
			rt.bindCompanionSession(division, state, cos)
		}
	}
	// A restored attack pet's effective max reaches its owner unasked:
	// until it does, a default client divides the pet's HP by the
	// native catalog max (#1896). The helper gates on the expansion.
	nowMs := rt.Now().UnixMilli()
	var restored []simulation.Frame
	for _, cos := range companions {
		if cos.Summoned {
			restored = append(restored, simFrames(rt.petEffectiveMaxFrames(cos, nowMs))...)
		}
	}
	if len(restored) > 0 {
		rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: division, OnlyCharacterID: c.ID, Frames: restored}})
	}
}

// Called with the division lock held; the map lock never spans a character
// authority door, movement constraint, or packet publication.
/*
================
forgetPetSession
================
*/
func (rt *Runtime) forgetPetSession(division, name string) {
	retired := make(map[petOwnerKey]*petSession)
	rt.petMu.Lock()
	for key, state := range rt.petSessions {
		if key.division == division && key.name == strings.ToLower(name) {
			retired[key] = state
			delete(rt.petSessions, key)
		}
	}
	if len(retired) != 0 {
		rt.petSessionKeys = nil
	}
	rt.petMu.Unlock()
	// The owner's pets leave the world with the owner: the gid index
	// forgets them here (cosownerindex.go).
	for _, state := range retired {
		if state != nil && state.character != nil {
			rt.cosOwners.forgetOwner(division, state.character)
			break
		}
	}
	for key, state := range retired {
		if key.gid == 0 {
			rt.deps.Update(state.character, "crow-brood-logout", func() bool {
				if len(state.character.Brood) == 0 {
					return false
				}
				state.character.Brood = nil
				return true
			})
		}
		rt.clearTailwindSource(key, rt.Now().UnixMilli())
		for gid := range state.crow.views {
			rt.publishCrowPrecision(key.division, gid, rt.Now().UnixMilli())
		}
		rt.cancelPetCombat(key, state, rt.Now().UnixMilli())
		rt.wolfHowls.Lock()
		howl, found := rt.wolfHowls.rows[key]
		delete(rt.wolfHowls.rows, key)
		rt.wolfHowls.Unlock()
		if found {
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: key.division, Frames: simFrames(rt.wolfHowlFrames(key.division, howl))}})
		}
	}
}

/*
================
orderedPetSessionKeys

The tick's session walk list: every gid-bearing session key in
(division, name, gid) order. Membership turns only on petSessions
writes - the binds and the forgets, all under petMu - so the sorted
list is cached on the runtime (petSessionKeys) and rebuilt only
after a writer drops it; a rebuild always allocates a fresh slice,
so a walk already ranging the old list is never disturbed (#2033).
Readiness is not membership: a session readies without a map write
(bindPetSession readies the children in place), so advancePets
still judges state.ready live at each visit.

The same rebuild fills petSessionEntries with the pointer each key
holds at build time; the two lists are one walk in two shapes and
are only ever built and dropped together.
================
*/
func (rt *Runtime) orderedPetSessionKeys() []petOwnerKey {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	rt.rebuildPetSessionWalkLocked()
	return rt.petSessionKeys
}

/*
================
rebuildPetSessionWalkLocked

Builds the cached walk (both shapes) if a membership writer dropped
it. The caller holds petMu.
================
*/
func (rt *Runtime) rebuildPetSessionWalkLocked() {
	if rt.petSessionKeys != nil {
		return
	}
	keys := make([]petOwnerKey, 0, len(rt.petSessions))
	for key := range rt.petSessions {
		if key.gid != 0 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].gid < keys[j].gid
	})
	entries := make([]petSessionEntry, len(keys))
	for i, key := range keys {
		entries[i] = petSessionEntry{key: key, state: rt.petSessions[key]}
	}
	rt.petSessionKeys = keys
	rt.petSessionEntries = entries
}

/*
================
orderedPetSessions

The session walk as (key, session) pairs: the sweep judges the
pointer captured at build time instead of re-hashing the owner's
two-string key through petSessions for every pet on every tick
(#3126). The capture can be a tick stale - a session bound since
the build waits for the next rebuild exactly as it waited outside
the key list, and a forget or rebind since the build is caught by
advancePetInDivision's revalidation under the division lock, which
judges the live entry. Readiness flips land on the same pointer,
so the sweep still judges state.ready live.
================
*/
func (rt *Runtime) orderedPetSessions() []petSessionEntry {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	rt.rebuildPetSessionWalkLocked()
	return rt.petSessionEntries
}

/*
================
advancePets
================
*/
func (rt *Runtime) advancePets(nowMs int64) []simulation.DivisionFrames {
	shieldFrames := rt.advancePetShields(nowMs)
	var out []simulation.DivisionFrames
	var tethers map[string]simulation.Tether
	for _, entry := range rt.orderedPetSessions() {
		key, state := entry.key, entry.state
		if state == nil || !state.ready {
			continue
		}
		characterID, frames, public, others, routed, tetherKey, tether := rt.advancePetInDivision(key, state, nowMs)
		if tether != nil {
			if tethers == nil {
				tethers = make(map[string]simulation.Tether)
			}
			tethers[tetherKey] = *tether
		}
		for _, other := range others {
			out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: other.CharacterID, Frames: simFrames(other.Frames)})
		}
		out = append(out, routed...)
		// The pet is the source: its own viewers see what it does
		// (the reference server's nearby broadcast on the COS object), the
		// same audience its queued B505 closes reach. Routed by the owner, a
		// viewer who saw the owner but not the pet got each cast open and
		// never its close (#804). The owner has these in its private batch.
		if len(public) > 0 {
			out = append(out, routePetPublicFrames(key.division, key.gid, characterID, public)...)
		}
		if len(frames) != 0 && characterID != 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: characterID, Frames: frames})
		}
	}
	if rt.Worlds != nil {
		rt.Worlds.ReplaceTethers(tethers)
	}
	return append(shieldFrames, out...)
}

/*
================
advancePetInDivision

One session fetch serves the whole per-pet tick (#3126): the sweep
hands in the state it already holds, and every lane below judges that
pointer instead of re-hashing the owner's string key through
petSessionFor. The entry under key cannot move while this runs - the
division lock is held and every session bind/forget takes it too -
and lanes only ever bind or retire child sessions, never this one.
The sweep captured its pointer before this lock, though, so a forget
and rebind in that gap still moves the entry: the capture is
revalidated against the map once the lock is held, with the raw key
(no fold, one map read). The key-only lane names stay for direct
callers (tests): each fetches the session exactly as every lane once
did for itself.
================
*/
func (rt *Runtime) advancePetInDivision(key petOwnerKey, state *petSession, nowMs int64) (int64, []simulation.Frame, []wire.Frame, []RecipientFrames, []simulation.DivisionFrames, string, *simulation.Tether) {
	unlock := rt.lockDivision(key.division)
	defer unlock()
	rt.petMu.Lock()
	if current := rt.petSessions[key]; current != state {
		state = current
	}
	rt.petMu.Unlock()
	frames := rt.advanceCrowBroodSession(key, state, nowMs)
	frames = append(frames, rt.advancePenguinSession(key, state, nowMs)...)
	rt.advancePetGearFieldsSession(key, state, nowMs)
	rt.advancePetGearPoisonSession(key, state, nowMs)
	// One facts capture, and at most one owner snapshot behind it,
	// serve the tailwind, crow-kit, plume and wolf lanes (#2030):
	// nothing between here and the wolf lane writes an owner record
	// the walk judges - the plume heal only raises hit points, the
	// lanes before it write session state and frame queues - so the
	// four walks may judge the same captured values. The arm drops
	// before the spirit lane: it and the hunt lane judge fresh. The
	// deferred clear is panic hygiene only; the explicit one below
	// is the one the spirit lane's freshness rests on.
	factsKey := rt.armElementalFactsSession(key, state, nowMs)
	defer rt.clearElementalFacts(factsKey)
	rt.advanceTailwindSession(key, state, nowMs)
	rt.advanceCrowKitSession(key, state, nowMs)
	rt.advanceCrowPlumeSession(key, state, nowMs)
	rt.resetWolfExecutionSession(key, state, nowMs)
	frames = append(frames, rt.advanceWolfKitSession(key, state, nowMs)...)
	rt.clearElementalFacts(factsKey)
	frames = append(frames, rt.advancePetSpiritSession(key, state, nowMs)...)
	frames = append(frames, rt.advancePetBond(key, state, nowMs)...)
	frames = append(frames, rt.advancePetSatietySession(key, state, nowMs)...)
	frames = append(frames, rt.advancePetNaturalRecoverySession(key, state, nowMs)...)
	frames = append(frames, rt.advancePetSession(key, state, nowMs)...)
	if state == nil {
		return 0, frames, nil, nil, nil, "", nil
	}
	if rt.AttackPetExpansion {
		if counter, send := rt.judgePetCounter(key, state); send {
			frames = append(frames, simFrames([]wire.Frame{counter})...)
		}
	}
	characterID := state.character.ID
	var tetherKey string
	var tether *simulation.Tether
	if state.tether != nil {
		tetherKey = simulation.WorldKey(key.division, state.character.Name)
		copied := *state.tether
		tether = &copied
	}
	public, others, routed := state.public, state.others, state.routed
	state.public, state.others, state.routed = nil, nil, nil
	return characterID, frames, public, others, routed, tetherKey, tether
}

/*
================
routePetPublicFrames

Splits a pet's public batch by per-frame SourceGID so 0x35C7 pickup animations
go to viewers of the pet while 0x36AB ground-item despawns go to every viewer
that published the drop, matching SweepExpired.
================
*/
func routePetPublicFrames(divisionID string, petGID uint32, ownerCharacterID int64, public []wire.Frame) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	start := 0
	currentSource := frameSourceGID(petGID, public[0])
	for i := 1; i <= len(public); i++ {
		var nextSource uint32
		if i < len(public) {
			nextSource = frameSourceGID(petGID, public[i])
			if nextSource == currentSource {
				continue
			}
		}
		out = append(out, simulation.DivisionFrames{
			DivisionID:        divisionID,
			SourceGID:         currentSource,
			ExceptCharacterID: ownerCharacterID,
			Frames:            simFrames(public[start:i]),
		})
		start = i
		currentSource = nextSource
	}
	return out
}

/*
================
advancePet

Key-only entry for direct callers: fetch the session, then judge it.
The per-tick sweep enters through advancePetSession with the state it
already holds (#3126).
================
*/
func (rt *Runtime) advancePet(key petOwnerKey, nowMs int64) (output []simulation.Frame) {
	rt.petMu.Lock()
	state := rt.petSessions[key]
	rt.petMu.Unlock()
	return rt.advancePetSession(key, state, nowMs)
}

/*
================
advancePetSession
================
*/
func (rt *Runtime) advancePetSession(key petOwnerKey, state *petSession, nowMs int64) (output []simulation.Frame) {
	if state == nil {
		return nil
	}
	state.tether = nil
	following := false
	defer func() {
		if !following {
			rt.releasePetFormation(key, state)
		}
	}()
	generation := state.generation
	defer func() {
		if state.pickup != nil && (state.mover.AnyFollower() == nil || generation != state.generation || nowMs >= state.pickupDeadline) {
			result := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			for _, f := range result.Frames {
				output = append(output, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
		}
	}()
	// Registered last, so it runs first: the stuck event a step raised is
	// answered before the formation and pickup cleanups above (petstuck.go).
	var answerStuck func() []simulation.Frame
	defer func() {
		if answerStuck != nil {
			output = append(output, answerStuck()...)
		}
	}()
	// The session binds the canonical character pointer. Avoid searching the
	// division's entire saved-character population on every pet tick. The
	// tick first judges on door-read facts (readPetTickFacts): a missing
	// companion ends the session's motion, and an admitted tick that can
	// finish on values - the bound follower's follow, the early stops -
	// pays no owner copy at all (#2139). A tick that must commit through
	// the record abstains to the copy, which judges exactly as before:
	// an unsummoned, dead, mounted or deletion-pending companion (or a
	// fallen owner) takes the cheaper stop path, never the whole record
	// (#1943). companionSeen separates the two refusals; ownerDead
	// carries the copy's life fact the guardian test below weighs, read
	// in the same door pass that judged admission.
	facts := rt.readPetTickFactsInto(&state.tickFacts, key.division, state.character, key.gid)
	if !facts.companionSeen {
		state.roar = nil
		rt.cancelPetCombat(key, state, nowMs)
		state.mover.StopInPlace(nowMs)
		return nil
	}
	if facts.admitted {
		if out, handled := rt.advancePetJudged(key, state, facts, nowMs, &following, &answerStuck); handled {
			return out
		}
	}
	companionSeen := false
	ownerDead := false
	snapshot := rt.characterSnapshotCompanionAdmitted(key.division, state.character, func(live *enterworld.Character) bool {
		pet := live.CompanionByGID(key.gid)
		if pet == nil {
			return false
		}
		companionSeen = true
		ownerDead = enterworld.CurrentHP(live) == 0
		return !live.DeletePending && !ownerDead && pet.Summoned && !pet.Mounted && pet.CurrentHP != 0
	})
	if snapshot == nil && !companionSeen {
		state.roar = nil
		rt.cancelPetCombat(key, state, nowMs)
		state.mover.StopInPlace(nowMs)
		return nil
	}
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if snapshot == nil || !ok {
		state.roar = nil
		// An unsummoned, dead or mounted pet leaves BATTLE; a later summon
		// must not resume an old fight.
		rt.cancelPetCombat(key, state, nowMs)
		if state.mover.AnyFollower() == nil {
			return nil
		}
		frames := state.mover.AnyFollower().Stop(nowMs)
		// Port-only: a living guardian must keep its real anchor while awaiting its owner's revival timer.
		_, _, _, _, guardian := rt.bondSource(key, state)
		if !guardian || !ownerDead {
			state.mover.StopInPlace(nowMs)
		}
		return frames
	}
	cos := snapshot.CompanionByGID(key.gid)
	ref, found := refs.CharacterRefByCodename(cos.Codename)
	if !found || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || !rt.companionFollows(ref.TidWord) {
		state.roar = nil
		state.mover.StopInPlace(nowMs)
		if found && ref != nil && ref.RefObjID == cos.RefObjID && isVehicleCOS(ref.TidWord) {
			// Natively the first primary actor is a vehicle (the
			// band-2 trade transport), so it holds its owner.
			state.tether = &simulation.Tether{Anchor: rt.companionLiveSpawn(key.division, snapshot, cos, nowMs),
				Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}
		}
		return nil
	}
	owner := rt.liveSpawn(simulation.WorldKey(key.division, snapshot.Name), snapshot, nowMs)
	if state.mover.AnyFollower() == nil || state.mover.AnyFollower().GID() != cos.GID || state.refObjID != cos.RefObjID {
		start := owner
		if isVehicleCOS(ref.TidWord) {
			// Port-only (transportfollow.go): a transport just stepped off
			// sets out from where its trader left it, not from the trader.
			start = rt.companionLiveSpawn(key.division, snapshot, cos, nowMs)
		}
		state.mover.FollowFrom(cos.GID, start)
		state.generation++
		state.refObjID = cos.RefObjID
	}
	// Relocation and summons build new followers; every tick installs the one
	// planner they all route with.
	state.mover.AnyFollower().SetRoutePlanner(rt.PlanCompanionRoute)
	if cos.BroodStage == 1 {
		rt.cancelPetCombat(key, state, nowMs)
		return state.mover.AnyFollower().Stop(nowMs)
	}
	if rt.TransportFollowTether && isVehicleCOS(ref.TidWord) {
		// Port-only (transportfollow.go): the following transport holds
		// its trader as a parked one does, from where it walks now.
		state.tether = &simulation.Tether{Anchor: state.mover.AnyFollower().Position(nowMs),
			Range: simulation.TradeTransportTetherRange, Reason: simulation.TetherTradeTransport}
	}
	answerStuck = func() []simulation.Frame {
		return rt.answerPetStuck(key, state, snapshot, cos, ref.TidWord, nowMs)
	}
	if state.displacement != nil {
		state.roar = nil
		if nowMs < state.displacement.untilMs {
			return state.mover.AnyFollower().Stop(nowMs)
		}
		state.displacement = nil
	}
	if rt.companionMovementBlocked(key.division, snapshot, cos) {
		state.roar = nil
		rt.cancelPetCombat(key, state, nowMs)
		return state.mover.AnyFollower().Stop(nowMs)
	}
	block := rt.cosAbnormal(key.division, snapshot.Name, cos.GID)
	// The step keeps the native run: natively it is compared with the authored
	// speed. Only the follower moves at the paced one.
	walk, run := cosParameter(ref, cos, block, movementWalkParameter), cosParameter(ref, cos, block, movementRunParameter)
	state.mover.AnyFollower().SetMovementSpeeds(walk, rt.cosPacedRun(ref, run), nowMs)
	var constraint func(simulation.Spawn, simulation.Spawn) (simulation.Spawn, *simulation.MoveError)
	if rt.ConstrainMovement != nil {
		constraint = func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
			return rt.ConstrainMovement(snapshot.Name, from, to)
		}
	}
	if rt.advancePetRoar(petCombatStep{key: key, state: state, snapshot: snapshot, pet: cos, ref: ref, nowMs: nowMs}) ||
		nowMs < state.wolf.howlHoldUntil {
		return state.mover.AnyFollower().Stop(nowMs)
	}
	if frames, handled := rt.advancePetCombat(petCombatStep{key: key, state: state, snapshot: snapshot,
		pet: cos, ref: ref, run: run, constraint: constraint, nowMs: nowMs}); handled {
		return frames
	}
	if state.pickup != nil {
		q := *state.pickup
		item, found := rt.characterGround(key.division, snapshot, q.GroundGID)
		pose := state.mover.AnyFollower().Position(nowMs)
		// A target that vanished, timed out, or can never be granted
		// at arrival (cosPickupTargetRetired) retires the same way:
		// the refusal answer, no walk, no warning.
		if !found || nowMs >= state.pickupDeadline || rt.cosPickupTargetRetired(key.division, snapshot, pose, item) {
			result := finishPendingCosPickup(state, failureResult(wire.ErrCodeInvalidRequest))
			frames := state.mover.AnyFollower().Stop(nowMs)
			for _, f := range result.Frames {
				frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
			}
			return frames
		}
		target := simulation.Spawn{RegionID: item.Position.RegionID, X: float64(item.Position.X), Y: float64(item.Y), Z: float64(item.Position.Z)}
		from := grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)}
		distance := grounditem.Distance2D(from, item.Position)
		if !grounditem.SameWorld(from, item.Position) || math.IsNaN(distance) || distance > grounditem.ExecuteRange {
			return state.mover.AnyFollower().Approach(target, float64(rt.cosPacedRun(ref, run)), nowMs, grounditem.ExecuteRange, constraint)
		}
		frames := state.mover.AnyFollower().Stop(nowMs)
		result := rt.applyCosGroundAt(key.division, state.character, q, cosGroundAttempt{now: time.UnixMilli(nowMs), command: state.pickupCommand, doors: rt.tickDoors()})
		result = finishPendingCosPickup(state, result)
		state.public = append(state.public, result.Broadcast...)
		// A shared pickup's receipt and the party loot notices belong to other
		// members (routeSharedPickup, partyLootNotice); advancePets drains
		// them beside the kill-settlement frames.
		state.others = append(state.others, result.Recipients...)
		for _, f := range result.Frames {
			frames = append(frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		return frames
	}
	if ref.TidWord>>11 >= 3 && ref.TidWord>>11 <= domain.MercenaryBand {
		following = true
		return rt.advanceOwnerFormation(petCombatStep{key: key, state: state, snapshot: snapshot, pet: cos, ref: ref, run: run, constraint: constraint, nowMs: nowMs})
	}
	return state.mover.AnyFollower().Advance(owner, float64(rt.cosPacedRun(ref, run)), nowMs, constraint)
}

// PetPresentation is the sole read boundary for peer pet motion. It holds the
// same division lock as advancement and returns only detached public fields.
/*
================
PetPresentation
================
*/
func (rt *Runtime) PetPresentation(division, name string) *simulation.PeerCOS {
	pets := rt.CompanionPresentations(division, name)
	if len(pets) == 0 {
		return nil
	}
	return pets[0]
}

/*
================
CompanionPresentations

Copy each companion under the division and character read doors. Every pet
supplies its own world position to peer visibility, including parked mounts.
================
*/
func (rt *Runtime) CompanionPresentations(division, name string) []*simulation.PeerCOS {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.companionPresentations(division, name)
}

/*
================
companionPresentations

CompanionPresentations for a caller that already holds the division lock:
it takes the character read door once.
================
*/
func (rt *Runtime) companionPresentations(division, name string) []*simulation.PeerCOS {
	var result []*simulation.PeerCOS
	var guildID int64
	rt.deps.Read(division, func() {
		result, guildID = rt.companionPresentationsInDoor(division, name)
	})
	// Guild() takes the same store read lock as Read. Resolve its detached
	// name only after releasing the character door, including peer snapshots.
	guildName := ""
	if guildID != 0 {
		if store := rt.deps.GuildAuthority(); store != nil {
			if guild, found := store.GuildRecord(division, guildID); found {
				guildName = guild.Name
			}
		}
	}
	for _, pet := range result {
		if pet.Row.Band == domain.MercenaryBand {
			pet.Row.OwnerName = guildName
		}
	}
	return result
}

/*
================
companionPresentationsInDoor

The presentations for a caller that already holds the character read door
(HandleObjectSelect's resolveLiveObject). It must take no door itself: the
store's read lock is a sync.RWMutex, and a second RLock queued behind a
waiting writer never returns while the first is held (the 2026-10-10
GameWorld deadlock: an object select with a summoned pet against the tick's
UpdateCharacter). The copied guild id lets publication resolve the guild
name outside this door; selection needs only the pose and life state.
================
*/
func (rt *Runtime) companionPresentationsInDoor(division, name string) ([]*simulation.PeerCOS, int64) {
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name)}]
	rt.petMu.Unlock()
	if owner == nil || !owner.ready {
		return nil, 0
	}
	var guildID int64
	if owner.character.GuildID != nil {
		guildID = *owner.character.GuildID
	}
	var result []*simulation.PeerCOS
	for _, pet := range owner.character.Companions() {
		if !pet.Summoned {
			continue
		}
		rt.petMu.Lock()
		state := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(name), gid: pet.GID}]
		rt.petMu.Unlock()
		if state == nil {
			continue
		}
		if projection := rt.companionPresentation(division, state, pet); projection != nil {
			if projection.Row.Band == domain.MercenaryBand {
				projection.Row.OwnerName = ""
				projection.Row.HoldType = enterworld.DressedJob(owner.character)
				projection.Row.PvpState = owner.character.PVPState()
			}
			result = append(result, projection)
		}
	}
	return result, guildID
}

/*
================
companionPresentation
================
*/
func (rt *Runtime) companionPresentation(division string, state *petSession, cos *enterworld.CharacterCOS) *simulation.PeerCOS {
	c := state.character
	now := rt.Now().UnixMilli()
	var result *simulation.PeerCOS
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return nil
	}
	ref, ok := refs.CharacterRefByCodename(cos.Codename)
	if !ok || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || ref.TidWord>>11 < 1 || ref.TidWord>>11 > 5 && ref.TidWord>>11 != domain.CapturedCOSBand {
		return nil
	}
	var world simulation.WorldState
	var revision uint64
	// Port-only (transportfollow.go): a following transport presents its
	// follower like a pet; a parked one, natively, its admission anchor.
	following := rt.companionFollows(ref.TidWord) && state.mover.AnyFollower() != nil && state.mover.AnyFollower().GID() == cos.GID
	if ref.TidWord>>11 <= 2 && !following || cos.Mounted {
		if state.boundCOS != cos {
			// A record the session was not bound for stands at its
			// owner, its admission pose, until something moves it.
			state.boundCOS = cos
			state.mover.Park(cos.GID, rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now))
			state.generation++
		}
		world, _ = state.mover.World(cos.GID)
		if cos.Mounted {
			world = rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
		}
	} else {
		if state.mover.AnyFollower() == nil || cos.GID != state.mover.AnyFollower().GID() || cos.RefObjID != state.refObjID {
			return nil
		}
		world, revision = state.mover.AnyFollower().Presentation()
	}
	block := rt.cosAbnormal(division, c.Name, cos.GID)
	// The row is a pure function of these inputs: the cos record and
	// its catalog row, the committed abnormal block (immutable, so
	// its pointer names its contents), the owner's committed record
	// (its CommitRevision moves with every store door) and the two
	// time-window faces (the trained attack speed and the relocated
	// sub-state). While they stand,
	// the kept row is handed out again; the live faces of the
	// presentation (world, revision, freshness) are filled per call
	// below either way.
	fingerprint := companionRowFingerprint{
		cos:           cos,
		ref:           ref,
		block:         block,
		gid:           cos.GID,
		refObjID:      cos.RefObjID,
		codename:      cos.Codename,
		name:          cos.Name,
		level:         cos.Level,
		satiety:       cos.Satiety,
		mercenary:     cos.MercenaryAttributes,
		summons:       cos.SummonGeneration,
		followRun:     cos.FollowRunFactor,
		followRunSet:  cos.FollowRunSet,
		ownerRevision: c.CommitRevision,
		attackSpeed:   rt.petAttackSpeed(petCombatStep{snapshot: c, pet: cos, state: state, nowMs: now}),
		mounted:       cos.Mounted,
		nativeStatus:  cos.NativeBodyStatus,
		dead:          cos.CurrentHP == 0,
		relocated:     state.relocatedAtMs != 0 && now-state.relocatedAtMs <= petAppearWindowMs,
	}
	result = &simulation.PeerCOS{Mounted: cos.Mounted, NativeBodyStatus: cos.NativeBodyStatus, World: world, Revision: revision, Session: state.session, Generation: state.generation,
		Fresh: state.summonedAtMs != 0 && now-state.summonedAtMs <= petAppearWindowMs ||
			state.arrivedAtMs != 0 && now-state.arrivedAtMs <= petAppearWindowMs}
	if state.presentationPrimed && state.presentationFingerprint == fingerprint {
		result.Row = state.presentationRow
		result.AbnormalVitals = append([]byte(nil), state.presentationVitals...)
	} else {
		name := cos.Name
		if name == "" {
			name = ref.Name
		}
		walk, run := rt.cosMovementSpeeds(ref, cos, block)
		result.Row = wire.CosSpawnBand2{Band: uint8(ref.TidWord >> 11), RefObjID: cos.RefObjID, Gid: cos.GID,
			Walk: walk, Run: run,
			Scale: cosParameter(ref, cos, block, actionSpeedParameter) / float32(fingerprint.attackSpeed), Name: name, OwnerName: enterworld.PartyMemberName(c), OwnerModelRef: enterworld.CharacterModelRef(c, nil), OwnerGid: enterworld.ObjectIDForCharacter(c)}
		if fingerprint.relocated {
			result.Row.State = 7
		}
		if block != nil && block.Mask != 0 {
			result.AbnormalVitals = abnormalVitalsPayload(cos.GID, block)
		}
		state.presentationPrimed = true
		state.presentationFingerprint = fingerprint
		state.presentationRow = result.Row
		state.presentationVitals = append([]byte(nil), result.AbnormalVitals...)
	}
	if fingerprint.dead {
		result.LifeState = wire.LifeStateDead
	}
	return result
}

/*
================
companionRowFingerprint

Everything one published companion spawn row is a pure function of:
the cos record (pointer and the fields the row or its derivation
reads), its catalog row, the committed abnormal block (immutable
after publication, so the pointer names its contents), the owner's
commit revision (the store bumps it on every committed write, so a
suit, an alias or a pet change re-derives), the trained attack speed
for this instant, and the relocated appearance window. Nothing here
allocates; the compare is plain value equality.
================
*/
type companionRowFingerprint struct {
	cos           *enterworld.CharacterCOS
	ref           *enterworld.CharacterRef
	block         *abnormal.Block
	gid           uint32
	refObjID      uint32
	codename      string
	name          string
	level         uint8
	satiety       uint16
	mercenary     uint8
	summons       uint64
	followRun     float32
	ownerRevision uint64
	attackSpeed   float64
	mounted       bool
	nativeStatus  uint8
	dead          bool
	followRunSet  bool
	relocated     bool
}

// Called inside the summon transaction, with the division operation lock held.
/*
================
rememberTransportCOS
================
*/
func (rt *Runtime) rememberTransportCOS(division string, c *enterworld.Character, pose simulation.Spawn) {
	rt.petMu.Lock()
	owner := rt.petSessions[petOwnerKey{division: division, name: strings.ToLower(c.Name)}]
	rt.petMu.Unlock()
	if owner == nil || c.ActiveCOS == nil {
		return
	}
	state := rt.bindCompanionSession(division, owner, c.ActiveCOS)
	state.boundCOS = c.ActiveCOS
	// The anchor is the transport's one pose: a follower bound earlier (at
	// the owner) must not outrank it; a following transport (port-only,
	// transportfollow.go) sets out from here on the next pet tick.
	state.mover.Park(c.ActiveCOS.GID, pose)
	state.generation++
	// Natively a transport summoned with goods aboard is a caravan.
	rt.registerCaravan(division, c)
}

/*
================
bindCompanionSession

The division lock serializes admission. Transport replacement retires its
own state without touching the owner's independent attack and pickup pets.
================
*/
func (rt *Runtime) bindCompanionSession(division string, owner *petSession, pet *enterworld.CharacterCOS) *petSession {
	key := petOwnerKey{division: division, name: strings.ToLower(owner.character.Name), gid: pet.GID}
	// Every summoned pet's runtime life starts at this door (world
	// entry, summon, mercenary call): the gid index notes its owner
	// here and nowhere else (cosownerindex.go).
	rt.cosOwners.note(division, pet.GID, owner.character)
	rt.petMu.Lock()
	old := rt.petSessions[key]
	if old != nil && old.character == owner.character && old.boundCOS == pet {
		old.session = owner.session
		old.ready = owner.ready
		rt.petMu.Unlock()
		return old
	}
	owner.formationSlots.Release(pet.GID)
	state := &petSession{formationSlot: -1, session: owner.session, ready: owner.ready, character: owner.character, boundCOS: pet, refObjID: pet.RefObjID,
		naturalRecoveryAtMs: rt.Now().UnixMilli() + petNaturalRecoveryPeriodMs}
	if old != nil {
		state.generation = old.generation + 1
	}
	rt.petSessions[key] = state
	rt.petSessionKeys = nil
	rt.petMu.Unlock()
	pose := rt.liveSpawn(simulation.WorldKey(division, owner.character.Name), owner.character, rt.Now().UnixMilli())
	pose = rt.companionAdmissionSpawn(pet, pose)
	state.mover.Place(pet.GID, pose)
	return state
}

/*
================
refreshCompanionOwnerLabels

A suited owner's companions carry the job alias, not the real name (#1983;
the native spawn writers read the owner name through the same job rule as
the player spawn). The peer COS lane only re-sends a pet when its session
generation moves, so a completed dress or undress bumps every summoned
companion session of this owner: the lane despawns and respawns each pet
with the new label.
================
*/
func (rt *Runtime) refreshCompanionOwnerLabels(division string, c *enterworld.Character) {
	rt.petMu.Lock()
	for key, state := range rt.petSessions {
		if key.division != division || key.gid == 0 || state == nil || state.character != c {
			continue
		}
		state.generation++
	}
	rt.petMu.Unlock()
}

// Successful owner re-entry retires motion/pickup from the departed world.
// The persisted pet identity and inventory survive; the new projection starts
// around its owner's admitted destination, before bootstrap publication.
/*
================
relocateReturningPet
================
*/
func (rt *Runtime) relocateReturningPet(division string, c *enterworld.Character, destination simulation.Spawn) map[petOwnerKey]petSession {
	rt.petMu.Lock()
	previous := make(map[petOwnerKey]petSession)
	ownerKey := petOwnerKey{division: division, name: strings.ToLower(c.Name)}
	if owner := rt.petSessions[ownerKey]; owner != nil {
		previous[ownerKey] = *owner
	}
	keys := make([]petOwnerKey, 0)
	for key, state := range rt.petSessions {
		if key.division != division || key.name != strings.ToLower(c.Name) || key.gid == 0 || state.character != c {
			continue
		}
		previous[key] = *state
		keys = append(keys, key)
	}
	rt.petMu.Unlock()
	// The parked transport travels with its trader; its tether follows on
	// the next pet tick, and the old anchor holds nothing until then.
	if rt.Worlds != nil {
		rt.Worlds.ClearTether(simulation.WorldKey(division, c.Name))
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].gid < keys[j].gid })
	for _, key := range keys {
		rt.petMu.Lock()
		state := rt.petSessions[key]
		rt.petMu.Unlock()
		rt.cancelPetCombat(key, state, rt.Now().UnixMilli())
		rt.wolfHowls.Lock()
		howl, found := rt.wolfHowls.rows[key]
		delete(rt.wolfHowls.rows, key)
		rt.wolfHowls.Unlock()
		if found {
			rt.queueMonsterLegFrames([]simulation.DivisionFrames{{DivisionID: key.division, Frames: simFrames(rt.wolfHowlFrames(key.division, howl))}})
		}
		rt.releasePetFormation(key, state)
		expireCosPickup(state)
		state.public = nil
		state.crow = crowKit{views: state.crow.views}
		state.generation++
		state.arrivedAtMs = rt.Now().UnixMilli()
		// The companion appears at the destination; one that left its
		// record keeps no pose from the old place.
		state.mover.Release()
		if cos := c.CompanionByGID(key.gid); cos != nil {
			state.mover.Place(cos.GID, rt.companionAdmissionSpawn(cos, destination))
			state.refObjID = cos.RefObjID
		}
	}
	return previous
}

/*
================
restoreCompanionRelocation

Projection can refuse after movement preparation. The division lock keeps
the old followers quiescent, so restoring them also restores pending pickup
and generation identity without replaying or cancelling an unrelated job.
================
*/
func (rt *Runtime) restoreCompanionRelocation(previous map[petOwnerKey]petSession) {
	rt.petMu.Lock()
	defer rt.petMu.Unlock()
	for key, state := range previous {
		if current := rt.petSessions[key]; current != nil {
			*current = state
		}
	}
}

/*
================
petArrivalFrames

The owner's own presentation of the pets that just travelled with them.
The bootstrap list placed each pet silently (grouped records carry no
sub-state byte, so no client shows an appear for them), so after the
list the owner is shown the arrival itself: one single spawn per living
attack or pickup pet, its sub-state byte 1, which the client answers
with the appear effect and the summon sound (client 854CD0) exactly as at a
summon, replacing the listed entity in place. Fresh is the gate: only
the summon and arrival stamps set it, and a summon cannot race a
teleport, so at a re-entry seam Fresh means arrived.
================
*/
func (rt *Runtime) petArrivalFrames(division string, c *enterworld.Character) []wire.Frame {
	nowMs := rt.Now().UnixMilli()
	var frames []wire.Frame
	for _, view := range rt.companionPresentations(division, c.Name) {
		if view == nil || !view.Fresh || view.LifeState == wire.LifeStateDead {
			continue
		}
		if view.Row.Band != 3 && view.Row.Band != 4 {
			continue
		}
		for _, frame := range view.SpawnFrames(nowMs) {
			frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
	}
	return frames
}

/*
================
followingCOSBand

Pets (3, 4) and a captured quest monster (6) follow their owner on foot;
transports (1, 2) move only with their rider. A parked trade transport
instead holds its trader within range (advancePet's tether), unless the
port-only SRO_TRANSPORT_FOLLOW has it follow (Runtime.companionFollows).
================
*/
func followingCOSBand(band uint16) bool {
	return band == 3 || band == 4 || band == domain.MercenaryBand || band == domain.CapturedCOSBand
}

/*
================
CompanionTargets

The monster tick's companion targets for one owner: every summoned,
living, unmounted companion in container order, at its presented
pose. A mounted ride is struck through its rider (monsterAttackStage).
================
*/
func (rt *Runtime) CompanionTargets(division string, ownerGID uint32, nowMs int64) []simulation.CompanionTarget {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.companionTargets(division, ownerGID, nowMs)
}

/*
================
companionTargets

CompanionTargets for a caller that already holds the division lock (a
monster area gathering its candidates at release).
================
*/
func (rt *Runtime) companionTargets(division string, ownerGID uint32, nowMs int64) []simulation.CompanionTarget {
	owner := rt.findCharacterByGid(division, ownerGID)
	if owner == nil {
		return nil
	}
	// One character door for the whole gather (#3027): the walk and
	// every pet's record read share one hold, instead of a fresh door
	// per pet as before. The reads are the same ones - the presented
	// pets, then CompanionByGID per pet with its duplicate refusal -
	// so the answer is unchanged, taken at one hold instead of many.
	// The guild name companionPresentations resolves for spawn rows
	// is not read here and is not paid for.
	var pets []*simulation.PeerCOS
	var records []enterworld.CharacterCOS
	rt.deps.Read(division, func() {
		pets, _ = rt.companionPresentationsInDoor(division, owner.Name)
		if len(pets) == 0 {
			return
		}
		records = make([]enterworld.CharacterCOS, len(pets))
		for i, pet := range pets {
			if live := owner.CompanionByGID(pet.Row.Gid); live != nil {
				records[i] = *live
			}
		}
	})
	var out []simulation.CompanionTarget
	for i, pet := range pets {
		// Natively a grab pet (slot 0x43C) is never a hostile target.
		if pet.Mounted || pet.LifeState == wire.LifeStateDead || pet.Row.Band == domain.PickupPetBand {
			continue
		}
		ref, found := rt.cosReference(&records[i])
		if !found {
			continue
		}
		out = append(out, simulation.CompanionTarget{Gid: pet.Row.Gid, Pose: pet.World.LiveSpawnAt(nowMs),
			BodyRadius: simulation.BodyRadius(ref.Parameters.BodyRadius), NativeBodyStatus: pet.NativeBodyStatus, Band: pet.Row.Band})
	}
	return out
}
