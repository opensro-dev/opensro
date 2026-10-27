/*
===========================================================================

rahid6.go - Rahid 6 (QNO_RM_OLDWOMAN_6): Shiphr's summons of Demon Shaitan

The first quest programme on the instanced-world framework (#321 step 5).
Shiphr (NPC_RM_SLAVE2) offers the quest once Rahid 5 is done; accepting
hands over one Rainbow Grass Bundle. While the quest runs, Shiphr's menu
carries a "Meet Shaitan" row: the party leader picks it, the native talk
gates answer in order (party and leadership, trade transport, shared
party options, once per day, a meeting already open), and the invitation
lane opens the Shaitan layer (instance definition 10) and prompts the
members who are doing or have finished this quest.

Inside, the programme owns the encounter clock: Tahomet appears 30 s
after the world opens, speaks the five-line confrontation at 5 s steps,
then the battle runs on the native timer - a warning with 10 minutes
left, one with 5, and at the end Shaitan takes his leave and the party
is returned. The layer's own deadline is set just past the programme's
end, so a party that somehow outlasts the programme is still evacuated
by the lane.

The kill is party business: when Tahomet falls, every resident doing
the quest who still holds the bundle trades all bundles for the Silver
Crescent Moon Necklace (the objective item - the shipped contents line
counts it), and a holder who lost the bundle hears why. The quest then
reports to Shiphr, whose completion takes the necklace and pays the
advertised experience. State the native keeps per character (its step
and timers) lives per layer here, the layer being the only world the
quest can happen in; the once-a-day stamp rides the persisted quest
supply day (QuestSupplies) under this quest's own reference id, a field
the capture supplies already persist, so no store schema step is owed.

Presentation: the confrontation and notice lines are quest
notifications carrying the authored symbols, in the native order and
cadence. The native speaks them as say-lines over the monster and the
leader; the port has no say plane yet, so the same text arrives as
banners - order and timing are the native ones, the bubble is not.

===========================================================================
*/
package quest

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	rahid6QuestCodename    = "QNO_RM_OLDWOMAN_6"
	rahid6ShiphrCodename   = "NPC_RM_SLAVE2"
	rahid6TahometCodename  = "MOB_QT_01_TAHOMET"
	rahid6BundleCodename   = "ITEM_QNO_RM_OLDWOMAN_7_03"
	rahid6NecklaceCodename = "ITEM_QNO_RM_OLDWOMAN_7_01"

	// rahid6Definition is INS_QNO_RM_OLDWOMAN_6 in the shipped
	// gameworld table: layer limit 100, player limit 8.
	rahid6Definition = instance.DefinitionID(10)

	// rahid6TravelPrefix marks the dialogue token of Shiphr's travel row,
	// the capture-supply token shape (AdvanceNpcQuest routes it).
	rahid6TravelPrefix = "rahid6:"

	// Encounter timing, read off the native encounter update: the
	// summon at 30 s, one confrontation line every 5 s (five lines),
	// then the battle clock - the first warning 600,000 ms in with
	// 10 minutes left, two 300,000 ms steps to the 5-minutes-left
	// warning and the farewell that ends the meeting.
	rahid6SpawnDelayMs  = 30_000
	rahid6ConvoStepMs   = 5_000
	rahid6BattleMs      = 600_000
	rahid6WarningStepMs = 300_000

	// The layer deadline is the programme's own end plus this slack:
	// spawn, the five confrontation steps and the full battle clock.
	// No native layer lifetime for the meeting exists in either
	// binary; the 1,208,490 ms once claimed here as a generic native
	// hard end was an inference, and it fell 47 s short of the
	// programme's own end, so the farewell could never arrive.
	// Authored, not native: the deadline is only the lane's backstop;
	// the programme ends the layer itself, on the kill or the farewell.
	rahid6LayerEndSlackMs = 8_490
	rahid6LayerLifetimeMs = rahid6SpawnDelayMs + 5*rahid6ConvoStepMs +
		rahid6BattleMs + 2*rahid6WarningStepMs + rahid6KillGraceMs + rahid6LayerEndSlackMs

	// The kill leaves the room a 60,000 ms grace: the reference death
	// handler raises the exit gate and sets its event timer to the
	// elapsed time plus 0xEA60, and the timer's expiry returns the
	// party. The deadline above makes room for a kill in the last
	// moment of the battle.
	rahid6KillGraceMs = 60_000

	// Tahomet appears 20 to 100 from the party: the native polar
	// random placement, the quest-spawn radius pair.
	rahid6SpawnRadiusMin  = 20
	rahid6SpawnRadiusSpan = 80
)

