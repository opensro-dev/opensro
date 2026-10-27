/*
===========================================================================

easteu21.go - EASTEU 21 (Cerberus 3): the Witch's Call of Hell

The Sunset Witch's Cerberus chain ends in an instanced summon. Its
earlier hunts are ordinary catalogue and capture quests (Cerberus 1
authored with the capture contracts, Cerberus 2 a plain lion hunt in
the shipped catalogue). EASTEU 21 is the second quest programme on
the instanced-world framework (#321 step 5), after Rahid 6.

The native shape, read off the reference quest class's own handlers:
the Witch offers the quest only to a member of an experience-shared,
item-shared party (anyone else hears the come-back-with-a-party line);
accepting hands over the Call of Hell (ITEM_QNO_EU_EASTEU_21_01), and a
holder who loses it can be re-granted one once per calendar day. The
party leader uses the Call at the Jupiter Temple plaza: the use gates,
in native order, are the place itself (within 600 of the plaza point),
a shared party, and leadership, with a summoned trade transport
refusing its holder. The invitation then opens instance definition 11
for the members doing or having finished the quest (or holding a Call).
The invitation itself carries no native lifetime (none exists in
either binary); the port composes one long enough for the whole
encounter below.

Inside, Kerberos appears at once, 20 to 100 from the party, stands
silent for two seconds, speaks its challenge, and the battle starts
five seconds after that, on the same clock Rahid's programme runs:
warnings with ten and five minutes left, at twenty minutes the boss
laughs and is gone, and ten seconds later the layer returns everyone. The kill is party business: every eligible
resident in the world is credited, the Call is spent, and the room
keeps a sixty-second grace before returning the party. A member whose
party standing breaks mid-fight (leaving, or a leadership change that
names them) is sent out with the instance-condition line, as the
native party-change handler does.

The once-a-day re-grant stamp rides the persisted quest-supply day
under this quest's own reference id, written when a Call is granted
(the native stamps a broader set of talks; the grant days are the only
ones the gate can observe). State the native keeps per character and
per world lives in the layer here, the layer being the only world the
summon can happen in. Dialogue and notice lines carry the authored
symbols in the native order; as with Rahid 6 they arrive as banners
until a say plane exists.

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
	easteu20QuestCodename  = "QNO_EU_EASTEU_20"
	easteu21QuestCodename  = "QNO_EU_EASTEU_21"
	easteu21WitchCodename  = "NPC_EU_WITCH"
	easteu20ItemCodename   = "ITEM_QNO_EU_EASTEU_20_01"
	easteu21CallCodename   = "ITEM_QNO_EU_EASTEU_21_01"
	easteu21RewardItemCode = "ITEM_QNO_EU_EASTEU_21_02"
	easteu21BossCodename   = "MOB_QT_01_KERBEROS"

	// easteu21Definition is INS_QNO_EU_EASTEU_21 in the shipped
	// gameworld table: layer limit 100, player limit 8.
	easteu21Definition = instance.DefinitionID(11)

	// easteu21GrantPrefix marks the dialogue token of the Witch's
	// re-grant row (AdvanceNpcQuest routes it).
	easteu21GrantPrefix = "easteu21:"

	// Encounter timing, read off the native programme: the layer
	// stands empty for 30,000 ms after it opens, then Kerberos is
	// named at the summon, stands silent for 2,000 ms, and speaks
	// its challenge; the battle clock runs from the battle's
	// opening 5,000 ms later - 600,000 ms to the first warning
	// with 300,000 ms steps, the boss leaves at 1,200,000 ms, and
	// the layer's own end is 1,210,000 ms on the same clock (a
	// 10,000 ms tail). The kill leaves a 60,000 ms grace.
	easteu21SpawnDelayMs     = 30_000
	easteu21ChallengeDelayMs = 2_000
	easteu21ActivateDelayMs  = 5_000
	easteu21BattleMs         = 600_000
	easteu21WarningStepMs    = 300_000
	easteu21BossLeavesMs     = 1_200_000
	easteu21LayerEndMs       = 1_210_000
	easteu21KillGraceMs      = 60_000

	// No native invitation lifetime exists in either binary for
	// this summon (the definition row carries none and the entry
	// handlers show no timing arithmetic). Authored, not native:
	// the challenge beat plus the encounter's own span plus one
	// authored minute for slow answers and re-entries; the
	// 30,000 ms summon phase draws on that same minute, which
	// still leaves the deadline past the layer's own end. The
	// deadline is only the lane's backstop, the programme ends
	// the layer itself on the kill or the clock.
	easteu21InviteSlackMs = 60_000
	easteu21InviteLifeMs  = easteu21ChallengeDelayMs + easteu21LayerEndMs + easteu21InviteSlackMs

	// The plaza reach: the native use validates its plaza point
	// through the world search with a 600 budget.
	easteu21PlazaRadius = 600.0

	// Kerberos appears 20 to 100 from the party: the native polar
	// random placement, the quest-spawn radius pair.
	easteu21SpawnRadiusMin  = 20
	easteu21SpawnRadiusSpan = 80
)

// easteu21PlazaSpawn is the Jupiter Temple plaza point the native use
// names (region 24838, read off the native location): the party leader
// must stand within reach of it to use the Call, and an invited party
// lands there in the layer, which duplicates the plaza's region space.
var easteu21PlazaSpawn = simulation.Spawn{RegionID: 24838, X: 886, Y: 8, Z: 854, Angle: 0}

// EASTEU 21 dialogue and notice symbols (SN_TALK_QNO_EU_EASTEU_21_*).
const (
	easteu21SymOffer       = "SN_TALK_QNO_EU_EASTEU_21_01"
	easteu21SymAccept      = "SN_TALK_QNO_EU_EASTEU_21_02"
	easteu21SymDeny        = "SN_TALK_QNO_EU_EASTEU_21_03"
	easteu21SymNotAchieved = "SN_TALK_QNO_EU_EASTEU_21_04"
	easteu21SymBagFull     = "SN_TALK_QNO_EU_EASTEU_21_05"
	easteu21SymComplete    = "SN_TALK_QNO_EU_EASTEU_21_06"
	easteu21SymGranted     = "SN_TALK_QNO_EU_EASTEU_21_07"
	easteu21SymNotShared   = "SN_TALK_QNO_EU_EASTEU_21_08"
	easteu21SymNotLeader   = "SN_TALK_QNO_EU_EASTEU_21_09"
	easteu21SymWrongPlace  = "SN_TALK_QNO_EU_EASTEU_21_10"
	easteu21SymRegrant     = "SN_TALK_QNO_EU_EASTEU_21_11"
	easteu21SymReceiveRow  = "SN_TALK_QNO_EU_EASTEU_21_12"
	easteu21SymBossShout   = "SN_TALK_QNO_EU_EASTEU_21_14"
	easteu21SymBossDeath   = "SN_TALK_QNO_EU_EASTEU_21_15"
	easteu21SymWarn10      = "SN_TALK_QNO_EU_EASTEU_21_16"
	easteu21SymWarn5       = "SN_TALK_QNO_EU_EASTEU_21_17"
	easteu21SymAchievedNow = "SN_TALK_QNO_EU_EASTEU_21_18"
	easteu21SymBossLeaves  = "SN_TALK_QNO_EU_EASTEU_21_19"
	easteu21SymNoPartyMode = "SN_TALK_QNO_EU_EASTEU_21_20"
	easteu21SymBattleBegun = "SN_TALK_QNO_EU_EASTEU_21_21"
	easteu21SymTransport   = "SN_TALK_QNO_EU_EASTEU_21_22"
	easteu21SymCondition   = "UIIT_MSG_QUEST_INS_CONDITION"
)

var easteu21Specs = []QuestSpec{
	{
		// Cerberus 3 (compiled row: words 0x130..0x13f, overrides
		// 0x58/0x5c/0x60/0xac plus the item-use hook at +0x4, prereq
		// 0x102, completions +0x2d = 1, minimum level 20 off the
		// questdata row). The objective the shipped contents line
		// counts is the Kerberos kill; the Call of Hell is the
		// programme's own key - granted at acceptance, spent by the
		// kill - and no inventory count of it completes anything.
		// The v1.150 reward text pays 28,300 EXP and a token item.
		Codename: easteu21QuestCodename, RequiredQuests: []string{easteu20QuestCodename},
		KindByte: 1, Objective: ObjectiveKill, KillMonsterCodenames: []string{easteu21BossCodename}, KillCount: 1,
		MaxCompletions:   1,
		StartNpcCodename: easteu21WitchCodename, EndNpcCodename: easteu21WitchCodename,
		OfferPromptSymbol: easteu21SymOffer, AcceptResponseSymbol: easteu21SymAccept,
		DenyResponseSymbol: easteu21SymDeny, NotAchievedSymbol: easteu21SymNotAchieved,
		CompletePromptSymbol: easteu21SymComplete, InventoryFullSymbol: easteu21SymBagFull,
		AchievedNowSymbol: easteu21SymAchievedNow,
		RewardExp:         28300,
		RewardItems:       []RewardItemLead{{ItemCodename: easteu21RewardItemCode, Count: 1}},
	},
}

/*
================
easteu21Layer

One open Cerberus summon, keyed by its world. BattleMs is when the
battle opened (after the shout), zero until then; the kill leaves a
grace deadline instead of closing at once.
================
*/
type easteu21Layer struct {
	division    string
	summoner    string
	residents   map[string]bool
	lease       instance.Lease
	openedMs    int64
	spawnedMs   int64
	shouted     bool
	battleMs    int64
	battleBegun bool
	warnings    int
	bossGone    bool
	killed      bool
	credited    bool
	graceEnd    int64
	ended       bool
	// bossSite is where the boss fell (NoteKillSite); the exit gate
	// stands beside it.
	bossSite      simulation.Spawn
	bossSiteKnown bool
}

