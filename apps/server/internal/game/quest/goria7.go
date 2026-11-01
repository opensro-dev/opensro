/*
===========================================================================

goria7.go - GORIA 7: the hunter's instanced Hun Lancer hunt

The last Goria programme, on the instanced-world framework like
IVY 3 before it. The native shape, read off the reference quest
class's own handlers:

The hunter in Jangan offers the quest (prerequisite GORIA 6, one
completion). Talking while it runs, without the Hun Lancer's proof
item and not yet entered today, offers the entry row: the entry
opens instance definition 13 for the party, once per calendar day.
Using the proof item near the instance point is the second door
in - party, place and reach gates with their own lines - and
spends nothing.

Inside, the Hun Lancer the layer names appears 30,000 ms after the
layer opens, 20 to 100 from the entry point; it speaks its threat
2,000 ms later and the battle opens 5,000 ms after that. The battle
clock warns with ten and five minutes left, at twenty minutes the
lancer is gone, and ten seconds later the layer returns everyone.
Its death is party business: every resident doing the quest is
credited with the proof item, the room hears the death line, the
exit gate rises beside the lancer, and sixty seconds later the
party is returned - the native handler moves nobody itself; its
event timer does.
A member whose party standing breaks, or who does not belong to
the hunt, is sent out with the instance-condition line.

The proof item's native source is the hunt itself: the native
death handler grants nothing when the lancer falls, the
delivery mission waits on the item, and no other
handler grants it - so the kill credit hands it to each credited
resident who lacks it, the way IVY 3's sign credit works. The
entry point (region 26991, the native item-use destination) is
the layer's door and the spawn's anchor.

Dialogue and notice lines carry the authored symbols in the
native order; as with the other programmes they arrive as banners
until a say plane exists. This file owns the open-world half -
the hunter's talk rows and the proof item's use; the layer the
entry opens, and its clock, live in goria7_layer.go.

===========================================================================
*/

package quest

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	goria6QuestCodename = "QNO_CA_GORIA_6"
	goria7QuestCodename = "QNO_CA_GORIA_7"
	goria7NpcCodename   = "NPC_CA_HUNTER"

	goria7ItemCodename = "ITEM_QNO_CA_GORIA_7_03"
	goria7BossCodename = "MOB_QT_01_HUNLANCER"

	// goria7Definition is INS_QNO_CA_GORIA_7 in the shipped
	// gameworld table (row 13): layer limit 100, player limit 8.
	goria7Definition = instance.DefinitionID(13)

	// goria7EntryPrefix marks the dialogue token of the hunter's
	// entry row (AdvanceNpcQuest routes it).
	goria7EntryPrefix = "goria7:"

	// Encounter timing, read off the native programme: the layer
	// stands empty for 30,000 ms after it opens, the lancer speaks
	// 2,000 ms after it is named, and the battle opens 5,000 ms
	// after the line; the battle clock warns at 600,000 ms and
	// 900,000 ms, the lancer leaves at 1,200,000 ms, and the
	// layer's own end is 1,210,000 ms on the same clock.
	goria7SpawnDelayMs = 30_000
	goria7FirstLineMs  = 2_000
	goria7BattleStepMs = 5_000
	goria7WarnFirstMs  = 600_000
	goria7WarnSecondMs = 900_000
	goria7BossLeavesMs = 1_200_000
	goria7LayerEndMs   = 1_210_000

	// The kill leaves the room a 60,000 ms grace (the death
	// handler's event timer, 0xEA60) before the party is returned.
	goria7KillGraceMs = 60_000

	// The native entry hands the layer a 1,260,000 ms lifetime
	// and a 60,000 ms entry window. The invitation deadline
	// composes both; the programme ends the layer itself on the
	// kill or the clock.
	goria7InviteLifeMs  = 1_260_000
	goria7InviteSlackMs = 60_000

	// The proof item's use validates its point through the world
	// search with a 700 budget.
	goria7ItemRadius = 700.0

	// The lancer appears 20 to 100 from the entry point: the
	// native polar random placement, the quest-spawn radius pair.
	goria7SpawnRadiusMin  = 20
	goria7SpawnRadiusSpan = 80

	// The lancer's span in the world: the native programme removes
	// it when the battle clock runs out, so the spawn carries the
	// same span as its own lifetime - the room's clock speaks the
	// leaving line on the same mark.
	goria7BossLifeMs = goria7FirstLineMs + goria7BattleStepMs + goria7BossLeavesMs
)

