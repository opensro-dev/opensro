/*
===========================================================================

ivy3.go - IVY 3 (Ivy 2): Ratchel's key to the Evil Order Fortress

The Ivy chain ends in an instanced summon, the third quest programme
on the instanced-world framework (#321 step 5), after Rahid 6 and
EASTEU 21 - and the first whose key is assembled rather than handed
over. The native shape, read off the reference quest class's own
handlers:

General Ratchel offers the quest only to a member of an
experience-shared, item-shared party (anyone else hears the
come-back-with-a-party line); accepting hands over five Horn Trumpets
and stamps the day, and a holder who has used them all can be
re-granted five, once per calendar day. A Horn used in one of the
three areas Ivy marked (the Amphitheater, Cleopatra's Gate, Haran's
Tower) calls that area's sentry leader for five minutes; its death
leaves the area's sign with the killer who lacks both the sign and
the finished key. Field Punishers of the Amphitheater bleed
Punisher's Blood (the shipped mission's five percent); using the
Blood while all three signs are held combines them into the key.

The party leader uses the key at the Evil Order Fortress point: the
use gates, in native order, are the place itself (within 600 of the
fortress point), a shared party, and leadership, with a summoned
trade transport refusing its holder. The invitation then opens
instance definition 12 for the members doing or having finished the
quest (or holding a key). Native state the native keeps per
character and per world lives in the layer here.

Inside, Ivy appears 30,000 ms after the layer opens, 20 to 100 from
the party, trades three lines five seconds apart starting two
seconds in, and the battle opens five seconds after the last: the
same battle clock EASTEU 21 runs - warnings with ten and five
minutes left, at twenty minutes Ivy is gone, and ten seconds later
the layer returns everyone. The kill is party business: every
eligible resident is credited with the Red Hair, the key is spent,
and the room keeps a sixty-second grace before returning the party.
A member whose party standing breaks mid-fight is sent out with the
instance-condition line, as the native party-change handler does.

The native sentry-death handler drops a sign at the feet of every
nearby party member who lacks it; the port's kill door sees only the
killer, so the sign lands with the killer. Every member can call and
kill their own sentry - the Horn's per-area check reads the user's
own held sign - so no member's road to the key is blocked. The
materials-ready line is spoken when a sign credit completes the set
of three signs and the Blood; natively it also follows the Blood's
pickup, which the port's kill door cannot observe.

Dialogue and notice lines carry the authored symbols in the native
order; as with Rahid 6 they arrive as banners until a say plane
exists. This file owns the open-world half - Ratchel's talk, the
Horn grants, the three item uses and the sentry signs; the layer
the key opens, and its clock, live in ivy3_layer.go.

===========================================================================
*/

package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	ivy1QuestCodename = "QNO_EU_IVY_1"
	ivy3QuestCodename = "QNO_EU_IVY_3"
	ivy3NpcCodename   = "NPC_EU_GENERAL"

	ivy3HornCodename  = "ITEM_QNO_EU_IVY_3_01"
	ivy3BloodCodename = "ITEM_QNO_EU_IVY_3_05"
	ivy3KeyCodename   = "ITEM_QNO_EU_IVY_3_06"
	ivy3HairCodename  = "ITEM_QNO_EU_IVY_3_08"

	ivy3BossCodename = "MOB_QT_01_IVY"
	ivy3BloodSource  = "MOB_AM_PUNISHER"

	// ivy3Definition is INS_QNO_EU_IVY_3 in the shipped gameworld
	// table: layer limit 100, player limit 8.
	ivy3Definition = instance.DefinitionID(12)

	// ivy3GrantPrefix marks the dialogue token of Ratchel's
	// re-grant row (AdvanceNpcQuest routes it).
	ivy3GrantPrefix = "ivy3:"

	// The Horn grant, at acceptance and at the daily re-grant.
	ivy3HornGrant = 5

	// Encounter timing, read off the native programme: the layer
	// stands empty for 30,000 ms after it opens, then Ivy is named
	// at the summon, speaks her threat 2,000 ms later, and trades
	// lines every 5,000 ms; the battle clock runs from the battle's
	// opening - 600,000 ms to the first warning with 300,000 ms
	// steps, Ivy leaves at 1,200,000 ms, and the layer's own end is
	// 1,210,000 ms on the same clock (a 10,000 ms tail). The kill
	// leaves a 60,000 ms grace.
	ivy3SpawnDelayMs  = 30_000
	ivy3FirstLineMs   = 2_000
	ivy3LineStepMs    = 5_000
	ivy3BattleMs      = 600_000
	ivy3WarningStepMs = 300_000
	ivy3BossLeavesMs  = 1_200_000
	ivy3LayerEndMs    = 1_210_000
	ivy3KillGraceMs   = 60_000

	// The native key use hands the layer a 1,260,000 ms lifetime and
	// a 60,000 ms entry window. The invitation deadline composes
	// both; the deadline is only the lane's backstop, the programme
	// ends the layer itself on the kill or the clock.
	ivy3InviteLifeMs  = 1_260_000
	ivy3InviteSlackMs = 60_000

	// A called sentry leader stands for 300,000 ms, then is gone
	// (the native spawn's timing handler).
	ivy3SentryLifeMs = 300_000

	// The fortress reach: the native key use validates its fortress
	// point through the world search with a 600 budget.
	ivy3FortressRadius = 600.0

	// Ivy and the sentries appear 20 to 100 from the named point:
	// the native polar random placement, the quest-spawn radius pair.
	ivy3SpawnRadiusMin  = 20
	ivy3SpawnRadiusSpan = 80
)