/*
================
easteu21Programme

The programme's runtime state: one layer per open world, the leases
the invitations returned, and which leader already holds a summon.
Process-local, like the leases themselves.
================
*/
type easteu21Programme struct {
	mu       sync.Mutex
	layers   map[instance.ID]*easteu21Layer
	leases   map[instance.ID]instance.Lease
	summoner map[string]instance.ID
}

/*
================
easteu21Now

The programme clock: milliseconds on the runtime clock, zero when
unwired (detached tests then see no elapsed time and nothing fires).
================
*/
func (rt *Runtime) easteu21Now() int64 {
	if rt.NowMs == nil {
		return 0
	}
	return rt.NowMs()
}

/*
================
RegisterEasteu21Programme

Subscribes the EASTEU 21 programme to its instance definition's
lifecycle (framework step 5). Death goes unregistered: the kill
reaches the programme through the quest kill updater, which knows
the monster and the killer's world.
================
*/
func (rt *Runtime) RegisterEasteu21Programme() {
	rt.RegisterInstanceHandler(easteu21Definition, InstanceHandler{
		Setup:        rt.easteu21Setup,
		Enter:        rt.easteu21Enter,
		Leave:        rt.easteu21Leave,
		Update:       rt.easteu21Update,
		Activated:    rt.easteu21Activated,
		PartyChanged: rt.easteu21PartyChanged,
	})
}

