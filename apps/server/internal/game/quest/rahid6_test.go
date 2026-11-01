/*
===========================================================================

rahid6_test.go - Rahid 6: Shiphr's summons, the gates and the Shaitan clock

Exercise the shipped QNO_RM_OLDWOMAN_6 on the instanced-world framework
with the real catalogue and inventory planner: the offer behind Rahid 5,
the acceptance bundle, the five travel gates in native order, the once
-a-day stamp, the 30 s summon and five-line confrontation, the battle
warnings and farewell, and the party-wide kill credit (bundle traded for
the necklace, the bundle-less warned, the finished helper untouched).

The doors (invitation, party facts, returns, frame pushes, spawn) and
the programme clock are stubs; definitions, inventory and completion
are the production ones.

===========================================================================
*/
package quest

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/calendar"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const rahid6TestDivision = "rahid6-test"

// rahid6World is one fixture: the party door knobs (facts), captured door
// outcomes (invites, pushes, spawns, returns, closes) and both clocks.
type rahid6World struct {
	rt       *Runtime
	def      *Definition
	deps     *enterworld.Deps
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

/*
================
newRahid6World

A runtime on the real catalogues with the instanced-world doors stubbed
and a roster of characters in one division.
================
*/
func newRahid6World(t *testing.T) *rahid6World {
	t.Helper()
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
	w := &rahid6World{rt: rt, deps: deps, division: division, pushed: make(map[string][]wire.Frame)}
	w.day = new(uint16)
	w.nowMs = new(int64)
	*w.nowMs = 1_000_000
	w.lease = instance.Lease{ID: instance.Pack(rahid6Definition, 3), Generation: 1}
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
	def, exists := rt.Defs.ByCodename(rahid6QuestCodename)
	if !exists {
		t.Fatal("Rahid 6 is not loaded")
	}
	w.def = def
	rt.RegisterRahid6Programme()
	return w
}

/*
================
rahid6Character

One fixture character: finished Rahid 5 (and, for a helper, Rahid 6).
================
*/
func rahid6Character(w *rahid6World, name string, completed bool) *enterworld.Character {
	c := questCharacter()
	c.Name = name
	*c.Level = 100
	experience := int64(0)
	c.Experience = &experience
	c.CompletedQuestIds = append([]uint32(nil), w.def.RequiredQuestIDs...)
	if completed {
		c.CompletedQuestIds = append(c.CompletedQuestIds, w.def.RefID)
	}
	w.division[rahid6TestDivision] = append(w.division[rahid6TestDivision], c)
	return c
}

/*
================
rahid6Accept

A fixture character that has accepted Rahid 6 (and holds the bundle).
================
*/
func rahid6Accept(t *testing.T, w *rahid6World, name string) *enterworld.Character {
	t.Helper()
	c := rahid6Character(w, name, false)
	if _, err := w.rt.StartQuest(c, rahid6QuestCodename); err != nil {
		t.Fatal(err)
	}
	return c
}

/*
================
rahid6SetupLayer

The layer opens with summoner and member inside.
================
*/
func (w *rahid6World) openLayer(leader, member *enterworld.Character) {
	w.rt.rahid6.rememberLease(w.lease, leader.Name)
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventSetup, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID})
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: leader.Name})
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: member.Name})
}

/*
================
rahid6Update

One programme tick at the fixture clock.
================
*/
func (w *rahid6World) update() {
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventUpdate, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID})
}

/*
================
refusalSymbol

The dialogue symbol an error carries, if any.
================
*/
func refusalSymbol(err error) string {
	var refusal *dialogueRefusal
	if errors.As(err, &refusal) {
		return refusal.DialogueSymbol()
	}
	return ""
}

