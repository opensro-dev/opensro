package monster

import (
	"fmt"
	"math"
)

// MoverEvent is one explicit cause of a monster behavior-state transition.
// Packet, planner, and arrival code must name the event instead of assigning
// MoverState's critical mode/target tuple directly.
type MoverEvent uint8

const (
	MoverEventNone MoverEvent = iota
	MoverEventSpawnHoldElapsed
	MoverEventIdleRepeated
	MoverEventStartWander
	MoverEventAggroAcquired
	MoverEventRetaliationArmed
	MoverEventChaseStarted
	MoverEventAttackStarted
	MoverEventSegmentArrived
	MoverEventTargetLost
	MoverEventLeashBroken
	MoverEventAttackUnavailable
	MoverEventAttackRefused
	MoverEventTargetDefeated
	MoverEventRecoveryElapsed
	MoverEventFollowStarted
	MoverEventLeaderLost
	MoverEventDisplaced
	MoverEventTraceAbandoned
	MoverEventHomingExpired
	MoverEventHomingRequired
	MoverEventHelpAccepted
	MoverEventWanderExpired
	MoverEventActivityLost
	MoverEventActivityResumed
	MoverEventControlBound
	MoverEventControlReleased
	MoverEventFollowSatisfied
	MoverEventEntryRefused
	MoverEventAttackApproachRequired
	MoverEventSkillCommandRejected
	MoverEventStuckIdled
	moverEventCount
)

func (event MoverEvent) String() string {
	switch event {
	case MoverEventStuckIdled:
		return "stuck-idled"
	case MoverEventAttackApproachRequired:
		return "attack-approach-required"
	case MoverEventSkillCommandRejected:
		return "skill-command-rejected"
	case MoverEventFollowSatisfied:
		return "follow-satisfied"
	case MoverEventEntryRefused:
		return "entry-refused"
	case MoverEventControlBound:
		return "control-bound"
	case MoverEventControlReleased:
		return "control-released"
	case MoverEventActivityLost:
		return "activity-lost"
	case MoverEventActivityResumed:
		return "activity-resumed"
	case MoverEventWanderExpired:
		return "wander-expired"
	case MoverEventHelpAccepted:
		return "help-accepted"
	case MoverEventTraceAbandoned:
		return "trace-abandoned"
	case MoverEventHomingExpired:
		return "homing-expired"
	case MoverEventHomingRequired:
		return "homing-required"
	case MoverEventDisplaced:
		return "displaced"
	case MoverEventNone:
		return "none"
	case MoverEventSpawnHoldElapsed:
		return "spawn-hold-elapsed"
	case MoverEventIdleRepeated:
		return "idle-repeated"
	case MoverEventStartWander:
		return "start-wander"
	case MoverEventAggroAcquired:
		return "aggro-acquired"
	case MoverEventRetaliationArmed:
		return "retaliation-armed"
	case MoverEventChaseStarted:
		return "chase-started"
	case MoverEventAttackStarted:
		return "attack-started"
	case MoverEventSegmentArrived:
		return "segment-arrived"
	case MoverEventTargetLost:
		return "target-lost"
	case MoverEventLeashBroken:
		return "leash-broken"
	case MoverEventAttackUnavailable:
		return "attack-unavailable"
	case MoverEventAttackRefused:
		return "attack-refused"
	case MoverEventTargetDefeated:
		return "target-defeated"
	case MoverEventRecoveryElapsed:
		return "recovery-elapsed"
	case MoverEventFollowStarted:
		return "follow-started"
	case MoverEventLeaderLost:
		return "leader-lost"
	default:
		return fmt.Sprintf("event-%d", uint8(event))
	}
}

func (mode MoverMode) String() string {
	switch mode {
	case MoverPending:
		return "pending"
	case MoverBattleReentry:
		return "battle-reentry"
	case MoverSpawning:
		return "spawning"
	case MoverIdle:
		return "idle"
	case MoverWandering:
		return "wandering"
	case MoverChasing:
		return "chasing"
	case MoverReturning:
		return "returning"
	case MoverAttacking:
		return "attacking"
	case MoverRecovering:
		return "recovering"
	case MoverFollowing:
		return "following"
	default:
		return fmt.Sprintf("mode-%d", uint8(mode))
	}
}

