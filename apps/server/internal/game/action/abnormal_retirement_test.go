/*
===========================================================================

abnormal_retirement_test.go - selective cancellation and effect ownership

Native freeze/sleep and stun choose different sets of installed skills.
Check live registry removal and packets, not only a selection predicate.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestAbnormalCancellationPreservesNativeProtectedEffects
================
*/
func TestAbnormalCancellationPreservesNativeProtectedEffects(t *testing.T) {
	for _, status := range []abnormal.Status{abnormal.Freeze, abnormal.Sleep, abnormal.Stun} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		skills := rt.deps.SkillData().(staticSkillSource)
		cases := []struct {
			name                       string
			row                        enterworld.SkillRow
			selected, stunned, current bool
		}{
			{name: "ordinary", row: enterworld.SkillRow{ActionKind: 1}, stunned: true},
			{name: "passive", row: enterworld.SkillRow{ActionKind: 0}},
			{name: "cbuf", row: enterworld.SkillRow{ActionKind: 1, BuffCancelConfirm: true}},
			{name: "nbuf", row: enterworld.SkillRow{ActionKind: 1, VoluntaryCancelBlocked: true}},
			{name: "bbuf", row: enterworld.SkillRow{ActionKind: 1, BuffSecondary: true}},
			{name: "wall", row: enterworld.SkillRow{ActionKind: 1,
				Replacement: statuseffect.ReplacementDescriptor{Category: 3}, CastGate: enterworld.SkillCastGate{Pw: true}}, selected: true},
			{name: "ao", row: enterworld.SkillRow{ActionKind: 1,
				Replacement: statuseffect.ReplacementDescriptor{Category: 3}, CastGate: enterworld.SkillCastGate{Ao: true}}, selected: true},
			{name: "other category 3", row: enterworld.SkillRow{ActionKind: 1,
				Replacement: statuseffect.ReplacementDescriptor{Category: 3}}},
			{name: "current", row: enterworld.SkillRow{ActionKind: 2}, selected: true, stunned: true, current: true},
		}
		for index, tc := range cases {
			id := uint32(100 + index)
			row := tc.row
			row.ID, row.Group = id, id
			skills[id] = row
			modifiers, err := statuseffect.NewModifiers([]paramkeeper.Write{{Parameter: 5, Value: 10}})
			if err != nil {
				t.Fatal(err)
			}
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: character.Name,
				SkillID: id, SkillGroup: id, InstanceToken: id, State: statuseffect.StateActive, Modifiers: modifiers}) {
				t.Fatalf("failed to install %s", tc.name)
			}
			if tc.current {
				rt.pendingSkillFinalizesMu.Lock()
				rt.installCurrentSkillCommandLocked(testDivision, character, id, row)
				rt.pendingSkillFinalizesMu.Unlock()
			}
		}
		record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
		owner := rt.applyPlayerAbnormalInDoor(testDivision, character, false, []abnormal.Record{record}, clock.NowMs())
		publication := rt.playerAbnormalPublication(testDivision, character, owner)
		remaining := make(map[uint32]bool)
		for _, effect := range rt.effects.Snapshot(testDivision, character.Name) {
			remaining[effect.SkillID] = true
		}
		for index, tc := range cases {
			removed := tc.selected
			if status == abnormal.Stun {
				removed = tc.stunned
			}
			present := remaining[uint32(100+index)]
			if present == removed {
				t.Errorf("status %d effect %s: present %v, want removed %v", status, tc.name, present, removed)
			}
		}
		if !saw(publication.public, wire.OpEndedEffectInstances) || !saw(publication.actor, wire.OpBaseStats) {
			t.Errorf("status %d missing retirement or stat publication: %+v", status, publication)
		}
	}
}
