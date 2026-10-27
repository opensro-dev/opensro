/*
===========================================================================

ivy3_test.go - IVY 3 (Ivy 2): Ratchel's party gate and the fortress

Exercise the shipped Ivy chain tail on the instanced-world framework
with the real catalogue and inventory planner: Ivy 1's completion
unlocking the offer, the shared-party offer gate, acceptance's five
Horn Trumpets and their once-a-day re-grant, the Horn's three areas
and their sentry signs, the Blood's five-percent drop and the key it
assembles, the fortress use gates and invitation, the dialogue and
battle clock (three lines, warnings, Ivy leaving, the layer's end),
and the party-wide kill credit with its sixty-second grace.

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
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

const ivy3TestDivision = "ivy3-test"

// ivy3World is one fixture: the party door knobs (facts), captured
// door outcomes (invites, pushes, spawns, returns, closes) and both
// clocks.
type ivy3World struct {
	rt       *Runtime
	def1     *Definition
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

func newIvy3World(t *testing.T) *ivy3World {
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
	w := &ivy3World{rt: rt, division: division, pushed: make(map[string][]wire.Frame)}
	w.day = new(uint16)
	w.nowMs = new(int64)
	*w.nowMs = 1_000_000
	w.lease = instance.Lease{ID: instance.Pack(ivy3Definition, 3), Generation: 1}
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
	def1, exists := rt.Defs.ByCodename(ivy1QuestCodename)
	if !exists {
		t.Fatal("IVY 1 is not loaded")
	}
	w.def1 = def1
	def, exists := rt.Defs.ByCodename(ivy3QuestCodename)
	if !exists {
		t.Fatal("IVY 3 is not loaded")
	}
	w.def = def
	rt.RegisterIvy3Programme()
	return w
}

// ivy3Character is one fixture character, its Ivy 1 hunt done when
// asked.
func ivy3Character(w *ivy3World, name string, ivy1Done bool) *enterworld.Character {
	c := questCharacter()
	c.Name = name
	c.ModelCodename = "CHAR_EU_WOMAN_NOBLE"
	*c.Level = 100
	experience := int64(0)
	skillExp := int64(0)
	c.Experience, c.SkillExp = &experience, &skillExp
	if ivy1Done {
		c.CompletedQuestIds = append(c.CompletedQuestIds, w.def1.RefID)
	}
	w.division[ivy3TestDivision] = append(w.division[ivy3TestDivision], c)
	return c
}

func ivy3Accept(t *testing.T, w *ivy3World, name string) *enterworld.Character {
	t.Helper()
	c := ivy3Character(w, name, true)
	if _, err := w.rt.StartQuest(c, ivy3QuestCodename); err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *ivy3World) update() {
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventUpdate, Division: ivy3TestDivision, Definition: ivy3Definition, World: w.lease.ID})
}

func (w *ivy3World) openLayer(names ...string) {
	w.rt.ivy3.rememberLease(w.lease, names[0])
	w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventSetup, Division: ivy3TestDivision, Definition: ivy3Definition, World: w.lease.ID})
	for _, name := range names {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventEnter, Division: ivy3TestDivision, Definition: ivy3Definition, World: w.lease.ID, Name: name})
	}
}

func loseItem(c *enterworld.Character, codename string) {
	rows := c.MissionInventory[:0]
	for _, row := range c.MissionInventory {
		if row.Codename != codename {
			rows = append(rows, row)
		}
	}
	c.MissionInventory = rows
}

/*
================
TestIvy3OfferGateAcceptanceAndDailyRegrant
================
*/
func TestIvy3OfferGateAcceptanceAndDailyRegrant(t *testing.T) {
	w := newIvy3World(t)
	*w.day = 10
	// Without Ivy 1 finished Ratchel has nothing: no offer, and no
	// come-back line either - the quest is simply not there.
	stranger := ivy3Character(w, "Stranger", false)
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	for _, row := range w.rt.OptionsForNpc(stranger, ivy3NpcCodename) {
		if row.Codename == ivy3QuestCodename {
			t.Fatal("IVY 3 was offered before Ivy 1 was finished")
		}
	}
	if _, err := w.rt.StartQuest(stranger, ivy3QuestCodename); err == nil {
		t.Fatal("IVY 3 started before Ivy 1 was finished")
	}
	c := ivy3Character(w, "RatchelFriend", true)
	// Solo, Ratchel speaks the come-back-with-a-party line and offers
	// nothing.
	w.facts = InstancePartyFacts{Found: true}
	found := false
	for _, row := range w.rt.OptionsForNpc(c, ivy3NpcCodename) {
		if row.Codename == ivy3QuestCodename && !row.Informational {
			t.Fatal("IVY 3 was offered outside a shared party")
		}
		if row.Codename == ivy3QuestCodename && row.PromptSymbol == ivy3SymNoParty && row.Informational {
			found = true
		}
	}
	if !found {
		t.Fatal("Ratchel never spoke the come-back-with-a-party line")
	}
	// In a shared party the offer stands.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	row, offered := npcRow(w.rt, c, ivy3QuestCodename, ivy3NpcCodename)
	if !offered || row.PromptSymbol != ivy3SymOffer {
		t.Fatalf("the shared party was offered %+v (offered %v)", row, offered)
	}
	// Acceptance hands over five Horn Trumpets.
	if _, err := w.rt.StartQuest(c, ivy3QuestCodename); err != nil {
		t.Fatal(err)
	}
	if captureItemCount(c, ivy3HornCodename) != ivy3HornGrant {
		t.Fatalf("acceptance handed over %d Horns, want %d", captureItemCount(c, ivy3HornCodename), ivy3HornGrant)
	}
	// Holding Horns, Ratchel shows the plain not-achieved row.
	if row, offered := npcRow(w.rt, c, ivy3QuestCodename, ivy3NpcCodename); !offered || row.PromptSymbol != ivy3SymNotAchieved {
		t.Fatalf("the plain not-achieved row was %+v (offered %v)", row, offered)
	}
	// Using them all up the day they were granted: no re-grant, only
	// the come-back-tomorrow line.
	loseItem(c, ivy3HornCodename)
	if row, offered := npcRow(w.rt, c, ivy3QuestCodename, ivy3NpcCodename); !offered || row.PromptSymbol != ivy3SymRegrantWait || !row.Informational {
		t.Fatalf("the grant-day row was %+v (offered %v)", row, offered)
	}
	// Tomorrow the re-grant row is there, and choosing it restores
	// five Horns with the get-the-mark banner.
	*w.day = 11
	row, offered = npcRow(w.rt, c, ivy3GrantPrefix+ivy3QuestCodename, ivy3NpcCodename)
	if !offered || row.PromptSymbol != ivy3SymRegrant || row.TitleSymbol != ivy3SymRegrantRow {
		t.Fatalf("the re-grant row was %+v (offered %v)", row, offered)
	}
	result, err := w.rt.AdvanceNpcQuest(c, ivy3GrantPrefix+ivy3QuestCodename, ivy3NpcCodename)
	if err != nil {
		t.Fatal(err)
	}
	if !hasNotice(result.Frames, ivy3SymGranted) {
		t.Fatal("the re-grant never spoke its banner")
	}
	if captureItemCount(c, ivy3HornCodename) != ivy3HornGrant {
		t.Fatal("the re-grant did not restore the Horns")
	}
	loseItem(c, ivy3HornCodename)
	if row, offered := npcRow(w.rt, c, ivy3QuestCodename, ivy3NpcCodename); !offered || row.PromptSymbol != ivy3SymRegrantWait {
		t.Fatalf("a second grant-day row was %+v (offered %v)", row, offered)
	}
	// Holding the assembled key instead also keeps the plain row.
	holdItems(t, w.rt, c, ivy3KeyCodename, 1)
	*w.day = 12
	if row, offered := npcRow(w.rt, c, ivy3QuestCodename, ivy3NpcCodename); !offered || row.PromptSymbol != ivy3SymNotAchieved {
		t.Fatalf("the key holder's row was %+v (offered %v)", row, offered)
	}
}

