/*
===========================================================================

easteu21_test.go - EASTEU 20/21: the Witch's party gate and the summon

Exercise the shipped Cerberus chain tail on the instanced-world
framework with the real catalogue and inventory planner: the lion
hunt, the shared-party offer gate, acceptance's Call of Hell and its
once-a-day re-grant, the plaza use gates and invitation, the battle
clock (shout, activation, warnings, the boss leaving, the layer's
end), and the party-wide kill credit with its sixty-second grace.

The doors (invitation, party facts, returns, frame pushes, spawn) and
the programme clock are stubs; definitions, inventory and completion
are the production ones.

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

const easteu21TestDivision = "easteu21-test"

// easteu21World is one fixture: the party door knobs (facts), captured
// door outcomes (invites, pushes, spawns, returns, closes) and both
// clocks.
type easteu21World struct {
	rt       *Runtime
	def20    *Definition
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

func newEasteu21World(t *testing.T) *easteu21World {
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
	w := &easteu21World{rt: rt, division: division, pushed: make(map[string][]wire.Frame)}
	w.day = new(uint16)
	w.nowMs = new(int64)
	*w.nowMs = 1_000_000
	w.lease = instance.Lease{ID: instance.Pack(easteu21Definition, 3), Generation: 1}
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
	def20, exists := rt.Defs.ByCodename(easteu20QuestCodename)
	if !exists {
		t.Fatal("EASTEU 20 is not loaded")
	}
	w.def20 = def20
	def, exists := rt.Defs.ByCodename(easteu21QuestCodename)
	if !exists {
		t.Fatal("EASTEU 21 is not loaded")
	}
	w.def = def
	rt.RegisterEasteu21Programme()
	return w
}

// easteuCharacter is one fixture character, its Cerberus 1 hunt done
// (and Cerberus 2 too, when asked).
func easteuCharacter(w *easteu21World, name string, huntDone bool) *enterworld.Character {
	c := questCharacter()
	c.Name = name
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	*c.Level = 100
	experience := int64(0)
	skillExp := int64(0)
	c.Experience, c.SkillExp = &experience, &skillExp
	if first, ok := w.rt.Defs.ByCodename("QNO_EU_EASTEU_19"); ok {
		c.CompletedQuestIds = append(c.CompletedQuestIds, first.RefID)
	}
	if huntDone {
		c.CompletedQuestIds = append(c.CompletedQuestIds, w.def20.RefID)
	}
	w.division[easteu21TestDivision] = append(w.division[easteu21TestDivision], c)
	return c
}

func easteu21Accept(t *testing.T, w *easteu21World, name string) *enterworld.Character {
	t.Helper()
	c := easteuCharacter(w, name, true)
	if _, err := w.rt.StartQuest(c, easteu21QuestCodename); err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *easteu21World) update() {
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventUpdate, Division: easteu21TestDivision, Definition: easteu21Definition, World: w.lease.ID})
}

func (w *easteu21World) openLayer(names ...string) {
	w.rt.easteu21.rememberLease(w.lease, names[0])
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventSetup, Division: easteu21TestDivision, Definition: easteu21Definition, World: w.lease.ID})
	for _, name := range names {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: easteu21TestDivision, Definition: easteu21Definition, World: w.lease.ID, Name: name})
	}
}

func loseCall(c *enterworld.Character) {
	rows := c.MissionInventory[:0]
	for _, row := range c.MissionInventory {
		if row.Codename != easteu21CallCodename {
			rows = append(rows, row)
		}
	}
	c.MissionInventory = rows
}

/*
================
TestEasteu20HuntCompletesIntoTheSummonOffer
================
*/
func TestEasteu20HuntCompletesIntoTheSummonOffer(t *testing.T) {
	w := newEasteu21World(t)
	t.Setenv(progression.EnvBetaGrowth, "0")
	c := easteuCharacter(w, "LionHunter", false)
	if _, err := w.rt.StartQuest(c, easteu20QuestCodename); err != nil {
		t.Fatal(err)
	}
	holdItems(t, w.rt, c, easteu20ItemCodename, 40)
	before := *c.Experience
	if _, err := w.rt.AdvanceNpcQuest(c, easteu20QuestCodename, easteu21WitchCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(c, w.def20.RefID) {
		t.Fatal("the lion hunt did not complete")
	}
	if got := *c.Experience - before; got != 60000 {
		t.Fatalf("hunt paid %d EXP, want 60000", got)
	}
	// The summon is offered only in shared mode: solo sees the
	// come-back line and no offer row.
	w.facts = InstancePartyFacts{Found: true}
	rows := w.rt.OptionsForNpc(c, easteu21WitchCodename)
	for _, row := range rows {
		if row.Codename == easteu21QuestCodename && !row.Informational {
			t.Fatal("the summon was offered outside a shared party")
		}
	}
	found := false
	for _, row := range rows {
		if row.Codename == easteu21QuestCodename && row.PromptSymbol == easteu21SymNoPartyMode && row.Informational {
			found = true
		}
	}
	if !found {
		t.Fatal("the Witch never spoke the come-back-with-a-party line")
	}
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	row, offered := npcRow(w.rt, c, easteu21QuestCodename, easteu21WitchCodename)
	if !offered || row.PromptSymbol != easteu21SymOffer {
		t.Fatalf("the shared party was offered %+v (offered %v)", row, offered)
	}
}

/*
================
TestEasteu21AcceptanceCallAndDailyRegrant
================
*/
func TestEasteu21AcceptanceCallAndDailyRegrant(t *testing.T) {
	w := newEasteu21World(t)
	*w.day = 10
	c := easteu21Accept(t, w, "Summoner")
	if captureItemCount(c, easteu21CallCodename) != 1 {
		t.Fatal("acceptance did not hand over the Call of Hell")
	}
	// Holding the Call, the Witch shows the plain not-achieved row.
	loseCall(c)
	// Losing it the day it was granted: no re-grant row today.
	if _, offered := npcRow(w.rt, c, easteu21GrantPrefix+easteu21QuestCodename, easteu21WitchCodename); offered {
		t.Fatal("the Call was re-granted on its grant day")
	}
	if row, offered := npcRow(w.rt, c, easteu21QuestCodename, easteu21WitchCodename); !offered || row.PromptSymbol != easteu21SymNotAchieved {
		t.Fatalf("the plain not-achieved row was %+v (offered %v)", row, offered)
	}
	// Tomorrow the re-grant row is there, and choosing it restores
	// the Call with the where-to-use-it banner.
	*w.day = 11
	row, offered := npcRow(w.rt, c, easteu21GrantPrefix+easteu21QuestCodename, easteu21WitchCodename)
	if !offered || row.PromptSymbol != easteu21SymRegrant {
		t.Fatalf("the re-grant row was %+v (offered %v)", row, offered)
	}
	if _, err := w.rt.AdvanceNpcQuest(c, easteu21GrantPrefix+easteu21QuestCodename, easteu21WitchCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, easteu21CallCodename) != 1 {
		t.Fatal("the re-grant did not restore the Call")
	}
	if _, offered := npcRow(w.rt, c, easteu21GrantPrefix+easteu21QuestCodename, easteu21WitchCodename); offered {
		t.Fatal("a second re-grant was offered the same day")
	}
}

/*
================
TestEasteu21CallUseGatesAndInvitation
================
*/
func TestEasteu21CallUseGatesAndInvitation(t *testing.T) {
	w := newEasteu21World(t)
	leader := easteu21Accept(t, w, "PlazaLeader")
	far := simulation.Spawn{RegionID: 24838, X: 0, Y: 0, Z: 0}
	// Not in a shared party: the party-mode line, no invitation.
	w.facts = InstancePartyFacts{Found: true}
	frames, admitted := w.rt.BeginItemUse(leader, easteu21CallCodename, easteu21PlazaSpawn, *w.nowMs)
	if admitted || !hasNotice(frames, easteu21SymNotShared) || len(w.invites) != 0 {
		t.Fatalf("solo use: admitted %v, invites %d", admitted, len(w.invites))
	}
	// In one but not its leader: the leadership line.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Shared: true}
	frames, _ = w.rt.BeginItemUse(leader, easteu21CallCodename, easteu21PlazaSpawn, *w.nowMs)
	if !hasNotice(frames, easteu21SymNotLeader) || len(w.invites) != 0 {
		t.Fatal("a member's use was not refused as a non-leader")
	}
	// The leader away from the plaza: the place line.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	frames, _ = w.rt.BeginItemUse(leader, easteu21CallCodename, far, *w.nowMs)
	if !hasNotice(frames, easteu21SymWrongPlace) || len(w.invites) != 0 {
		t.Fatal("a distant use was not refused for its place")
	}
	// The leader with a trade transport out: the transport line.
	w.facts.Transported = true
	frames, _ = w.rt.BeginItemUse(leader, easteu21CallCodename, easteu21PlazaSpawn, *w.nowMs)
	if !hasNotice(frames, easteu21SymTransport) || len(w.invites) != 0 {
		t.Fatal("a transported use was not refused")
	}
	// At the plaza, leading a shared party: the invitation opens,
	// the Call is kept, and the call carries the native lifetime.
	w.facts.Transported = false
	if _, admitted := w.rt.BeginItemUse(leader, easteu21CallCodename, easteu21PlazaSpawn, *w.nowMs); admitted {
		t.Fatal("the Call was consumed by its use")
	}
	if len(w.invites) != 1 {
		t.Fatalf("invites = %d, want 1", len(w.invites))
	}
	call := w.invites[0]
	if call.Definition != easteu21Definition || call.Entry != easteu21PlazaSpawn ||
		call.DeadlineMs-*w.nowMs != easteu21InviteLifeMs {
		t.Fatalf("invite = %+v", call)
	}
	if captureItemCount(leader, easteu21CallCodename) != 1 {
		t.Fatal("the Call left the leader's bag")
	}
	if !call.Eligible(leader) {
		t.Fatal("the leader is not eligible for their own summon")
	}
	helper := easteuCharacter(w, "CallHolder", true)
	holdItems(t, w.rt, helper, easteu21CallCodename, 1)
	if !call.Eligible(helper) {
		t.Fatal("a Call holder without the quest was refused")
	}
	stranger := easteuCharacter(w, "Stranger", false)
	if call.Eligible(stranger) {
		t.Fatal("a character with neither the quest nor a Call was invited")
	}
}

/*
================
TestEasteu21BattleClockAndTimeout

The stage marks are stated as literals on purpose: they are the
native reading (the encounter updater's own stores), not values
derived from the constants under test, so a wrong constant fails
here instead of drifting with the test. The room stands empty for
30,000 ms, Kerberos appears at t+30,000, two silent seconds, the
challenge at t+32,000, the battle at t+37,000, then battle-clock
marks of 600,000 / 900,000 / 1,200,000 / 1,210,000 counted from
the battle's opening.
================
*/
func TestEasteu21BattleClockAndTimeout(t *testing.T) {
	w := newEasteu21World(t)
	leader := easteu21Accept(t, w, "ClockLeader")
	member := easteu21Accept(t, w, "ClockMember")
	w.openLayer(leader.Name, member.Name)
	start := *w.nowMs
	// The room stands empty through its thirty-second summon phase.
	w.update()
	if len(w.spawned) != 0 {
		t.Fatalf("Kerberos spawned during the summon phase: %v", w.spawned)
	}
	*w.nowMs = start + 29_999
	w.update()
	if len(w.spawned) != 0 {
		t.Fatalf("Kerberos spawned during the summon phase: %v", w.spawned)
	}
	*w.nowMs = start + 30_000
	w.update()
	if len(w.spawned) != 1 || w.spawned[0].Codename != easteu21BossCodename {
		t.Fatalf("spawned = %v", w.spawned)
	}
	if hasNotice(w.pushed[leader.Name], easteu21SymBossShout) {
		t.Fatal("Kerberos spoke during its silent beat")
	}
	*w.nowMs = start + 31_999
	w.update()
	if hasNotice(w.pushed[leader.Name], easteu21SymBossShout) {
		t.Fatal("Kerberos spoke during its silent beat")
	}
	*w.nowMs = start + 32_000
	w.update()
	if !hasNotice(w.pushed[leader.Name], easteu21SymBossShout) {
		t.Fatal("Kerberos never spoke its challenge")
	}
	// Five seconds of challenge before the battle opens.
	*w.nowMs = start + 37_000 - 1
	w.update()
	if hasNotice(w.pushed[leader.Name], easteu21SymBattleBegun) {
		t.Fatal("the battle opened during the challenge")
	}
	*w.nowMs = start + 37_000
	w.update()
	if !hasNotice(w.pushed[leader.Name], easteu21SymBattleBegun) {
		t.Fatal("the battle never opened")
	}
	battle := start + 37_000
	*w.nowMs = battle + 600_000
	w.update()
	if !hasNotice(w.pushed[member.Name], easteu21SymWarn10) {
		t.Fatal("the ten-minute warning never landed")
	}
	*w.nowMs = battle + 900_000
	w.update()
	if !hasNotice(w.pushed[member.Name], easteu21SymWarn5) {
		t.Fatal("the five-minute warning never landed")
	}
	*w.nowMs = battle + 1_200_000
	w.update()
	if !hasNotice(w.pushed[member.Name], easteu21SymBossLeaves) {
		t.Fatal("Kerberos never left")
	}
	if len(w.closed) != 0 {
		t.Fatal("the layer closed when the boss left")
	}
	// One millisecond before the layer's own mark the room still
	// stands: the end is the layer's own mark, not the boss's
	// leaving, and not a millisecond earlier.
	*w.nowMs = battle + 1_209_999
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("the layer ended before its own clock ran out")
	}
	// The layer's own end returns everyone and closes behind them.
	*w.nowMs = battle + 1_210_000
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 2 {
		t.Fatalf("closed %d returned %d, want 1 and 2", len(w.closed), len(w.returned))
	}
	// A kill after the boss has left earns nothing.
	packed := uint32(instance.Pack(easteu21Definition, 3))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteEasteu21Kill(leader, easteu21BossCodename)
	if at := activeQuestIndex(leader, w.def.RefID); at < 0 || objectiveMet(leader, w.def, leader.ActiveQuests[at]) {
		t.Fatal("a late kill credited the quest")
	}
}