// ivy3FortressSpawn is the Evil Order Fortress point the native key
// use names (region 25183, read off the native location): the party
// leader must stand within reach of it to use the key, and an invited
// party lands there in the layer, which duplicates the fortress's
// region space.
var ivy3FortressSpawn = simulation.Spawn{RegionID: 25183, X: 1882, Y: -169, Z: 632, Angle: 0}

// IVY 3 dialogue and notice symbols (SN_TALK_QNO_EU_IVY_3_*).
const (
	ivy3SymOffer          = "SN_TALK_QNO_EU_IVY_3_01"
	ivy3SymAccept         = "SN_TALK_QNO_EU_IVY_3_02"
	ivy3SymDeny           = "SN_TALK_QNO_EU_IVY_3_03"
	ivy3SymNotAchieved    = "SN_TALK_QNO_EU_IVY_3_04"
	ivy3SymBagFull        = "SN_TALK_QNO_EU_IVY_3_05"
	ivy3SymComplete       = "SN_TALK_QNO_EU_IVY_3_06"
	ivy3SymMaterialsReady = "SN_TALK_QNO_EU_IVY_3_07"
	ivy3SymKeyNotShared   = "SN_TALK_QNO_EU_IVY_3_08"
	ivy3SymKeyNotLeader   = "SN_TALK_QNO_EU_IVY_3_09"
	ivy3SymKeyWrongPlace  = "SN_TALK_QNO_EU_IVY_3_10"
	ivy3SymBossThreat     = "SN_TALK_QNO_EU_IVY_3_11"
	ivy3SymPlayerPlea     = "SN_TALK_QNO_EU_IVY_3_12"
	ivy3SymBossRefusal    = "SN_TALK_QNO_EU_IVY_3_13"
	ivy3SymWarn10         = "SN_TALK_QNO_EU_IVY_3_14"
	ivy3SymWarn5          = "SN_TALK_QNO_EU_IVY_3_15"
	ivy3SymRegrant        = "SN_TALK_QNO_EU_IVY_3_16"
	ivy3SymRegrantRow     = "SN_TALK_QNO_EU_IVY_3_17"
	ivy3SymGranted        = "SN_TALK_QNO_EU_IVY_3_18"
	ivy3SymRegrantWait    = "SN_TALK_QNO_EU_IVY_3_19"
	ivy3SymAchievedNow    = "SN_TALK_QNO_EU_IVY_3_20"
	ivy3SymBossDeath      = "SN_TALK_QNO_EU_IVY_3_21"
	ivy3SymBossLeaves     = "SN_TALK_QNO_EU_IVY_3_22"
	ivy3SymNoParty        = "SN_TALK_QNO_EU_IVY_3_23"
	ivy3SymBattleBegun    = "SN_TALK_QNO_EU_IVY_3_24"
	ivy3SymHornWrongPlace = "SN_TALK_QNO_EU_IVY_3_25"
	ivy3SymHornNotShared  = "SN_TALK_QNO_EU_IVY_3_26"
	ivy3SymKeyTransport   = "SN_TALK_QNO_EU_IVY_3_27"
	ivy3SymCondition      = "UIIT_MSG_QUEST_INS_CONDITION"
)

