/*
===========================================================================

hwan_arena_test.go - the Blue Berserk arenas: the trial clock and the ritual

The doors (invitation, party facts, returns, pushes, spawn) and the clock are
stubs; definitions, inventory and the quest objective owners are the
production ones, over the shipped data (skipped without the cap-110 quest
tables, like the other catalog tests).

===========================================================================
*/
package quest

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const hwanTestDivision = "hwan-test"

type hwanWorld struct {
	rt       *Runtime
	division enterworld.StaticCharacterSource
	nowMs    *int64
	lease    instance.Lease
	facts    InstancePartyFacts
	invites  []InstanceInviteCall
	spawned  []simulation.QuestMonsterSpawn
	returned []string
	closed   []instance.Lease
	pushed   map[string][]wire.Frame
}

func newHwanWorld(t *testing.T) *hwanWorld {
	t.Helper()
	defs, items := loadShippedDefinitions(t)
	if _, ok := defs.ByCodename("QNO_CH_HWAN_1_1"); !ok {
		t.Skip("the built data has no cap-110 quest tables (SRO_CAP110_QUESTS)")
	}
	division := enterworld.StaticCharacterSource{}
	deps := &enterworld.Deps{Items: items, Characters: division}
	rt, err := NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
	w := &hwanWorld{rt: rt, division: division, pushed: map[string][]wire.Frame{}, facts: InstancePartyFacts{Found: true}}
	w.nowMs = new(int64)
	*w.nowMs = 1_000_000
	rt.NowMs = func() int64 { return *w.nowMs }
	rt.InstancePartyFacts = func(*enterworld.Character) InstancePartyFacts { return w.facts }
	rt.InstanceInvite = func(_ *enterworld.Character, call InstanceInviteCall) InstanceInviteOutcome {
		w.invites = append(w.invites, call)
		w.lease = instance.Lease{ID: instance.Pack(call.Definition, 7), Generation: 1}
		return InstanceInviteOutcome{Opened: true, LeaderIn: true, Lease: w.lease}
	}
	rt.InstanceReturn = func(c *enterworld.Character) bool { w.returned = append(w.returned, c.Name); return true }
	rt.InstanceClose = func(lease instance.Lease) { w.closed = append(w.closed, lease) }
	rt.PushCharacterFrames = func(name string, frames []wire.Frame) { w.pushed[name] = append(w.pushed[name], frames...) }
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		w.spawned = append(w.spawned, request)
		return true
	}
	rt.RegisterHwanArenaProgrammes()
	return w
}

// player makes a Chinese character who has done what the arena quest needs and accepted it.
func (w *hwanWorld) player(t *testing.T, name string, level int64, quest string, done ...string) *enterworld.Character {
	t.Helper()
	c := hwanChainCharacter(t, w.rt.Defs, level, done...)
	c.Name = name
	experience, skillExp := int64(0), int64(0)
	c.Experience, c.SkillExp = &experience, &skillExp
	w.division[hwanTestDivision] = append(w.division[hwanTestDivision], c)
	if _, err := w.rt.StartQuest(c, quest); err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *hwanWorld) event(kind InstanceEventKind, name string) {
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: kind, Division: hwanTestDivision, Definition: instance.DefinitionID(w.lease.ID.Definition()),
		World: w.lease.ID, Name: name})
}

// enter chooses the entry row and lets the layer see its resident arrive.
func (w *hwanWorld) enter(t *testing.T, c *enterworld.Character, quest, npc string) {
	t.Helper()
	if _, err := w.rt.AdvanceNpcQuest(c, hwanEntryPrefix+quest, npc); err != nil {
		t.Fatal(err)
	}
	w.event(InstanceEventSetup, "")
	w.event(InstanceEventEnter, c.Name)
}

// advance moves the clock and runs one layer pass.
func (w *hwanWorld) advance(ms int64) {
	*w.nowMs += ms
	w.event(InstanceEventUpdate, "")
}

func (w *hwanWorld) spawnedCodes() []string {
	var codes []string
	for _, request := range w.spawned {
		codes = append(codes, request.Codename)
	}
	return codes
}

func countOf(codes []string, want string) int {
	n := 0
	for _, code := range codes {
		if code == want {
			n++
		}
	}
	return n
}

func TestHwanArenaTableMatchesTheWorldTable(t *testing.T) {
	seen := map[int]bool{}
	for _, arena := range hwanArenas {
		seen[int(arena.definition)] = true
		if arena.entry.RegionID == 0 || arena.centre.RegionID != arena.entry.RegionID {
			t.Errorf("definition %d: entry region %d, centre region %d", arena.definition, arena.entry.RegionID, arena.centre.RegionID)
		}
	}
	for id := 32; id <= 39; id++ {
		if !seen[id] {
			t.Errorf("gameworlddata row %d has no arena", id)
		}
	}
}

