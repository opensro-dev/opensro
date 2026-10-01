package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// W3 base falsifiers for the 0x745A object select/interact handler; W5
// extends, and the NPC-select lane appends the 0xB45A emission pins. The
// wire bytes are the client contract: exactly [u32le gid] (native
// NPC-click trace body 41 0D 03 00, and the sub_692ba0 4-byte append).
// The live roster-NPC grant answers the 0xB45A talk grant (hand-rolled
// oracle bytes below); player and ground grants and every refusal stay
// frameless - see the select.go package comment.

func selectBody(gid uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, gid)
	return out
}

// selectTestRuntime pins the env-derived NPC gate OFF so an ambient
// MISSION_SPAWN_NPCS=1 cannot flake the suite (the movement testRuntime
// precedent); tests that want the roster set NpcSpawn explicitly.
func selectTestRuntime(character *enterworld.Character) *Runtime {
	rt, _ := newTestRuntime(character, testItems())
	rt.NpcSpawn = enterworld.NpcSpawnConfig{}
	return rt
}

func setSelectCharacters(t *testing.T, rt *Runtime, characters enterworld.StaticCharacterSource) {
	t.Helper()
	deps, ok := rt.deps.(*enterworld.Deps)
	if !ok {
		t.Fatalf("runtime dependencies are %T, want *enterworld.Deps test fixture", rt.deps)
	}
	deps.Characters = characters
}

func TestObjectSelectRecordsSelfAndPeerAndGround(t *testing.T) {
	character := testCharacter()
	peer := &enterworld.Character{ID: 9, Name: "peerChar", ModelCodename: "CHAR_EU_MAN1"}
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character, peer}})

	// Self: the clear-marker and self-click send sites name the local
	// player's own entity.
	self := enterworld.ObjectIDForCharacter(character)
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(self)); outcome.Refusal != "" {
		t.Fatalf("self gid refused: %s", outcome.Refusal)
	}
	if gid, ok := rt.Selected.Get(testDivision, character.Name); !ok || gid != self {
		t.Errorf("selection = %d/%v, want self %d recorded", gid, ok, self)
	}

	// A division peer's player entity.
	peerGid := enterworld.ObjectIDForCharacter(peer)
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(peerGid)); outcome.Refusal != "" {
		t.Fatalf("peer gid refused: %s", outcome.Refusal)
	}
	if gid, _ := rt.Selected.Get(testDivision, character.Name); gid != peerGid {
		t.Errorf("selection = %d, want peer %d (re-select replaces)", gid, peerGid)
	}

	// A live ground drop (gid allocated by the registry itself).
	dropped := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02"})
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(dropped.Gid)); outcome.Refusal != "" {
		t.Fatalf("ground gid refused: %s", outcome.Refusal)
	}
	if gid, _ := rt.Selected.Get(testDivision, character.Name); gid != dropped.Gid {
		t.Errorf("selection = %d, want ground %d", gid, dropped.Gid)
	}
}

func TestObjectSelectNpcGidsFollowTheSpawnGate(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	npcGid := rt.NpcRoster[0].ObjectID

	// Roster disabled: the gid exists on no client, accepting it would
	// widen the domain.
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(npcGid)); outcome.Refusal == "" {
		t.Error("NPC gid accepted while MISSION_SPAWN_NPCS is off")
	}

	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(npcGid)); outcome.Refusal != "" {
		t.Errorf("NPC gid refused while roster enabled: %s", outcome.Refusal)
	}

	// Beyond the roster: the next identity is nobody's NPC.
	beyond := rt.NpcRoster[len(rt.NpcRoster)-1].ObjectID + 1
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(beyond)); outcome.Refusal == "" {
		t.Error("gid one past the roster accepted")
	}
}

