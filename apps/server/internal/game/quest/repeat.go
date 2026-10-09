package quest

import "opensro.online/server/internal/game/enterworld"

func completionCount(c *enterworld.Character, id uint32) uint32 {
	count := c.QuestCompletionCounts[id]
	if count == 0 && questCompleted(c, id) {
		return 1
	}
	return count
}

func canAcceptAgain(c *enterworld.Character, def *Definition) bool {
	// An ended quest is in state 5, which the talk never offers (state 4).
	if questEnded(c, def.RefID) {
		return false
	}
	if def.Repeatable {
		return true
	}
	count := completionCount(c, def.RefID)
	if count == 0 && completedThroughPredecessor(c, def) {
		count = 1
	}
	return count < max(1, def.MaxCompletions)
}

/*
================
completedThroughPredecessor

True when a quest named by def.CompletedBy is complete.
================
*/
func completedThroughPredecessor(c *enterworld.Character, def *Definition) bool {
	for _, id := range def.CompletedByIDs {
		if questCompleted(c, id) {
			return true
		}
	}
	return false
}

/*
================
creditPredecessorCompletions

Adds every quest a completed predecessor also completes to the completed
list, once. Returns whether the list changed.
================
*/
func creditPredecessorCompletions(c *enterworld.Character, defs *Definitions) bool {
	changed := false
	for _, def := range defs.All() {
		if questCompleted(c, def.RefID) || !completedThroughPredecessor(c, def) {
			continue
		}
		completed := make([]uint32, 0, len(c.CompletedQuestIds)+1)
		completed = append(completed, c.CompletedQuestIds...)
		c.CompletedQuestIds = append(completed, def.RefID)
		changed = true
	}
	return changed
}

func recordCompletion(c *enterworld.Character, id uint32) {
	count := completionCount(c, id)
	if c.QuestCompletionCounts == nil {
		c.QuestCompletionCounts = make(map[uint32]uint32)
	}
	if count < ^uint32(0) {
		count++
	}
	c.QuestCompletionCounts[id] = count
}

// v1.150 5c4087..5c40d7 formats the low nibble first (current run),
// then the high nibble (limit). The old unlimited exchange keeps zero.
func repeatTitleByte(def *Definition, completed uint32) uint8 {
	if def.Repeatable {
		return 0
	}
	limit := min(uint32(15), max(uint32(1), def.MaxCompletions))
	return uint8(limit<<4 | min(limit, completed+1))
}