/*
================
TestIvy3HornAreasAndSignCredit

The Horn's areas are stated as literals on purpose: region, point
and radius are the native records, so a wrong record fails here
instead of drifting with the test.
================
*/
func TestIvy3HornAreasAndSignCredit(t *testing.T) {
	w := newIvy3World(t)
	// The three native placement records, pinned whole: region,
	// point, radius, sentry and sign, in the native order.
	wantAreas := []ivy3HornArea{
		{point: simulation.Spawn{RegionID: 27237, X: 1057, Y: 218, Z: 634}, radius: 1000,
			monster: "MOB_QT_01_PUNISHER", sign: "ITEM_QNO_EU_IVY_3_04"},
		{point: simulation.Spawn{RegionID: 25954, X: 1661, Y: 180, Z: 210}, radius: 700,
			monster: "MOB_QT_03_PUNISHER_CLON", sign: "ITEM_QNO_EU_IVY_3_03"},
		{point: simulation.Spawn{RegionID: 25187, X: 646, Y: 182, Z: 204}, radius: 600,
			monster: "MOB_QT_01_SOLDIER_CLON", sign: "ITEM_QNO_EU_IVY_3_02"},
	}
	if len(ivy3HornAreas) != len(wantAreas) {
		t.Fatalf("horn areas = %d, want %d", len(ivy3HornAreas), len(wantAreas))
	}
	for i, want := range wantAreas {
		if ivy3HornAreas[i] != want {
			t.Fatalf("horn area %d = %+v, want %+v", i, ivy3HornAreas[i], want)
		}
	}
	leader := ivy3Accept(t, w, "HornLeader")
	carrier := ivy3Accept(t, w, "HornCarrier")
	updater := w.rt.KillUpdater()
	far := simulation.Spawn{RegionID: 27237, X: 0, Y: 0, Z: 0}
	// Solo, the Horn answers its party line and calls nothing.
	w.facts = InstancePartyFacts{Found: true}
	frames, admitted := w.rt.BeginItemUse(leader, ivy3HornCodename, ivy3HornAreas[0].point, *w.nowMs)
	if admitted || !hasNotice(frames, ivy3SymHornNotShared) || len(w.spawned) != 0 {
		t.Fatalf("solo horn: admitted %v, spawned %d", admitted, len(w.spawned))
	}
	// Shared, but nowhere near a marked area: the place line.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	frames, admitted = w.rt.BeginItemUse(leader, ivy3HornCodename, far, *w.nowMs)
	if admitted || !hasNotice(frames, ivy3SymHornWrongPlace) || len(w.spawned) != 0 {
		t.Fatalf("distant horn: admitted %v, spawned %d", admitted, len(w.spawned))
	}
	// At the Amphitheater point the Punisher is called, 20 to 100
	// from the point, standing five minutes; the Horn is spent.
	if _, admitted := w.rt.BeginItemUse(leader, ivy3HornCodename, ivy3HornAreas[0].point, *w.nowMs); !admitted {
		t.Fatal("the Horn was not spent on a successful call")
	}
	want := simulation.QuestMonsterSpawn{
		Codename: "MOB_QT_01_PUNISHER", Position: simulation.Spawn{RegionID: 27237, X: 1057, Y: 218, Z: 634},
		FixedPosition: true, RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 300000, NowMs: *w.nowMs,
	}
	if len(w.spawned) != 1 || w.spawned[0] != want {
		t.Fatalf("spawned = %v, want %+v", w.spawned, want)
	}
	// Its death leaves the Amphitheater sign with the killer.
	updater(leader, "MOB_QT_01_PUNISHER", 0)
	if captureItemCount(leader, "ITEM_QNO_EU_IVY_3_04") != 1 {
		t.Fatal("the sentry's sign never reached the killer")
	}
	// The call is spent: another death of the same codename, with no
	// new Horn behind it, leaves nothing - even for a carrier who
	// holds no sign.
	updater(carrier, "MOB_QT_01_PUNISHER", 0)
	if captureItemCount(carrier, "ITEM_QNO_EU_IVY_3_04") != 0 {
		t.Fatal("an uncalled sentry left a sign")
	}
	// Standing in the area while holding its sign calls nothing.
	frames, admitted = w.rt.BeginItemUse(leader, ivy3HornCodename, ivy3HornAreas[0].point, *w.nowMs)
	if admitted || !hasNotice(frames, ivy3SymHornWrongPlace) || len(w.spawned) != 1 {
		t.Fatalf("signed horn: admitted %v, spawned %d", admitted, len(w.spawned))
	}
	// A call left standing past its five minutes lapses: its death
	// leaves nothing.
	if _, admitted := w.rt.BeginItemUse(leader, ivy3HornCodename, ivy3HornAreas[1].point, *w.nowMs); !admitted {
		t.Fatal("the Cleopatra's Gate Horn was refused")
	}
	*w.nowMs += ivy3SentryLifeMs
	updater(leader, "MOB_QT_03_PUNISHER_CLON", 0)
	if captureItemCount(leader, "ITEM_QNO_EU_IVY_3_03") != 0 {
		t.Fatal("a lapsed sentry left its sign")
	}
	// A key holder may still call the sentry - the Horn's check is
	// the held sign alone - but its death leaves them no sign: the
	// finished key already stands in the sign's place.
	holdItems(t, w.rt, carrier, ivy3KeyCodename, 1)
	if _, admitted := w.rt.BeginItemUse(carrier, ivy3HornCodename, ivy3HornAreas[1].point, *w.nowMs); !admitted {
		t.Fatal("a key holder's Horn was refused")
	}
	updater(carrier, "MOB_QT_03_PUNISHER_CLON", 0)
	if captureItemCount(carrier, "ITEM_QNO_EU_IVY_3_03") != 0 {
		t.Fatal("a key holder took a sign")
	}
}