/*
================
TestEasteu21PartyChangedExpels

A resident whose party standing breaks mid-summon hears the
instance-condition line and is sent out; anyone else in the room
is untouched, and a name that is not inside changes nothing.
================
*/
func TestEasteu21PartyChangedExpels(t *testing.T) {
	w := newEasteu21World(t)
	leader := easteu21Accept(t, w, "PartyLeader")
	member := easteu21Accept(t, w, "PartyMember")
	w.openLayer(leader.Name, member.Name)
	changed := func(name string) {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventPartyChanged, Division: easteu21TestDivision, Definition: easteu21Definition, World: w.lease.ID, Name: name})
	}
	changed("Stranger")
	if len(w.returned) != 0 {
		t.Fatalf("returned = %v, want none", w.returned)
	}
	changed(member.Name)
	if !hasNotice(w.pushed[member.Name], easteu21SymCondition) {
		t.Fatal("the member never heard the instance-condition line")
	}
	if len(w.returned) != 1 || w.returned[0] != member.Name {
		t.Fatalf("returned = %v, want the member", w.returned)
	}
	if hasNotice(w.pushed[leader.Name], easteu21SymCondition) {
		t.Fatal("the leader heard the condition line")
	}
}

/*
================
TestEasteu21KillCreditAndGrace
================
*/
func TestEasteu21KillCreditAndGrace(t *testing.T) {
	w := newEasteu21World(t)
	t.Setenv(progression.EnvBetaGrowth, "0")
	leader := easteu21Accept(t, w, "KillLeader")
	member := easteu21Accept(t, w, "KillMember")
	w.openLayer(leader.Name, member.Name)
	w.update()
	// A kill marked in another world, or of something else, does
	// nothing here.
	wrong := uint32(instance.Pack(instance.DefinitionID(10), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteEasteu21Kill(leader, easteu21BossCodename)
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("a kill in another world ended the summon")
	}
	if hasNotice(w.pushed[member.Name], easteu21SymBossDeath) {
		t.Fatal("a kill in another world spoke the death line")
	}
	if at := activeQuestIndex(leader, w.def.RefID); at >= 0 && objectiveMet(leader, w.def, leader.ActiveQuests[at]) {
		t.Fatal("a kill in another world credited the quest")
	}
	packed := uint32(instance.Pack(easteu21Definition, 3))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteEasteu21Kill(leader, "MOB_QT_01_SOMETHING_ELSE")
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("another monster's fall ended the summon")
	}
	if hasNotice(w.pushed[member.Name], easteu21SymBossDeath) {
		t.Fatal("another monster's fall spoke the death line")
	}
	if at := activeQuestIndex(leader, w.def.RefID); at >= 0 && objectiveMet(leader, w.def, leader.ActiveQuests[at]) {
		t.Fatal("another monster's fall credited the quest")
	}
	// Kerberos falls: the room hears its death line, both hunters are
	// credited and disarmed of their Calls.
	w.rt.noteEasteu21Kill(leader, easteu21BossCodename)
	w.update()
	if !hasNotice(w.pushed[member.Name], easteu21SymBossDeath) {
		t.Fatal("the death line never landed")
	}
	for _, c := range []*enterworld.Character{leader, member} {
		at := activeQuestIndex(c, w.def.RefID)
		if at < 0 || !objectiveMet(c, w.def, c.ActiveQuests[at]) {
			t.Fatalf("%s was not credited with the kill", c.Name)
		}
		if captureItemCount(c, easteu21CallCodename) != 0 {
			t.Fatalf("%s kept a Call after the kill", c.Name)
		}
	}
	// The grace minute: still open, then returned and closed.
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("the layer closed before its grace ran out")
	}
	*w.nowMs += easteu21KillGraceMs
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 2 {
		t.Fatalf("closed %d returned %d, want 1 and 2", len(w.closed), len(w.returned))
	}
	// Back at the Witch the kill reports and pays.
	before := *leader.Experience
	if _, err := w.rt.AdvanceNpcQuest(leader, easteu21QuestCodename, easteu21WitchCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(leader, w.def.RefID) {
		t.Fatal("the summon did not complete at the Witch")
	}
	if got := *leader.Experience - before; got != 28300 {
		t.Fatalf("the Witch paid %d EXP, want 28300", got)
	}
}

