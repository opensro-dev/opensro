/*
===========================================================================

rebirth_test.go - tests for rebirth.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"encoding/json"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
rebirthTestCharacter
================
*/
func rebirthTestCharacter(level, hp int64) *enterworld.Character {
	character := testCharacter()
	character.Level = &level
	character.CurrentHP = &hp
	mp := int64(75)
	character.CurrentMP = &mp
	state := simulation.DefaultWorldState(simulation.ChinaStartProfile())
	writeBackWorld(character, state)
	return character
}

/*
================
TestRebirthPointAppointmentPersistsAdmittedPoint
================
*/
func TestRebirthPointAppointmentPersistsAdmittedPoint(t *testing.T) {
	character := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(character, testItems())
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true}
	guideIndex := len(rt.NpcRoster) - 1
	rt.NpcRoster[guideIndex].TalkFlags = simulation.NpcTalkFlagRecallPoint
	rt.NpcRoster[guideIndex].RebirthPoint = simulation.EuropeStartProfile()
	guideGID := rt.NpcRoster[guideIndex].ObjectID
	rt.Selected.Set(testDivision, character.Name, guideGID)

	result := rt.HandleRebirthPointAppointment(testDivision, character,
		wire.NewWriter(4).U32(guideGID).Payload())
	if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpRebirthPointAppointResult ||
		len(result.Frames[0].Payload) != 1 || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("appointment frames = %+v", result.Frames)
	}
	want := simulation.EuropeStartProfile()
	if character.World == nil || character.World.RebirthPoint == nil ||
		missionSpawnFromWorld(character.World.RebirthPoint, simulation.Spawn{}) != want {
		t.Fatalf("persisted rebirth point = %+v, want exact town spawn %+v", character.World, want)
	}
	snapshot := character.Snapshot()
	if snapshot.World.RebirthPoint == character.World.RebirthPoint {
		t.Fatal("character snapshot aliases the mutable rebirth-point record")
	}
}