// goria7EntrySpawn is the instance point the native proof-item
// use names (region 26991, read off the native destination): the
// party lands there in the layer, which duplicates the region
// space, and the lancer is named near it.
var goria7EntrySpawn = simulation.Spawn{RegionID: 26991, X: 1154, Y: 142, Z: 1506, Angle: 0}

// GORIA 7 dialogue and notice symbols (SN_TALK_QNO_CA_GORIA_7_*).
const (
	goria7SymOffer        = "SN_TALK_QNO_CA_GORIA_7_01"
	goria7SymAccept       = "SN_TALK_QNO_CA_GORIA_7_02"
	goria7SymDeny         = "SN_TALK_QNO_CA_GORIA_7_03"
	goria7SymNotAchieved  = "SN_TALK_QNO_CA_GORIA_7_04"
	goria7SymEnter        = "SN_TALK_QNO_CA_GORIA_7_05"
	goria7SymEnterRow     = "SN_TALK_QNO_CA_GORIA_7_06"
	goria7SymComplete     = "SN_TALK_QNO_CA_GORIA_7_09"
	goria7SymItemParty    = "SN_TALK_QNO_CA_GORIA_7_11"
	goria7SymItemLeader   = "SN_TALK_QNO_CA_GORIA_7_12"
	goria7SymItemLocation = "SN_TALK_QNO_CA_GORIA_7_13"
	goria7SymBossLine     = "SN_TALK_QNO_CA_GORIA_7_14"
	goria7SymBossDeath    = "SN_TALK_QNO_CA_GORIA_7_15"
	goria7SymWarnFirst    = "SN_TALK_QNO_CA_GORIA_7_16"
	goria7SymWarnSecond   = "SN_TALK_QNO_CA_GORIA_7_17"
	goria7SymEnterAgain   = "SN_TALK_QNO_CA_GORIA_7_18"
	goria7SymEnterWait    = "SN_TALK_QNO_CA_GORIA_7_19"
	goria7SymBossLeaves   = "SN_TALK_QNO_CA_GORIA_7_23"
	goria7SymNoOffer      = "SN_TALK_QNO_CA_GORIA_7_24"
	goria7SymBattleBegun  = "SN_TALK_QNO_CA_GORIA_7_25"
	goria7SymCondition    = "UIIT_MSG_QUEST_INS_CONDITION"
)

var goria7Specs = []QuestSpec{
	{
		// Goria 7 (the class stores 40 as its level condition,
		// one completion, prerequisite GORIA 6; the shipped
		// contents line counts the lancer hunt and the proof
		// item's delivery under one line). The v1.150 reward
		// text pays 55,000 EXP and 4,000 skill EXP.
		Codename: goria7QuestCodename, RequiredQuests: []string{goria6QuestCodename},
		KindByte: 1, Objective: ObjectiveParallel, MinLevel: 40, MaxCompletions: 1,
		StartNpcCodename: goria7NpcCodename, EndNpcCodename: goria7NpcCodename,
		OfferPromptSymbol: goria7SymOffer, AcceptResponseSymbol: goria7SymAccept,
		DenyResponseSymbol: goria7SymDeny, NotAchievedSymbol: goria7SymNotAchieved,
		CompletePromptSymbol: goria7SymComplete,
		RewardExp:            55000, RewardSkillExp: 4000,
		Objectives: []MissionSpec{
			{ContentsSymbol: "SN_CON_QNO_CA_GORIA_7", Objective: ObjectiveKill,
				KillMonsterCodenames: []string{goria7BossCodename}, KillCount: 1},
			{ContentsSymbol: "SN_CON_QNO_CA_GORIA_7", Objective: ObjectiveCollect,
				CollectItemCodename: goria7ItemCodename, CollectCount: 1},
		},
	},
}

/*
================
goria7Now

The programme clock: milliseconds on the runtime clock, zero when
unwired (detached tests then see no elapsed time and nothing fires).
================
*/
func (rt *Runtime) goria7Now() int64 {
	if rt.NowMs == nil {
		return 0
	}
	return rt.NowMs()
}

/*
================
goria7EnteredToday

Whether the entry already opened on the current calendar day: the
persisted quest-supply day under this quest's reference id (the
native talk stamps the day when the entry row is offered, when
the entry opens, and at acceptance; the port stamps it when the
entry opens).
================
*/
func (rt *Runtime) goria7EnteredToday(c *enterworld.Character, def *Definition) bool {
	state, exists := c.QuestSupplies[def.RefID]
	return exists && state.Day == rt.CalendarNow().Day
}

