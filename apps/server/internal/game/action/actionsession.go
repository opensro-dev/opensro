/*
===========================================================================

actionsession.go - publish the shared native object-action lifetime

4AD270 writes the actor's command count, not a pickup discriminator. This
owner retains the pending back command and private queue publications. The
existing continuation and cast owners execute commands and commit gameplay.

===========================================================================
*/
package action

import (
	"bytes"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	singleActionCount = 1
	pairedActionCount = 2
)

/*
================
deferredObjectAction

The immutable decoded-shape request waits behind the executing front. Costs,
range and target lifetime are checked by the ordinary owner when dequeued.
================
*/
type deferredObjectAction struct {
	payload []byte
}

/*
================
actionQueueCount

4AD270 always reports the remaining queue, including when an independent
effect-cancellation command completes during combat or pickup approach.
================
*/
func (rt *Runtime) actionQueueCount(division, name string) uint8 {
	if value, exists := rt.actionSessions.Load(simulation.WorldKey(division, name)); exists && value.(actionSessionPublication).queued {
		return pairedActionCount
	}
	_, intent := rt.combatIntentFor(division, name)
	_, pickup := rt.Pending.Peek(grounditem.PendingKey(division, name))
	if intent || pickup || rt.hasOpenSkillCast(division, name) {
		return singleActionCount
	}
	return 0
}

/*
================
queueObjectAction

4AD630 keeps one pending back entry during EXECUTE/KEEP_UP. Apply that rule
before individual skill owners: otherwise heals, buffs and Trace silently
refuse a busy actor while offensive skills queue. Instant imbues bypass it.

The same entry holds a skill press that arrives within cooldownGraceMs of
the skill becoming ready, cast or no cast: it waits for readiness instead of
being refused (a deliberate deviation, see cooldownGraceMs).
================
*/
func (rt *Runtime) queueObjectAction(division string, c *enterworld.Character, payload []byte) (OpResult, bool) {
	if c == nil {
		return OpResult{}, false
	}
	casting := rt.hasOpenSkillCast(division, c.Name)
	// Only a skill press can be grace-queued; anything else with no cast open
	// goes straight on without the snapshot.
	if !casting && wire.ClassifyTargetActionLane(payload) != wire.TargetActionSkill {
		return OpResult{}, false
	}
	snapshot := rt.characterSnapshot(division, c)
	readyAtMs := rt.graceReadyAtMs(snapshot, payload)
	if !casting && readyAtMs == 0 {
		return OpResult{}, false
	}
	switch wire.ClassifyTargetActionLane(payload) {
	case wire.TargetActionBasicAttack:
		if _, err := wire.DecodeBasicAttackEngage(payload); err != nil {
			return OpResult{}, false
		}
	case wire.TargetActionFollow:
		if _, err := wire.DecodeFollowTarget(payload); err != nil {
			return OpResult{}, false
		}
	case wire.TargetActionGroundItemPickup:
		if _, err := wire.DecodeTargetInteract(payload); err != nil {
			return OpResult{}, false
		}
	case wire.TargetActionSkill:
		cast, err := wire.DecodeSkillAction(payload)
		if err != nil || rt.deps.SkillData() == nil {
			return OpResult{}, false
		}
		skill, exists := rt.deps.SkillData().SkillByID(cast.ActionId)
		if !exists || skill.InstantSelfEffectPinned || skill.Imbue.Pinned {
			return OpResult{}, false
		}
	default:
		return OpResult{}, false
	}
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || snapshot.NativeTeleportMode != 0 || mountedOnCOS(snapshot) {
		return OpResult{}, false
	}
	// 4ACC40 validates a skill command (mask 0x37, call at 4ACED4) before it
	// may enter the queue. A refused press (cooldown, MP, weapon, ammunition)
	// answers its error and leaves the running attack and its continuation in
	// place; queueing it unchecked replaced the auto-attack with a command
	// that was then refused when the swing closed, and every attack stopped.
	// A press inside the grace window is admitted without its cooldown; the
	// release replays it through full admission once it is ready.
	mask := admitCommand
	if readyAtMs != 0 {
		mask &^= admitCooldown
	}
	if code := rt.queuedSkillAdmission(division, snapshot, payload, mask); code != 0 {
		return offensiveRefusal(code), true
	}
	pending := basicAttackIntent{
		DivisionID: division, CharacterName: c.Name, SingleCast: true,
		Deferred: &deferredObjectAction{payload: bytes.Clone(payload)},
	}
	rt.actionSessions.Store(simulation.WorldKey(division, c.Name), actionSessionPublication{
		division: division, name: c.Name, characterID: c.ID, queued: true, pending: &pending, readyAtMs: readyAtMs,
	})
	state := wire.ArmActionState()
	state.State = pairedActionCount
	frame := wire.Frame{Opcode: wire.OpActionState, Payload: state.Encode()}
	return OpResult{Frames: []wire.Frame{frame}, ActorPrivate: []wire.Frame{frame}}, true
}