/*
================
TestIvy3SignSetSpeaksMaterialsReady
================
*/
func TestIvy3SignSetSpeaksMaterialsReady(t *testing.T) {
	w := newIvy3World(t)
	leader := ivy3Accept(t, w, "Collector")
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	holdItems(t, w.rt, leader, ivy3BloodCodename, 1)
	holdItems(t, w.rt, leader, "ITEM_QNO_EU_IVY_3_04", 1)
	holdItems(t, w.rt, leader, "ITEM_QNO_EU_IVY_3_03", 1)
	if _, admitted := w.rt.BeginItemUse(leader, ivy3HornCodename, ivy3HornAreas[2].point, *w.nowMs); !admitted {
		t.Fatal("the Haran's Tower Horn was refused")
	}
	frames, _ := w.rt.KillUpdater()(leader, "MOB_QT_01_SOLDIER_CLON", 0)
	if captureItemCount(leader, "ITEM_QNO_EU_IVY_3_02") != 1 {
		t.Fatal("the last sign never reached the killer")
	}
	if !hasNotice(frames, ivy3SymMaterialsReady) {
		t.Fatal("completing the materials never spoke the materials-ready line")
	}
}

/*
================
TestIvy3BloodDropAndKeyAssembly
================
*/
func TestIvy3BloodDropAndKeyAssembly(t *testing.T) {
	w := newIvy3World(t)
	c := ivy3Accept(t, w, "BloodHunter")
	roll := func(values ...uint32) func() (uint32, error) {
		i := 0
		return func() (uint32, error) {
			value := values[i%len(values)]
			i++
			return value, nil
		}
	}
	// The Amphitheater Punisher bleeds at five percent, granted by
	// the kill handler: a sample inside the rate grants, one
	// outside does not.
	w.rt.CaptureRoll = roll(1, 0)
	w.rt.KillUpdater()(c, ivy3BloodSource, 0)
	if captureItemCount(c, ivy3BloodCodename) != 1 {
		t.Fatal("the in-rate kill granted no Blood")
	}
	dry := ivy3Accept(t, w, "DryHunter")
	w.rt.CaptureRoll = roll(0x7fff, 0x7fff)
	w.rt.KillUpdater()(dry, ivy3BloodSource, 0)
	if captureItemCount(dry, ivy3BloodCodename) != 0 {
		t.Fatal("the out-of-rate kill granted Blood")
	}
	w.rt.CaptureRoll = roll(1, 0)
	w.rt.KillUpdater()(dry, "MOB_AM_SOLDIER", 0)
	if captureItemCount(dry, ivy3BloodCodename) != 0 {
		t.Fatal("another monster granted Blood")
	}
	// One Blood is all the mission leaves: holding one stops the
	// grant, and a hunt already counted grants nothing more.
	w.rt.KillUpdater()(c, ivy3BloodSource, 0)
	if captureItemCount(c, ivy3BloodCodename) != 1 {
		t.Fatal("a second Blood was granted")
	}
	// The Blood alone combines nothing, and spends nothing.
	if frames, admitted := w.rt.BeginItemUse(c, ivy3BloodCodename, ivy3FortressSpawn, *w.nowMs); admitted || len(frames) != 0 {
		t.Fatalf("signless blood: admitted %v, frames %v", admitted, frames)
	}
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_04", 1)
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_03", 1)
	if frames, admitted := w.rt.BeginItemUse(c, ivy3BloodCodename, ivy3FortressSpawn, *w.nowMs); admitted || len(frames) != 0 {
		t.Fatalf("two signs assembled the key: admitted %v, frames %v", admitted, frames)
	}
	if captureItemCount(c, ivy3KeyCodename) != 0 {
		t.Fatal("a key appeared from two signs")
	}
	// All three signs: the key is assembled, the signs are spent, and
	// the caller spends the Blood.
	holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_02", 1)
	if _, admitted := w.rt.BeginItemUse(c, ivy3BloodCodename, ivy3FortressSpawn, *w.nowMs); !admitted {
		t.Fatal("the Blood was not spent on the assembled key")
	}
	if captureItemCount(c, ivy3KeyCodename) != 1 {
		t.Fatal("the key was never assembled")
	}
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(c, sign) != 0 {
			t.Fatalf("sign %s survived the assembly", sign)
		}
	}
	// A key that cannot be granted refuses in silence: no line,
	// and the signs stay where they are.
	blocked := ivy3Accept(t, w, "BlockedBlood")
	holdItems(t, w.rt, blocked, ivy3BloodCodename, 1)
	for _, sign := range ivy3SignCodenames {
		holdItems(t, w.rt, blocked, sign, 1)
	}
	plan := w.rt.PlanInventory
	w.rt.PlanInventory = func(*enterworld.Character, []inventory.ItemAmount, []inventory.ItemAmount) ([]enterworld.InventoryRow, []wire.Frame, error) {
		return nil, nil, errors.New("cannot place")
	}
	frames, admitted := w.rt.BeginItemUse(blocked, ivy3BloodCodename, ivy3FortressSpawn, *w.nowMs)
	w.rt.PlanInventory = plan
	if admitted || len(frames) != 0 {
		t.Fatalf("refused blood: admitted %v, frames %v", admitted, frames)
	}
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(blocked, sign) != 1 {
			t.Fatalf("sign %s was spent by a refused assembly", sign)
		}
	}
	if captureItemCount(blocked, ivy3KeyCodename) != 0 {
		t.Fatal("a key appeared from a refused assembly")
	}
}