func TestObjectSelectMonsterGrantCarriesCurrentHPAndEnforcesViewerScope(t *testing.T) {
	character := testCharacter()
	const (
		viewerRegion = 0x62a8
		farRegion    = 0x62ac
	)
	registry := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			1933: {
				RefObjID: 1933,
				TidWord:  0x00c6,
				Codename: "MOB_CH_MANGNYANG",
				MaxHP:    54,
			},
		},
		[]monster.NestRow{
			{
				SpawnPoint:            monster.SpawnPoint{RefObjID: 1933, RegionID: viewerRegion, X: 900, Y: 20, Z: 450},
				RetailEvidence:        true,
				MaxCount:              1,
				HasChampionTactics:    true,
				ChampionGenPercentage: 100,
			},
			{
				SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: farRegion, X: 900, Y: 20, Z: 450},
			},
		},
	))
	// rand() 0: the promotion roll passes and the split lands in the giant band.
	registry.SetRandomSource(func() float64 { return 0 })
	rt := selectTestRuntime(character)
	rt.Monsters = registry
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})

	registry.StartDivision(testDivision)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	visible := registry.InstancesInRegions(testDivision, []uint16{viewerRegion})
	if len(visible) != 1 {
		t.Fatalf("visible monster count = %d, want 1", len(visible))
	}
	monster := visible[0]
	if monster.CurrentHP != 1080 {
		t.Fatalf("giant current HP = %d, want client-scaled 1080", monster.CurrentHP)
	}

	outcome := rt.HandleObjectSelect(testDivision, character, selectBody(monster.Gid))
	if outcome.Refusal != "" {
		t.Fatalf("live in-scope monster refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 {
		t.Fatalf("monster grant carries %d frames, want exactly 1", len(outcome.Frames))
	}
	want := []byte{1}
	want = binary.LittleEndian.AppendUint32(want, monster.Gid)
	want = append(want, 1)
	want = binary.LittleEndian.AppendUint32(want, monster.CurrentHP)
	want = binary.LittleEndian.AppendUint32(want, 0)
	if frame := outcome.Frames[0]; frame.Opcode != wire.OpObjectSelectResult ||
		!bytes.Equal(frame.Payload, want) {
		t.Fatalf("monster 0xB45A = opcode 0x%04X payload % X, want 0xB45A % X",
			frame.Opcode, frame.Payload, want)
	}

	registry.StartDivision(testDivision)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	far := registry.InstancesInRegions(testDivision, []uint16{farRegion})
	if len(far) != 1 {
		t.Fatalf("far monster count = %d, want 1", len(far))
	}
	if refused := rt.HandleObjectSelect(testDivision, character, selectBody(far[0].Gid)); refused.Refusal == "" {
		t.Fatal("out-of-scope monster gid accepted")
	}

	if !registry.Defeat(testDivision, monster.Gid, rt.Now()) {
		t.Fatal("fixture: defeating selected monster failed")
	}
	if defeated := rt.HandleObjectSelect(testDivision, character, selectBody(monster.Gid)); defeated.Refusal == "" {
		t.Fatal("defeated monster gid remained selectable")
	}
}

func TestObjectSelectRefusesUnknownAndMalformedSilently(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)

	cases := [][]byte{
		{},                          // empty
		{0x41, 0x0D, 0x03},          // short
		{0x41, 0x0D, 0x03, 0x00, 0}, // over-long
		selectBody(0xDEADBEEF),      // well-formed, resolves to nothing
	}
	for _, payload := range cases {
		outcome := rt.HandleObjectSelect(testDivision, character, payload)
		if outcome.Refusal == "" {
			t.Errorf("payload % X accepted, want refusal", payload)
		}
	}
	if _, ok := rt.Selected.Get(testDivision, character.Name); ok {
		t.Error("a refusal recorded a selection")
	}

	character.DeletePending = true
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(enterworld.ObjectIDForCharacter(character))); outcome.Refusal == "" {
		t.Error("deletePending accepted")
	}
	character.DeletePending = false

	if outcome := rt.HandleObjectSelect(testDivision, nil, selectBody(1)); outcome.Refusal == "" {
		t.Error("nil character accepted")
	}

	// A deleted-pending PEER is not a selectable object either.
	ghost := &enterworld.Character{ID: 11, Name: "ghost", DeletePending: true}
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character, ghost}})
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(enterworld.ObjectIDForCharacter(ghost))); outcome.Refusal == "" {
		t.Error("delete-pending peer gid accepted")
	}
}

// ---------------------------------------------------------------------------
// W5 extensions.
// ---------------------------------------------------------------------------