/*
================
Easteu21MemberEligible

The invitation predicate the native entry states: members doing this
quest, having finished it, or holding a Call of Hell may come.
================
*/
func (rt *Runtime) Easteu21MemberEligible(c *enterworld.Character) bool {
	def, ok := rt.Defs.ByCodename(easteu21QuestCodename)
	if !ok || c == nil {
		return false
	}
	return activeQuestIndex(c, def.RefID) >= 0 || questCompleted(c, def.RefID) ||
		captureItemCount(c, easteu21CallCodename) > 0
}

/*
================
easteu21AcceptanceItems

One Call of Hell, granted on acceptance (the Witch's gather contract),
only for this quest.
================
*/
func easteu21AcceptanceItems(def *Definition) []inventory.ItemAmount {
	if def.Codename != easteu21QuestCodename {
		return nil
	}
	return []inventory.ItemAmount{{Codename: easteu21CallCodename, Count: 1}}
}

/*
================
easteu21SharedParty

The offer and entry condition the class checks twice over: in a
party, and the party in shared (experience and item) mode. Leadership
is a separate, later gate.
================
*/
func (rt *Runtime) easteu21SharedParty(c *enterworld.Character) bool {
	if rt.InstancePartyFacts == nil {
		return false
	}
	facts := rt.InstancePartyFacts(c)
	return facts.Found && facts.InParty && facts.Shared
}