/*
================
TestRahid6SpecLoadsAndOfferRequiresRahid5

The curated spec: collect one necklace at Shiphr behind Rahid 5, one
completion, 2,573,000 EXP. Acceptance hands over the bundle; without
Rahid 5 the offer refuses.
================
*/
func TestRahid6SpecLoadsAndOfferRequiresRahid5(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	def := w.def
	if def.Objective != ObjectiveCollect || def.CollectItemCodename != rahid6NecklaceCodename || def.CollectCount != 1 ||
		def.MaxCompletions != 1 || def.StartNpcCodename != rahid6ShiphrCodename || def.EndNpcCodename != rahid6ShiphrCodename ||
		def.RewardExp != 2573000 {
		t.Fatalf("spec %+v", def)
	}
	if len(def.RequiredQuestIDs) == 0 {
		t.Fatal("spec carries no Rahid 5 prerequisite")
	}
	c := questCharacter()
	c.Name = "NoPredecessor"
	if _, err := w.rt.StartQuest(c, rahid6QuestCodename); err == nil {
		t.Fatal("Rahid 6 accepted without Rahid 5")
	}
	leader := rahid6Accept(t, w, "RahidLeader")
	if captureItemCount(leader, rahid6BundleCodename) != 1 {
		t.Fatal("acceptance omitted the Rainbow Grass Bundle")
	}
}

/*
================
TestRahid6AcceptanceRefusedOnFullBag

A bundle that cannot be placed refuses acceptance whole - the quest is
not left active without its fiction item.
================
*/
func TestRahid6AcceptanceRefusedOnFullBag(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	w.rt.PlanInventory = func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
		return nil, nil, errors.New("bag full")
	}
	c := rahid6Character(w, "FullBag", false)
	if _, err := w.rt.StartQuest(c, rahid6QuestCodename); err == nil {
		t.Fatal("acceptance granted with an unplaceable bundle")
	}
	if activeQuestIndex(c, w.def.RefID) >= 0 {
		t.Fatal("a refused acceptance left the quest active")
	}
}

/*
================
TestRahid6TravelGatesInNativeOrder

Shiphr's "Meet Shaitan" row offered while active; each gate answers its
own line in the native order; a good party opens the meeting with the
native deadline, stamps the day, and a second summon answers the gate
for the rest of the day.
================
*/
func TestRahid6TravelGatesInNativeOrder(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	code := rahid6TravelPrefix + rahid6QuestCodename
	var row *NpcOption
	for i := range w.rt.OptionsForNpc(leader, rahid6ShiphrCodename) {
		option := w.rt.OptionsForNpc(leader, rahid6ShiphrCodename)[i]
		if option.Codename == code {
			row = &option
		}
	}
	if row == nil || row.TitleSymbol != rahid6SymTravelTitle || !row.Complete || row.PromptSymbol != "SN_TALK_QNO_RM_OLDWOMAN_6_07" {
		t.Fatalf("travel row %+v", row)
	}
	gates := []struct {
		name  string
		facts InstancePartyFacts
		want  string
	}{
		{"no party", InstancePartyFacts{Found: true}, rahid6SymNoParty},
		{"not leader", InstancePartyFacts{Found: true, InParty: true}, rahid6SymNoParty},
		{"transport", InstancePartyFacts{Found: true, InParty: true, Leader: true, Transported: true, Shared: true}, rahid6SymTransport},
		{"unshared", InstancePartyFacts{Found: true, InParty: true, Leader: true}, rahid6SymNotShared},
	}
	for _, gate := range gates {
		w.facts = gate.facts
		if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); refusalSymbol(err) != gate.want {
			t.Fatalf("%s: err %v, want %s", gate.name, err, gate.want)
		}
	}
	if len(w.invites) != 0 {
		t.Fatal("a refused gate opened a meeting")
	}
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if len(w.invites) != 1 || w.invites[0].Definition != rahid6Definition || w.invites[0].Entry != rahid6EntrySpawn ||
		w.invites[0].DeadlineMs != *w.nowMs+rahid6LayerLifetimeMs || w.invites[0].Eligible == nil {
		t.Fatalf("invites %+v", w.invites)
	}
	if !w.invites[0].Eligible(leader) {
		t.Fatal("an active leader is not eligible for his own meeting")
	}
	// Same day: the once-a-day line answers before anything else opens.
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); refusalSymbol(err) != rahid6SymSummonedToday {
		t.Fatalf("same-day err %v, want %s", err, rahid6SymSummonedToday)
	}
	// Next day, with the meeting still open: the busy line.
	*w.day++
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); refusalSymbol(err) != rahid6SymBusy {
		t.Fatalf("next-day err %v, want %s", err, rahid6SymBusy)
	}
	// The room closes (last resident leaves); the next day opens again.
	w.openLayer(leader, rahid6Accept(t, w, "RahidMember"))
	for _, name := range []string{"RahidLeader", "RahidMember"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: name})
	}
	if len(w.closed) != 1 || w.closed[0] != w.lease {
		t.Fatalf("closed %v, want the lease", w.closed)
	}
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); err != nil {
		t.Fatalf("next day after close: %v", err)
	}
	if len(w.invites) != 2 {
		t.Fatalf("invites %d, want 2", len(w.invites))
	}
}

