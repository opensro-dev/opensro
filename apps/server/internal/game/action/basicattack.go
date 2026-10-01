/*
===========================================================================

basicattack.go - basic attacks and the attack intent loop

Own pursuit and linked attack progression so command replacement, range checks,
and stage execution share one simulation order.

===========================================================================
*/

package action

import (
	"math"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// defaultUnarmedActionRange is the v1.150 bare-hand reach in native world
// units. SKILL_PUNCH_01 deliberately has action-range column 0 because the
// player base-attack family obtains reach from its weapon class; the empty
// weapon class uses the same 6-unit reach carried by every shipped one-hand,
// spear, dagger, staff, wand, and harp weapon row. Zero-range bow/crossbow
// basics use their item range; EU caster basics override it with skill range.
const defaultUnarmedActionRange = 6

// Research server 004B04CA: target-range navigation has a 100 ms floor.
// Waiting 500 ms (or for a reach-relative drift threshold) lets short legs
// expire and repeatedly strands the actor outside live attack range.
// The simulation owns scheduling; this owner only gates goal publication.
const attackPursuitResteerMinMs = 100

const attackPursuitPositionEpsilon = 0.01

/*
==================
skillActionReach

skillActionReach resolves the action before its equipment fallback. Native
4ADA8C..4ADAB8 uses the skill's nonzero range for basic and advanced casts;
only zero calls the equipped/unarmed range resolver. Keep pursuit and hit
admission on this same rule (EU caster basic skills author 150, not zero).

4ADAB8..4ADB4E (and 4AE87E..4AE912 for casts) then add the caster's CBRA
and WIRU values when the row asks for them (getv +0x50C, +0x4E8), and cut
the sum by keeper param 0xB7 percent. Each step is stored as a float.
==================
*/
func skillActionReach(skill enterworld.SkillRow, loadout combat.Loadout, caster combat.Stats) simulation.ActionReach {
	var reach float32
	switch {
	case skill.ActionRange > 0:
		reach = float32(skill.ActionRange)
	case loadout.HasWeapon:
		reach = float32(loadout.ActionRange)
	default:
		reach = defaultUnarmedActionRange
	}
	// 4AE849..4AE87A adds ru only on the equipment fallback, before getv
	// bonuses and the range-reduction keeper. It is a distance, not a rate.
	if skill.ActionRange == 0 && skill.BuffModifiers.Ru {
		reach = float32(float64(reach) + float64(skill.BuffModifiers.RuRate))
	}
	for _, slot := range [...]enterworld.SkillParameter{enterworld.ParameterCrossbowRange, enterworld.ParameterWizardRange} {
		if skill.Attack.Parameters.Has(slot) {
			reach = float32(float64(reach) + float64(caster.SkillParameters[slot]))
		}
	}
	cut, _ := caster.Param(0xb7)
	return reducedActionReach(reach, cut)
}

/*
================
reducedActionReach

4ADAB8..4ADB4E applies Myopia after the skill and keeper range contributions.
Players, monsters and their COS targets share the same float store boundary.
================
*/
func reducedActionReach(reach, reduction float32) simulation.ActionReach {
	return simulation.ActionReach(float32((1 - float64(reduction)/100) * float64(reach)))
}

/*
================
playerActionReach

Resolve range against the complete keeper, including effects and abnormals.
================
*/
func (rt *Runtime) playerActionReach(division string, c *enterworld.Character, skill enterworld.SkillRow, loadout combat.Loadout) simulation.ActionReach {
	caster, _, err := rt.playerCombatStats(division, c)
	if err != nil {
		return 0
	}
	return skillActionReach(skill, loadout, caster)
}

/*
==================
basicAttackIntent

basicAttackIntent is the authoritative continuation of one native engage
command. The wire command chooses the target once; subsequent ticks keep
approaching and striking until a superseding command or invalid state
clears the intent. SupportCast walks into reach for one support cast.
FollowTarget instead retains player pursuit without opening combat. All
three share one slot so a superseding command cannot leave a second owner.
==================
*/
type basicAttackIntent struct {
	Deferred      *deferredObjectAction // immutable command admitted behind a committed cast
	FollowTarget  bool                  // persistent player pursuit, with no combat action
	FollowSession uint64                // prevents a reconnected target inheriting old pursuit
	SupportCast   bool                  // a player-targeted heal, cure or resurrection waiting for reach
	CaptureCast   bool                  // a Monster Mask waiting to reach its corpse
	SingleCast    bool                  // executes the explicit sequence before any authored basic continuation
	ResumeBasic   bool                  // transition resolves the current weapon after the explicit action closes
	ComboRootID   uint32                // nonzero only after a root stage commits; never supplied by the client
	DivisionID    string
	CharacterName string
	TargetGid     uint32
	SkillID       uint32
	ActionReach   simulation.ActionReach
	CooldownMs    int64
	NextActionMs  int64
	// ChainLatencyConsumedMs is how much of the command's native 500 ms chain
	// latency budget (record+28, reset by CActionRecord_Release 4AC954) the
	// chain steps have consumed. ChainLatencyUsedMs mirrors record+2C, which
	// grows only when a step outlasts the remaining budget (4AEC48).
	ChainLatencyConsumedMs int64
	ChainLatencyUsedMs     int64

	// ApproachTargetSample is the live target pose from which the most recent
	// player movement goal was authored. It belongs to this attack intent; the
	// monster's private movement-plan destination must never cross this
	// boundary. HasApproach records that a goal was issued even after the
	// player reaches it, so a slowly moving target cannot restart one tiny leg
	// per simulation tick.
	ApproachTargetSample simulation.Spawn
	ApproachIssuedAtMs   int64
	HasApproach          bool
}

// chainLatencyBudgetMs is the per-command allowance native chain steps take
// out of their waits so client latency does not stretch the authored combo.
const chainLatencyBudgetMs = 500

/*
==================
spendChainLatency

spendChainLatency returns how long a chain step holds before the next step
may execute. Native EXECUTE (4AEC2D..4AEC62) waits the step's action
duration (ref+78, column 13), NOT casting+duration, after first spending
what remains of the 500 ms budget. The wait is measured from the step's
own execution; KEEP_UP (4AECDA) compares strictly, hence the caller's +1.
==================
*/
func (intent *basicAttackIntent) spendChainLatency(durationMs int64) int64 {
	budget := chainLatencyBudgetMs - intent.ChainLatencyConsumedMs
	if budget <= 0 {
		return durationMs
	}
	if durationMs > budget {
		intent.ChainLatencyUsedMs += budget
		intent.ChainLatencyConsumedMs = chainLatencyBudgetMs
		return durationMs - budget
	}
	intent.ChainLatencyConsumedMs += durationMs
	return 0
}

/*
================
ClearCombatIntent

Movement, actor teardown and explicit cancellation retire the same command
slot. Follow shares this lifetime so it cannot resume after a new command.
================
*/
func (rt *Runtime) ClearCombatIntent(divisionID, characterName string) {
	rt.finishCombatIntent(divisionID, characterName)
	rt.discardQueuedAction(divisionID, characterName)
}

/*
================
finishCombatIntent

Retire the executing front without discarding an independently queued next
command. The action-session owner promotes it after the current cast closes.
================
*/
func (rt *Runtime) finishCombatIntent(divisionID, characterName string) {
	rt.basicAttackIntentsMu.Lock()
	delete(rt.basicAttackIntents, simulation.WorldKey(divisionID, characterName))
	rt.basicAttackIntentsMu.Unlock()
}

/*
================
setCombatIntent
================
*/
func (rt *Runtime) setCombatIntent(intent basicAttackIntent) {
	rt.basicAttackIntentsMu.Lock()
	rt.basicAttackIntents[simulation.WorldKey(intent.DivisionID, intent.CharacterName)] = intent
	rt.basicAttackIntentsMu.Unlock()
}

/*
================
combatIntentIsCurrent

The tick must not resurrect a command replaced after its snapshot.
================
*/
func (rt *Runtime) combatIntentIsCurrent(intent basicAttackIntent) bool {
	rt.basicAttackIntentsMu.Lock()
	defer rt.basicAttackIntentsMu.Unlock()
	current, ok := rt.basicAttackIntents[simulation.WorldKey(intent.DivisionID, intent.CharacterName)]
	return ok && current == intent
}

/*
================
combatIntentSnapshot

Release the intent mutex before callers enter character or world authority.
================
*/
func (rt *Runtime) combatIntentSnapshot() []basicAttackIntent {
	rt.basicAttackIntentsMu.Lock()
	out := make([]basicAttackIntent, 0, len(rt.basicAttackIntents))
	for _, intent := range rt.basicAttackIntents {
		out = append(out, intent)
	}
	rt.basicAttackIntentsMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].DivisionID != out[j].DivisionID {
			return out[i].DivisionID < out[j].DivisionID
		}
		return strings.ToLower(out[i].CharacterName) < strings.ToLower(out[j].CharacterName)
	})
	return out
}

