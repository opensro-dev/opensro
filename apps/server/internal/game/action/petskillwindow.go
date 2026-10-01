package action

import (
	"math"
	"sort"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	wire "opensro.online/server/internal/game/item/wire"
)

// The magic-state board's kind-3 slot (sub_67A470 -> sub_6E6E00) is driven by
// an item whose RefItemData+0x29C Param1 is authored in SECONDS. The
// classification and the remaining-seconds rule live in enterworld so the
// item-use path, the tick sweep and the world-entry re-raise share one owner.
func petSkillItemType(typeIDs [4]int64) bool { return enterworld.PetSkillItemType(typeIDs) }

// itemParam1Seconds reads RefItemData+0x29C for a family whose Param1 unit is
// seconds. The projection publishes the column unscaled because the unit
// belongs to the consumer: the same column is minutes for a COS summoner and
// milliseconds for a recall scroll.
func itemParam1Seconds(ref *enterworld.ItemRef) int64 {
	if ref == nil || ref.NativeFields == "" {
		return 0
	}
	seconds, ok := ref.NativeFields.Lookup("itemParam1_29c")
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || math.Trunc(seconds) != seconds || seconds <= 0 || seconds*1000 > float64(^uint32(0)) {
		return 0
	}
	return int64(seconds)
}

// petSkillWindowCapacity bounds one character's live rows. sub_6E6150 keys a
// row by kind and id, so distinct items stack rather than replace; the same
// item restarts its own row.
const petSkillWindowCapacity = 8

func upsertPetSkillWindow(windows []domain.PetSkillWindow, itemRefObjID uint32, codename string, endUnixMs int64) []domain.PetSkillWindow {
	for i := range windows {
		if windows[i].ItemRefObjID == itemRefObjID {
			windows[i].Codename = codename
			windows[i].EndUnixMs = endUnixMs
			return windows
		}
	}
	if len(windows) >= petSkillWindowCapacity {
		return windows
	}
	return append(windows, domain.PetSkillWindow{ItemRefObjID: itemRefObjID, Codename: codename, EndUnixMs: endUnixMs})
}

func petSkillWindowRemaining(window domain.PetSkillWindow, nowMs int64) uint32 {
	return enterworld.PetSkillWindowRemaining(window.EndUnixMs, nowMs)
}

type petSkillWindowIndex struct {
	mu     sync.Mutex
	owners map[petOwnerKey]struct{}
}

func (index *petSkillWindowIndex) track(divisionID, characterName string) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if index.owners == nil {
		index.owners = map[petOwnerKey]struct{}{}
	}
	index.owners[petOwnerKey{division: divisionID, name: characterName}] = struct{}{}
}

func (index *petSkillWindowIndex) keys() []petOwnerKey {
	index.mu.Lock()
	defer index.mu.Unlock()
	keys := make([]petOwnerKey, 0, len(index.owners))
	for key := range index.owners {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].division != keys[j].division {
			return keys[i].division < keys[j].division
		}
		return keys[i].name < keys[j].name
	})
	return keys
}

func (index *petSkillWindowIndex) forget(key petOwnerKey) {
	index.mu.Lock()
	defer index.mu.Unlock()
	delete(index.owners, key)
}

// TrackPetSkillWindows hands a character to the tick sweep. World entry calls
// it through enterworld.Deps.TrackTimedWindows, because the sweep's index is
// otherwise filled only by item use and a relogged window would never retire.
func (rt *Runtime) TrackPetSkillWindows(divisionID, characterName string) {
	rt.petSkillWindows.track(divisionID, characterName)
}

/*
================
TrackTimedWindows

World entry hands a character with any timed row to every sweep that owns
one: pet-skill windows and param jobs (paramjob.go).
================
*/
func (rt *Runtime) TrackTimedWindows(divisionID, characterName string) {
	rt.petSkillWindows.track(divisionID, characterName)
	rt.paramJobOwners.track(divisionID, characterName)
}

// advancePetSkillWindows retires each spent row with the native zero pair,
// which sub_775F20 turns into sub_6E6150's remove selector. An exhausted
// window is not self-retiring on the client: sub_6E6AA0 holds the row at zero
// because only server teardown may remove it.
func (rt *Runtime) advancePetSkillWindows(nowMs int64) {
	type retirement struct {
		key    petOwnerKey
		frames []wire.Frame
	}
	var due []retirement
	for _, key := range rt.petSkillWindows.keys() {
		unlock := rt.lockDivision(key.division)
		character := rt.findCharacter(key.division, key.name)
		if character == nil {
			unlock()
			continue
		}
		var frames []wire.Frame
		kept := character.PetSkillWindows[:0]
		for _, window := range character.PetSkillWindows {
			if window.EndUnixMs > nowMs {
				kept = append(kept, window)
				continue
			}
			frames = append(frames, wire.Frame{
				Opcode:  wire.OpCosStateRefresh,
				Payload: wire.EncodeCosSummonTimerRetire3691(window.ItemRefObjID),
			})
		}
		character.PetSkillWindows = kept
		empty := len(kept) == 0
		unlock()
		if empty {
			rt.petSkillWindows.forget(key)
		}
		if len(frames) > 0 {
			due = append(due, retirement{key: key, frames: frames})
		}
	}
	for _, row := range due {
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(row.key.division, row.key.name, row.frames)
		}
	}
}