/*
================
TestRahid6SummonStampsTheDayWhenTheDoorRefuses

The daily stamp rides one character write; if that door refuses (the
leader stopped being authoritative mid-invitation), the day is held
in the programme's own record instead, and the once-a-day gate still
answers after the meeting ends.
================
*/
func TestRahid6SummonStampsTheDayWhenTheDoorRefuses(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	member := rahid6Accept(t, w, "RahidMember")
	w.deps.UpdateCharacter = func(_ *enterworld.Character, label string, update func() bool) bool {
		if label == "rahid6-summon" {
			return false
		}
		return update()
	}
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	code := rahid6TravelPrefix + rahid6QuestCodename
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if _, stamped := leader.QuestSupplies[w.def.RefID]; stamped {
		t.Fatal("the refused stamp still landed in the persisted supplies")
	}
	w.openLayer(leader, member)
	for _, name := range []string{"RahidLeader", "RahidMember"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: name})
	}
	if len(w.closed) != 1 {
		t.Fatalf("closed %v, want the meeting's layer", w.closed)
	}
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); refusalSymbol(err) != rahid6SymSummonedToday {
		t.Fatalf("same-day err %v, want %s", err, rahid6SymSummonedToday)
	}
	if len(w.invites) != 1 {
		t.Fatalf("invites %d, want 1", len(w.invites))
	}
}

/*
================
TestRahid6TravelRegrantsALostBundle

A leader who lost the bundle is handed another before departing; a
leader already holding the objective is handed nothing.
================
*/
func TestRahid6TravelRegrantsALostBundle(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	leader.MissionInventory = nil
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	if _, err := w.rt.AdvanceNpcQuest(leader, rahid6TravelPrefix+rahid6QuestCodename, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(leader, rahid6BundleCodename) != 1 {
		t.Fatal("the lost bundle was not replaced")
	}
	// The meeting runs its course and the room closes behind the party.
	member := rahid6Accept(t, w, "RahidMember")
	w.openLayer(leader, member)
	for _, name := range []string{"RahidLeader", "RahidMember"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: name})
	}
	if len(w.closed) != 1 {
		t.Fatalf("closed %v, want the meeting's layer", w.closed)
	}
	leader.MissionInventory = nil
	holdItems(t, w.rt, leader, rahid6NecklaceCodename, 1)
	*w.day++
	if _, err := w.rt.AdvanceNpcQuest(leader, rahid6TravelPrefix+rahid6QuestCodename, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(leader, rahid6BundleCodename) != 0 {
		t.Fatal("a necklace holder was handed a bundle")
	}
}

