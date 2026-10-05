package monster

/*
================================================================================
Monster tactics

Hostility belongs to the server. The v1.150 client method at vslot +0xA0 is
an unconditional false return, while the v1.188 server tables carry the
per-tactic aggression flag and sight range. Production behavior therefore
comes from the matched Nest/Tactics evidence attached to each Instance.

The behavior lifecycle is class-wide native behavior. A fresh entity remains
in SPAWN for three seconds, enters IDLE for 2.001..5.999 seconds, then either
repeats IDLE (the native 1..100 roll is <25) or enters WANDER. WANDER owns its
5.000..5.999 second state deadline and requests a 46..124-unit movement after
a separate 30-unit collision probe. An unmatched v1.150 npcpos anchor receives that passive
lifecycle. Aggression, sight, and chase leash remain evidence-only because
they are authored per Nest/Tactics row in the v1.188 source.
================================================================================
*/

const (
	// The v1.188 SPAWN state dispatches its completion event after 3000ms.
	RetailSpawnHoldMs int64 = 3000

	// The v1.188 IDLE state calls sub_545b70(1). Its integer result is the
	// inclusive 2.001..5.999 second band around the 4-second center.
	RetailIdleDelayMinMs int64 = 2001
	RetailIdleDelayMaxMs int64 = 5999

	// The ordinary idle decision rolls 1..100 and repeats IDLE while the
	// result is strictly below 25: 24 repeat outcomes, 76 wander outcomes.
	RetailIdleRepeatThreshold = 25

	// The v1.188 idle-move action calls sub_545b70(2), which returns
	// rand()%1000 + 5000.
	retailWanderDelayMinMs int64 = 5000
	retailWanderDelayMaxMs int64 = 5999

	// sub_545860 probes 30 units ahead; 541730 uses a separate distance.
	retailWanderProbeDistance = 30
)

// BehaviorPolicy owns class-wide state cadence. It is deliberately separate
// from Tactics: Tactics is authored per nest (hostility/range/leash), whereas
// this policy is the native monster-state contract shared by every instance.
// Mission injects samples; monster owns their conversion to native integer
// domains so labs, tests, and production cannot each invent different timing.
type BehaviorPolicy struct{}

// RetailBehaviorPolicy returns the stateless production policy.
func RetailBehaviorPolicy() BehaviorPolicy { return BehaviorPolicy{} }

func (BehaviorPolicy) SpawnHoldMs() int64 { return RetailSpawnHoldMs }

// 545B84..545BB4: sign draw first, then rand()%2000. The two zero-offset
// outcomes both produce 4000; a uniform 3999-value distribution is different.
func NativeIdleDelayMs(random func() uint32) int64 {
	sign := int64(-1)
	if random()%2 != 0 {
		sign = 1
	}
	return 4000 + sign*int64(random()%2000)
}

func NativeWanderDelayMs(random func() uint32) int64 {
	return 5000 + int64(random()%1000)
}

func (BehaviorPolicy) IdleDelayMs(sample float64) int64 {
	return inclusiveSample(sample, RetailIdleDelayMinMs, RetailIdleDelayMaxMs)
}

func (BehaviorPolicy) WanderDelayMs(sample float64) int64 {
	return inclusiveSample(sample, retailWanderDelayMinMs, retailWanderDelayMaxMs)
}

func (BehaviorPolicy) RepeatIdle(sample float64) bool {
	roll := inclusiveSample(sample, 1, 100)
	return roll < RetailIdleRepeatThreshold
}

func inclusiveSample(sample float64, minimum, maximum int64) int64 {
	if maximum <= minimum {
		return minimum
	}
	if sample < 0 {
		sample = 0
	}
	if sample >= 1 {
		// The sources promise [0,1), but clamp defensive injectors without
		// importing floating-point policy into every caller.
		sample = 0.9999999999999999
	}
	return minimum + int64(sample*float64(maximum-minimum+1))
}

// Tactics is the behavior contract resolved for one live monster.
type Tactics struct {
	Aggressive bool
	SightRange float64

	// ChaseLeash is the Nest containment radius. A chaser outside this
	// radius drops its target and returns to its generated home position.
	ChaseLeash float64

	WanderProbeDistance float64
}

// TacticsResolver is an explicit mission-plane dependency. Production uses
// ResolveTactics; tests may inject a fixed contract without changing shipped
// monster data.
type TacticsResolver func(Instance) Tactics

// ResolveTactics maps one instance to its server behavior. Every mobile
// monster receives the class-wide retail idle-wander primitive. Per-nest
// aggression, sight, and chase containment are applied only with a matched
// population row. An unmatched monster is therefore mobile but passive.
func ResolveTactics(instance Instance) Tactics {
	nest := instance.Nest
	if !nest.PolicyPinned && !nest.RetailEvidence && !nest.HasControls && instance.Ref.WalkSpeed <= 0 {
		return Tactics{}
	}
	tactics := Tactics{
		WanderProbeDistance: retailWanderProbeDistance,
	}
	if !nest.PolicyPinned && !nest.RetailEvidence && !nest.HasControls {
		return tactics
	}
	tactics.Aggressive = nest.Aggressive
	tactics.SightRange = nest.SightRange
	tactics.ChaseLeash = nest.Radius
	if nest.HasControls {
		// 53FA37..53FA4F: effective sight includes the actor's body radius.
		tactics.SightRange = float64(float32(nest.SightRange + instance.BodyRadius()))
		// Native combat containment is TraceData relative to the target.
		// Radius still owns nest/home geometry; it is not a battle leash.
		tactics.ChaseLeash = 0
	}
	return tactics
}