/*
================
ivy3HornArea

One of the three areas Ivy marked: the point the Horn validates, the
sentry leader it calls there, and the sign that leader's death
leaves. The records stand in the native order.
================
*/
type ivy3HornArea struct {
	point   simulation.Spawn
	radius  float64
	monster string
	sign    string
}

var ivy3HornAreas = []ivy3HornArea{
	{point: simulation.Spawn{RegionID: 27237, X: 1057, Y: 218, Z: 634}, radius: 1000,
		monster: "MOB_QT_01_PUNISHER", sign: "ITEM_QNO_EU_IVY_3_04"},
	{point: simulation.Spawn{RegionID: 25922, X: 1661, Y: 180, Z: 210}, radius: 700,
		monster: "MOB_QT_03_PUNISHER_CLON", sign: "ITEM_QNO_EU_IVY_3_03"},
	{point: simulation.Spawn{RegionID: 25187, X: 646, Y: 182, Z: 204}, radius: 600,
		monster: "MOB_QT_01_SOLDIER_CLON", sign: "ITEM_QNO_EU_IVY_3_02"},
}

// ivy3SignCodenames is the key's material set, in area order.
var ivy3SignCodenames = []string{"ITEM_QNO_EU_IVY_3_04", "ITEM_QNO_EU_IVY_3_03", "ITEM_QNO_EU_IVY_3_02"}

var ivy3Specs = []QuestSpec{
	{
		// Ivy 2 (compiled row: words 0x130..0x13f, overrides
		// 0x58/0x5c/0x60/0x68/0x74/0xac plus the item-use hook at
		// +0x4, prereq 0x102, completions +0x2d = 1, minimum level
		// 30 off the questdata row). The objective the shipped
		// contents line counts is the Red Hair, Ivy's own drop to
		// the credited party; the key is the programme's own -
		// assembled from the three signs and the Blood, spent by
		// the kill - and no inventory count of it completes
		// anything. The v1.150 reward text pays 40,000 EXP and
		// 4,000 skill EXP; the token stack rides the reward rows.
		Codename: ivy3QuestCodename, RequiredQuests: []string{ivy1QuestCodename},
		KindByte: 1, Objective: ObjectiveCollect, CollectItemCodename: ivy3HairCodename, CollectCount: 1,
		MaxCompletions:   1,
		StartNpcCodename: ivy3NpcCodename, EndNpcCodename: ivy3NpcCodename,
		OfferPromptSymbol: ivy3SymOffer, AcceptResponseSymbol: ivy3SymAccept,
		DenyResponseSymbol: ivy3SymDeny, NotAchievedSymbol: ivy3SymNotAchieved,
		CompletePromptSymbol: ivy3SymComplete, InventoryFullSymbol: ivy3SymBagFull,
		AchievedNowSymbol: ivy3SymAchievedNow,
		RewardExp:         40000, RewardSkillExp: 4000,
		RewardItems: []RewardItemLead{{ItemCodename: "ITEM_QNO_EU_IVY_3_07", Count: 20}},
		// Punisher's Blood: the Amphitheater Punisher's five
		// percent mission drop, one held at a time.
		MonsterDrop: &MonsterDropRule{ItemCodename: ivy3BloodCodename,
			MonsterCodenames: []string{ivy3BloodSource}, ChancePercent: 5},
	},
}

/*
================
ivy3Sentry

One called sentry leader still standing: the sign its death leaves,
and when the call lapses (the native five minutes).
================
*/
type ivy3Sentry struct {
	sign      string
	expiresMs int64
}

/*
================
ivy3Now

The programme clock: milliseconds on the runtime clock, zero when
unwired (detached tests then see no elapsed time and nothing fires).
================
*/
func (rt *Runtime) ivy3Now() int64 {
	if rt.NowMs == nil {
		return 0
	}
	return rt.NowMs()
}

