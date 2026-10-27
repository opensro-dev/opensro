package enterworld

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TWO-SIDED FIXTURE (the internal/game/world/simulation peer_spawn_row precedent): pin the
// REAL enter-world packet sequence the bootstrap emitter builds for the
// canonical test character, so the client harness can drive the SAME bytes
// through its REAL entered chain (missionPacketBridge 0x379d/0x32b3/0x31db
// accumulation -> OnMyCharacterEntered -> CICPlayer_New + DeserializeFull)
// and then run behavioral pins against the resulting local-player twin
// (server-wave W5: the 0x3122 stateType-1 gait apply falsifiers).
//
// If this emitter drifts, THIS gate fails; if the pinned bytes stop feeding
// the client's entered chain, the client parity harness fails. Nothing re-implements either side.

type enteredFixturePacket struct {
	NativeOpcode uint16 `json:"nativeOpcode"`
	PayloadHex   string `json:"payloadHex"`
}

// enteredFixtureSeed mirrors EXACTLY what the live scene host passes to
// seedWipMissionBootstrapEnv before the packet loop (CPSMission.tsx ~1133):
// the login stat block sources and the local player's RefObjData selectors.
type enteredFixtureSeed struct {
	MaxHp          int64  `json:"maxHp"`
	MaxMp          int64  `json:"maxMp"`
	Strength       int64  `json:"strength"`
	Intellect      int64  `json:"intellect"`
	ModelRef       uint32 `json:"modelRef"`
	SexSelector1ac int    `json:"sexSelector1ac"`
	CountryByte9c  int    `json:"countryByte9c"`
}

type enteredFixture struct {
	Comment         []string               `json:"comment"`
	CharacterName   string                 `json:"characterName"`
	CharacterID     int64                  `json:"characterId"`
	Gid             uint32                 `json:"gid"`
	RegionID        uint16                 `json:"regionId"`
	Seed            enteredFixtureSeed     `json:"seed"`
	Packets         []enteredFixturePacket `json:"packets"`
	RefItemSnapshot []RefItemRow           `json:"refItemSnapshot"`
	EquipItems      []EquipItemRow         `json:"equipItems"`
}

// liveItemNativeFieldsByCodename loads the real itemdata numeric fields from the
// recorded live bootstrap (testdata/live_bootstrap_asd2_full.json). The
// client's entered chain deserializes each inventory item through the folded
// PK2 readers (client 78BD00 durability/variance derivations reject an absent
// column loudly), so the fixture's refItemSnapshot must carry the same full
// records the live server ships - never invented minima.
func liveItemNativeFieldsByCodename(t *testing.T) map[string]NativeFields {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "live_bootstrap_asd2_full.json"))
	if err != nil {
		t.Skipf("live full fixture unavailable: %v", err)
	}
	var live struct {
		RefItemSnapshot []RefItemRow `json:"refItemSnapshot"`
	}
	if err := json.Unmarshal(raw, &live); err != nil {
		t.Fatalf("parse live full fixture: %v", err)
	}
	records := make(map[string]NativeFields, len(live.RefItemSnapshot))
	for _, row := range live.RefItemSnapshot {
		if len(row.NativeFields) > 0 {
			records[row.Codename] = row.NativeFields
		}
	}
	return records
}