// TestObjectSelectAcceptsTheLiveNativeTraceBytes replays the captured retail
// frame LITERALLY: clicking the spawned NPC_EU_SMITH on the live native
// gateway sent 0x745A body 41 0D 03 00 - gid
// 200001 is the stable identity of the first explicit test-roster row. The
// bytes are pasted, not rebuilt through the same encoder the handler uses,
// so an endianness or base drift on either side fails here.
func TestObjectSelectAcceptsTheLiveNativeTraceBytes(t *testing.T) {
	// The traced NPC identity was 200001.
	character := &enterworld.Character{ID: 1, Name: "asd2", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}

	outcome := rt.HandleObjectSelect(testDivision, character, []byte{0x41, 0x0D, 0x03, 0x00})
	if outcome.Refusal != "" {
		t.Fatalf("the captured retail frame refused: %s", outcome.Refusal)
	}
	if outcome.Selected != 200001 {
		t.Errorf("decoded gid = %d, want 200001 (0x00030D41 little-endian)", outcome.Selected)
	}
	if gid, ok := rt.Selected.Get(testDivision, character.Name); !ok || gid != 200001 {
		t.Errorf("selection store = %d/%v, want 200001 recorded", gid, ok)
	}
}

// TestObjectSelectIsDivisionScoped pins the isolation a missing division
// filter would break: a character in ANOTHER division is not a selectable
// object, and a recorded selection is keyed to the acting division only.
func TestObjectSelectIsDivisionScoped(t *testing.T) {
	character := testCharacter()
	stranger := &enterworld.Character{ID: 21, Name: "stranger", ModelCodename: "CHAR_EU_MAN1"}
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{
		testDivision: {character},
		"elsewhere":  {stranger},
	})

	// The stranger's gid is live in "elsewhere", not in the acting division.
	strangerGid := enterworld.ObjectIDForCharacter(stranger)
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(strangerGid)); outcome.Refusal == "" {
		t.Error("another division's character gid accepted")
	}

	// A ground drop in another division is equally out of reach.
	foreignDrop := rt.Ground.Add("elsewhere", grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02"})
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(foreignDrop.Gid)); outcome.Refusal == "" {
		t.Error("another division's ground drop accepted")
	}

	// A grant records under the acting division's key only.
	self := enterworld.ObjectIDForCharacter(character)
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(self)); outcome.Refusal != "" {
		t.Fatalf("self select refused: %s", outcome.Refusal)
	}
	if _, ok := rt.Selected.Get("elsewhere", character.Name); ok {
		t.Error("selection leaked into another division's key")
	}
}

// TestObjectSelectGroundLivenessFollowsTheRegistry pins the LIVE half of the
// liveness gate: a drop is selectable only while the division registry still
// holds it - once picked up (removed), the same gid must refuse, and the
// refusal must not disturb the previously recorded selection.
func TestObjectSelectGroundLivenessFollowsTheRegistry(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})

	dropped := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02"})
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(dropped.Gid)); outcome.Refusal != "" {
		t.Fatalf("live drop refused: %s", outcome.Refusal)
	}

	if _, ok := rt.Ground.Remove(testDivision, dropped.Gid); !ok {
		t.Fatal("fixture: removing the drop failed")
	}
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(dropped.Gid)); outcome.Refusal == "" {
		t.Error("picked-up drop gid still accepted - the gate reads a stale registry")
	}
	// The failed re-select left the prior recorded selection alone.
	if gid, ok := rt.Selected.Get(testDivision, character.Name); !ok || gid != dropped.Gid {
		t.Errorf("prior selection = %d/%v, want the earlier grant %d untouched", gid, ok, dropped.Gid)
	}
}

// ---------------------------------------------------------------------------
// The 0xB45A talk-grant emission (NPC-select lane).
// ---------------------------------------------------------------------------

// b45aGrantOracle hand-rolls the expected 0xB45A grant body with
// encoding/binary (never wire's Writer, so the assertion cannot
// inherit an encoder bug): {u8 result 1}{u32le gid}{u8 vitalsMask 0}
// {u32le capabilityFlags}{u8 npcExtra 0}.
func b45aGrantOracle(gid, flags uint32) []byte {
	out := []byte{0x01}
	out = binary.LittleEndian.AppendUint32(out, gid)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint32(out, flags)
	out = append(out, 0x00)
	return out
}

