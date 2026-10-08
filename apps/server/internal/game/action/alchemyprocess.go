package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// The division lock serializes cancellation against the durable transaction.
// A cancel can never undo a committed conversion or manufacture a partial bag.
func (rt *Runtime) HandleAlchemyProcess(division string, character *enterworld.Character, opcode uint16, payload []byte) []wire.Frame {
	op := alchemy.OpCompoundResult
	if opcode == alchemy.OpDissolve {
		op = alchemy.OpDissolveResult
	}
	r, err := alchemy.DecodeProcess(opcode, payload)
	if err != nil {
		return alchemyFailure(op, err)
	}
	if character == nil || rt.Alchemy == nil {
		return alchemyFailure(op, nil)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if r.Cancel {
		rt.clearCompoundJob(compoundKey{division, character.Name})
		return []wire.Frame{{Opcode: alchemy.OpCompoundResult, Payload: []byte{1, 1}}}
	}
	key := compoundKey{division, character.Name}
	if _, busy := rt.compoundJob(key); busy {
		return nil
	}
	if r.Mode == 3 {
		frames, _ := rt.commitAlchemyProcess(character, op, r, nil)
		return frames
	}
	snapshot := character.Snapshot()
	before := invItemsFromBag(snapshot)
	steps, err := rt.Alchemy.CompoundSteps(before, r)
	if err != nil {
		return alchemyFailure(op, err)
	}
	frames, committed := rt.commitAlchemyProcess(character, op, steps[0], nil)
	if committed && len(steps) > 1 {
		rt.storeCompoundJob(key, compoundJob{character: character, steps: steps[1:], expected: compoundExpected(before, steps[1:]), lastMs: rt.Now().UnixMilli()})
	}
	return frames
}

// Caller holds the division lock. Each native batch step has one durable commit.
func (rt *Runtime) commitAlchemyProcess(character *enterworld.Character, op uint16, r alchemy.ProcessRequest, expected map[uint8]inventory.Item) ([]wire.Frame, bool) {
	var frames []wire.Frame
	var err error
	committed := rt.deps.Update(character, "alchemy-process", func() bool {
		if character.DeletePending || enterworld.CurrentHP(character) <= 0 {
			return false
		}
		before := invItemsFromBag(character)
		// Products land in the bag the character holds at commit.
		r.BagEnd = inventory.BagEnd(character)
		if !compoundInputsUnchanged(before, r.Slots, expected) {
			err = alchemy.Refusal(6)
			return false
		}
		var result alchemy.ProcessOutcome
		if r.Mode == 3 {
			result, err = rt.Alchemy.Dissolve(before, r, rt.AlchemyRoll)
		} else {
			result, err = rt.Alchemy.Compound(before, r, rt.AlchemyRoll)
		}
		if err != nil {
			return false
		}
		frames = alchemy.ProcessFrames(op, before, result)
		character.MissionInventory = alchemyRows(character.MissionInventory, before, result.Items)
		if rt.UpdateQuestInventory != nil {
			quest, _ := rt.UpdateQuestInventory(character)
			frames = append(frames, quest...)
		}
		return true
	})
	if !committed {
		return alchemyFailure(op, err), false
	}
	return frames, true
}