// The complete legal transition graph. Self-transitions are deliberate:
// chase re-aim, attack cooldown re-entry, and a matured chase segment all
// retain their behavior state while recording a new causal event.
var moverTransitionTable = map[MoverMode]map[MoverEvent]MoverMode{
	MoverPending: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventActivityResumed:  MoverWandering,
		MoverEventSegmentArrived:   MoverPending,
		MoverEventRetaliationArmed: MoverChasing,
		MoverEventDisplaced:        MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
	},
	MoverSpawning: {
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
		MoverEventDisplaced:        MoverIdle,
		MoverEventSpawnHoldElapsed: MoverIdle,
		MoverEventRetaliationArmed: MoverChasing,
	},
	MoverIdle: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventEntryRefused:     MoverIdle,
		MoverEventSegmentArrived:   MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
		MoverEventHomingRequired:   MoverReturning,
		MoverEventDisplaced:        MoverIdle,
		MoverEventFollowStarted:    MoverFollowing,
		MoverEventIdleRepeated:     MoverIdle,
		MoverEventStartWander:      MoverWandering,
		MoverEventAggroAcquired:    MoverChasing,
		MoverEventRetaliationArmed: MoverChasing,
	},
	MoverWandering: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventFollowStarted:    MoverFollowing,
		MoverEventEntryRefused:     MoverIdle,
		MoverEventActivityLost:     MoverPending,
		MoverEventStartWander:      MoverWandering,
		MoverEventWanderExpired:    MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
		MoverEventHomingRequired:   MoverReturning,
		MoverEventDisplaced:        MoverIdle,
		MoverEventAggroAcquired:    MoverChasing,
		MoverEventRetaliationArmed: MoverChasing,
		MoverEventSegmentArrived:   MoverIdle,
	},
	MoverChasing: {
		MoverEventStuckIdled:        MoverIdle,
		MoverEventControlBound:      MoverIdle,
		MoverEventControlReleased:   MoverIdle,
		MoverEventHelpAccepted:      MoverBattleReentry,
		MoverEventTraceAbandoned:    MoverIdle,
		MoverEventDisplaced:         MoverChasing,
		MoverEventRetaliationArmed:  MoverChasing,
		MoverEventChaseStarted:      MoverChasing,
		MoverEventAttackStarted:     MoverAttacking,
		MoverEventSegmentArrived:    MoverChasing,
		MoverEventTargetLost:        MoverReturning,
		MoverEventLeashBroken:       MoverReturning,
		MoverEventAttackUnavailable: MoverReturning,
	},
	MoverAttacking: {
		MoverEventStuckIdled:             MoverIdle,
		MoverEventAttackApproachRequired: MoverChasing,
		MoverEventSkillCommandRejected:   MoverAttacking,
		MoverEventControlBound:           MoverIdle,
		MoverEventControlReleased:        MoverIdle,
		MoverEventHelpAccepted:           MoverBattleReentry,
		MoverEventTraceAbandoned:         MoverIdle,
		MoverEventDisplaced:              MoverChasing,
		MoverEventRetaliationArmed:       MoverChasing,
		MoverEventChaseStarted:           MoverChasing,
		MoverEventAttackStarted:          MoverAttacking,
		MoverEventTargetLost:             MoverReturning,
		MoverEventLeashBroken:            MoverReturning,
		MoverEventAttackUnavailable:      MoverReturning,
		MoverEventAttackRefused:          MoverReturning,
		MoverEventTargetDefeated:         MoverRecovering,
	},
	MoverRecovering: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventHelpAccepted:     MoverBattleReentry,
		MoverEventDisplaced:        MoverIdle,
		MoverEventRetaliationArmed: MoverChasing,
		MoverEventRecoveryElapsed:  MoverReturning,
	},
	MoverReturning: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
		MoverEventAggroAcquired:    MoverChasing,
		MoverEventHomingExpired:    MoverIdle,
		MoverEventDisplaced:        MoverIdle,
		MoverEventRetaliationArmed: MoverChasing,
		MoverEventSegmentArrived:   MoverIdle,
	},
	MoverFollowing: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventHelpAccepted:     MoverChasing,
		MoverEventDisplaced:        MoverIdle,
		MoverEventFollowStarted:    MoverFollowing,
		MoverEventSegmentArrived:   MoverFollowing,
		MoverEventFollowSatisfied:  MoverIdle,
		MoverEventAggroAcquired:    MoverChasing,
		MoverEventRetaliationArmed: MoverChasing,
	},
	MoverBattleReentry: {
		MoverEventStuckIdled:       MoverIdle,
		MoverEventControlBound:     MoverIdle,
		MoverEventControlReleased:  MoverIdle,
		MoverEventHelpAccepted:     MoverBattleReentry,
		MoverEventTraceAbandoned:   MoverIdle,
		MoverEventRetaliationArmed: MoverChasing,
		MoverEventDisplaced:        MoverIdle,
	},
}

