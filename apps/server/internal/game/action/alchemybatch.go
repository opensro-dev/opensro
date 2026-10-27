/*
===========================================================================

alchemybatch.go - multi-step alchemy compound clock and batch execution

===========================================================================
*/

package action

import (
	"reflect"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

type compoundKey struct{ division, name string }

// Absence is idle; a stored job is waiting for the next character update.
// Executing is synchronous under the division lock, never a second timer owner.
type compoundJob struct {
	character      *enterworld.Character
	steps          []alchemy.ProcessRequest
	expected       map[uint8]inventory.Item
	lastMs         int64
	elapsedSeconds float32
	counter        uint8
}

/*
================
compoundJob
================
*/
func (rt *Runtime) compoundJob(key compoundKey) (compoundJob, bool) {
	rt.compoundMu.Lock()
	defer rt.compoundMu.Unlock()
	j, ok := rt.compoundJobs[key]
	return j, ok
}

/*
================
storeCompoundJob
================
*/
func (rt *Runtime) storeCompoundJob(key compoundKey, job compoundJob) {
	rt.compoundMu.Lock()
	defer rt.compoundMu.Unlock()
	if rt.compoundJobs == nil {
		rt.compoundJobs = make(map[compoundKey]compoundJob)
	}
	rt.compoundJobs[key] = job
}

/*
================
clearCompoundJob
================
*/
func (rt *Runtime) clearCompoundJob(key compoundKey) {
	rt.compoundMu.Lock()
	defer rt.compoundMu.Unlock()
	delete(rt.compoundJobs, key)
}

/*
================
compoundClock

Natively the counter is checked before it is incremented. Execution resets
it to zero when the craft completes, then the same update increments it to
one. Never run more than one callback per host update. The native timer
retains elapsed debt after
subtracting the one-second period; it does not discard overdue time.
================
*/
func compoundClock(job compoundJob, nowMs int64) (compoundJob, bool) {
	if nowMs <= job.lastMs {
		return job, false
	}
	job.elapsedSeconds += float32(float64(nowMs-job.lastMs) / 1000)
	job.lastMs = nowMs
	if job.elapsedSeconds < 1 {
		return job, false
	}
	job.elapsedSeconds -= 1
	if job.counter == 3 {
		job.counter = 1
		return job, true
	}
	job.counter++
	return job, false
}

/*
================
advanceCompoundJobs
================
*/
func (rt *Runtime) advanceCompoundJobs(nowMs int64) {
	rt.compoundMu.Lock()
	keys := make([]compoundKey, 0, len(rt.compoundJobs))
	for key := range rt.compoundJobs {
		keys = append(keys, key)
	}
	rt.compoundMu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})
	for _, key := range keys {
		frames := rt.advanceCompoundJob(key, nowMs)
		if len(frames) > 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(key.division, key.name, frames)
		}
	}
}

/*
================
advanceCompoundJob
================
*/
func (rt *Runtime) advanceCompoundJob(key compoundKey, nowMs int64) []wire.Frame {
	unlock := rt.lockDivision(key.division)
	defer unlock()
	job, ok := rt.compoundJob(key)
	if !ok {
		return nil
	}
	var frames []wire.Frame
	character := rt.findCharacter(key.division, key.name)
	if character == nil || character != job.character {
		rt.clearCompoundJob(key)
		return nil
	}
	snapshot := character.Snapshot()
	if snapshot.DeletePending || enterworld.CurrentHP(snapshot) <= 0 {
		rt.clearCompoundJob(key)
		return []wire.Frame{{Opcode: 0x3359, Payload: []byte{2, 6}}}
	}
	var due bool
	job, due = compoundClock(job, nowMs)
	if due {
		step := job.steps[0]
		frames, ok = rt.commitAlchemyProcess(character, alchemy.OpCompoundResult, step, job.expected)
		if !ok {
			frames[len(frames)-1].Opcode = 0x3359
		}
		if ok {
			job.steps = job.steps[1:]
		}
		if !ok || len(job.steps) == 0 {
			rt.clearCompoundJob(key)
		} else {
			rt.storeCompoundJob(key, job)
		}
	} else {
		rt.storeCompoundJob(key, job)
	}
	return frames
}

/*
================
compoundExpected
================
*/
func compoundExpected(items []inventory.Item, steps []alchemy.ProcessRequest) map[uint8]inventory.Item {
	needed := map[uint8]bool{}
	for _, step := range steps {
		for _, slot := range step.Slots {
			needed[slot] = true
		}
	}
	out := map[uint8]inventory.Item{}
	for _, item := range items {
		if needed[item.Slot] {
			item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
			out[item.Slot] = item
		}
	}
	return out
}

/*
================
compoundInputsUnchanged
================
*/
func compoundInputsUnchanged(items []inventory.Item, slots []uint8, expected map[uint8]inventory.Item) bool {
	if expected == nil {
		return true
	}
	for _, slot := range slots {
		found := false
		for _, item := range items {
			if item.Slot == slot {
				found = reflect.DeepEqual(item, expected[slot])
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