/*
================
TestRahid6Timeline

The encounter clock: Tahomet at 30 s in the layer population at radius
20-100, the five-line confrontation at 5 s steps, the battle-begun
line, warnings at 10 and 5 minutes left, then the farewell returns
everyone and closes the layer.
================
*/
func TestRahid6Timeline(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	member := rahid6Accept(t, w, "RahidMember")
	w.openLayer(leader, member)
	start := *w.nowMs
	*w.nowMs = start + rahid6SpawnDelayMs - 1
	w.update()
	if len(w.spawned) != 0 {
		t.Fatal("Tahomet spawned before 30 s")
	}
	*w.nowMs = start + rahid6SpawnDelayMs
	w.update()
	if len(w.spawned) != 1 {
		t.Fatalf("spawned %v", w.spawned)
	}
	spawn := w.spawned[0]
	if spawn.Codename != rahid6TahometCodename || spawn.Population != w.lease || spawn.RadiusMin != rahid6SpawnRadiusMin ||
		spawn.RadiusSpan != rahid6SpawnRadiusSpan || spawn.Division != rahid6TestDivision {
		t.Fatalf("spawn request %+v", spawn)
	}
	spawnedAt := *w.nowMs
	for i, symbol := range rahid6ConvoLines {
		*w.nowMs = spawnedAt + int64(i+1)*rahid6ConvoStepMs
		beforeLeader := len(w.pushed["RahidLeader"])
		beforeMember := len(w.pushed["RahidMember"])
		w.update()
		if !hasNotice(w.pushed["RahidLeader"][beforeLeader:], symbol) || !hasNotice(w.pushed["RahidMember"][beforeMember:], symbol) {
			t.Fatalf("line %d (%s) not spoken to the room", i, symbol)
		}
	}
	if !hasNotice(w.pushed["RahidLeader"], rahid6SymBattleBegun) {
		t.Fatal("the battle-begun line never arrived")
	}
	battleAt := *w.nowMs
	*w.nowMs = battleAt + rahid6BattleMs
	w.update()
	if !hasNotice(w.pushed["RahidLeader"], rahid6SymWarn10) {
		t.Fatal("the 10-minute warning never arrived")
	}
	*w.nowMs = battleAt + rahid6BattleMs + rahid6WarningStepMs
	w.update()
	if !hasNotice(w.pushed["RahidLeader"], rahid6SymWarn5) {
		t.Fatal("the 5-minute warning never arrived")
	}
	*w.nowMs = battleAt + rahid6BattleMs + 2*rahid6WarningStepMs
	w.update()
	if !hasNotice(w.pushed["RahidLeader"], rahid6SymFarewell) {
		t.Fatal("the farewell never arrived")
	}
	if len(w.returned) != 2 || len(w.closed) != 1 || w.closed[0] != w.lease {
		t.Fatalf("returned %v closed %v, want both residents home and the layer closed", w.returned, w.closed)
	}
	if w.rt.rahid6.summonedBy("RahidLeader") {
		t.Fatal("the ended meeting still answers the busy gate")
	}
}

