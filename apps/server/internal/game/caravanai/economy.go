/*
===========================================================================

economy.go - slice 5's earned money events, fired off the runtime lock

A step earns money events under r.mu (a contract delivery in
deliverAgentLocked, a parley tribute in applyDecisionLocked), but the
consumer's hooks take world doors of their own, and the damage door
nests r.mu inside them - so no hook may fire under the lock. Earned
events queue on the Runtime and StepAgent fires them from
drainEconomy once the lock is dropped, in the order they were earned.

===========================================================================
*/
package caravanai

// economyEvent is one slice-5 money event waiting for the step lock to
// drop: exactly one of reward (pay a Hunter) or cargo drop (a parley
// tribute hits the ground) is set.
type economyEvent struct {
	rewardHunter  string
	rewardGold    int64
	rewardTrader  string
	dropAgent     string
	dropRegionID  uint16
	dropX         float32
	dropY         float32
	dropZ         float32
	dropStacks    int
	dropStackGold int64
	drop          bool
	division      string
}

/*
==================
drainEconomy

Fires the step's queued slice-5 money events (contract rewards, parley
cargo drops) off the runtime lock, in the order they were earned. The
queue is per-runtime, drained by whichever step runs next in the
division; an event earned by a command path never enters it (commands
move no money but the hire fee, charged inline through ChargeHireFee).
==================
*/
func (r *Runtime) drainEconomy(division string) {
	r.mu.Lock()
	events := r.pendingEconomy
	r.pendingEconomy = nil
	r.mu.Unlock()
	for _, event := range events {
		if event.division != division {
			// Another division's step earns on its own drain; put it
			// back rather than firing it from the wrong tick.
			r.mu.Lock()
			r.pendingEconomy = append(r.pendingEconomy, event)
			r.mu.Unlock()
			continue
		}
		if event.drop {
			if r.hooks.OnParleyCargoDrop != nil {
				r.hooks.OnParleyCargoDrop(event.division, event.dropAgent, event.dropRegionID,
					event.dropX, event.dropY, event.dropZ, event.dropStacks, event.dropStackGold)
			}
			continue
		}
		if r.hooks.OnContractReward != nil {
			r.hooks.OnContractReward(event.division, event.rewardHunter, event.rewardGold, event.rewardTrader)
		}
	}
}
