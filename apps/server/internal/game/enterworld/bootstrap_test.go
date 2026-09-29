package enterworld

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type fakeItems map[string]*ItemRef

func TestBootstrapJSONOmitsBrowserPresentationPaths(t *testing.T) {
	result := &BootstrapResult{
		NativeResult: nativeResultSuccess,
		RefObjSnapshot: []RefObjRow{{
			RefObjID: 1, TidWord: 2, Codename: "CHAR_TEST", Kind: "npc",
		}},
		RefItemSnapshot: []RefItemRow{{
			RefObjID: 2, TypeFlags: 3, Codename: "ITEM_TEST", Kind: "item",
		}},
		LocalPlayerEntry: &LocalPlayerEntry{RaceKey: RaceKeyChina},
		ChatMessages:     []string{}, SystemMessages: []string{}, Packets: []Packet{},
	}
	blob, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal semantic EnterWorld: %v", err)
	}
	for _, forbidden := range []string{
		"modelPath", "rideModelPath", "riderTransformMode", "dropModel", "icon",
		"portrait", "raceMark", "publicPath", "nativeTexturePath",
	} {
		if strings.Contains(string(blob), `"`+forbidden+`"`) {
			t.Errorf("EnterWorld v2 emitted browser presentation field %q: %s", forbidden, blob)
		}
	}
}

func (f fakeItems) ItemRefByCodename(codename string) (*ItemRef, bool) {
	row, ok := f[codename]
	return row, ok
}

// testItems carries the itemdata rows the china starter roster and the
// measured bug fixtures resolve. TypeIDs [3,1,6,...] are the equipment
// weapon family; garments [3,1,1,...].
func testItems() fakeItems {
	// Country 3 / RequiredSex 2 are the equip-gate PASS values (the Go zero
	// values mean China-only / female-only) - fixture refs must set them or
	// any future equip-gated path would refuse these rows.
	weapon := func(refObjID uint32, codename string) *ItemRef {
		return &ItemRef{RefObjID: refObjID, Codename: codename, TypeIDs: [4]int64{3, 1, 6, 2}, Name: codename, VarianceIntMin1c0: i64(56), Country: 3, RequiredSex: 2}
	}
	garment := func(refObjID uint32, codename string) *ItemRef {
		return &ItemRef{RefObjID: refObjID, Codename: codename, TypeIDs: [4]int64{3, 1, 1, 1}, Name: codename, VarianceIntMin1c0: i64(33), Country: 3, RequiredSex: 2}
	}
	gold := func(refObjID uint32, codename string) *ItemRef {
		return &ItemRef{RefObjID: refObjID, Codename: codename, TypeIDs: [4]int64{3, 3, 5, 0}, Name: "Gold", Country: 3, RequiredSex: 2}
	}
	return fakeItems{
		"ITEM_CH_SPEAR_01_A_DEF":        weapon(3644, "ITEM_CH_SPEAR_01_A_DEF"),
		"ITEM_CH_BLADE_01_A_DEF":        weapon(3633, "ITEM_CH_BLADE_01_A_DEF"),
		"ITEM_CH_BLADE_01_A":            weapon(107, "ITEM_CH_BLADE_01_A"),
		"ITEM_CH_M_CLOTHES_01_BA_A_DEF": garment(11, "ITEM_CH_M_CLOTHES_01_BA_A_DEF"),
		"ITEM_CH_M_CLOTHES_01_LA_A_DEF": garment(12, "ITEM_CH_M_CLOTHES_01_LA_A_DEF"),
		"ITEM_CH_M_CLOTHES_01_FA_A_DEF": garment(13, "ITEM_CH_M_CLOTHES_01_FA_A_DEF"),
		"ITEM_CH_M_HEAVY_01_LA_A":       garment(5049, "ITEM_CH_M_HEAVY_01_LA_A"),
		"ITEM_ETC_GOLD_01":              gold(1, "ITEM_ETC_GOLD_01"),
		"ITEM_ETC_GOLD_02":              gold(2, "ITEM_ETC_GOLD_02"),
		"ITEM_ETC_GOLD_03":              gold(3, "ITEM_ETC_GOLD_03"),
	}
}

func testDeps(characters ...*Character) *Deps {
	return &Deps{
		Roster:            testRoster(),
		Characters:        StaticCharacterSource{DefaultDivisionID: characters},
		Items:             testItems(),
		EquipItemsEnabled: true,
	}
}