/*
================
goria7EntryOption

The hunter's entry row, offered while the quest runs unachieved
and the holder has no proof item: today's entry already spent
answers with the come-back-tomorrow line in its place. The native
row also stands behind the daily stamp the offer itself sets;
the port stamps the day when the entry opens (advanceGoria7Entry).
================
*/
func (rt *Runtime) goria7EntryOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	if def.Codename != goria7QuestCodename || npc != goria7NpcCodename {
		return NpcOption{}, false
	}
	at := activeQuestIndex(c, def.RefID)
	if at < 0 || objectiveMet(c, def, c.ActiveQuests[at]) {
		return NpcOption{}, false
	}
	if captureItemCount(c, goria7ItemCodename) > 0 {
		return NpcOption{}, false
	}
	if rt.goria7EnteredToday(c, def) {
		return NpcOption{
			Codename: def.Codename, TitleSymbol: def.TitleSymbol,
			PromptSymbol: goria7SymEnterWait, Informational: true,
		}, true
	}
	return NpcOption{
		Codename:     goria7EntryPrefix + def.Codename,
		TitleSymbol:  goria7SymEnterRow,
		PromptSymbol: goria7SymEnter,
		Complete:     true,
	}, true
}

/*
================
advanceGoria7Entry

The confirmed entry row: the invitation opens instance
definition 13 for the party, and the day is stamped. Native entry
is the party's - a shared party, led by the talker; anyone else
is refused without a line, as the native entry answer falls back
to the offer row.
================
*/
func (rt *Runtime) advanceGoria7Entry(c *enterworld.Character, npc string) (OpResult, error) {
	def, ok := rt.Defs.ByCodename(goria7QuestCodename)
	if !ok || npc != goria7NpcCodename {
		return OpResult{}, fmt.Errorf("quest %s wrong entry NPC", goria7QuestCodename)
	}
	if activeQuestIndex(c, def.RefID) < 0 {
		return OpResult{}, fmt.Errorf("quest %s is not active", goria7QuestCodename)
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return OpResult{}, fmt.Errorf("quest %s instance doors unavailable", goria7QuestCodename)
	}
	facts := rt.InstancePartyFacts(c)
	if !facts.Found || !facts.InParty || !facts.Shared || !facts.Leader {
		return OpResult{}, fmt.Errorf("quest %s entry needs a shared party led by the talker", goria7QuestCodename)
	}
	if rt.goria7.summonedBy(c.Name) {
		return OpResult{}, fmt.Errorf("quest %s entry already open", goria7QuestCodename)
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: goria7Definition,
		Entry:      goria7EntrySpawn,
		DeadlineMs: rt.goria7Now() + goria7InviteLifeMs + goria7InviteSlackMs,
		Eligible:   rt.Goria7MemberEligible,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		return OpResult{}, fmt.Errorf("quest %s instance would not open", goria7QuestCodename)
	}
	rt.goria7.rememberLease(outcome.Lease, c.Name)
	setCaptureSupply(c, def.RefID, rt.CalendarNow().Day, false)
	return OpResult{}, nil
}

/*
================
useGoria7Item

The proof item, used at the instance point: the second door into
the layer. Native gate order: a shared party (its own line), the
entry point itself (its own line - the region is folded into the
point it measures from), then the party leader (its own line).
The item is never spent - the delivery at the hunter spends it.
================
*/
func (rt *Runtime) useGoria7Item(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if c.World != nil && c.World.InstanceReturn != nil {
		// Already standing in a layer: the native use reads only
		// the open world, and silently.
		return nil, false
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return nil, false
	}
	facts := rt.InstancePartyFacts(c)
	if !facts.Found {
		return nil, false
	}
	if !facts.InParty || !facts.Shared {
		return []wire.Frame{questNotification(goria7SymItemParty)}, false
	}
	if at.RegionID != goria7EntrySpawn.RegionID || !withinQuestArea(at, goria7EntrySpawn, goria7ItemRadius) {
		return []wire.Frame{questNotification(goria7SymItemLocation)}, false
	}
	if !facts.Leader {
		return []wire.Frame{questNotification(goria7SymItemLeader)}, false
	}
	if rt.goria7.summonedBy(c.Name) {
		return nil, false
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: goria7Definition,
		Entry:      goria7EntrySpawn,
		DeadlineMs: rt.goria7Now() + goria7InviteLifeMs + goria7InviteSlackMs,
		Eligible:   rt.Goria7MemberEligible,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		return nil, false
	}
	rt.goria7.rememberLease(outcome.Lease, c.Name)
	return nil, false
}
