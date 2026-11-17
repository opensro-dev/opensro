/*
===========================================================================

wiring_caravanai_test.go - shipped caravan actors and feature boundaries

===========================================================================
*/
package main

import (
	"path/filepath"
	"testing"

	"context"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestCaravanWorldBoundary
================
*/
func TestCaravanWorldBoundary(t *testing.T) {
	game := &gameplayPlane{items: &action.Runtime{}}
	for _, world := range []uint32{domain.DefaultWorldInstance, domain.DefaultWorldInstance + 1} {
		if len(game.worldNpcsForInstance(world)) != 0 {
			t.Fatal("disabled caravans published NPC stand-ins")
		}
	}
}

/*
================
TestCaravanShippedActors
================
*/
func TestCaravanShippedActors(t *testing.T) {
	paths := gamedatatest.Paths(t)
	authority, err := store.Open(t.TempDir(), store.Options{DefaultSkills: enterworld.DefaultSkillSeeder(enterworld.NewTextdataSkills(paths.TextdataDir))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	roster, err := enterworld.LoadRoster(filepath.Join(paths.CharacterAuthorityDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	deps := &enterworld.Deps{Roster: roster, Characters: authority.Characters(), Items: enterworld.NewTextdataItems(paths.TextdataDir), Levels: enterworld.NewTextdataLevels(paths.TextdataDir), Skills: enterworld.NewTextdataSkills(paths.TextdataDir), MagicOptions: enterworld.NewTextdataMagicOptions(paths.TextdataDir), EquipItemsEnabled: true}
	deps.MutateCharacter = authority.MutateCharacter
	deps.UpdateCharacter = authority.UpdateCharacter
	deps.MutateCharacters = authority.MutateCharacters
	deps.UpdateCharacters = authority.UpdateCharacters
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	game := &gameplayPlane{divisionID: "caravan-test", deps: deps, items: action.NewRuntime(deps, nil)}
	game.water = movement.NewAuthorityValidator(paths.WorldAuthorityDir)
	game.movement = movement.NewRuntime(deps, game.items.Worlds)
	game.movement.Nav = game.water
	game.movement.PathGuard = movement.NewPathGuardFromEnv(game.water)
	game.movement.ClientClip = movement.NewClientClipFromEnv(game.water)
	game.items.NpcRoster = simulation.LoadNpcWorldRoster(paths.TextdataDir)
	if err := game.items.ConfigurePortals(paths.TextdataDir); err != nil {
		t.Fatal(err)
	}
	cfg := caravanai.Config{Enabled: true}
	if err := game.configureCaravanHighway(&cfg); err != nil {
		t.Fatal(err)
	}
	game.caravanAgents = caravanai.NewRuntime(cfg, nil, caravanai.WorldHooks{Move: game.caravanMove, Ferry: game.caravanFerry})
	t.Cleanup(func() {
		if err := game.caravanAgents.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := game.initCaravanActors(); err != nil {
		t.Fatal(err)
	}
	if len(game.caravanActors) != 9 {
		t.Fatalf("got %d actors", len(game.caravanActors))
	}
	for _, c := range game.caravanActors {
		if !c.RuntimeOnly || c.AccountID != "" || enterworld.DressedJob(c) == 0 || deps.CharacterByID(game.divisionID, c.ID) != c {
			t.Fatalf("invalid combat actor %s", c.Name)
		}
		hp, maxHP, _, _ := game.items.GameplayVitals(game.divisionID, c)
		if hp <= 0 || hp != maxHP {
			t.Fatalf("%s has HP %d/%d", c.Name, hp, maxHP)
		}
	}
	gold := int64(100)
	hunter := &enterworld.Character{Name: "escorthunter", Gold: &gold}
	for _, c := range game.caravanActors {
		if enterworld.DressedJob(c) == domain.JobHunter {
			hunter.MissionInventory = append([]domain.InventoryRow(nil), c.MissionInventory...)
			break
		}
	}
	if err := authority.CreateCharacter(game.divisionID, "escort-account", hunter); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		game.caravanEscortReward(game.divisionID, hunter.Name, 1000, "trader")
	}
	if *hunter.Gold != 3100 || hunter.CaravanRewardCount != 3 {
		t.Fatalf("daily reward cap failed: gold=%d count=%d", *hunter.Gold, hunter.CaravanRewardCount)
	}
	initial := game.caravanAgents.EnsureDivisionRoster(game.divisionID)[0]

	var moved *caravanai.AgentUnit
	for now := int64(1000); now <= 11000; now += 100 {
		_, moved, _ = game.caravanAgents.StepAgent(context.Background(), game.divisionID, initial.Name, nil, now)
		game.items.SyncServerActor(game.divisionID, game.caravanActors[0], simulation.Spawn{RegionID: moved.RegionID, X: float64(moved.X), Y: float64(moved.Y), Z: float64(moved.Z)}, false, moved.CargoStacks, false)
	}
	if moved.WaypointIndex == 0 || (moved.X == initial.X && moved.Z == initial.Z) {
		t.Fatalf("caravan stuck at its terminal: %+v", moved)
	}
	for now := int64(11100); now <= 3600000 && moved.OriginTown == caravanai.TownJangan; now += 100 {
		_, moved, _ = game.caravanAgents.StepAgent(context.Background(), game.divisionID, initial.Name, nil, now)
		game.items.SyncServerActor(game.divisionID, game.caravanActors[0], simulation.Spawn{RegionID: moved.RegionID, X: float64(moved.X), Y: float64(moved.Y), Z: float64(moved.Z)}, false, moved.CargoStacks, false)
	}
	if moved.OriginTown == caravanai.TownJangan {
		road := game.caravanRoads[initial.Name]
		if road != nil && road.route != nil {
			t.Logf("road status=%v probes=%d points=%d index=%d", road.route.Status(), road.route.Probes(), road.route.Len(), road.index)
			if road.index < road.route.Len() {
				t.Logf("next=%+v", road.route.Point(road.index))
			}
		}
		t.Fatalf("highway delivery did not finish: waypoint=%d region=%d x=%.1f z=%.1f", moved.WaypointIndex, moved.RegionID, moved.X, moved.Z)
	}
	for variant, routes := range []map[[2]caravanai.TownID][]caravanai.Waypoint{cfg.Highway, cfg.Detours} {
		for pair, points := range routes {
			if pair == [2]caravanai.TownID{caravanai.TownJangan, caravanai.TownDonwhang} && variant == 0 {
				continue
			}
			at := points[0]
			index := 1
			game.caravanRoads = nil
			for frame := 0; frame < 36000 && index < len(points); frame++ {
				game.items.SyncServerActor(game.divisionID, game.caravanActors[0], simulation.Spawn{RegionID: at.RegionID, X: float64(at.X), Y: float64(at.Y), Z: float64(at.Z)}, false, 10, false)
				target := points[index]
				arrived := false
				if target.FerryTargetID != 0 {
					at, arrived = game.caravanFerry(game.divisionID, initial.Name, target.FerrySourceRef, target.FerryTargetID)
				} else {
					at, arrived = game.caravanMove(initial.Name, at, target, 5)
				}
				if arrived {
					index++
				}
			}
			if index < len(points) {
				t.Fatalf("highway %s->%s variant %d stuck at %d region=%d x=%.1f y=%.1f z=%.1f", pair[0], pair[1], variant, index, at.RegionID, at.X, at.Y, at.Z)
			}
		}
	}

	game.items.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	thief, trader := game.caravanActors[8], game.caravanActors[1]
	at := simulation.Spawn{RegionID: 0x62aa, X: 500, Y: 0, Z: 500}
	game.items.SyncServerActor(game.divisionID, trader, at, false, 12, true)
	at.X += 2
	game.items.SyncServerActor(game.divisionID, thief, at, false, 0, false)
	if _, _, err := combat.PlayerStats(thief, combat.Catalogs{Items: deps.Items, Skills: deps.Skills, MagicOptions: deps.MagicOptions}); err != nil {
		t.Fatal(err)
	}

	before, _, _, _ := game.items.GameplayVitals(game.divisionID, trader)
	out := game.items.HandleTargetInteract(game.divisionID, thief, (wire.SkillAction{ActionId: game.caravanAttackSkill(thief), HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(trader)}).Encode())
	after, _, _, _ := game.items.GameplayVitals(game.divisionID, trader)
	if after >= before {
		t.Fatalf("real actor skill dealt no damage: %s frames=%+v", out.DiagnosticRefusal, out.Frames)
	}

	rows := len(trader.ActiveCOS.Container.Rows)
	frames := game.items.DropServerActorCargo(game.divisionID, trader.Name, 3)
	if len(frames) == 0 || len(trader.ActiveCOS.Container.Rows) != rows-3 {
		t.Fatal("parley did not scatter real cargo")
	}
	for i, row := range trader.ActiveCOS.Container.Rows {
		if row.Slot != int64(i) {
			t.Fatal("remaining cargo slots collided")
		}
	}

}
