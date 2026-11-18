/*
===========================================================================

winter_missions.go - the winter event's Server Missions ("Winter Princess
and Knights")

The v1.188 server system, ported (SRO_CHRISTMAS_EVENT in mode winter).
The counting is the v1.188 shard manager's; the game server only reports
what moved and pays the rewards. The rows are the shipped _RefServerEvent and its rewards
(v1.188 SRO_VT_SHARD):

	30001 Snow Flake Event          an exchange at So-ok      goal 18 per online player + 100
	30002 Snow Slave Hunting Event  a Snow Slave kill         goal 360 per online player + 100
	30003 White Knight summon Event a 30002 completion        goal 1
	30004 Princess summon Event     a 30003 completion        goal 5

Both first missions watch the winter quest's event (DetectingTargetID
20000, QEV_WINTER_EVENT_2009); a kill reports amount 1, an exchange the
number of gifts the talk gave. A mission completes when its count reaches
its goal: completions grow by count/goal, the remainder carries over, and
a per-player goal is set again from the players online then.
The completion feeds the missions that watch it, once per completion:
every Snow Slave Hunting completion summons the White Knights, every fifth
the Princesses. The missions repeat until the event ends.

The rewards (_RefServerEventReward, paid ten seconds after the completion)
are action/winter_missions.go's: everyone's EXP/SP +20% for an hour
(30001), the hunter whose kill completed 30002 wins +100% EXP/SP for three
hours, and the summon missions land their monsters on 40 fixed points.

===========================================================================
*/
package progression

import "time"

// The native mission identities (_RefServerEvent.ID).
const (
	WinterMissionSnowFlake  uint32 = 30001
	WinterMissionSnowSlave  uint32 = 30002
	WinterMissionKnights    uint32 = 30003
	WinterMissionPrincesses uint32 = 30004
)

// WinterQuestEventID is the winter quest's event, the target both first
// missions watch (QEV_WINTER_EVENT_2009, event 20000).
const WinterQuestEventID uint32 = 20000

/*
================
WinterMissionTrigger

What a report says moved (the 0x3CF1 type): a Snow Slave kill, or So-ok's
exchange. A chained completion reports as an exchange does (type 2).
================
*/
type WinterMissionTrigger uint8

const (
	WinterTriggerKill     WinterMissionTrigger = 1
	WinterTriggerExchange WinterMissionTrigger = 2
)

// WinterMissionRewardDelay is how long after a completion its reward is
// paid (_RefServerEvent column at +0x54: 10 s on every row).
const WinterMissionRewardDelay = 10 * time.Second

// The 30001 reward (_RefServerEventReward_ExpUPForPlayers row 0x1ED):
// every player's EXP and skill EXP +20% for 3600 s.
const (
	WinterExpUpDuration = time.Hour
	WinterExpUpPercent  = 20
)

// The 30002 winner's reward: SKILL_EVENT_EXP_SP_UP_100_01 (v1.696
// skilldata: 10800000 ms, EXP and skill EXP +100%).
const (
	WinterWinnerBuffDuration = 3 * time.Hour
	WinterWinnerBuffPercent  = 100
)

// winterGoalBase is the constant term of a per-player goal.
const winterGoalBase = 100

/*
================
winterMissionRef

One _RefServerEvent row: whom it watches (the winter quest's event, or
another mission), which report type counts, and its goal: per online
player, or fixed.
================
*/
type winterMissionRef struct {
	ID      uint32
	Name    string // SN_SERVER_EVENT_MISSION_<ID>, English
	Watches uint32 // WinterQuestEventID or another mission
	Trigger WinterMissionTrigger
	PerUser int // goal per online player (+0x4E), 0 for a fixed goal
	Fixed   int // the fixed goal (+0x4D set)
	// Gauge is the client gauge flag (+0x58): the mission's progress is
	// broadcast every five minutes. Only 30001 sets it.
	Gauge bool
}

var winterMissionRefs = []winterMissionRef{
	{ID: WinterMissionSnowFlake, Name: "Snow Flake Event", Watches: WinterQuestEventID, Trigger: WinterTriggerExchange, PerUser: 18, Gauge: true},
	{ID: WinterMissionSnowSlave, Name: "Snow Slave Hunting Event", Watches: WinterQuestEventID, Trigger: WinterTriggerKill, PerUser: 360},
	{ID: WinterMissionKnights, Name: "White Knight summon Event", Watches: WinterMissionSnowSlave, Trigger: WinterTriggerExchange, Fixed: 1},
	{ID: WinterMissionPrincesses, Name: "Princess summon Event", Watches: WinterMissionKnights, Trigger: WinterTriggerExchange, Fixed: 5},
}