// rahid6EntrySpawn is where an invited party lands: instance layers
// duplicate the Roc Apex region space (the shipped region codes name
// regions 25532..26302 RN_RM_ROCAPEX, the summit Shaitan guards), and
// the party is set down at the middle region's centre. The exact native
// start pose is authored in the native instance start table, which no
// readable copy survives in this checkout; the region and the centred
// pose are the recorded inference, and the confrontation finds the
// party wherever in the room it stands.
var rahid6EntrySpawn = simulation.Spawn{RegionID: 26045, X: 0, Y: 0, Z: 0, Angle: 0}

// Rahid 6 dialogue and notice symbols (SN_TALK_QNO_RM_OLDWOMAN_6_*).
const (
	rahid6SymTravelTitle   = "SN_TALK_QNO_RM_OLDWOMAN_6_28" // "Meet Shaitan"
	rahid6SymNoParty       = "SN_TALK_QNO_RM_OLDWOMAN_6_26"
	rahid6SymTransport     = "SN_TALK_QNO_RM_OLDWOMAN_6_35"
	rahid6SymNotShared     = "SN_TALK_QNO_RM_OLDWOMAN_6_22"
	rahid6SymSummonedToday = "SN_TALK_QNO_RM_OLDWOMAN_6_18"
	rahid6SymBusy          = "SN_TALK_QNO_RM_OLDWOMAN_6_23"
	rahid6SymBundleLost    = "SN_TALK_QNO_RM_OLDWOMAN_6_29"
	rahid6SymSurrender     = "SN_TALK_QNO_RM_OLDWOMAN_6_13"
	rahid6SymFarewell      = "SN_TALK_QNO_RM_OLDWOMAN_6_17"
	rahid6SymWarn10        = "SN_TALK_QNO_RM_OLDWOMAN_6_30"
	rahid6SymWarn5         = "SN_TALK_QNO_RM_OLDWOMAN_6_31"
	rahid6SymBattleBegun   = "SN_TALK_QNO_RM_OLDWOMAN_6_34"
	rahid6SymCondition     = "UIIT_MSG_QUEST_INS_CONDITION"
)

// rahid6ConvoLines is the confrontation in order: Shaitan, the
// summoner, Shaitan, the summoner, Shaitan - then the battle begins.
var rahid6ConvoLines = []string{
	"SN_TALK_QNO_RM_OLDWOMAN_6_25",
	"SN_TALK_QNO_RM_OLDWOMAN_6_08",
	"SN_TALK_QNO_RM_OLDWOMAN_6_09",
	"SN_TALK_QNO_RM_OLDWOMAN_6_10",
	"SN_TALK_QNO_RM_OLDWOMAN_6_11",
}

