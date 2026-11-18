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
	"opensro.online/server/internal/transport"
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
		if c.ActiveCOS != nil && (c.ActiveCOS.GID < domain.COSGIDBase || c.ActiveCOS.GID > domain.COSGIDLimit || c.ActiveCOS.Codename != "COS_T_DHORSE1") {
			t.Fatal("transport identity or shipped reference invalid")
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
	var bandit monster.MonsterRef
	for _, ref := range template.Refs {
		if ref.TypeID4 == 2 && ref.Level == 90 && strings.HasPrefix(ref.Codename, "MOB_THIEF_") {
			bandit = ref
			break
		}
	}
	if bandit.RefObjID == 0 {
		t.Fatal("shipped max-level thief reference unavailable")
	}
	game.items.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{bandit.RefObjID: bandit}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: bandit.RefObjID, RegionID: at.RegionID, X: at.X + 3, Y: at.Y, Z: at.Z}, MaxCount: 1, RetailEvidence: true}}))
	game.items.Monsters.StartDivision(game.divisionID)
	game.items.Monsters.AdvancePopulation(game.items.Monsters.CurrentTimeMillis())
	guard := game.caravanActors[4]
	game.items.SyncServerActor(game.divisionID, guard, at, false, 0, false)
	if err := game.items.AdmitServerActor(game.divisionID, guard.Name); err != nil {
		t.Fatal(err)
	}
	unit := game.caravanAgents.EnsureDivisionRoster(game.divisionID)[4]
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
	oldGID := trader.RuntimeTransportGID
	trader.ActiveCOS = nil
	trader.CurrentHP = new(int64)
	revived := game.items.SyncServerActor(game.divisionID, trader, at, true, 10, false)
	if revived.ActiveCOS == nil || revived.ActiveCOS.GID != oldGID || len(revived.ActiveCOS.Container.Rows) != 10 || !enterworld.CharacterAlive(revived) {
		t.Fatal("defeated caravan did not recover its actor and loaded transport")
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
		gold := int64(50000)
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
	rt := caravanai.NewRuntime(cfg, nil, caravanai.WorldHooks{
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
		gold := int64(50000)
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

	// !hire auto-invites the hiring player (0x3393 prompt) and accepting forms the party.
	reply, handled := rt.HandlePlayerCommand(wiringPublicationDivision, hirer.Name, domain.JobTrader, "!hire [AI]Hunter_Kwan", 1000)
	if !handled || !strings.Contains(reply, "guard your caravan") {
		t.Fatalf("!hire unexpected reply=%q handled=%v", reply, handled)
	}
	_ = waitForOpcode(hirerConn, party.OpInvitationProposal)

	hirerConn.in <- transport.Frame{Opcode: party.OpInvitationProposal, Payload: []byte{1, 1}}
	_ = waitForOpcode(hirerConn, party.OpCreatePartyAck)
	_ = waitForOpcode(hirerConn, party.OpPartyInfo)
	if _, ok := parties.Registry().PartyOf(wiringPublicationDivision, "[AI]Hunter_Kwan"); !ok {
		t.Fatal("expected party formed after accepting !hire prompt")
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
