/*
===========================================================================

movingmarch_test.go - a bard dance never holds the caster in place

Reported: after Moving March (SKILL_EU_BARD_SPEEDUPA_MSPEED_A_01) the bard
could still act but could not walk. The movement lane refuses a ground
command while CGObjChar_IsAttackLocked (4AAB40) holds, and the client holds
its moves while the action count (0xB2CD) is non-zero.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestMovingMarchLeavesTheBardFreeToWalk
================
*/
func TestMovingMarchLeavesTheBardFreeToWalk(t *testing.T) {
	const movingMarch = 9734
	shipped, ok := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t)).SkillByID(movingMarch)
	if !ok {
		t.Fatalf("shipped skill %d is missing", movingMarch)
	}
	rt, clock, character, _ := newCombatTestRuntime(t, 100)
	skills := rt.deps.SkillData().(staticSkillSource)
	// The fixture carries a sword; the dance's harp requirement is not
	// what this test is about.
	shipped.RequiredWeaponKinds = [2]uint8{0xff, 0xff}
	skills[movingMarch] = shipped
	character.Skills = append(character.Skills, movingMarch)

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{ActionId: movingMarch}.Encode())
	t.Logf("cast reply: %+v", result.Frames)
	for step := 0; step <= 40; step++ {
		if rt.PlayerAttackLocked(testDivision, character.Name) {
			t.Fatalf("attack lock still held %d ms after Moving March", step*100)
		}
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.now.UnixMilli())
	}
}
