/*
===========================================================================

goria7_test.go - GORIA 7 (Quests of the Mamluk): the hun-lancer summons

Exercise the shipped GORIA 7 tail on the instanced-world framework
with the real catalogue and inventory planner: the hunter's entry row
and its once-a-day stamp, the proof item's place gates, the layer
clock (lancer at thirty seconds, the boss's first word, the battle's
open, the two warnings, the boss gone, everyone's return), and the
kill's proof credit. The doors (invitation, party facts, returns,
pushes, spawn) and the clock are stubs; definitions, inventory and
completion are the production ones.

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
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const goria7TestDivision = "goria7-test"

type goria7World struct {
	rt       *Runtime
	prev     *Definition
	def      *Definition
	day      *uint16
	nowMs    *int64
	lease    instance.Lease
	division enterworld.StaticCharacterSource
	facts    InstancePartyFacts
	invites  []InstanceInviteCall
	pushed   map[string][]wire.Frame
	spawned  []simulation.QuestMonsterSpawn
	returned []string
	closed   []instance.Lease
}

func newGoria7World(t *testing.T) *goria7World {
	t.Helper()
	licensed.RequireGameData(t)
	textdata := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(textdata)
	defs, err := LoadDefinitions(NewCatalog(textdata), items)
	if err != nil {
		t.Fatal(err)
	}
	division := enterworld.StaticCharacterSource{}
	deps := &enterworld.Deps{Items: items, Levels: enterworld.NewTextdataLevels(textdata), Characters: division}
	rt, err := NewRuntime(deps, defs, progression.NewRuntime(deps).ExperienceUpdater())
	if err != nil {
		t.Fatal(err)
	}
	rt.PlanInventory = action.NewRuntime(deps, nil).PlanQuestInventory
	w := &goria7World{rt: rt, division: division, pushed: make(map[string][]wire.Frame)}
	w.day = new(uint16)
	w.nowMs = new(int64)
	*w.nowMs = 1_000_000
	w.lease = instance.Lease{ID: instance.Pack(goria7Definition, 5), Generation: 1}
	rt.CalendarNow = func() calendar.Value { return calendar.Value{Day: *w.day} }
	rt.NowMs = func() int64 { return *w.nowMs }
	rt.InstancePartyFacts = func(*enterworld.Character) InstancePartyFacts { return w.facts }
	rt.InstanceInvite = func(_ *enterworld.Character, call InstanceInviteCall) InstanceInviteOutcome {
		w.invites = append(w.invites, call)
		return InstanceInviteOutcome{Opened: true, LeaderIn: true, Lease: w.lease}
	}
	rt.InstanceReturn = func(c *enterworld.Character) bool { w.returned = append(w.returned, c.Name); return true }
	rt.InstanceClose = func(lease instance.Lease) { w.closed = append(w.closed, lease) }
	rt.PushCharacterFrames = func(name string, frames []wire.Frame) { w.pushed[name] = append(w.pushed[name], frames...) }
	rt.SpawnQuestMonster = func(_ *enterworld.Character, request simulation.QuestMonsterSpawn) bool {
		w.spawned = append(w.spawned, request)
		return true
	}
	prev, exists := rt.Defs.ByCodename(goria6QuestCodename)
	if !exists {
		t.Fatal("GORIA 6 is not loaded")
	}
	w.prev = prev
	def, exists := rt.Defs.ByCodename(goria7QuestCodename)
	if !exists {
		t.Fatal("GORIA 7 is not loaded")
	}
	w.def = def
	rt.RegisterGoria7Programme()
	return w
}

func goria7Character(w *goria7World, name string, prevDone bool) *enterworld.Character {
	c := questCharacter()
	c.Name = name
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	*c.Level = 50
	experience := int64(0)
	skillExp := int64(0)
	c.Experience, c.SkillExp = &experience, &skillExp
	if prevDone {
		c.CompletedQuestIds = append(c.CompletedQuestIds, w.prev.RefID)
	}
	w.division[goria7TestDivision] = append(w.division[goria7TestDivision], c)
	return c
}

func goria7Accept(t *testing.T, w *goria7World, name string) *enterworld.Character {
	t.Helper()
	c := goria7Character(w, name, true)
	if _, err := w.rt.StartQuest(c, goria7QuestCodename); err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *goria7World) update() {
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventUpdate, Division: goria7TestDivision, Definition: goria7Definition, World: w.lease.ID})
}

func (w *goria7World) openLayer(names ...string) {
	w.rt.goria7.rememberLease(w.lease, names[0])
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventSetup, Division: goria7TestDivision, Definition: goria7Definition, World: w.lease.ID})
	for _, name := range names {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: goria7TestDivision, Definition: goria7Definition, World: w.lease.ID, Name: name})
	}
}

// The lancer's spawn request, pinned whole on purpose: region,
// point, spread and life are the native records.
func (w *goria7World) requireSpawn(t *testing.T, want simulation.QuestMonsterSpawn) {
	t.Helper()
	if len(w.spawned) != 1 || w.spawned[0] != want {
		t.Fatalf("spawned = %v, want %+v", w.spawned, want)
	}
}

/*
================
TestGoria7SpecShape

Pins the authored record: the level gate, the rewards, both
missions and the hunter at both ends.
================
*/
func TestGoria7SpecShape(t *testing.T) {
	w := newGoria7World(t)
	if w.def.MinLevel != 40 {
		t.Fatalf("level condition = %d, want 40", w.def.MinLevel)
	}
	if w.def.RewardExp != 55_000 || w.def.RewardSkillExp != 4_000 {
		t.Fatalf("rewards = %d/%d, want 55000 and 4000", w.def.RewardExp, w.def.RewardSkillExp)
	}
	if w.def.StartNpcCodename != goria7NpcCodename || w.def.EndNpcCodename != goria7NpcCodename {
		t.Fatalf("start/end NPC = %s/%s, want the hunter at both ends", w.def.StartNpcCodename, w.def.EndNpcCodename)
	}
	if len(w.def.Objectives) != 2 {
		t.Fatalf("missions = %d, want 2", len(w.def.Objectives))
	}
	kill := w.def.Objectives[0]
	if kill.Objective != ObjectiveKill || len(kill.KillMonsterCodenames) != 1 || kill.KillMonsterCodenames[0] != goria7BossCodename || kill.KillCount != 1 {
		t.Fatalf("mission 1 = %+v, want the lancer kill", kill)
	}
	collect := w.def.Objectives[1]
	if collect.Objective != ObjectiveCollect || collect.CollectItemCodename != goria7ItemCodename || collect.CollectCount != 1 {
		t.Fatalf("mission 2 = %+v, want one proof item", collect)
	}
}