/*
================
ivy3AcceptanceItems

Five Horn Trumpets, granted on acceptance (Ratchel's gather
contract), only for this quest.
================
*/
func ivy3AcceptanceItems(def *Definition) []inventory.ItemAmount {
	if def.Codename != ivy3QuestCodename {
		return nil
	}
	return []inventory.ItemAmount{{Codename: ivy3HornCodename, Count: ivy3HornGrant}}
}

/*
================
ivy3SharedParty

The offer and entry condition the class checks twice over: in a
party, and the party in shared (experience and item) mode. Leadership
is a separate, later gate.
================
*/
func (rt *Runtime) ivy3SharedParty(c *enterworld.Character) bool {
	if rt.InstancePartyFacts == nil {
		return false
	}
	facts := rt.InstancePartyFacts(c)
	return facts.Found && facts.InParty && facts.Shared
}

/*
================
ivy3NoPartyOption

Ratchel's offer gate: IVY 3 is only ever offered to a shared party;
anyone else hears the come-back-with-a-party line in the offer's
place (the class's offer condition).
================
*/
func (rt *Runtime) ivy3NoPartyOption(c *enterworld.Character, def *Definition, npcCodename string, level int64) (NpcOption, bool) {
	if def.Codename != ivy3QuestCodename || admissionLevel(def) > level ||
		!canAcceptAgain(c, def) || !prerequisitesMet(c, def) ||
		!rt.calendarAvailable(def, false) || !questNpcMatches(def, def.StartNpcCodename, npcCodename) ||
		rt.ivy3SharedParty(c) {
		return NpcOption{}, false
	}
	return NpcOption{Codename: def.Codename, TitleSymbol: def.TitleSymbol, PromptSymbol: ivy3SymNoParty, Informational: true}, true
}

/*
================
ivy3GrantedToday

Whether Horns were already granted (at acceptance or by re-grant) on
the current calendar day: the persisted quest-supply day under this
quest's reference id (capture supplies never read this quest's
entry - it has no supply row).
================
*/
func (rt *Runtime) ivy3GrantedToday(c *enterworld.Character, def *Definition) bool {
	state, exists := c.QuestSupplies[def.RefID]
	return exists && state.Day == rt.CalendarNow().Day
}

/*
================
ivy3GrantHorns

Hands five Horn Trumpets to a holder who has none, and stamps the
grant day. A full bag answers with the quest's bag-full line.
================
*/
func (rt *Runtime) ivy3GrantHorns(c *enterworld.Character, def *Definition) error {
	if rt.PlanInventory == nil {
		return fmt.Errorf("quest inventory owner unavailable")
	}
	var refusal error
	day := rt.CalendarNow().Day
	changed := rt.deps.Update(c, "ivy3-grant", func() bool {
		if c == nil || c.DeletePending || activeQuestIndex(c, def.RefID) < 0 {
			refusal = fmt.Errorf("quest %s is not active", ivy3QuestCodename)
			return false
		}
		if captureItemCount(c, ivy3HornCodename) > 0 {
			return true
		}
		rows, _, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: ivy3HornCodename, Count: ivy3HornGrant}})
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
ivy3GrantOption

Ratchel's re-grant row, offered while the quest runs unachieved, the
holder has neither Horns nor the finished key, and none were granted
today. A grant already spent today answers with the come-back-
tomorrow line in its place; holding a Horn or the key keeps the
plain not-achieved row.
================
*/
func (rt *Runtime) ivy3GrantOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	if def.Codename != ivy3QuestCodename || npc != ivy3NpcCodename {
		return NpcOption{}, false
	}
	at := activeQuestIndex(c, def.RefID)
	if at < 0 || objectiveMet(c, def, c.ActiveQuests[at]) {
		return NpcOption{}, false
	}
	if captureItemCount(c, ivy3HornCodename) > 0 || captureItemCount(c, ivy3KeyCodename) > 0 {
		return NpcOption{}, false
	}
	if rt.ivy3GrantedToday(c, def) {
		return NpcOption{
			Codename: def.Codename, TitleSymbol: def.TitleSymbol,
			PromptSymbol: ivy3SymRegrantWait, Informational: true,
		}, true
	}
	return NpcOption{
		Codename:     ivy3GrantPrefix + def.Codename,
		TitleSymbol:  ivy3SymRegrantRow,
		PromptSymbol: ivy3SymRegrant,
		Complete:     true,
	}, true
}