/*
================
queuedSkillAdmission

The command-phase admission of a skill press about to be queued; zero for
anything else (attack, follow, pickup) and for the base attack, which
4ACC40 sends straight to the queue.
================
*/
func (rt *Runtime) queuedSkillAdmission(division string, snapshot *enterworld.Character, payload []byte, mask admitMask) uint16 {
	skill, ok := rt.queuedSkill(snapshot, payload)
	if !ok {
		return 0
	}
	return rt.skillAdmission(division, snapshot, skill, rt.Now().UnixMilli(), nil, nil, mask)
}

/*
================
queuedSkill

The skill a command payload presses, unless it is not a skill press or is
the base attack (4ACC40 queues that unchecked).
================
*/
func (rt *Runtime) queuedSkill(snapshot *enterworld.Character, payload []byte) (enterworld.SkillRow, bool) {
	if snapshot == nil || rt.deps.SkillData() == nil || wire.ClassifyTargetActionLane(payload) != wire.TargetActionSkill {
		return enterworld.SkillRow{}, false
	}
	cast, err := wire.DecodeSkillAction(payload)
	if err != nil {
		return enterworld.SkillRow{}, false
	}
	skill, exists := rt.deps.SkillData().SkillByID(cast.ActionId)
	if !exists || isPinnedBaseAttack(snapshot, skill.Codename) {
		return enterworld.SkillRow{}, false
	}
	return skill, true
}

/*
================
graceReadyAtMs

When a skill press that is not ready yet will be (skillReadyAtMs), if that
is within cooldownGraceMs; zero for a ready skill, one further off, an
instant imbue (it never queues) and anything that is not a skill press.
================
*/
func (rt *Runtime) graceReadyAtMs(snapshot *enterworld.Character, payload []byte) int64 {
	skill, ok := rt.queuedSkill(snapshot, payload)
	if !ok || skill.InstantSelfEffectPinned || skill.Imbue.Pinned {
		return 0
	}
	now := rt.Now().UnixMilli()
	ready := skillReadyAtMs(snapshot, skill)
	if ready <= now || ready-now > cooldownGraceMs {
		return 0
	}
	return ready
}

/*
================
actionSessionPublication
================
*/
type actionSessionPublication struct {
	division, name string
	characterID    int64
	queued         bool
	pending        *basicAttackIntent
	// readyAtMs holds a grace-queued press until its skill is ready (0: none).
	readyAtMs int64
}

/*
================
actionSessionPrior

Admission may reject after consulting another owner. Preserve both native
queue entries until a replacement has actually been accepted.
================
*/
type actionSessionPrior struct {
	intent      *basicAttackIntent
	publication actionSessionPublication
	published   bool
}

/*
================
captureActionSession
================
*/
func (rt *Runtime) captureActionSession(division string, c *enterworld.Character) actionSessionPrior {
	var prior actionSessionPrior
	if c == nil {
		return prior
	}
	if intent, exists := rt.combatIntentFor(division, c.Name); exists {
		prior.intent = &intent
	}
	if value, exists := rt.actionSessions.Load(simulation.WorldKey(division, c.Name)); exists {
		prior.publication, prior.published = value.(actionSessionPublication), true
	}
	return prior
}

/*
================
combatIntentFor

Read the shared continuation without retaining its mutex across cast/world
owners. Callers serialize mutations with the division operation lock.
================
*/
func (rt *Runtime) combatIntentFor(division, name string) (basicAttackIntent, bool) {
	rt.basicAttackIntentsMu.Lock()
	defer rt.basicAttackIntentsMu.Unlock()
	intent, ok := rt.basicAttackIntents[simulation.WorldKey(division, name)]
	return intent, ok
}

