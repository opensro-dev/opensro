/*
===========================================================================

skillfinalize.go - the skill-close queue: delayed cast-finalize controls

pendingSkillFinalize entries retain one delayed control packet's actor
route until the simulation clock reaches its due time: accepted casts
queue their closing frames here, a chain root holds its bracket open
across the chain's stages, and the tick drains what is due in queue
order. Split from skillcombat.go, which keeps cast admission, resource
commit and cast release.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pendingSkillFinalize

One delayed control packet retains its actor route independently of damage.
================
*/
type pendingSkillFinalize struct {
	sourceGID     uint32
	divisionID    string
	characterName string
	// ownerKey is simulation.WorldKey(divisionID, characterName), computed
	// once at queue time: the tick's owner scans (snapshot, drain, chain
	// brackets) walk this queue every tick and re-keying per entry per tick
	// was pure allocation churn (#1820 D). The two fields above never
	// change after queueing, so the cached key cannot go stale.
	ownerKey string
	dueAtMs  int64
	frame    wire.Frame
	// Presentation-only closes retain session ownership without blocking commands.
	presentationOnly bool
	// chainBracket marks the mode-2 close of a chain ROOT. The v1.150 client
	// (85CB60) appends every linked stage's B245 to that root's deco, which
	// carries the one authored clip for the whole chain; closing it before
	// the last stage turns the remaining stages into result-only temporaries
	// (damage with the caster snapped to idle). Stages re-arm it instead of
	// opening brackets of their own; it never gates the chain's own stages.
	chainBracket bool
}

/*
================
newPendingSkillFinalize

newPendingSkillFinalize starts one delayed control for an owner. The
cached ownerKey is derived here, from the division and character the
caller names, so no literal at a queue site can leave the key empty and
strand the cast outside the tick's owner scans (#1820 D).
================
*/
func newPendingSkillFinalize(divisionID, characterName string) pendingSkillFinalize {
	return pendingSkillFinalize{
		divisionID:    divisionID,
		characterName: characterName,
		ownerKey:      simulation.WorldKey(divisionID, characterName),
	}
}

/*
================
queueSkillFinalize

Retain control order until the simulation clock reaches the due time.
================
*/
func (rt *Runtime) queueSkillFinalize(
	divisionID string,
	characterName string,
	sourceGID uint32,
	dueAtMs int64,
	frame wire.Frame,
) {
	if sourceGID == 0 {
		panic("cast control requires a source GID")
	}
	rt.pendingSkillFinalizesMu.Lock()
	entry := newPendingSkillFinalize(divisionID, characterName)
	entry.sourceGID = sourceGID
	entry.dueAtMs = dueAtMs
	entry.frame = frame
	rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, entry)
	rt.pendingSkillFinalizesMu.Unlock()
}

/*
==================
queueSkillCastClose

queueSkillCastClose schedules the mode-2 close for one accepted offensive
action. A chain root's close is its chain bracket; a server-owned stage
(rootID != 0) extends that bracket to its own action end and opens none:
the client never created a cast for the stage token, so a close on it is
a silent miss while the root would still be cut at its own duration.
==================
*/
func (rt *Runtime) queueSkillCastClose(divisionID, characterName string, sourceGID, token uint32, skill enterworld.SkillRow, rootID uint32, closeAtMs int64) {
	if rootID != 0 {
		rt.extendChainBracket(divisionID, characterName, closeAtMs)
		return
	}
	if sourceGID == 0 {
		panic("cast control requires a source GID")
	}
	rt.pendingSkillFinalizesMu.Lock()
	entry := newPendingSkillFinalize(divisionID, characterName)
	entry.sourceGID = sourceGID
	entry.dueAtMs = closeAtMs
	entry.frame = wire.SkillCastFinalizeFrame(token)
	entry.chainBracket = skill.ChainNext != 0
	rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, entry)
	rt.pendingSkillFinalizesMu.Unlock()
}

/*
==================
queueDetachedCastClose

The closing B505 of a released action that no longer owns its caster
(self effects, planted traps). It routes through the caster's scope but is
marked as presentation-only, so hasOpenSkillCast stays false. Keep the real
session owner so disconnect and world re-entry retire the queued packet.
==================
*/
func (rt *Runtime) queueDetachedCastClose(divisionID, characterName string, sourceGID, token uint32, closeAtMs int64) {
	if sourceGID == 0 {
		panic("action: detached cast close has no source")
	}
	rt.pendingSkillFinalizesMu.Lock()
	entry := newPendingSkillFinalize(divisionID, characterName)
	entry.sourceGID = sourceGID
	entry.dueAtMs = closeAtMs
	entry.frame = wire.SkillCastFinalizeFrame(token)
	entry.presentationOnly = true
	rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, entry)
	rt.pendingSkillFinalizesMu.Unlock()
}

/*
==================
extendChainBracket

extendChainBracket keeps the root deco alive through the stage that was
just accepted. A bracket already drained stays closed; its stages then
resolve as native temporary results, exactly as a late linked B245 would.
==================
*/
func (rt *Runtime) extendChainBracket(divisionID, characterName string, closeAtMs int64) {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	owner := simulation.WorldKey(divisionID, characterName)
	for i := range rt.pendingSkillFinalizes {
		pending := &rt.pendingSkillFinalizes[i]
		if pending.chainBracket && pending.ownerKey == owner {
			pending.dueAtMs = max(pending.dueAtMs, closeAtMs)
		}
	}
}