/*
================
liveChainOwners

Keep the root bracket open while another server-owned chain stage remains.
================
*/
func (rt *Runtime) liveChainOwners() map[string]struct{} {
	rt.basicAttackIntentsMu.Lock()
	defer rt.basicAttackIntentsMu.Unlock()
	owners := make(map[string]struct{})
	for key, intent := range rt.basicAttackIntents {
		if intent.ComboRootID != 0 && !intent.ResumeBasic {
			owners[key] = struct{}{}
		}
	}
	return owners
}

/*
================
findCharacter

Prefer indexed authority lookup; the roster fallback serves small test stores.
================
*/
func (rt *Runtime) findCharacter(divisionID, characterName string) *enterworld.Character {
	if source, ok := rt.deps.(domain.CharacterLookup); ok {
		return source.CharacterByName(divisionID, characterName)
	}
	for _, candidate := range rt.deps.CharactersForDivision(divisionID) {
		if candidate != nil && strings.EqualFold(candidate.Name, characterName) {
			return candidate
		}
	}
	return nil
}

/*
==================
resolveBasicAttack

resolveBasicAttack binds the current equipped weapon to the learned racial
base row. It deliberately resolves on every strike: equipment, death, and
skill state can change after the double-click and are authority inputs, not
properties cached forever by a UI gesture.
==================
*/
func (rt *Runtime) resolveBasicAttack(character *enterworld.Character) (enterworld.SkillRow, combat.Loadout, string) {
	var zeroSkill enterworld.SkillRow
	var zeroLoadout combat.Loadout
	if !enterworld.CharacterAlive(character) {
		return zeroSkill, zeroLoadout, "character-not-alive"
	}
	base, loadout, err := combat.PlayerStats(character, rt.statCatalogs())
	if err != nil {
		return zeroSkill, zeroLoadout, "character-loadout-invalid"
	}
	source := rt.deps.SkillData()
	if source == nil {
		return zeroSkill, zeroLoadout, "skill-data-unavailable"
	}
	if id := rt.transformAttackSkill(character); id != 0 {
		skill, ok := source.SkillByID(id)
		actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
		if !ok || !skill.CombatPinned || !skill.Attack.Present || !skill.TargetRequired ||
			!actionLifecyclePinned || actionLifecycleMs == 0 || !skill.TimingPinned || skill.CoolTimeMs == 0 {
			return zeroSkill, zeroLoadout, "transform-attack-unavailable"
		}
		if skillActionReach(skill, loadout, base) <= 0 {
			return zeroSkill, zeroLoadout, "attack-range-unavailable"
		}
		return skill, loadout, ""
	}
	for _, skillID := range character.Skills {
		skill, ok := source.SkillByID(skillID)
		actionLifecycleMs, actionLifecyclePinned := skill.ActionLifecycleMs()
		if !ok || !skill.CombatPinned || !skill.Attack.Present ||
			!skill.TargetRequired || !actionLifecyclePinned || actionLifecycleMs == 0 ||
			!skill.TimingPinned || skill.CoolTimeMs == 0 ||
			!isPinnedBaseAttack(character, skill.Codename) ||
			!loadoutMatchesSkill(loadout, skill.RequiredWeaponKinds) {
			continue
		}
		if skillActionReach(skill, loadout, base) <= 0 {
			return zeroSkill, zeroLoadout, "attack-range-unavailable"
		}
		return skill, loadout, ""
	}
	return zeroSkill, zeroLoadout, "compatible-base-attack-unavailable"
}