var rahid6Specs = []QuestSpec{
	{
		// CQNO_RM_OLDWOMAN_6 (compiled row: words 0x130..0x13f, overrides
		// 0x58/0x5c/0x60/0x74/0xac, prereq 0x102, completions +0x2d = 1).
		// The objective is holding the Silver Crescent Moon Necklace:
		// the shipped contents line counts it ("Find Certificate of
		// Demon Shaitan (%d)"), the kill evaluator trades the bundle
		// for it, and Shiphr's pay step takes it back. v1.150's reward
		// row pays 2,573,000 EXP and nothing else.
		Codename: rahid6QuestCodename, RequiredQuests: []string{"QNO_RM_OLDWOMAN_5"},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: rahid6NecklaceCodename, CollectCount: 1,
		MaxCompletions:   1,
		StartNpcCodename: rahid6ShiphrCodename, EndNpcCodename: rahid6ShiphrCodename,
		OfferPromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_01", AcceptResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_03",
		DenyResponseSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_04", NotAchievedSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_07",
		CompletePromptSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_19", InventoryFullSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_27",
		AchievedNowSymbol: "SN_TALK_QNO_RM_OLDWOMAN_6_21",
		RewardExp:         2573000,
	},
}

/*
================
InstancePartyFacts

The party facts the "Meet Shaitan" talk gates read, answered by the
action lane (which owns the party registry) through the wired door.
================
*/
type InstancePartyFacts struct {
	Found       bool
	InParty     bool
	Leader      bool
	Shared      bool
	Transported bool
}

/*
================
InstanceInviteCall

One demand to bring the leader's party into an instanced world.
Eligible is the programme's own member predicate; DeadlineMs is the
layer's hard end, absolute on the runtime clock.
================
*/
type InstanceInviteCall struct {
	Definition instance.DefinitionID
	Entry      simulation.Spawn
	DeadlineMs int64
	Eligible   func(*enterworld.Character) bool
}

/*
================
InstanceInviteOutcome

What an invitation did: whether the layer opened and the leader
landed, and the lease the programme spawns into and closes. A layer
that would not open and a leader who never landed answer the same
busy line, so the outcome carries no finer failure shape.
================
*/
type InstanceInviteOutcome struct {
	Opened   bool
	LeaderIn bool
	Lease    instance.Lease
	Prompted []string
	Skipped  []string
}

/*
================
rahid6Layer

One open Shaitan meeting, keyed by its world. Residents are the names
currently inside; the lease arrives with the invitation outcome (the
setup event can be delivered before it lands), so spawn and close
read it lazily.
================
*/
type rahid6Layer struct {
	division  string
	summoner  string
	residents map[string]bool
	lease     instance.Lease
	openedMs  int64
	spawnedMs int64
	convoLine int
	battleMs  int64
	warnings  int
	killed    bool
	credited  bool
	graceEnd  int64
	ended     bool
	// bossSite is where the boss fell (NoteKillSite); the exit gate
	// stands beside it.
	bossSite      simulation.Spawn
	bossSiteKnown bool
}

/*
================
rahid6Programme

The programme's runtime state: one layer per open world, the leases
the invitations returned, and which leader already holds a meeting.
Process-local, like the leases themselves.
================
*/
type rahid6Programme struct {
	mu          sync.Mutex
	layers      map[instance.ID]*rahid6Layer
	leases      map[instance.ID]instance.Lease
	summoner    map[string]instance.ID
	summonedDay map[string]uint16
}

/*
================
rahid6Now

The programme clock: milliseconds on the runtime clock, zero when
unwired (detached tests then see no elapsed time and nothing fires).
================
*/
func (rt *Runtime) rahid6Now() int64 {
	if rt.NowMs == nil {
		return 0
	}
	return rt.NowMs()
}

/*
================
RegisterRahid6Programme

Subscribes the Rahid 6 programme to its instance definition's
lifecycle (framework step 5). Death and party-change go unregistered:
the kill reaches the programme through the quest kill updater, which
knows the monster and the killer's world, and no native rule reads a
resident's party change mid-meeting.
================
*/
func (rt *Runtime) RegisterRahid6Programme() {
	rt.RegisterInstanceHandler(rahid6Definition, InstanceHandler{
		Setup:     rt.rahid6Setup,
		Enter:     rt.rahid6Enter,
		Leave:     rt.rahid6Leave,
		Update:    rt.rahid6Update,
		Activated: rt.rahid6Activated,
	})
}