/*
================
easteu21NoPartyOption

The Witch's offer gate: EASTEU 21 is only ever offered to a shared
party; anyone else hears the come-back-with-a-party line in the
offer's place (the class's offer condition).
================
*/
func (rt *Runtime) easteu21NoPartyOption(c *enterworld.Character, def *Definition, npcCodename string, level int64) (NpcOption, bool) {
	if def.Codename != easteu21QuestCodename || admissionLevel(def) > level ||
		!canAcceptAgain(c, def) || !prerequisitesMet(c, def) ||
		!rt.calendarAvailable(def, false) || !questNpcMatches(def, def.StartNpcCodename, npcCodename) ||
		rt.easteu21SharedParty(c) {
		return NpcOption{}, false
	}
	return NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: easteu21SymNoPartyMode, Informational: true}, true
}

/*
================
easteu21GrantedToday

Whether a Call was already granted (at acceptance or by re-grant) on
the current calendar day: the persisted quest-supply day under this
quest's reference id (capture supplies never read this quest's entry -
it has no supply row).
================
*/
func (rt *Runtime) easteu21GrantedToday(c *enterworld.Character, def *Definition) bool {
	state, exists := c.QuestSupplies[def.RefID]
	return exists && state.Day == rt.CalendarNow().Day
}

/*
================
easteu21GrantCall

Hands one Call of Hell to a holder who has none, and stamps the
grant day. A full bag answers with the quest's bag-full line.
================
*/
func (rt *Runtime) easteu21GrantCall(c *enterworld.Character, def *Definition) error {
	if rt.PlanInventory == nil {
		return fmt.Errorf("quest inventory owner unavailable")
	}
	var refusal error
	day := rt.CalendarNow().Day
	changed := rt.deps.Update(c, "easteu21-grant", func() bool {
		if c == nil || c.DeletePending || activeQuestIndex(c, def.RefID) < 0 {
			refusal = fmt.Errorf("quest %s is not active", easteu21QuestCodename)
			return false
		}
		if captureItemCount(c, easteu21CallCodename) > 0 {
			return true
		}
		rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: easteu21CallCodename, Count: 1}})
		if err != nil {
			refusal = inventoryRefusal(def, err)
			return false
		}
		c.MissionInventory = rows
		setCaptureSupply(c, def.RefID, day, false)
		return true
	})
	if refusal != nil {
		return refusal
	}
	if !changed {
		return fmt.Errorf("grant character no longer authoritative")
	}
	return nil
}

/*
================
easteu21GrantOption

The Witch's re-grant row, offered while the quest runs unachieved at
the Witch, the holder has no Call, and none was granted today. It
takes the plain not-achieved row's place (its prompt is the re-grant
speech); choosing it runs easteu21's grant in advanceEasteu21Grant.
================
*/
func (rt *Runtime) easteu21GrantOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	if def.Codename != easteu21QuestCodename || npc != easteu21WitchCodename {
		return NpcOption{}, false
	}
	at := activeQuestIndex(c, def.RefID)
	if at < 0 || objectiveMet(c, def, c.ActiveQuests[at]) {
		return NpcOption{}, false
	}
	if captureItemCount(c, easteu21CallCodename) > 0 || rt.easteu21GrantedToday(c, def) {
		return NpcOption{}, false
	}
	return NpcOption{
		Codename:     easteu21GrantPrefix + def.Codename,
		TitleSymbol:  easteu21SymReceiveRow,
		PromptSymbol: easteu21SymRegrant,
		Complete:     true,
	}, true
}