/*
================
TestIvy3KeyUseGatesAndInvitation
================
*/
func TestIvy3KeyUseGatesAndInvitation(t *testing.T) {
	w := newIvy3World(t)
	leader := ivy3Accept(t, w, "KeyLeader")
	holdItems(t, w.rt, leader, ivy3KeyCodename, 1)
	far := simulation.Spawn{RegionID: 25183, X: 0, Y: 0, Z: 0}
	// Away from the fortress point: the place line, no invitation.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true}
	frames, admitted := w.rt.BeginItemUse(leader, ivy3KeyCodename, far, *w.nowMs)
	if admitted || !hasNotice(frames, ivy3SymKeyWrongPlace) || len(w.invites) != 0 {
		t.Fatalf("distant key: admitted %v, invites %d", admitted, len(w.invites))
	}
	// At the point but not in a shared party: the party-mode line.
	w.facts = InstancePartyFacts{Found: true}
	frames, _ = w.rt.BeginItemUse(leader, ivy3KeyCodename, ivy3FortressSpawn, *w.nowMs)
	if !hasNotice(frames, ivy3SymKeyNotShared) || len(w.invites) != 0 {
		t.Fatal("a solo key use was not refused for its party mode")
	}
	// In one but not its leader: the leadership line.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Shared: true}
	frames, _ = w.rt.BeginItemUse(leader, ivy3KeyCodename, ivy3FortressSpawn, *w.nowMs)
	if !hasNotice(frames, ivy3SymKeyNotLeader) || len(w.invites) != 0 {
		t.Fatal("a member's key use was not refused as a non-leader")
	}
	// The leader with a trade transport out: the transport line.
	w.facts = InstancePartyFacts{Found: true, InParty: true, Leader: true, Shared: true, Transported: true}
	frames, _ = w.rt.BeginItemUse(leader, ivy3KeyCodename, ivy3FortressSpawn, *w.nowMs)
	if !hasNotice(frames, ivy3SymKeyTransport) || len(w.invites) != 0 {
		t.Fatal("a transported key use was not refused")
	}
	// At the point, leading a shared party: the invitation opens and
	// the key is kept for Ivy's death to spend.
	w.facts.Transported = false
	if _, admitted := w.rt.BeginItemUse(leader, ivy3KeyCodename, ivy3FortressSpawn, *w.nowMs); admitted {
		t.Fatal("the key was consumed by its use")
	}
	if len(w.invites) != 1 {
		t.Fatalf("invites = %d, want 1", len(w.invites))
	}
	call := w.invites[0]
	if call.Definition != ivy3Definition || call.Entry != ivy3FortressSpawn ||
		call.DeadlineMs-*w.nowMs != ivy3InviteLifeMs+ivy3InviteSlackMs {
		t.Fatalf("invite = %+v", call)
	}
	if captureItemCount(leader, ivy3KeyCodename) != 1 {
		t.Fatal("the key left the leader's bag")
	}
	if !call.Eligible(leader) {
		t.Fatal("the leader is not eligible for their own summon")
	}
	holder := ivy3Character(w, "KeyHolder", true)
	holdItems(t, w.rt, holder, ivy3KeyCodename, 1)
	if !call.Eligible(holder) {
		t.Fatal("a key holder without the quest was refused")
	}
	stranger := ivy3Character(w, "Stranger", false)
	if call.Eligible(stranger) {
		t.Fatal("a character with neither the quest nor a key was invited")
	}
	// A second key use while the summon stands is silent.
	if frames, admitted := w.rt.BeginItemUse(leader, ivy3KeyCodename, ivy3FortressSpawn, *w.nowMs); admitted || len(frames) != 0 || len(w.invites) != 1 {
		t.Fatalf("second key use: admitted %v, frames %v, invites %d", admitted, frames, len(w.invites))
	}
}

