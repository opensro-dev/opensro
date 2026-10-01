/*
===========================================================================

lifecycle_test.go - the complete native abnormal-slot lifecycle

Every published status must obey the same replacement, source and teardown
contract. Callback-specific AI and hit rules remain explicit below.

===========================================================================
*/

package abnormal

import "testing"

/*
================
TestEveryStatusLifecycle

Use all 23 real status slots, excluding the native reserved holes. An equal
record cannot extend a timer; a stronger replacement retains its dead-source
latch, and every exit path must remove the active slot and published mask.
================
*/
func TestEveryStatusLifecycle(t *testing.T) {
	for _, source := range Sources {
		for _, exit := range []string{"expiry", "cure", "death"} {
			var block Block
			owner := &fakeOwner{alive: true, monster: true, hp: 1000, maxHP: 1000,
				maxMP: 1000, block: &block, params: map[uint16]float32{5: 100, 6: 100}}
			record := Record{Status: source.Status, Grade: 1, DurationMs: 1000, SourceGID: 9,
				SourceName: "caster", Param28: 20, Param2C: 20, Param34: 10, Param38: 10,
				Param3C: 20, Param40: 10, Param44: 20, Param48: 10, PeriodMs: 1000}
			if source.Status <= Zombie {
				record.Level = 1
			}
			if !block.Apply(owner, record, 100) || block.Mask != source.Status.Bit() {
				t.Fatalf("status %d exit %s: initial admission failed", source.Status, exit)
			}
			if block.Apply(owner, record, 200) || block.Slots[source.Status].StartedAt != 100 {
				t.Fatalf("status %d: equal strength extended the timer", source.Status)
			}
			block.Slots[source.Status].SourceDied = true
			record.Grade++
			if record.Level != 0 {
				record.Level++
			}
			if !block.Apply(owner, record, 300) || !block.Slots[source.Status].SourceDied {
				t.Fatalf("status %d: stronger replacement lost source latch", source.Status)
			}
			if !block.ForgetSource(9, "caster") || !block.Has(source.Status) || block.Slots[source.Status].SourceGID != 0 {
				t.Fatalf("status %d: source disconnect cured the victim or retained identity", source.Status)
			}
			switch exit {
			case "expiry":
				owner.now = 1300
				block.Update(owner, owner.now)
				if !block.Has(source.Status) {
					t.Fatalf("status %d: expired at equality", source.Status)
				}
				owner.now++
				block.Update(owner, owner.now)
			case "cure":
				block.Clear(owner, source.Status)
			case "death":
				owner.alive = false
				block.ClearAll(owner)
			}
			if block.Active() || block.Mask != 0 {
				t.Fatalf("status %d exit %s: retained mask %x", source.Status, exit, block.Mask)
			}
			block.Update(owner, 2000)
			if block.Active() || block.Mask != 0 {
				t.Fatalf("status %d exit %s: a later tick revived the slot", source.Status, exit)
			}
		}
	}
}

/*
================
TestFearAndConfusionOnlyNotifyNaturalEnd

4A4BD0/4A4F70 require a living monster and an unretired slot for event 15.
Cures and death must not enqueue a fresh AI transition during teardown.
================
*/
func TestFearAndConfusionOnlyNotifyNaturalEnd(t *testing.T) {
	for _, status := range []Status{Fear, Confusion} {
		for _, exit := range []string{"expiry", "cure", "death"} {
			var block Block
			owner := &fakeOwner{alive: true, monster: true, block: &block}
			block.Apply(owner, Record{Status: status, Grade: 1, DurationMs: 1000, SourceGID: 9}, 100)
			if len(owner.ai) != 1 || owner.ai[0][0] != 0x14 {
				t.Fatalf("status %d: missing AI entry %+v", status, owner.ai)
			}
			want := 1
			switch exit {
			case "expiry":
				owner.now = 1101
				block.Update(owner, owner.now)
				want = 2
			case "cure":
				block.Clear(owner, status)
			case "death":
				owner.alive = false
				block.ClearAll(owner)
			}
			if len(owner.ai) != want || want == 2 && owner.ai[1][0] != 0x15 {
				t.Fatalf("status %d exit %s: AI events %+v", status, exit, owner.ai)
			}
		}
	}
}

/*
================
TestHitRetirementUsesSeparateNativeConditions
================
*/
func TestHitRetirementUsesSeparateNativeConditions(t *testing.T) {
	for _, hit := range []HitContext{{}, {Attack: true}, {Magical: true}, {Attack: true, Magical: true}} {
		for _, bomb := range []bool{false, true} {
			var block Block
			owner := &fakeOwner{alive: true, block: &block, roll: true}
			for _, status := range []Status{Root, Sleep, Stun} {
				block.Apply(owner, Record{Status: status, Grade: 1, DurationMs: 1000}, 0)
			}
			if bomb {
				block.Apply(owner, Record{Status: TimeBomb, Grade: 1, DurationMs: 1000}, 0)
			}
			block.BreakOnHit(owner, hit)
			if block.Has(Root) == hit.Magical || block.Has(Sleep) != (!hit.Attack || bomb) || block.Has(Stun) != (!hit.Attack || bomb) {
				t.Fatalf("hit %+v bomb %v: mask %x", hit, bomb, block.Mask)
			}
		}
	}
}