/*
================
TestGoria7EntryRowAndDayStamp

The hunter answers the entry row while the kill stands undone and
nobody entered today; opening the layer stamps today, so the row
becomes the come-back-tomorrow line. A solo hunter offers nothing.
================
*/
func TestGoria7EntryRowAndDayStamp(t *testing.T) {
	w := newGoria7World(t)
	*w.day = 20
	c := goria7Accept(t, w, "EntryLeader")
	// Solo, the entry row stands (the native offer does not ask
	// after the party), but choosing it opens nothing: the entry
	// wants a shared party led by the talker.
	w.facts = InstancePartyFacts{Found: true}
	if _, offered := npcRow(w.rt, c, goria7EntryPrefix+goria7QuestCodename, goria7NpcCodename); !offered {
		t.Fatal("the entry row was not offered")
	}
	if _, err := w.rt.AdvanceNpcQuest(c, goria7EntryPrefix+goria7QuestCodename, goria7NpcCodename); err == nil {
		t.Fatal("a solo entry pick opened the layer")
	}
	if len(w.invites) != 0 {
		t.Fatal("a solo entry pick sent an invitation")
	}
	// Shared, led by the talker: the row stands and choosing it
	// opens definition 13 at the native point, stamping today.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	row, offered := npcRow(w.rt, c, goria7EntryPrefix+goria7QuestCodename, goria7NpcCodename)
	if !offered || row.PromptSymbol != goria7SymEnter || row.TitleSymbol != goria7SymEnterRow {
		t.Fatalf("the entry row was %+v (offered %v)", row, offered)
	}
	_, err := w.rt.AdvanceNpcQuest(c, goria7EntryPrefix+goria7QuestCodename, goria7NpcCodename)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.invites) != 1 {
		t.Fatalf("invites = %v, want one", w.invites)
	}
	invite := w.invites[0]
	if invite.Definition != goria7Definition || invite.Entry != goria7EntrySpawn {
		t.Fatalf("invite = %+v, want definition 13 at the native point", invite)
	}
	if invite.DeadlineMs-*w.nowMs != 1_260_000+60_000 {
		t.Fatalf("invite deadline = %d, want now + 1,320,000", invite.DeadlineMs)
	}
	if !w.rt.goria7EnteredToday(c, w.def) {
		t.Fatal("the entry day was not stamped")
	}
	// Entered today: the row becomes the come-back line and no
	// second invitation goes out.
	row, offered = npcRow(w.rt, c, goria7QuestCodename, goria7NpcCodename)
	if !offered || row.PromptSymbol != goria7SymEnterWait || !row.Informational {
		t.Fatalf("the come-back row was %+v (offered %v)", row, offered)
	}
	// Once the proof stands in the bag the entry row is gone,
	// and the hunter answers the not-achieved line instead,
	// the same row the native offer branch shows a proof
	// holder (default row; probed 2026-10-26).
	holdItems(t, w.rt, c, goria7ItemCodename, 1)
	for _, row := range w.rt.OptionsForNpc(c, goria7NpcCodename) {
		if row.Codename == goria7EntryPrefix+goria7QuestCodename {
			t.Fatal("the entry row was offered to a proof holder")
		}
	}
	row, offered = npcRow(w.rt, c, goria7QuestCodename, goria7NpcCodename)
	if !offered || row.PromptSymbol != goria7SymNotAchieved || !row.Informational || row.Complete {
		t.Fatalf("the proof-holder row was %+v (offered %v)", row, offered)
	}
	// A member who is not the talk leader cannot open the hunt.
	member := goria7Accept(t, w, "EntryMember")
	memberFacts := InstancePartyFacts{Found: true, InParty: true, Shared: true}
	w.facts = memberFacts
	if _, err := w.rt.AdvanceNpcQuest(member, goria7EntryPrefix+goria7QuestCodename, goria7NpcCodename); err == nil {
		t.Fatal("a non-leader opened the layer")
	}
	if len(w.invites) != 1 {
		t.Fatal("a non-leader's pick sent an invitation")
	}
}