/*
================
TestIvy3BattleClockAndTimeout

The stage marks are stated as literals on purpose: they are the
native reading (the encounter updater's own stores), not values
derived from the constants under test, so a wrong constant fails
here instead of drifting with the test. The room stands empty for
30,000 ms, Ivy appears at t+30,000, speaks at t+32,000, the lines
trade every 5,000 ms, the battle opens at t+47,000, then battle-
clock marks of 600,000 / 900,000 / 1,200,000 / 1,210,000 counted
from the battle's opening.
================
*/
func TestIvy3BattleClockAndTimeout(t *testing.T) {
	w := newIvy3World(t)
	leader := ivy3Accept(t, w, "ClockLeader")
	member := ivy3Accept(t, w, "ClockMember")
	w.openLayer(leader.Name, member.Name)
	start := *w.nowMs
	w.update()
	*w.nowMs = start + 29_999
	w.update()
	if len(w.spawned) != 0 {
		t.Fatalf("Ivy spawned during the summon phase: %v", w.spawned)
	}
	*w.nowMs = start + 30_000
	w.update()
	if len(w.spawned) != 1 || w.spawned[0].Codename != ivy3BossCodename || w.spawned[0].Population != w.lease {
		t.Fatalf("spawned = %v", w.spawned)
	}
	// The three lines, five seconds apart; nothing before its beat.
	beats := []struct {
		at     int64
		symbol string
	}{
		{32_000, ivy3SymBossThreat},
		{37_000, ivy3SymPlayerPlea},
		{42_000, ivy3SymBossRefusal},
	}
	for _, beat := range beats {
		*w.nowMs = start + beat.at - 1
		w.update()
		if hasNotice(w.pushed[member.Name], beat.symbol) {
			t.Fatalf("line %s landed a millisecond early", beat.symbol)
		}
		*w.nowMs = start + beat.at
		w.update()
		if !hasNotice(w.pushed[member.Name], beat.symbol) {
			t.Fatalf("line %s never landed", beat.symbol)
		}
	}
	*w.nowMs = start + 47_000 - 1
	w.update()
	if hasNotice(w.pushed[member.Name], ivy3SymBattleBegun) {
		t.Fatal("the battle opened during the lines")
	}
	*w.nowMs = start + 47_000
	w.update()
	if !hasNotice(w.pushed[member.Name], ivy3SymBattleBegun) {
		t.Fatal("the battle never opened")
	}
	battle := start + 47_000
	*w.nowMs = battle + 600_000
	w.update()
	if !hasNotice(w.pushed[member.Name], ivy3SymWarn10) {
		t.Fatal("the ten-minute warning never landed")
	}
	*w.nowMs = battle + 900_000
	w.update()
	if !hasNotice(w.pushed[member.Name], ivy3SymWarn5) {
		t.Fatal("the five-minute warning never landed")
	}
	*w.nowMs = battle + 1_200_000
	w.update()
	if !hasNotice(w.pushed[member.Name], ivy3SymBossLeaves) {
		t.Fatal("Ivy never left")
	}
	if len(w.closed) != 0 {
		t.Fatal("the layer closed when Ivy left")
	}
	*w.nowMs = battle + 1_209_999
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("the layer ended before its own clock ran out")
	}
	*w.nowMs = battle + 1_210_000
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 2 {
		t.Fatalf("closed %d returned %d, want 1 and 2", len(w.closed), len(w.returned))
	}
	// A kill after Ivy has left earns nothing.
	packed := uint32(instance.Pack(ivy3Definition, 3))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteIvy3Kill(leader, ivy3BossCodename)
	if captureItemCount(leader, ivy3HairCodename) != 0 {
		t.Fatal("a late kill handed over the Red Hair")
	}
}