/*
================
advanceEasteu21Grant

The confirmed re-grant row: one Call of Hell, the grant day stamped,
and the Witch's where-to-use-it banner.
================
*/
func (rt *Runtime) advanceEasteu21Grant(c *enterworld.Character, npc string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(easteu21QuestCodename)
	if !ok || npc != easteu21WitchCodename {
		return OpResult{}, fmt.Errorf("quest %s wrong grant NPC", easteu21QuestCodename)
	}
	if activeQuestIndex(c, def.RefID) < 0 {
		return OpResult{}, fmt.Errorf("quest %s is not active", easteu21QuestCodename)
	}
	if err := rt.easteu21GrantCall(c, def); err != nil {
		return OpResult{}, err
	}
	return OpResult{Frames: []wire.Frame{questNotification(easteu21SymGranted)}}, nil
}

/*
================
stampEasteu21Acceptance

Records the acceptance-day grant: a Call lost the same day is not
re-granted until tomorrow (the native talk-after stamp, observed at
the grant). Called from the quest acceptance composition.
================
*/
func (rt *Runtime) stampEasteu21Acceptance(c *enterworld.Character, def *Definition) {
	setCaptureSupply(c, def.RefID, rt.CalendarNow().Day, false)
}

/*
================
useEasteu21Call

The Call of Hell, used at the Jupiter Temple plaza (BeginItemUse's
handler; the caller never consumes it - the native use only opens the
way). Native gate order: the place (within 600 of the plaza point),
a shared party, leadership; then the invitation. A summoned trade
transport refuses the leader inside the entry check, as it refuses
each member inside theirs.
================
*/
func (rt *Runtime) useEasteu21Call(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if c.World != nil && c.World.InstanceReturn != nil {
		// Already standing in a layer: the native use reads only the
		// open world, and silently.
		return nil, false
	}
	if !withinQuestArea(at, easteu21PlazaSpawn, easteu21PlazaRadius) {
		return []wire.Frame{questNotification(easteu21SymWrongPlace)}, false
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return nil, false
	}
	facts := rt.InstancePartyFacts(c)
	switch {
	case !facts.Found:
		return nil, false
	case !facts.InParty || !facts.Shared:
		return []wire.Frame{questNotification(easteu21SymNotShared)}, false
	case !facts.Leader:
		return []wire.Frame{questNotification(easteu21SymNotLeader)}, false
	case facts.Transported:
		return []wire.Frame{questNotification(easteu21SymTransport)}, false
	}
	if rt.easteu21.summonedBy(c.Name) {
		return nil, false
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: easteu21Definition,
		Entry:      easteu21PlazaSpawn,
		DeadlineMs: rt.easteu21Now() + easteu21InviteLifeMs,
		Eligible:   rt.Easteu21MemberEligible,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		// A layer that would not open: the native use is silent. A
		// layer that opened without the leader landing remembers no
		// lease here; it lives on the framework's deadline handling
		// alone, and no second summon is held against the leader.
		return nil, false
	}
	rt.easteu21.rememberLease(outcome.Lease, c.Name)
	return nil, false
}

/*
================
summonedBy

Whether this leader already holds an open summon.
================
*/
func (p *easteu21Programme) summonedBy(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, open := p.summoner[name]
	return open
}

/*
================
rememberLease

Keeps the invitation's lease and its summoner under the world the
layer opened as. The setup event may already have been delivered;
the layer adopts the lease at its next update.
================
*/
func (p *easteu21Programme) rememberLease(lease instance.Lease, summoner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leases == nil {
		p.leases = make(map[instance.ID]instance.Lease)
	}
	if p.summoner == nil {
		p.summoner = make(map[string]instance.ID)
	}
	p.leases[lease.ID] = lease
	p.summoner[summoner] = lease.ID
}

