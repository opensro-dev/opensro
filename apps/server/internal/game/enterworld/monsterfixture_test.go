package enterworld

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// ---- The pinned cross-language monster fixture (WIP seq216 contract) ----
//
// Two-sided pin, the peerVisibilityServerRowsParity pattern: this Go test
// regenerates testdata/monster_spawn_fixture.json from the REAL emitter
// chain (registry -> MonsterObjectListInstances -> MonsterObjectListRows +
// the bootstrap bracket packets) and requires the checked-in file to match
// byte for byte; the client parity harness reads
// the SAME file and drives the payloads through the client's REAL
// client 77BD30 -> 77BDC0 -> 77BFB0 -> 777370 group chain.
// Either side drifting fails its own gate; neither re-implements the other.
//
// Two explicit web-port decisions the fixture makes loud on change:
//   - name-mask bit0 is set and carries the textdataname-resolved retail
//     name, giving the browser a stable label without claiming native
//     producer parity;
//   - create-row speed-index byte 2 selects the walk channel, matching the
//     server's wander timing.
//
// Regenerate (TESTER runs; test suites are TESTER's lane):
//
//	UPDATE_MONSTER_SPAWN_FIXTURE=1 go test ./internal/game/enterworld -run TestMonsterSpawnFixturePinned
type monsterSpawnFixture struct {
	Comment        []string               `json:"comment"`
	RefObjSnapshot []monsterFixtureRefRow `json:"refObjSnapshot"`
	Packets        []monsterFixturePacket `json:"packets"`
	Expect         monsterFixtureExpect   `json:"expect"`
}

type monsterFixtureRefRow struct {
	RefObjID  uint32 `json:"refObjId"`
	TidWord   uint16 `json:"tidWord"`
	Codename  string `json:"codename"`
	NameStrID string `json:"nameStrId,omitempty"`
	Name      string `json:"name,omitempty"`
	Level     uint8  `json:"level,omitempty"`
	MaxHP     uint32 `json:"maxHp,omitempty"`
	Kind      string `json:"kind"`
}

func monsterFixtureRow(row RefObjRow) monsterFixtureRefRow {
	return monsterFixtureRefRow{
		RefObjID:  row.RefObjID,
		TidWord:   row.TidWord,
		Codename:  row.Codename,
		NameStrID: row.NameStrID,
		Name:      row.Name,
		Level:     row.Level,
		MaxHP:     row.MaxHP,
		Kind:      row.Kind,
	}
}

type monsterFixturePacket struct {
	Opcode     uint16 `json:"opcode"`
	PayloadHex string `json:"payloadHex"`
}