func (m MoverState) HelpLatched() bool { return m.helpLatched }
func (m MoverState) InNativeBattle() bool {
	return m.mode == MoverChasing || m.mode == MoverAttacking || m.mode == MoverRecovering || m.mode == MoverBattleReentry
}

// MoverTransitionAllowed exposes the table as a read-only query for tests,
// diagnostics, and planners that want to fail before requesting an event.
func MoverTransitionAllowed(from MoverMode, event MoverEvent) (MoverMode, bool) {
	next, ok := moverTransitionTable[from][event]
	return next, ok
}

// MoverTransitionError is a causal state-machine failure. Its text includes
// the state, event, target, and transition serial so the simulation ticker's
// panic recovery identifies the earliest invalid behavior edge.
type MoverTransitionError struct {
	From      MoverMode
	Event     MoverEvent
	TargetGID uint32
	Serial    uint64
	Invariant string
}

func (err *MoverTransitionError) Error() string {
	return fmt.Sprintf(
		"monster mover transition rejected: state=%s event=%s target=%d serial=%d: %s",
		err.From,
		err.Event,
		err.TargetGID,
		err.Serial,
		err.Invariant,
	)
}

// Mode returns the current behavior state.
func (m MoverState) Mode() MoverMode { return m.mode }

// TargetGID returns the player currently owned by chase/attack behavior.
func (m MoverState) TargetGID() uint32 { return m.targetGID }

// RetaliationPending reports that the next chase goal must bypass ordinary
// re-aim throttling because a hit just assigned this target.
func (m MoverState) RetaliationPending() bool { return m.retaliationPending }

// Retaliating distinguishes damage-owned pursuit from aggressive sight aggro.
func (m MoverState) Retaliating() bool { return m.retaliating }

// RetaliationRevision is the optimistic-concurrency token for mover commits.
func (m MoverState) RetaliationRevision() uint64 { return m.retaliationRevision }

func (m MoverState) FollowLeaderGID() uint32 { return m.followLeaderGID }

// PreviousEvent, LastEvent, and TransitionSerial expose the last two accepted
// causal edges. Two entries preserve both a release reason and its immediate
// segment-arrival completion without allocating an unbounded history.
func (m MoverState) PreviousEvent() MoverEvent { return m.previousEvent }
func (m MoverState) LastEvent() MoverEvent     { return m.lastEvent }
func (m MoverState) TransitionSerial() uint64  { return m.transitionSerial }

// ChaseGuidance returns the target movement snapshot adopted with the current
// chase leg. The command destination, sampled live target pose, and derived
// stand-off destination are separate domains and must not be substituted for
// one another.
func (m MoverState) ChaseGuidance() (ChaseGuidance, bool) {
	return m.chaseGuidance, m.hasChaseGuidance
}

// SetChaseGuidance records the movement intention for a newly authored chase
// leg. Only the chase owner may hold this cache; the snapshot's in-flight bit
// determines whether the leg is eligible for a bounded live-position refresh.
func (m *MoverState) SetChaseGuidance(guidance ChaseGuidance) error {
	if m == nil {
		return &MoverTransitionError{Invariant: "nil mover cannot own chase guidance"}
	}
	if m.mode != MoverChasing || m.targetGID == 0 {
		return m.transitionArgumentError(MoverEventChaseStarted, m.targetGID,
			"chase guidance requires an owned chase")
	}
	m.chaseGuidance = guidance
	m.hasChaseGuidance = true
	return nil
}

