package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func objectiveRequired(def *Definition) uint32 {
	if def.Objective == ObjectiveKill {
		return def.KillCount
	}
	return def.CollectCount
}

// KillUpdater runs once on the accepted fatal transition, inside the same
// character transaction as EXP/SP. No per-frame roster scan or client kill
// packet can advance progress. Target identity comes from the monster owner.
func (rt *Runtime) KillUpdater() func(*enterworld.Character, string, uint8) ([]wire.Frame, bool) {
	return func(character *enterworld.Character, codename string, rarity uint8) ([]wire.Frame, bool) {
		if character == nil || character.DeletePending {
			return nil, false
		}
		var frames []wire.Frame
		for i, record := range character.ActiveQuests {
			def, ok := rt.Defs.ByRefID(record.RefID)
			if ok {
				def, ok = definitionAtStage(def, record.Stage)
			}
			if !ok || def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 || (def.Objective != ObjectiveKill && def.Objective != ObjectiveParallel) {
				continue
			}
			updated, changed := refreshMissions(character, def, record, codename, rarity)
			if !changed {
				continue
			}
			character.ActiveQuests[i] = updated
			frames = append(frames, missionProgressFrames(def, record, updated)...)
		}
		return frames, len(frames) > 0
	}
}

// The action owner already checks the selected NPC/session and liveness.
// Completion rechecks objective truth in the character transaction.
func (rt *Runtime) CompleteNpcQuest(character *enterworld.Character, codename string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(codename)
	if !ok {
		return OpResult{}, fmt.Errorf("quest completion: unknown %s", codename)
	}
	if def.Objective == ObjectiveTalk {
		return rt.CompleteTalkQuest(character, codename)
	}
	if def.EndNpcCodename == "" {
		return OpResult{}, fmt.Errorf("quest completion: %s has no NPC completion contract", codename)
	}
	return rt.completeReward(character, def)
}