func TestBuildFailurePaths(t *testing.T) {
	deps := testDeps(chinaSpearman())

	missing := Build(deps, BootstrapRequest{})
	if missing.NativeResult != 0 || missing.NativeErrorCode != 0x10 || missing.Reason != "missingCharacterName" {
		t.Fatalf("missing name: %+v", missing)
	}
	unknown := Build(deps, BootstrapRequest{CharacterName: "nobody"})
	if unknown.NativeErrorCode != 0x10 || unknown.Reason != "characterNotFound" {
		t.Fatalf("unknown character: %+v", unknown)
	}
	pending := chinaSpearman()
	pending.DeletePending = true
	depsPending := testDeps(pending)
	deleted := Build(depsPending, BootstrapRequest{CharacterName: "asd2"})
	if deleted.NativeErrorCode != 0x02 || deleted.Reason != "deletePending" {
		t.Fatalf("delete pending: %+v", deleted)
	}
}

func TestBuildRefusesACharacterPersistedInsideAnUnauthorizedArea(t *testing.T) {
	character := chinaSpearman()
	regionID := int64(0x7e7e)
	x, y, z, angle := 900.0, 0.0, 920.0, int64(16384)
	character.World = &CharacterWorld{
		Spawn: &WorldSpawn{
			RegionID: &regionID,
			X:        &x,
			Y:        &y,
			Z:        &z,
			Angle:    &angle,
		},
		SpawnSet: true,
	}
	deps := testDeps(character)
	deps.CanEnterWorldRegion = func(candidate *Character, region uint16) bool {
		return region != 0x7e7e || candidate.GMPrivilege
	}

	refused := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if refused.NativeResult != 0 || refused.Reason != "areaAccessDenied" {
		t.Fatalf("ordinary character entered GM area: %+v", refused)
	}
	character.GMPrivilege = true
	accepted := Build(deps, BootstrapRequest{CharacterName: character.Name})
	if accepted.NativeResult != 1 {
		t.Fatalf("GM character was refused: %+v", accepted)
	}
}

func TestBuildFailureEnvelopeShape(t *testing.T) {
	result := Failure(0x10, "characterNotFound")
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"nativeResult", "nativeErrorCode", "nativeError", "packets", "reason"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("failure envelope lacks %q: %s", key, data)
		}
	}
	if len(fields) != 5 {
		t.Errorf("failure envelope has extra keys: %s", data)
	}
	if string(fields["packets"]) != "[]" {
		t.Errorf("failure packets = %s, want []", fields["packets"])
	}
	var nativeError NativeAgentError
	if err := json.Unmarshal(fields["nativeError"], &nativeError); err != nil {
		t.Fatalf("nativeError: %v", err)
	}
	if nativeError.TextKey != "UIO_MSG_ERROR_ID" || nativeError.Presentation != "direct" {
		t.Errorf("0x10 describes as %+v, want UIO_MSG_ERROR_ID/direct", nativeError)
	}
}

func TestDescribeNativeAgentError(t *testing.T) {
	ok := DescribeNativeAgentError(0x01)
	if !ok.OK || ok.Presentation != "none" {
		t.Errorf("0x01 = %+v", ok)
	}
	subcode := DescribeNativeAgentError(0x02)
	if subcode.OK || subcode.Presentation != "subcode" || subcode.StatusTextSuffix != "(S2)" {
		t.Errorf("0x02 = %+v", subcode)
	}
	unknown := DescribeNativeAgentError(0x42)
	if unknown.TextKey != "UIO_MSG_ERROR_SEVER_CONNECT" {
		t.Errorf("unknown code = %+v", unknown)
	}
}

// TestBuildFirstBootstrapSeedsInventory: the first-ever bootstrap initializes
// the authoritative inventory from the creation starter items, persists, and
// the loadout lists what is now worn. A spearman created without armor gets
// the spear only: the native create request carries item id 0 for the
// garments (CharacterCreateRequest_Write), so nothing else is granted.
func TestBuildFirstBootstrapSeedsInventory(t *testing.T) {
	character := chinaSpearman()
	character.ID = 7
	persisted := 0
	deps := testDeps(character)
	deps.MutateCharacter = func(_ *Character, _ string, fn func()) { fn(); persisted++ }

	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	if result.NativeResult != 1 {
		t.Fatalf("bootstrap failed: %+v", result)
	}
	if persisted != 1 {
		t.Errorf("persist hook ran %d times, want 1", persisted)
	}
	if character.Gold == nil || *character.Gold != 0 {
		t.Errorf("first-boot gold = %v, want retail zero", character.Gold)
	}

	rows := character.MissionInventory
	wantSlots := map[int64]string{
		6: "ITEM_CH_SPEAR_01_A_DEF",
	}
	if len(rows) != len(wantSlots) {
		t.Fatalf("seeded %d rows (%+v), want %d", len(rows), rows, len(wantSlots))
	}
	for slot, codename := range wantSlots {
		row := findRowBySlot(rows, slot)
		if row == nil || row.Codename != codename {
			t.Errorf("slot %d = %+v, want %s", slot, row, codename)
		}
	}
	if findRowBySlot(rows, 13) != nil {
		t.Fatal("diagnostic bag witness leaked into starter inventory")
	}

	loadout := result.LocalPlayerEntry.VisualLoadout
	if !reflect.DeepEqual(loadout.Items, []VisualItem{{RefObjID: 3644}}) {
		t.Errorf("first-boot items = %+v, want the now-worn creation spear", loadout.Items)
	}
}