type monsterFixtureExpect struct {
	Gid       uint32  `json:"gid"`
	RefObjID  uint32  `json:"refObjId"`
	RegionID  uint16  `json:"regionId"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	WalkSpeed float64 `json:"walkSpeed"`
	RunSpeed  float64 `json:"runSpeed"`
	Name      string  `json:"name"`
}

// buildMonsterFixture drives the normal authored-population emitter path end to end:
// a fresh registry over a deterministic one-nest template, the region-scoped
// population resolver, the 0x3417 row builder, and
// the same 0x30CB/0x330A bracket bytes enterworld.Build wraps rows with
// (enterworld.go: {byListSub=1, u16 count} start, empty finalize).
//
// The registry here is DEDICATED: the fixture pins exactly ONE population
// row (its bytes are consumed by WIP's harness byte-for-byte), so it must
// never inherit rows from the shared ring-test helper (that leak was
// TESTER seq450 RED-3). The nest carries the historical fixture coordinates
// directly; no synthetic player-relative spawn path participates.
func buildMonsterFixture() monsterSpawnFixture {
	registry := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {
			RefObjID: 1933, TidWord: 0x00C6,
			Codename: "MOB_CH_MANGNYANG", NameStrID: "SN_MOB_CH_MANGNYANG",
			Name: "Mangyang", ModelPath: `mob\china\mangnyang.bsr`,
			Level: 1, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100,
		},
	}, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62a8, X: 972, Y: 20, Z: 449}},
	}))
	// The spawn heading is a population draw; rand() 0 pins it to 0.
	registry.SetRandomSource(func() float64 { return 0 })
	config := MonsterSpawnConfig{Enabled: true}
	entry := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: 0x62a8, X: 960, Y: 20, Z: 458}}
	admitMonsterFixture(registry, entry)

	instances := config.MonsterObjectListInstances(registry, "global-official", entry)
	rows := MonsterObjectListRows(registry, "global-official", instances, 0, nil)

	packets := []monsterFixturePacket{
		{Opcode: OpcodeObjectListStart, PayloadHex: hex.EncodeToString([]byte{0x01, byte(len(rows) & 0xff), byte(len(rows) >> 8)})},
	}
	for _, row := range rows {
		payload := make([]byte, len(row.Payload))
		for i, v := range row.Payload {
			payload[i] = byte(v)
		}
		packets = append(packets, monsterFixturePacket{Opcode: row.NativeOpcode, PayloadHex: hex.EncodeToString(payload)})
	}
	packets = append(packets, monsterFixturePacket{Opcode: OpcodeObjectListFinalize, PayloadHex: ""})

	var refRows []monsterFixtureRefRow
	for _, row := range config.MonsterRefObjSnapshot(registry) {
		refRows = append(refRows, monsterFixtureRow(row))
	}

	instance := instances[0]
	return monsterSpawnFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/enterworld/monsterfixture_test.go (TestMonsterSpawnFixturePinned).",
			"The normal region-scoped population object-list bracket for one authored",
			"MOB_CH_MANGNYANG: 0x30CB {byListSub=1, u16 count} start, 0x3417 create row(s)",
			"(shared bionic tail + rarity u8, NO HP), empty 0x330A finalize. Consumed by the",
			"harness monsterServerRowsParity test through the REAL client 77bd30/77bdc0/77bfb0/777370 chain.",
			"refObjSnapshot here carries the fixture row only: the full 178-type roster (A8 CLOSED by",
			"RZ seq234) joins the fixture when scoped population emission lands with Q2.",
			"Regenerate: UPDATE_MONSTER_SPAWN_FIXTURE=1 go test ./internal/game/enterworld -run TestMonsterSpawnFixturePinned",
		},
		RefObjSnapshot: refRows,
		Packets:        packets,
		Expect: monsterFixtureExpect{
			Gid:       instance.Gid,
			RefObjID:  instance.Ref.RefObjID,
			RegionID:  instance.Nest.RegionID,
			X:         instance.Nest.X,
			Y:         instance.Nest.Y,
			Z:         instance.Nest.Z,
			WalkSpeed: instance.Ref.WalkSpeed,
			RunSpeed:  instance.Ref.RunSpeed,
			Name:      instance.Ref.DisplayName(),
		},
	}
}

// ---- the scoped-emission fixture (WIP seq395 scenarios S1-S4) ----

type monsterScopedFixture struct {
	Comment        []string                `json:"comment"`
	RefObjSnapshot []monsterFixtureRefRow  `json:"refObjSnapshot"`
	Scenarios      []monsterScopedScenario `json:"scenarios"`
}

type monsterScopedScenario struct {
	Name    string                 `json:"name"`
	Packets []monsterFixturePacket `json:"packets"`
	Expect  map[string]interface{} `json:"expect"`
}

func packetOf(opcode uint16, payload []byte) monsterFixturePacket {
	return monsterFixturePacket{Opcode: opcode, PayloadHex: hex.EncodeToString(payload)}
}

func packetsOfFrames(frames []simulation.Frame) []monsterFixturePacket {
	out := make([]monsterFixturePacket, 0, len(frames))
	for _, frame := range frames {
		out = append(out, packetOf(frame.Opcode, frame.Payload))
	}
	return out
}

func rowsToPackets(rows []Packet) []monsterFixturePacket {
	out := make([]monsterFixturePacket, 0, len(rows))
	for _, row := range rows {
		payload := make([]byte, len(row.Payload))
		for i, v := range row.Payload {
			payload[i] = byte(v)
		}
		out = append(out, packetOf(row.NativeOpcode, payload))
	}
	return out
}

// buildMonsterScopedFixture drives the REAL scoped emitters end to end:
// the ring login bracket (S1), the eviction shapes incl. the 1-row and
// 2-row despawn brackets that guard the client 712620 span bug (S2), the
// move-while-scoped goal + glide (S3), and same-gid re-enter across
// brackets (S4).
func buildMonsterScopedFixture() monsterScopedFixture {
	refs := map[uint32]monster.MonsterRef{
		1933: {
			RefObjID: 1933, TidWord: 0x00C6,
			Codename: "MOB_CH_MANGNYANG", NameStrID: "SN_MOB_CH_MANGNYANG",
			Name: "Mangyang", ModelPath: `mob\china\mangnyang.bsr`,
			Level: 1, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100,
		},
	}
	regionA := simulation.RegionIDForSectors(16, 16)
	regionB := simulation.RegionIDForSectors(19, 16)
	registry := simulation.NewMonsterState(monster.TemplateFromParts(refs, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionA, X: 100, Y: 10, Z: 100}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionA, X: 300, Y: 10, Z: 300}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionB, X: 500, Y: 10, Z: 500}},
	}))
	// The spawn heading is a population draw; rand() 0 pins it to 0.
	registry.SetRandomSource(func() float64 { return 0 })
	config := MonsterSpawnConfig{Enabled: true}
	// Login inside regionA's first interest block: both A rows share it and
	// regionB is far outside the 320-unit neighbourhood. The position only
	// selects instances; it is not serialized into the pinned fixture.
	entry := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: int64(regionA), X: 200, Y: 10, Z: 200}}
	admitMonsterFixture(registry, entry)

	// S1: the login ring bracket - regionA's two rows, real 0x30CB/0x330A
	// wrapping exactly as enterworld.Build assembles it.
	instances := config.MonsterObjectListInstances(registry, "global-official", entry)
	rows := MonsterObjectListRows(registry, "global-official", instances, 0, nil)
	s1Packets := []monsterFixturePacket{packetOf(OpcodeObjectListStart, []byte{0x01, byte(len(rows) & 0xff), byte(len(rows) >> 8)})}
	s1Packets = append(s1Packets, rowsToPackets(rows)...)
	s1Packets = append(s1Packets, packetOf(OpcodeObjectListFinalize, []byte{}))
	gidA1, gidA2 := instances[0].Gid, instances[1].Gid
	s1 := monsterScopedScenario{
		Name:    "S1-ring-login-bracket",
		Packets: s1Packets,
		Expect: map[string]interface{}{
			"rowCount": float64(len(rows)),
			"gids":     []interface{}{float64(gidA1), float64(gidA2)},
			"regionId": float64(regionA),
		},
	}

	// S2: eviction shapes. The live emitter uses the 0x36AB single for a
	// lone exit and the byListSub=2 bracket for bulk; the 1-row bracket
	// shape is ALSO pinned (generic builder output) because 1- and 2-row
	// spans are exactly what the client 712620 bug dropped.
	despawnSingle := wire.ObjectDespawn{Gid: gidA1}
	s2 := monsterScopedScenario{
		Name: "S2-eviction-shapes",
		Packets: append(append(
			[]monsterFixturePacket{packetOf(wire.OpObjectDespawn, despawnSingle.Encode())},
			packetsOfFrames(simulation.MonsterDespawnBracketFrames([]uint32{gidA1}))...),
			packetsOfFrames(simulation.MonsterDespawnBracketFrames([]uint32{gidA1, gidA2}))...),
		Expect: map[string]interface{}{
			"single":       float64(gidA1),
			"bracket1":     []interface{}{float64(gidA1)},
			"bracket2":     []interface{}{float64(gidA1), float64(gidA2)},
			"emitterShape": "single for one exit; byListSub=2 bracket for bulk",
		},
	}

	// S3: move-while-scoped - the REAL 0xB738 goal (with an optional
	// source block) + a following 0x30E3 re-sync for gidA1.
	goal := simulation.BuildMovementAckPayload(gidA1, simulation.MovementRequest{
		Mode: simulation.MovementAckDestinationMode, RegionID: regionA, X: 120, Y: 10, Z: 140,
	}, &simulation.MovementSource{RegionID: regionA, X: 100, Y: 10, Z: 100})
	glide := wire.ObjectSourceMove{
		Position: wire.Position{RegionID: regionA, X: 110, Y: 10, Z: 120, Heading: 0x2000},
		Gid:      gidA1,
	}
	s3 := monsterScopedScenario{
		Name: "S3-move-while-scoped",
		Packets: []monsterFixturePacket{
			packetOf(simulation.OpMovementAck, goal),
			packetOf(wire.OpObjectSourceMove, glide.Encode()),
		},
		Expect: map[string]interface{}{
			"gid": float64(gidA1), "destX": float64(120), "destZ": float64(140),
			"walkSpeed": float64(8), "runSpeed": float64(22),
		},
	}

	// S4: same-gid re-enter across brackets - spawn row, bulk despawn,
	// byte-identical spawn row again.
	reRow := rowsToPackets(MonsterObjectListRows(registry, "global-official", instances[:1], 0, nil))
	s4Packets := []monsterFixturePacket{packetOf(OpcodeObjectListStart, []byte{0x01, 0x01, 0x00})}
	s4Packets = append(s4Packets, reRow...)
	s4Packets = append(s4Packets, packetOf(OpcodeObjectListFinalize, []byte{}))
	s4Packets = append(s4Packets, packetsOfFrames(simulation.MonsterDespawnBracketFrames([]uint32{gidA1, gidA2}))...)
	s4Packets = append(s4Packets, s4Packets[0:3]...) // the identical re-spawn bracket
	s4 := monsterScopedScenario{
		Name:    "S4-same-gid-reenter",
		Packets: s4Packets,
		Expect:  map[string]interface{}{"gid": float64(gidA1)},
	}

	// S5 (WIP seq419): mid-motion scope-exit - a 0xB738 goal in flight,
	// then the despawn for the same gid BEFORE arrival. The server emits
	// exactly this when a viewer walks out of a wandering monster's ring.
	s5Goal := simulation.BuildMovementAckPayload(gidA2, simulation.MovementRequest{
		Mode: simulation.MovementAckDestinationMode, RegionID: regionA, X: 320, Y: 10, Z: 340,
	}, &simulation.MovementSource{RegionID: regionA, X: 300, Y: 10, Z: 300})
	s5Despawn := wire.ObjectDespawn{Gid: gidA2}
	s5 := monsterScopedScenario{
		Name: "S5-mid-motion-scope-exit",
		Packets: []monsterFixturePacket{
			packetOf(simulation.OpMovementAck, s5Goal),
			packetOf(wire.OpObjectDespawn, s5Despawn.Encode()),
		},
		Expect: map[string]interface{}{"gid": float64(gidA2), "despawnBeforeArrival": true},
	}

	var refRows []monsterFixtureRefRow
	for _, row := range config.MonsterRefObjSnapshot(registry) {
		refRows = append(refRows, monsterFixtureRow(row))
	}
	return monsterScopedFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/enterworld/monsterfixture_test.go (TestMonsterScopedFixturePinned).",
			"Q2 scoped-emission scenarios for the harness (WIP seq395 S1-S4 + seq419 S5): ring login",
			"bracket, eviction shapes (0x36AB single + byListSub=2 brackets at 1 and 2 rows - the exact",
			"spans the client 712620 bug dropped), 0xB738 goal + 0x30E3 glide for a scoped gid, same-gid",
			"re-enter, and mid-motion scope-exit (goal in flight then despawn before arrival).",
			"Live emitter shape: single for one exit, bracket for bulk (flagged in S2).",
			"ROLE (binding, board seq437): monsters spawn at SHIPPED v1.150 npcpos COORDINATES, scoped",
			"per-viewer by a POLICY ring; density/respawn/nest-ownership NOT modelled (tables absent).",
			"Regenerate: UPDATE_MONSTER_SCOPED_FIXTURE=1 go test ./internal/game/enterworld -run TestMonsterScopedFixturePinned",
		},
		RefObjSnapshot: refRows,
		Scenarios:      []monsterScopedScenario{s1, s2, s3, s4, s5},
	}
}

// TestMonsterScopedFixturePinned regenerates the scoped fixture through
// the REAL emitters and requires the checked-in file to match.
func TestMonsterScopedFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "monster_scoped_fixture.json")
	fresh := buildMonsterScopedFixture()

	if os.Getenv("UPDATE_MONSTER_SCOPED_FIXTURE") == "1" {
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
		t.Fatalf("fixture missing (%v) - generate with UPDATE_MONSTER_SCOPED_FIXTURE=1", err)
	}
	var pinned monsterScopedFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if len(pinned.Scenarios) != len(fresh.Scenarios) {
		t.Fatalf("fixture has %d scenarios, emitter builds %d - regenerate", len(pinned.Scenarios), len(fresh.Scenarios))
	}
	for i, scenario := range fresh.Scenarios {
		got := pinned.Scenarios[i]
		if got.Name != scenario.Name || len(got.Packets) != len(scenario.Packets) {
			t.Fatalf("scenario %q drifted (name/count)", scenario.Name)
		}
		for j, packet := range scenario.Packets {
			if got.Packets[j].Opcode != packet.Opcode || got.Packets[j].PayloadHex != packet.PayloadHex {
				t.Errorf("scenario %q packet %d drifted:\n got 0x%04X %s\nwant 0x%04X %s",
					scenario.Name, j, packet.Opcode, packet.PayloadHex, got.Packets[j].Opcode, got.Packets[j].PayloadHex)
			}
		}
	}
}

// TestMonsterSpawnFixturePinned regenerates the fixture through the REAL
// emitter and requires the checked-in file to match byte for byte.
func TestMonsterSpawnFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "monster_spawn_fixture.json")
	fresh := buildMonsterFixture()

	if os.Getenv("UPDATE_MONSTER_SPAWN_FIXTURE") == "1" {
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
		t.Fatalf("fixture missing (%v) - generate with UPDATE_MONSTER_SPAWN_FIXTURE=1", err)
	}
	var pinned monsterSpawnFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}

	if len(pinned.Packets) != len(fresh.Packets) {
		t.Fatalf("fixture has %d packets, emitter builds %d - regenerate", len(pinned.Packets), len(fresh.Packets))
	}
	for i, packet := range fresh.Packets {
		got := pinned.Packets[i]
		if got.Opcode != packet.Opcode || got.PayloadHex != packet.PayloadHex {
			t.Errorf("packet %d drifted from the pinned fixture:\n got 0x%04X %s\nwant 0x%04X %s\n(regenerate + re-run the harness parity test)",
				i, packet.Opcode, packet.PayloadHex, got.Opcode, got.PayloadHex)
		}
	}
	if len(pinned.RefObjSnapshot) != len(fresh.RefObjSnapshot) {
		t.Fatalf("fixture roster rows = %d, emitter builds %d", len(pinned.RefObjSnapshot), len(fresh.RefObjSnapshot))
	}
	for i, row := range fresh.RefObjSnapshot {
		if pinned.RefObjSnapshot[i] != row {
			t.Errorf("roster row %d drifted: got %+v want %+v", i, pinned.RefObjSnapshot[i], row)
		}
	}
	if pinned.Expect != fresh.Expect {
		t.Errorf("expect block drifted:\n got %+v\nwant %+v", pinned.Expect, fresh.Expect)
	}
}