/*
================
beginBasicAttack

Run the whole transition at the caller's one simulation instant.
================
*/
func (rt *Runtime) beginBasicAttack(divisionID string, character *enterworld.Character, engage wire.BasicAttackEngage, nowMs int64) OpResult {
	if character == nil || rt.Monsters == nil || engage.TargetGid == 0 {
		return OpResult{DiagnosticRefusal: "attack-context-unavailable"}
	}
	snapshot := rt.characterSnapshot(divisionID, character)
	if snapshot == nil || snapshot.DeletePending {
		return OpResult{DiagnosticRefusal: "character-unavailable"}
	}
	skill, loadout, refusal := rt.resolveBasicAttack(snapshot)
	if refusal != "" {
		return OpResult{DiagnosticRefusal: refusal}
	}
	target, ok := rt.characterMonster(divisionID, snapshot, engage.TargetGid)
	if !ok {
		return OpResult{DiagnosticRefusal: "target-unavailable"}
	}
	if target.CurrentHP == 0 {
		return OpResult{DiagnosticRefusal: "target-dead"}
	}
	intent := basicAttackIntent{
		DivisionID: divisionID, CharacterName: snapshot.Name,
		TargetGid: engage.TargetGid, SkillID: skill.ID,
		ActionReach: rt.playerActionReach(divisionID, snapshot, skill, loadout), CooldownMs: int64(rt.playerSkillCooldown(divisionID, snapshot, skill)),
	}
	rt.setCombatIntent(intent)
	return rt.advanceBasicAttackIntent(character, intent, nowMs)
}