// TestBuildRestoredSessionWornItemsOracle: a restored character wearing the
// picked-up copper blade and the heavy pants is shown with exactly those
// items, in socket order, off the actual bootstrap response.
func TestBuildRestoredSessionWornItemsOracle(t *testing.T) {
	character := chinaSpearman()
	character.ID = 9
	character.Gold = f64ToI64(25000)
	character.MissionInventory = []InventoryRow{
		{Slot: 6, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x32c, VarianceBits: "0", Durability: 56, StackCount: 1},
		{Slot: 1, RefObjID: 11, Codename: "ITEM_CH_M_CLOTHES_01_BA_A_DEF", TypeFlags: 0x8c, VarianceBits: "0", StackCount: 1},
		{Slot: 4, RefObjID: 5049, Codename: "ITEM_CH_M_HEAVY_01_LA_A", TypeFlags: 0x8c, VarianceBits: "0", StackCount: 1},
	}
	deps := testDeps(character)

	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	if result.NativeResult != 1 {
		t.Fatalf("bootstrap failed: %+v", result)
	}
	loadout := result.LocalPlayerEntry.VisualLoadout
	want := []VisualItem{{RefObjID: 11}, {RefObjID: 5049}, {RefObjID: 107}}
	if !reflect.DeepEqual(loadout.Items, want) {
		t.Fatalf("restored items = %+v, want the worn rows in socket order %+v", loadout.Items, want)
	}
	// A restored session must not touch persisted gold.
	if character.Gold == nil || *character.Gold != 25000 {
		t.Errorf("restored gold = %v, want untouched 25000", character.Gold)
	}
}

func f64ToI64(v int64) *int64 { return &v }

// TestBuildPayloadShapeStableAcrossSessions: the Node comment's exact
// concern - a first-ever and a restored bootstrap must carry the same
// visualLoadout field set, or the client takes different branches on the
// same code path.
func TestBuildPayloadShapeStableAcrossSessions(t *testing.T) {
	character := chinaSpearman()
	deps := testDeps(character)
	first := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	restored := Build(deps, BootstrapRequest{CharacterName: "asd2"})

	shape := func(result *BootstrapResult) []string {
		data, err := json.Marshal(result.LocalPlayerEntry.VisualLoadout)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		return keys
	}
	firstKeys := shape(first)
	restoredKeys := shape(restored)
	if len(firstKeys) != len(restoredKeys) {
		t.Fatalf("payload shape differs between sessions: first %v, restored %v", firstKeys, restoredKeys)
	}
	firstSet := map[string]bool{}
	for _, key := range firstKeys {
		firstSet[key] = true
	}
	for _, key := range restoredKeys {
		if !firstSet[key] {
			t.Fatalf("restored session adds %q the first-ever payload lacks", key)
		}
	}
}

func TestBuildPacketSequence(t *testing.T) {
	character := chinaSpearman()
	character.ID = 3
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})

	wantOpcodes := []uint16{
		OpcodeResetClient,
		OpcodeMyCharacterData,
		OpcodeMyCharacterChunk,
		OpcodeMyCharacterFlush,
		OpcodeServerClockGidLatch,
		OpcodeObjectListStart,
		OpcodeObjectListFinalize,
	}
	if len(result.Packets) != len(wantOpcodes) {
		t.Fatalf("packet count = %d, want %d", len(result.Packets), len(wantOpcodes))
	}
	for index, want := range wantOpcodes {
		if result.Packets[index].NativeOpcode != want {
			t.Errorf("packet[%d] opcode = %#x, want %#x", index, result.Packets[index].NativeOpcode, want)
		}
	}
	// Reset client carries the spawn region little-endian (china 0x62a8).
	if !reflect.DeepEqual(result.Packets[0].Payload, []int{0xa8, 0x62}) {
		t.Errorf("reset payload = %v, want [0xa8 0x62]", result.Packets[0].Payload)
	}
	// Clock latch preserves GID and carries a valid native calendar.
	gid := 100000 + 3
	latch := result.Packets[4].Payload
	if len(latch) != 8 || !reflect.DeepEqual(latch[:4], []int{gid & 255, (gid >> 8) & 255, (gid >> 16) & 255, (gid >> 24) & 255}) || latch[6] >= 24 || latch[7] >= 60 {
		t.Fatalf("invalid clock latch: %v", latch)
	}
	// Object list start: [1, rowCount, 0] with no rows from the nil seams.
	if !reflect.DeepEqual(result.Packets[5].Payload, []int{0x01, 0x00, 0x00}) {
		t.Errorf("object list start payload = %v, want [1 0 0]", result.Packets[5].Payload)
	}
	if len(result.Packets[2].Payload) == 0 {
		t.Error("char-data chunk payload is empty")
	}
}