// Validate checks the state tuple independently of an event. CommitMover
// calls this as the last authority gate, catching accidental struct writes
// even inside monster tests or future package-local code.
func (m MoverState) Validate() error {
	fail := func(reason string) error {
		return &MoverTransitionError{
			From: m.mode, Event: m.lastEvent, TargetGID: m.targetGID,
			Serial: m.transitionSerial, Invariant: reason,
		}
	}
	if m.mode >= moverModeCount {
		return fail("unknown mover mode")
	}
	hasTarget := m.targetGID != 0
	if (m.mode == MoverFollowing) != (m.followLeaderGID != 0) {
		return fail("FOLLOW requires a leader; other states must release it")
	}
	switch m.mode {
	case MoverChasing, MoverAttacking:
		if !hasTarget {
			return fail("chasing/attacking requires a nonzero target")
		}
	default:
		if hasTarget {
			return fail("spawning/idle/wandering/returning must not retain a target")
		}
	}
	if m.retaliationPending && (m.mode != MoverChasing || !m.retaliating) {
		return fail("pending retaliation requires a retaliation-owned chase")
	}
	if m.retaliating &&
		(m.mode != MoverChasing && m.mode != MoverAttacking) {
		return fail("retaliation ownership may exist only while chasing/attacking")
	}
	if m.mode == MoverAttacking && m.ArriveMs > m.DepartMs {
		return fail("attacking may not own a movement segment")
	}
	if m.mode == MoverAttacking &&
		(m.AttackReach < 0 || math.IsNaN(m.AttackReach) || math.IsInf(m.AttackReach, 0) ||
			(!m.AttackSummon && !m.AttackSelfEffect && m.AttackReach == 0) || m.AttackCooldownMs <= 0) {
		return fail("attacking requires a resolved range and cooldown")
	}
	if m.hasChaseGuidance && m.mode != MoverChasing {
		return fail("only a chase may retain target movement guidance")
	}
	if m.mode == MoverRecovering && m.BehaviorDeadlineMs <= 0 {
		return fail("recovering requires an authored action deadline")
	}
	if m.mode != MoverChasing && m.mode != MoverAttacking &&
		(m.AttackSummon || m.AttackSelfEffect || m.AttackSkillID != 0 || m.AttackReach != 0 ||
			m.AttackCooldownMs != 0 || m.NextAttackMs != 0) {
		return fail("non-combat state retained attack-plan fields")
	}
	return nil
}