/*
================
TestIvy3KillCreditGraceAndTurnIn
================
*/
func TestIvy3KillCreditGraceAndTurnIn(t *testing.T) {
	w := newIvy3World(t)
	t.Setenv(progression.EnvBetaGrowth, "0")
	leader := ivy3Accept(t, w, "KillLeader")
	member := ivy3Accept(t, w, "KillMember")
	keyless := ivy3Accept(t, w, "KillKeyless")
	// Both hunters have walked the open-world half: the three
	// sentry hunts and the Punisher's Blood stand counted before
	// Ivy falls, so the kill credit is all their journal lacks.
	for _, c := range []*enterworld.Character{leader, member} {
		holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_04", 1)
		holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_03", 1)
		holdItems(t, w.rt, c, "ITEM_QNO_EU_IVY_3_02", 1)
		holdItems(t, w.rt, c, ivy3BloodCodename, 1)
		for _, mob := range []string{"MOB_QT_01_SOLDIER_CLON", "MOB_QT_03_PUNISHER_CLON", "MOB_QT_01_PUNISHER", ivy3BloodSource} {
			w.rt.KillUpdater()(c, mob, 0)
		}
	}
	holdItems(t, w.rt, leader, ivy3KeyCodename, 1)
	holdItems(t, w.rt, member, ivy3KeyCodename, 1)
	w.openLayer(leader.Name, member.Name, keyless.Name)
	w.update()
	// A kill marked in another world, or of something else, does
	// nothing here.
	wrong := uint32(instance.Pack(instance.DefinitionID(10), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteIvy3Kill(leader, ivy3BossCodename)
	w.update()
	if len(w.closed) != 0 || hasNotice(w.pushed[member.Name], ivy3SymBossDeath) {
		t.Fatal("a kill in another world reached the layer")
	}
	packed := uint32(instance.Pack(ivy3Definition, 3))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.noteIvy3Kill(leader, "MOB_QT_01_SOMETHING_ELSE")
	w.update()
	if len(w.closed) != 0 || hasNotice(w.pushed[member.Name], ivy3SymBossDeath) {
		t.Fatal("another monster's fall reached the layer")
	}
	// Ivy falls: the room hears her death line, both hunters are
	// credited with the Red Hair and disarmed of their keys.
	w.rt.noteIvy3Kill(leader, ivy3BossCodename)
	w.update()
	if !hasNotice(w.pushed[member.Name], ivy3SymBossDeath) {
		t.Fatal("the death line never landed")
	}
	for _, c := range []*enterworld.Character{leader, member} {
		if captureItemCount(c, ivy3HairCodename) != 1 {
			t.Fatalf("%s was not credited with the Red Hair", c.Name)
		}
		if captureItemCount(c, ivy3KeyCodename) != 0 {
			t.Fatalf("%s kept a key after the kill", c.Name)
		}
		if captureItemCount(c, ivy3HornCodename) != 0 {
			t.Fatalf("%s kept Horns after the kill", c.Name)
		}
		at := activeQuestIndex(c, w.def.RefID)
		if at < 0 || !objectiveMet(c, w.def, c.ActiveQuests[at]) {
			t.Fatalf("%s's quest did not stand achieved", c.Name)
		}
	}
	// The keyless resident leaves with nothing: no Red Hair, no
	// achievement - and their Horns are gone all the same.
	if captureItemCount(keyless, ivy3HairCodename) != 0 {
		t.Fatal("a keyless resident was credited with the Red Hair")
	}
	if captureItemCount(keyless, ivy3HornCodename) != 0 {
		t.Fatal("a keyless resident kept their Horns after the kill")
	}
	if at := activeQuestIndex(keyless, w.def.RefID); at < 0 || objectiveMet(keyless, w.def, keyless.ActiveQuests[at]) {
		t.Fatal("a keyless resident's quest stood achieved")
	}
	// The grace minute: still open, then returned and closed.
	w.update()
	if len(w.closed) != 0 {
		t.Fatal("the layer closed before its grace ran out")
	}
	*w.nowMs += ivy3KillGraceMs
	w.update()
	if len(w.closed) != 1 || len(w.returned) != 3 {
		t.Fatalf("closed %d returned %d, want 1 and 3", len(w.closed), len(w.returned))
	}
	// Back at Ratchel the Red Hair reports and pays: the hair is
	// taken, the reward text lands, and the quest is done for good.
	before := *leader.Experience
	var pointsBefore int64
	if leader.SkillPoints != nil {
		pointsBefore = *leader.SkillPoints
	}
	skillBefore := *leader.SkillExp
	// The Blood and the three signs the hunts granted are still in
	// the bag: the kill credit counted the hunts, it did not spend
	// the items. The turn-in below is what takes them.
	if captureItemCount(leader, ivy3BloodCodename) != 1 {
		t.Fatal("the Blood left the bag before its turn-in")
	}
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(leader, sign) != 1 {
			t.Fatalf("sign %s left the bag before its turn-in", sign)
		}
	}
	if _, err := w.rt.AdvanceNpcQuest(leader, ivy3QuestCodename, ivy3NpcCodename); err != nil {
		t.Fatal(err)
	}
	if !questCompleted(leader, w.def.RefID) {
		t.Fatal("the summon did not complete at Ratchel")
	}
	if got := *leader.Experience - before; got != 40000 {
		t.Fatalf("Ratchel paid %d EXP, want 40000", got)
	}
	var pointsAfter int64
	if leader.SkillPoints != nil {
		pointsAfter = *leader.SkillPoints
	}
	if got := (pointsAfter-pointsBefore)*progression.SkillExpPerSP + *leader.SkillExp - skillBefore; got != 4000 {
		t.Fatalf("Ratchel paid %d skill EXP, want 4000", got)
	}
	if captureItemCount(leader, ivy3HairCodename) != 0 {
		t.Fatal("the Red Hair survived its turn-in")
	}
	// The turn-in also takes the Blood and every sign still held:
	// quest items the finished quest leaves behind.
	if captureItemCount(leader, ivy3BloodCodename) != 0 {
		t.Fatal("the Blood survived its turn-in")
	}
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(leader, sign) != 0 {
			t.Fatalf("sign %s survived its turn-in", sign)
		}
	}
	if captureItemCount(leader, "ITEM_QNO_EU_IVY_3_07") != 20 {
		t.Fatal("the reward token stack never landed")
	}
	if _, err := w.rt.StartQuest(leader, ivy3QuestCodename); err == nil {
		t.Fatal("IVY 3 was accepted a second time")
	}
}

