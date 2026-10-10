package simulation

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

func peerSession(id, division string, characterID int64, name string) SessionSnapshot {
	return SessionSnapshot{
		SessionID:   id,
		DivisionID:  division,
		CharacterID: characterID,
		World:       DefaultWorldState(EuropeStartProfile()),
		Appearance: &PeerAppearance{
			RefObjID:      1907,
			Name:          name,
			BodyShapeByte: 4,
		},
	}
}

func peerFramesTo(push *fakePusher, sessionID string, opcode uint16) [][]byte {
	var out [][]byte
	for _, pushed := range push.toSession {
		if pushed.sessionID != sessionID {
			continue
		}
		for _, frame := range pushed.frames {
			if frame.Opcode == opcode {
				out = append(out, frame.Payload)
			}
		}
	}
	return out
}

// TestTickerSpawnsPeerOncePerViewer: two same-division sessions each receive
// exactly one 0x30D7 spawn row for the OTHER character - never for
// themselves, and never twice across ticks.
func TestTickerSpawnsPeerOncePerViewer(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source := &fakeSource{sessions: []SessionSnapshot{
		peerSession("s1", "DIV_A", 1, "PeerA"),
		peerSession("s2", "DIV_A", 2, "PeerB"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(startMs)
	ticker.RunTick(startMs + 250)

	expectations := []struct {
		viewer   string
		peerName string
		peerGid  uint32
	}{
		{"s1", "PeerB", PlayerObjectID(2)},
		{"s2", "PeerA", PlayerObjectID(1)},
	}
	for _, want := range expectations {
		rows := peerFramesTo(push, want.viewer, wire.OpSingleObjectSpawn)
		if len(rows) != 1 {
			t.Fatalf("%s received %d spawn rows, want exactly 1", want.viewer, len(rows))
		}
		decoded, err := wire.DecodePlayerSpawnRow(rows[0], map[uint32]uint16{}, true)
		if err != nil {
			t.Fatalf("%s spawn row decode: %v", want.viewer, err)
		}
		if decoded.Name != want.peerName || decoded.Gid != want.peerGid {
			t.Fatalf("%s saw %q gid %d, want %q gid %d", want.viewer, decoded.Name, decoded.Gid, want.peerName, want.peerGid)
		}
		if decoded.RefObjID != 1907 || decoded.BodyShapeByte != 4 || decoded.AppearFlag != 1 {
			t.Fatalf("%s spawn row = %+v", want.viewer, decoded)
		}
		// The pose is the LIVE plane (no segment in flight -> the spawn).
		spawn := DefaultWorldState(EuropeStartProfile()).Spawn
		if decoded.RegionID != spawn.RegionID || decoded.X != float32(spawn.X) || decoded.Z != float32(spawn.Z) {
			t.Fatalf("%s spawn pose = %+v, want the live spawn %+v", want.viewer, decoded.Position, spawn)
		}
	}
}

/*
================
TestPeerSpawnCarriesItsItemReferencesFirst

A viewer may not hold the references for items a peer acquired after the
viewer's bootstrap (#340): the spawn push leads with the references for
the worn set and a player skin's copied equipment, then the row.
================
*/
func TestPeerSpawnCarriesItsItemReferencesFirst(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	const referenceOpcode = 14
	peer := peerSession("s2", "DIV_A", 2, "PeerB")
	peer.Appearance.Equipment = []wire.PlayerEquipItem{{RefObjID: 11459, TypeFlags: wire.PackTypeFlags(3, 1, 6, 2)}}
	peer.Appearance.Skin = wire.TransformSkin{RefObjID: 1907, Player: true, Equipment: [9]uint32{0, 3700}}
	source := &fakeSource{sessions: []SessionSnapshot{peerSession("s1", "DIV_A", 1, "PeerA"), peer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	var asked [][]uint32
	ticker.ItemReferences = func(ids []uint32) []Frame {
		asked = append(asked, append([]uint32(nil), ids...))
		return []Frame{{Opcode: referenceOpcode, Payload: []byte("refs")}}
	}

	ticker.RunTick(startMs)

	for _, pushed := range push.toSession {
		if pushed.sessionID != "s1" {
			continue
		}
		if len(pushed.frames) < 2 || pushed.frames[0].Opcode != referenceOpcode || pushed.frames[1].Opcode != wire.OpSingleObjectSpawn {
			t.Fatalf("s1 push = %+v, want the references, then the spawn row", pushed.frames)
		}
		if pushed.frames[0].ScopeGID != 0 || pushed.frames[1].ScopeGID != PlayerObjectID(2) {
			t.Fatal("the scope change must stay on the spawn row")
		}
	}
	found := false
	for _, ids := range asked {
		if len(ids) == 10 && ids[0] == 11459 && ids[2] == 3700 {
			found = true
		}
	}
	if !found {
		t.Fatalf("reference requests = %v, want PeerB's worn 11459 and skin 3700", asked)
	}
}

// TestTickerDespawnsDepartedPeerAndRespawns: when a peer's session leaves the
// snapshot the viewer gets one 0x36AB for its gid; when it returns, a fresh
// 0x30D7 rides again.
func TestTickerDespawnsDepartedPeerAndRespawns(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	both := []SessionSnapshot{
		peerSession("s1", "DIV_A", 1, "PeerA"),
		peerSession("s2", "DIV_A", 2, "PeerB"),
	}
	source := &fakeSource{sessions: both}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(startMs)

	// Peer s2 departs.
	source.sessions = both[:1]
	ticker.RunTick(startMs + 250)

	despawns := peerFramesTo(push, "s1", wire.OpObjectDespawn)
	if len(despawns) != 1 {
		t.Fatalf("s1 received %d despawns, want 1", len(despawns))
	}
	gid, err := wire.DecodeObjectDespawn(despawns[0])
	if err != nil {
		t.Fatalf("despawn decode: %v", err)
	}
	if gid.Gid != PlayerObjectID(2) {
		t.Fatalf("despawned gid = %d, want %d", gid.Gid, PlayerObjectID(2))
	}

	// Peer s2 returns: one fresh spawn row.
	source.sessions = both
	ticker.RunTick(startMs + 500)
	rows := peerFramesTo(push, "s1", wire.OpSingleObjectSpawn)
	if len(rows) != 2 {
		t.Fatalf("s1 spawn rows across the rejoin = %d, want 2", len(rows))
	}
}

// TestTickerPeerVisibilityFences: a different division never sees the peer,
// and a session without appearance data never spawns anywhere.
func TestTickerPeerVisibilityFences(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	bare := peerSession("s3", "DIV_A", 3, "NoAppearance")
	bare.Appearance = nil
	source := &fakeSource{sessions: []SessionSnapshot{
		peerSession("s1", "DIV_A", 1, "PeerA"),
		peerSession("s2", "DIV_B", 2, "PeerB"),
		bare,
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(startMs)

	if rows := peerFramesTo(push, "s1", wire.OpSingleObjectSpawn); len(rows) != 0 {
		t.Fatalf("s1 sees %d rows, want 0 (s2 is another division, s3 has no appearance)", len(rows))
	}
	if rows := peerFramesTo(push, "s2", wire.OpSingleObjectSpawn); len(rows) != 0 {
		t.Fatalf("s2 sees %d rows across the division fence, want 0", len(rows))
	}
	if rows := peerFramesTo(push, "s3", wire.OpSingleObjectSpawn); len(rows) != 1 {
		t.Fatalf("s3 sees %d rows, want 1 (PeerA is visible; s3's own missing appearance only stops s3 from spawning elsewhere)", len(rows))
	}
}

// ---- The pinned cross-language fixture ----

// peerSpawnFixture is the two-sided contract artifact: this Go test pins the
// emitter output byte-for-byte against testdata/peer_spawn_row_fixture.json,
// and the client parity harness reads the SAME file and drives every payload through the client's REAL
// sub_86afb0 fold chain. Either side drifting fails its own gate.
//
// Regenerate with UPDATE_PEER_SPAWN_FIXTURE=1 go test ./internal/game/world/simulation/ -run
// TestPeerSpawnRowFixturePinned (then re-run the harness test).
type peerSpawnFixture struct {
	Comment   []string                   `json:"comment"`
	Opcode    uint16                     `json:"opcode"`
	Scenarios []peerSpawnFixtureScenario `json:"scenarios"`
}

type peerSpawnFixtureScenario struct {
	Name          string `json:"name"`
	CharacterName string `json:"characterName"`
	ModelRefObjID uint32 `json:"modelRefObjId"`
	// ModelTidWord is the sub_851420 user-leaf classification word the
	// viewer's spawn roster row carries (char 0x02 | tid1 0x04 | tid2 0x20).
	ModelTidWord uint16 `json:"modelTidWord"`
	// CountryByte9c/SexSelector1ac are the client-seed record context bytes
	// (+0x9c/+0x1ac; 0 is a legal wire value per the bootstrap 0|1
	// validation - the wave-3 fixture precedent).
	CountryByte9c  uint8                  `json:"countryByte9c"`
	SexSelector1ac uint8                  `json:"sexSelector1ac"`
	BodyShapeByte  uint8                  `json:"bodyShapeByte"`
	Gid            uint32                 `json:"gid"`
	RegionID       uint16                 `json:"regionId"`
	X              float64                `json:"x"`
	Y              float64                `json:"y"`
	Z              float64                `json:"z"`
	Heading        uint16                 `json:"heading"`
	Equipment      []peerSpawnFixtureItem `json:"equipment"`
	// The guild half of the sub_869df0 non-local tail: a non-empty
	// GuildName arms the client's +0x7bc BindGuild gate, and CrestParamA
	// is the G-crest filename parameter (sub_833d40 @0x833e42). Zero
	// values keep the retail no-guild row shape.
	GuildName      string `json:"guildName,omitempty"`
	GuildID        uint32 `json:"guildId,omitempty"`
	GuildGrantName string `json:"guildGrantName,omitempty"`
	CrestParamA    uint32 `json:"crestParamA,omitempty"`
	// JobType/JobGrade are a suited peer's job mode (the alias rides as
	// CharacterName, as sessionworld.go presents it).
	JobType    uint8  `json:"jobType,omitempty"`
	JobGrade   uint8  `json:"jobGrade,omitempty"`
	PayloadHex string `json:"payloadHex"`
}

type peerSpawnFixtureItem struct {
	RefObjID  uint32 `json:"refObjId"`
	TypeFlags uint16 `json:"typeFlags"`
	OptLevel  uint8  `json:"optLevel"`
	Codename  string `json:"codename"`
}

func fixtureScenarios() []peerSpawnFixtureScenario {
	return []peerSpawnFixtureScenario{
		{
			// The wave-3 golden shape with live values: an unarmed peer.
			Name:          "unarmed-peer",
			CharacterName: "PeerA",
			ModelRefObjID: 1907, // the canonical char-data model fallback
			ModelTidWord:  0x0026,
			BodyShapeByte: 4, // the real asd2 creation body-shape byte
			Gid:           PlayerObjectID(42),
			RegionID:      25000,
			X:             1616, Y: 20, Z: 1650,
			Heading: 0,
		},
		{
			// The real asd2 WORN set (live_bootstrap_asd2_full.json
			// missionInventory equipment-band rows, slot order 1/4/5/6):
			// garments + the CH blade. All equip-band, so every row carries
			// its optLevel byte; nothing maps to visual slot 8 (no TID
			// job or cape is worn by this fixture), so the hold type is 4
			// and the guild-member sub-block rides.
			Name:          "worn-set-peer",
			CharacterName: "PeerB",
			ModelRefObjID: 1907,
			ModelTidWord:  0x0026,
			BodyShapeByte: 4,
			Gid:           PlayerObjectID(43),
			RegionID:      25000,
			X:             1620, Y: 20, Z: 1652,
			Heading: 0x4000,
			Equipment: []peerSpawnFixtureItem{
				{RefObjID: 3643, TypeFlags: 0x18AC, OptLevel: 0, Codename: "ITEM_CH_M_CLOTHES_01_BA_A_DEF"},
				{RefObjID: 3644, TypeFlags: 0x20AC, OptLevel: 0, Codename: "ITEM_CH_M_CLOTHES_01_LA_A_DEF"},
				{RefObjID: 3645, TypeFlags: 0x30AC, OptLevel: 0, Codename: "ITEM_CH_M_CLOTHES_01_FA_A_DEF"},
				{RefObjID: 107, TypeFlags: 0x1B2C, OptLevel: 0, Codename: "ITEM_CH_BLADE_01_A"},
			},
		},
		{
			// A guild member: the non-empty guild name arms the client's
			// +0x7bc BindGuild gate (@0x0086a242 - the +0x7a8 wstring size
			// field) and the @0x0086a12d member sub-block carries the guild
			// id + crestParamA the crest filename derives from
			// (G{prefix}_{guildId}_{crestParamA}.crb, sub_833d40 @0x833e42).
			Name:          "guild-member-peer",
			CharacterName: "PeerC",
			ModelRefObjID: 1907,
			ModelTidWord:  0x0026,
			BodyShapeByte: 4,
			Gid:           PlayerObjectID(44),
			RegionID:      25000,
			X:             1624, Y: 20, Z: 1654,
			Heading:        0x2000,
			GuildName:      "NineSuns",
			GuildID:        3,
			GuildGrantName: "Vanguard",
			CrestParamA:    7,
		},
		{
			// A thief in job mode: the suit (3/1/7/2) in visual slot 8 sets
			// the hold type and the client's active job class (868D00), and
			// the job alias rides as the name.
			Name:          "job-suit-peer",
			CharacterName: "ty4_goods",
			ModelRefObjID: 1907,
			ModelTidWord:  0x0026,
			BodyShapeByte: 4,
			Gid:           PlayerObjectID(45),
			RegionID:      25000,
			X:             1628, Y: 20, Z: 1656,
			Heading: 0,
			Equipment: []peerSpawnFixtureItem{
				{RefObjID: 2163, TypeFlags: 0x13AC, OptLevel: 0, Codename: "ITEM_CH_M_TRADE_THIEF_02"},
			},
			JobType:  2,
			JobGrade: 1,
		},
	}
}

func buildFixturePayload(s peerSpawnFixtureScenario) []byte {
	appearance := PeerAppearance{
		RefObjID:       s.ModelRefObjID,
		Name:           s.CharacterName,
		BodyShapeByte:  s.BodyShapeByte,
		GuildName:      s.GuildName,
		GuildID:        s.GuildID,
		GuildGrantName: s.GuildGrantName,
		CrestParam:     s.CrestParamA,
		JobType:        s.JobType,
		JobGrade:       s.JobGrade,
	}
	for _, item := range s.Equipment {
		appearance.Equipment = append(appearance.Equipment, wire.PlayerEquipItem{
			RefObjID:  item.RefObjID,
			TypeFlags: item.TypeFlags,
			OptLevel:  item.OptLevel,
		})
	}
	return BuildPeerSpawnRow(appearance, s.Gid, Spawn{
		RegionID: s.RegionID, X: s.X, Y: s.Y, Z: s.Z, Angle: s.Heading,
	})
}

// TestPeerSpawnRowFixturePinned regenerates each scenario through the REAL
// emitter and requires the checked-in fixture to match byte for byte.
func TestPeerSpawnRowFixturePinned(t *testing.T) {
	path := filepath.Join("testdata", "peer_spawn_row_fixture.json")

	scenarios := fixtureScenarios()
	for i := range scenarios {
		scenarios[i].PayloadHex = hex.EncodeToString(buildFixturePayload(scenarios[i]))
	}
	fresh := peerSpawnFixture{
		Comment: []string{
			"GENERATED + PINNED by internal/game/world/simulation/peervis_test.go (TestPeerSpawnRowFixturePinned).",
			"Each payloadHex is one 0x30D7 CICUser spawn row from simulation.BuildPeerSpawnRow,",
			"the exact bytes the peer-visibility tick leg pushes.",
			"Regenerate: UPDATE_PEER_SPAWN_FIXTURE=1 go test ./internal/game/world/simulation/ -run TestPeerSpawnRowFixturePinned",
		},
		Opcode:    wire.OpSingleObjectSpawn,
		Scenarios: scenarios,
	}

	if os.Getenv("UPDATE_PEER_SPAWN_FIXTURE") == "1" {
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
		t.Fatalf("fixture missing (%v) - run with UPDATE_PEER_SPAWN_FIXTURE=1 to generate", err)
	}
	var pinned peerSpawnFixture
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	if pinned.Opcode != wire.OpSingleObjectSpawn {
		t.Fatalf("fixture opcode = 0x%04X, want 0x30D7", pinned.Opcode)
	}
	if len(pinned.Scenarios) != len(scenarios) {
		t.Fatalf("fixture has %d scenarios, emitter builds %d - regenerate", len(pinned.Scenarios), len(scenarios))
	}
	for i, scenario := range scenarios {
		got := pinned.Scenarios[i]
		if got.PayloadHex != scenario.PayloadHex {
			t.Errorf("scenario %q payload drifted from the pinned fixture:\n got %s\nwant %s\n(regenerate with UPDATE_PEER_SPAWN_FIXTURE=1 and re-run the harness parity test)",
				scenario.Name, scenario.PayloadHex, got.PayloadHex)
		}
	}

	// The fixture bytes must also decode through the Go decoder - the same
	// TID resolve the client performs.
	for _, scenario := range pinned.Scenarios {
		tids := map[uint32]uint16{}
		for _, item := range scenario.Equipment {
			tids[item.RefObjID] = item.TypeFlags
		}
		payload, err := hex.DecodeString(scenario.PayloadHex)
		if err != nil {
			t.Fatalf("scenario %q hex: %v", scenario.Name, err)
		}
		decoded, err := wire.DecodePlayerSpawnRow(payload, tids, true)
		if err != nil {
			t.Fatalf("scenario %q does not decode: %v", scenario.Name, err)
		}
		if decoded.Name != scenario.CharacterName || decoded.Gid != scenario.Gid {
			t.Fatalf("scenario %q decoded to %q gid %d", scenario.Name, decoded.Name, decoded.Gid)
		}
	}
}