/*
================
TestRahid6TimelineEndsInsideItsOwnDeadline

The timeline composed with the deadline the invitation stamps: the
farewell lands and the programme closes the layer itself before the
lane's hard end can evacuate anyone. (The clock and the deadline were
once composed blind: the farewell sat past the hard end and the lane
evacuated the party without it.)
================
*/
func TestRahid6TimelineEndsInsideItsOwnDeadline(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	member := rahid6Accept(t, w, "RahidMember")
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	code := rahid6TravelPrefix + rahid6QuestCodename
	if _, err := w.rt.AdvanceNpcQuest(leader, code, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if len(w.invites) != 1 {
		t.Fatalf("invites %v", w.invites)
	}
	deadline := w.invites[0].DeadlineMs
	w.openLayer(leader, member)
	for *w.nowMs < deadline && len(w.closed) == 0 {
		*w.nowMs += rahid6ConvoStepMs
		w.update()
	}
	if !hasNotice(w.pushed["RahidLeader"], rahid6SymFarewell) {
		t.Fatal("the farewell never arrived before the layer deadline")
	}
	if len(w.closed) != 1 || w.closed[0] != w.lease {
		t.Fatalf("closed %v, want the programme to end its own layer before the deadline", w.closed)
	}
	if *w.nowMs >= deadline {
		t.Fatalf("the layer ended at %d, deadline %d", *w.nowMs, deadline)
	}
}

/*
================
TestRahid6KillCrediting

Tahomet falls: the room hears the surrender; every resident doing the
quest trades all bundles for the necklace (and hears the obtained
line), the bundle-less one hears the bundle-lost line, the finished
helper is untouched. A minute later the meeting returns everyone and
closes.
================
*/
func TestRahid6KillCrediting(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	holdItems(t, w.rt, leader, rahid6BundleCodename, 1) // two bundles held
	member := rahid6Accept(t, w, "RahidMember")
	member.MissionInventory = nil // lost the bundle on the road
	helper := rahid6Character(w, "RahidHelper", true)
	w.openLayer(leader, member)
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: helper.Name})
	// A kill outside world 10, and a kill of something else, mark nothing.
	packed := uint32(instance.Pack(instance.DefinitionID(9), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteRahid6Kill(leader, rahid6TahometCodename)
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("a kill in another world ended the meeting")
	}
	packed = uint32(instance.Pack(rahid6Definition, 3))
	w.rt.noteRahid6Kill(leader, "MOB_QT_01_SOMETHING_ELSE")
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("another monster's fall ended the meeting")
	}
	w.rt.noteRahid6Kill(leader, rahid6TahometCodename)
	w.update()
	if captureItemCount(leader, rahid6BundleCodename) != 0 || captureItemCount(leader, rahid6NecklaceCodename) != 1 {
		t.Fatalf("leader holds bundles %d necklace %d, want 0 and 1", captureItemCount(leader, rahid6BundleCodename), captureItemCount(leader, rahid6NecklaceCodename))
	}
	if !hasNotice(w.pushed["RahidLeader"], "SN_TALK_QNO_RM_OLDWOMAN_6_21") {
		t.Fatal("the necklace holder never heard the obtained line")
	}
	if !hasNotice(w.pushed["RahidMember"], rahid6SymBundleLost) {
		t.Fatal("the bundle-less resident never heard why")
	}
	if captureItemCount(helper, rahid6NecklaceCodename) != 0 || hasNotice(w.pushed["RahidHelper"], rahid6SymBundleLost) {
		t.Fatal("the finished helper was credited or warned")
	}
	for _, name := range []string{"RahidLeader", "RahidMember", "RahidHelper"} {
		if !hasNotice(w.pushed[name], rahid6SymSurrender) {
			t.Fatalf("%s never heard the surrender", name)
		}
	}
	// The grace minute: still open, then everyone home.
	w.update()
	if len(w.returned) != 0 || len(w.closed) != 0 {
		t.Fatalf("returned %v closed %v before the grace ran out", w.returned, w.closed)
	}
	*w.nowMs += rahid6KillGraceMs
	w.update()
	if len(w.returned) != 3 || len(w.closed) != 1 {
		t.Fatalf("returned %v closed %v, want everyone home and one close", w.returned, w.closed)
	}
}

