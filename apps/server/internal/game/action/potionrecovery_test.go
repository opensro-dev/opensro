/*
===========================================================================

potionrecovery_test.go - authority and lifetime behavior of queued recovery

Drive the resident clock and actual item-use request, including a status cure
between pulses and retirement on death, disconnect and replacement session.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"testing"
	"time"
)

/*
================
TestPotionPulsesUseLiveKeeperAndResidentTick
================
*/
func TestPotionPulsesUseLiveKeeperAndResidentTick(t *testing.T) {
	c, items, body := recoveryFixture(1)
	items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 120
	rt, clock := newTestRuntime(c, items)
	rt.BindRecoverySession(testDivision, c, 1)
	start := clock.NowMs()
	record := abnormal.Record{Status: abnormal.Panic, Grade: 1, DurationMs: 10000, SourceGID: enterworld.ObjectIDForCharacter(c), Param34: 50}
	rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, start)
	result := rt.HandleItemUse(testDivision, c, body)
	if len(result.Broadcast) == 0 || enterworld.CurrentHP(c) != 13 {
		t.Fatalf("first reduced pulse HP %d", enterworld.CurrentHP(c))
	}
	clock.Advance(time.Second - time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 13 {
		t.Fatal("potion pulsed before its timer")
	}
	clock.Advance(time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 25 {
		t.Fatal("resident tick did not credit reduced potion step")
	}
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 25 {
		t.Fatal("duplicate host timestamp replayed recovery")
	}
	rt.clearPlayerAbnormalInDoor(testDivision, c, clock.NowMs())
	clock.Advance(time.Second)
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 50 {
		t.Fatalf("cure did not restore next pulse: %d", enterworld.CurrentHP(c))
	}
}

/*
================
TestPotionQueueRetiresWithCharacterLifetime
================
*/
func TestPotionQueueRetiresWithCharacterLifetime(t *testing.T) {
	for _, end := range []string{"death", "disconnect", "replacement"} {
		t.Run(end, func(t *testing.T) {
			c, items, body := recoveryFixture(1)
			items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 120
			rt, clock := newTestRuntime(c, items)
			rt.BindRecoverySession(testDivision, c, 1)
			rt.HandleItemUse(testDivision, c, body)
			switch end {
			case "death":
				c.CurrentHP = testInt64(0)
				rt.settlePlayerDeathInDoor(testDivision, c, clock.NowMs())
				c.CurrentHP = testInt64(1)
			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
			case "replacement":
				rt.BindRecoverySession(testDivision, c, 2)
			}
			hp := enterworld.CurrentHP(c)
			clock.Advance(time.Second)
			rt.advanceNaturalRecovery(clock.NowMs())
			if enterworld.CurrentHP(c) != hp {
				t.Fatalf("%s leaked queued healing", end)
			}
		})
	}
}