/*
================
TestDesignatedRebirthRestoresWorldVitalsAndLife
================
*/
func TestDesignatedRebirthRestoresWorldVitalsAndLife(t *testing.T) {
	character := rebirthTestCharacter(20, 0)
	appointed := simulation.EuropeStartProfile()
	character.World.RebirthPoint = worldSpawnFromMission(appointed)
	rt, _ := newTestRuntime(character, testItems())

	result := rt.HandleLocalRebirth(testDivision, character, []byte{1})
	// Peers see the position, LIFE and the 4DF2E0 untouchable body mode.
	if len(result.Frames) < 8 || len(result.Broadcast) != 3 {
		t.Fatalf("rebirth burst = %d private/%d broadcast, want reset corpus + LIFE / 3", len(result.Frames), len(result.Broadcast))
	}
	if result.Frames[0].Opcode != enterworld.OpcodeResetClient ||
		len(result.Frames[0].Payload) != 2 ||
		uint16(result.Frames[0].Payload[0])|uint16(result.Frames[0].Payload[1])<<8 != appointed.RegionID {
		t.Fatalf("rebirth reset head = %+v, want 0x3369 region %#x", result.Frames[0], appointed.RegionID)
	}
	wantCorpus := []uint16{
		enterworld.OpcodeResetClient,
		7, // Fresh browser projection precedes the native character stream.
		enterworld.OpcodeMyCharacterData,
		enterworld.OpcodeMyCharacterChunk,
		enterworld.OpcodeMyCharacterFlush,
		enterworld.OpcodeServerClockGidLatch,
		enterworld.OpcodeObjectListStart,
		enterworld.OpcodeObjectListFinalize,
	}
	for index, opcode := range wantCorpus {
		if result.Frames[index].Opcode != opcode {
			t.Fatalf("rebirth corpus[%d] = %#x, want %#x", index, result.Frames[index].Opcode, opcode)
		}
	}
	var entry struct {
		Bootstrap struct {
			Character struct {
				HP int64 `json:"hp"`
				MP int64 `json:"mp"`
			} `json:"character"`
			LocalPlayerEntry struct {
				StartProfile struct {
					RegionID uint16  `json:"regionId"`
					X        float64 `json:"x"`
					Z        float64 `json:"z"`
				} `json:"startProfile"`
			} `json:"localPlayerEntry"`
		} `json:"bootstrap"`
	}
	if err := json.Unmarshal(result.Frames[1].Payload[9:], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Bootstrap.Character.HP != *character.CurrentHP || entry.Bootstrap.Character.MP != *character.CurrentMP || entry.Bootstrap.Character.HP <= 0 {
		t.Fatalf("re-entry retained corpse vitals: %+v", entry.Bootstrap.Character)
	}
	pose := entry.Bootstrap.LocalPlayerEntry.StartProfile
	if pose.RegionID != appointed.RegionID || pose.X != appointed.X || pose.Z != appointed.Z {
		t.Fatalf("re-entry retained login placement: %+v, want %+v", pose, appointed)
	}
	lifeFrame := result.Frames[len(result.Frames)-1]
	if lifeFrame.Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("rebirth tail opcode = %#x, want LIFE state", lifeFrame.Opcode)
	}
	life, err := wire.DecodeObjectStateRefresh(lifeFrame.Payload)
	if err != nil || life.StateType != wire.StateChannelLife || life.Value != wire.LifeStateAlive {
		t.Fatalf("life frame = %+v / %v", life, err)
	}
	if character.CurrentHP == nil || *character.CurrentHP != enterworld.DerivedMaxHP(character) ||
		character.CurrentMP == nil || *character.CurrentMP != enterworld.DerivedMaxMP(character) {
		t.Fatalf("restored vitals hp=%v mp=%v", character.CurrentHP, character.CurrentMP)
	}
	if character.World.RebirthPoint == nil || character.World.Spawn == nil ||
		missionSpawnFromWorld(character.World.Spawn, simulation.Spawn{}) != appointed {
		t.Fatalf("world write-back lost spawn or appointment: %+v", character.World)
	}
	correction, err := wire.DecodeObjectSourceCorrection(result.Broadcast[0].Payload)
	if err != nil || correction.RegionID != appointed.RegionID ||
		correction.X != float32(appointed.X) || correction.Y != float32(appointed.Y) ||
		correction.Z != float32(appointed.Z) || correction.Heading != appointed.Angle {
		t.Fatalf("peer rebirth correction = %+v / %v, want exact appointed spawn %+v", correction, err, appointed)
	}
}

/*
================
TestDevelopmentRestoreViewerRevivesThroughPresentRebirth
================
*/
func TestDevelopmentRestoreViewerRevivesThroughPresentRebirth(t *testing.T) {
	character := rebirthTestCharacter(3, 0)
	rt, _ := newTestRuntime(character, testItems())
	result := rt.DevelopmentRestoreViewer(testDivision, character)
	if result.DiagnosticRefusal != "" || !enterworld.CharacterAlive(character) || len(result.Frames) == 0 {
		t.Fatalf("dead viewer restore %q alive %v frames %d", result.DiagnosticRefusal, enterworld.CharacterAlive(character), len(result.Frames))
	}
	wounded := int64(1)
	character.CurrentHP = &wounded
	healed := rt.DevelopmentRestoreViewer(testDivision, character)
	if character.CurrentHP == nil || *character.CurrentHP <= wounded || len(healed.Frames) != 1 || healed.Frames[0].Opcode != simulation.OpVitalsUpdate {
		t.Fatalf("living heal hp %v frames %+v", character.CurrentHP, healed.Frames)
	}
}

