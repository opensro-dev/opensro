/*
===========================================================================

wiring_caravanai_test.go - shipped caravan actors and feature boundaries

===========================================================================
*/
package main

import (
	"context"
	"encoding/binary"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	livepresence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

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
	game := &gameplayPlane{divisionID: "caravan-test", deps: deps, items: action.NewRuntime(deps, nil), hub: &transport.Hub{}}
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
	game.caravanAgents = caravanai.NewRuntime(cfg, nil, nil, caravanai.WorldHooks{Move: game.caravanMove, Ferry: game.caravanFerry, PickUp: game.caravanPickUp})
	t.Cleanup(func() {
		if err := game.caravanAgents.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := game.initCaravanActors(); err != nil {
		t.Fatal(err)
	}
	if len(game.caravanActors) != caravanai.RosterHunters+caravanai.RosterTraders+caravanai.RosterThieves {
		t.Fatalf("got %d actors", len(game.caravanActors))
	}
	for _, c := range game.caravanActors {
		if *c.Level != 90 || *c.MaxLevel != 90 || len(c.MissionInventory) < 12 || len(game.caravanSkills[c.Name]) != 3 {
			t.Fatalf("incomplete max-level build: %s level=%d gear=%d skills=%v", c.Name, *c.Level, len(c.MissionInventory), game.caravanSkills[c.Name])
		}
		if enterworld.ObjectIDForCharacter(c) >= domain.NPCGIDBase {
			t.Fatal("actor occupied a foreign object identity range")
		}
		// The ticker routes an actor's and its transport's frames to the
		// viewers that published them (Ticker.RuntimeObject).
		if !game.caravanObject(enterworld.ObjectIDForCharacter(c)) || (c.RuntimeTransportGID != 0 && !game.caravanObject(c.RuntimeTransportGID)) {
			t.Fatalf("%s is not a routed runtime object", c.Name)
		}
		if c.ActiveCOS != nil && (c.ActiveCOS.GID < domain.COSGIDBase || c.ActiveCOS.GID > domain.COSGIDLimit || c.ActiveCOS.Codename != "COS_T_DHORSE3") {
			t.Fatalf("transport identity or shipped reference invalid: %+v", c.ActiveCOS)
		}
		if !c.RuntimeOnly || c.AccountID != "" || enterworld.DressedJob(c) == 0 || deps.CharacterByID(game.divisionID, c.ID) != c {
			t.Fatalf("invalid combat actor %s", c.Name)
		}
		hp, maxHP, _, _ := game.items.GameplayVitals(game.divisionID, c)
		if hp <= 0 || hp != maxHP {
			t.Fatalf("%s has HP %d/%d", c.Name, hp, maxHP)
		}
	}
	for _, actor := range game.caravanActors {
		if actor.ActiveCOS == nil {
			continue
		}
		before := simulation.SeedWorldState(actor).Spawn
		game.items.SyncServerActor(game.divisionID, actor, before, false, 10, false)
		result := game.items.ServerActorRide(game.divisionID, actor.Name, true)
		if !actor.ActiveCOS.Mounted {
			t.Fatalf("shipped actor mount refused: %s %+v", actor.Name, result)
		}
		synced := game.items.SyncServerActor(game.divisionID, actor, before, false, 10, false)
		if !synced.ActiveCOS.Mounted {
			t.Fatal("mounted actor snapshot lost its ride state")
		}
		game.items.ServerActorRide(game.divisionID, actor.Name, false)
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
	// The daily cap is the runtime's (contracts_slice5_test.go); the door
	// pays a dressed, living Hunter and refuses a balance past MaxGold.
	game.caravanEscortReward(game.divisionID, hunter.Name, 1000, "trader")
	if *hunter.Gold != 1100 {
		t.Fatalf("escort reward: gold=%d, want 1100", *hunter.Gold)
	}
	full := int64(wire.MaxGold) - 10
	if !game.deps.Update(hunter, "test-fill", func() bool { hunter.Gold = &full; return true }) {
		t.Fatal("could not fill the purse")
	}
	game.caravanEscortReward(game.divisionID, hunter.Name, 1000, "trader")
	if game.caravanHirePayout(game.divisionID, hunter.Name, 1000) || *hunter.Gold != full {
		t.Fatalf("a credit past MaxGold moved gold: %d, want %d", *hunter.Gold, full)
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
	thief := game.deps.CharacterByName(game.divisionID, "[AI]Thief_Viper")
	trader := game.deps.CharacterByName(game.divisionID, "[AI]Trader_Mei")
	at := simulation.Spawn{RegionID: 0x62aa, X: 500, Y: 0, Z: 500}
	game.items.SyncServerActorCargo(game.divisionID, trader, at, false, 10, 3, true)
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

	now := game.items.Now()
	game.items.Now = func() time.Time { return now }
	tick := game.items.TickHook()
	before, _, _, _ = game.items.GameplayVitals(game.divisionID, trader)
	for frame := 0; frame < 200; frame++ {
		now = now.Add(100 * time.Millisecond)
		game.caravanCast(thief, enterworld.ObjectIDForCharacter(trader), now.UnixMilli())
		tick(now.UnixMilli())
	}
	after, _, _, _ = game.items.GameplayVitals(game.divisionID, trader)
	if after >= before {
		t.Fatal("advanced skill rotation did not damage its opponent")
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
	// The Trader picks its own dropped goods back up into its transport.
	everywhere := func(grounditem.Point) bool { return true }
	goods := game.items.ServerActorLootNear(game.divisionID, trader.Name, true, game.items.GroundItems(game.divisionID), everywhere)
	if len(goods) != 3 {
		t.Fatalf("the Trader sees %d of its 3 dropped goods", len(goods))
	}
	if picked := game.items.ServerActorPickup(game.divisionID, trader.Name, goods[0].Gid); len(picked.Broadcast) == 0 || len(trader.ActiveCOS.Container.Rows) != rows-2 {
		t.Fatalf("the Trader did not recover its goods: %q rows=%d", picked.DiagnosticRefusal, len(trader.ActiveCOS.Container.Rows))
	}
	// A Thief takes another caravan's goods and loose items.
	if len(game.items.ServerActorLootNear(game.divisionID, thief.Name, true, game.items.GroundItems(game.divisionID), everywhere)) != 2 {
		t.Fatal("the Thief does not see the Trader's goods")
	}
	if took := game.items.ServerActorPickup(game.divisionID, thief.Name, goods[1].Gid); len(took.Broadcast) == 0 {
		t.Fatal("the Thief did not take the goods")
	}
	if thief.ActiveCOS == nil || thief.ActiveCOS.Container == nil || len(thief.ActiveCOS.Container.Rows) != 1 {
		t.Fatalf("expected the Thief to summon a transport and load stolen goods into it, cos=%+v", thief.ActiveCOS)
	}
	potion, _ := deps.Items.ItemRefByCodename("ITEM_ETC_HP_POTION_05")
	dropped := game.items.Ground.Add(game.divisionID, grounditem.Item{RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 5,
		Position: grounditem.Point{RegionID: at.RegionID, X: float32(at.X), Z: float32(at.Z)}})
	if took := game.items.ServerActorPickup(game.divisionID, thief.Name, dropped.Gid); len(took.Broadcast) == 0 {
		t.Fatalf("the Thief did not pick up a potion stack: %q", took.DiagnosticRefusal)
	}
	if _, left := game.items.Ground.Get(game.divisionID, dropped.Gid); left {
		t.Fatal("the picked-up potions stayed on the ground")
	}

	template := monster.LoadTemplate(paths.TextdataDir)
	var bandit, nativeHunter monster.MonsterRef
	for _, ref := range template.Refs {
		if ref.TypeID4 == 2 && ref.Level == 90 && strings.HasPrefix(ref.Codename, "MOB_THIEF_") && (bandit.RefObjID == 0 || ref.RefObjID < bandit.RefObjID) {
			bandit = ref
		}
		if ref.TypeID4 == 3 && strings.HasPrefix(ref.Codename, "MOB_HUNTER_") &&
			(nativeHunter.RefObjID == 0 || ref.Level > nativeHunter.Level || ref.Level == nativeHunter.Level && ref.RefObjID < nativeHunter.RefObjID) {
			nativeHunter = ref
		}
	}
	if bandit.RefObjID == 0 {
		t.Fatal("shipped max-level thief reference unavailable")
	}
	game.items.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{bandit.RefObjID: bandit}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: bandit.RefObjID, RegionID: at.RegionID, X: at.X + 3, Y: at.Y, Z: at.Z}, MaxCount: 1, RetailEvidence: true}}))
	game.items.Monsters.StartDivision(game.divisionID)
	game.items.Monsters.AdvancePopulation(game.items.Monsters.CurrentTimeMillis())
	guard := game.deps.CharacterByName(game.divisionID, "[AI]Hunter_Wei")
	game.items.SyncServerActor(game.divisionID, guard, at, false, 0, false)
	if err := game.items.AdmitServerActor(game.divisionID, guard.Name); err != nil {
		t.Fatal(err)
	}
	units := game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	unit := units[slices.IndexFunc(units, func(unit *caravanai.AgentUnit) bool { return unit.Name == guard.Name })]
	unit.RegionID, unit.X, unit.Y, unit.Z = at.RegionID, float32(at.X), float32(at.Y), float32(at.Z)
	enemy, _ := game.caravanNPCThreat(unit, now.UnixMilli())
	if enemy == 0 {
		t.Fatal("hunter ignored native NPC thief")
	}
	original, _ := game.items.Monsters.Get(game.divisionID, enemy)
	for frame := 0; frame < 150; frame++ {
		now = now.Add(100 * time.Millisecond)
		game.caravanCast(guard, enemy, now.UnixMilli())
		tick(now.UnixMilli())
	}
	damaged, _ := game.items.Monsters.Get(game.divisionID, enemy)
	if damaged.CurrentHP >= original.CurrentHP {
		t.Fatal("hunter's advanced skills did not damage the native NPC thief")
	}

	advanced := false
	for _, id := range game.caravanSkills[guard.Name] {
		skill, _ := deps.Skills.SkillByID(id)
		if guard.OffensiveSkillCooldowns[skill.Group] > 0 || skill.CoolTimeGroup != 0 && guard.SharedSkillCooldowns[skill.CoolTimeGroup] > 0 {
			advanced = true
		}
	}
	if !advanced {
		t.Fatal("hunter used only base attacks")
	}

	if nativeHunter.RefObjID == 0 {
		t.Fatal("shipped max-level hunter reference unavailable")
	}
	for _, name := range []string{"[AI]Thief_Viper", "[AI]Thief_Kael"} {
		actor := game.deps.CharacterByName(game.divisionID, name)
		game.items.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(
			map[uint32]monster.MonsterRef{nativeHunter.RefObjID: nativeHunter},
			[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: nativeHunter.RefObjID, RegionID: at.RegionID, X: at.X + 3, Y: at.Y, Z: at.Z}, MaxCount: 1, RetailEvidence: true}},
		))
		game.items.Monsters.StartDivision(game.divisionID)
		game.items.Monsters.AdvancePopulation(now.UnixMilli())
		game.items.SyncServerActor(game.divisionID, actor, at, false, 0, false)
		if err := game.items.AdmitServerActor(game.divisionID, name); err != nil {
			t.Fatal(err)
		}
		unit := units[slices.IndexFunc(units, func(unit *caravanai.AgentUnit) bool { return unit.Name == name })]
		unit.RegionID, unit.X, unit.Y, unit.Z = at.RegionID, float32(at.X), float32(at.Y), float32(at.Z)
		target, _ := game.caravanNPCThreat(unit, now.UnixMilli())
		initial, ok := game.items.Monsters.Get(game.divisionID, target)
		if !ok || !initial.HunterMonster() {
			t.Fatalf("%s did not acquire a native hunter", name)
		}
		for range 80 {
			now = now.Add(100 * time.Millisecond)
			game.caravanCast(actor, target, now.UnixMilli())
			tick(now.UnixMilli())
		}
		if injured, ok := game.items.Monsters.Get(game.divisionID, target); ok && injured.CurrentHP >= initial.CurrentHP {
			t.Fatalf("%s did not damage a native hunter", name)
		}
	}
	oldGID := trader.RuntimeTransportGID
	trader.ActiveCOS = nil
	trader.CurrentHP = new(int64)
	revived := game.items.SyncServerActor(game.divisionID, trader, at, true, 10, false)
	if revived.ActiveCOS == nil || revived.ActiveCOS.GID != oldGID || len(revived.ActiveCOS.Container.Rows) != 10 || !enterworld.CharacterAlive(revived) {
		t.Fatal("defeated caravan did not recover its actor and loaded transport")
	}
	// Hired goods are priced like a player's own load: the star thresholds
	// are shares of a full load of these goods in this transport, and the
	// goods sell at another town's shop as a player's sale would.
	three, five := game.items.ServerActorStarGoods(game.divisionID, trader.Name, 3), game.items.ServerActorStarGoods(game.divisionID, trader.Name, 5)
	if five <= 0 || three != five*400/1000 || game.items.ServerActorStarGoods(game.divisionID, trader.Name, 1) != five/10 {
		t.Fatalf("star goods 3=%d 5=%d are not the full-load shares", three, five)
	}
	if sale, ok := game.items.ServerActorCargoSale(game.divisionID, trader.Name, "NPC_WC_SPECIAL"); game.items.Commerce != nil && (!ok || sale <= 0) {
		t.Fatalf("the cargo did not value at Donwhang's shop: %d %v", sale, ok)
	} else if game.items.Commerce == nil && ok {
		t.Fatal("a sale valued without a shop catalog")
	}

	// A patrol thief clears all nearby cargo before it starts the native scroll.
	robber := deps.CharacterByName(game.divisionID, "[AI]Thief_Crow")
	units = game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	robberUnit := units[slices.IndexFunc(units, func(u *caravanai.AgentUnit) bool { return u.Name == robber.Name })]
	robbery := simulation.Spawn{RegionID: robberUnit.RegionID, X: float64(robberUnit.X), Y: float64(robberUnit.Y), Z: float64(robberUnit.Z)}
	game.items.SyncServerActor(game.divisionID, robber, robbery, false, 0, false)
	// Even a thief hit in the fight resumes looking for goods once combat has stopped.
	game.caravanAgents.RecordPlayerAttack(game.divisionID, robber.Name, trader.Name, 1, now.UnixMilli())
	var haul []uint32
	ref, _ := deps.Items.ItemRefByCodename("ITEM_ETC_TRADE_CH_01")
	for range 3 {
		item := game.items.Ground.Add(game.divisionID, grounditem.Item{
			Position:   grounditem.Point{RegionID: robbery.RegionID, X: float32(robbery.X), Z: float32(robbery.Z)},
			TradeOwner: trader.Job.Alias, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 4,
		})
		haul = append(haul, item.Gid)
	}
	for i := range haul {
		game.showCaravanLoot(robber, robberUnit, nil, now.UnixMilli())
		if game.items.ServerActorReturnPending(game.divisionID, robber.Name) {
			t.Fatalf("the Den scroll started with %d stacks still on the ground", len(haul)-i)
		}
		_, robberUnit, _ = game.caravanAgents.StepAgent(context.Background(), game.divisionID, robber.Name, nil, now.UnixMilli())
		picked := 0
		for _, candidate := range haul {
			if _, left := game.items.Ground.Get(game.divisionID, candidate); !left {
				picked++
			}
		}
		if picked != i+1 {
			t.Fatalf("scan %d left %d of %d stacks on the ground", i+1, len(haul)-picked, len(haul))
		}
		if i == 0 {
			deps.Mutate(robber, "one-slot robber test transport", func() {
				robber.ActiveCOS.Container.Capacity = 1
			})
		}
		now = now.Add(time.Second)
	}
	// A different trader's goods cannot merge into the occupied slot.
	excess := game.items.Ground.Add(game.divisionID, grounditem.Item{
		Position:   grounditem.Point{RegionID: robbery.RegionID, X: float32(robbery.X), Z: float32(robbery.Z)},
		TradeOwner: "OtherTrader", RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 4,
	})
	game.showCaravanLoot(robber, robberUnit, nil, now.UnixMilli())
	if _, left := game.items.Ground.Get(game.divisionID, excess.Gid); !left {
		t.Fatal("unmergeable cargo disappeared from the ground")
	}
	if !game.items.ServerActorReturnPending(game.divisionID, robber.Name) {
		t.Fatal("the Den scroll did not start after collecting the whole haul")
	}
	if ride := game.items.ServerActorRide(game.divisionID, robber.Name, true); !robber.ActiveCOS.Mounted {
		t.Fatalf("loaded thief could not mount during the scroll: %+v", ride)
	}
	game.keepCaravanSpeed(robber, robberUnit, now.UnixMilli())
	units = game.caravanAgents.EnsureDivisionRoster(game.divisionID)
	robberUnit = units[slices.IndexFunc(units, func(u *caravanai.AgentUnit) bool { return u.Name == robber.Name })]
	if want := game.caravanRunSpeed(trader); robberUnit.RunSpeed != want {
		t.Fatalf("loaded thief speed %v, want trader speed %v", robberUnit.RunSpeed, want)
	}

	// Regression: HealServerActor heals active transport HP alongside actor vitals, and
	// caravanCast does not interrupt an open skill cast when ServerActorCasting is true.
	cosHPBefore, cosMaxHP, cosLost := game.items.ServerActorTransportVitals(game.divisionID, trader.Name)
	if cosLost || cosMaxHP == 0 || cosHPBefore == 0 {
		t.Fatalf("unexpected transport vitals state: hp=%d max=%d lost=%v", cosHPBefore, cosMaxHP, cosLost)
	}
	deps.Mutate(trader, "damage transport", func() {
		trader.ActiveCOS.CurrentHP = 100
	})
	game.items.HealServerActor(game.divisionID, trader.Name, 500)
	cosHPAfter, _, _ := game.items.ServerActorTransportVitals(game.divisionID, trader.Name)
	if cosHPAfter <= 100 {
		t.Fatalf("expected HealServerActor to recover transport HP from 100, got %d", cosHPAfter)
	}
	if castRes := game.caravanCast(thief, enterworld.ObjectIDForCharacter(trader), now.Add(time.Minute).UnixMilli()); len(castRes.Frames) == 0 && len(castRes.Broadcast) == 0 {
		t.Fatal("expected initial thief bow cast to emit frames")
	}
	if game.items.ServerActorCasting(game.divisionID, thief.Name) {
		suppressed := game.caravanCast(thief, enterworld.ObjectIDForCharacter(trader), now.Add(time.Minute).UnixMilli())
		if len(suppressed.Frames) != 0 || len(suppressed.Broadcast) != 0 {
			t.Fatalf("expected overlapping caravanCast to suppress mid-cast restart, got frames=%d broadcast=%d", len(suppressed.Frames), len(suppressed.Broadcast))
		}
	}

	// Finish through the same arrival lane the live GameWorld uses.
	now = now.Add(5*time.Minute + time.Second)
	game.items.TickHook()(now.UnixMilli())
	ctx, cancel := context.WithCancel(context.Background())
	arrivalsDone := make(chan struct{})
	go func() {
		defer close(arrivalsDone)
		game.items.RunReturnArrivals(ctx)
	}()
	defer func() { cancel(); <-arrivalsDone }()
	den := caravanai.TownWaypoint(caravanai.TownThiefDen).RegionID
	wait.Eventually(t, 3*time.Second, "thief arrives in the Bandit Den", func() bool {
		return game.items.LiveSpawnFor(game.divisionID, robber).RegionID == den
	})
	cancel()
	<-arrivalsDone

	grand := caravanai.NewRuntime(cfg, nil, nil, caravanai.WorldHooks{})
	defer grand.Close()
	if !grand.StartGrandCaravan(game.divisionID, now.UnixMilli()) {
		t.Fatal("could not muster the shipped grand caravan")
	}
	departure := now.UnixMilli() + caravanai.GrandCaravanMusterMs
	grand.StepAgent(context.Background(), game.divisionID, robber.Name, nil, departure)
	game.items.TradeTiers = true
	var grandActors []*enterworld.Character
	var transports []uint32
	for _, u := range grand.EnsureDivisionRoster(game.divisionID) {
		if u.RunKind != caravanai.RunGrand || u.Role != caravanai.RoleTrader {
			continue
		}
		c := deps.CharacterByName(game.divisionID, u.Name)
		spawn := simulation.Spawn{RegionID: u.RegionID, X: float64(u.X), Y: float64(u.Y), Z: float64(u.Z)}
		loaded := game.items.SyncServerActorCargo(game.divisionID, c, spawn, false, u.CargoStacks, u.CargoStars, u.OriginTown == caravanai.TownDonwhang)
		if loaded.ActiveCOS == nil {
			t.Fatalf("grand trader %s left without a transport", u.Name)
		}
		transports = append(transports, loaded.ActiveCOS.GID)
		grandActors = append(grandActors, c)
	}
	stars := game.items.CaravanStarFrames(game.divisionID, nil, grandActors, departure)
	if len(transports) != 2 || len(stars) != 1 || !slices.Equal(stars[0].Payload, wire.EncodeCaravanStars(transports)) {
		t.Fatalf("expected two five-star transports for the departing convoy, transports=%v stars=%+v", transports, stars)
	}

}