func TestTrialOneRunsItsClockAndPassesBySurvival(t *testing.T) {
	w := newHwanWorld(t)
	c := w.player(t, "Trialist", 95, "QNO_CH_HWAN_1_1")
	if _, offered := npcRow(w.rt, c, hwanEntryPrefix+"QNO_CH_HWAN_1_1", "NPC_CH_GENARAL"); !offered {
		t.Fatal("the General did not offer the arena row")
	}
	w.facts.JobSuit = true
	if _, err := w.rt.AdvanceNpcQuest(c, hwanEntryPrefix+"QNO_CH_HWAN_1_1", "NPC_CH_GENARAL"); err == nil || len(w.invites) != 0 {
		t.Fatal("a job outfit entered the arena")
	}
	w.facts.JobSuit = false
	w.enter(t, c, "QNO_CH_HWAN_1_1", "NPC_CH_GENARAL")
	if len(w.invites) != 1 || !w.invites[0].Solo || w.invites[0].Definition != 36 || w.invites[0].Entry.RegionID != 22966 {
		t.Fatalf("invites %+v, want one solo entry to definition 36", w.invites)
	}
	w.advance(5_000)
	if len(w.spawned) != 0 {
		t.Fatal("something spawned inside the 10 s grace")
	}
	w.advance(5_000)
	w.advance(0)
	if got := w.spawnedCodes(); countOf(got, "MOB_QT_CH_SOLDIER_01_1") != 1 {
		t.Fatalf("spawned %v, want the commander once", got)
	}
	// An archer every 30 s from then on (the first at once), for five minutes.
	for step := 0; step < 9; step++ {
		w.advance(30_000)
	}
	if got := w.spawnedCodes(); countOf(got, "MOB_QT_CH_SOLDIER_01_2") < 9 {
		t.Fatalf("spawned %v: too few archers after 4.5 minutes", got)
	}
	w.advance(30_000)
	w.advance(0)
	if countOf(w.spawnedCodes(), "MOB_QT_CH_SOLDIER_01_1") != 1 {
		t.Fatal("the commander spawned twice")
	}
	archers := countOf(w.spawnedCodes(), "MOB_QT_CH_SOLDIER_01_2")
	if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_1_11") {
		t.Fatal("no 5 minutes remaining notice")
	}
	w.advance(299_000)
	w.advance(2_000)
	if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_1_09") {
		t.Fatal("the test was not passed at ten minutes")
	}
	if countOf(w.spawnedCodes(), "MOB_QT_CH_SOLDIER_01_2") != archers {
		t.Fatal("archers kept coming after the first five minutes")
	}
	at := activeQuestIndex(c, mustDef(t, w.rt, "QNO_CH_HWAN_1_1").RefID)
	def := mustDef(t, w.rt, "QNO_CH_HWAN_1_1")
	if at < 0 || !objectiveMet(c, def, c.ActiveQuests[at]) {
		t.Fatal("surviving did not complete the trial's objective")
	}
	if len(w.returned) != 0 {
		t.Fatal("returned before the pass grace")
	}
	w.advance(10_000)
	if len(w.returned) != 1 || w.returned[0] != c.Name {
		t.Fatalf("returned %v, want the resident", w.returned)
	}
	if _, err := w.rt.AdvanceNpcQuest(c, "QNO_CH_HWAN_1_1", "NPC_CH_GENARAL"); err != nil {
		t.Fatal(err)
	}
}

func TestTrialCommanderDeathPassesAtOnceAndAnEmptyArenaFails(t *testing.T) {
	w := newHwanWorld(t)
	c := w.player(t, "Killer", 95, "QNO_CH_HWAN_1_1")
	w.enter(t, c, "QNO_CH_HWAN_1_1", "NPC_CH_GENARAL")
	w.advance(10_000)
	w.advance(0)
	// The kill reaches the quest updater from inside the layer.
	c.World = insideLayer(w)
	if frames, _ := w.rt.KillUpdater()(c, "MOB_QT_CH_SOLDIER_01_1", 0); len(frames) == 0 {
		t.Fatal("the kill did not count for the quest")
	}
	w.advance(1_000)
	w.advance(1_000)
	if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_1_09") {
		t.Fatal("the commander's death did not pass the test at once")
	}
	archers := countOf(w.spawnedCodes(), "MOB_QT_CH_SOLDIER_01_2")
	w.advance(30_000)
	if countOf(w.spawnedCodes(), "MOB_QT_CH_SOLDIER_01_2") != archers {
		t.Fatal("archers kept coming after the commander fell")
	}

	// A second player is dead before the commander appears: the test fails and ends after 15 s.
	w2 := newHwanWorld(t)
	d := w2.player(t, "Fallen", 95, "QNO_CH_HWAN_1_1")
	w2.enter(t, d, "QNO_CH_HWAN_1_1", "NPC_CH_GENARAL")
	hp := int64(0)
	d.CurrentHP = &hp
	w2.advance(1_000)
	if !notifiedAny(w2.pushed[d.Name], "SN_TALK_QNO_CH_HWAN_1_1_08") {
		t.Fatal("a dead resident did not fail the test")
	}
	w2.advance(14_000)
	if len(w2.returned) != 0 {
		t.Fatal("returned before the 15 s countdown")
	}
	w2.advance(2_000)
	if len(w2.returned) != 1 {
		t.Fatalf("returned %v after the countdown", w2.returned)
	}
}