/*
================
TestIvy3PartyChangedExpels

A resident whose party standing breaks mid-summon hears the
instance-condition line and is sent out; anyone else in the room
is untouched, and a name that is not inside changes nothing.
================
*/
func TestIvy3PartyChangedExpels(t *testing.T) {
	w := newIvy3World(t)
	leader := ivy3Accept(t, w, "PartyLeader")
	member := ivy3Accept(t, w, "PartyMember")
	w.openLayer(leader.Name, member.Name)
	changed := func(name string) {
		w.rt.DispatchInstanceEvent(InstanceEvent{Kind: InstanceEventPartyChanged, Division: ivy3TestDivision, Definition: ivy3Definition, World: w.lease.ID, Name: name})
	}
	changed("Stranger")
	if len(w.returned) != 0 {
		t.Fatalf("returned = %v, want none", w.returned)
	}
	changed(member.Name)
	if !hasNotice(w.pushed[member.Name], ivy3SymCondition) {
		t.Fatal("the member never heard the instance-condition line")
	}
	if len(w.returned) != 1 || w.returned[0] != member.Name {
		t.Fatalf("returned = %v, want the member", w.returned)
	}
	if hasNotice(w.pushed[leader.Name], ivy3SymCondition) {
		t.Fatal("the leader heard the condition line")
	}
}

