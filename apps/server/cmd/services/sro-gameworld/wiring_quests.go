/*
===========================================================================

wiring_quests.go - quest-programme composition for one gameworld division

Wires the quest runtime's instanced-world doors (#321 step 5) to the
action lane, which owns parties, layers and transfers, then registers
the quest programmes that subscribe to those worlds. The division is
bound here, as it is for the quest spawn door; quest code itself stays
division-agnostic.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
wireQuestProgrammes

The quest programmes' instanced-world doors: the runtime clock, frame
delivery by character name, the party facts the talk gates read, the
party invitation that opens a layer, the per-character return and the
layer close. Rahid 6 is the first programme registered.
================
*/
func (game *gameplayPlane) wireQuestProgrammes(quests *quest.Runtime) {
	quests.NowMs = func() int64 { return game.items.Now().UnixMilli() }
	quests.PushCharacterFrames = func(name string, frames []wire.Frame) {
		game.items.PushCharacterFrames(game.divisionID, name, frames)
	}
	quests.InstancePartyFacts = func(character *enterworld.Character) quest.InstancePartyFacts {
		facts := game.items.InstancePartyFacts(game.divisionID, character.Name)
		return quest.InstancePartyFacts{
			Found: facts.Found, InParty: facts.InParty, Leader: facts.Leader,
			Shared: facts.Shared, Transported: facts.Transported,
		}
	}
	quests.InstanceInvite = func(leader *enterworld.Character, call quest.InstanceInviteCall) quest.InstanceInviteOutcome {
		result := game.items.InvitePartyToInstance(action.InstanceInviteRequest{
			Division:   game.divisionID,
			LeaderName: leader.Name,
			Definition: call.Definition,
			Entry:      call.Entry,
			DeadlineMs: call.DeadlineMs,
			Eligible:   call.Eligible,
		})
		return quest.InstanceInviteOutcome{
			Opened:         result.OpenStatus == instance.Success,
			LeaderIn:       result.LeaderStatus == instance.Success,
			AllocationFull: result.OpenStatus == instance.AllocationFull,
			Lease:          result.Lease,
			Prompted:       result.Prompted,
			Skipped:        result.Skipped,
		}
	}
	quests.InstanceReturn = func(character *enterworld.Character) bool {
		return game.items.ReturnCharacterFromInstance(game.divisionID, character) == instance.Success
	}
	quests.InstanceClose = func(lease instance.Lease) {
		game.items.CloseInstanceWorld(game.divisionID, lease)
	}
	quests.RegisterRahid6Programme()
}
