/*
===========================================================================

ended.go - quests closed for good without being completed

The native server keeps a quest record in state 5 for a quest the character
can never take: QuestManager_CreateEndedRecord (570F10) creates one with
count 0, or SetQuestState(5) overwrites an existing record and keeps its
count. KT_SMITH_2 (8A77A0) is the fork that uses it: refusing its first
page ends SMITH_2 and SMITH_3, pressing Accept ends ACCESSORY_2 and
ACCESSORY_3, and ACCESSORY_2 requires both blacksmith quests ended and
never completed (list 0x108, CBasicQuest_MeetsPrerequisites 9262A0).

The port keeps the ended ids in Character.EndedQuestIds. Native sends no
update for state 5 (SetQuestState 5709C0 with arg4 = 0), so nothing here
reaches the client: the login completed list stays the completions, and an
ended quest that was completed stays completed with its count.

===========================================================================
*/
package quest

import (
	"fmt"
	"slices"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
questEnded
================
*/
func questEnded(c *enterworld.Character, id uint32) bool {
	return slices.Contains(c.EndedQuestIds, id)
}

/*
================
markQuestsEnded

Adds each id once; returns whether the list changed. The id is added even
for an active quest, and its active record is left in place: native
SetQuestState(5) would overwrite it, but every ending this port authors
targets quests whose own prerequisites keep them inactive at that moment
(the KT fork), so the case never arises.
================
*/
func markQuestsEnded(c *enterworld.Character, ids []uint32) bool {
	changed := false
	for _, id := range ids {
		if questEnded(c, id) {
			continue
		}
		ended := make([]uint32, 0, len(c.EndedQuestIds)+1)
		ended = append(ended, c.EndedQuestIds...)
		c.EndedQuestIds = append(ended, id)
		changed = true
	}
	return changed
}

/*
================
offerAvailable

The native talk offers a quest only in state 4: not active, not used up,
prerequisites met. Level and country gate it as StartQuest does.
================
*/
func offerAvailable(c *enterworld.Character, def *Definition) bool {
	level := int64(1)
	if c.Level != nil {
		level = *c.Level
	}
	if level < int64(def.Level) || (def.CountryByte != 3 && int(def.CountryByte) != enterworld.NativeCountryByte9C(c)) {
		return false
	}
	return activeQuestIndex(c, def.RefID) < 0 && canAcceptAgain(c, def) && prerequisitesMet(c, def)
}

/*
================
endOnAccept

Ends the quest's AcceptEndsQuests, and the chosen reply's EndsQuests,
when its Accept is pressed on a live offer. 8A77A0 ends them whether or
not the acceptance (+0x188) succeeds (8A7857 skips only the start and its
line), so this runs before StartQuest and survives its refusal. 8C6180
likewise ends its reply's quest outside the +0x188 success branch: it ends
whether or not the acceptance succeeds.
================
*/
func (rt *Runtime) endOnAccept(character *enterworld.Character, def *Definition, branch int, branched bool) {
	ids := def.AcceptEndsQuestIDs
	if branched && branch < len(def.OfferBranches) {
		ids = append(append([]uint32(nil), ids...), def.OfferBranches[branch].endsQuestIDs...)
	}
	if len(ids) == 0 {
		return
	}
	rt.deps.Update(character, "quest-accept-ends", func() bool {
		if character.DeletePending || !offerAvailable(character, def) {
			return false
		}
		return markQuestsEnded(character, ids)
	})
}

/*
================
RefuseQuestOffer

An offer page's refusal row: ends the quest's RefuseEndsQuests. Refused
unless the offer still stands, so a stale or forged choice ends nothing.
================
*/
func (rt *Runtime) RefuseQuestOffer(character *enterworld.Character, codename string) (OpResult, error) {
	if character == nil {
		return OpResult{}, fmt.Errorf("quest refuse: nil character")
	}
	def, ok := rt.Defs.ByCodename(codename)
	if !ok || len(def.RefuseEndsQuestIDs) == 0 {
		return OpResult{}, fmt.Errorf("quest refuse: %s has no refusal", codename)
	}
	var refusal error
	settled := false
	changed := rt.deps.Update(character, "quest-refuse", func() bool {
		if character.DeletePending || !offerAvailable(character, def) {
			refusal = fmt.Errorf("quest refuse: %s is not on offer", codename)
			return false
		}
		if !markQuestsEnded(character, def.RefuseEndsQuestIDs) {
			// Already ended: the refusal stands without a write.
			settled = true
			return false
		}
		return true
	})
	if refusal != nil {
		return OpResult{}, refusal
	}
	if !changed && !settled {
		return OpResult{}, fmt.Errorf("quest refuse: character is no longer authoritative")
	}
	return OpResult{}, nil
}

/*
================
loadEndedQuests

Resolves the ended-quest lists. Every quest they name must be loaded: an
ending or an ended prerequisite on a quest this port does not run would
never change.
================
*/
func loadEndedQuests(def *Definition, defs *Definitions) error {
	if len(def.RequiredQuestCompletions) > len(def.RequiredQuests) {
		return fmt.Errorf("quest %s has %d completion counts for %d prerequisites", def.Codename, len(def.RequiredQuestCompletions), len(def.RequiredQuests))
	}
	for _, count := range def.RequiredQuestCompletions {
		if count == 0 {
			return fmt.Errorf("quest %s requires a prerequisite completed zero times", def.Codename)
		}
	}
	refuses := false
	for _, page := range def.OfferPages {
		if (page.RefuseSymbol == "") != (page.RefuseResponseSymbol == "") {
			return fmt.Errorf("quest %s offer page refusal needs its row and its answer", def.Codename)
		}
		refuses = refuses || page.RefuseSymbol != ""
	}
	if refuses != (len(def.RefuseEndsQuests) > 0) {
		return fmt.Errorf("quest %s: a refusal row and RefuseEndsQuests come together", def.Codename)
	}
	lists := []struct {
		codes []string
		ids   *[]uint32
	}{
		{def.RequiredEndedQuests, &def.RequiredEndedQuestIDs},
		{def.AcceptEndsQuests, &def.AcceptEndsQuestIDs},
		{def.RefuseEndsQuests, &def.RefuseEndsQuestIDs},
	}
	// Do not resolve into the shared spec table's branch slice.
	def.OfferBranches = append([]OfferBranch(nil), def.OfferBranches...)
	for i := range def.OfferBranches {
		branch := &def.OfferBranches[i]
		lists = append(lists, struct {
			codes []string
			ids   *[]uint32
		}{branch.EndsQuests, &branch.endsQuestIDs})
	}
	for _, list := range lists {
		for _, code := range list.codes {
			ended, ok := defs.byCodename[code]
			if !ok {
				return fmt.Errorf("quest %s names unavailable quest %s as ended", def.Codename, code)
			}
			*list.ids = append(*list.ids, ended.RefID)
		}
	}
	return nil
}
