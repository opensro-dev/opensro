/*
===========================================================================

abnormal_fixture_test.go - status admission for detached gameplay fixtures.

Production prepares source facts before the authority write transaction.
Legacy in-memory fixtures have no store lock, so this adapter retains their
compact setup without making unsafe admission available to the game binary.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
applyPlayerAbnormalInDoor

Only detached fixtures use this adapter. Real-store tests exercise preparation
outside the write and call the same commit operation as production.
================
*/
func (rt *Runtime) applyPlayerAbnormalInDoor(division string, character *enterworld.Character, damaged bool, records []abnormal.Record, now int64) *playerAbnormalOwner {
	if !damaged && len(records) == 0 {
		return nil
	}
	owner := rt.newPlayerAbnormalOwner(division, character, now)
	owner.sources = rt.capturePlayerAbnormalSources(division, character, owner.block, records)
	owner.applyHit(abnormal.HitContext{Magical: damaged, Attack: damaged}, records)
	return owner
}

/*
================
equipCombatTestPet

Give COS tests a real reference identity and independent combat parameters.
The damage adapter must never fill a missing reference with its rider's stats.
================
*/
func equipCombatTestPet(t *testing.T, rt *Runtime, character *enterworld.Character, band uint16) *enterworld.CharacterRef {
	t.Helper()
	gid, valid := enterworld.CosObjectIDForCharacter(character)
	if !valid {
		t.Fatal("fixture has no COS identity")
	}
	ref := &enterworld.CharacterRef{RefObjID: 9, Codename: "PET", TidWord: band<<11 | 0x1c6, CanRide: band == 1 || band == 2,
		Level: 1, MaxHP: 1_000_000, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
		Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 10, HitRate: 10}}
	items := cosTestItemSource{staticItemSource: testItems(), characters: map[string]*enterworld.CharacterRef{ref.Codename: ref}}
	rt.deps.(*enterworld.Deps).Items = items
	character.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: ref.RefObjID, Codename: ref.Codename,
		Level: ref.Level, CurrentHP: ref.MaxHP, CurrentMP: ref.MaxMP, StateFlags: 1, Summoned: true}
	return ref
}