/*
================
TestGoria7ProofItemGates

The item's three refusals, in the native order, and the party it
calls when they pass. The proof is never spent by its own use.
================
*/
func TestGoria7ProofItemGates(t *testing.T) {
	w := newGoria7World(t)
	c := goria7Accept(t, w, "ProofCarrier")
	holdItems(t, w.rt, c, goria7ItemCodename, 1)
	// Not a shared party: the shared-party line.
	w.facts = InstancePartyFacts{Found: true}
	frames, admitted := w.rt.BeginItemUse(c, goria7ItemCodename, goria7EntrySpawn, *w.nowMs)
	if admitted || !hasNotice(frames, goria7SymItemParty) || len(w.invites) != 0 {
		t.Fatalf("party gate: admitted %v, invites %d", admitted, len(w.invites))
	}
	// A shared party in the wrong region: the location line - the
	// region is folded into the point the handler measures from.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	far := simulation.Spawn{RegionID: 25000, X: 0, Y: 0, Z: 0}
	frames, admitted = w.rt.BeginItemUse(c, goria7ItemCodename, far, *w.nowMs)
	if admitted || !hasNotice(frames, goria7SymItemLocation) || len(w.invites) != 0 {
		t.Fatalf("region gate: admitted %v, invites %d", admitted, len(w.invites))
	}
	// In the region but far from the point: the same location line.
	inRegion := simulation.Spawn{RegionID: 26991, X: 0, Y: 0, Z: 0}
	frames, admitted = w.rt.BeginItemUse(c, goria7ItemCodename, inRegion, *w.nowMs)
	if admitted || !hasNotice(frames, goria7SymItemLocation) || len(w.invites) != 0 {
		t.Fatalf("distance gate: admitted %v, invites %d", admitted, len(w.invites))
	}
	// At the point but not the party leader: the leader line - and
	// a non-leader in the wrong region hears the location line
	// first (the native order asks after the point before the
	// leader).
	w.facts = InstancePartyFacts{Found: true, InParty: true, Shared: true}
	frames, admitted = w.rt.BeginItemUse(c, goria7ItemCodename, goria7EntrySpawn, *w.nowMs)
	if admitted || !hasNotice(frames, goria7SymItemLeader) || len(w.invites) != 0 {
		t.Fatalf("leader gate: admitted %v, invites %d", admitted, len(w.invites))
	}
	frames, admitted = w.rt.BeginItemUse(c, goria7ItemCodename, far, *w.nowMs)
	if admitted || !hasNotice(frames, goria7SymItemLocation) || len(w.invites) != 0 {
		t.Fatalf("leader-location order: admitted %v, invites %d", admitted, len(w.invites))
	}
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	// At the point the party is called in; the proof stays put
	// (admitted is the caller's spend signal, and this use spends
	// nothing - the delivery at the hunter spends it).
	if _, admitted := w.rt.BeginItemUse(c, goria7ItemCodename, goria7EntrySpawn, *w.nowMs); admitted {
		t.Fatal("the proof was spent by its own use")
	}
	if len(w.invites) != 1 || w.invites[0].Definition != goria7Definition {
		t.Fatalf("invites = %v, want the summons", w.invites)
	}
	if captureItemCount(c, goria7ItemCodename) != 1 {
		t.Fatal("the proof was spent by its own use")
	}
}

