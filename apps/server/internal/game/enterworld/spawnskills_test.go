package enterworld

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"opensro.online/server/internal/testsupport/tier"
	"opensro.online/server/internal/transport"
	"os"
	"strconv"
	"testing"
)

func TestShippedSkillBootstrapFrameBudget(t *testing.T) {
	tier.Long(t, "the whole shipped skill catalogue fits the entry frame budget")
	rows := sharedShippedSkills(t).SpawnSkillRows()
	blob, err := EnterWorldBlob(&BootstrapResult{NativeResult: 1, RefSkillSnapshot: rows})
	if err != nil {
		t.Fatal(err)
	}
	frame := transport.Frame{Opcode: transport.OpEnterWorldResult, Payload: transport.EncodeEnterWorldResult(transport.EnterWorldResult{OK: true, Blob: blob})}
	t.Logf("shipped skill bootstrap: %d rows, %d encoded bytes (limit %d)", len(rows), frame.EncodedLen(), transport.MaxFrameBytes)
	// Reserve space for the character, item and object references in the full entry.
	if frame.EncodedLen()+(2<<20) > transport.MaxFrameBytes {
		t.Fatal("skill catalogue exhausts world-entry frame budget")
	}
}

func TestSpawnStatusWalkDoesNotInterpretParameterValuesAsTags(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69] = strconv.Itoa(0x617474)
	fields[70] = strconv.Itoa(0x65667461)
	if encodedSpawnStatus(fields) {
		t.Fatal("payload stole efta ownership")
	}
	fields[75] = strconv.Itoa(0x65667461)
	if !encodedSpawnStatus(fields) {
		t.Fatal("efta marker missing")
	}
	fields[69] = strconv.Itoa(0x73736f75)
	if encodedSpawnStatus(fields) {
		t.Fatal("ssou did not terminate")
	}
}
func TestShippedSpawnSkillReferencesPublishThroughBootstrap(t *testing.T) {
	tier.Long(t, "every shipped spawn-skill row publishes through the bootstrap")
	rows := sharedShippedSkills(t).SpawnSkillRows()
	if len(rows) < 27000 {
		t.Fatalf("incomplete skill authority: %d", len(rows))
	}
	tokens, statuses := 0, 0
	for i, row := range rows {
		if i > 0 && rows[i-1].ID >= row.ID {
			t.Fatal("unordered/duplicate references")
		}
		if row.Token {
			tokens++
		}
		if row.Status {
			statuses++
		}
		if row.UI != nil {
			skill, ok := sharedShippedSkills(t).SkillByID(row.ID)
			if !ok || row.UI.NameSymbol != skill.NameSymbol || row.UI.Icon != skill.Icon {
				t.Fatalf("skill %d lost native name/icon", row.ID)
			}
			if row.ID == 3 && (row.UI.NameSymbol != "SN_SKILL_CH_SWORD_SMASH_A" || row.UI.Icon != "skill\\china\\sword_smash_a.ddj") {
				t.Fatalf("shipped skill 3 presentation changed: %+v", row.UI)
			}
		}
	}
	if tokens == 0 || statuses == 0 {
		t.Fatalf("missing branch references: token=%d status=%d", tokens, statuses)
	}
	payload, err := json.Marshal(&BootstrapResult{NativeResult: 1, RefSkillSnapshot: rows})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Complete shipped skill bootstrap: %d rows, %d JSON bytes", len(rows), len(payload))
	var decoded struct {
		Rows []SpawnSkillRow `json:"refSkillSnapshot"`
	}
	if err = json.Unmarshal(payload, &decoded); err != nil || len(decoded.Rows) != len(rows) {
		t.Fatal("bootstrap dropped skill authority", err)
	}
	for i, row := range decoded.Rows {
		if row.EffectRider != rows[i].EffectRider || row.EffectDurationMs != rows[i].EffectDurationMs || row.ZeroEffectDuration != rows[i].ZeroEffectDuration {
			t.Fatal("bootstrap dropped effect layout")
		}
	}
}
func TestSpawnSkillStatusMatchesOriginalExecutableForEveryShippedRow(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-spawn-skill-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Rows []SpawnSkillRow `json:"rows"`
	}
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	actual := sharedShippedSkills(t).SpawnSkillRows()
	if len(actual) != len(oracle.Rows) {
		t.Fatal("native row count differs")
	}
	for i, row := range actual {
		want := oracle.Rows[i]
		if row.ID != want.ID || row.Token != want.Token || row.Status != want.Status || row.EffectRider != want.EffectRider {
			t.Fatalf("native skill mismatch: %+v != %+v", row, want)
		}
	}
}
func TestBuffClassificationUsesTagBoundariesAndPublishesToUI(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69] = strconv.Itoa(0x617474)
	fields[70] = strconv.Itoa(0x62627566)
	if encodedTailContainsTag(fields, 0x62627566) {
		t.Fatal("attack value interpreted as bbuf tag")
	}
	fields[75] = strconv.Itoa(0x62627566)
	if !encodedTailContainsTag(fields, 0x62627566) {
		t.Fatal("bbuf tag missing")
	}
	secondary := 0
	for _, row := range sharedShippedSkills(t).SpawnSkillRows() {
		if row.UI != nil {
			source, ok := sharedShippedSkills(t).SkillByID(row.ID)
			if !ok || row.UI.BuffSecondary != source.BuffSecondary {
				t.Fatal("buff classification lost")
			}
			if row.UI.BuffSecondary {
				secondary++
			}
		}
	}
	if secondary == 0 {
		t.Fatal("shipped secondary buff family absent")
	}
}
func TestPackedFortressWorldHasOneWireAndWebValue(t *testing.T) {
	entry := &LocalPlayerEntry{ModelRef: 1907}
	character := &Character{}
	baseline := BuildLocalPlayerEntryPayload(character, entry, 0, nil)
	if binary.LittleEndian.Uint32(baseline[len(baseline)-6:]) != 0x10001 {
		t.Fatal("no-war sentinel changed")
	}
	for _, packed := range []uint32{0, 0x10002, 0xffffffff} {
		entry.FortressWorld = &packed
		wire := BuildLocalPlayerEntryPayload(character, entry, 0, nil)
		if !bytes.Equal(wire[:len(wire)-6], baseline[:len(baseline)-6]) || binary.LittleEndian.Uint32(wire[len(wire)-6:]) != packed {
			t.Fatal("packed world corrupted entry", packed)
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			World uint32 `json:"fortressWorld"`
		}
		if err = json.Unmarshal(raw, &value); err != nil || value.World != packed {
			t.Fatal("web/wire packed world drift", packed, err)
		}
	}
}