// TestObjectSelectNpcGrantAnswersTheTalkGrant pins the emission the NPC
// talk window opens from: the live roster-NPC grant carries exactly one
// 0xB45A frame whose bytes match the hand-rolled oracle - NPC_EU_SMITH's
// runtime capability row is 0x03 (shop|talk). The evidence catalogue also
// records action-0xb, but runtime must not advertise that button until its
// gameplay owner is reconstructed.
func TestObjectSelectNpcGrantAnswersTheTalkGrant(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}

	npcGid := rt.NpcRoster[0].ObjectID
	outcome := rt.HandleObjectSelect(testDivision, character, selectBody(npcGid))
	if outcome.Refusal != "" {
		t.Fatalf("NPC grant refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 {
		t.Fatalf("NPC grant carries %d frame(s), want exactly 1", len(outcome.Frames))
	}
	frame := outcome.Frames[0]
	if frame.Opcode != wire.OpObjectSelectResult {
		t.Fatalf("grant frame opcode = 0x%04X, want 0xB45A", frame.Opcode)
	}
	if want := b45aGrantOracle(npcGid, 0x03); !bytes.Equal(frame.Payload, want) {
		t.Errorf("0xB45A payload\n got % X\nwant % X", frame.Payload, want)
	}
	// The grant still records the selection (guild create reads it).
	if gid, ok := rt.Selected.Get(testDivision, character.Name); !ok || gid != npcGid {
		t.Errorf("selection store = %d/%v, want %d recorded", gid, ok, npcGid)
	}
}

// TestObjectSelectPlayerAndGroundGrantsStayFrameless pins the evidence
// boundary: no native response payload has been proven for either successful
// select, so both record selection WITHOUT inventing a 0xB45A body. The
// complete client fold can consume those class arms if a future authority
// supplies a measured shape.
func TestObjectSelectPlayerAndGroundGrantsStayFrameless(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})

	self := enterworld.ObjectIDForCharacter(character)
	outcome := rt.HandleObjectSelect(testDivision, character, selectBody(self))
	if outcome.Refusal != "" {
		t.Fatalf("self grant refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 0 {
		t.Errorf("player grant carries %d frame(s), want none", len(outcome.Frames))
	}

	dropped := rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02"})
	outcome = rt.HandleObjectSelect(testDivision, character, selectBody(dropped.Gid))
	if outcome.Refusal != "" {
		t.Fatalf("ground grant refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 0 {
		t.Errorf("ground grant carries %d frame(s), want none", len(outcome.Frames))
	}
}

// TestObjectSelectUnknownCodenameNpcStillRefreshesBinding pins the typed-NPC
// boundary: a live roster row always answers 0xB45A, including a legitimate
// zero-capability word. Otherwise selecting it leaves the client's previous
// NPC binding/menu alive.
//
// The test appends a synthetic def to this runtime's owned roster. Global
// world policy and parallel runtimes remain isolated.
func TestObjectSelectUnknownCodenameNpcStillRefreshesBinding(t *testing.T) {
	character := testCharacter()
	rt := selectTestRuntime(character)
	savedLength := len(rt.NpcRoster)
	rt.NpcRoster = append(rt.NpcRoster, simulation.NpcDef{
		ObjectID: rt.NpcRoster[savedLength-1].ObjectID + 1,
		RefObjID: 9999,
		TidWord:  0x0146,
		Codename: "NPC_TEST_NO_CAPABILITY_ROW",
		Name:     "Uncharted",
	})

	setSelectCharacters(t, rt, enterworld.StaticCharacterSource{testDivision: {character}})
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}

	unseededGid := rt.NpcRoster[savedLength].ObjectID
	outcome := rt.HandleObjectSelect(testDivision, character, selectBody(unseededGid))
	if outcome.Refusal != "" {
		t.Fatalf("unseeded-codename NPC grant refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 || outcome.Frames[0].Opcode != wire.OpObjectSelectResult {
		t.Fatalf("unseeded-codename grant frames = %#v, want one typed 0xB45A", outcome.Frames)
	}
	want := wire.EncodeNpcObjectSelectResult(unseededGid, 0, 0)
	if !bytes.Equal(outcome.Frames[0].Payload, want) {
		t.Errorf("unseeded-codename 0xB45A = % X, want % X", outcome.Frames[0].Payload, want)
	}
	if gid, ok := rt.Selected.Get(testDivision, character.Name); !ok || gid != unseededGid {
		t.Errorf("selection store = %d/%v, want %d recorded", gid, ok, unseededGid)
	}
}
