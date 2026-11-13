/*
===========================================================================
sookarena_entry_test.go - login chest seeds, recovery and legacy probes.
===========================================================================
*/
package action

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestSookEntryChest
================
*/
func TestSookEntryChest(t *testing.T) {
	for _, phase := range []string{"closed", "claimable", "recoverable"} {
		t.Run(phase, func(t *testing.T) {
			c := instanceWorldTestCharacter(11, "LoginChest")
			rt, _ := sookFixture(t, c)
			if phase != "closed" {
				c.SookChest = &domain.ArenaChest{ID: "saved", Stage: 1, Pending: phase == "claimable", Checkpoint: phase == "recoverable", Items: []domain.InventoryRow{chestTestReward()}}
			}
			before := domain.CloneCharacter(c)
			packets := rt.EntryEventFrames(testDivision, c)
			want := 1
			if phase != "closed" {
				want = 2
			}
			if len(packets) != want || packets[len(packets)-1].NativeOpcode != opArenaChest {
				t.Fatalf("login packets = %+v", packets)
			}
			if phase != "closed" && packets[0].NativeOpcode != opCommerceItemReferences {
				t.Fatal("saved reward references must precede the preview")
			}
			var chest arenaProjection
			if err := json.Unmarshal(packets[len(packets)-1].Payload, &chest); err != nil || chest.Phase != phase {
				t.Fatalf("login chest = %+v, error = %v", chest, err)
			}
			legacy := rt.HandleArenaDecision(testDivision, c, []byte{0}).Frames
			if len(legacy) != len(packets) {
				t.Fatal("legacy probe and login seed differ")
			}
			for i, frame := range legacy {
				if frame.Opcode != packets[i].NativeOpcode || !reflect.DeepEqual(frame.Payload, packets[i].Payload) {
					t.Fatal("legacy probe and login seed differ")
				}
			}
			if !reflect.DeepEqual(c, before) {
				t.Fatal("login seed changed saved character state")
			}
		})
	}
}

/*
================
TestSookEntryRecoveredCheckpointIgnoresOldRun
================
*/
func TestSookEntryRecoveredCheckpointIgnoresOldRun(t *testing.T) {
	c := instanceWorldTestCharacter(11, "RecoveredLogin")
	rt, clock := sookFixture(t, c)
	rt.enterSookArena(testDivision, c)
	run := rt.sookRun(testDivision, c)
	run.program.Started = clock.Now().UnixMilli() - 300000
	run.program.StageStarted = run.program.Started
	run.program.Kills = 1
	rt.advanceSookArena(clock.Now().UnixMilli())
	for _, phase := range []string{"checkpoint", "recoverable"} {
		if phase == "recoverable" {
			// Admission may restore town before the old socket/run is retired.
			rt.recoverSookArena(testDivision, c)
		}
		packets := rt.EntryEventFrames(testDivision, domain.CloneCharacter(c))
		var chest arenaProjection
		if err := json.Unmarshal(packets[len(packets)-1].Payload, &chest); err != nil || chest.Phase != phase {
			t.Fatalf("login chest = %+v, want %s, error = %v", chest, phase, err)
		}
	}
}

/*
================
TestSookEntryComposesNoticesAndFlagOff
================
*/
func TestSookEntryComposesNoticesAndFlagOff(t *testing.T) {
	c := instanceWorldTestCharacter(11, "EventLogin")
	rt, _ := waveTestRuntime(t, true)
	want := rt.HalloweenEntryFrames(testDivision, c)
	if len(want) == 0 {
		t.Fatal("fixture needs active event notices")
	}
	if got := rt.EntryEventFrames(testDivision, c); !reflect.DeepEqual(got, want) {
		t.Fatal("arena off changed Halloween notices")
	}
	rt.sookArena = &sookArenaRuntime{}
	got := rt.EntryEventFrames(testDivision, c)
	if len(got) != len(want)+1 || !reflect.DeepEqual(got[:len(want)], want) || got[len(want)].NativeOpcode != opArenaChest {
		t.Fatal("arena login seed replaced event notices")
	}
	if got := rt.EntryEventFrames(testDivision, nil); got != nil {
		t.Fatal("nil character received entry packets")
	}
}