/*
================
Rahid6MemberEligible

The invitation predicate the native popup states: members doing this
quest, or having finished it, may come. Wired as the invite call's
Eligible closure at the composition root.
================
*/
func (rt *Runtime) Rahid6MemberEligible(c *enterworld.Character) bool {
	def, ok := rt.Defs.ByCodename(rahid6QuestCodename)
	if !ok || c == nil {
		return false
	}
	return activeQuestIndex(c, def.RefID) >= 0 || questCompleted(c, def.RefID)
}

/*
================
rahid6AcceptanceItems

One Rainbow Grass Bundle, granted on acceptance like the trap
supplies: the fiction requires holding it until the meeting, and
losing it is a real failure the kill answers for.
================
*/
func rahid6AcceptanceItems(def *Definition) []inventory.ItemAmount {
	if def.Codename != rahid6QuestCodename {
		return nil
	}
	return []inventory.ItemAmount{{Codename: rahid6BundleCodename, Count: 1}}
}

/*
================
rahid6TravelOption

Shiphr's "Meet Shaitan" row, offered while the quest runs unachieved
at Shiphr. It takes the plain not-achieved row's place (the row's
prompt is that same speech); choosing and confirming it runs the
gates and the invitation in advanceRahid6Travel.
================
*/
func (rt *Runtime) rahid6TravelOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	if def.Codename != rahid6QuestCodename || npc != rahid6ShiphrCodename {
		return NpcOption{}, false
	}
	at := activeQuestIndex(c, def.RefID)
	if at < 0 {
		return NpcOption{}, false
	}
	return NpcOption{
		Codename:     rahid6TravelPrefix + def.Codename,
		TitleSymbol:  rahid6SymTravelTitle,
		PromptSymbol: notAchievedSymbol(c, def, c.ActiveQuests[at]),
		Complete:     true,
	}, true
}

/*
================
rahid6SummonedToday

The once-a-day gate: the persisted quest-supply day under this
quest's reference id holds the last summon's day (capture supplies
never read this quest's entry - it has no supply row).
================
*/
func (rt *Runtime) rahid6SummonedToday(c *enterworld.Character, def *Definition) bool {
	today := rt.CalendarNow().Day
	if state, exists := c.QuestSupplies[def.RefID]; exists && state.Day == today {
		return true
	}
	return rt.rahid6.summonedOn(c.Name, today)
}

/*
================
advanceRahid6Travel

The confirmed "Meet Shaitan" row. Native gate order: no party or not
its leader (_26), a summoned trade transport (_35), unshared party
options (_22), already summoned today (_18), a meeting already open
(_23); then the invitation. A leader who lost the bundle is handed
another before departing - losing it is recoverable at Shiphr, never
at the meeting.
================
*/
func (rt *Runtime) advanceRahid6Travel(c *enterworld.Character, npc string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(rahid6QuestCodename)
	if !ok || npc != rahid6ShiphrCodename {
		return OpResult{}, fmt.Errorf("quest %s wrong travel NPC", rahid6QuestCodename)
	}
	if activeQuestIndex(c, def.RefID) < 0 {
		return OpResult{}, fmt.Errorf("quest %s is not active", rahid6QuestCodename)
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return OpResult{}, fmt.Errorf("instance owner unavailable")
	}
	facts := rt.InstancePartyFacts(c)
	switch {
	case !facts.Found:
		return OpResult{}, fmt.Errorf("travel character unavailable")
	case !facts.InParty || !facts.Leader:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("not leading a party"), rahid6SymNoParty}
	case facts.Transported:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("trade transport summoned"), rahid6SymTransport}
	case !facts.Shared:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("party options not shared"), rahid6SymNotShared}
	case rt.rahid6SummonedToday(c, def):
		return OpResult{}, &dialogueRefusal{fmt.Errorf("already summoned today"), rahid6SymSummonedToday}
	case rt.rahid6.summonedBy(c.Name):
		return OpResult{}, &dialogueRefusal{fmt.Errorf("a meeting is already open"), rahid6SymBusy}
	}
	if err := rt.rahid6RegrantBundle(c, def); err != nil {
		return OpResult{}, err
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: rahid6Definition,
		Entry:      rahid6EntrySpawn,
		DeadlineMs: rt.rahid6Now() + rahid6LayerLifetimeMs,
		Eligible:   rt.Rahid6MemberEligible,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		// A layer that would not open, or a leader who never landed:
		// the room answers as occupied - try again later.
		return OpResult{}, &dialogueRefusal{fmt.Errorf("the meeting would not open"), rahid6SymBusy}
	}
	rt.rahid6.rememberLease(outcome.Lease, c.Name)
	day := rt.CalendarNow().Day
	if rt.deps.Update(c, "rahid6-summon", func() bool {
		if c == nil || c.DeletePending {
			return false
		}
		setCaptureSupply(c, def.RefID, day, false)
		return true
	}) {
		return OpResult{}, nil
	}
	// The character door refused the stamp - the leader stopped being
	// authoritative between the invitation and the write. The meeting
	// stands, so keep the day in the programme's own record: the
	// once-a-day gate still answers once the meeting ends.
	rt.rahid6.rememberSummonedDay(c.Name, day)
	return OpResult{}, nil
}