/*
================
TestCaravanPartyAndMovementPolish
================
*/
func TestCaravanPartyAndMovementPolish(t *testing.T) {
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
	deps := &enterworld.Deps{
		Roster:            roster,
		Characters:        authority.Characters(),
		Items:             enterworld.NewTextdataItems(paths.TextdataDir),
		Levels:            enterworld.NewTextdataLevels(paths.TextdataDir),
		Skills:            enterworld.NewTextdataSkills(paths.TextdataDir),
		MagicOptions:      enterworld.NewTextdataMagicOptions(paths.TextdataDir),
		EquipItemsEnabled: true,
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(deps.Characters)
	deps.MutateCharacter = authority.MutateCharacter
	deps.UpdateCharacter = authority.UpdateCharacter
	deps.MutateCharacters = authority.MutateCharacters
	deps.UpdateCharacters = authority.UpdateCharacters
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }

	jangan := caravanai.TownWaypoint(caravanai.TownJangan)
	ptrSpawn := func(r uint16, x, y, z float64) *domain.WorldSpawn {
		ri := int64(r)
		return &domain.WorldSpawn{RegionID: &ri, X: &x, Y: &y, Z: &z}
	}
	spawn := ptrSpawn(jangan.RegionID, float64(jangan.X), float64(jangan.Y), float64(jangan.Z))
	mkPlayer := func(account, name string) *enterworld.Character {
		lvl := int64(90)
		str := int64(300)
		intel := int64(300)
		hp := int64(20000)
		gold := int64(5_000_000)
		c := &enterworld.Character{
			Name:          name,
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     new(int64),
			Gender:        new(int64),
			Level:         &lvl,
			MaxLevel:      &lvl,
			Strength:      &str,
			Intellect:     &intel,
			CurrentHP:     &hp,
			CurrentMP:     &hp,
			Gold:          &gold,
			World:         &domain.CharacterWorld{Spawn: spawn, SpawnSet: true},
			MissionInventory: []domain.InventoryRow{
				{Slot: enterworld.JobSuitSlot, RefObjID: 8986, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobTrader), StackCount: 1},
			},
			Job: domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: name},
		}
		if err := authority.CreateCharacter(wiringPublicationDivision, account, c); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", name, err)
		}
		return deps.CharacterByName(wiringPublicationDivision, name)
	}
	hirer := mkPlayer("acct-hirer", "PlayerTrader")
	other := mkPlayer("acct-other", "OtherTrader")

	server := wiringPublicationServer(t)
	pres := livepresence.NewDirectory(server.Hub)
	items := action.NewRuntime(deps, nil)
	items.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	items.NpcRoster = simulation.LoadNpcWorldRoster(paths.TextdataDir)
	if err := items.ConfigurePortals(paths.TextdataDir); err != nil {
		t.Fatal(err)
	}
	parties := party.NewRuntime(deps, pres)
	parties.UseMemberVitals(items.GameplayVitals)
	parties.UseLivePose(items.LiveSpawnFor)
	parties.Register(server.Hub)

	game := &gameplayPlane{
		divisionID: wiringPublicationDivision,
		deps:       deps,
		hub:        server.Hub,
		presence:   pres,
		items:      items,
		parties:    parties,
	}
	game.water = movement.NewAuthorityValidator(paths.WorldAuthorityDir)
	game.movement = movement.NewRuntime(deps, items.Worlds)
	game.movement.Nav = game.water

	cfg := caravanai.Config{Enabled: true}
	if err := game.configureCaravanHighway(&cfg); err != nil {
		t.Fatal(err)
	}
	rt := caravanai.NewRuntime(cfg, nil, nil, caravanai.WorldHooks{
		Move:      game.caravanMove,
		Ferry:     game.caravanFerry,
		Player:    game.caravanPlayer,
		OnHireFee: game.caravanHireFee,
		OnContractFormed: func(division, playerName, agentName string) {
			game.parties.InvitePlayerFromRuntimeActor(division, agentName, playerName)
		},
	})
	t.Cleanup(func() { _ = rt.Close() })
	game.caravanAgents = rt
	rt.EnsureDivisionRoster(wiringPublicationDivision)
	if err := game.initCaravanActors(); err != nil {
		t.Fatal(err)
	}
	parties.CanPartyWithRuntimeActor = rt.IsContractPartner

	var kwanActor *enterworld.Character
	for _, a := range game.caravanActors {
		if a.Name == "[AI]Hunter_Kwan" {
			kwanActor = a
			break
		}
	}
	if kwanActor == nil {
		t.Fatal("missing [AI]Hunter_Kwan actor")
	}
	if speed := game.caravanRunSpeed(kwanActor); speed != 75 {
		t.Fatalf("caravanRunSpeed(JobHunter) = %v, want 75", speed)
	}
	if label := formatCaravanMapLabel(&caravanai.AgentUnit{
		Name: "[AI]Trader_Jin", Role: caravanai.RoleTrader, State: caravanai.StateTraveling,
		CargoStars: 5, OriginTown: caravanai.TownJangan, DestTown: caravanai.TownDonwhang,
	}); label != "[AI]Trader_Jin (5* Jangan -> Donwhang)" {
		t.Fatalf("formatCaravanMapLabel = %q", label)
	}

	// Place Kwan right beside the players in Jangan and admit him.
	rt.StageRecruiters(wiringPublicationDivision, caravanai.TownJangan, jangan)
	var kwanSnap caravanai.AgentUnit
	for _, u := range rt.EnsureDivisionRoster(wiringPublicationDivision) {
		if u.Name == "[AI]Hunter_Kwan" {
			kwanSnap = *u
			break
		}
	}
	kwanAt := simulation.Spawn{RegionID: kwanSnap.RegionID, X: float64(kwanSnap.X), Y: float64(kwanSnap.Y), Z: float64(kwanSnap.Z)}
	items.SyncServerActor(wiringPublicationDivision, kwanActor, kwanAt, false, 0, false)
	if err := items.AdmitServerActor(wiringPublicationDivision, kwanActor.Name); err != nil {
		t.Fatal(err)
	}
	deps.MutateCharacter(hirer, "stage hirer", func() {
		hirer.World.Spawn = ptrSpawn(kwanAt.RegionID, kwanAt.X, kwanAt.Y, kwanAt.Z)
		gold := int64(5_000_000)
		hirer.Gold = &gold
		hirer.MissionInventory = []domain.InventoryRow{
			{Slot: enterworld.JobSuitSlot, RefObjID: 8986, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobTrader), StackCount: 1},
		}
	})
	deps.MutateCharacter(other, "stage other", func() {
		other.World.Spawn = ptrSpawn(kwanAt.RegionID, kwanAt.X, kwanAt.Y, kwanAt.Z)
		other.MissionInventory = []domain.InventoryRow{
			{Slot: enterworld.JobSuitSlot, RefObjID: 8986, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, domain.JobTrader), StackCount: 1},
		}
	})

	bindSession := func(account string, c *enterworld.Character) (*transport.Session, *wiringPublicationConn) {
		conn := &wiringPublicationConn{
			in: make(chan transport.Frame, 8), out: make(chan transport.Frame, 64), closed: make(chan struct{}),
		}
		conn.in <- transport.Frame{Opcode: transport.OpHello, Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(account)})}
		go server.Hub.AcceptConn(conn)
		t.Cleanup(func() { _ = conn.Close("test complete") })
		var welcome transport.Frame
		select {
		case welcome = <-conn.out:
		case <-time.After(wiringPublicationTimeout):
			t.Fatal("missing welcome")
		}
		w, err := transport.DecodeWelcome(welcome.Payload)
		if err != nil {
			t.Fatal(err)
		}
		sess, ok := server.Hub.Session(w.SessionID)
		if !ok {
			t.Fatal("missing session")
		}
		sess.BindCharacter(wiringPublicationDivision, c.Name, uint32(c.ID))
		if !sess.TryMarkWorldReady() {
			t.Fatal("world ready failed")
		}
		server.Hub.BindExclusive(wiringPublicationDivision+":"+strings.ToLower(c.Name), sess)
		parties.WorldBound(sess, wiringPublicationDivision, c)
		return sess, conn
	}
	_, hirerConn := bindSession("acct-hirer", hirer)
	_, otherConn := bindSession("acct-other", other)

	waitForOpcode := func(conn *wiringPublicationConn, opcode uint16) transport.Frame {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case f := <-conn.out:
				if f.Opcode == opcode {
					return f
				}
			case <-deadline:
				t.Fatalf("timed out waiting for opcode 0x%04X", opcode)
			}
		}
	}

	kwanGID := enterworld.ObjectIDForCharacter(kwanActor)
	invitePayload := make([]byte, 5)
	binary.LittleEndian.PutUint32(invitePayload[0:4], kwanGID)
	invitePayload[4] = party.PartyOptionExpShare | party.PartyOptionItemShare | party.PartyOptionJoinAnyone

	// Non-hirer invite must be refused without forming a party.
	otherConn.in <- transport.Frame{Opcode: party.OpPartyInviteRequest, Payload: invitePayload}
	ack := waitForOpcode(otherConn, party.OpCreatePartyAck)
	if len(ack.Payload) != 2 || ack.Payload[0] != 2 || ack.Payload[1] != 0x0c {
		t.Fatalf("expected non-hirer 0x70D5 refusal {2, 0x0c}, got payload=% X", ack.Payload)
	}

	// !hire forms the party at once, the hirer leading it: hiring was the
	// player's consent, so no 0x3393 prompt asks again.
	reply, handled := rt.HandlePlayerCommand(wiringPublicationDivision, hirer.Name, domain.JobTrader, "!hire [AI]Hunter_Kwan", 1000)
	if !handled || !strings.Contains(reply, "joins you for one trade run") {
		t.Fatalf("!hire unexpected reply=%q handled=%v", reply, handled)
	}
	_ = waitForOpcode(hirerConn, party.OpCreatePartyAck)
	_ = waitForOpcode(hirerConn, party.OpPartyInfo)
	if snapshot, ok := parties.Registry().PartyOf(wiringPublicationDivision, "[AI]Hunter_Kwan"); !ok {
		t.Fatal("expected the hire to form the party")
	} else if snapshot.LeaderID != enterworld.ObjectIDForCharacter(hirer) {
		t.Fatalf("the hirer does not lead the hire's party (leader %d)", snapshot.LeaderID)
	}

	// Leaving dissolves bot-only party, then hirer manually sending 0x70D5 auto-accepts immediately.
	hirerConn.in <- transport.Frame{Opcode: party.OpPartyLeaveRequest}
	_ = waitForOpcode(hirerConn, party.OpPartyUpdate)
	if _, ok := parties.Registry().PartyOf(wiringPublicationDivision, "[AI]Hunter_Kwan"); ok {
		t.Fatal("bot-only party should not linger after hirer leaves")
	}

	hirerConn.in <- transport.Frame{Opcode: party.OpPartyInviteRequest, Payload: invitePayload}
	ack = waitForOpcode(hirerConn, party.OpCreatePartyAck)
	if len(ack.Payload) < 1 || ack.Payload[0] != 1 {
		t.Fatalf("expected immediate auto-accept 0xB0D5 from hirer invite, got % X", ack.Payload)
	}
	_ = waitForOpcode(hirerConn, party.OpPartyInfo)
	if _, ok := parties.Registry().PartyOf(wiringPublicationDivision, "[AI]Hunter_Kwan"); !ok {
		t.Fatal("expected party formed after hirer direct 0x70D5 invite")
	}
}

