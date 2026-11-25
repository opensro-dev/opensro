/*
===========================================================================

runtime_remote.go - when an agent asks the remote (LLM) brain

The remote brain is asked only for situations worth the latency, at most
once per agent at a time; its answer overrides the hardcoded decision
while it still fits what the agent sees.

===========================================================================
*/
package caravanai

import (
	"strings"
)

/*
==================
remoteDecisionLocked

Takes the agent's remote answer when it has arrived, or asks for one in the
background; never blocks. An answer applies only while the agent is still in
the state it was asked about and its target is still nearby; otherwise it is
dropped and the local decision stands. A new request goes out at most once
per remoteDecisionIntervalMs per agent, and only when the brain mode wants
this situation (hybrid asks only during encounters).
==================
*/
func (r *Runtime) remoteDecisionLocked(unit *AgentUnit, sit Situation, nowMs int64) (Decision, bool) {
	if r.remote == nil {
		return Decision{}, false
	}
	key := agentKey(unit.Division, unit.Name)
	if req, pending := r.inflight[key]; pending {
		select {
		case d := <-req.result:
			delete(r.inflight, key)
			if req.state == unit.State && targetNearby(d.TargetName, sit.Players) {
				return d, true
			}
		default:
		}
		return Decision{}, false
	}
	if !wantsRemote(r.cfg.BrainMode, sit) || nowMs < unit.nextRemoteAtMs {
		return Decision{}, false
	}
	unit.nextRemoteAtMs = nowMs + remoteDecisionIntervalMs
	req := &remoteDecision{result: make(chan Decision, 1), state: unit.State}
	r.inflight[key] = req
	r.remoteJobs.Add(1)
	go func() {
		defer r.remoteJobs.Done()
		// The channel holds one answer, so the send never blocks even
		// when the agent is gone and nobody reads it.
		req.result <- r.remote.Decide(r.remoteCtx, sit)
	}()
	return Decision{}, false
}

/*
==================
wantsRemote

Whether the brain mode asks the remote brain about this situation: llm
always, hybrid only during an encounter (players nearby or under attack).
==================
*/
func wantsRemote(mode BrainMode, sit Situation) bool {
	switch mode {
	case BrainLLM:
		return true
	case BrainHybrid:
		return len(sit.Players) > 0 || sit.UnderAttack
	default:
		return false
	}
}

/*
==================
targetNearby

Whether a remote decision's target (empty for none) is still among the
players the agent sees now.
==================
*/
func targetNearby(target string, players []ObservedPlayer) bool {
	if target == "" {
		return true
	}
	for _, p := range players {
		if strings.EqualFold(p.Name, target) {
			return true
		}
	}
	return false
}