/*
================
rahid6RegrantBundle

Hands one bundle to a leader holding neither the bundle nor the
necklace. A full bag answers with the quest's bag-full line.
================
*/
func (rt *Runtime) rahid6RegrantBundle(c *enterworld.Character, def *Definition) error {
	if rt.PlanInventory == nil {
		return fmt.Errorf("quest inventory owner unavailable")
	}
	var refusal error
	changed := rt.deps.Update(c, "rahid6-bundle", func() bool {
		if c == nil || c.DeletePending || activeQuestIndex(c, def.RefID) < 0 {
			refusal = fmt.Errorf("quest %s is not active", rahid6QuestCodename)
			return false
		}
		if captureItemCount(c, rahid6BundleCodename) > 0 || captureItemCount(c, rahid6NecklaceCodename) > 0 {
			return true
		}
		rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: rahid6BundleCodename, Count: 1}})
		if err != nil {
			refusal = inventoryRefusal(def, err)
			return false
		}
		c.MissionInventory = rows
		return true
	})
	if refusal != nil {
		return refusal
	}
	if !changed {
		return fmt.Errorf("travel character no longer authoritative")
	}
	return nil
}

/*
================
summonedBy

Whether this leader already holds an open meeting.
================
*/
func (p *rahid6Programme) summonedBy(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, open := p.summoner[name]
	return open
}

/*
================
rememberSummonedDay

The once-a-day gate's process-local fallback, written when the
persisted stamp's character door refuses the write. Forgotten on
restart with the leases; the persisted stamp covers relogs.
================
*/
func (p *rahid6Programme) rememberSummonedDay(name string, day uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.summonedDay == nil {
		p.summonedDay = make(map[string]uint16)
	}
	p.summonedDay[name] = day
}

/*
================
summonedOn

Whether the process-local record holds this leader's summon day.
================
*/
func (p *rahid6Programme) summonedOn(name string, day uint16) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	held, recorded := p.summonedDay[name]
	return recorded && held == day
}

/*
================
rememberLease

Keeps the invitation's lease and its summoner under the world the
layer opened as. The setup event may already have been delivered;
the layer adopts the lease at its next update.
================
*/
func (p *rahid6Programme) rememberLease(lease instance.Lease, summoner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leases == nil {
		p.leases = make(map[instance.ID]instance.Lease)
	}
	if p.summoner == nil {
		p.summoner = make(map[string]instance.ID)
	}
	p.leases[lease.ID] = lease
	// Recorded unconditionally: the setup event may not have landed
	// yet, and rahid6ForgetLayer sweeps every entry naming the world,
	// so a layer that already ended cannot refuse its leader forever.
	p.summoner[summoner] = lease.ID
}

