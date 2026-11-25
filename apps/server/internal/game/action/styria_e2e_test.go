/*
===========================================================================

styria_e2e_test.go - Styria Clash: the epic's done-when, driven whole

The composed loop the slices never drove in one place (#3648): with
the switch on, a Hunter and a Thief select the arena manager in the
live roster, talk, and apply through the real conversation choice;
the start matches them into world 107; they fight over the treasure
boxes through the object-select lane; the clock returns them and
pays the win and lose rewards - the EXP and, since #4375 staged the
reward items, the rune seals and stones too - with the status
snapshots recording each phase. With the switch off there is no
manager, no rows and no tick. The draw reward shares this path
(TestStyriaClockDecidesWinAndDraw).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/styria"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
styriaManager

The live arena manager in the runtime's roster, if one stands.
================
*/
func styriaManager(rt *Runtime) (simulation.NpcDef, bool) {
	for _, npc := range rt.NpcRoster {
		if npc.Codename == styria.ManagerCodename {
			return npc, true
		}
	}
	return simulation.NpcDef{}, false
}

/*
================
styriaApplyAtManager

One application through the real conversation: select the manager,
talk for the options, and send the Apply row's choice byte.
================
*/
func styriaApplyAtManager(t *testing.T, rt *Runtime, character *enterworld.Character, gid uint32) {
	t.Helper()
	if outcome := rt.HandleObjectSelect(testDivision, character, selectBody(gid)); outcome.Refusal != "" {
		t.Fatalf("%s could not select the manager: %s", character.Name, outcome.Refusal)
	}
	frames, refusal := rt.HandleNpcAction(testDivision, character, npcActionBody(gid, simulation.NpcTalkFlagTalk))
	if refusal != "" {
		t.Fatalf("%s could not talk to the manager: %s", character.Name, refusal)
	}
	statusFrame := false
	for _, frame := range frames {
		if frame.Opcode == wire.OpStyriaStatus {
			statusFrame = true
		}
	}
	if !statusFrame {
		t.Fatalf("%s's manager conversation carried no status snapshot", character.Name)
	}
	conversation, ok := rt.NpcDialogs.Get(testDivision, character.Name)
	if !ok {
		t.Fatalf("%s holds no conversation after the talk", character.Name)
	}
	choice := -1
	for index, option := range conversation.Options {
		if option.Codename == styria.ServiceApply {
			choice = index
		}
	}
	if choice < 0 {
		t.Fatalf("%s was offered no Apply row: %+v", character.Name, conversation.Options)
	}
	if _, refusal := rt.HandleNpcDialogResponse(testDivision, character, []byte{byte(5 + choice)}); refusal != "" {
		t.Fatalf("%s's Apply choice refused: %s", character.Name, refusal)
	}
	if !rt.styriaRegistered(testDivision, character.Name) {
		t.Fatalf("%s holds no application after the conversation", character.Name)
	}
}

/*
================
styriaAssertRewardItems

One paid character's item half (#4375): every expected codename
resolves in the tree's item data, and the character holds exactly
the expected count of each (and of nothing else the reward names).
================
*/
func styriaAssertRewardItems(t *testing.T, rt *Runtime, character *enterworld.Character, want map[string]int64) {
	t.Helper()
	refs := rt.deps.ItemReferences()
	held := map[string]int64{}
	for codename := range want {
		ref, ok := refs.ItemRefByCodename(codename)
		if !ok {
			t.Fatalf("reward item %s is not in the item data", codename)
		}
		for _, row := range character.MissionInventory {
			if row.RefObjID == ref.RefObjID {
				held[codename] += row.StackCount
			}
		}
	}
	for codename, count := range want {
		if held[codename] != count {
			t.Fatalf("%s holds %d of %s, want %d (all held: %v)", character.Name, held[codename], codename, count, held)
		}
	}
}

