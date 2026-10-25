package enterworld

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The monster snapshot row carries the ref's BCRadius so the browser's
// predicted run-up can stop at the server's combat spacing.
func TestMonsterRefObjRowCarriesBodyRadius(t *testing.T) {
	row := monsterRefObjRow(monster.MonsterRef{
		RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", NameStrID: "SN_MOB_CH_MANGNYANG",
		Name: "Mangyang", Level: 1, MaxHP: 54, BodyRadius: 2,
	})
	if row.BodyRadius != 2 {
		t.Fatalf("monster row body radius = %v, want the ref's 2: %+v", row.BodyRadius, row)
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"bodyRadius":2`) {
		t.Errorf("monster row JSON %s lacks the body radius", encoded)
	}
}

func admitMonsterFixture(registry *simulation.MonsterState, entry *LocalPlayerEntry) {
	registry.StartDivision("global-official")
	lease, ok := registry.PopulationLease("global-official", instance.Pack(1, 1))
	if !ok {
		panic("fixture world missing")
	}
	entry.Population = lease
	registry.AdvancePopulation(registry.CurrentTimeMillis())
}

// monsterTestRegistry: one nest in 0x62a7 at local block (2,1), one two
// sectors further east in 0x62aa, and one dungeon floor row. The 0x62a7 nest
// borders the common 0x62a8 test login region but lies six interest blocks
// from its (960,458) login pose.
func monsterTestRegistry() *simulation.MonsterState {
	template := monster.Template{
		Refs: map[uint32]monster.MonsterRef{
			1933: {
				RefObjID: 1933, TidWord: 0x00C6,
				Codename: "MOB_CH_MANGNYANG", NameStrID: "SN_MOB_CH_MANGNYANG",
				Name: "Mangyang", ModelPath: `mob\china\mangnyang.bsr`,
				RideModelPath: `res\mob\china\bluetiger.bsr`, RiderTransformMode: 2,
				Level: 1, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100,
			},
		},
	}
	return simulation.NewMonsterState(monster.TemplateFromParts(template.Refs, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62a7, X: 812.68, Y: 75.08, Z: 392.90}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x62aa, X: 100, Y: 0, Z: 200}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: 0x8001, X: 50, Y: 0, Z: 60}}, // dungeon floor
	}))
}

// Kill-switch env semantics (HUMAN RULING seq388 / item B seq401):
// unset = monsters ON (retail posture), "0" = off, "1" = on.
func TestMonsterSpawnConfigKillSwitch(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{env: "", want: true},
		{env: "0", want: false},
		{env: "1", want: true},
	}
	for _, tc := range cases {
		if tc.env == "" {
			os.Unsetenv("MISSION_SPAWN_MONSTERS")
		} else {
			os.Setenv("MISSION_SPAWN_MONSTERS", tc.env)
		}
		if got := MonsterSpawnConfigFromEnv().Enabled; got != tc.want {
			t.Errorf("MISSION_SPAWN_MONSTERS=%q -> Enabled=%v, want %v", tc.env, got, tc.want)
		}
	}
	os.Unsetenv("MISSION_SPAWN_MONSTERS")
}

// The kill switch: a disabled config emits nothing at all - no
// instances, no snapshot rows (nothing on the wire to seed).
func TestMonsterSpawnGatesDisabled(t *testing.T) {
	registry := monsterTestRegistry()
	entry := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: 0x62a8, X: 960, Y: 20, Z: 458}}

	disabled := MonsterSpawnConfig{}
	if got := disabled.MonsterObjectListInstances(registry, "global-official", entry); len(got) != 0 {
		t.Fatalf("disabled config resolved %d instances", len(got))
	}
	if got := disabled.MonsterRefObjSnapshot(registry); len(got) != 0 {
		t.Fatalf("disabled config produced %d snapshot rows", len(got))
	}
}

// The login set is the native interest area at the login pose (the viewer's
// 320-unit block and its eight neighbours): bordering the login region is not
// enough, nests outside the neighbourhood stream later as scoped 0x30D7
// singles, and the refObjSnapshot still carries the FULL spawnable roster
// (WIP C1: the client seeds once per session; an unseeded refObjId on a later
// single is silently dropped).
func TestMonsterLoginSetIsInterestScoped(t *testing.T) {
	registry := monsterTestRegistry()
	enabled := MonsterSpawnConfig{Enabled: true}

	bordering := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: 0x62a8, X: 960, Y: 20, Z: 458}}
	admitMonsterFixture(registry, bordering)
	if got := enabled.MonsterObjectListInstances(registry, "global-official", bordering); len(got) != 0 {
		t.Fatalf("login six blocks from the 0x62a7 nest resolved %d instances, want 0", len(got))
	}

	entry := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: 0x62a7, X: 1000, Y: 75, Z: 400}}
	admitMonsterFixture(registry, entry)
	instances := enabled.MonsterObjectListInstances(registry, "global-official", entry)
	if len(instances) != 1 {
		t.Fatalf("login instances = %d, want 1 (the neighbouring-block 0x62a7 nest only)", len(instances))
	}
	if instances[0].Nest.RegionID != 0x62a7 {
		t.Fatalf("login instance region = %#04x, want 0x62a7", instances[0].Nest.RegionID)
	}

	// The tick reconciles first sight against exactly what this list created.
	RecordMonsterObjectList(registry, "global-official", &Character{ID: 42}, instances)
	recorded, ok := registry.TakeObjectList("global-official", simulation.PlayerObjectID(42))
	if !ok || len(recorded) != 1 || recorded[0] != instances[0].Gid {
		t.Fatalf("recorded object list = %v (ok=%v), want the one created gid %d", recorded, ok, instances[0].Gid)
	}

	snapshot := enabled.MonsterRefObjSnapshot(registry)
	if len(snapshot) != 1 || snapshot[0].RefObjID != 1933 || snapshot[0].TidWord != 0x00C6 ||
		snapshot[0].Codename != "MOB_CH_MANGNYANG" ||
		snapshot[0].NameStrID != "SN_MOB_CH_MANGNYANG" ||
		snapshot[0].Name != "Mangyang" || snapshot[0].Level != 1 ||
		snapshot[0].MaxHP != 54 ||
		snapshot[0].Kind != "monster" {
		t.Fatalf("snapshot rows = %+v, want the full spawnable roster", snapshot)
	}
}

// Dungeon login: ring math is meaningless over sector-bit region ids, so
// the scope is exactly the current region (POLICY, coordinator seq401 A).
func TestMonsterDungeonLoginScopesSingleRegion(t *testing.T) {
	registry := monsterTestRegistry()
	entry := &LocalPlayerEntry{StartProfile: StartProfile{RegionID: -32767, X: 100, Y: 0, Z: 100}} // 0x8001
	admitMonsterFixture(registry, entry)

	enabled := MonsterSpawnConfig{Enabled: true}
	instances := enabled.MonsterObjectListInstances(registry, "global-official", entry)
	if len(instances) != 1 {
		t.Fatalf("dungeon login instances = %d, want 1 (the 0x8001 floor row only)", len(instances))
	}
	if instances[0].Nest.RegionID != 0x8001 {
		t.Fatalf("dungeon instance region = %#04x, want 0x8001", instances[0].Nest.RegionID)
	}
}