func TestBuildSuccessEnvelopeShape(t *testing.T) {
	character := chinaSpearman()
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(fields["bootstrapMode"]) != `"`+BootstrapMode+`"` {
		t.Errorf("bootstrapMode = %s", fields["bootstrapMode"])
	}
	if string(fields["protocolVersion"]) != "2" {
		t.Errorf("protocolVersion = %s, want 2", fields["protocolVersion"])
	}
	if string(fields["refObjSnapshot"]) != "[]" {
		t.Errorf("refObjSnapshot = %s, want []", fields["refObjSnapshot"])
	}
	if string(fields["chatMessages"]) != "[]" {
		t.Errorf("chatMessages = %s, want []", fields["chatMessages"])
	}
	if string(fields["systemMessages"]) != "[]" {
		t.Errorf("systemMessages = %s, want []", fields["systemMessages"])
	}
	for _, presentationKey := range []string{"areaLabel", "dungeonMinimap", "preloadContract", "preloadAssets"} {
		if _, ok := fields[presentationKey]; ok {
			t.Errorf("semantic bootstrap must not emit browser presentation field %q", presentationKey)
		}
	}
	if _, ok := fields["nativeErrorCode"]; ok {
		t.Error("success envelope must not carry nativeErrorCode")
	}
	if _, ok := fields["reason"]; ok {
		t.Error("success envelope must not carry reason")
	}
	// The character snapshot rides the response with its inventory rows -
	// the audit scripts read character.missionInventory straight off it.
	var snapshot struct {
		MissionInventory []InventoryRow  `json:"missionInventory"`
		Mission          *MissionRuntime `json:"mission"`
	}
	if err := json.Unmarshal(fields["character"], &snapshot); err != nil {
		t.Fatalf("character snapshot: %v", err)
	}
	if len(snapshot.MissionInventory) == 0 {
		t.Error("character snapshot lacks missionInventory rows")
	}
	if snapshot.Mission == nil || snapshot.Mission.EventGuideStateMask == nil {
		t.Error("character snapshot lacks the normalized mission runtime block")
	}
}

func TestBuildRefItemSnapshotCoversInventoryAndGold(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{
		{Slot: 6, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x32c, VarianceBits: "0", StackCount: 1},
		{Slot: 4, RefObjID: 5049, Codename: "ITEM_CH_M_HEAVY_01_LA_A", TypeFlags: 0x8c, VarianceBits: "0", StackCount: 1},
	}
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})

	byCodename := map[string]RefItemRow{}
	for _, row := range result.RefItemSnapshot {
		byCodename[row.Codename] = row
	}
	// The worn blade is NOT in the creation equip roster (that derives the
	// spear), but the wire carries it, so its row must ride the snapshot -
	// the weapon audit script reads the codename from here.
	if _, ok := byCodename["ITEM_CH_BLADE_01_A"]; !ok {
		t.Errorf("snapshot lacks the worn blade row; have %v", byCodename)
	}
	for _, gold := range []string{"ITEM_ETC_GOLD_01", "ITEM_ETC_GOLD_02", "ITEM_ETC_GOLD_03"} {
		if _, ok := byCodename[gold]; !ok {
			t.Errorf("snapshot lacks gold tier %s", gold)
		}
	}
	// The inventory-only reference carries the persisted row's flags.
	if got := byCodename["ITEM_CH_BLADE_01_A"].TypeFlags; got != 0x32c {
		t.Errorf("inventory blade typeFlags = %#x, want 0x32c", got)
	}
	pants, ok := byCodename["ITEM_CH_M_HEAVY_01_LA_A"]
	if !ok {
		t.Fatalf("snapshot lacks the worn pants row; have %v", byCodename)
	}
	if pants.TypeFlags != 0x8c {
		t.Errorf("pants typeFlags = %#x, want the inventory row's 0x8c", pants.TypeFlags)
	}
}