/*
================
TestStyriaDoneWhenHunterAndThief

The done-when with the flag on: apply, be matched, fight for
treasure, and get the win and lose rewards, EXP and items alike.
================
*/
func TestStyriaDoneWhenHunterAndThief(t *testing.T) {
	t.Setenv(styria.Env, "on")
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	hunter.Job = domain.CharacterJob{Type: domain.JobHunter, Grade: 1}
	thief.Job = domain.CharacterJob{Type: domain.JobThief, Grade: 1}
	rt, clock := newStyriaRuntime(t, hunter, thief)
	rt.NpcSpawn = enterworld.NpcSpawnConfig{Enabled: true, AtPlayer: true}
	rt.deps.(*enterworld.Deps).Levels = enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	rt.deps.(*enterworld.Deps).Items = enterworld.NewTextdataItems(gamedatatest.TextdataDir(t))
	prog := progression.NewRuntime(rt.deps)
	rt.UpdateExperience = prog.ExperienceUpdater()
	rt.UpdateJobExperience = prog.JobExperienceUpdater()
	var statusLog styriaStatusLog
	statusLog.byName = map[string][]wire.StyriaStatus{}
	statusLog.arm(rt)

	// The roster production loads from the shipped tables: the
	// manager's stands join it only while the switch is on.
	rt.NpcRoster = simulation.LoadNpcWorldRoster(gamedatatest.TextdataDir(t))
	manager, stands := styriaManager(rt)
	if !stands {
		t.Fatal("no arena manager stands in the roster with the switch on")
	}
	styriaStandAt(rt, hunter, manager.Spawn)
	styriaStandAt(rt, thief, manager.Spawn)
	// The manager's stand is the anchor the transfer remembers:
	// the close evacuates both fighters back onto it.
	homeRegion := manager.Spawn.RegionID
	styriaApplyAtManager(t, rt, hunter, manager.ObjectID)
	styriaApplyAtManager(t, rt, thief, manager.ObjectID)

	// The start: arm the watcher inside the window, then tick at 20:00.
	// The arm tick is also the first standing push: applied, phase 0.
	rt.advanceStyriaClash(clock.NowMs())
	if status, ok := statusLog.last("Hunter"); !ok || status.Phase != 0 || status.OwnState != 1 {
		t.Fatalf("hunter standing after applying = %+v, %v", status, ok)
	}
	clock.now = time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC)
	rt.advanceStyriaClash(clock.NowMs())
	match := rt.styria.matches[testDivision]
	if match == nil {
		t.Fatal("no live match after the start")
	}
	for _, c := range []*enterworld.Character{hunter, thief} {
		if got := styriaWorldOf(c).Definition(); got != instance.DefinitionID(styria.WorldID) {
			t.Fatalf("%s stands in world %d, want the arena %d", c.Name, got, styria.WorldID)
		}
	}
	if status, ok := statusLog.last("Thief"); !ok || status.Phase != 1 || status.OwnState != 2 {
		t.Fatalf("thief standing in the match = %+v, %v", status, ok)
	}

	// The fight: the hunter banks the east box twice (past its
	// refill) and the thief answers once at the west box.
	styriaStandAt(rt, hunter, simulation.Spawn{RegionID: 32242, X: 1100, Z: -1832})
	if outcome := rt.HandleObjectSelect(testDivision, hunter, selectBody(242003)); outcome.Selected != 242003 {
		t.Fatalf("hunter's east box click = %+v", outcome)
	}
	clock.now = clock.now.Add(61 * time.Second)
	if outcome := rt.HandleObjectSelect(testDivision, hunter, selectBody(242003)); outcome.Selected != 242003 {
		t.Fatalf("hunter's refilled box click = %+v", outcome)
	}
	styriaStandAt(rt, thief, simulation.Spawn{RegionID: 31985, X: 650, Z: -1832})
	if outcome := rt.HandleObjectSelect(testDivision, thief, selectBody(242000)); outcome.Selected != 242000 {
		t.Fatalf("thief's west box click = %+v", outcome)
	}
	if score := styriaScoreOf(match); score != [2]int{2, 1} {
		t.Fatalf("score before the clock: %v, want [2 1]", score)
	}

	hunterSkill, thiefSkill := skillHolding(hunter), skillHolding(thief)
	clock.now = time.UnixMilli(match.endsMs)
	rt.advanceStyriaClash(clock.NowMs())
	if got := hunter.Job.WeeklyReward; int64(got) != styria.RewardJobExpWin {
		t.Fatalf("winner job contribution = %d, want %d", got, styria.RewardJobExpWin)
	}
	if got := thief.Job.WeeklyReward; int64(got) != styria.RewardJobExpLose {
		t.Fatalf("loser job contribution = %d, want %d", got, styria.RewardJobExpLose)
	}
	if got := skillHolding(hunter) - hunterSkill; got != 670 {
		t.Fatalf("winner skill exp gained %d, want the level-20 band 670", got)
	}
	if got := skillHolding(thief) - thiefSkill; got != 670 {
		t.Fatalf("loser skill exp gained %d, want the level-20 band 670", got)
	}
	// The item half (#4375): the tree's reward rows resolve, so the
	// grants land - the winner holds a seal of each rune and five of
	// each stone, the loser one of each stone.
	styriaAssertRewardItems(t, rt, hunter, map[string]int64{
		styria.MagicRuneSealCodename:    styria.RewardRuneSealsWin,
		styria.StrengthRuneSealCodename: styria.RewardRuneSealsWin,
		styria.BlueStoneCodename:        styria.RewardStonesWin,
		styria.BlackStoneCodename:       styria.RewardStonesWin,
	})
	styriaAssertRewardItems(t, rt, thief, map[string]int64{
		styria.BlueStoneCodename:  styria.RewardStonesLose,
		styria.BlackStoneCodename: styria.RewardStonesLose,
	})
	if status, ok := statusLog.last("Hunter"); !ok || status.Phase != 2 || status.Outcome != 1 ||
		int64(status.JobExp) != styria.RewardJobExpWin || status.SkillExp != 670 {
		t.Fatalf("hunter result snapshot = %+v, %v", status, ok)
	}
	if status, ok := statusLog.last("Thief"); !ok || status.Phase != 2 || status.Outcome != 1 ||
		int64(status.JobExp) != styria.RewardJobExpLose {
		t.Fatalf("thief result snapshot = %+v, %v", status, ok)
	}
	// The close evacuates both fighters onto the entry anchor, and
	// no second tick pays again.
	clock.now = time.UnixMilli(match.closeMs)
	rt.advanceStyriaClash(clock.NowMs())
	for _, c := range []*enterworld.Character{hunter, thief} {
		if pose := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs()); pose.RegionID != homeRegion {
			t.Fatalf("%s stands in region %d after the close, want the anchor %d", c.Name, pose.RegionID, homeRegion)
		}
	}
	rt.advanceStyriaClash(clock.NowMs())
	if got := hunter.Job.WeeklyReward; int64(got) != styria.RewardJobExpWin {
		t.Fatalf("winner paid twice: job contribution = %d", got)
	}
	styriaAssertRewardItems(t, rt, hunter, map[string]int64{
		styria.MagicRuneSealCodename:    styria.RewardRuneSealsWin,
		styria.StrengthRuneSealCodename: styria.RewardRuneSealsWin,
		styria.BlueStoneCodename:        styria.RewardStonesWin,
		styria.BlackStoneCodename:       styria.RewardStonesWin,
	})
}