/*
================
TestEasteu21ExitGateRisesBesideTheBoss

The reference kill handler raises the exit gate beside Kerberos (#1878):
the site its owner reports for the fall in this layer is where the gate
rises, once, on the kill; a site in another world or of another monster
is ignored. The room stays for the sixty-second grace, then the layer's
end returns it and takes the gate down.
================
*/
func TestEasteu21ExitGateRisesBesideTheBoss(t *testing.T) {
	w := newEasteu21World(t)
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
	leader := easteu21Accept(t, w, "GateLeader")
	w.openLayer(leader.Name)
	w.update()

	site := simulation.Spawn{RegionID: 25000, X: 120, Y: 4, Z: -30}
	wrong := uint32(instance.Pack(instance.DefinitionID(2), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, easteu21BossCodename, simulation.Spawn{RegionID: 1})
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, "MOB_QT_01_SOMETHING_ELSE", simulation.Spawn{RegionID: 2})
	w.rt.NoteKillSite(leader, easteu21BossCodename, site)
	w.rt.KillUpdater()(leader, easteu21BossCodename, 0)
	w.rt.NoteKillSite(leader, easteu21BossCodename, simulation.Spawn{RegionID: 3})
	w.update()
	if len(gates) != 1 || gates[0].world != w.lease.ID || gates[0].fell != site {
		t.Fatalf("gates raised = %+v, want one beside %+v in %v", gates, site, w.lease.ID)
	}
	*w.nowMs += easteu21KillGraceMs - 1
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
TestEasteu21LastLeaveTakesTheGateDown

The room may leave through the gate before the grace runs out: the
last resident's leave closes the layer, and the gate goes with it.
================
*/
func TestEasteu21LastLeaveTakesTheGateDown(t *testing.T) {
	w := newEasteu21World(t)
	var removed []instance.ID
	w.rt.SpawnExitGate = func(instance.ID, simulation.Spawn) bool { return true }
	w.rt.RemoveExitGates = func(world instance.ID) { removed = append(removed, world) }
	leader := easteu21Accept(t, w, "GateLeader")
	w.openLayer(leader.Name)
	w.update()
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, easteu21BossCodename, simulation.Spawn{RegionID: 25000})
	w.rt.KillUpdater()(leader, easteu21BossCodename, 0)
	w.update()
	for _, name := range []string{"GateLeader"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: easteu21TestDivision, Definition: easteu21Definition, World: w.lease.ID, Name: name})
	}
	if len(removed) != 1 || removed[0] != w.lease.ID || len(w.closed) != 1 {
		t.Fatalf("gates removed %+v closed %v, want the layer's on the last leave", removed, w.closed)
	}
}