/*
================
TestRahid6ActivatedExpelsTheUninvited

A resident neither doing nor having finished the quest hears the
instance-condition line and is returned; an eligible member is left
alone. Completion at Shiphr then takes the necklace and pays.
================
*/
func TestRahid6ActivatedExpelsTheUninvited(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	leader := rahid6Accept(t, w, "RahidLeader")
	guest := rahid6Character(w, "RahidGuest", false)
	// The guest never accepted: strip even the Rahid 5 prerequisite.
	guest.CompletedQuestIds = nil
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventActivated, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: guest.Name})
	if !hasNotice(w.pushed["RahidGuest"], rahid6SymCondition) || len(w.returned) != 1 || w.returned[0] != "RahidGuest" {
		t.Fatalf("guest: pushed %v returned %v", w.pushed["RahidGuest"], w.returned)
	}
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventActivated, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: leader.Name})
	if len(w.returned) != 1 || hasNotice(w.pushed["RahidLeader"], rahid6SymCondition) {
		t.Fatal("an eligible resident was expelled")
	}
	// Report to Shiphr with the necklace: completion takes it and pays.
	holdItems(t, w.rt, leader, rahid6NecklaceCodename, 1)
	expBefore := *leader.Experience
	if _, err := w.rt.AdvanceNpcQuest(leader, rahid6QuestCodename, rahid6ShiphrCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(leader, w.def.RefID) || captureItemCount(leader, rahid6NecklaceCodename) != 0 {
		t.Fatal("completion left the necklace or the quest unfinished")
	}
	if *leader.Experience <= expBefore {
		t.Fatalf("experience %d -> %d, want the reward paid", expBefore, *leader.Experience)
	}
}

/*
================
TestRahid6ExitGateRisesBesideTheBoss

The reference kill handler raises the exit gate beside Tahomet (#1878):
the site its owner reports for the fall in this layer is where the gate
rises, once, on the kill; a site in another world or of another monster
is ignored. The room stays for the sixty-second grace, then the layer's
end returns it and takes the gate down.
================
*/
func TestRahid6ExitGateRisesBesideTheBoss(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
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
	leader := rahid6Accept(t, w, "GateLeader")
	w.openLayer(leader, rahid6Accept(t, w, "GateMember"))
	w.update()

	site := simulation.Spawn{RegionID: 25000, X: 120, Y: 4, Z: -30}
	wrong := uint32(instance.Pack(instance.DefinitionID(2), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, rahid6TahometCodename, simulation.Spawn{RegionID: 1})
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, "MOB_QT_01_SOMETHING_ELSE", simulation.Spawn{RegionID: 2})
	w.rt.NoteKillSite(leader, rahid6TahometCodename, site)
	w.rt.KillUpdater()(leader, rahid6TahometCodename, 0)
	w.rt.NoteKillSite(leader, rahid6TahometCodename, simulation.Spawn{RegionID: 3})
	w.update()
	if len(gates) != 1 || gates[0].world != w.lease.ID || gates[0].fell != site {
		t.Fatalf("gates raised = %+v, want one beside %+v in %v", gates, site, w.lease.ID)
	}
	*w.nowMs += rahid6KillGraceMs - 1
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
TestRahid6LastLeaveTakesTheGateDown

The room may leave through the gate before the grace runs out: the
last resident's leave closes the layer, and the gate goes with it.
================
*/
func TestRahid6LastLeaveTakesTheGateDown(t *testing.T) {
	clientdata.RequireGameData(t)
	w := newRahid6World(t)
	var removed []instance.ID
	w.rt.SpawnExitGate = func(instance.ID, simulation.Spawn) bool { return true }
	w.rt.RemoveExitGates = func(world instance.ID) { removed = append(removed, world) }
	leader := rahid6Accept(t, w, "GateLeader")
	w.openLayer(leader, rahid6Accept(t, w, "GateMember"))
	w.update()
	packed := uint32(w.lease.ID)
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, rahid6TahometCodename, simulation.Spawn{RegionID: 25000})
	w.rt.KillUpdater()(leader, rahid6TahometCodename, 0)
	w.update()
	for _, name := range []string{"GateLeader", "GateMember"} {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventLeave, Division: rahid6TestDivision, Definition: rahid6Definition, World: w.lease.ID, Name: name})
	}
	if len(removed) != 1 || removed[0] != w.lease.ID || len(w.closed) != 1 {
		t.Fatalf("gates removed %+v closed %v, want the layer's on the last leave", removed, w.closed)
	}
}