/*
================
layerFor

The layer state for one world, created on first sight (setup, or an
enter that raced it).
================
*/
func (rt *Runtime) rahid6LayerFor(event InstanceEvent) *rahid6Layer {
	p := &rt.rahid6
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.layers == nil {
		p.layers = make(map[instance.ID]*rahid6Layer)
	}
	layer := p.layers[event.World]
	if layer == nil {
		layer = &rahid6Layer{
			division:  event.Division,
			residents: make(map[string]bool),
			openedMs:  rt.rahid6Now(),
		}
		p.layers[event.World] = layer
	}
	return layer
}

/*
================
forgetLayer

Drops a finished layer and everything that pointed at it.
================
*/
func (rt *Runtime) rahid6ForgetLayer(world instance.ID) {
	p := &rt.rahid6
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, id := range p.summoner {
		if id == world {
			delete(p.summoner, name)
		}
	}
	delete(p.layers, world)
	delete(p.leases, world)
}

/*
================
rahid6Setup

A layer opened: start its encounter clock.
================
*/
func (rt *Runtime) rahid6Setup(event InstanceEvent) {
	rt.rahid6LayerFor(event)
}

/*
================
rahid6Enter

A resident arrived. The first through the door is the summoner (the
invitation moves the leader in before any member answers).
================
*/
func (rt *Runtime) rahid6Enter(event InstanceEvent) {
	layer := rt.rahid6LayerFor(event)
	rt.rahid6.mu.Lock()
	layer.residents[event.Name] = true
	if layer.summoner == "" {
		layer.summoner = event.Name
	}
	rt.rahid6.mu.Unlock()
}

/*
================
rahid6Leave

A resident returned. When the last one leaves a meeting that is not
already ending, the room closes behind them instead of idling to
its deadline.
================
*/
func (rt *Runtime) rahid6Leave(event InstanceEvent) {
	rt.rahid6.mu.Lock()
	layer := rt.rahid6.layers[event.World]
	rt.rahid6.mu.Unlock()
	if layer == nil {
		// The meeting already ended and was forgotten; this leave is
		// its echo (the returns the end itself issued).
		return
	}
	rt.rahid6.mu.Lock()
	delete(layer.residents, event.Name)
	empty := len(layer.residents) == 0
	ended := layer.ended
	lease := layer.lease
	if lease.ID == 0 {
		// No update has adopted the invitation's lease yet; the close
		// below must still name the layer.
		lease = rt.rahid6.leases[event.World]
	}
	rt.rahid6.mu.Unlock()
	if !empty || ended {
		return
	}
	rt.rahid6ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}