/*
================
TestStyriaDoneWhenStaysOff

The done-when with the flag off: nothing changes - no manager in
the roster, no application rows, no tick, no snapshots.
================
*/
func TestStyriaDoneWhenStaysOff(t *testing.T) {
	t.Setenv(styria.Env, "off")
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, clock := newStyriaRuntime(t, hunter)
	rt.styria = nil
	rt.ConfigureStyria("off")
	if rt.styria != nil {
		t.Fatal("the engine installed itself while off")
	}
	rt.NpcRoster = simulation.LoadNpcWorldRoster(gamedatatest.TextdataDir(t))
	if manager, stands := styriaManager(rt); stands {
		t.Fatalf("an arena manager stands while off (gid %d)", manager.ObjectID)
	}
	if options := rt.styriaTalkOptions(testDivision, hunter, styria.ManagerCodename); options != nil {
		t.Fatalf("application rows while off: %+v", options)
	}
	var statusLog styriaStatusLog
	statusLog.byName = map[string][]wire.StyriaStatus{}
	statusLog.arm(rt)
	if out := rt.advanceStyriaClash(clock.NowMs()); out != nil {
		t.Fatalf("tick frames while off: %+v", out)
	}
	if len(statusLog.byName) != 0 {
		t.Fatalf("status snapshots while off: %v", statusLog.byName)
	}
}