// Transition applies one legal causal event and validates the resulting
// tuple. It is the only writer for mode, target, and retaliation ownership.
func (m *MoverState) Transition(event MoverEvent, targetGID uint32) error {
	if m == nil {
		return &MoverTransitionError{Event: event, TargetGID: targetGID, Invariant: "nil mover"}
	}
	if err := m.Validate(); err != nil {
		return &MoverTransitionError{
			From: m.mode, Event: event, TargetGID: targetGID,
			Serial: m.transitionSerial, Invariant: "invalid pre-state: " + err.Error(),
		}
	}
	next, allowed := MoverTransitionAllowed(m.mode, event)
	if !allowed {
		return &MoverTransitionError{
			From: m.mode, Event: event, TargetGID: targetGID,
			Serial: m.transitionSerial, Invariant: "event is not legal from this state",
		}
	}
	candidate := *m
	// Re-aiming within one behavior may reuse geometry. Every ownership
	// change cancels it, including same-target retaliation and displacement.
	if event != MoverEventChaseStarted && event != MoverEventFollowStarted &&
		event != MoverEventActivityLost && event != MoverEventWanderExpired && event != MoverEventIdleRepeated &&
		event != MoverEventControlBound && event != MoverEventControlReleased &&
		event != MoverEventFollowSatisfied && event != MoverEventEntryRefused {
		candidate.CancelNavigation()
	}

	switch event {
	case MoverEventAttackApproachRequired, MoverEventSkillCommandRejected:
		if targetGID == 0 || targetGID != candidate.targetGID {
			return m.transitionArgumentError(event, targetGID, "refusal must retain the owned target")
		}
		// Timer-5 refusal opens the next AI attempt. The stored interval is
		// retained: conditional jitter uses its previous value on adoption.
		candidate.NextAttackMs = 0
		if event == MoverEventSkillCommandRejected {
			candidate.AttackSkillID = 0
		}
	case MoverEventControlBound, MoverEventControlReleased:
		if targetGID != 0 {
			return m.transitionArgumentError(event, targetGID, "control event does not carry an attack target")
		}
		candidate.targetGID = 0
		candidate.retaliationPending, candidate.retaliating = false, false
		candidate.retaliationRevision++
		candidate.BehaviorDeadlineMs = 0
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
	case MoverEventHelpAccepted:
		if targetGID == 0 {
			return m.transitionArgumentError(event, targetGID, "help requires its accepted target")
		}
		candidate.targetGID = targetGID
		candidate.helpLatched = true
		if m.InNativeBattle() {
			candidate.targetGID = 0
			candidate.helpLatched = false
		}
		candidate.retaliationRevision++ // reject planners predating this event
		candidate.retaliating, candidate.retaliationPending = false, false
		candidate.From, candidate.To = Pose{}, Pose{}
		candidate.DepartMs, candidate.ArriveMs = 0, 0
		candidate.navigation = nil
		candidate.BehaviorDeadlineMs = 0
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
	case MoverEventDisplaced:
		if targetGID != candidate.targetGID {
			return m.transitionArgumentError(event, targetGID, "displacement preserves target identity")
		}
		candidate.retaliationRevision++
		candidate.retaliationPending = candidate.retaliating
		candidate.BehaviorDeadlineMs = 0
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
		candidate.From, candidate.To = candidate.Pose, candidate.Pose
		candidate.DepartMs, candidate.ArriveMs = 0, 0
	case MoverEventAggroAcquired:
		if targetGID == 0 {
			return m.transitionArgumentError(event, targetGID, "aggro acquisition requires a target")
		}
		candidate.targetGID = targetGID
		candidate.retaliating = false
		candidate.retaliationPending = false
		candidate.BehaviorDeadlineMs = 0
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
	case MoverEventRetaliationArmed:
		if targetGID == 0 {
			return m.transitionArgumentError(event, targetGID, "retaliation requires an attacker target")
		}
		candidate.targetGID = targetGID
		candidate.retaliating = true
		candidate.retaliationPending = true
		candidate.BehaviorDeadlineMs = 0
		candidate.retaliationRevision++
		candidate.clearAttackSelectionPreservingCooldown()
		candidate.clearChaseGuidance()
	case MoverEventChaseStarted:
		if targetGID == 0 || targetGID != candidate.targetGID {
			return m.transitionArgumentError(event, targetGID, "event target must match the owned target")
		}
		candidate.retaliationPending = false
		candidate.BehaviorDeadlineMs = 0
		candidate.clearChaseGuidance()
	case MoverEventAttackStarted:
		if targetGID == 0 || targetGID != candidate.targetGID {
			return m.transitionArgumentError(event, targetGID, "event target must match the owned target")
		}
		candidate.retaliationPending = false
		candidate.BehaviorDeadlineMs = 0
		candidate.clearChaseGuidance()
	case MoverEventHomingRequired, MoverEventTraceAbandoned, MoverEventTargetLost, MoverEventLeashBroken,
		MoverEventAttackUnavailable, MoverEventAttackRefused, MoverEventStuckIdled:
		if targetGID != 0 {
			return m.transitionArgumentError(event, targetGID, "target-release event requires target 0")
		}
		candidate.targetGID = 0
		candidate.retaliating = false
		candidate.retaliationPending = false
		candidate.BehaviorDeadlineMs = 0
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
	case MoverEventTargetDefeated:
		if candidate.BehaviorDeadlineMs <= 0 {
			return m.transitionArgumentError(event, targetGID, "target defeat requires an authored action deadline")
		}
		if targetGID != 0 {
			return m.transitionArgumentError(event, targetGID, "target-release event requires target 0")
		}
		candidate.targetGID = 0
		candidate.retaliating = false
		candidate.retaliationPending = false
		candidate.clearAttackPlan()
		candidate.clearChaseGuidance()
	case MoverEventRecoveryElapsed:
		if targetGID != 0 {
			return m.transitionArgumentError(event, targetGID, "recovery event requires target 0")
		}
		candidate.BehaviorDeadlineMs = 0
	case MoverEventFollowStarted:
		// This event's identity is a leader, not an attack target.
		if targetGID == 0 {
			return m.transitionArgumentError(event, targetGID, "follow requires a nonzero leader")
		}
		candidate.followLeaderGID = targetGID
	case MoverEventFollowSatisfied, MoverEventEntryRefused, MoverEventSpawnHoldElapsed, MoverEventIdleRepeated,
		MoverEventStartWander, MoverEventSegmentArrived,
		MoverEventLeaderLost, MoverEventHomingExpired, MoverEventWanderExpired,
		MoverEventActivityLost, MoverEventActivityResumed:
		if targetGID != 0 {
			return m.transitionArgumentError(event, targetGID, "non-target event requires target 0")
		}
	}

	if next != MoverIdle {
		candidate.idleEntryPending = false
	} else if m.mode != MoverIdle || event != MoverEventSegmentArrived {
		candidate.idleEntryPending = true
	}
	candidate.mode = next
	if next != MoverReturning {
		candidate.HomingStartedMs, candidate.HomingAcquireAfterMs = 0, 0
		// The stored walk-home destination belongs to one homing episode.
		// A matured leg settles into MoverIdle without ending the walk
		// (the native stays in HOMING across move stops), and the
		// idle-entry check decides whether that entry ends it; every
		// other exit from MoverReturning, and every battle entry,
		// discards the goal so the next walk re-selects.
		if (m.mode == MoverReturning && event != MoverEventSegmentArrived) || candidate.InNativeBattle() {
			candidate.homingGoal = Pose{}
			candidate.homingGoalSet = false
		}
	}
	if next != MoverChasing && next != MoverAttacking {
		candidate.helpLatched = false
		candidate.PursuitChannel = 0
		candidate.LastBattleActivityMs = 0
	}
	if next != MoverFollowing {
		candidate.followLeaderGID = 0
	}
	if resetsStuckOnTransition(*m, event) {
		candidate.ResetStuck()
	}
	candidate.previousEvent = candidate.lastEvent
	candidate.lastEvent = event
	candidate.transitionSerial++
	if err := candidate.Validate(); err != nil {
		return &MoverTransitionError{
			From: candidate.mode, Event: event, TargetGID: targetGID,
			Serial: candidate.transitionSerial, Invariant: "invalid post-state: " + err.Error(),
		}
	}
	*m = candidate
	return nil
}