/*
================
publishActionSession

Accepted attack, skill and Trace commands must arm the same latch as pickup.
Do not broadcast B2CD: it changes only the requesting player's interface.
================
*/
func (rt *Runtime) publishActionSession(division string, c *enterworld.Character, result OpResult, previous actionSessionPrior) OpResult {
	if c == nil {
		return result
	}
	current, intent := rt.combatIntentFor(division, c.Name)
	opened := false
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) > 1 && frame.Payload[0] == 1 {
			opened = true
		}
	}
	key := simulation.WorldKey(division, c.Name)
	if !opened && !intent {
		if previous.intent != nil {
			rt.setCombatIntent(*previous.intent)
		}
		if previous.published {
			rt.actionSessions.Store(key, previous.publication)
		}
		return result
	}
	if result.DiagnosticRefusal != "" {
		return result
	}
	if !intent && (!opened || !rt.hasOpenSkillCast(division, c.Name)) {
		return result
	}
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	publication := actionSessionPublication{division: division, name: c.Name, characterID: c.ID}
	publication.queued = !opened && intent && (previous.intent == nil || current != *previous.intent) && rt.hasOpenSkillCast(division, c.Name)
	publication.pending = &current
	if _, published := rt.actionSessions.Load(key); published {
		if !publication.queued {
			return result
		}
	}
	if publication.queued {
		// The executable continuation stays at the front. In particular, a
		// queued replacement must not truncate an already committed chain.
		if previous.intent != nil {
			rt.setCombatIntent(*previous.intent)
		} else {
			rt.finishCombatIntent(division, c.Name)
		}
	} else {
		publication.pending = nil
	}
	rt.actionSessions.Store(key, publication)
	state := wire.ArmActionState()
	if publication.queued {
		state.State = pairedActionCount
	}
	frame := wire.Frame{Opcode: wire.OpActionState, Payload: state.Encode()}
	result.Frames = append(result.Frames, frame)
	result.ActorPrivate = append(result.ActorPrivate, frame)
	return result
}

/*
================
actionSessionSnapshot

Snapshot publication owners in stable order before taking division locks.
================
*/
func (rt *Runtime) actionSessionSnapshot() []actionSessionPublication {
	var sessions []actionSessionPublication
	rt.actionSessions.Range(func(_, value any) bool {
		sessions = append(sessions, value.(actionSessionPublication))
		return true
	})
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].division != sessions[j].division {
			return sessions[i].division < sessions[j].division
		}
		return sessions[i].name < sessions[j].name
	})
	return sessions
}

/*
================
retireActionSessions

Natural and forced termination publish through the same sorted private lane.
================
*/
func (rt *Runtime) retireActionSessions() []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, session := range rt.actionSessionSnapshot() {
		unlock := rt.lockDivision(session.division)
		key := simulation.WorldKey(session.division, session.name)
		_, intent := rt.combatIntentFor(session.division, session.name)
		if value, exists := rt.actionSessions.Load(key); exists && !rt.hasOpenSkillCast(session.division, session.name) {
			current := value.(actionSessionPublication)
			// A grace-queued press stays queued until its skill is ready;
			// advanceQueuedActionSessions promotes it then.
			if current.queued && current.pending != nil && rt.Now().UnixMilli() < current.readyAtMs {
				unlock()
				continue
			}
			state := wire.ReleaseActionState()
			if current.queued && current.pending != nil {
				// 4AD390: the executing front finished (or was cancelled) in
				// this tick; the pending back entry becomes the new front.
				// Dropping it here lost an attack clicked during a swing.
				rt.setCombatIntent(*current.pending)
				current.queued, current.pending, current.readyAtMs = false, nil, 0
				rt.actionSessions.Store(key, current)
				state.State = singleActionCount
			} else if intent {
				if !current.queued {
					unlock()
					continue
				}
				current.queued, current.pending, current.readyAtMs = false, nil, 0
				rt.actionSessions.Store(key, current)
				state.State = singleActionCount
			} else {
				rt.actionSessions.Delete(key)
			}
			out = append(out, simulation.DivisionFrames{
				DivisionID: session.division, OnlyCharacterID: session.characterID,
				Frames: []simulation.Frame{{Opcode: wire.OpActionState, Payload: state.Encode()}},
			})
		}
		unlock()
	}
	return out
}