/*
================
WinterMissionName

A mission's English name, as the client's SN_SERVER_EVENT_MISSION_<ID>
reads; "" for an unknown identity.
================
*/
func WinterMissionName(id uint32) string {
	if ref, ok := findWinterMission(id); ok {
		return ref.Name
	}
	return ""
}

/*
================
findWinterMission
================
*/
func findWinterMission(id uint32) (winterMissionRef, bool) {
	for _, ref := range winterMissionRefs {
		if ref.ID == id {
			return ref, true
		}
	}
	return winterMissionRef{}, false
}

/*
================
WinterMissionState

One mission's live row (_ServerEvent: Completions, Goal, Count). It is
what the shard keeps across a restart and what a progress gauge reads.
================
*/
type WinterMissionState struct {
	ID          uint32 `json:"id"`
	Completions int    `json:"completions"`
	Goal        int    `json:"goal"`
	Count       int    `json:"count"`
	// Gauge is the native gauge flag: the client shows this mission's
	// progress (only the Snow Flake Event).
	Gauge bool `json:"-"`
}

/*
================
WinterMissionCompletion

A mission that completed: how many times at once, and the player whose
report completed it ("" for a chained mission).
================
*/
type WinterMissionCompletion struct {
	ID     uint32
	Times  int
	Winner string
}

/*
================
WinterMissions

Every mission's row. The zero value holds none; NewWinterMissions makes
the four.
================
*/
type WinterMissions struct {
	states []WinterMissionState
	// goals are the port-only fixed test goals (winter_mission_goals.go);
	// nil is native.
	goals WinterMissionGoals
}

/*
================
NewWinterMissions

The four missions, from what the shard kept (a row it did not keep, or a
kept row with no goal, starts at zero with its goal set from online). A
mission goals names takes that fixed goal, a kept row's included.
================
*/
func NewWinterMissions(kept []WinterMissionState, online int, goals WinterMissionGoals) *WinterMissions {
	m := &WinterMissions{states: make([]WinterMissionState, len(winterMissionRefs)), goals: goals}
	for i, ref := range winterMissionRefs {
		state := WinterMissionState{ID: ref.ID}
		for _, row := range kept {
			if row.ID == ref.ID && row.Goal > 0 && row.Count >= 0 && row.Completions >= 0 {
				state = row
			}
		}
		if fixed, ok := goals[ref.ID]; ok {
			state.Goal = fixed
		} else if state.Goal <= 0 {
			state.Goal = ref.goal(online)
		}
		state.Gauge = ref.Gauge
		m.states[i] = state
	}
	return m
}

/*
================
winterMissionRef.goal

A per-player goal from the players online now, or the fixed goal.
================
*/
func (ref winterMissionRef) goal(online int) int {
	if ref.PerUser == 0 {
		return max(1, ref.Fixed)
	}
	return ref.PerUser*max(0, online) + winterGoalBase
}

/*
================
WinterMissions.Record

One report: amount of trigger moved, by player. Every mission watching the
winter quest's event with that trigger counts it; a completion feeds the
missions watching it (the shard manager feeds a completion back as a
report). Returns
the completions, in the order they happened.
================
*/
func (m *WinterMissions) Record(trigger WinterMissionTrigger, amount int, player string, online int) []WinterMissionCompletion {
	if m == nil || amount <= 0 {
		return nil
	}
	var out []WinterMissionCompletion
	m.process(WinterQuestEventID, trigger, amount, player, online, &out)
	return out
}

/*
================
WinterMissions.process
================
*/
func (m *WinterMissions) process(target uint32, trigger WinterMissionTrigger, amount int, player string, online int, out *[]WinterMissionCompletion) {
	for i, ref := range winterMissionRefs {
		if ref.Watches != target || ref.Trigger != trigger {
			continue
		}
		state := &m.states[i]
		state.Count += amount
		if state.Count < state.Goal {
			continue
		}
		times := state.Count / state.Goal
		state.Completions += times
		state.Count %= state.Goal
		if _, fixed := m.goals[ref.ID]; !fixed && ref.PerUser != 0 {
			state.Goal = ref.goal(online)
		}
		*out = append(*out, WinterMissionCompletion{ID: ref.ID, Times: times, Winner: player})
		m.process(ref.ID, WinterTriggerExchange, times, "", online, out)
	}
}

/*
================
WinterMissions.Snapshot

A copy of every row, in mission order: the progress a gauge shows and the
shard keeps.
================
*/
func (m *WinterMissions) Snapshot() []WinterMissionState {
	if m == nil {
		return nil
	}
	return append([]WinterMissionState(nil), m.states...)
}