/*
================
TestGoria7LayerClock

The native marks, pinned as literals: the lancer stands at thirty
seconds, speaks at two, opens its battle at five, warns at ten and
five minutes out, and is gone twenty minutes in; ten seconds later
the room is home and the layer closed.
================
*/
func TestGoria7LayerClock(t *testing.T) {
	w := newGoria7World(t)
	leader := goria7Accept(t, w, "ClockLeader")
	member := goria7Accept(t, w, "ClockMember")
	w.openLayer(leader.Name, member.Name)
	opened := *w.nowMs
	// Before the clock's first mark nothing moves.
	*w.nowMs = opened + 30_000 - 1
	w.update()
	if len(w.spawned) != 0 {
		t.Fatal("the lancer stood a millisecond early")
	}
	*w.nowMs = opened + 30_000
	w.update()
	w.requireSpawn(t, simulation.QuestMonsterSpawn{
		Division: goria7TestDivision, Population: w.lease,
		Codename: goria7BossCodename, Position: simulation.Spawn{RegionID: 26991, X: 1154, Y: 142, Z: 1506, Angle: 0},
		FixedPosition: true, RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 1_207_000, NowMs: *w.nowMs,
	})
	spawnedAt := opened + 30_000
	battleAt := spawnedAt + 2_000 + 5_000
	beats := []struct {
		at     int64
		symbol string
	}{
		{spawnedAt + 2_000, goria7SymBossLine},
		{battleAt, goria7SymBattleBegun},
		{battleAt + 600_000, goria7SymWarnFirst},
		{battleAt + 900_000, goria7SymWarnSecond},
		{battleAt + 1_200_000, goria7SymBossLeaves},
	}
	for _, beat := range beats {
		*w.nowMs = beat.at - 1
		w.update()
		if hasNotice(w.pushed[member.Name], beat.symbol) {
			t.Fatalf("line %s landed a millisecond early", beat.symbol)
		}
		*w.nowMs = beat.at
		w.update()
		if !hasNotice(w.pushed[member.Name], beat.symbol) {
			t.Fatalf("line %s never landed", beat.symbol)
		}
	}
	// The boss gone, the layer still stands for ten seconds, then
	// puts everyone out.
	*w.nowMs = battleAt + 1_210_000 - 1
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("the layer ended before its own clock ran out")
	}
	*w.nowMs = battleAt + 1_210_000
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 2 {
		t.Fatalf("closed %d returned %d, want 1 and 2", len(w.closed), len(w.returned))
	}
	// A kill marked after the boss walked earns nothing.
	packed := uint32(instance.Pack(goria7Definition, 5))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteGoria7Kill(leader, goria7BossCodename)
	if captureItemCount(leader, goria7ItemCodename) != 0 {
		t.Fatal("a late kill handed over a proof")
	}
}

