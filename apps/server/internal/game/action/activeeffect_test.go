package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

func newActiveEffectTestRuntime(t *testing.T, skills staticSkillSource) (*Runtime, *enterworld.Character) {
	t.Helper()
	character := testCharacter()
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      testItems(),
		Skills:     skills,
	}
	return NewRuntime(deps, nil), character
}

func TestCancelActiveEffectUsesExactSkillThenRetiresOnTick(t *testing.T) {
	rt, character := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9},
		// A different rank in the same group cannot cancel this application.
		101: {ID: 101, Group: 9},
	})
	if !rt.ApplyCharacterEffect(testDivision, character.Name, 100, 0x11223344, statuseffect.StateActive, false) {
		t.Fatal("active effect prerequisite did not apply")
	}

	result := rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 101}).Encode())
	assertOpcodes(t, result.Frames, wire.OpActionState)
	if got := result.Frames[0].Payload; len(got) != 2 || got[0] != wire.ActionStateKindRelease || got[1] != 0 {
		t.Fatalf("action close = % X, want [02 00]", got)
	}
	rows := rt.effects.Snapshot(testDivision, character.Name)
	if len(rows) != 1 || rows[0].StopRequested {
		t.Fatal("another rank cancelled the active skill", rows)
	}
	rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 100}).Encode())
	rows = rt.effects.Snapshot(testDivision, character.Name)
	if len(rows) != 1 || !rows[0].StopRequested {
		t.Fatalf("request handler erased the effect instead of clearing its live flag: %+v", rows)
	}

	routed := rt.TickHook()(1_000_000)
	if len(routed) != 1 || routed[0].DivisionID != testDivision || len(routed[0].Frames) != 1 {
		t.Fatalf("retirement route = %+v, want one division broadcast", routed)
	}
	frame := routed[0].Frames[0]
	if frame.Opcode != wire.OpEndedEffectInstances || len(frame.Payload) != 5 ||
		frame.Payload[0] != 1 || binary.LittleEndian.Uint32(frame.Payload[1:]) != 0x11223344 {
		t.Fatalf("ended-instance frame = 0x%04X % X", frame.Opcode, frame.Payload)
	}
	if rows := rt.effects.Snapshot(testDivision, character.Name); len(rows) != 0 {
		t.Fatalf("retired effect remained live: %+v", rows)
	}
	if again := rt.TickHook()(1_000_001); len(again) != 0 {
		t.Fatalf("effect retirement broadcast twice: %+v", again)
	}
}

func TestDrainStoppedCharacterEffectSkipsAbsentActorFallback(t *testing.T) {
	rt, character := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9},
	})
	if !rt.ApplyCharacterEffect(testDivision, character.Name, 100, 0x55667788, statuseffect.StateActive, false) {
		t.Fatal("active effect prerequisite did not apply")
	}
	rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 100}).Encode())
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters = enterworld.StaticCharacterSource{testDivision: nil}
	if routed := rt.TickHook()(1_000_000); len(routed) != 0 {
		t.Fatalf("absent actor fallback emitted frames: %+v", routed)
	}
}

func TestCancelActiveEffectOptionalTokenAndOwnerAreBoundaries(t *testing.T) {
	rt, character := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9},
	})
	other := *character
	other.Name = "other"
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters = enterworld.StaticCharacterSource{testDivision: {character, &other}}

	for _, row := range []struct {
		name  string
		token uint32
	}{
		{character.Name, 11},
		{character.Name, 22},
		{other.Name, 33},
	} {
		if !rt.ApplyCharacterEffect(testDivision, row.name, 100, row.token, statuseffect.StateActive, false) {
			t.Fatalf("apply %s/%d failed", row.name, row.token)
		}
	}

	rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 100, InstanceToken: 22}).Encode())
	rows := rt.effects.Snapshot(testDivision, character.Name)
	if len(rows) != 2 || rows[0].StopRequested || !rows[1].StopRequested {
		t.Fatalf("optional token selected the wrong local instance: %+v", rows)
	}
	if otherRows := rt.effects.Snapshot(testDivision, other.Name); len(otherRows) != 1 || otherRows[0].StopRequested {
		t.Fatalf("request crossed character ownership: %+v", otherRows)
	}
}

func TestNoBuffMarkerBlocksVoluntaryStopUnlessSourceDescriptorAllowsIt(t *testing.T) {
	rt, character := newActiveEffectTestRuntime(t, staticSkillSource{
		100: {ID: 100, Group: 9, VoluntaryCancelBlocked: true},
	})
	if !rt.ApplyCharacterEffect(testDivision, character.Name, 100, 44, statuseffect.StateActive, false) {
		t.Fatal("protected effect did not apply")
	}
	rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 100}).Encode())
	if routed := rt.TickHook()(1_000_000); len(routed) != 0 {
		t.Fatalf("nbuf-protected effect was retired: %+v", routed)
	}

	// The live source descriptor's +0x20 == 1 is the native override.
	if !rt.ApplyCharacterEffect(testDivision, character.Name, 100, 45, statuseffect.StateActive, true) {
		t.Fatal("source-override effect did not apply")
	}
	rt.HandleTargetInteract(testDivision, character,
		(wire.CancelActiveEffectRequest{EffectID: 100, InstanceToken: 45}).Encode())
	if routed := rt.TickHook()(1_000_001); len(routed) != 1 {
		t.Fatalf("source-authorized effect did not retire: %+v", routed)
	}
}