/*
==================
clearSkillFinalizes

clearSkillFinalizes retires the server-owned B245 action brackets for one
disconnected character. A queued B505 belongs to that session lifecycle:
retaining it can block a quick reconnect and later broadcast a stale close.
==================
*/
func (rt *Runtime) clearSkillFinalizes(divisionID, characterName string) {
	// Every re-entry path resets the client's action latch. Forget the old
	// publication here so the first action in the new world must arm it again.
	rt.actionSessions.Delete(simulation.WorldKey(divisionID, characterName))
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()

	ownerKey := simulation.WorldKey(divisionID, characterName)
	delete(rt.currentSkillCommands, ownerKey)
	monsters := rt.pendingMonsterCasts[:0]
	for _, pending := range rt.pendingMonsterCasts {
		if !pending.selfEffect && simulation.WorldKey(pending.division, pending.characterName) == ownerKey {
			name := monsterCastOwner(pending.instance.Gid)
			entry := newPendingSkillFinalize(pending.division, name)
			entry.sourceGID = pending.instance.Gid
			entry.frame = wire.SkillCastFinalizeFrame(pending.token)
			rt.pendingSkillFinalizes = append(rt.pendingSkillFinalizes, entry)
		} else {
			monsters = append(monsters, pending)
		}
	}
	rt.pendingMonsterCasts = monsters
	projectiles := rt.pendingProjectileCasts[:0]
	for _, pending := range rt.pendingProjectileCasts {
		if pending.ownerKey != ownerKey {
			projectiles = append(projectiles, pending)
		}
	}
	rt.pendingProjectileCasts = projectiles
	kept := rt.pendingSkillFinalizes[:0]
	for _, pending := range rt.pendingSkillFinalizes {
		if pending.ownerKey != ownerKey {
			kept = append(kept, pending)
		}
	}
	rt.pendingSkillFinalizes = kept
}

/*
==================
openSkillCastOwnerSnapshot

openSkillCastOwnerSnapshot captures action ownership at the start of one
authoritative simulation-tick transaction. A due finalize may remove the live
queue entry later in that transaction, but repeat admission must not observe
that internal mutation until the next tick: clients need the published B505
release before another B245 can acquire motion-state ownership.

The value reports whether the owner's casting instance is open. Only that
gates a server-owned chain stage (see chainStageBlocked); every entry gates
a new command.
==================
*/
func (rt *Runtime) openSkillCastOwnerSnapshot() map[string]bool {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()

	owners := make(map[string]bool, len(rt.pendingSkillFinalizes)+len(rt.currentSkillCommands)+len(rt.pendingProjectileCasts))
	for _, pending := range rt.pendingSkillFinalizes {
		if !pending.presentationOnly {
			owners[pending.ownerKey] = false
		}
	}
	for owner := range rt.currentSkillCommands {
		owners[owner] = true
	}
	for _, pending := range rt.pendingProjectileCasts {
		owners[pending.ownerKey] = true
	}
	return owners
}

/*
==================
drainSkillFinalizes

drainSkillFinalizes emits due controls exactly once, retaining their source
and queue order. Only adjacent controls with the same route may coalesce.
A chain bracket is additionally held while its chain command is live: the
next step becomes due exactly when the previous step's action ends, so a
deadline alone would close the root deco between two stages.
==================
*/
func (rt *Runtime) drainSkillFinalizes(nowMs int64) []simulation.DivisionFrames {
	return rt.drainSelectedSkillFinalizes(nowMs, false)
}

/*
================
drainSelectedSkillFinalizes

Monster controls may settle before AI while player controls retain tick order.
================
*/
func (rt *Runtime) drainSelectedSkillFinalizes(nowMs int64, monstersOnly bool) []simulation.DivisionFrames {
	chains := rt.liveChainOwners()
	rt.pendingSkillFinalizesMu.Lock()
	if len(rt.pendingSkillFinalizes) == 0 {
		rt.pendingSkillFinalizesMu.Unlock()
		return nil
	}

	kept := rt.pendingSkillFinalizes[:0]
	var out []simulation.DivisionFrames
	for _, pending := range rt.pendingSkillFinalizes {
		if monstersOnly && !strings.HasPrefix(pending.characterName, "@monster:") {
			kept = append(kept, pending)
			continue
		}
		_, held := chains[pending.ownerKey]
		if pending.dueAtMs > nowMs || (pending.chainBracket && held) {
			kept = append(kept, pending)
			continue
		}
		routeIndex := len(out)
		out = append(out, simulation.DivisionFrames{DivisionID: pending.divisionID, SourceGID: pending.sourceGID})
		out[routeIndex].Frames = append(out[routeIndex].Frames, simulation.Frame{
			Opcode:  pending.frame.Opcode,
			Payload: pending.frame.Payload,
			Current: pending.frame.Current,
			Scope:   pending.frame.Scope,
		})
	}
	rt.pendingSkillFinalizes = kept
	rt.pendingSkillFinalizesMu.Unlock()

	return coalesceDivisionFrames(out)
}