/*
================
advanceIvy3Grant

The confirmed re-grant row: five Horn Trumpets, the grant day
stamped, and Ratchel's get-the-mark-this-time banner.
================
*/
func (rt *Runtime) advanceIvy3Grant(c *enterworld.Character, npc string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(ivy3QuestCodename)
	if !ok || npc != ivy3NpcCodename {
		return OpResult{}, fmt.Errorf("quest %s wrong grant NPC", ivy3QuestCodename)
	}
	if activeQuestIndex(c, def.RefID) < 0 {
		return OpResult{}, fmt.Errorf("quest %s is not active", ivy3QuestCodename)
	}
	if err := rt.ivy3GrantHorns(c, def); err != nil {
		return OpResult{}, err
	}
	return OpResult{Frames: []wire.Frame{questNotification(ivy3SymGranted)}}, nil
}

/*
================
stampIvy3Acceptance

Records the acceptance-day grant: Horns used up the same day are not
re-granted until tomorrow (the native talk-after stamp, observed at
the grant). Called from the quest acceptance composition.
================
*/
func (rt *Runtime) stampIvy3Acceptance(c *enterworld.Character, def *Definition) {
	setCaptureSupply(c, def.RefID, rt.CalendarNow().Day, false)
}

/*
================
useIvy3Horn

The Horn Trumpet, blown in one of the three marked areas: the native
gate order is the shared party, then the first area whose point the
user stands within and whose sign they do not yet hold. That area's
sentry leader is called 20 to 100 from the area point and stands for
five minutes; its death leaves the sign (noteIvy3SentryKill). The
caller consumes the Horn on success.
================
*/
func (rt *Runtime) useIvy3Horn(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if rt.InstancePartyFacts == nil {
		return nil, false
	}
	facts := rt.InstancePartyFacts(c)
	if !facts.Found {
		return nil, false
	}
	if !facts.InParty || !facts.Shared {
		return []wire.Frame{questNotification(ivy3SymHornNotShared)}, false
	}
	for _, area := range ivy3HornAreas {
		if captureItemCount(c, area.sign) > 0 {
			continue
		}
		if !withinQuestArea(at, area.point, area.radius) {
			continue
		}
		request := simulation.QuestMonsterSpawn{
			Codename:      area.monster,
			Position:      area.point,
			FixedPosition: true,
			RadiusMin:     ivy3SpawnRadiusMin,
			RadiusSpan:    ivy3SpawnRadiusSpan,
			LifetimeMs:    ivy3SentryLifeMs,
			NowMs:         rt.ivy3Now(),
		}
		if rt.SpawnQuestMonster == nil || !rt.SpawnQuestMonster(c, request) {
			return nil, false
		}
		rt.ivy3.rememberSentry(area.monster, area.sign, rt.ivy3Now()+ivy3SentryLifeMs)
		return nil, true
	}
	return []wire.Frame{questNotification(ivy3SymHornWrongPlace)}, false
}

/*
================
rememberSentry

Keeps one called sentry against its death: which sign it leaves, and
when the call lapses.
================
*/
func (p *ivy3Programme) rememberSentry(codename, sign string, expiresMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sentries == nil {
		p.sentries = make(map[string][]ivy3Sentry)
	}
	p.sentries[codename] = append(p.sentries[codename], ivy3Sentry{sign: sign, expiresMs: expiresMs})
}

/*
================
takeSentry

Drops the oldest still-standing call of a sentry codename and
answers its sign; calls past their five minutes lapse.
================
*/
func (p *ivy3Programme) takeSentry(codename string, nowMs int64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	calls := p.sentries[codename]
	for len(calls) > 0 && calls[0].expiresMs <= nowMs {
		calls = calls[1:]
	}
	if len(calls) == 0 {
		delete(p.sentries, codename)
		return "", false
	}
	sign := calls[0].sign
	if len(calls) == 1 {
		delete(p.sentries, codename)
	} else {
		p.sentries[codename] = calls[1:]
	}
	return sign, true
}

