package monster

// PendingMover retains a settled, inactive actor without its live movement
// machinery. It is not a spawn template: position and every retained clock,
// revision, channel and transition identity survive expansion unchanged.
type PendingMover struct {
	Spawning                                                                      bool
	BehaviorDeadlineMs                                                            int64
	Pose                                                                          Pose
	Activity                                                                      ActivityCadence
	PreviousEvent, LastEvent                                                      MoverEvent
	TransitionSerial, RetaliationRevision                                         uint64
	AttackIntervalMs, LastBattleActivityMs, HomingStartedMs, HomingAcquireAfterMs uint32
	Channel, PursuitChannel, NavigationChannel                                    uint8
	NavigationSpeed                                                               float64
	Progress                                                                      MovementProgress
	Stuck                                                                         bool
}

func (m MoverState) PendingSnapshot() (PendingMover, bool) {
	if (m.mode != MoverPending && m.mode != MoverSpawning) || m.DepartMs != 0 || m.ArriveMs != 0 || m.intent.phase != navigationNone {
		return PendingMover{}, false
	}
	p := PendingMover{m.mode == MoverSpawning, m.BehaviorDeadlineMs, m.Pose, m.Activity, m.previousEvent, m.lastEvent, m.transitionSerial, m.retaliationRevision, m.AttackIntervalMs, m.LastBattleActivityMs, m.HomingStartedMs, m.HomingAcquireAfterMs, m.Channel, m.PursuitChannel, m.intent.channel, m.intent.speed, m.progress, m.stuck}
	m.BehaviorDeadlineMs = 0
	m.Pose = Pose{}
	m.Activity = ActivityCadence{}
	m.mode = 0
	m.previousEvent = 0
	m.lastEvent = 0
	m.transitionSerial = 0
	m.retaliationRevision = 0
	m.AttackIntervalMs = 0
	m.LastBattleActivityMs = 0
	m.HomingStartedMs = 0
	m.HomingAcquireAfterMs = 0
	m.Channel = 0
	m.PursuitChannel = 0
	m.intent.channel = 0
	m.intent.speed = 0
	m.progress = MovementProgress{}
	m.stuck = false
	// No live segment or route refers to this completed path's height closure.
	m.navigation = nil
	if m != (MoverState{}) {
		return PendingMover{}, false
	}
	return p, true
}

func (p PendingMover) Expand() MoverState {
	mode := MoverPending
	if p.Spawning {
		mode = MoverSpawning
	}
	return MoverState{mode: mode, BehaviorDeadlineMs: p.BehaviorDeadlineMs, Pose: p.Pose, Activity: p.Activity,
		previousEvent: p.PreviousEvent, lastEvent: p.LastEvent, transitionSerial: p.TransitionSerial,
		retaliationRevision: p.RetaliationRevision, AttackIntervalMs: p.AttackIntervalMs,
		LastBattleActivityMs: p.LastBattleActivityMs, HomingStartedMs: p.HomingStartedMs,
		HomingAcquireAfterMs: p.HomingAcquireAfterMs, Channel: p.Channel, PursuitChannel: p.PursuitChannel,
		intent:   navigationIntent{speed: p.NavigationSpeed, channel: p.NavigationChannel},
		progress: p.Progress, stuck: p.Stuck}
}