/*
================
TestPresentPositionRebirthIsLevelGated
================
*/
func TestPresentPositionRebirthIsLevelGated(t *testing.T) {
	high := rebirthTestCharacter(11, 0)
	highRuntime, _ := newTestRuntime(high, testItems())
	if result := highRuntime.HandleLocalRebirth(testDivision, high, []byte{2}); len(result.Frames) != 0 {
		t.Fatalf("level 11 present-position rebirth was accepted: %+v", result)
	}

	low := rebirthTestCharacter(10, 0)
	lowRuntime, _ := newTestRuntime(low, testItems())
	result := lowRuntime.HandleLocalRebirth(testDivision, low, []byte{2})
	// Correction, vitals, LIFE, then the 4DF2E0 untouchable body mode.
	wantPrivate := []uint16{
		wire.OpObjectSourceCorrection,
		enterworld.OpcodeVitalsUpdate,
		wire.OpObjectStateRefresh,
		wire.OpObjectStateRefresh,
	}
	if got := opcodesOf(result.Frames); len(got) != len(wantPrivate) {
		t.Fatalf("level 10 present-position rebirth frames = %+v, want in-place transaction %+v", got, wantPrivate)
	} else {
		for index, opcode := range wantPrivate {
			if got[index] != opcode {
				t.Fatalf("present-position frame[%d] = %#x, want %#x", index, got[index], opcode)
			}
		}
	}
	if len(result.Broadcast) != 3 || result.Broadcast[0].Opcode != wire.OpObjectSourceCorrection ||
		result.Broadcast[1].Opcode != wire.OpObjectStateRefresh || result.Broadcast[2].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("present-position peer frames = %+v, want correction + LIFE + body mode", opcodesOf(result.Broadcast))
	}
	for _, frame := range result.Frames {
		if frame.Opcode == enterworld.OpcodeResetClient {
			t.Fatal("present-position rebirth emitted 0x3369 and would remount the resident mission scene")
		}
	}
	vitals := result.Frames[1].Payload
	wantHP := uint32(1 + int64(float64(float32(enterworld.DerivedMaxHP(low)))*float64(float32(0.4))))
	recoveredMP := int64(float64(float32(enterworld.DerivedMaxMP(low))) * float64(float32(0.4)))
	wantMP := uint32(min(enterworld.DerivedMaxMP(low), 75+recoveredMP))
	if len(vitals) != 15 ||
		binary.LittleEndian.Uint32(vitals[0:4]) != enterworld.ObjectIDForCharacter(low) ||
		binary.LittleEndian.Uint16(vitals[4:6]) != 0 ||
		vitals[6] != 0x03 ||
		binary.LittleEndian.Uint32(vitals[7:11]) != wantHP ||
		binary.LittleEndian.Uint32(vitals[11:15]) != wantMP {
		t.Fatalf("present-position vitals payload = %x, want source-zero one HP plus 40 percent and retained MP", vitals)
	}
	life, err := wire.DecodeObjectStateRefresh(result.Frames[2].Payload)
	if err != nil || life.StateType != wire.StateChannelLife || life.Value != wire.LifeStateAlive {
		t.Fatalf("present-position LIFE frame = %+v / %v", life, err)
	}
	body, err := wire.DecodeObjectStateRefresh(result.Frames[3].Payload)
	if err != nil || body.StateType != wire.StateChannelBody || body.Value != untouchableBodyStatus {
		t.Fatalf("present-position body mode frame = %+v / %v, want untouchable", body, err)
	}
}