func TestEffectPublicationEntryAndExpiryShareOneOwner(t *testing.T) {
	for _, status := range []bool{false, true} {
		for _, rider := range []bool{false, true} {
			rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: {ID: 100, Group: 9, SpawnToken: true, SpawnStatus: status, EffectRider: rider, EffectDurationMs: 5000}})
			var actor, peers []wire.Frame
			rt.PushCharacterFrames = func(d, n string, f []wire.Frame) { actor = append(actor, f...) }
			rt.PushDivisionPeerFrames = func(d, n string, f []wire.Frame) { peers = append(peers, f...) }
			p := EffectPresentation{Phase: 2}
			if status {
				p.Phase = 1
			}
			if rider {
				p.Rider = 1000
			}
			if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, p, 10000) {
				t.Fatal("apply")
			}
			wantLen := 12
			if status {
				wantLen++
			}
			if rider {
				wantLen += 4
			}
			if len(actor) != 1 || len(peers) != 1 || actor[0].Opcode != wire.OpAttachedEffect || len(actor[0].Payload) != wantLen || string(actor[0].Payload) != string(peers[0].Payload) {
				t.Fatal("actor/peer publication drift")
			}
			entry := rt.entrySkillsAt(testDivision, c.Name, 11000)
			if len(entry) != 1 || *entry[0].Token != 99 || *entry[0].Remaining != 4000+p.Rider || entry[0].Status != p.Phase {
				t.Fatalf("entry lost live effect: %+v", entry)
			}
			// Snapshot does not replay application or reset its deadline.
			rt.entrySkillsAt(testDivision, c.Name, 12000)
			if len(actor) != 1 {
				t.Fatal("entry replayed apply")
			}
			expiry := int64(15000 + p.Rider)
			if rows := rt.TickHook()(expiry - 1); len(rows) != 0 {
				t.Fatal("early expiry")
			}
			if rows := rt.TickHook()(expiry); len(rows) != 0 || len(actor) != 1 {
				t.Fatal("native duration expires strictly after its deadline")
			}
			atDeadline := rt.entrySkillsAt(testDivision, c.Name, expiry)
			if len(atDeadline) != 1 || *atDeadline[0].Remaining != 0 {
				t.Fatal("zero remaining time erased a still-live native effect")
			}
			ended := rt.TickHook()(expiry + 1)
			if len(ended) != 0 || len(actor) != 2 || len(peers) != 2 || actor[1].Opcode != wire.OpEndedEffectInstances || string(actor[1].Payload) != string(peers[1].Payload) {
				t.Fatal("expiry missing teardown")
			}
			if len(rt.entrySkillsAt(testDivision, c.Name, expiry+1)) != 0 || len(rt.TickHook()(expiry+2)) != 0 {
				t.Fatal("expired effect resurrected")
			}
			if len(actor) != 2 || len(peers) != 2 {
				t.Fatal("retirement published twice")
			}
		}
	}
}

func TestEffectZeroDurationAndRiderWrap(t *testing.T) {
	for _, tc := range []struct {
		name            string
		duration, rider uint32
		present         bool
	}{
		{"absent", 0, 0, false},
		{"explicit-zero", 0, 0, true},
		{"rider-wraps-to-zero", 0xffffffff, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: {
				ID: 100, Group: 9, SpawnToken: true, EffectRider: true,
				EffectDurationMs: tc.duration, EffectDurationPresent: tc.present,
			}})
			if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2, Rider: tc.rider}, 0) {
				t.Fatal("zero duration must be a real application")
			}
			rt.TickHook()(0)
			if len(rt.entrySkillsAt(testDivision, c.Name, 0)) != 1 {
				t.Fatal("application absent at its release timestamp")
			}
			rt.TickHook()(1)
			want := 1
			if tc.present {
				want = 0
			}
			if got := len(rt.entrySkillsAt(testDivision, c.Name, 1)); got != want {
				t.Fatalf("entry count %d, want %d", got, want)
			}
		})
	}
}

func TestEffectPublicationRejectsMismatchedRidersBeforeMutation(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: {ID: 100, Group: 9}})
	sends := 0
	rt.PushCharacterFrames = func(d, n string, f []wire.Frame) { sends++ }
	if rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2, Rider: 1}, 0) {
		t.Fatal("accepted illegal rider")
	}
	if sends != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("partial application")
	}
}