func TestTrialTwoSpawnsOneHealerWithTheCommander(t *testing.T) {
	w := newHwanWorld(t)
	c := w.player(t, "Second", 100, "QNO_CH_HWAN_2_1", "QNO_CH_HWAN_1_6")
	w.enter(t, c, "QNO_CH_HWAN_2_1", "NPC_CH_GENARAL")
	if w.invites[0].Definition != 37 {
		t.Fatalf("definition %d, want 37", w.invites[0].Definition)
	}
	w.advance(10_000)
	w.advance(0)
	w.advance(200_000)
	got := w.spawnedCodes()
	if countOf(got, "MOB_QT_CH_SOLDIER_02_1") != 1 || countOf(got, "MOB_QT_CH_SOLDIER_02_2") != 1 {
		t.Fatalf("spawned %v, want the level 100 commander and one healer", got)
	}
}

func TestRitualPaysFragmentsOnlyForABerserkKill(t *testing.T) {
	for _, berserk := range []bool{true, false} {
		w := newHwanWorld(t)
		c := w.player(t, "Ritualist", 95, "QNO_CH_HWAN_1_5", "QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3", "QNO_CH_HWAN_1_3_1", "QNO_CH_HWAN_1_4", "QNO_CH_HWAN_1_4_1")
		w.enter(t, c, "QNO_CH_HWAN_1_5", "NPC_CH_SHAMAN")
		if captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01") != 1 || w.invites[0].Definition != 32 {
			t.Fatalf("talisman %d, definition %d", captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01"), w.invites[0].Definition)
		}
		w.advance(10_000)
		w.advance(0)
		if got := w.spawnedCodes(); len(got) != 1 || got[0] != "MOB_QT_CH_CHARGHOST_01" {
			t.Fatalf("spawned %v, want the Old Ghost", got)
		}
		c.World = insideLayer(w)
		if berserk {
			c.NativeBodyStatus = 1
		}
		w.rt.KillUpdater()(c, "MOB_QT_CH_CHARGHOST_01", 0)
		w.advance(1_000)
		if berserk {
			if captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_02") != 1 || captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01") != 0 {
				t.Fatalf("fragment %d talisman %d", captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_02"), captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01"))
			}
		} else {
			if captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_02") != 0 || captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01") != 1 {
				t.Fatalf("a kill outside Berserk gave a fragment or took the talisman")
			}
			if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_5_09") {
				t.Fatal("no failed-to-obtain notice")
			}
		}
		w.advance(10_000)
		if len(w.returned) != 1 {
			t.Fatalf("returned %v ten seconds after the kill", w.returned)
		}
	}
}

func TestRitualWarnsAtFiveMinutesAndFailsAtTen(t *testing.T) {
	w := newHwanWorld(t)
	c := w.player(t, "Slow", 95, "QNO_CH_HWAN_1_5", "QNO_CH_HWAN_1_1", "QNO_CH_HWAN_1_2", "QNO_CH_HWAN_1_3", "QNO_CH_HWAN_1_3_1", "QNO_CH_HWAN_1_4", "QNO_CH_HWAN_1_4_1")
	w.enter(t, c, "QNO_CH_HWAN_1_5", "NPC_CH_SHAMAN")
	w.advance(10_000)
	w.advance(0)
	w.advance(300_000)
	if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_5_10") {
		t.Fatal("no five minute warning")
	}
	w.advance(300_000)
	w.advance(1_000)
	if !notifiedAny(w.pushed[c.Name], "SN_TALK_QNO_CH_HWAN_1_5_09") || len(w.returned) != 1 {
		t.Fatalf("the ritual did not fail at ten minutes (returned %v)", w.returned)
	}
	if captureItemCount(c, "ITEM_QNO_CH_HWAN_1_5_01") != 1 {
		t.Fatal("a failed ritual took the talisman")
	}
}

func notifiedAny(frames []wire.Frame, symbol string) bool { return notifiedWith(frames, symbol) }

func mustDef(t *testing.T, rt *Runtime, code string) *Definition {
	t.Helper()
	def, ok := rt.Defs.ByCodename(code)
	if !ok {
		t.Fatalf("%s is not loaded", code)
	}
	return def
}

// insideLayer is the world record of a character standing in the test's layer.
func insideLayer(w *hwanWorld) *enterworld.CharacterWorld {
	packed := uint32(w.lease.ID)
	return &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
}