/*
================
TestRecallAppointmentGateCatalogAndPersistence
================
*/
func TestRecallAppointmentGateCatalogAndPersistence(t *testing.T) {
	licensed.RequireGameData(t)
	c := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(c, testItems())
	rt.NpcSpawn.Enabled = true
	dir := gamedatatest.TextdataDir(t)
	roster, err := simulation.AppendTeleportGates(dir, simulation.LoadNpcWorldRoster(dir))
	if err != nil {
		t.Fatal(err)
	}
	rt.NpcRoster = roster
	if err = rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, npc := range rt.NpcRoster {
		id, exists := rt.portals.sources[npc.RefObjID]
		if !exists {
			continue
		}
		destination := rt.portals.destinations[id]
		eligible := destination.recall && destination.spawn.RegionID != 0 && destination.spawn.RegionID&0x8000 == 0
		if (npc.TalkFlags&simulation.NpcTalkFlagRecallPoint != 0) != eligible {
			t.Fatalf("capability mismatch %s", npc.Codename)
		}
		if !eligible {
			continue
		}
		count++
		key := simulation.WorldKey(testDivision, c.Name)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.DefaultWorldState(npc.Spawn) }, func(w *simulation.WorldState) { w.Spawn = npc.Spawn; w.MoveSegment = nil })
		rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
		payload := wire.NewWriter(4).U32(npc.ObjectID).Payload()
		result := rt.HandleRebirthPointAppointment(testDivision, c, payload)
		if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("appointment rejected %s: %+v", npc.Codename, result)
		}
		raw, _ := json.Marshal(c)
		var restored enterworld.Character
		if err = json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.World.RebirthGateRefID != npc.RefObjID || rt.appointedRebirthPoint(&restored) != destination.spawn {
			t.Fatalf("persistence mismatch %s", npc.Codename)
		}
		original := c.Snapshot()
		for _, body := range [][]byte{nil, payload[:3], append(append([]byte{}, payload...), 0)} {
			if got := rt.HandleRebirthPointAppointment(testDivision, c, body); len(got.Frames) != 0 {
				t.Fatal("malformed accepted")
			}
		}
		rt.Selected.Clear(testDivision, c.Name)
		if got := rt.HandleRebirthPointAppointment(testDivision, c, payload); len(got.Frames) != 0 {
			t.Fatal("stale selection accepted")
		}
		rt.Selected.Set(testDivision, c.Name, npc.ObjectID)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.DefaultWorldState(npc.Spawn) }, func(w *simulation.WorldState) { w.Spawn.X += 1000 })
		if got := rt.HandleRebirthPointAppointment(testDivision, c, payload); len(got.Frames) != 0 {
			t.Fatal("distant appointment accepted")
		}
		if c.World.RebirthGateRefID != original.World.RebirthGateRefID {
			t.Fatal("refusal mutated appointment")
		}
	}
	if count < 5 {
		t.Fatalf("only %d eligible gates checked", count)
	}
}

/*
================
TestRecallReferenceUsesCurrentCatalogThenStoredFallback
================
*/
func TestRecallReferenceUsesCurrentCatalogThenStoredFallback(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	rt, _ := newTestRuntime(c, testItems())
	old := simulation.ChinaStartProfile()
	current := simulation.EuropeStartProfile()
	c.World.RebirthPoint = worldSpawnFromMission(old)
	c.World.RebirthGateRefID = 2094
	rt.portals = &portalCatalog{sources: map[uint32]uint32{2094: 1}, destinations: map[uint32]portalDestination{1: {recall: true, spawn: current}}}
	if rt.appointedRebirthPoint(c) != current {
		t.Fatal("ignored reference")
	}
	hp := int64(0)
	c.CurrentHP = &hp
	result := rt.HandleLocalRebirth(testDivision, c, []byte{1})
	if len(result.Frames) == 0 || rt.Worlds.Snapshot(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.WorldState{} }).Spawn != current {
		t.Fatal("death recall ignored reference")
	}
	delete(rt.portals.sources, 2094)
	if rt.appointedRebirthPoint(c) != old {
		t.Fatal("lost stored fallback")
	}
}

/*
================
TestRecallAppointmentSurvivesAuthorityReopen
================
*/
func TestRecallAppointmentSurvivesAuthorityReopen(t *testing.T) {
	licensed.RequireGameData(t)
	d := openDoorRuntime(t, t.TempDir(), rebirthTestCharacter(20, 100))
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(division string, fn func()) {
		d.authority.ReadCharacters(division, func([]*enterworld.Character) { fn() })
	}
	d.rt.NpcSpawn.Enabled = true
	d.rt.NpcRoster = []simulation.NpcDef{{ObjectID: 252094, RefObjID: 2094, Teleport: &simulation.TeleportGateBounds{Radius: 100, Height: 200}}}
	dir := gamedatatest.TextdataDir(t)
	if err := d.rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	d.rt.Selected.Set(testDivision, d.character.Name, 252094)
	out := d.rt.HandleRebirthPointAppointment(testDivision, d.character, wire.NewWriter(4).U32(252094).Payload())
	if len(out.Frames) != 1 {
		t.Fatal("appointment did not commit")
	}
	want := d.rt.appointedRebirthPoint(d.character)
	d = d.reboot(t)
	if d.character.World.RebirthGateRefID != 2094 {
		t.Fatal("reboot lost gate identity")
	}
	if err := d.rt.ConfigurePortals(dir); err != nil {
		t.Fatal(err)
	}
	if d.rt.appointedRebirthPoint(d.character) != want {
		t.Fatal("reboot changed appointed destination")
	}
}