/*
================
objectActionCommitted

4ACC10 reads the executing front command's commitment flag. Cancellation
and mount admission must retain the same linked-skill lifetime, including
intervals between positive-time stages.
================
*/
func (rt *Runtime) objectActionCommitted(division, name string) bool {
	intent, exists := rt.combatIntentFor(division, name)
	open := rt.hasOpenSkillCast(division, name)
	return (!exists && open) || (intent.SingleCast && (intent.NextActionMs != 0 || intent.ComboRootID != 0) && open)
}

/*
================
cancelObjectAction

4ACC40 permits cancelling a normal attack but preserves a skill once its
execute phase or linked-chain commitment is active. Forced abnormal-status
interruption continues to use cancelPreparingProjectile directly.
================
*/
func (rt *Runtime) cancelObjectAction(division string, character, snapshot *enterworld.Character) OpResult {
	key := simulation.WorldKey(division, snapshot.Name)
	if value, published := rt.actionSessions.Load(key); published {
		publication := value.(actionSessionPublication)
		if publication.queued {
			// 4ACCE4 removes only the pending back entry. The executing cast
			// and its original repeating/linked continuation keep ownership.
			publication.queued, publication.pending, publication.readyAtMs = false, nil, 0
			rt.actionSessions.Store(key, publication)
			state := wire.ReleaseActionState()
			state.State = singleActionCount
			return OpResult{Frames: []wire.Frame{{Opcode: wire.OpActionState, Payload: state.Encode()}}}
		}
	}
	if rt.objectActionCommitted(division, snapshot.Name) {
		// Inference across versions: preserve 0x4004's low error byte for
		// 75BAF5's one-byte category-0x19 reader. Code 4 is silent in v1.150.
		const committedActionNotice = 4
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpActionState, Payload: wire.NoticeActionState(singleActionCount, committedActionNotice).Encode()}}}
	}
	from := rt.liveSpawn(key, snapshot, rt.Now().UnixMilli())
	stopped, _ := rt.enterBasicAttackRange(character, snapshot, key, from, rt.Now().UnixMilli())
	rt.ClearCombatIntent(division, snapshot.Name)
	closed := rt.cancelPreparingProjectile(division, snapshot.Name)
	rt.actionSessions.Delete(key)
	return prependOpResult(stopped, OpResult{
		Broadcast: closed,
		Frames:    append(closed, wire.Frame{Opcode: wire.OpActionState, Payload: wire.ReleaseActionState().Encode()}),
	})
}

/*
================
advanceQueuedActionSessions

4AD390 retires the executing front before the pending command may run. Keep
the existing cast/chain owner active until its final close, then install the
replacement through the normal continuation owner and publish count one.
4AED19's authored skill-to-basic handoff is also a new command. Publish its
release so a movement cancellation refused by the skill can cancel the basic.
================
*/
func (rt *Runtime) advanceQueuedActionSessions(nowMs int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, session := range rt.actionSessionSnapshot() {
		key := simulation.WorldKey(session.division, session.name)
		unlock := rt.lockDivision(session.division)
		latest, exists := rt.actionSessions.Load(key)
		if !exists {
			unlock()
			continue
		}
		session = latest.(actionSessionPublication)
		if rt.hasOpenSkillCast(session.division, session.name) || nowMs < session.readyAtMs {
			unlock()
			continue
		}
		if !session.queued {
			intent, retained := rt.combatIntentFor(session.division, session.name)
			if !retained || !intent.ResumeBasic || intent.ResumeNotified || nowMs <= intent.NextActionMs {
				unlock()
				continue
			}
			intent.ResumeNotified = true
			session.pending = &intent
		}
		if session.pending == nil {
			unlock()
			continue
		}
		rt.setCombatIntent(*session.pending)
		session.queued, session.pending, session.readyAtMs = false, nil, 0
		rt.actionSessions.Store(key, session)
		state := wire.ReleaseActionState()
		state.State = singleActionCount
		out = append(out, simulation.DivisionFrames{DivisionID: session.division, OnlyCharacterID: session.characterID, Frames: []simulation.Frame{{Opcode: wire.OpActionState, Payload: state.Encode()}}})
		unlock()
	}
	return out
}

/*
================
discardQueuedAction

Movement and forced interruption cancel the pending replacement as well as
the current continuation. Normal front completion uses finishCombatIntent.
================
*/
func (rt *Runtime) discardQueuedAction(division, name string) {
	key := simulation.WorldKey(division, name)
	if value, exists := rt.actionSessions.Load(key); exists {
		session := value.(actionSessionPublication)
		session.pending = nil
		rt.actionSessions.Store(key, session)
	}
}
