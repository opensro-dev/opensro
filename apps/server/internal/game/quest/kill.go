package quest

import (
	"fmt"
	"slices"

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
		frames := rt.NoteKillChains(character, codename)
		frames = append(frames, rt.CreditKillQuests(character, codename, rarity, nil)...)
		return frames, len(frames) > 0
	}
}

/*
================
NoteKillChains

The authored chains' watchers on one kill (the Rahid, East-EU, Goria and
Hwan chains and Ivy's blood grant). These belong to the killing
character's own event: when a party shares the kill (CreditKillQuests
under a DivideType), the chains still watch only the killer's blow.
================
*/
func (rt *Runtime) NoteKillChains(character *enterworld.Character, codename string) []wire.Frame {
	rt.noteRahid6Kill(character, codename)
	rt.noteEasteu21Kill(character, codename)
	rt.noteGoria7Kill(character, codename)
	rt.noteHwanKill(character, codename)
	frames := rt.noteIvy3Kill(character, codename)
	return append(frames, rt.ivy3BloodGrant(character, codename)...)
}

/*
================
CreditKillQuests

The mission loop of one kill event for a single character: every active
kill quest refreshes (or, when only names some, just those RefIDs), and
each named quest's exchange runs. A shared kill (the native DivideType
fan-out) credits members by RefID through this entry; the solo killer
passes nil and takes all of his.
================
*/
func (rt *Runtime) CreditKillQuests(character *enterworld.Character, codename string, rarity uint8, only map[uint32]bool) []wire.Frame {
	var frames []wire.Frame
	for i, record := range character.ActiveQuests {
		if only != nil && !only[record.RefID] {
			continue
		}
		def, ok := rt.Defs.ByRefID(record.RefID)
		if ok {
			def, ok = definitionAtStage(def, record.Stage)
		}
		if !ok || def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 || (def.Objective != ObjectiveKill && def.Objective != ObjectiveParallel) {
			continue
		}
		updated, changed := refreshMissions(character, def, record, codename, rarity)
		if changed {
			character.ActiveQuests[i] = updated
			frames = append(frames, missionProgressFrames(def, record, updated)...)
		}
		// The exchange runs on every kill event, matching or not: the
		// server's handler tests the mask after its mission loop.
		frames = append(frames, rt.applyKillExchange(character, def, i)...)
	}
	return frames
}

/*
================
KillTaker

One quest of a character that takes a single kill: its RefID, its
DivideType (the kill-share mode) and whether it gathers from the kill
(a MonsterDrop mission) instead of counting it.
================
*/
type KillTaker struct {
	RefID      uint32
	DivideType uint8
	Drop       bool
}

/*
================
KillTakers

The quests of character that take a kill of (codename, rarity): an
unfilled kill mission matches, or a gather mission drops from it. The
party's kill fan-out builds its holder lists from these.
================
*/
func (rt *Runtime) KillTakers(character *enterworld.Character, codename string, rarity uint8) []KillTaker {
	var out []KillTaker
	for _, record := range character.ActiveQuests {
		def, ok := rt.Defs.ByRefID(record.RefID)
		if ok {
			def, ok = definitionAtStage(def, record.Stage)
		}
		if !ok || def.TimeLimitMinutes > 0 && record.RemainingMinutes == 0 || waitingBranch(def, record) {
			continue
		}
		if killQuestTakes(def, record, codename, rarity) {
			out = append(out, KillTaker{RefID: def.RefID, DivideType: def.DivideType, Drop: def.Objective == ObjectiveCollect && def.MonsterDrop != nil})
		}
	}
	return out
}

/*
================
killQuestTakes

Whether one quest takes the kill: an unfilled kill mission names the
monster, or a gather mission drops from it (species and grade rules as
the drop itself reads them; the held cap is the roll's, not the list's).
================
*/
func killQuestTakes(def *Definition, record enterworld.ActiveQuestRecord, codename string, rarity uint8) bool {
	for i := 0; i < missionCount(def); i++ {
		m := missionDefinition(def, i)
		if m.Objective == ObjectiveKill && killTargetMatches(m, codename, rarity) && recordProgress(missionRecord(record, m)) < m.KillCount {
			return true
		}
	}
	if def.Objective != ObjectiveCollect || def.MonsterDrop == nil {
		return false
	}
	rule := def.MonsterDrop
	if !rule.AnyMonster && !slices.Contains(rule.MonsterCodenames, codename) {
		return false
	}
	if len(rule.MonsterGrades) > 0 {
		for i, code := range rule.MonsterCodenames {
			if code == codename {
				return rule.MonsterGrades[i] == rarity&15
			}
		}
		return false
	}
	return true
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