/*
================
TestGoria7KillCreditAndTurnIn

The lancer falls: the room hears its death line, every resident the
quest stands open for takes the proof, and the hunt pays at the
hunter. A finished helper in the room takes nothing.
================
*/
func TestGoria7KillCreditAndTurnIn(t *testing.T) {
	w := newGoria7World(t)
	t.Setenv(progression.EnvBetaGrowth, "0")
	leader := goria7Accept(t, w, "KillLeader")
	member := goria7Accept(t, w, "KillMember")
	finished := goria7Character(w, "KillFinished", true)
	finished.CompletedQuestIds = append(finished.CompletedQuestIds, w.def.RefID)
	w.openLayer(leader.Name, member.Name, finished.Name)
	w.update()
	// A kill in another world, or of something else, does nothing.
	wrong := uint32(instance.Pack(instance.DefinitionID(10), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteGoria7Kill(leader, goria7BossCodename)
	w.update()
	if len(w.closed) != 0 || hasNotice(w.pushed[member.Name], goria7SymBossDeath) {
		t.Fatal("a kill in another world reached the layer")
	}
	packed := uint32(instance.Pack(goria7Definition, 5))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteGoria7Kill(leader, "MOB_QT_01_SOMETHING_ELSE")
	w.update()
	if len(w.closed) != 0 || hasNotice(w.pushed[member.Name], goria7SymBossDeath) {
		t.Fatal("another monster's fall reached the layer")
	}
	// The lancer falls: death line and proof for the hunters; a
	// minute later everyone is home.
	w.rt.noteGoria7Kill(leader, goria7BossCodename)
	w.update()
	if !hasNotice(w.pushed[member.Name], goria7SymBossDeath) {
		t.Fatal("the death line never landed")
	}
	for _, c := range []*enterworld.Character{leader, member} {
		if captureItemCount(c, goria7ItemCodename) != 1 {
			t.Fatalf("%s was not credited with the proof", c.Name)
		}
		if at := activeQuestIndex(c, w.def.RefID); at < 0 || !objectiveMet(c, w.def, c.ActiveQuests[at]) {
			t.Fatalf("%s's quest did not stand achieved", c.Name)
		}
	}
	if captureItemCount(finished, goria7ItemCodename) != 0 {
		t.Fatal("a finished helper was credited with the proof")
	}
	// The grace minute: still open, then everyone home.
	w.update()
	if len(w.closed) != 0 || len(w.returned) != 0 {
		t.Fatal("the hunt closed before its grace ran out")
	}
	*w.nowMs += goria7KillGraceMs
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 3 {
		t.Fatalf("closed %d returned %d, want 1 and 3", len(w.closed), len(w.returned))
	}
	// Back at the hunter the proof reports and pays: EXP, skill
	// EXP, and the quest is done for good.
	before := *leader.Experience
	var pointsBefore int64
	if leader.SkillPoints != nil {
		pointsBefore = *leader.SkillPoints
	}
	skillBefore := *leader.SkillExp
	if _, err := w.rt.AdvanceNpcQuest(leader, goria7QuestCodename, goria7NpcCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(leader, w.def.RefID) {
		t.Fatal("the proof did not complete at the hunter")
	}
	if got := *leader.Experience - before; got != 55_000 {
		t.Fatalf("the hunter paid %d EXP, want 55000", got)
	}
	var pointsAfter int64
	if leader.SkillPoints != nil {
		pointsAfter = *leader.SkillPoints
	}
	if got := (pointsAfter-pointsBefore)*progression.SkillExpPerSP + *leader.SkillExp - skillBefore; got != 4_000 {
		t.Fatalf("the hunter paid %d skill EXP, want 4000", got)
	}
	if captureItemCount(leader, goria7ItemCodename) != 0 {
		t.Fatal("the proof survived its turn-in")
	}
	if _, err := w.rt.StartQuest(leader, goria7QuestCodename); err == nil {
		t.Fatal("GORIA 7 was accepted a second time")
	}
}

/*
================
TestGoria7PartyChangedExpels

A resident whose party standing breaks mid-summon hears the
instance-condition line and is sent out; anyone else in the room
is untouched.
================
*/
func TestGoria7PartyChangedExpels(t *testing.T) {
	w := newGoria7World(t)
	leader := goria7Accept(t, w, "PartyLeader")
	member := goria7Accept(t, w, "PartyMember")
	w.openLayer(leader.Name, member.Name)
	changed := func(name string) {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventPartyChanged, Division: goria7TestDivision, Definition: goria7Definition, World: w.lease.ID, Name: name})
	}
	changed("Stranger")
	if len(w.returned) != 0 {
		t.Fatalf("returned = %v, want none", w.returned)
	}
	changed(member.Name)
	if !hasNotice(w.pushed[member.Name], goria7SymCondition) {
		t.Fatal("the member never heard the instance-condition line")
	}
	if len(w.returned) != 1 || w.returned[0] != member.Name {
		t.Fatalf("returned = %v, want the member", w.returned)
	}
	if hasNotice(w.pushed[leader.Name], goria7SymCondition) {
		t.Fatal("the leader heard the condition line")
	}
}

/*
================
TestGoria7ExitGateRisesBesideTheBoss

The reference kill handler raises the exit gate beside the lancer (#1878):
the site its owner reports for the fall in this layer is where the gate
rises, once, on the kill; a site in another world or of another monster
is ignored. The room stays for the sixty-second grace, then the layer's
end returns it and takes the gate down.
================
*/
func TestGoria7ExitGateRisesBesideTheBoss(t *testing.T) {
	w := newGoria7World(t)
	type raised struct {
		world instance.ID
		fell  simulation.Spawn
	}
	var gates []raised
	var removed []instance.ID
	w.rt.SpawnExitGate = func(world instance.ID, fell simulation.Spawn) bool {
		gates = append(gates, raised{world, fell})
		return true
	}
	w.rt.RemoveExitGates = func(world instance.ID) { removed = append(removed, world) }
	leader := goria7Accept(t, w, "GateLeader")
	w.openLayer(leader.Name)
	w.update()

	site := simulation.Spawn{RegionID: 25000, X: 120, Y: 4, Z: -30}
	wrong := uint32(instance.Pack(instance.DefinitionID(2), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, goria7BossCodename, simulation.Spawn{RegionID: 1})
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, "MOB_QT_01_SOMETHING_ELSE", simulation.Spawn{RegionID: 2})
	w.rt.NoteKillSite(leader, goria7BossCodename, site)
	w.rt.KillUpdater()(leader, goria7BossCodename, 0)
	w.rt.NoteKillSite(leader, goria7BossCodename, simulation.Spawn{RegionID: 3})
	w.update()
	if len(gates) != 1 || gates[0].world != w.lease.ID || gates[0].fell != site {
		t.Fatalf("gates raised = %+v, want one beside %+v in %v", gates, site, w.lease.ID)
	}
	*w.nowMs += goria7KillGraceMs - 1
	w.update()
	if len(gates) != 1 || len(w.closed) != 0 || len(removed) != 0 {
		t.Fatalf("before the grace ran out: gates %+v closed %v removed %v", gates, w.closed, removed)
	}
	*w.nowMs++
	w.update()
	if len(w.closed) != 1 || len(removed) != 1 || removed[0] != w.lease.ID {
		t.Fatalf("closed %v gates removed %+v, want the layer's", w.closed, removed)
	}
}

/*
================
TestGoria7LastLeaveTakesTheGateDown

The room may leave through the gate before the grace runs out: the
last resident's leave closes the layer, and the gate goes with it.
================
*/
func TestGoria7LastLeaveTakesTheGateDown(t *testing.T) {
	w := newGoria7World(t)
	var removed []instance.ID
	w.rt.SpawnExitGate = func(instance.ID, simulation.Spawn) bool { return true }
	w.rt.RemoveExitGates = func(world instance.ID) { removed = append(removed, world) }
	leader := goria7Accept(t, w, "GateLeader")
	w.openLayer(leader.Name)
	w.update()
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, goria7BossCodename, simulation.Spawn{RegionID: 25000})
	w.rt.KillUpdater()(leader, goria7BossCodename, 0)
	w.update()
	for _, name := range []string{"GateLeader"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: goria7TestDivision, Definition: goria7Definition, World: w.lease.ID, Name: name})
	}
	if len(removed) != 1 || removed[0] != w.lease.ID || len(w.closed) != 1 {
		t.Fatalf("gates removed %+v closed %v, want the layer's on the last leave", removed, w.closed)
	}
}