/*
================
TestCaravanThiefDenOpenPlazaAndMovement

Verifies against the shipped v1.150 navmesh that:
  - TownThiefDen terminal is on open walkable ground (not inside NPC_TD_THIEF_BUY's stairs),
  - Idle Thieves in Thief Den actually move around instead of stacking stuck on one pixel,
  - Highway patrol Thieves remain out on the roads (outside Thief Den),
  - Traders stay mounted on their Lv.90 COS_T_DHORSE3 transport horses.

================
*/
func TestCaravanThiefDenOpenPlazaAndMovement(t *testing.T) {
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
	game := &gameplayPlane{divisionID: "caravan-thiefden", deps: deps, items: action.NewRuntime(deps, nil)}
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
	denTerm := cfg.Terminals[caravanai.TownThiefDen]
	t.Logf("ThiefDen terminal: region=%d x=%.2f y=%.2f z=%.2f", denTerm.RegionID, denTerm.X, denTerm.Y, denTerm.Z)
	game.caravanAgents = caravanai.NewRuntime(cfg, nil, nil, caravanai.WorldHooks{Move: game.caravanMove, Ferry: game.caravanFerry})
	t.Cleanup(func() { _ = game.caravanAgents.Close() })
	if err := game.initCaravanActors(); err != nil {
		t.Fatal(err)
	}

	// Step all agents 80 ticks (8 seconds) and verify idle Thieves move freely
	// in Thief Den, 4 highway Thieves stay outside Thief Den, and every Trader
	// mounts its Lv.90 COS_T_DHORSE3 transport horse.
	initial := map[string]caravanai.Waypoint{}
	for _, u := range game.caravanAgents.EnsureDivisionRoster(game.divisionID) {
		initial[u.Name] = caravanai.Waypoint{RegionID: u.RegionID, X: u.X, Y: u.Y, Z: u.Z}
	}
	now := int64(1_000_000)
	for tick := 0; tick < 80; tick++ {
		now += 100
		for i, u := range game.caravanAgents.EnsureDivisionRoster(game.divisionID) {
			_, updated, _ := game.caravanAgents.StepAgentWithWorldThreat(context.Background(), game.divisionID, u.Name, nil, false, now)
			if updated != nil && i < len(game.caravanActors) {
				c := game.caravanActors[i]
				if updated.Role == caravanai.RoleTrader {
					game.items.ServerActorRide(game.divisionID, c.Name, true)
				}
			}
		}
	}
	movedInDen := 0
	patrollingOutsideDen := 0
	for i, u := range game.caravanAgents.EnsureDivisionRoster(game.divisionID) {
		if u.Role == caravanai.RoleThief {
			moved := caravanai.WaypointDistance(initial[u.Name], caravanai.Waypoint{RegionID: u.RegionID, X: u.X, Z: u.Z})
			t.Logf("Thief %-18s state=%-12s region=%d (%.1f,%.1f) avail=%v moved=%.1f", u.Name, u.State, u.RegionID, u.X, u.Z, u.AvailableForHire, moved)
			if u.RegionID == denTerm.RegionID {
				if moved > 5 {
					movedInDen++
				}
				if !u.AvailableForHire {
					t.Fatalf("idle Thief %s in Thief Den is not hireable (state=%s)", u.Name, u.State)
				}
			} else {
				patrollingOutsideDen++
				if moved <= 5 {
					t.Fatalf("highway Thief %s did not move (region=%d moved=%.1f)", u.Name, u.RegionID, moved)
				}
			}
		}
		if u.Role == caravanai.RoleTrader {
			c := game.caravanActors[i]
			if c.ActiveCOS == nil || !c.ActiveCOS.Mounted || c.ActiveCOS.Codename != "COS_T_DHORSE3" {
				t.Fatalf("Trader %s lost mounted COS_T_DHORSE3: %+v", c.Name, c.ActiveCOS)
			}
		}
	}
	if movedInDen < 4 {
		t.Fatalf("expected idle Thieves in Thief Den to move freely, only %d moved", movedInDen)
	}
	if patrollingOutsideDen != 4 {
		t.Fatalf("expected 4 highway Thieves outside Thief Den, got %d", patrollingOutsideDen)
	}
}