/*
================
rahid6Activated

The post-entry checkpoint: a resident who is neither doing nor has
finished this quest does not belong to the meeting - they hear the
instance-condition line and are returned (the native tail loop).
================
*/
func (rt *Runtime) rahid6Activated(event InstanceEvent) {
	character := rt.rahid6Character(event.Division, event.Name)
	if character == nil || rt.Rahid6MemberEligible(character) {
		return
	}
	rt.rahid6PushOne(event.Name, rahid6SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
rahid6Character

One division character by name, from the character source.
================
*/
func (rt *Runtime) rahid6Character(division, name string) *enterworld.Character {
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

/*
================
rahid6PushOne

One quest-notification frame to one character, when the push door
is wired.
================
*/
func (rt *Runtime) rahid6PushOne(name, symbol string) {
	if rt.PushCharacterFrames == nil || name == "" {
		return
	}
	rt.PushCharacterFrames(name, []wire.Frame{questNotification(symbol)})
}

/*
================
rahid6Push

One notice to every resident, in name order so a test (and a log)
reads the same sequence every time.
================
*/
func (rt *Runtime) rahid6Push(layer *rahid6Layer, symbol string) {
	for _, name := range rahid6ResidentNames(layer) {
		rt.rahid6PushOne(name, symbol)
	}
}

/*
================
rahid6ResidentNames

A layer's residents, sorted.
================
*/
func rahid6ResidentNames(layer *rahid6Layer) []string {
	names := make([]string, 0, len(layer.residents))
	for name := range layer.residents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

/*
================
noteRahid6Kill

Tahomet fell: mark its layer. Called from the quest kill updater,
which runs inside the killer's character transaction - so this only
records; the credit runs at the layer's next update, lock-free.
================
*/
func (rt *Runtime) noteRahid6Kill(c *enterworld.Character, codename string) {
	if codename != rahid6TahometCodename || c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != rahid6Definition {
		return
	}
	rt.rahid6.mu.Lock()
	if layer := rt.rahid6.layers[world]; layer != nil {
		layer.killed = true
	}
	rt.rahid6.mu.Unlock()
}

/*
================
noteRahid6BossSite

Where Tahomet fell in its layer (NoteKillSite, just before the kill
note): the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteRahid6BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != rahid6TahometCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != rahid6Definition {
		return
	}
	rt.rahid6.mu.Lock()
	if layer := rt.rahid6.layers[world]; layer != nil && !layer.killed && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.rahid6.mu.Unlock()
}

/*
================
rahid6Update

The encounter clock. One pass per layer tick; elapsed time comes
from the runtime clock, never from counting ticks.
================
*/
func (rt *Runtime) rahid6Update(event InstanceEvent) {
	layer := rt.rahid6LayerFor(event)
	rt.rahid6.mu.Lock()
	if layer.lease.ID == 0 {
		layer.lease = rt.rahid6.leases[event.World]
	}
	killed := layer.killed && !layer.credited
	if killed {
		layer.credited = true
	}
	ended := layer.ended
	rt.rahid6.mu.Unlock()
	if ended {
		return
	}
	now := rt.rahid6Now()
	if killed {
		rt.rahid6FinishKill(event, layer, now)
		return
	}
	if layer.killed {
		if now >= layer.graceEnd {
			rt.rahid6EndLayer(event, layer)
		}
		return
	}
	if layer.spawnedMs == 0 {
		if now-layer.openedMs >= rahid6SpawnDelayMs {
			layer.spawnedMs = now
			rt.rahid6SpawnTahomet(event.Division, layer)
		}
		return
	}
	if layer.battleMs == 0 {
		for layer.convoLine < len(rahid6ConvoLines) &&
			now >= layer.spawnedMs+int64(layer.convoLine+1)*rahid6ConvoStepMs {
			rt.rahid6Push(layer, rahid6ConvoLines[layer.convoLine])
			layer.convoLine++
		}
		if layer.convoLine == len(rahid6ConvoLines) {
			layer.battleMs = now
			rt.rahid6Push(layer, rahid6SymBattleBegun)
		}
		return
	}
	switch elapsed := now - layer.battleMs; {
	case layer.warnings == 0 && elapsed >= rahid6BattleMs:
		layer.warnings = 1
		rt.rahid6Push(layer, rahid6SymWarn10)
	case layer.warnings == 1 && elapsed >= rahid6BattleMs+rahid6WarningStepMs:
		layer.warnings = 2
		rt.rahid6Push(layer, rahid6SymWarn5)
	case layer.warnings == 2 && elapsed >= rahid6BattleMs+2*rahid6WarningStepMs:
		rt.rahid6Push(layer, rahid6SymFarewell)
		rt.rahid6EndLayer(event, layer)
	}
}

/*
================
rahid6SpawnTahomet

Places Tahomet 20 to 100 from a resident (the summoner when they are
still inside) in the layer's population, inside that resident's
character door, the quest-spawn contract.
================
*/
func (rt *Runtime) rahid6SpawnTahomet(division string, layer *rahid6Layer) {
	if rt.SpawnQuestMonster == nil {
		return
	}
	host := layer.summoner
	if !layer.residents[host] {
		names := rahid6ResidentNames(layer)
		if len(names) == 0 {
			return
		}
		host = names[0]
	}
	character := rt.rahid6Character(division, host)
	if character == nil {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division:   division,
		Population: layer.lease,
		Codename:   rahid6TahometCodename,
		RadiusMin:  rahid6SpawnRadiusMin,
		RadiusSpan: rahid6SpawnRadiusSpan,
		NowMs:      rt.rahid6Now(),
	}
	rt.deps.Update(character, "rahid6-spawn", func() bool {
		return rt.SpawnQuestMonster(character, request)
	})
}

/*
================
rahid6FinishKill

Tahomet is down. His surrender line goes to the room; then every
resident doing the quest trades the bundle for the necklace - the
whole party, the dead included, exactly the native kill evaluator -
and a resident who lost the bundle hears why there is no necklace.
The exit gate rises beside him and the room's sixty-second grace
starts; the update returns whoever is still inside when it runs out.
================
*/
func (rt *Runtime) rahid6FinishKill(event InstanceEvent, layer *rahid6Layer, now int64) {
	rt.rahid6Push(layer, rahid6SymSurrender)
	layer.graceEnd = now + rahid6KillGraceMs
	// The reference handler raises the exit gate beside Tahomet, then
	// sets the sixty-second event timer: the gate lets the room leave
	// early, the timer still returns whoever stays.
	rt.rahid6.mu.Lock()
	site, known := layer.bossSite, layer.bossSiteKnown
	rt.rahid6.mu.Unlock()
	if known && rt.SpawnExitGate != nil {
		rt.SpawnExitGate(event.World, site)
	}
	def, ok := rt.Defs.ByCodename(rahid6QuestCodename)
	if ok {
		for _, name := range rahid6ResidentNames(layer) {
			rt.rahid6CreditKill(event.Division, name, def)
		}
	}
}

/*
================
rahid6CreditKill

One resident's kill credit: all bundles leave, one necklace arrives,
atomically, and the collect recompute speaks the obtained line. A
resident not doing the quest (a finished helper) is untouched.
================
*/
func (rt *Runtime) rahid6CreditKill(division, name string, def *Definition) {
	character := rt.rahid6Character(division, name)
	if character == nil {
		return
	}
	var frames []wire.Frame
	lost := false
	rt.deps.Update(character, "rahid6-kill-credit", func() bool {
		if character.DeletePending || activeQuestIndex(character, def.RefID) < 0 {
			return false
		}
		bundles := captureItemCount(character, rahid6BundleCodename)
		if bundles == 0 {
			lost = captureItemCount(character, rahid6NecklaceCodename) == 0
			return false
		}
		if rt.PlanInventory == nil {
			return false
		}
		rows, updates, err := rt.PlanInventory(character,
			[]inventory.ItemAmount{{Codename: rahid6BundleCodename, Count: bundles}},
			[]inventory.ItemAmount{{Codename: rahid6NecklaceCodename, Count: 1}})
		if err != nil {
			return false
		}
		character.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(character)
		frames = append(updates, objectives...)
		return true
	})
	if lost {
		rt.rahid6PushOne(name, rahid6SymBundleLost)
	}
	if len(frames) > 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(name, frames)
	}
}

/*
================
rahid6EndLayer

Returns every resident to their anchor and closes the layer; the
close evacuates anyone the returns missed, so nobody is stranded.
================
*/
func (rt *Runtime) rahid6EndLayer(event InstanceEvent, layer *rahid6Layer) {
	rt.rahid6.mu.Lock()
	if layer.ended {
		rt.rahid6.mu.Unlock()
		return
	}
	layer.ended = true
	lease := layer.lease
	rt.rahid6.mu.Unlock()
	for _, name := range rahid6ResidentNames(layer) {
		if character := rt.rahid6Character(event.Division, name); character != nil && rt.InstanceReturn != nil {
			rt.InstanceReturn(character)
		}
	}
	rt.rahid6ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}
