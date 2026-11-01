package monster

import (
	"errors"
	"strings"
	"testing"
)

const stateMachineTargetGID = uint32(100003)

func coherentMover(mode MoverMode) MoverState {
	mover := MoverState{mode: mode}
	switch mode {
	case MoverFollowing:
		mover.followLeaderGID = stateMachineTargetGID
	case MoverChasing:
		mover.targetGID = stateMachineTargetGID
	case MoverAttacking:
		mover.targetGID = stateMachineTargetGID
		mover.AttackSkillID = 160
		mover.AttackReach = 10
		mover.AttackCooldownMs = 3000
	case MoverRecovering:
		mover.BehaviorDeadlineMs = 1
	}
	return mover
}

func transitionTarget(event MoverEvent) uint32 {
	switch event {
	case MoverEventHelpAccepted, MoverEventAggroAcquired, MoverEventRetaliationArmed,
		MoverEventChaseStarted, MoverEventAttackStarted, MoverEventFollowStarted,
		MoverEventAttackApproachRequired, MoverEventSkillCommandRejected:
		return stateMachineTargetGID
	default:
		return 0
	}
}

func TestMoverTransitionTableIsExhaustiveAndExecutable(t *testing.T) {
	for mode := MoverMode(0); mode < moverModeCount; mode++ {
		if _, present := moverTransitionTable[mode]; !present {
			t.Fatalf("mode %s has no transition table", mode)
		}
		for event := MoverEventNone; event < moverEventCount; event++ {
			next, allowed := MoverTransitionAllowed(mode, event)
			_, tableHasEdge := moverTransitionTable[mode][event]
			if allowed != tableHasEdge {
				t.Fatalf("transition lookup disagrees with table for %s + %s", mode, event)
			}
			if !allowed {
				mover := coherentMover(mode)
				before := mover
				if err := mover.Transition(event, transitionTarget(event)); err == nil || mover != before {
					t.Fatalf("rejected edge %s + %s accepted or partially mutated state", mode, event)
				}
				continue
			}

			mover := coherentMover(mode)
			if event == MoverEventAttackStarted {
				mover.AttackSkillID = 160
				mover.AttackReach = 10
				mover.AttackCooldownMs = 3000
			}
			if event == MoverEventTargetDefeated {
				mover.BehaviorDeadlineMs = 1
			}
			target := transitionTarget(event)
			if event == MoverEventDisplaced {
				target = mover.TargetGID()
			}
			if err := mover.Transition(event, target); err != nil {
				t.Fatalf("legal transition %s + %s failed: %v", mode, event, err)
			}
			if mover.Mode() != next || mover.LastEvent() != event || mover.TransitionSerial() != 1 {
				t.Fatalf("%s + %s produced mode=%s event=%s serial=%d, want %s/%s/1",
					mode, event, mover.Mode(), mover.LastEvent(), mover.TransitionSerial(), next, event)
			}
			if err := mover.Validate(); err != nil {
				t.Fatalf("%s + %s produced invalid state: %v", mode, event, err)
			}
		}
	}
}

func TestMoverTransitionRejectsIllegalEdgeWithoutPartialMutation(t *testing.T) {
	mover := coherentMover(MoverIdle)
	before := mover
	err := mover.Transition(MoverEventAttackStarted, stateMachineTargetGID)
	var transitionErr *MoverTransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("illegal transition error = %T %v, want *MoverTransitionError", err, err)
	}
	if mover != before {
		t.Fatalf("illegal transition partially mutated mover: before=%+v after=%+v", before, mover)
	}
	if transitionErr.From != MoverIdle || transitionErr.Event != MoverEventAttackStarted ||
		!strings.Contains(transitionErr.Error(), "state=idle event=attack-started") {
		t.Fatalf("transition error lost causal edge: %+v", transitionErr)
	}
}

func TestMoverTransitionRejectsInvalidPostStateAtomically(t *testing.T) {
	mover := coherentMover(MoverChasing)
	before := mover
	err := mover.Transition(MoverEventAttackStarted, stateMachineTargetGID)
	if err == nil || !strings.Contains(err.Error(), "attacking requires a resolved range and cooldown") {
		t.Fatalf("attack without a plan error = %v", err)
	}
	if mover != before {
		t.Fatalf("invalid post-state partially mutated mover: before=%+v after=%+v", before, mover)
	}
}

