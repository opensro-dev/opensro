/*
===========================================================================

skillfireshield_family_test.go - all retail Fire Shield books in gameplay

Exercise every shipped rank with its original cost and shield requirement.
The common timed owner must honor book conflicts without stacking resistance
and release each book's contributions on expiry and voluntary cancellation.

===========================================================================
*/

package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
fireShieldRank
================
*/
type fireShieldRank struct {
	code       string
	resistance uint16
	level      uint16
}

/*
================
fireShieldRanks

The retail family has six Phoenix, Flower and King ranks and one Emperor.
Expected powers are independent of the compiled modifier under test.
================
*/
func fireShieldRanks() []fireShieldRank {
	var ranks []fireShieldRank
	for _, book := range []struct {
		name  string
		power uint16
		count int
	}{{"A", 18, 6}, {"B", 38, 6}, {"C", 58, 6}, {"D", 78, 1}} {
		for rank := 1; rank <= book.count; rank++ {
			power := book.power + 3*uint16(rank-1)
			level := 100 - power
			// 590768..5907A0 truncates after x87 division and multiplication.
			// With a power-100 probe, 59% and 53% fall below integer boundaries.
			if power == 41 || power == 47 {
				level--
			}
			ranks = append(ranks, fireShieldRank{
				code:       fmt.Sprintf("SKILL_CH_FIRE_SHIELD_%s_%02d", book.name, rank),
				resistance: power,
				level:      level,
			})
		}
	}
	return ranks
}

/*
================
TestFireShieldEveryRankUsesNativeCostAndRetires
================
*/
func TestFireShieldEveryRankUsesNativeCostAndRetires(t *testing.T) {
	for _, rank := range fireShieldRanks() {
		for _, retire := range []string{"expiry", "cancel"} {
			t.Run(rank.code+"/"+retire, func(t *testing.T) {
				rt, clock, c, row := fireShieldFixture(t, rank.code)
				before := fireShieldProbe(t, rt, c)
				mp := enterworld.CurrentMP(c)
				started := clock.NowMs()
				out := castSelf(rt, c, row.ID)
				if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 ||
					out.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, row.ID) {
					t.Fatalf("cast: %+v", out)
				}
				if got := mp - enterworld.CurrentMP(c); got != int64(row.Consumption.MP) {
					t.Fatalf("MP cost %d, want retail %d", got, row.Consumption.MP)
				}
				for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
					if effect.SkillID == row.ID && effect.ExpiresAtMs != started+int64(row.EffectDurationMs) {
						t.Fatalf("deadline %d, want %d", effect.ExpiresAtMs, started+int64(row.EffectDurationMs))
					}
				}
				buffed := fireShieldProbe(t, rt, c)
				for status := abnormal.Freeze; status <= abnormal.Zombie; status++ {
					if got := buffed[status]; got.Level != rank.level || got.DurationMs == 0 ||
						got.DurationMs >= before[status].DurationMs {
						t.Fatalf("status %v: %+v, resistance %d", status, got, rank.resistance)
					}
				}
				if buffed[abnormal.Sleep] != before[abnormal.Sleep] {
					t.Fatal("Fire Shield changed a non-elemental status")
				}
				if retire == "expiry" {
					clock.Advance(time.Duration(row.EffectDurationMs+1) * time.Millisecond)
				} else {
					clock.Advance(3 * time.Second)
					rt.drainSkillFinalizes(clock.NowMs())
					rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: row.ID}).Encode())
				}
				rt.TickHook()(clock.NowMs())
				if hasSkillEffect(rt, c.Name, row.ID) {
					t.Fatal("retired effect remains active")
				}
				after := fireShieldProbe(t, rt, c)
				for status, record := range before {
					if after[status] != record {
						t.Fatalf("status %v after %s: %+v, want %+v", status, retire, after[status], record)
					}
				}
			})
		}
	}
}

/*
================
TestFireShieldRanksReplaceAndBooksHonorNativeConflicts
================
*/
func TestFireShieldRanksReplaceAndBooksHonorNativeConflicts(t *testing.T) {
	ranks := fireShieldRanks()
	rt, clock, c, _ := fireShieldFixture(t, ranks[0].code)
	var previous uint32
	for _, rank := range ranks {
		row := shippedOffense(t, rank.code)
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
		if previous != 0 {
			c.Skills = append(c.Skills, row.ID)
		}
		c.CurrentMP = testInt64(10000)
		if previous != 0 {
			old, ok := rt.deps.SkillData().SkillByID(previous)
			if !ok {
				t.Fatal("previous row missing")
			}
			if old.Replacement.Group != row.Replacement.Group {
				// Different books share packed state 16 but have distinct native
				// replacement groups. The old book must be cancelled first.
				out := castSelf(rt, c, row.ID)
				frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
				if !ok || len(frame.Payload) != 2 || frame.Payload[0] != 2 || frame.Payload[1] != 12 ||
					!hasSkillEffect(rt, c.Name, previous) || hasSkillEffect(rt, c.Name, row.ID) {
					t.Fatalf("%s did not preserve native book conflict: %+v", rank.code, out)
				}
				rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: previous}).Encode())
				rt.TickHook()(clock.NowMs())
			}
		}
		if out := castSelf(rt, c, row.ID); out.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, row.ID) {
			t.Fatalf("%s cast: %+v", rank.code, out)
		}
		for status, record := range fireShieldProbe(t, rt, c) {
			if status <= abnormal.Zombie && record.Level != rank.level {
				t.Fatalf("%s status %v: %+v; books stacked", rank.code, status, record)
			}
		}
		if previous != 0 && hasSkillEffect(rt, c.Name, previous) {
			t.Fatalf("%s left previous rank %d active", rank.code, previous)
		}
		clock.Advance(10 * time.Second)
		rt.drainSkillFinalizes(clock.NowMs())
		previous = row.ID
	}
	if out := castSelf(rt, c, shippedOffense(t, ranks[0].code).ID); hasSkillEffect(rt, c.Name, shippedOffense(t, ranks[0].code).ID) {
		t.Fatalf("Phoenix replaced Emperor: %+v", out)
	}
}