/*
================
advanceBasicAttackIntent

Dispatch the command owner before resolving any combat skill or cost.
================
*/
func (rt *Runtime) advanceBasicAttackIntent(character *enterworld.Character, intent basicAttackIntent, nowMs int64) OpResult {
	if intent.Deferred != nil {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return rt.handleTargetInteractLocked(intent.DivisionID, character, intent.Deferred.payload)
	}
	if intent.FollowTarget {
		return rt.advanceFollowIntent(character, intent, nowMs)
	}
	if intent.SupportCast {
		return rt.advanceSupportCastIntent(character, intent, nowMs)
	}
	if intent.CaptureCast {
		return rt.advanceCaptureIntent(character, intent, nowMs)
	}
	snapshot := rt.characterSnapshot(intent.DivisionID, character)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	// Admission must precede pursuit and the movement-to-combat handoff. The
	// central cast gate in acceptSkillCastAt protects direct skill requests,
	// but reaching it only after approach would let a seated double-click move
	// the character before the eventual cast refusal.
	if rt.skillCastPostureBlocked(intent.DivisionID, snapshot, nowMs) {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	// CGObjChar_CheckTargetAttackable (5291D0), reached from
	// CGObjPC_CanAttackTarget (52BF90, vtable +0x62C): an untouchable attacker
	// (body mode 2) may not attack anything.
	if snapshot.NativeBodyStatus == untouchableBodyStatus {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return offensiveRefusal(0x3020)
	}
	var skill enterworld.SkillRow
	var loadout combat.Loadout
	var refusal string
	if intent.ResumeBasic {
		if nowMs <= intent.NextActionMs || rt.hasOpenSkillCast(intent.DivisionID, intent.CharacterName) {
			return OpResult{}
		}
		// 4AED19..4AEDD0 transfers the target to a new basic command. Resolve
		// its weapon/learned row now, never repeat or re-charge the old skill.
		basic, _, why := rt.resolveBasicAttack(snapshot)
		if why != "" {
			rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
			return OpResult{DiagnosticRefusal: why}
		}
		intent.SingleCast, intent.ResumeBasic = false, false
		intent.ComboRootID, intent.SkillID = 0, basic.ID
		intent.ChainLatencyConsumedMs, intent.ChainLatencyUsedMs = 0, 0
	}
	if intent.SingleCast {
		if intent.ComboRootID == 0 {
			skill, loadout, refusal = rt.resolveOffensiveSkill(snapshot, intent.SkillID)
		} else {
			skill, loadout, refusal = rt.resolveOffensiveStage(snapshot, intent.ComboRootID, intent.SkillID)
		}
		if refusal == "" && intent.ComboRootID == 0 {
			if _, code := rt.offensiveCost(intent.DivisionID, snapshot, skill, nowMs); code != 0 {
				rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
				return offensiveRefusal(code)
			}
		}
	}
	if !intent.SingleCast {
		skill, loadout, refusal = rt.resolveBasicAttack(snapshot)
	}
	if refusal != "" || skill.ID != intent.SkillID {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		if refusal == "" {
			refusal = "base-attack-changed"
		}
		if intent.SingleCast {
			return rt.offensiveAdmissionRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: refusal}
	}
	target, ok := rt.characterMonster(intent.DivisionID, snapshot, intent.TargetGid)
	if !ok || target.CurrentHP == 0 {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	mover, ok := rt.Monsters.Mover(intent.DivisionID, target.Gid)
	if !ok {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	targetPose := mover.LivePoseAt(nowMs, nil)
	targetSpawn := simulation.Spawn{RegionID: targetPose.RegionID, X: targetPose.X, Y: targetPose.Y, Z: targetPose.Z}
	worldKey := simulation.WorldKey(intent.DivisionID, snapshot.Name)
	live := rt.liveSpawn(worldKey, snapshot, nowMs)
	actionReach := rt.playerActionReach(intent.DivisionID, snapshot, skill, loadout)
	spacing, spacingOK := rt.playerToMonsterCombatSpacing(snapshot, target, actionReach)
	if !spacingOK {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{DiagnosticRefusal: "combat-spacing-unavailable"}
	}
	if !spacing.Contains(live, targetSpawn) {
		if intent.ComboRootID != 0 {
			// A broken combo does not pursue and resume an old attack later.
			rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
			return OpResult{}
		}
		// Equipment is an authority input for every continuation tick. Keep the
		// pursuit stop distance in the same freshly-resolved loadout snapshot as
		// the eventual strike; otherwise swapping weapon families while chasing
		// leaves the approach leg bound to the weapon worn at double-click time.
		reachChanged := math.Abs(float64(intent.ActionReach-actionReach)) > attackPursuitPositionEpsilon
		intent.ActionReach = actionReach
		if !reachChanged && !rt.pursuitSteerDue(intent, worldKey, snapshot, targetSpawn, nowMs) {
			return OpResult{}
		}
		return rt.approachIntentTarget(character, snapshot, intent, spacing, live, targetSpawn, nowMs)
	}
	// Entering action range transfers ownership from movement to combat.
	// Native B245 enters motion state 2 and stops local path following; commit
	// the same live point on the authority plane so later range checks and
	// peer corrections cannot continue an obsolete approach behind the attack.
	combatTransition, transitioned := rt.enterBasicAttackRange(
		character,
		snapshot,
		worldKey,
		targetSpawn,
		nowMs,
	)
	if !transitioned {
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	intent.HasApproach = false
	rt.setCombatIntent(intent)
	if nowMs < intent.NextActionMs || rt.actionAdmissionBlocked(intent.DivisionID, intent.CharacterName, intent.ComboRootID != 0) {
		return combatTransition
	}
	result, decision := rt.acceptSkillStageAt(intent.DivisionID, character, snapshot, wire.SkillAction{
		ActionId: skill.ID, HasTarget: true, TargetGid: intent.TargetGid,
	}, nowMs, intent.ComboRootID)
	switch decision {
	case skillCastDeferred:
		// Range and the previous action bracket are transient states. Preserve
		// the target intent; the next simulation tick re-evaluates the live poses.
		return prependOpResult(combatTransition, result)
	case skillCastRefused:
		rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		return prependOpResult(combatTransition, result)
	}
	intent.ActionReach = actionReach
	if intent.SingleCast {
		remaining, alive := rt.Monsters.Get(intent.DivisionID, intent.TargetGid)
		if skill.ChainNext != 0 && alive && remaining.CurrentHP > 0 {
			if intent.ComboRootID == 0 {
				intent.ComboRootID = skill.ID
			}
			intent.SkillID = skill.ChainNext
			intent.NextActionMs = nowMs + intent.spendChainLatency(int64(skill.ActionDurationMs)) + 1
			rt.setCombatIntent(intent)
			return prependOpResult(combatTransition, result)
		}
		if skill.ContinueBasicAttack && alive && remaining.CurrentHP > 0 {
			intent.ResumeBasic = true
			lifetime, _ := skill.ActionLifecycleMs()
			// 4AEC9E..4AECB3: an authored basic-attack continuation also
			// returns the chain latency the steps borrowed.
			intent.NextActionMs = nowMs + int64(lifetime) + intent.ChainLatencyUsedMs
			rt.setCombatIntent(intent)
		} else {
			rt.finishCombatIntent(intent.DivisionID, intent.CharacterName)
		}
		return prependOpResult(combatTransition, result)
	}
	intent.CooldownMs = int64(rt.playerSkillCooldown(intent.DivisionID, snapshot, skill))
	intent.NextActionMs = nowMs + intent.CooldownMs
	rt.setCombatIntent(intent)
	return prependOpResult(combatTransition, result)
}

/*
==================
pursuitSteerDue

Whether a pursuing intent may publish a new approach goal. Never replace a
young goal. Once the packet floor expires, an in-flight leg is replaced
only for changed target geometry. A leg that has already settled may be
replaced from the current sample even for smaller drift: live range already
proved the actor is out of reach, so suppressing it forever would strand
the engage.
==================
*/
func (rt *Runtime) pursuitSteerDue(intent basicAttackIntent, worldKey string, snapshot *enterworld.Character, target simulation.Spawn, nowMs int64) bool {
	if !intent.HasApproach {
		return true
	}
	world := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(snapshot) })
	approachInFlight := world.MoveSegment.Valid() && nowMs < world.MoveSegment.ArrivesAtMs
	targetMoved := simulation.WorldDistance2D(intent.ApproachTargetSample, target) > 0.01
	young := nowMs-intent.ApproachIssuedAtMs < attackPursuitResteerMinMs
	return !young && (!approachInFlight || targetMoved)
}

/*
==================
approachIntentTarget

Publishes one approach leg toward the intent's target, a monster or a
player, and records the sample it was authored from.
==================
*/
func (rt *Runtime) approachIntentTarget(character, snapshot *enterworld.Character, intent basicAttackIntent, spacing simulation.CombatSpacing, from, target simulation.Spawn, nowMs int64) OpResult {
	goal, disposition := spacing.ApproachGoal(from, target)
	if disposition == simulation.CombatApproachInvalid {
		rt.ClearCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	if disposition == simulation.CombatApproachHold {
		// Range admission and goal derivation consume the same live sample, so
		// this is only a quantisation/coordinate-boundary no-op. Keep the
		// identity latch; the next authoritative tick will sample again.
		return OpResult{}
	}
	return rt.commitIntentMovement(character, snapshot, intentMovement{
		intent: intent, from: from, target: target, goal: goal, nowMs: nowMs,
	})
}

/*
================
intentMovement

Combat and follow derive different goals but share navigation admission,
authoritative movement commits and actor/peer publication.
================
*/
type intentMovement struct {
	intent             basicAttackIntent
	from, target, goal simulation.Spawn
	nowMs              int64
}

/*
================
commitIntentMovement
================
*/
func (rt *Runtime) commitIntentMovement(character, snapshot *enterworld.Character, move intentMovement) OpResult {
	intent, from, target, goal, nowMs := move.intent, move.from, move.target, move.goal, move.nowMs
	worldKey := simulation.WorldKey(intent.DivisionID, character.Name)
	_, fromOwner := rt.liveNav(worldKey, snapshot, nowMs)
	goal, walk, refusal := rt.constrainWalk(snapshot.Name, from, fromOwner, goal)
	if refusal != nil || simulation.WorldDistance2D(from, goal) < attackPursuitPositionEpsilon {
		// Collision/path ownership can be transient (the target may move
		// back into reach or open a route). It is not a terminal command
		// refusal, so preserve the engage state and re-evaluate next tick.
		return OpResult{}
	}
	var ack []byte
	var runChanged bool
	rt.bindResidentRegion(worldKey, nowMs)
	if !rt.deps.Update(character, "basic-attack-approach", func() bool {
		if character.DeletePending {
			return false
		}
		state := rt.Worlds.Update(simulation.WorldKey(intent.DivisionID, character.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(world *simulation.WorldState) {
				request := simulation.MovementRequest{Mode: simulation.MovementAckDestinationMode,
					RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z}
				mode := world.MovementMode
				if intent.FollowTarget && mode != simulation.RunMode {
					mode = simulation.RunMode
					runChanged = true
				}
				result := simulation.ApplyMove(world, enterworld.ObjectIDForCharacter(character), request, mode, nowMs)
				if result.LiveBefore == from {
					world.CommitWalk(walk.Spans, walk.Rest)
				}
				ack = result.AckPayload
			})
		writeBackWorld(character, state)
		return true
	}) || len(ack) == 0 {
		rt.ClearCombatIntent(intent.DivisionID, intent.CharacterName)
		return OpResult{}
	}
	intent.ApproachTargetSample = target
	intent.ApproachIssuedAtMs = nowMs
	intent.HasApproach = true
	rt.setCombatIntent(intent)
	frames := []wire.Frame{{Opcode: simulation.OpMovementAck, Payload: ack}}
	if runChanged {
		// 4B0800 switches a pursuing walker to run after issuing its goal.
		frames = append(frames, wire.Frame{Opcode: wire.OpObjectStateRefresh, Payload: wire.ObjectStateRefresh{
			Gid: enterworld.ObjectIDForCharacter(character), StateType: wire.StateChannelMove, Value: simulation.RunMode,
		}.Encode()})
	}
	return OpResult{Frames: frames, Broadcast: frames}
}

/*
==================
enterBasicAttackRange

enterBasicAttackRange is the movement-to-combat ownership transfer. It
samples no clock and consumes the same live target pose used by range
admission. In one character-authority update it settles any pursuit leg,
derives the region-aware target bearing, commits that yaw, then publishes
B2F5 before B245. B2F5 owns path halt and peer correction, but the v1.150
client deliberately skips its yaw write for the locally controlled actor.
The stationary B245 that follows omits bit 3 because that bit starts actor
movement. The new client consumes the authoritative heading from B2F5;
original-client local facing requires a separate native-equivalence review.
==================
*/
func (rt *Runtime) enterBasicAttackRange(
	character, snapshot *enterworld.Character,
	worldKey string,
	target simulation.Spawn,
	nowMs int64,
) (OpResult, bool) {
	world := rt.Worlds.Snapshot(worldKey, func() simulation.WorldState { return simulation.SeedWorldState(snapshot) })
	live := world.LiveSpawnAt(nowMs)
	heading, hasHeading := simulation.HeadingFromMovement(live, target)
	needsCorrection := world.MoveSegment.Valid() || (hasHeading && live.Angle != heading)
	if !needsCorrection {
		return OpResult{}, true
	}
	var committed simulation.Spawn
	accepted := rt.deps.Update(character, "basic-attack-enter-range", func() bool {
		if character.DeletePending {
			return false
		}
		state := rt.Worlds.Update(worldKey,
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(current *simulation.WorldState) {
				current.SettleLive(nowMs)
				if nextHeading, ok := simulation.HeadingFromMovement(current.Spawn, target); ok {
					current.Spawn.Angle = nextHeading
				}
				committed = current.Spawn
			})
		writeBackWorld(character, state)
		return true
	})
	if !accepted {
		return OpResult{}, false
	}
	rt.bindResidentRegion(worldKey, nowMs)
	correction := wire.Frame{
		Opcode: wire.OpObjectSourceCorrection,
		Payload: wire.ObjectSourceCorrection{
			Gid: enterworld.ObjectIDForCharacter(snapshot),
			Position: wire.Position{
				RegionID: committed.RegionID,
				X:        float32(committed.X), Y: float32(committed.Y), Z: float32(committed.Z),
				Heading: committed.Angle,
			},
		}.Encode(),
	}
	return OpResult{Frames: []wire.Frame{correction}, Broadcast: []wire.Frame{correction}}, true
}

/*
==================
prependOpResult

prependOpResult composes two already-ordered authority transitions without
allowing actor and peer routes to drift. Pending is owned by the tail
operation; basic combat transitions never manufacture pending work.
==================
*/
func prependOpResult(prefix, tail OpResult) OpResult {
	return OpResult{
		Frames:       append(append([]wire.Frame{}, prefix.Frames...), tail.Frames...),
		Broadcast:    append(append([]wire.Frame{}, prefix.Broadcast...), tail.Broadcast...),
		ActorPrivate: append(append([]wire.Frame{}, prefix.ActorPrivate...), tail.ActorPrivate...),
		Recipients:   append(append([]RecipientFrames{}, prefix.Recipients...), tail.Recipients...),
		Pending:      tail.Pending,
	}
}

/*
================
advanceBasicAttackIntents

The simulation tick advances detached commands under their division lock,
then routes their results after releasing authority locks.
================
*/
func (rt *Runtime) advanceBasicAttackIntents(nowMs int64, openActionOwners map[string]bool) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, intent := range rt.combatIntentSnapshot() {
		// A chain stage waits only on the casting instance; its root bracket
		// is deliberately still open (see chainStageBlocked).
		casting, ownsOpenAction := openActionOwners[simulation.WorldKey(intent.DivisionID, intent.CharacterName)]
		if ownsOpenAction && (casting || intent.ComboRootID == 0 || intent.ResumeBasic) {
			continue
		}
		unlock := rt.lockDivision(intent.DivisionID)
		// A request can cancel/replace an intent after the tick snapshots it but
		// before this division lock is acquired. Never resurrect that stale cast.
		if !rt.combatIntentIsCurrent(intent) {
			unlock()
			continue
		}
		character := rt.findCharacter(intent.DivisionID, intent.CharacterName)
		result := rt.advanceBasicAttackIntent(character, intent, nowMs)
		unlock()
		if result.DiagnosticRefusal != "" {
			log.Debugf(
				"action: 0x72CD continuation refused for %s: %s",
				intent.CharacterName, result.DiagnosticRefusal,
			)
		}
		if intent.SingleCast && len(result.Broadcast) == 0 {
			if character != nil && len(result.Frames) > 0 {
				private := simulation.DivisionFrames{DivisionID: intent.DivisionID, OnlyCharacterID: character.ID}
				for _, frame := range result.Frames {
					private.Frames = append(private.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
				}
				out = append(out, private)
			}
			continue
		}
		frames := result.Broadcast
		if len(frames) == 0 {
			frames = result.Frames
		}
		if len(frames) == 0 {
			continue
		}
		routed := simulation.DivisionFrames{DivisionID: intent.DivisionID, SourceGID: enterworld.ObjectIDForCharacter(character)}
		for _, frame := range frames {
			routed.Frames = append(routed.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
		}
		out = append(out, routed)
		out = append(out, recipientDivisionFrames(intent.DivisionID, result.Recipients)...)
		if character != nil && len(result.ActorPrivate) > 0 {
			private := simulation.DivisionFrames{
				DivisionID:      intent.DivisionID,
				OnlyCharacterID: character.ID,
			}
			for _, frame := range result.ActorPrivate {
				private.Frames = append(private.Frames, simulation.Frame{
					Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope,
				})
			}
			out = append(out, private)
		}
	}
	return out
}
