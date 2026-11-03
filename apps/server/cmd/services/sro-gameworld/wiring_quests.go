/*
===========================================================================

wiring_quests.go - quest composition for one gameworld division

Wires the quest runtime's doors (its inventory and monster-spawn
seats, every quest hook the item lane calls, and the instanced-world
doors of #321 step 5) to the action lane, which owns parties, layers
and transfers, then registers the quest programmes that subscribe to
those worlds. The division is bound here, as it is for the quest
spawn door; quest code itself stays division-agnostic.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wireQuestDoors

The quest runtime's own doors and the item lane's quest hooks: the
inventory planner and the monster spawn, trap capture and quest item
use, the minute and calendar pulses, monster drops, and the NPC
conversation hooks; then the programme composition below.
================
*/
func (game *gameplayPlane) wireQuestDoors(quests *quest.Runtime) {
	quests.PlanInventory = game.items.PlanQuestInventory
	quests.SpawnQuestMonster = func(character *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		return game.items.SpawnQuestMonster(game.divisionID, character, request)
	}
	game.wireQuestProgrammes(quests)
	game.items.CanPlaceQuestTrap = quests.CanPlaceTrap
	game.items.CaptureQuestTrap = quests.CaptureQuestTrap
	game.items.UpdateQuestInventory = quests.InventoryUpdater()
	game.items.UpdateQuestKill = quests.KillUpdater()
	game.items.UpdateQuestTeleport = quests.TeleportUpdater()
	game.items.NoteQuestKillSite = quests.NoteKillSite
	quests.SpawnExitGate = func(world instance.ID, fell simulation.Spawn) bool {
		_, ok := game.items.SpawnQuestExitGate(world, fell)
		return ok
	}
	quests.RemoveExitGates = game.items.RemoveQuestExitGates
	game.items.QuestTravelBlocks = quests.TravelBlocks
	game.items.AdvanceQuestMinute = quests.AdvanceMinute
	game.items.AdvanceQuestItem = quests.AdvanceItemUse
	game.items.ForgetQuestItem = quests.ForgetItemUse
	game.items.UseQuestItem = quests.BeginItemUse
	game.items.ReleaseQuestCapturesOnDeath = quests.ReleaseCapturesOnDeath
	game.items.AdvanceQuestCalendar = quests.AdvanceCalendar
	game.items.QuestMonsterDrops = quests.MonsterDrops
	game.items.NpcQuests = action.NpcQuestHooks{
		Prepare: quests.PrepareNpcQuest,
		Options: func(divisionID string, character *enterworld.Character, npcCodename string) []action.NpcQuestOption {
			var rows []quest.NpcOption
			var resuscitation bool
			game.deps.Read(divisionID, func() {
				rows = quests.OptionsForNpc(character, npcCodename)
				resuscitation = quest.ResuscitationAvailable(character, npcCodename)
			})
			out := make([]action.NpcQuestOption, 0, len(rows))
			for _, row := range rows {
				pages := make([]action.NpcDialogPage, 0, len(row.Pages))
				for _, page := range row.Pages {
					pages = append(pages, action.NpcDialogPage{PromptSymbol: page.PromptSymbol, ReplySymbol: page.ReplySymbol,
						RefuseSymbol: page.RefuseSymbol, RefuseResponseSymbol: page.RefuseResponseSymbol})
				}
				branches := make([]action.NpcDialogBranch, 0, len(row.Branches))
				for _, branch := range row.Branches {
					branches = append(branches, action.NpcDialogBranch{Codename: branch.Codename, ReplySymbol: branch.ReplySymbol,
						AcceptResponseSymbol: branch.AcceptResponseSymbol})
				}
				out = append(out, action.NpcQuestOption{
					Codename: row.Codename, TitleSymbol: row.TitleSymbol,
					PromptSymbol: row.PromptSymbol, Complete: row.Complete,
					AcceptResponseSymbol: row.AcceptResponseSymbol, DenyResponseSymbol: row.DenyResponseSymbol,
					Pages: pages, Branches: branches, Informational: row.Informational, SideTalk: row.SideTalk,
					AcceptRowSymbol: row.AcceptRowSymbol,
				})
			}
			if resuscitation {
				out = append(out, action.NpcQuestOption{
					Codename: quest.ResuscitationService, TitleSymbol: "SN_TALK_QSP_ALL_POTION_1_07",
					PromptSymbol: "SN_TALK_QSP_ALL_POTION_1_00", Immediate: true,
				})
			}
			// The Halloween rows (port-only): the rolled exchange and
			// the helmet rows, appended last and only while the event
			// runs, so So-ok's quest rows (Capture the Flag among
			// them) keep their order and places.
			out = append(out, game.items.HalloweenTalkOptions(npcCodename, character)...)
			return out
		},
		Accept: func(character *enterworld.Character, codename string) ([]wire.Frame, error) {
			result, err := quests.StartQuest(character, codename)
			return result.Frames, err
		},
		Finish: func(character *enterworld.Character, codename, npcCodename string) ([]wire.Frame, error) {
			if codename == action.HalloweenExchangeService {
				return game.items.ExchangeHalloweenBox(character, npcCodename)
			}
			if len(codename) > len(action.HalloweenHelmetServicePrefix) &&
				codename[:len(action.HalloweenHelmetServicePrefix)] == action.HalloweenHelmetServicePrefix {
				return game.items.ExchangeHalloweenHelmet(character, npcCodename, codename)
			}
			if codename == quest.ResuscitationService {
				result, err := quests.OpenResuscitation(character, npcCodename)
				return result.Frames, err
			}
			result, err := quests.AdvanceNpcQuest(character, codename, npcCodename)
			return result.Frames, err
		},
		Refuse: func(character *enterworld.Character, codename string) ([]wire.Frame, error) {
			result, err := quests.RefuseQuestOffer(character, codename)
			return result.Frames, err
		},
		OpenResuscitation: func(character *enterworld.Character, npcCodename string) ([]wire.Frame, error) {
			result, err := quests.OpenResuscitation(character, npcCodename)
			return result.Frames, err
		},
	}
	game.items.GMQuests = gmQuestHooks(quests)
}

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
			Opened:   result.OpenStatus == instance.Success,
			LeaderIn: result.LeaderStatus == instance.Success,
			Lease:    result.Lease,
			Prompted: result.Prompted,
			Skipped:  result.Skipped,
		}
	}
	quests.InstanceReturn = func(character *enterworld.Character) bool {
		return game.items.ReturnCharacterFromInstance(game.divisionID, character) == instance.Success
	}
	quests.InstanceClose = func(lease instance.Lease) {
		game.items.CloseInstanceWorld(game.divisionID, lease)
	}
	quests.RegisterRahid6Programme()
	quests.RegisterEasteu21Programme()
	quests.RegisterIvy3Programme()
	quests.RegisterGoria7Programme()
}
