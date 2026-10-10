/*
===========================================================================

register_test.go - EnterWorld admission and native frame publication through the handler.

===========================================================================
*/

package enterworld

import (
	"encoding/json"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// TestHandleEnterWorldSuccess drives the exact frame SCOUT-A's harness sends
// (division "0", character asd2) through the enter-world core and checks the
// frozen 0x0007 contract plus the native frame push order.
/*
================
TestHandleEnterWorldSuccess
================
*/
func TestHandleEnterWorldSuccess(t *testing.T) {
	character := chinaSpearman()
	character.ID = 7
	deps := testDeps(character)
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(deps.Characters)

	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, "0", "asd2"))
	outcome := HandleEnterWorld(deps, payload)

	if !outcome.OK {
		t.Fatalf("enter world failed: %+v", outcome.Result)
	}
	if outcome.DivisionID != DefaultDivisionID || outcome.CharacterName != "asd2" {
		t.Fatalf("identity bind = %q/%q, want %q/asd2", outcome.DivisionID, outcome.CharacterName, DefaultDivisionID)
	}

	decoded, err := transport.DecodeEnterWorldResult(outcome.ResultPayload)
	if err != nil {
		t.Fatalf("result payload does not decode: %v", err)
	}
	if !decoded.OK || decoded.NativeErrorCode != 0 {
		t.Fatalf("result = %+v, want ok", decoded)
	}

	var blob struct {
		V         int                        `json:"v"`
		Bootstrap map[string]json.RawMessage `json:"bootstrap"`
	}
	if err := json.Unmarshal(decoded.Blob, &blob); err != nil {
		t.Fatalf("blob does not parse: %v", err)
	}
	if blob.V != EnterWorldBlobVersion {
		t.Errorf("blob version = %d, want %d", blob.V, EnterWorldBlobVersion)
	}
	if _, ok := blob.Bootstrap["packets"]; ok {
		t.Error("blob must not carry packets; they ride as native frames")
	}
	if string(blob.Bootstrap["nativeResult"]) != "1" {
		t.Errorf("blob nativeResult = %s", blob.Bootstrap["nativeResult"])
	}
	if string(blob.Bootstrap["protocolVersion"]) != "3" {
		t.Errorf("blob protocolVersion = %s, want 3", blob.Bootstrap["protocolVersion"])
	}
	if _, ok := blob.Bootstrap["localPlayerEntry"]; !ok {
		t.Error("blob lacks localPlayerEntry")
	}

	wantOpcodes := []uint16{
		OpcodeResetClient, OpcodeMyCharacterData, OpcodeMyCharacterChunk,
		OpcodeMyCharacterFlush, OpcodeServerClockGidLatch,
		OpcodeObjectListStart, OpcodeObjectListFinalize,
	}
	if len(outcome.Frames) != len(wantOpcodes) {
		t.Fatalf("frame count = %d, want %d", len(outcome.Frames), len(wantOpcodes))
	}
	for index, want := range wantOpcodes {
		if outcome.Frames[index].NativeOpcode != want {
			t.Errorf("frame[%d] opcode = %#x, want %#x", index, outcome.Frames[index].NativeOpcode, want)
		}
	}
}

/*
================
TestHandleEnterWorldFailureAndMalformed
================
*/
func TestHandleEnterWorldFailureAndMalformed(t *testing.T) {
	deps := testDeps(chinaSpearman())

	payload := transport.EncodeEnterWorld(entryauth.NewPostAuthHandlerEntryFixture("", "nobody"))
	outcome := HandleEnterWorld(deps, payload)
	if outcome.OK {
		t.Fatal("unknown character must fail")
	}
	decoded, err := transport.DecodeEnterWorldResult(outcome.ResultPayload)
	if err != nil {
		t.Fatalf("failure payload does not decode: %v", err)
	}
	if decoded.OK || decoded.NativeErrorCode != 0x10 {
		t.Fatalf("failure result = %+v, want !ok code 0x10", decoded)
	}
	var blob struct {
		V         int                        `json:"v"`
		Bootstrap map[string]json.RawMessage `json:"bootstrap"`
	}
	if err := json.Unmarshal(decoded.Blob, &blob); err != nil {
		t.Fatalf("failure blob does not parse: %v", err)
	}
	if string(blob.Bootstrap["reason"]) != `"characterNotFound"` {
		t.Errorf("failure blob reason = %s", blob.Bootstrap["reason"])
	}
	if len(outcome.Frames) != 0 {
		t.Errorf("failure pushed %d native frames, want none", len(outcome.Frames))
	}

	malformed := HandleEnterWorld(deps, []byte{0xff})
	if malformed.OK {
		t.Fatal("malformed payload must fail")
	}
	decodedMalformed, err := transport.DecodeEnterWorldResult(malformed.ResultPayload)
	if err != nil {
		t.Fatalf("malformed-result payload does not decode: %v", err)
	}
	if decodedMalformed.NativeErrorCode != 0x02 {
		t.Errorf("malformed code = %#x, want 0x02", decodedMalformed.NativeErrorCode)
	}
}