/*
================
useIvy3Blood

Punisher's Blood, clicked with the three signs held: they combine
into the key. Anything less is silent and spends nothing (the native
use shows no line). The caller consumes the Blood on success.
================
*/
func (rt *Runtime) useIvy3Blood(c *enterworld.Character, def *Definition) ([]wire.Frame, bool) {
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(c, sign) == 0 {
			return nil, false
		}
	}
	if rt.PlanInventory == nil {
		return nil, false
	}
	take := make([]inventory.ItemAmount, 0, len(ivy3SignCodenames))
	for _, sign := range ivy3SignCodenames {
		take = append(take, inventory.ItemAmount{Codename: sign, Count: 1})
	}
	rows, updates, err := rt.PlanInventory(c, take, []inventory.ItemAmount{{Codename: ivy3KeyCodename, Count: 1}})
	if err != nil {
		return []wire.Frame{questNotification(ivy3SymBagFull)}, false
	}
	c.MissionInventory = rows
	return updates, true
}

/*
================
useIvy3Key

The assembled key, used at the Evil Order Fortress point (the caller
never consumes it - Ivy's death spends it, see ivy3CreditKill).
Native gate order: the place (within 600 of the fortress point), a
shared party, leadership; then the invitation. A summoned trade
transport refuses the leader inside the entry check, as it refuses
each member inside theirs.
================
*/
func (rt *Runtime) useIvy3Key(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if c.World != nil && c.World.InstanceReturn != nil {
		// Already standing in a layer: the native use reads only the
		// open world, and silently.
		return nil, false
	}
	if !withinQuestArea(at, ivy3FortressSpawn, ivy3FortressRadius) {
		return []wire.Frame{questNotification(ivy3SymKeyWrongPlace)}, false
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return nil, false
	}
	facts := rt.InstancePartyFacts(c)
	switch {
	case !facts.Found:
		return nil, false
	case !facts.InParty || !facts.Shared:
		return []wire.Frame{questNotification(ivy3SymKeyNotShared)}, false
	case !facts.Leader:
		return []wire.Frame{questNotification(ivy3SymKeyNotLeader)}, false
	case facts.Transported:
		return []wire.Frame{questNotification(ivy3SymKeyTransport)}, false
	}
	if rt.ivy3.summonedBy(c.Name) {
		return nil, false
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: ivy3Definition,
		Entry:      ivy3FortressSpawn,
		DeadlineMs: rt.ivy3Now() + ivy3InviteLifeMs + ivy3InviteSlackMs,
		Eligible:   rt.Ivy3MemberEligible,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		// A layer that would not open: the native use is silent. A
		// layer that opened without the leader landing remembers no
		// lease here; it lives on the framework's deadline handling
		// alone, and no second summon is held against the leader.
		return nil, false
	}
	rt.ivy3.rememberLease(outcome.Lease, c.Name)
	return nil, false
}

/*
================
noteIvy3Kill

Two deaths reach the programme from the quest kill updater, which
runs inside the killer's character transaction: Ivy in her layer
(only recorded - the credit runs at the layer's next update), and a
called sentry leader in the open world, whose sign the killer picks
up here, inside their own door. A killer already holding the key or
that sign takes nothing. When the credit completes the three signs
with the Blood held, the materials-ready line follows.
================
*/
func (rt *Runtime) noteIvy3Kill(c *enterworld.Character, codename string) []wire.Frame {
	if c == nil {
		return nil
	}
	if codename == ivy3BossCodename {
		rt.noteIvy3BossKill(c)
		return nil
	}
	sign, called := rt.ivy3.takeSentry(codename, rt.ivy3Now())
	if !called {
		return nil
	}
	def, ok := rt.Defs.ByCodename(ivy3QuestCodename)
	if !ok || activeQuestIndex(c, def.RefID) < 0 {
		return nil
	}
	if captureItemCount(c, ivy3KeyCodename) > 0 || captureItemCount(c, sign) > 0 {
		return nil
	}
	if rt.PlanInventory == nil {
		return nil
	}
	rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: sign, Count: 1}})
	if err != nil {
		return nil
	}
	c.MissionInventory = rows
	frames := updates
	if captureItemCount(c, ivy3BloodCodename) > 0 && ivy3SignsHeld(c) {
		frames = append(frames, questNotification(ivy3SymMaterialsReady))
	}
	return frames
}

/*
================
ivy3SignsHeld

Whether all three area signs are in the bag.
================
*/
func ivy3SignsHeld(c *enterworld.Character) bool {
	for _, sign := range ivy3SignCodenames {
		if captureItemCount(c, sign) == 0 {
			return false
		}
	}
	return true
}