func TestMoverValidateRejectsSplitBehaviorOwnership(t *testing.T) {
	tests := []struct {
		name string
		edit func(*MoverState)
		want string
	}{
		{
			name: "chase without target",
			edit: func(m *MoverState) { m.mode = MoverChasing },
			want: "requires a nonzero target",
		},
		{
			name: "idle retaining target",
			edit: func(m *MoverState) { m.targetGID = stateMachineTargetGID },
			want: "must not retain a target",
		},
		{
			name: "pending retaliation outside chase",
			edit: func(m *MoverState) { m.retaliationPending = true },
			want: "pending retaliation requires",
		},
		{
			name: "attack and movement overlap",
			edit: func(m *MoverState) {
				*m = coherentMover(MoverAttacking)
				m.DepartMs, m.ArriveMs = 1, 2
			},
			want: "may not own a movement segment",
		},
		{
			name: "idle retaining attack plan",
			edit: func(m *MoverState) { m.AttackReach = 10 },
			want: "retained attack-plan fields",
		},
		{
			name: "idle retaining chase guidance",
			edit: func(m *MoverState) {
				m.chaseGuidance = NewChaseGuidance(Pose{RegionID: 25000, X: 100}, true)
				m.hasChaseGuidance = true
			},
			want: "only a chase may retain target movement guidance",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mover := coherentMover(MoverIdle)
			test.edit(&mover)
			err := mover.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMoverChaseGuidanceIsReleasedWhenChaseOwnershipEnds(t *testing.T) {
	mover := coherentMover(MoverChasing)
	guidance := NewChaseGuidance(Pose{RegionID: 25000, X: 1150, Z: 1000}, true)
	if err := mover.SetChaseGuidance(guidance); err != nil {
		t.Fatalf("SetChaseGuidance: %v", err)
	}
	if got, ok := mover.ChaseGuidance(); !ok || got != guidance {
		t.Fatalf("chase guidance = %+v (ok=%v), want %+v", got, ok, guidance)
	}

	mover.AttackSkillID = 160
	mover.AttackReach = 10
	mover.AttackCooldownMs = 3000
	if err := mover.Transition(MoverEventAttackStarted, stateMachineTargetGID); err != nil {
		t.Fatalf("attack transition: %v", err)
	}
	if guidance, ok := mover.ChaseGuidance(); ok {
		t.Fatalf("attack state retained chase guidance: %+v", guidance)
	}
}

func TestMoverRetaliationLifecycleHasOneCausalPath(t *testing.T) {
	mover := coherentMover(MoverIdle)
	mustTransition := func(event MoverEvent, targetGID uint32) {
		t.Helper()
		if err := mover.Transition(event, targetGID); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
	}

	mustTransition(MoverEventStartWander, 0)
	mustTransition(MoverEventSegmentArrived, 0)
	mustTransition(MoverEventRetaliationArmed, stateMachineTargetGID)
	if !mover.Retaliating() || !mover.RetaliationPending() || mover.RetaliationRevision() != 1 {
		t.Fatalf("retaliation edge did not atomically own target: %+v", mover)
	}
	mustTransition(MoverEventChaseStarted, stateMachineTargetGID)
	mover.AttackSkillID = 160
	mover.AttackReach = 10
	mover.AttackCooldownMs = 3000
	mustTransition(MoverEventAttackStarted, stateMachineTargetGID)
	mustTransition(MoverEventTargetLost, 0)
	mustTransition(MoverEventSegmentArrived, 0)

	if mover.Mode() != MoverIdle || mover.TargetGID() != 0 || mover.Retaliating() ||
		mover.RetaliationPending() || mover.AttackSkillID != 0 || mover.AttackReach != 0 ||
		mover.AttackCooldownMs != 0 || mover.NextAttackMs != 0 {
		t.Fatalf("completed retaliation lifecycle leaked behavior ownership: %+v", mover)
	}
	if mover.LastEvent() != MoverEventSegmentArrived || mover.TransitionSerial() != 7 {
		t.Fatalf("causal ledger = %s/%d, want segment-arrived/7", mover.LastEvent(), mover.TransitionSerial())
	}
	if mover.PreviousEvent() != MoverEventTargetLost {
		t.Fatalf("causal ledger lost release reason: previous=%s", mover.PreviousEvent())
	}
}

func TestSummonActionHasNoMeleeRangeButRetainsActionOwnership(t *testing.T) {
	m := coherentMover(MoverChasing)
	m.AttackSkillID, m.AttackCooldownMs, m.AttackSummon = 160, 3000, true
	if err := m.Transition(MoverEventAttackStarted, stateMachineTargetGID); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	// A normal attack still cannot borrow a summon's zero reach.
	m.AttackSummon = false
	if err := m.Validate(); err == nil {
		t.Fatal("zero-reach melee admitted")
	}
	m.AttackSummon = true
	if err := m.Transition(MoverEventTargetLost, 0); err != nil {
		t.Fatal(err)
	}
	if m.AttackSummon {
		t.Fatal("summon selection survived combat teardown")
	}
	m = coherentMover(MoverAttacking)
	m.AttackSummon, m.AttackReach, m.AttackCooldownMs = true, 0, 0
	if err := m.Validate(); err == nil {
		t.Fatal("summon without cooldown admitted")
	}
}

/*
================
TestMoverStuckProgressAndStateResets
================
*/
func TestMoverStuckProgressAndStateResets(t *testing.T) {
	origin := Pose{RegionID: 25000, X: 100, Z: 100}
	m := MoverState{mode: MoverIdle, Pose: origin}
	if err := m.BindController(ControlSummoned, 777); err != nil {
		t.Fatalf("bind controller: %v", err)
	}
	if err := m.Transition(MoverEventAggroAcquired, 42); err != nil {
		t.Fatalf("acquire target: %v", err)
	}
	for i := uint8(1); i < 10; i++ {
		m.NoteMoveIssued(origin, false)
		if m.NoteMoveStopped(Pose{RegionID: 25000, X: 105, Z: 100}) {
			t.Fatalf("stop %d reported stuck before limit", i)
		}
	}
	// Intra-BATTLE transitions (attack <-> chase) stay inside native state 3
	// and therefore preserve the stuck failure counter.
	m.AttackSkillID = 160
	m.AttackReach = 10
	m.AttackCooldownMs = 3000
	if err := m.Transition(MoverEventAttackStarted, 42); err != nil {
		t.Fatalf("attack start: %v", err)
	}
	if err := m.Transition(MoverEventChaseStarted, 42); err != nil {
		t.Fatalf("chase restart: %v", err)
	}
	if m.Progress().Failures() != 9 {
		t.Fatalf("failures = %d after intra-battle transition, want 9", m.Progress().Failures())
	}
	// The 10th short stop marks the mover stuck.
	m.NoteMoveIssued(origin, false)
	if !m.NoteMoveStopped(Pose{RegionID: 25000, X: 110, Z: 100}) || !m.Stuck() {
		t.Fatal("10th short stop did not mark mover stuck")
	}
	// MoverEventStuckIdled transitions to MoverIdle, clears combat target,
	// preserves controllerGID (+0x14C), and resets stuck progress (the
	// native tactics state change).
	if err := m.Transition(MoverEventStuckIdled, 0); err != nil {
		t.Fatalf("stuck idled: %v", err)
	}
	if m.Mode() != MoverIdle || m.TargetGID() != 0 || m.ControllerGID() != 777 || !m.IdleEntryPending() {
		t.Fatalf("unexpected state after stuck idled: mode=%v target=%d controller=%d idleEntry=%v",
			m.Mode(), m.TargetGID(), m.ControllerGID(), m.IdleEntryPending())
	}
	if m.Progress().Failures() != 0 || m.Stuck() {
		t.Fatalf("stuck state not cleared by MoverEventStuckIdled: failures=%d stuck=%v", m.Progress().Failures(), m.Stuck())
	}
}

/*
================
TestMoverHomingGoalClearsOnBattleEntry

The stored walk-home destination belongs to one homing episode. A matured
leg settles into idle without ending the walk, so the mover carries the
goal into idle; the next battle entry discards it there, and the following
walk home selects a fresh one.
================
*/
func TestMoverHomingGoalClearsOnBattleEntry(t *testing.T) {
	goal := Pose{RegionID: 25000, X: 120, Z: 90}
	mover := coherentMover(MoverReturning)
	mover.SetHomingGoal(goal)
	if err := mover.Transition(MoverEventSegmentArrived, 0); err != nil {
		t.Fatalf("settled leg: %v", err)
	}
	if mover.Mode() != MoverIdle {
		t.Fatalf("settled leg mode = %s, want idle", mover.Mode())
	}
	if stored, ok := mover.HomingGoal(); !ok || stored != goal {
		t.Fatalf("arrival discarded the stored goal: %+v stored=%t", stored, ok)
	}
	if err := mover.Transition(MoverEventAggroAcquired, stateMachineTargetGID); err != nil {
		t.Fatalf("battle entry: %v", err)
	}
	if mover.Mode() != MoverChasing {
		t.Fatalf("battle entry mode = %s, want chasing", mover.Mode())
	}
	if _, ok := mover.HomingGoal(); ok {
		t.Fatal("battle entry kept the previous walk's goal")
	}
}

/*
================
TestMoverHomingGoalClearsOnNonArrivalExit

An exit from returning that is not a settled arrival - here the homing
timer maturing - ends the episode, and the stored goal is discarded
with it.
================
*/
func TestMoverHomingGoalClearsOnNonArrivalExit(t *testing.T) {
	goal := Pose{RegionID: 25000, X: 120, Z: 90}
	mover := coherentMover(MoverReturning)
	mover.SetHomingGoal(goal)
	if _, ok := mover.HomingGoal(); !ok {
		t.Fatal("goal was not stored")
	}
	if err := mover.Transition(MoverEventHomingExpired, 0); err != nil {
		t.Fatalf("homing expiry: %v", err)
	}
	if mover.Mode() != MoverIdle {
		t.Fatalf("homing expiry mode = %s, want idle", mover.Mode())
	}
	if _, ok := mover.HomingGoal(); ok {
		t.Fatal("non-arrival exit kept the ended walk's goal")
	}
}