func TestBuildCaseInsensitiveNameAndDivisionDefault(t *testing.T) {
	character := chinaSpearman()
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "  ASD2  "})
	if result.NativeResult != 1 {
		t.Fatalf("case-insensitive lookup failed: %+v", result)
	}
	if result.DivisionID != DefaultDivisionID {
		t.Errorf("division = %q, want default %q", result.DivisionID, DefaultDivisionID)
	}
}

func TestBootstrapRequestUnmarshalToleratesNumericDivision(t *testing.T) {
	var request BootstrapRequest
	if err := json.Unmarshal([]byte(`{"characterName":"asd2","divisionId":0}`), &request); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if request.DivisionID != "0" || request.CharacterName != "asd2" {
		t.Fatalf("request = %+v", request)
	}
	if err := json.Unmarshal([]byte(`{"characterName":"asd2","divisionId":"global-official"}`), &request); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if request.DivisionID != "global-official" {
		t.Fatalf("string division = %q", request.DivisionID)
	}
}

func TestBuildItemBodyBytes(t *testing.T) {
	body := BuildItemBody(WireItem{
		RefObjID:     107,
		Plus:         0,
		VarianceBits: 0x8000000000000000,
		Durability:   56,
	})
	want := []byte{
		0x6b, 0x00, 0x00, 0x00, // refObjId 107
		0x00,                                           // plus
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, // varianceBits top bit
		0x38, 0x00, 0x00, 0x00, // durability 56
		0x00, // magicParamCount
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v, want %v", body, want)
	}
}

func TestBuildLocalPlayerEntryPayloadStructure(t *testing.T) {
	character := chinaSpearman()
	character.ID = 3
	character.Name = "asd2"
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	payload := result.Packets[2].Payload

	// Fixed prefix: u32 modelRef (1907 = 0x773).
	if payload[0] != 0x73 || payload[1] != 0x07 || payload[2] != 0 || payload[3] != 0 {
		t.Errorf("payload[0:4] = %v, want modelRef 1907 LE", payload[0:4])
	}
	// level/maxLevel default 1.
	if payload[5] != 1 || payload[6] != 1 {
		t.Errorf("level bytes = %v %v, want 1 1", payload[5], payload[6])
	}
	// Gold u64 at offset 19: a fresh character starts with zero gold.
	if payload[19] != 0 || payload[20] != 0 {
		t.Errorf("gold bytes = %#x %#x, want zero", payload[19], payload[20])
	}
	// Inventory block: capacity 45 then the seeded row count: the spear only,
	// since a no-armor creation grants no garments.
	if payload[54] != 45 {
		t.Errorf("inventory capacity byte = %d, want 45", payload[54])
	}
	if payload[55] != 1 {
		t.Errorf("inventory count byte = %d, want 1 seeded row", payload[55])
	}
	// The character name rides as u16-length + bytes.
	name := []byte("asd2")
	found := false
	for index := 0; index+2+len(name) <= len(payload); index++ {
		if payload[index] == len(name) && payload[index+1] == 0 {
			match := true
			for offset, char := range name {
				if payload[index+2+offset] != int(char) {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("payload does not carry the u16-length-prefixed character name")
	}
	// Tail: fortress-war "none" sentinel 0x10001 LE then two zero bytes.
	tail := payload[len(payload)-6:]
	if !reflect.DeepEqual(tail, []int{0x01, 0x00, 0x01, 0x00, 0x00, 0x00}) {
		t.Errorf("payload tail = %v, want fortress sentinel + 2 zero bytes", tail)
	}
}

func TestGameTimeAndVitalsPayloads(t *testing.T) {
	if got := BuildGameTimePayload(); len(got) != 4 || got[2] >= 24 || got[3] >= 60 {
		t.Fatalf("invalid clock: %v", got)
	}
	character := chinaSpearman()
	character.ID = 1
	vitals := BuildVitalsRefreshPayload(character)
	// u32 gid 100001, u16 0, u8 mask 3, u32 hp 200, u32 mp 200: the
	// fixture has no persisted currents, so they fall back to the DERIVED
	// maxima (level 1, creation-base 20/20 -> 200/200, the retail
	// creation vitals).
	gid := 100001
	want := []byte{
		byte(gid), byte(gid >> 8), byte(gid >> 16), byte(gid >> 24),
		0, 0,
		3,
		200, 0, 0, 0,
		200, 0, 0, 0,
	}
	if !reflect.DeepEqual(vitals, want) {
		t.Errorf("vitals payload = %v, want %v", vitals, want)
	}
}
