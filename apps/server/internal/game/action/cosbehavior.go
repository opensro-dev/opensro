/*
===========================================================================

cosbehavior.go - owns cosbehavior behavior and its checked data boundaries

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// Behavior state belongs to the same character transaction as the COS
// lifecycle. A response is emitted only after that transaction commits.
/*
================
HandleCosBehavior
================
*/
func (rt *Runtime) HandleCosBehavior(division string, c *enterworld.Character, p []byte) OpResult {
	q, err := wire.DecodeCosBehavior(p)
	if c == nil || err != nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if !ok {
		return OpResult{}
	}
	committed := rt.deps.Update(c, "cos-behavior", func() bool {
		cos := c.CompanionByGID(q.GID)

		if c.DeletePending || cos == nil || !cos.Summoned || cos.CurrentHP == 0 || cos.GID != q.GID {
			return false
		}
		ref, found := refs.CharacterRefByCodename(cos.Codename)
		if !found || ref == nil || ref.RefObjID != cos.RefObjID || ref.TidWord&0x7fe != 0x1c6 || !q.ValidTransition(uint32(ref.TidWord>>11), cos.CommandMode) {
			return false
		}
		cos.CommandMode = q.Mode
		return true
	})
	// The client proves the failure grammar, but not which server error code
	// belongs to each invalid lifecycle. Do not invent a retail error value.
	if !committed {
		return OpResult{}
	}
	return OpResult{Frames: []wire.Frame{q.Success()}}
}