/*
================
layerFor

The layer state for one world, created on first sight (setup, or an
enter that raced it).
================
*/
func (rt *Runtime) easteu21LayerFor(event InstanceEvent) *easteu21Layer {
	p := &rt.easteu21
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.layers == nil {
		p.layers = make(map[instance.ID]*easteu21Layer)
	}
	layer := p.layers[event.World]
	if layer == nil {
		layer = &easteu21Layer{
			division:  event.Division,
			residents: make(map[string]bool),
			openedMs:  rt.easteu21Now(),
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
func (rt *Runtime) easteu21ForgetLayer(world instance.ID) {
	p := &rt.easteu21
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
easteu21Setup

A layer opened: start its encounter clock.
================
*/
func (rt *Runtime) easteu21Setup(event InstanceEvent) {
	rt.easteu21LayerFor(event)
}

/*
================
easteu21Enter

A resident arrived. The first through the door is the summoner (the
invitation moves the leader in before any member answers).
================
*/
func (rt *Runtime) easteu21Enter(event InstanceEvent) {
	layer := rt.easteu21LayerFor(event)
	rt.easteu21.mu.Lock()
	layer.residents[event.Name] = true
	if layer.summoner == "" {
		layer.summoner = event.Name
	}
	rt.easteu21.mu.Unlock()
}

/*
================
easteu21Leave

A resident returned. When the last one leaves a summon that is not
already ending, the room closes behind them instead of idling to
its deadline.
================
*/
func (rt *Runtime) easteu21Leave(event InstanceEvent) {
	rt.easteu21.mu.Lock()
	layer := rt.easteu21.layers[event.World]
	rt.easteu21.mu.Unlock()
	if layer == nil {
		return
	}
	rt.easteu21.mu.Lock()
	delete(layer.residents, event.Name)
	empty := len(layer.residents) == 0
	ended := layer.ended
	lease := layer.lease
	if lease.ID == 0 {
		lease = rt.easteu21.leases[event.World]
	}
	rt.easteu21.mu.Unlock()
	if !empty || ended {
		return
	}
	rt.easteu21ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}

/*
================
easteu21Activated

The post-entry checkpoint: a resident who is neither doing nor has
finished this quest, and holds no Call, does not belong to the summon -
they hear the instance-condition line and are returned.
================
*/
func (rt *Runtime) easteu21Activated(event InstanceEvent) {
	character := rt.easteu21Character(event.Division, event.Name)
	if character == nil || rt.Easteu21MemberEligible(character) {
		return
	}
	rt.easteu21PushOne(event.Name, easteu21SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
easteu21PartyChanged

A resident's party standing broke mid-summon (they left, or a
leadership change named them): the native handler answers with the
instance-condition line and sends them out.
================
*/
func (rt *Runtime) easteu21PartyChanged(event InstanceEvent) {
	rt.easteu21.mu.Lock()
	layer := rt.easteu21.layers[event.World]
	inside := layer != nil && layer.residents[event.Name]
	rt.easteu21.mu.Unlock()
	if !inside {
		return
	}
	character := rt.easteu21Character(event.Division, event.Name)
	if character == nil {
		return
	}
	rt.easteu21PushOne(event.Name, easteu21SymCondition)
	if rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
}

/*
================
easteu21Character

One division character by name, from the character source.
================
*/
func (rt *Runtime) easteu21Character(division, name string) *enterworld.Character {
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

/*
================
easteu21PushOne

One quest-notification frame to one character, when the push door
is wired.
================
*/
func (rt *Runtime) easteu21PushOne(name, symbol string) {
	if rt.PushCharacterFrames == nil || name == "" {
		return
	}
	rt.PushCharacterFrames(name, []wire.Frame{questNotification(symbol)})
}

/*
================
easteu21Push

One notice to every resident, in name order so a test (and a log)
reads the same sequence every time.
================
*/
func (rt *Runtime) easteu21Push(layer *easteu21Layer, symbol string) {
	for _, name := range easteu21ResidentNames(layer) {
		rt.easteu21PushOne(name, symbol)
	}
}

/*
================
easteu21ResidentNames

A layer's residents, sorted.
================
*/
func easteu21ResidentNames(layer *easteu21Layer) []string {
	names := make([]string, 0, len(layer.residents))
	for name := range layer.residents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

/*
================
noteEasteu21Kill

Kerberos fell: mark its layer. Called from the quest kill updater,
which runs inside the killer's character transaction - so this only
records; the credit runs at the layer's next update, lock-free.
================
*/
func (rt *Runtime) noteEasteu21Kill(c *enterworld.Character, codename string) {
	if codename != easteu21BossCodename || c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != easteu21Definition {
		return
	}
	rt.easteu21.mu.Lock()
	if layer := rt.easteu21.layers[world]; layer != nil && !layer.bossGone {
		layer.killed = true
	}
	rt.easteu21.mu.Unlock()
}

/*
================
noteEasteu21BossSite

Where Kerberos fell in its layer (NoteKillSite, just before the kill
note): the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteEasteu21BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != easteu21BossCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != easteu21Definition {
		return
	}
	rt.easteu21.mu.Lock()
	if layer := rt.easteu21.layers[world]; layer != nil && !layer.killed && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.easteu21.mu.Unlock()
}

/*
================
easteu21Update

The encounter clock. One pass per layer tick; elapsed time comes
from the runtime clock, never from counting ticks. Kerberos is
named at the summon and stands silent for two seconds before its
challenge; the battle clock runs from the challenge, the battle
opens five seconds in, warnings land with ten and five minutes
left, the boss leaves at twenty minutes, and the layer's own end
returns the room; a kill instead leaves a sixty-second grace
after its credit.
================
*/
func (rt *Runtime) easteu21Update(event InstanceEvent) {
	layer := rt.easteu21LayerFor(event)
	rt.easteu21.mu.Lock()
	if layer.lease.ID == 0 {
		layer.lease = rt.easteu21.leases[event.World]
	}
	killed := layer.killed && !layer.credited
	if killed {
		layer.credited = true
	}
	ended := layer.ended
	rt.easteu21.mu.Unlock()
	if ended {
		return
	}
	now := rt.easteu21Now()
	if killed {
		rt.easteu21FinishKill(event, layer, now)
		return
	}
	if layer.killed {
		if now >= layer.graceEnd {
			rt.easteu21EndLayer(event, layer)
		}
		return
	}
	if layer.spawnedMs == 0 {
		if now-layer.openedMs < easteu21SpawnDelayMs {
			return
		}
		// Anchored at the phase's exact end, not at the tick that
		// noticed it, like every clock below.
		layer.spawnedMs = layer.openedMs + easteu21SpawnDelayMs
		rt.easteu21SpawnKerberos(event.Division, layer)
		return
	}
	if !layer.shouted {
		if now-layer.spawnedMs >= easteu21ChallengeDelayMs {
			// The battle clock is anchored at the battle's opening
			// (the challenge plus its 5,000 ms), not at the challenge
			// itself and not at the tick that noticed it.
			layer.shouted = true
			layer.battleMs = layer.spawnedMs + easteu21ChallengeDelayMs + easteu21ActivateDelayMs
			rt.easteu21Push(layer, easteu21SymBossShout)
		}
		return
	}
	if !layer.battleBegun {
		if now >= layer.battleMs {
			layer.battleBegun = true
			rt.easteu21Push(layer, easteu21SymBattleBegun)
		}
		return
	}
	switch elapsed := now - layer.battleMs; {
	case layer.bossGone:
		if elapsed >= easteu21LayerEndMs {
			rt.easteu21EndLayer(event, layer)
		}
	case layer.warnings == 0 && elapsed >= easteu21BattleMs:
		layer.warnings = 1
		rt.easteu21Push(layer, easteu21SymWarn10)
	case layer.warnings == 1 && elapsed >= easteu21BattleMs+easteu21WarningStepMs:
		layer.warnings = 2
		rt.easteu21Push(layer, easteu21SymWarn5)
	case elapsed >= easteu21BossLeavesMs:
		layer.bossGone = true
		rt.easteu21Push(layer, easteu21SymBossLeaves)
	}
}

/*
================
easteu21SpawnKerberos

Places Kerberos 20 to 100 from a resident (the summoner when they
are still inside) in the layer's population, inside that resident's
character door, the quest-spawn contract.
================
*/
func (rt *Runtime) easteu21SpawnKerberos(division string, layer *easteu21Layer) {
	if rt.SpawnQuestMonster == nil {
		return
	}
	host := layer.summoner
	if !layer.residents[host] {
		names := easteu21ResidentNames(layer)
		if len(names) == 0 {
			return
		}
		host = names[0]
	}
	character := rt.easteu21Character(division, host)
	if character == nil {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division:   division,
		Population: layer.lease,
		Codename:   easteu21BossCodename,
		RadiusMin:  easteu21SpawnRadiusMin,
		RadiusSpan: easteu21SpawnRadiusSpan,
		NowMs:      rt.easteu21Now(),
	}
	rt.deps.Update(character, "easteu21-spawn", func() bool {
		return rt.SpawnQuestMonster(character, request)
	})
}

/*
================
easteu21FinishKill

Kerberos is down. Its death line goes to the room; then every
resident doing the quest is credited with the kill and their Call
is spent - the whole party, the dead included, exactly the native
kill evaluator - the exit gate rises beside it, and the room's
sixty-second grace starts.
================
*/
func (rt *Runtime) easteu21FinishKill(event InstanceEvent, layer *easteu21Layer, now int64) {
	rt.easteu21Push(layer, easteu21SymBossDeath)
	layer.graceEnd = now + easteu21KillGraceMs
	// The reference handler raises the exit gate beside Kerberos, then
	// sets the sixty-second event timer: the gate lets the room leave
	// early, the timer still returns whoever stays.
	rt.easteu21.mu.Lock()
	site, known := layer.bossSite, layer.bossSiteKnown
	rt.easteu21.mu.Unlock()
	if known && rt.SpawnExitGate != nil {
		rt.SpawnExitGate(event.World, site)
	}
	def, ok := rt.Defs.ByCodename(easteu21QuestCodename)
	if ok {
		for _, name := range easteu21ResidentNames(layer) {
			rt.easteu21CreditKill(event.Division, name, def)
		}
	}
}

/*
================
easteu21CreditKill

One resident's kill credit: the kill mission advances and every
Call they hold is spent, atomically. A resident not doing the quest
(a finished helper, or a Call holder without it) is untouched.
================
*/
func (rt *Runtime) easteu21CreditKill(division, name string, def *Definition) {
	character := rt.easteu21Character(division, name)
	if character == nil {
		return
	}
	var frames []wire.Frame
	rt.deps.Update(character, "easteu21-kill-credit", func() bool {
		if character.DeletePending {
			return false
		}
		at := activeQuestIndex(character, def.RefID)
		if at < 0 {
			return false
		}
		updated, changed := refreshMissions(character, def, character.ActiveQuests[at], easteu21BossCodename, 0)
		if changed {
			frames = append(frames, missionProgressFrames(def, character.ActiveQuests[at], updated)...)
			character.ActiveQuests[at] = updated
		}
		if rt.PlanInventory != nil {
			if held := captureItemCount(character, easteu21CallCodename); held > 0 {
				rows, updates, err := rt.PlanInventory(character,
					[]inventory.ItemAmount{{Codename: easteu21CallCodename, Count: held}}, nil)
				if err == nil {
					character.MissionInventory = rows
					frames = append(frames, updates...)
				}
			}
		}
		return changed || len(frames) > 0
	})
	if len(frames) > 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(name, frames)
	}
}

/*
================
easteu21EndLayer

Returns every resident to their anchor and closes the layer; the
close evacuates anyone the returns missed, so nobody is stranded.
================
*/
func (rt *Runtime) easteu21EndLayer(event InstanceEvent, layer *easteu21Layer) {
	rt.easteu21.mu.Lock()
	if layer.ended {
		rt.easteu21.mu.Unlock()
		return
	}
	layer.ended = true
	lease := layer.lease
	rt.easteu21.mu.Unlock()
	for _, name := range easteu21ResidentNames(layer) {
		if character := rt.easteu21Character(event.Division, name); character != nil && rt.InstanceReturn != nil {
			rt.InstanceReturn(character)
		}
	}
	rt.easteu21ForgetLayer(event.World)
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(event.World)
	}
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}
