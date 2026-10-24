package enterworld

import (
	"bytes"
	"testing"
)

// The 0x32B3 skill list rides sub_866e50's second marker-framed loop:
// discarded count byte, then per entry marker 1 + u32 skilldata id + u8
// value, terminated by marker 2. The value byte is 1 (the native entry
// ctor's default, and what the learn ack inserts). These bytes feed the
// client's learned-skill collection directly, so they are pinned here.
func TestCharDataEmitsTheLearnedSkillList(t *testing.T) {
	character := &Character{
		ID:        1,
		Name:      "skillcarrier",
		Masteries: []CharacterMastery{{ID: 257, Level: 1}},
		Skills:    []uint32{2, 0x01020304},
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	// Mastery list (count 1, one row, end) then the skill list.
	want := []byte{
		0x01,                               // mastery count
		0x01, 0x01, 0x01, 0x00, 0x00, 0x01, // marker + mastery 257 + level 1
		0x02,                               // mastery end marker
		0x02,                               // skill count
		0x01, 0x02, 0x00, 0x00, 0x00, 0x01, // marker + skill 2 + value 1
		0x01, 0x04, 0x03, 0x02, 0x01, 0x01, // marker + skill 0x01020304 + value 1
		0x02,             // skill end marker
		0x00, 0x00, 0x00, // quest block (this character has no quest state)
	}
	if !bytes.Contains(payload, want) {
		t.Fatalf("char-data payload does not carry the mastery+skill block\nwant subsequence % X", want)
	}
}

// A character with quest state emits the full three-section quest block
// (sub_8673d0 grammar): completed refs, active SQuestInfo bodies with
// flag-gated fields, and tracked records with the flags&0x02 optional.
// Pinned byte-exact so an emission drift fails HERE, next to the writer.
func TestCharDataEmitsThePopulatedQuestBlock(t *testing.T) {
	character := &Character{
		ID:                1,
		Name:              "questcarrier",
		CompletedQuestIds: []uint32{2}, // QTUTORIAL_CH
		ActiveQuests: []ActiveQuestRecord{
			{RefID: 5, U08: 1, U09: 1, Flags: 0x00}, // QNO_CH_SOLDIER_EA1_1, minimal 3-byte body
			{
				RefID: 29, U08: 2, U09: 1, Flags: 0x5c, // QSP_ALL_POTION_1, every gated block
				Progress: 0x00500000,
				U10:      1,
				Contents: []ActiveQuestContentsNode{
					{Tag: 0, Kind: 1, Description: "AB", ObjectiveValues: []uint32{3}},
					{Tag: 1, Kind: 2, Description: "", ObjectiveSentinel: true},
				},
				TargetIds: []uint32{2005}, // NPC_CH_POTION (npcpos row)
			},
		},
		TrackedQuests: []TrackedQuestRecord{
			{RefID: 29, Flags: 0x03, ValueA: 0x11, Word: 0x2233, Tail6: []uint8{1, 2, 3, 4, 5, 6}, Optional: 0x44556677},
		},
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	want := []byte{
		// skill list: empty framing before the quest block anchors the find.
		0x00, 0x02,
		// section 1: completed
		0x01, 0x02, 0x00, 0x00, 0x00,
		// section 2: active
		0x02,
		0x05, 0x00, 0x00, 0x00, // refId 5
		0x01, 0x01, 0x00, // u08, u09, flags 0 (no gated bytes)
		0x1d, 0x00, 0x00, 0x00, // refId 29
		0x02, 0x01, 0x5c, // u08, u09, flags 0x5c
		0x00, 0x00, 0x50, 0x00, // progress (flags&4)
		0x01,       // u10 (flags&8)
		0x02,       // contents count (flags&0x10)
		0x00, 0x01, // node tag 0, kind 1
		0x02, 0x00, 0x41, 0x42, // wire string "AB"
		0x01, 0x03, 0x00, 0x00, 0x00, // objectiveCount 1 + value 3
		0x01, 0x02, // node tag 1, kind 2
		0x00, 0x00, // empty wire string
		0xff,                   // the objective sentinel: NO value array
		0x01,                   // target count (flags&0x40)
		0xd5, 0x07, 0x00, 0x00, // target 2005
		// section 3: tracked
		0x01,
		0x1d, 0x00, 0x00, 0x00, // refId 29
		0x03, 0x11, // flags, valueA
		0x33, 0x22, // word
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, // tail
		0x77, 0x66, 0x55, 0x44, // optional (flags&2)
	}
	if !bytes.Contains(payload, want) {
		t.Fatalf("char-data payload does not carry the populated quest block\nwant subsequence % X", want)
	}
}

// TestCharDataCompletedQuestClampKeepsCountAlignedWithBodies: unlike the
// mastery/skill lists, the completed-quest count is the reader's loop
// bound (sub_8673d0), so past 255 the emission must clamp - 255 bodies
// under a 255 count, never a wrapped count over fewer bodies - and the
// sections after it stay aligned.
func TestCharDataCompletedQuestClampKeepsCountAlignedWithBodies(t *testing.T) {
	completed := make([]uint32, 300)
	for i := range completed {
		completed[i] = uint32(i + 1)
	}
	character := &Character{
		ID:                1,
		Name:              "questveteran",
		CompletedQuestIds: completed,
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	// The expected subsequence: empty skill framing, the clamped count,
	// exactly 255 quest ids (1..255), then the empty active and tracked
	// section counts in their places.
	var want bytes.Buffer
	want.Write([]byte{0x00, 0x02}) // skill list: count 0, end marker
	want.WriteByte(0xff)           // completed count, clamped
	for i := 1; i <= 255; i++ {
		want.Write([]byte{byte(i), byte(i >> 8), 0x00, 0x00})
	}
	want.Write([]byte{0x00, 0x00}) // active + tracked counts
	if !bytes.Contains(payload, want.Bytes()) {
		t.Fatal("char-data payload does not carry the clamped 255-body completed list")
	}
}

// A record with no learned skills must emit the exact empty framing the
// established wire builder emits: byte-frozen protocol continuity.
func TestCharDataEmptySkillListStaysByteFrozen(t *testing.T) {
	character := &Character{
		ID:        1,
		Name:      "novice",
		Masteries: []CharacterMastery{{ID: 257, Level: 1}},
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	want := []byte{
		0x01,                               // mastery count
		0x01, 0x01, 0x01, 0x00, 0x00, 0x01, // mastery 257 level 1
		0x02,       // mastery end marker
		0x00, 0x02, // skill list: count 0, end marker
		0x00, 0x00, 0x00, // quest block: the EMPTY-state pin (all three sections zero)
	}
	if !bytes.Contains(payload, want) {
		t.Fatalf("skill-less char-data drifted from the frozen empty framing\nwant subsequence % X", want)
	}
}

// StartProfileForRaceProfile adapts the Europe start spec into the wire
// StartProfile shape the entry carries.
func StartProfileForRaceProfile() StartProfile {
	spec := StartProfileForRace(RaceKeyEurope)
	return StartProfile{
		RegionID: spec.RegionID,
		X:        spec.X,
		Y:        spec.Y,
		Z:        spec.Z,
		Angle:    spec.Angle,
	}
}