func TestResolvedEntryRetainsCharacterWorldInstance(t *testing.T) {
	packed := uint32(0x20001)
	character := &Character{World: &CharacterWorld{PackedInstance: &packed}}
	entry := ResolveLocalPlayerEntry(character, nil)
	payload := BuildLocalPlayerEntryPayload(character, &entry, 0, nil)
	if entry.FortressWorld == nil || *entry.FortressWorld != packed || binary.LittleEndian.Uint32(payload[len(payload)-6:]) != packed {
		t.Fatal("character world instance lost during entry construction")
	}
	packed = 0x30001
	if *entry.FortressWorld != 0x20001 {
		t.Fatal("entry retained mutable character world pointer")
	}
}
func TestEffectRiderMarkersRespectParameterOwnership(t *testing.T) {
	for _, tag := range []int64{0x52504255, 0x53544455, 0x44544452} {
		fields := make([]string, 118)
		for i := range fields {
			fields[i] = "0"
		}
		fields[69] = strconv.FormatInt(tag, 10)
		if encodedEffectRider(fields) {
			t.Fatal("standalone argument became getv")
		}
		fields[69] = strconv.Itoa(0x67657476)
		fields[70] = strconv.FormatInt(tag, 10)
		if !encodedEffectRider(fields) {
			t.Fatal("getv rider missing")
		}
		fields[71] = strconv.Itoa(0x65667461)
		if !encodedSpawnStatus(fields) {
			t.Fatal("status after getv lost")
		}
		fields[69] = strconv.Itoa(0x617474)
		fields[70] = strconv.Itoa(0x67657476)
		fields[71] = strconv.FormatInt(tag, 10)
		if encodedEffectRider(fields) {
			t.Fatal("attack arguments became getv")
		}
	}
}
func TestEntrySkillsWireMatchesSemanticSnapshot(t *testing.T) {
	token, remaining := uint32(99), uint32(4000)
	c := &Character{Name: "fixture"}
	entry := &LocalPlayerEntry{ModelRef: 1907}
	empty := BuildLocalPlayerEntryPayload(c, entry, 0, nil)
	entry.SpawnSkills = []EntrySkill{{ID: 7, Token: &token, Remaining: &remaining, Status: 1, HasStatus: true}}
	payload := BuildLocalPlayerEntryPayload(c, entry, 0, nil)
	prefix := 0
	for prefix < len(empty) && empty[prefix] == payload[prefix] {
		prefix++
	}
	want := []byte{1, 7, 0, 0, 0, 99, 0, 0, 0, 0xa0, 0x0f, 0, 0, 1}
	if !bytes.Equal(payload[prefix:prefix+len(want)], want) || !bytes.Equal(payload[prefix+len(want):], empty[prefix+1:]) {
		t.Fatal("local effect bytes changed following name fields")
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var decoded LocalPlayerEntry
	if err = json.Unmarshal(raw, &decoded); err != nil || len(decoded.SpawnSkills) != 1 || *decoded.SpawnSkills[0].Remaining != 4000 {
		t.Fatal("JSON lost live effect", err)
	}
}

func TestEntrySkillsRejectMalformedPublication(t *testing.T) {
	token := uint32(1)
	for _, rows := range [][]EntrySkill{{{ID: 1, Token: &token, Status: 2}}, {{ID: 1, Remaining: &token, Status: 2}}, {{ID: 1, Status: 1}}, {{ID: 0, Status: 2}}, make([]EntrySkill, 256)} {
		if validEntrySkills(rows) {
			t.Fatal("malformed entry effect admitted")
		}
		if payload := BuildLocalPlayerEntryPayload(&Character{Name: "asd2"}, &LocalPlayerEntry{SpawnSkills: rows}, 0, nil); payload != nil {
			t.Fatal("malformed entry effect serialized")
		}
	}
}

func TestBuffDurationAdmissionKeepsAbsentAndExplicitZeroDistinct(t *testing.T) {
	fields := make([]string, 74)
	for i := range fields {
		fields[i] = "0"
	}
	if encodedTailContainsTag(fields, 0x64757261) {
		t.Fatal("absent dura became a zero-duration descriptor")
	}
	fields[69] = strconv.Itoa(0x64757261)
	if !encodedTailContainsTag(fields, 0x64757261) || encodedEffectDuration(fields) != 0 {
		t.Fatal("explicit zero duration lost")
	}
	fields[70] = "1000"
	if encodedEffectDuration(fields) != 1000 {
		t.Fatal("nonzero duration lost")
	}
	for _, row := range sharedShippedSkills(t).SpawnSkillRows() {
		skill, ok := sharedShippedSkills(t).SkillByID(row.ID)
		if !ok || row.ZeroEffectDuration != (skill.EffectDurationPresent && skill.EffectDurationMs == 0) {
			t.Fatalf("duration admission changed for %d", row.ID)
		}
	}
}

func TestBuffDescriptorParametersStayInTheirPrimaryBlock(t *testing.T) {
	fields := make([]string, 90)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69] = strconv.Itoa(0x6c6e6b73)
	if !encodedPrimaryParameterEquals(fields, 0x6c6e6b73, 3, 0, false) {
		t.Fatal("lnks zero fourth parameter lost")
	}
	fields[73] = "1"
	if encodedPrimaryParameterEquals(fields, 0x6c6e6b73, 3, 0, false) {
		t.Fatal("visible link hidden")
	}
	fields[74] = strconv.Itoa(0x656672)
	fields[75] = "3"
	if !encodedPrimaryParameterEquals(fields, 0x656672, 0, 3, true) {
		t.Fatal("efr kind 3 missing")
	}
	fields[74] = strconv.Itoa(0x73736f75)
	fields[75] = strconv.Itoa(0x656672)
	fields[76] = "3"
	if encodedPrimaryParameterEquals(fields, 0x656672, 0, 3, true) {
		t.Fatal("secondary block changed primary timer")
	}
}

