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
	"opensro.online/server/internal/game/item/wire"
	"strings"
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
				rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, clock.NowMs())
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

/*
================
TestPotionReuseCanPrecedeFinalRecoveryPulse

49B710 admits another Chinese absolute potion after its 1.1-second reuse
lock, while 49A510 still owes pulses from the first. Only the queue front
advances; the second potion's immediate credit does not replace that front.
================
*/
func TestPotionReuseCanPrecedeFinalRecoveryPulse(t *testing.T) {
	c, items, body := recoveryFixture(1)
	c.ModelCodename = "CHAR_CH_MAN_ADVENTURER"
	items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 20
	rt, clock := newTestRuntime(c, items)
	rt.BindRecoverySession(testDivision, c, 1)
	first := rt.HandleItemUse(testDivision, c, body)
	if first.Frames[0].Payload[0] != 1 || enterworld.CurrentHP(c) != 5 {
		t.Fatalf("first potion: HP %d, result %+v", enterworld.CurrentHP(c), first)
	}
	clock.Advance(time.Second)
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 9 {
		t.Fatalf("first queued pulse: HP %d", enterworld.CurrentHP(c))
	}
	clock.Advance(99 * time.Millisecond)
	assertItemUseRefusedUnchanged(t, rt, c, body, wire.ErrCodeItemReuseDelay)
	clock.Advance(time.Millisecond)
	second := rt.HandleItemUse(testDivision, c, body)
	if second.Frames[0].Payload[0] != 1 || enterworld.CurrentHP(c) != 13 {
		t.Fatalf("reuse before first potion finishes: HP %d, result %+v", enterworld.CurrentHP(c), second)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 21 && row.StackCount != 18 {
			t.Fatalf("two accepted uses consumed %d potions", 20-row.StackCount)
		}
	}
	clock.Advance(900 * time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if enterworld.CurrentHP(c) != 17 {
		t.Fatalf("queue advanced more than its first potion: HP %d", enterworld.CurrentHP(c))
	}
	// The host path above proves wiring. Finish just the potion timer so
	// natural regeneration does not add unrelated credits to this total.
	key := recoveryKey{testDivision, strings.ToLower(c.Name)}
	for range 6 {
		clock.Advance(time.Second)
		rt.recoverPotionResident(key, clock.NowMs())
	}
	if enterworld.CurrentHP(c) != 41 {
		t.Fatalf("two five-pulse potions lost or duplicated credit: HP %d", enterworld.CurrentHP(c))
	}
}

/*
================
TestPotionQueuePreservesSameResidentAndRejectsStaleRetirement

A same-session scene admission keeps the actor's queue. A delayed close
from the replaced transport must not clear the replacement actor's queue.
================
*/
func TestPotionQueuePreservesSameResidentAndRejectsStaleRetirement(t *testing.T) {
	for _, transition := range []string{"same-session-entry", "old-session-close"} {
		t.Run(transition, func(t *testing.T) {
			c, items, body := recoveryFixture(1)
			items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 20
			rt, clock := newTestRuntime(c, items)
			// wiring_sessions.go binds actor ownership before recovery. That
			// owner rejects the displaced transport's late close notification.
			rt.BindPetSession(testDivision, c, 2)
			rt.BindRecoverySession(testDivision, c, 2)
			rt.HandleItemUse(testDivision, c, body)
			if transition == "same-session-entry" {
				rt.BindRecoverySession(testDivision, c, 2)
			} else {
				rt.ForgetCharacterSession(testDivision, c.Name, 1)
			}
			clock.Advance(time.Second)
			rt.recoverPotionResident(recoveryKey{testDivision, strings.ToLower(c.Name)}, clock.NowMs())
			if enterworld.CurrentHP(c) != 9 {
				t.Fatalf("%s lost the current resident's pulse: %d", transition, enterworld.CurrentHP(c))
			}
		})
	}
}

/*
================
TestPotionQueueOverdueTicksAdvanceOnePulsePerHostUpdate
================
*/
func TestPotionQueueOverdueTicksAdvanceOnePulsePerHostUpdate(t *testing.T) {
	c, items, body := recoveryFixture(1)
	items["ITEM_ETC_HP_POTION_01"].RecoveryHP = 20
	rt, clock := newTestRuntime(c, items)
	rt.BindRecoverySession(testDivision, c, 1)
	rt.HandleItemUse(testDivision, c, body)
	key := recoveryKey{testDivision, strings.ToLower(c.Name)}
	clock.Advance(10 * time.Second)
	rt.recoverPotionResident(key, clock.NowMs())
	if enterworld.CurrentHP(c) != 9 {
		t.Fatal("late update caught up more than one queued pulse")
	}
	rt.recoverPotionResident(key, clock.NowMs())
	if enterworld.CurrentHP(c) != 9 {
		t.Fatal("same host timestamp replayed a pulse")
	}
	clock.Advance(time.Millisecond)
	rt.recoverPotionResident(key, clock.NowMs())
	if enterworld.CurrentHP(c) != 13 {
		t.Fatal("overdue timer lost its phase")
	}
}
