package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/simulation"
)

// Native manager+1D8 owns a command, not its client token's whole lifetime.
// Positive-time instant/projectile casts install it during preparation and
// clear it after release processing, even when flight or
// presentation finalization remains. Persistent/onff and area continuation
// producers require separate transitions before using this for all admission.
type currentSkillCommand struct {
	characterID    int64
	token, skillID uint32
	descriptor     statuseffect.ReplacementDescriptor
	pinned         bool
}

// Caller holds pendingSkillFinalizesMu and the division operation lock.
func (rt *Runtime) installCurrentSkillCommandLocked(division string, c *enterworld.Character, token uint32, skill enterworld.SkillRow) {
	if rt.currentSkillCommands == nil {
		rt.currentSkillCommands = make(map[string]currentSkillCommand)
	}
	rt.currentSkillCommands[simulation.WorldKey(division, c.Name)] = currentSkillCommand{
		characterID: c.ID, token: token, skillID: skill.ID,
		descriptor: skill.Replacement, pinned: skill.ReplacementPinned,
	}
}

// Release is serialized with admission by the division operation lock. Native
// clears unconditionally here: do not add a token-equality guard based on the
// C++ reconstruction. Queue dequeue is deliberately not the clear operation.
func (rt *Runtime) clearCurrentSkillCommand(division, name string) {
	rt.pendingSkillFinalizesMu.Lock()
	delete(rt.currentSkillCommands, simulation.WorldKey(division, name))
	rt.pendingSkillFinalizesMu.Unlock()
}

// Return existence separately from metadata validity. A live command without
// pinned metadata must never masquerade as an empty conflict word. Reference
// metadata is captured at admission, so an executing command cannot change
// identity if the catalog is replaced. Active-effect bits remain registry-owned.
func (rt *Runtime) currentSkillCommandFor(division string, c *enterworld.Character) (currentSkillCommand, bool) {
	if c == nil {
		return currentSkillCommand{}, false
	}
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	command, ok := rt.currentSkillCommands[simulation.WorldKey(division, c.Name)]
	return command, ok && command.characterID == c.ID
}
