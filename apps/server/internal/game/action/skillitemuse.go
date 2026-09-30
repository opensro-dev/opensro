/*
===========================================================================

skillitemuse.go - item-owned skills use the common effect lifecycle

Only complete descriptors enter this path. The inventory debit follows a
successful installation; parameter refresh is private while the attached
effect is public. Persistence, replacement and expiry remain registry-owned.

===========================================================================
*/
package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
skillItemUse
================
*/
type skillItemUse struct {
	division string
	ref      *enterworld.ItemRef
	row      int
	request  wire.ItemUseRequest
	nowMs    int64
}

/*
================
useSkillItem

49C2B0 cases 1..3 resolve the associated skill and call 59B8D0. cbuf+dura
uses the owner timed-job path, not a combat cast or a name-specific effect.
================
*/
func (rt *Runtime) useSkillItem(character *enterworld.Character, use skillItemUse, result *OpResult) bool {
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if !ok || use.ref.AssociatedSkillCodename == "" {
		result.DiagnosticRefusal = "item-use: missing skill reference prerequisite " + use.ref.Codename
		return false
	}
	skill, found := source.SkillByCodename(use.ref.AssociatedSkillCodename)
	if found && skill.CastGate.QuestTrap.Present {
		return rt.useQuestTrap(character, use, skill, result)
	}
	speed := skill.MovementModifier.Present && skill.MovementModifier.Supported
	sight := skill.Concealment.Pinned && skill.Concealment.Sight.Present
	stats := skill.TimedEffect.Pinned && skill.TimedEffect.ItemProgram
	if !found || !speed && !sight && !stats || skill.EffectDurationMs == 0 {
		result.DiagnosticRefusal = "item-use: unsupported skill effect prerequisite " + use.ref.AssociatedSkillCodename
		return false
	}
	if stats {
		if _, err := rt.PlayerBaseStats(use.division, character); err != nil {
			result.DiagnosticRefusal = "item-use: character stats unavailable"
			return false
		}
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	if token == 0 {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	frames, applied := rt.commitCharacterEffect(use.division, character, skill, token,
		statuseffect.StateActive, false, EffectPresentation{Phase: 2}, use.nowMs)
	if !applied {
		return false
	}
	remaining := rt.consumeItemUseRow(character, use.row)
	*result = OpResult{
		Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
			Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}},
		Broadcast: frames,
	}
	result.Frames = append(result.Frames, frames...)
	if stats {
		// Attached-effect announcements do not contain the new derived stats.
		// Keep their projection private and publish current vitals alongside it.
		result.Frames = append(result.Frames, rt.gaugeDropFrames(use.division, character, true, true, true)...)
	}
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	return true
}