/*
================
TestHandleGameReady
================
*/
func TestHandleGameReady(t *testing.T) {
	stats := wire.BaseStats{PhysicalAttackMin: 7, PhysicalAttackMax: 9}
	if got := HandleGameReady(nil, stats); got != nil {
		t.Fatalf("no bound character must push nothing, got %v", got)
	}
	character := chinaSpearman()
	character.ID = 1
	frames := HandleGameReady(character, stats)
	if len(frames) != 3 {
		t.Fatalf("frame count = %d, want 3", len(frames))
	}
	if frames[0].NativeOpcode != OpcodeGameTime || frames[1].NativeOpcode != wire.OpBaseStats || frames[2].NativeOpcode != OpcodeVitalsUpdate {
		t.Fatalf("opcodes = %#x %#x %#x, want 0x31ad 0x343c 0x33a6", frames[0].NativeOpcode, frames[1].NativeOpcode, frames[2].NativeOpcode)
	}
	if p := frames[0].Payload; len(p) != 4 || p[2] >= 24 || p[3] >= 60 {
		t.Errorf("game time payload = %v", frames[0].Payload)
	}
	if !reflect.DeepEqual(frames[1].Payload, NewPacket(wire.OpBaseStats, BuildLoginStatBlock(character, stats)).Payload) {
		t.Errorf("base stats payload = %v, want canonical login stat block", frames[1].Payload)
	}

	deadHP := int64(0)
	character.CurrentHP = &deadHP
	deadFrames := HandleGameReady(character, stats)
	if len(deadFrames) != 4 {
		t.Fatalf("dead frame count = %d, want clock + base stats + vitals + LIFE-dead", len(deadFrames))
	}
	death := deadFrames[3]
	wantDeath := NewPacket(wire.OpObjectStateRefresh, wire.ObjectStateRefresh{
		Gid:       ObjectIDForCharacter(character),
		StateType: wire.StateChannelLife,
		Value:     wire.LifeStateDead,
	}.Encode())
	if death.NativeOpcode != wantDeath.NativeOpcode || !reflect.DeepEqual(death.Payload, wantDeath.Payload) {
		t.Fatalf("death frame = %+v, want %+v", death, wantDeath)
	}
}

/*
================
TestDevResolveDivisionIDFromCatalog
================
*/
func TestDevResolveDivisionIDFromCatalog(t *testing.T) {
	source := StaticCharacterSource{DefaultDivisionID: {chinaSpearman()}}
	allowed := []string{DefaultDivisionID, domain.TestDivisionID}
	resolve := DevResolveDivisionID(source, allowed, DefaultDivisionID)
	if got := resolve("0"); got != DefaultDivisionID {
		t.Errorf("resolve(0) = %q", got)
	}
	if got := resolve(""); got != DefaultDivisionID {
		t.Errorf("resolve(empty) = %q", got)
	}
	if got := resolve(DefaultDivisionID); got != DefaultDivisionID {
		t.Errorf("resolve(default) = %q", got)
	}
	multi := StaticCharacterSource{DefaultDivisionID: {chinaSpearman()}, "eu-01": {chinaSpearman()}}
	resolveMulti := DevResolveDivisionID(
		multi,
		[]string{domain.TestDivisionID},
		DefaultDivisionID,
	)
	if got := resolveMulti("eu-01"); got != "eu-01" {
		t.Errorf("known division must win, got %q", got)
	}
	if got := resolveMulti(domain.TestDivisionID); got != domain.TestDivisionID {
		t.Errorf("advertised empty division = %q, want %q", got, domain.TestDivisionID)
	}
}

// TestEnterWorldOverInjectedSource drives the full enter-world path over
// an injected character source - the shape the authority store hands the
// wiring (store.Characters()). The retired characters-dev.json loader this
// test used to exercise is gone per ADR-1 D9 (the store is the only
// character source; boot never reads a retired file).
/*
================
TestEnterWorldOverInjectedSource
================
*/
func TestEnterWorldOverInjectedSource(t *testing.T) {
	record := `{"id": 3, "name": "asd2", "raceIndex": 1, "gender": 0,
		 "modelCodename": "CHAR_CH_MAN_ADVENTURER",
		 "weaponSelected": true, "weaponIndex": 3,
		 "missionInventory": [
			{"slot": 6, "refObjId": 107, "codename": "ITEM_CH_BLADE_01_A",
			 "typeFlags": 812, "varianceBits": "0", "durability": 56, "stackCount": 1}
		 ]}`
	character := &Character{}
	if err := json.Unmarshal([]byte(record), character); err != nil {
		t.Fatalf("parse fixture record: %v", err)
	}
	source := StaticCharacterSource{"global-official": {character}}

	deps := &Deps{
		Roster:            testRoster(),
		Characters:        source,
		Items:             testItems(),
		EquipItemsEnabled: true,
	}
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(source)
	outcome := HandleEnterWorld(deps, transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, "0", "ASD2")))
	if !outcome.OK {
		t.Fatalf("enter world over injected source failed: %+v", outcome.Result)
	}
	loadout := outcome.Result.LocalPlayerEntry.VisualLoadout
	if worn := findRowBySlot(outcome.Result.Character.MissionInventory, 6); worn == nil ||
		!reflect.DeepEqual(loadout.Items[len(loadout.Items)-1], VisualItem{RefObjID: worn.RefObjID, Plus: worn.Plus}) {
		t.Fatalf("worn blade not rendered from injected source: %+v", loadout.Items)
	}
}
