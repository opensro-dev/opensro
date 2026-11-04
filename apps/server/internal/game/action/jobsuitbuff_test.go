/*
===========================================================================

jobsuitbuff_test.go - a worn job suit's bonus, tiered by job level

Port-only, not native: every rule is checked with SRO_JOB_SUIT_TIERS off
(native: no writes, no speed factor, no frames) and on.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestJobSuitTiersFromEnv
================
*/
func TestJobSuitTiersFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvJobSuitTiers, value)
		if got := JobSuitTiersFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}

/*
================
TestJobSuitTierLadder
================
*/
func TestJobSuitTierLadder(t *testing.T) {
	for grade, want := range map[int64]int{0: 0, 1: 1, 2: 2, 3: 3, 4: 3, 5: 4, 6: 4, 7: 5} {
		if got := jobSuitTierForGrade(grade); got != want {
			t.Fatalf("grade %d: tier %d, want %d", grade, got, want)
		}
	}
}

/*
================
TestJobSuitWritesAndSpeed

Off, unsuited and wrong-job suits gain nothing; on, the tier's flat
STR/INT and percent speed apply. The tier reads the worn suit's own job.
================
*/
func TestJobSuitWritesAndSpeed(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	c := testCharacter()
	dressThief(c)
	c.Job.Grade = 7
	if rt.jobSuitTier(c) != 0 || rt.jobSuitWrites(c) != nil || rt.jobSuitSpeedFactor(c) != 1 {
		t.Fatal("flag off applied a bonus")
	}
	rt.JobSuitTiers = true
	if got := rt.jobSuitTier(c); got != 5 {
		t.Fatalf("grade 7 tier %d, want 5", got)
	}
	writes := rt.jobSuitWrites(c)
	if len(writes) != 2 || writes[0].Parameter != 1 || writes[0].Value != 20 ||
		writes[1].Parameter != 2 || writes[1].Value != 20 || writes[0].Source != jobSuitBuffModifierSource {
		t.Fatalf("writes %+v", writes)
	}
	if got := rt.jobSuitSpeedFactor(c); got != 1.05 {
		t.Fatalf("speed factor %v, want 1.05", got)
	}
	for grade, want := range map[int64]float32{1: 1, 2: 5, 3: 10, 4: 10, 5: 15, 6: 15, 7: 20} {
		c.Job.Grade = uint8(grade)
		if got := rt.jobSuitWrites(c); len(got) != 2 || got[0].Value != want {
			t.Fatalf("grade %d writes %+v, want STR/INT %v", grade, got, want)
		}
	}
	c.Job.Grade = 7
	c.Job.Type = domain.JobTrader
	if rt.jobSuitTier(c) != 0 || rt.jobSuitWrites(c) != nil {
		t.Fatal("a trader wearing a thief suit was buffed")
	}
	c.Job.Type = domain.JobThief
	c.MissionInventory = nil
	if rt.jobSuitTier(c) != 0 || rt.jobSuitWrites(c) != nil || rt.jobSuitSpeedFactor(c) != 1 {
		t.Fatal("an unsuited player was buffed")
	}
}

/*
================
TestJobSuitWritesDieWithTheSuit

Taking the suit off strips the bonus in the same admission: the writes
read the live dressed state, so the row leaving slot 8 ends them.
================
*/
func TestJobSuitWritesDieWithTheSuit(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.JobSuitTiers = true
	c := testCharacter()
	dressThief(c)
	c.Job.Grade = 5
	if got := rt.jobSuitWrites(c); len(got) != 2 || got[0].Value != 15 {
		t.Fatalf("suited writes %+v, want STR/INT 15", got)
	}
	c.MissionInventory = c.MissionInventory[:0]
	if got := rt.jobSuitWrites(c); got != nil {
		t.Fatalf("writes after undress %+v, want none", got)
	}
	if got := rt.jobSuitSpeedFactor(c); got != 1 {
		t.Fatalf("speed factor after undress %v, want 1", got)
	}
}

/*
================
TestJobSuitTierRaisesTheProjectedStats

The tier's writes reach the stat block the client is sent.
================
*/
func TestJobSuitTierRaisesTheProjectedStats(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	dressForJob(rt, actor, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	actor.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 7, Alias: "Shadow"}
	native, err := rt.PlayerBaseStats(testDivision, actor)
	if err != nil {
		t.Fatal(err)
	}
	if off, _ := rt.PlayerBaseStats(testDivision, actor); off != native {
		t.Fatal("flag off changed the stat block")
	}
	rt.JobSuitTiers = true
	on, err := rt.PlayerBaseStats(testDivision, actor)
	if err != nil || on.StrWord != native.StrWord+20 || on.IntWord != native.IntWord+20 {
		t.Fatalf("str %d->%d int %d->%d (%v)", native.StrWord, on.StrWord, native.IntWord, on.IntWord, err)
	}
}

/*
================
TestJobSuitDressRefreshesTheTier

Completing a wear under the rule adds the tier's stat block to the
frames the native in-place reload already sends; with the rule off the
same dress sends exactly the native frames.
================
*/
func TestJobSuitDressRefreshesTheTier(t *testing.T) {
	counts := map[bool]int{}
	for _, on := range []bool{false, true} {
		rt, clock, c := jobFixture(t)
		rt.JobSuitTiers = on
		c.Job = domain.CharacterJob{Type: domain.JobTrader, Grade: 3, Alias: "Merchant"}
		wear := encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: jobTestSuitSlot, DestSlot: jobSuitSlot, Quantity: 1})
		rt.HandleItemMove(testDivision, c, wear)
		var pushed []wire.Frame
		rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { pushed = append(pushed, frames...) }
		clock.now = clock.now.Add(jobDressSeconds * time.Second)
		rt.advanceJobDresses(clock.NowMs())
		if enterworld.DressedJob(c) != domain.JobTrader {
			t.Fatalf("on=%v: the dress did not complete", on)
		}
		for _, frame := range pushed {
			if frame.Opcode == wire.OpBaseStats {
				counts[on]++
			}
		}
	}
	if counts[true] != counts[false]+1 {
		t.Fatalf("stat blocks on=%d off=%d, want exactly one added by the rule", counts[true], counts[false])
	}
}

/*
================
TestJobSuitTierRefreshesOnGradeRise

A job EXP grant that crosses a tier boundary while the suit is worn
re-sends the stat block at once; under the flag off nothing is added.
================
*/
func TestJobSuitTierRefreshesOnGradeRise(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, actor, _ := newCombatTestRuntime(t, 100)
		dressForJob(rt, actor, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
		actor.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 4, Alias: "Shadow"}
		rt.JobSuitTiers = on
		rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
			c.Job.Grade = 5
			return []wire.Frame{{Opcode: 0x35ee}}, true
		}
		frames, ok := rt.addJobExperience(testDivision, actor, 10)
		if !ok || len(frames) == 0 || frames[0].Opcode != 0x35ee {
			t.Fatalf("on=%v: grant frames %+v", on, frames)
		}
		stats := 0
		for _, frame := range frames {
			if frame.Opcode == wire.OpBaseStats {
				stats++
			}
		}
		if on && stats != 1 {
			t.Fatalf("tier rise sent %d stat blocks, want 1 (frames %+v)", stats, frames)
		}
		if !on && len(frames) != 1 {
			t.Fatalf("flag off added frames: %+v", frames)
		}
	}
}
