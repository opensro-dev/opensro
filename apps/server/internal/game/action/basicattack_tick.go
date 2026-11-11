/*
===========================================================================

basicattack_tick.go - the per-tick scan of settled attack intents

The tick side of the basic-attack lane: the sorted intent snapshot,
the round-robin cursor and spread budget that keep a burst from
starving the tail, and the routing of each intent into its division
lane. Per-intent evaluation lives in basicattack.go.

===========================================================================
*/

package action

import (
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
advanceBasicAttackIntents

The simulation tick advances detached commands under their division lock,
then routes their results after releasing authority locks.
================
*/
func (rt *Runtime) advanceBasicAttackIntents(nowMs int64, openActionOwners map[string]bool) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	rt.basicAttackSnapshot = rt.fillCombatIntents(rt.basicAttackSnapshot)
	intents := rt.basicAttackSnapshot
	if len(intents) == 0 {
		return nil
	}
	// The scan resumes past the intent the last call served last, so a
	// burst draining over several ticks under the spread budget cannot
	// starve the tail of the sorted snapshot (#2143). A cursor whose
	// intent retired finds no match and the scan starts at the head.
	start := 0
	for i, intent := range intents {
		if simulation.WorldKey(intent.DivisionID, intent.CharacterName) == rt.basicAttackCursor {
			start = i + 1
			break
		}
	}
	if start == len(intents) {
		start = 0
	}
	started := time.Now()
	served := 0
	for n := 0; n < len(intents); n++ {
		intent := intents[(start+n)%len(intents)]
		// A settled swing wait runs untouched to its stamped tick
		// (#3101): the evaluation that stamped it had just resolved
		// the caster, target and spacing, and only the swing timer
		// can act next. The finish checks inside the evaluation (a
		// dead or deleted caster, a blocked posture, an untouchable
		// target) wait with it: they land at the wake evaluation
		// instead of the tick they occur on, at most one swing
		// interval later, and the wake re-judges every one of them
		// before anything is spent.
		if nowMs < intent.WaitUntilMs {
			continue
		}
		// A chain stage waits only on the casting instance; its root bracket
		// is deliberately still open (see chainStageBlocked).
		casting, ownsOpenAction := openActionOwners[simulation.WorldKey(intent.DivisionID, intent.CharacterName)]
		if ownsOpenAction && (casting || intent.ComboRootID == 0 || intent.ResumeBasic) {
			continue
		}
		if served > 0 && time.Since(started) >= basicAttackSpreadBudget {
			return out
		}
		character, result, ok := rt.advanceBasicAttackIntentInDivision(intent, nowMs)
		served++
		rt.basicAttackCursor = simulation.WorldKey(intent.DivisionID, intent.CharacterName)
		if !ok {
			continue
		}
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
		// A result that declares an actor-only tail has said what is public;
		// only a result with neither portion publishes its whole burst.
		frames := result.Broadcast
		if len(frames) == 0 && len(result.ActorPrivate) == 0 {
			frames = result.Frames
		}
		if len(frames) > 0 {
			routed := simulation.DivisionFrames{DivisionID: intent.DivisionID, SourceGID: enterworld.ObjectIDForCharacter(character)}
			for _, frame := range frames {
				routed.Frames = append(routed.Frames, simulation.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope})
			}
			out = append(out, routed)
		}
		out = append(out, observedDivisionFrames(intent.DivisionID, result.Around)...)
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
	// The whole snapshot was served: no burst is draining, so the next
	// tick starts at the head and keeps the sorted order.
	rt.basicAttackCursor = ""
	return out
}

/*
================
advanceBasicAttackIntentInDivision
================
*/
func (rt *Runtime) advanceBasicAttackIntentInDivision(intent basicAttackIntent, nowMs int64) (*enterworld.Character, OpResult, bool) {
	// The wait for the division lane is named as its own slow step
	// (basicAttackDivisionWait): a slow advanceBasicAttackIntents reading
	// beside it is the lane holder's cost, not the intent's work (#2143).
	// The wait still sits inside the enclosing step's time.
	var unlock func()
	rt.Steps.Time("basicAttackDivisionWait", func() {
		unlock = rt.lockDivision(intent.DivisionID)
	})
	defer unlock()
	// A request can cancel/replace an intent after the tick snapshots it but
	// before this division lock is acquired. Never resurrect that stale cast.
	if !rt.combatIntentIsCurrent(intent) {
		return nil, OpResult{}, false
	}
	character := rt.findCharacter(intent.DivisionID, intent.CharacterName)
	return character, rt.advanceBasicAttackIntent(character, intent, nowMs, rt.tickDoors()), true
}