func buildEnteredFixture(t *testing.T) enteredFixture {
	t.Helper()
	// The canonical shared character (the TestBuildPacketSequence shape):
	// chinaSpearman "asd2", ID 3 -> gid 100003.
	character := chinaSpearman()
	character.ID = 3
	deps := testDeps(character)
	// Dress the item source with the LIVE itemdata records so the wire
	// refItemSnapshot rows carry the real PK2 columns the client's entered
	// chain derives durability/variance from.
	liveRecords := liveItemNativeFieldsByCodename(t)
	items := deps.Items.(fakeItems)
	for codename, ref := range items {
		if nativeFields, ok := liveRecords[codename]; ok {
			ref.NativeFields = nativeFields
		}
	}
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	if result.NativeResult != 1 {
		t.Fatalf("bootstrap failed: %+v", result)
	}
	if result.LocalPlayerEntry == nil {
		t.Fatal("bootstrap result lacks localPlayerEntry")
	}

	packets := make([]enteredFixturePacket, 0, len(result.Packets))
	for _, packet := range result.Packets {
		body := make([]byte, len(packet.Payload))
		for index, value := range packet.Payload {
			body[index] = byte(value)
		}
		packets = append(packets, enteredFixturePacket{
			NativeOpcode: packet.NativeOpcode,
			PayloadHex:   hex.EncodeToString(body),
		})
	}
	return enteredFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/enterworld/enteredfixture_test.go (TestLocalPlayerEnteredFixturePinned).",
			"The REAL enter-world packet sequence enterworld.Build emits for the canonical",
			"test character (chinaSpearman 'asd2', ID 3 -> gid 100003): reset / char-data",
			"begin / chunk (BuildLocalPlayerEntryPayload) / flush / gid latch / object list.",
			"Regenerate: UPDATE_ENTERED_FIXTURE=1 go test ./internal/game/enterworld/ -run TestLocalPlayerEnteredFixturePinned",
		},
		CharacterName: "asd2",
		CharacterID:   3,
		Gid:           ObjectIDForCharacter(character),
		RegionID:      uint16(result.LocalPlayerEntry.StartProfile.RegionID),
		Seed: enteredFixtureSeed{
			MaxHp:          DerivedMaxHP(result.Character),
			MaxMp:          DerivedMaxMP(result.Character),
			Strength:       CharacterStrength(result.Character),
			Intellect:      CharacterIntellect(result.Character),
			ModelRef:       result.LocalPlayerEntry.ModelRef,
			SexSelector1ac: result.LocalPlayerEntry.SexSelector1AC,
			CountryByte9c:  result.LocalPlayerEntry.CountryByte9C,
		},
		Packets:         packets,
		RefItemSnapshot: result.RefItemSnapshot,
		EquipItems:      result.EquipItems,
	}
}

// TestLocalPlayerEnteredFixturePinned regenerates the sequence through the
// REAL emitter and requires the checked-in fixture to match byte for byte.
func TestLocalPlayerEnteredFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "local_player_entered_fixture.json")
	fresh := buildEnteredFixture(t)

	if os.Getenv("UPDATE_ENTERED_FIXTURE") == "1" {
		blob, err := json.MarshalIndent(fresh, "", "  ")
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(blob, '\n'), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture missing (%v) - run with UPDATE_ENTERED_FIXTURE=1 to generate", err)
	}
	var pinned enteredFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if pinned.Gid != fresh.Gid || pinned.CharacterName != fresh.CharacterName {
		t.Fatalf("fixture identity = %s/%d, emitter builds %s/%d - regenerate",
			pinned.CharacterName, pinned.Gid, fresh.CharacterName, fresh.Gid)
	}
	if len(pinned.Packets) != len(fresh.Packets) {
		t.Fatalf("fixture has %d packets, emitter builds %d - regenerate", len(pinned.Packets), len(fresh.Packets))
	}
	for index, packet := range fresh.Packets {
		got := pinned.Packets[index]
		// The calendar is now sampled from world lifetime, not a fixture seed.
		if packet.NativeOpcode == OpcodeServerClockGidLatch && got.NativeOpcode == packet.NativeOpcode {
			body, err := hex.DecodeString(packet.PayloadHex)
			if err != nil || len(body) != 8 || body[6] >= 24 || body[7] >= 60 || len(got.PayloadHex) != 16 || packet.PayloadHex[:8] != got.PayloadHex[:8] {
				t.Errorf("invalid live clock latch: %s", packet.PayloadHex)
			}
			continue
		}
		if got.NativeOpcode != packet.NativeOpcode || got.PayloadHex != packet.PayloadHex {
			t.Errorf("packet[%d] drifted from the pinned fixture:\n got 0x%04X %s\nwant 0x%04X %s\n(regenerate with UPDATE_ENTERED_FIXTURE=1 and re-run the harness parity test)",
				index, packet.NativeOpcode, packet.PayloadHex, got.NativeOpcode, got.PayloadHex)
		}
	}

	// Sequence-shape sanity against the wire constants (a re-ordered emitter
	// invalidates the client's accumulation contract even with per-packet
	// byte equality).
	wantLeading := []uint16{
		OpcodeResetClient,
		OpcodeMyCharacterData,
		OpcodeMyCharacterChunk,
		OpcodeMyCharacterFlush,
		OpcodeServerClockGidLatch,
	}
	for index, want := range wantLeading {
		if pinned.Packets[index].NativeOpcode != want {
			t.Errorf("packet[%d] opcode = 0x%04X, want 0x%04X", index, pinned.Packets[index].NativeOpcode, want)
		}
	}
	if pinned.Packets[2].PayloadHex == "" {
		t.Error("the 0x32B3 chunk payload is empty - the entered chain would deserialize nothing")
	}
}