/*
================
resetsStuckOnTransition

The reference server's tactics state change zeroes the tactics object's
+0x148 on every native AI-state entry, including self-transitions that
re-enter the same state ID. Sub-mode transitions inside the battle state
or multi-leg continuations inside the homing/follow states do not run the
state change and keep the counter.
================
*/
func resetsStuckOnTransition(from MoverState, event MoverEvent) bool {
	switch event {
	case MoverEventSpawnHoldElapsed,
		MoverEventIdleRepeated,
		MoverEventStartWander,
		MoverEventAggroAcquired,
		MoverEventRetaliationArmed,
		MoverEventTargetLost,
		MoverEventLeashBroken,
		MoverEventAttackUnavailable,
		MoverEventAttackRefused,
		MoverEventRecoveryElapsed,
		MoverEventLeaderLost,
		MoverEventTraceAbandoned,
		MoverEventHomingExpired,
		MoverEventHelpAccepted,
		MoverEventWanderExpired,
		MoverEventActivityLost,
		MoverEventActivityResumed,
		MoverEventControlBound,
		MoverEventControlReleased,
		MoverEventFollowSatisfied,
		MoverEventEntryRefused,
		MoverEventStuckIdled:
		return true
	case MoverEventFollowStarted:
		return from.mode != MoverFollowing
	case MoverEventHomingRequired:
		return from.lastEvent != MoverEventSegmentArrived
	case MoverEventSegmentArrived, MoverEventDisplaced:
		return from.mode == MoverWandering && !from.stuck
	default:
		return false
	}
}

func (m MoverState) transitionArgumentError(event MoverEvent, targetGID uint32, reason string) error {
	return &MoverTransitionError{
		From: m.mode, Event: event, TargetGID: targetGID,
		Serial: m.transitionSerial, Invariant: reason,
	}
}

func (m *MoverState) clearAttackSelectionPreservingCooldown() {
	m.AttackSummon = false
	m.AttackSelfEffect = false
	m.AttackSkillID = 0
	m.AttackReach = 0
	m.AttackCooldownMs = 0
}

func (m *MoverState) clearAttackPlan() {
	m.clearAttackSelectionPreservingCooldown()
	m.NextAttackMs = 0
}

func (m *MoverState) clearChaseGuidance() {
	m.chaseGuidance = ChaseGuidance{}
	m.hasChaseGuidance = false
}

// TakeIdleEntry consumes the entry callback once. A movement arrival while
// already IDLE does not re-enter the state (the native idle-state base).
func (m *MoverState) TakeIdleEntry() bool {
	if m.mode != MoverIdle || !m.idleEntryPending {
		return false
	}
	m.idleEntryPending = false
	return true
}
func (m MoverState) IdleEntryPending() bool { return m.mode == MoverIdle && m.idleEntryPending }