/*
================
TestIvy3ExitGateRisesBesideHerAndFalls

The reference kill handler raises the exit gate beside Ivy (#1878): the
site her owner reports for her fall in this layer is where the gate
rises, once, on the kill; a site in another world or of another monster
is ignored; the layer's end takes the gate down.
================
*/
func TestIvy3ExitGateRisesBesideHerAndFalls(t *testing.T) {
	w := newIvy3World(t)
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
	leader := ivy3Accept(t, w, "GateLeader")
	w.openLayer(leader.Name)
	w.update()

	site := simulation.Spawn{RegionID: 25000, X: 120, Y: 4, Z: -30}
	wrong := uint32(instance.Pack(instance.DefinitionID(10), 1))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &wrong, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, ivy3BossCodename, simulation.Spawn{RegionID: 1})
	packed := uint32(instance.Pack(ivy3Definition, 3))
	leader.World = &enterworld.CharacterWorld{PackedInstance: &packed, InstanceReturn: &domain.InstanceReturnAnchor{}}
	w.rt.NoteKillSite(leader, "MOB_QT_01_SOMETHING_ELSE", simulation.Spawn{RegionID: 2})
	w.rt.NoteKillSite(leader, ivy3BossCodename, site)
	w.rt.noteIvy3Kill(leader, ivy3BossCodename)
	w.update()
	if len(gates) != 1 || gates[0].world != w.lease.ID || gates[0].fell != site {
		t.Fatalf("gates raised = %+v, want one beside %+v in %v", gates, site, w.lease.ID)
	}
	w.update()
	if len(gates) != 1 {
		t.Fatalf("the gate rose again: %+v", gates)
	}
	*w.nowMs += ivy3KillGraceMs
	w.update()
	if len(removed) != 1 || removed[0] != w.lease.ID {
		t.Fatalf("gates removed = %+v, want the layer's", removed)
	}
}