// efr writes one of three distinct pointers; lnks rewrites a single pointer.
func TestBuffDescriptorRepeatedTagsPreserveNativePointerOwnership(t *testing.T) {
	fields := make([]string, 100)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69], fields[70] = strconv.Itoa(0x656672), "3"
	fields[76], fields[77] = strconv.Itoa(0x656672), "1"
	if !encodedPrimaryParameterEquals(fields, 0x656672, 0, 3, true) {
		t.Fatal("later efr=1 cleared the distinct efr=3 pointer")
	}
	fields[83], fields[87] = strconv.Itoa(0x6c6e6b73), "0"
	fields[88], fields[92] = strconv.Itoa(0x6c6e6b73), "1"
	if encodedPrimaryParameterEquals(fields, 0x6c6e6b73, 3, 0, false) {
		t.Fatal("earlier lnks pointer survived replacement")
	}
}

// Client 6DE630, 8608A0 and 85CE40 read hste/hst2, hide and dttp pointers from the
// same record that classifies the buff; the UI projection carries them intact.
func TestBuffViewerSuppressionParametersPublishToUI(t *testing.T) {
	rows := map[uint32]SpawnSkillRow{}
	for _, row := range sharedShippedSkills(t).SpawnSkillRows() {
		rows[row.ID] = row
	}
	for id, want := range map[uint32]SkillUiStatusLevel{7929: {Mask: 1, Level: 3}, 7934: {Mask: 1, Level: 8}} {
		if row := rows[id]; row.UI == nil || row.UI.Hide == nil || *row.UI.Hide != want || row.UI.Detect != nil {
			t.Fatalf("hide %d: %+v", id, row.UI)
		}
	}
	if row := rows[7177]; row.UI == nil || row.UI.Detect == nil || *row.UI.Detect != (SkillUiStatusLevel{Mask: 5, Level: 3}) {
		t.Fatalf("dttp lost: %+v", row.UI)
	}
	for _, id := range []uint32{5410, 20060} {
		if row := rows[id]; row.UI == nil || row.UI.SpeedBuff == nil || !row.UI.SpeedBuff.Active {
			t.Fatalf("speed buff %d lost: %+v", id, row.UI)
		}
	}
	fields := make([]string, 90)
	for i := range fields {
		fields[i] = "0"
	}
	fields[69] = strconv.Itoa(0x68737465)
	if got := encodedSpeedBuff(fields); !got.Present || got.Active {
		t.Fatalf("zero hste must stay a suppressed speed buff: %+v", got)
	}
	fields[71], fields[72], fields[73] = strconv.Itoa(0x68696465), "1", "4"
	fields[75], fields[76], fields[77] = strconv.Itoa(0x68696465), "2", "6"
	if got := encodedStatusLevel(fields, 0x68696465); got != (SkillStatusLevel{Present: true, Mask: 2, Level: 6}) {
		t.Fatalf("later hide block must replace the pointer: %+v", got)
	}
}
