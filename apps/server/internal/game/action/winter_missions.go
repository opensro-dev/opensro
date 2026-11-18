/*
===========================================================================

winter_missions.go - the winter event's Server Missions, live

The game server's half of "Winter Princess and Knights"
(SRO_CHRISTMAS_EVENT in mode winter; the rules are
progression/winter_missions.go's). A Snow Slave kill and a So-ok
exchange report to the division's missions, as the v1.188 game server
reported them to its shard manager; a completion's reward is paid ten
seconds later on the tick, as the shard manager's delayed completion
fires:

  - Snow Flake Event: every player's EXP and skill EXP +20% for an hour
    (ExpUPForPlayers), applied by the kill reward (winterExpUpBonus);
  - Snow Slave Hunting Event: the hunter whose kill completed it wins
    +100% EXP and skill EXP for three hours
    (SKILL_EVENT_EXP_SP_UP_100_01), kept as two param jobs shown on the
    buff board with the snow flake's icon, the skill's own;
  - White Knight / Princess summon Event: one monster on each of the 40
    native points (progression/winter_summons.go), standing until the
    event ends.

Each completion is announced with the v1.696 client's own English lines
(UIIT_MSG_SERVER_EVENT_CLEAR, _WINER, _CLEAR_RECALL; START and END when
the window opens and closes). A line over the notification's 100-unit
budget is sent in two at its sentence break. The v1.150 client has no
mission gauge; WinterMissionProgress exposes what a gauge would show.

The rows are kept per division in the store (WinterMissionStore) and
reloaded at the first tick, so a restart resumes them. Kills and
exchanges report from inside doors: they only touch this owner's state
under its own leaf lock. Everything that reads characters, the store or
the monster owner runs on the tick goroutine.

===========================================================================
*/
package action

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// winterOnlineRecountEvery is how often a division's online players are
// counted for the per-player goals (the count is read at a completion).
const winterOnlineRecountEvery = 30 * time.Second

// winterSaveEvery is how often changed rows are handed to the store, each
// hand-off a commit: a restart loses at most this much of a hunt.
const winterSaveEvery = 15 * time.Second

// winterWinnerSkill names the winner's buff on its param jobs.
const winterWinnerSkill = "SKILL_EVENT_EXP_SP_UP_100_01"

// The native lines (v1.696 textuisystem, English), split where one is
// longer than a notice.
const (
	winterNoticeStart        = "(%s) has been started."
	winterNoticeEnd          = "(%s) has been ended."
	winterNoticeClear        = "Congratulations. Server Mission(%s) is completed."
	winterNoticeIncrease     = "For next (%d) minutes, (%d)%% increase event will be in progress."
	winterNoticeWinner       = "Congratulations (%s). You selected Server Mission (%s)."
	winterNoticeWinnerBoost  = "(%d)minutes from now (%d)%% increase event will be in progress."
	winterNoticeRecall       = "Server Mission(%s) is completed. Now (%s) summon event will begin."
	winterEntryNoticeOn      = "Winter Princess and Knights runs until %s UTC."
	winterEntryNoticeExpUp   = "Snow Flake Event: EXP and skill EXP +%d%% for %d more minute(s)."
	winterEntryNoticeMission = "%s: %d / %d"
	winterEntryTimeShape     = "Jan 2 15:04"
)

/*
================
WinterMissionStore

Where the division's rows are kept (the store's metadata plane). Nil
keeps them in memory for the process's life.
================
*/
type WinterMissionStore interface {
	WinterMissions(division string) domain.WinterMissions
	SetWinterMissions(division string, missions domain.WinterMissions)
}

/*
================
winterMissionsState

The owner's state. mu guards divisions and pending; seen and active are
the tick's alone.
================
*/
type winterMissionsState struct {
	mu        sync.Mutex
	divisions map[string]*winterDivision
	pending   []winterReward
	seen      bool
	active    bool
	savedAt   time.Time
}

/*
================
winterDivision

One division's missions, its last online count and the running EXP
bonus. dirty marks rows the store has not seen.
================
*/
type winterDivision struct {
	missions   *progression.WinterMissions
	online     int
	countedAt  time.Time
	expUpUntil time.Time
	dirty      bool
}

/*
================
winterReward

A completion waiting for its reward delay.
================
*/
type winterReward struct {
	division   string
	completion progression.WinterMissionCompletion
	dueAt      time.Time
}

//============================================================================
// Reports
//============================================================================

/*
================
Runtime.reportWinterKill

A Snow Slave's kill counts once for the Snow Slave Hunting Event, its
killer the would-be winner (amount 1). Called from the kill
settlement, inside its door.
================
*/
func (rt *Runtime) reportWinterKill(division string, killer *enterworld.Character, defeated monster.Instance, nowMs int64) {
	if killer == nil || defeated.Ref.Codename != progression.ChristmasSnowSlaveCodename {
		return
	}
	rt.recordWinterMission(division, progression.WinterTriggerKill, 1, killer.Name, time.UnixMilli(nowMs))
}

/*
================
Runtime.reportWinterExchange

A So-ok click counts its gifts for the Snow Flake Event (amount = the
exchanges of the talk). Called after the exchange commits,
outside its door.
================
*/
func (rt *Runtime) reportWinterExchange(character *enterworld.Character, gifts uint32) {
	if character == nil || gifts == 0 || rt.Monsters == nil {
		return
	}
	for _, population := range rt.Monsters.OpenWorldPopulations() {
		if rt.findCharacter(population.Division, character.Name) == character {
			rt.recordWinterMission(population.Division, progression.WinterTriggerExchange, int(gifts), character.Name, rt.Now())
			return
		}
	}
}

/*
================
Runtime.recordWinterMission

One report into the division's missions; each completion waits for its
reward delay. A division the tick has not prepared yet reports nothing.
================
*/
func (rt *Runtime) recordWinterMission(division string, trigger progression.WinterMissionTrigger, amount int, player string, now time.Time) {
	if !rt.Christmas.ActiveAt(now) {
		return
	}
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.divisions[division]
	if state == nil {
		return
	}
	state.dirty = true
	for _, done := range state.missions.Record(trigger, amount, player, state.online) {
		w.pending = append(w.pending, winterReward{division: division, completion: done, dueAt: now.Add(progression.WinterMissionRewardDelay)})
	}
}

//============================================================================
// The tick
//============================================================================

/*
================
Runtime.advanceWinterMissions

Prepares each division's missions (the store's rows), recounts its
online players, announces the window's start and end, pays the
rewards that came due and hands changed rows to the store. With the
event off this is one branch.
================
*/
func (rt *Runtime) advanceWinterMissions() {
	if !rt.Christmas.Enabled() || rt.Monsters == nil {
		return
	}
	now := rt.Now()
	populations := rt.Monsters.OpenWorldPopulations()
	for _, population := range populations {
		rt.prepareWinterDivision(population.Division, now)
	}
	active := rt.Christmas.ActiveAt(now)
	w := &rt.winterMissions
	// The first sight only records the window: a restart inside it
	// never announces a start again.
	if w.seen && active != w.active {
		line := winterNoticeEnd
		if active {
			line = winterNoticeStart
		}
		for _, population := range populations {
			for _, row := range rt.WinterMissionProgress(population.Division) {
				rt.announce(population.Division, announceWinterMission, fmt.Sprintf(line, progression.WinterMissionName(row.ID)), wire.NoticeLink{}, 0, now.UnixMilli())
			}
		}
	}
	w.seen, w.active = true, active
	paid := false
	for _, reward := range rt.dueWinterRewards(now) {
		if active {
			rt.payWinterReward(reward, now)
			paid = true
		}
	}
	// A paid reward is saved at once (the EXP bonus's end); a count
	// waits for the next save.
	if paid || now.Sub(w.savedAt) >= winterSaveEvery {
		w.savedAt = now
		rt.saveWinterMissions()
	}
}

/*
================
Runtime.prepareWinterDivision

The first tick loads the division's kept rows; every
winterOnlineRecountEvery the division's players present are counted.
================
*/
func (rt *Runtime) prepareWinterDivision(division string, now time.Time) {
	w := &rt.winterMissions
	w.mu.Lock()
	state := w.divisions[division]
	recount := state == nil || now.Sub(state.countedAt) >= winterOnlineRecountEvery
	w.mu.Unlock()
	if !recount {
		return
	}
	online := rt.countPresentCharacters(division)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.divisions == nil {
		w.divisions = map[string]*winterDivision{}
	}
	state = w.divisions[division]
	if state == nil {
		var kept domain.WinterMissions
		if rt.WinterMissionStore != nil {
			kept = rt.WinterMissionStore.WinterMissions(division)
		}
		rows := make([]progression.WinterMissionState, 0, len(kept.Rows))
		for _, row := range kept.Rows {
			rows = append(rows, progression.WinterMissionState{ID: row.ID, Completions: row.Completions, Goal: row.Goal, Count: row.Count})
		}
		state = &winterDivision{missions: progression.NewWinterMissions(rows, online, rt.WinterMissionGoals)}
		if kept.ExpUpUntilMs > 0 {
			state.expUpUntil = time.UnixMilli(kept.ExpUpUntilMs)
		}
		w.divisions[division] = state
	}
	state.online, state.countedAt = online, now
}

/*
================
Runtime.countPresentCharacters

The division's characters in the world now, as the population density
reads them (PopulationPlayers).
================
*/
func (rt *Runtime) countPresentCharacters(division string) int {
	if rt.RewardActorPresent == nil {
		return 0
	}
	count := 0
	characters := rt.deps.CharactersForDivision(division)
	rt.deps.Read(division, func() {
		for _, c := range characters {
			if c != nil && !c.DeletePending && rt.RewardActorPresent(division, c.Name) {
				count++
			}
		}
	})
	return count
}

/*
================
Runtime.dueWinterRewards

Takes the rewards whose delay has passed, in completion order.
================
*/
func (rt *Runtime) dueWinterRewards(now time.Time) []winterReward {
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	var due []winterReward
	kept := w.pending[:0]
	for _, reward := range w.pending {
		if now.Before(reward.dueAt) {
			kept = append(kept, reward)
			continue
		}
		due = append(due, reward)
	}
	w.pending = kept
	return due
}

/*
================
Runtime.saveWinterMissions

Hands every division's changed rows to the store.
================
*/
func (rt *Runtime) saveWinterMissions() {
	if rt.WinterMissionStore == nil {
		return
	}
	type save struct {
		division string
		missions domain.WinterMissions
	}
	var saves []save
	w := &rt.winterMissions
	w.mu.Lock()
	for division, state := range w.divisions {
		if !state.dirty {
			continue
		}
		state.dirty = false
		kept := domain.WinterMissions{}
		for _, row := range state.missions.Snapshot() {
			kept.Rows = append(kept.Rows, domain.WinterMissionRow{ID: row.ID, Completions: row.Completions, Goal: row.Goal, Count: row.Count})
		}
		if !state.expUpUntil.IsZero() {
			kept.ExpUpUntilMs = state.expUpUntil.UnixMilli()
		}
		saves = append(saves, save{division, kept})
	}
	w.mu.Unlock()
	for _, s := range saves {
		rt.WinterMissionStore.SetWinterMissions(s.division, s.missions)
	}
}

//============================================================================
// Rewards
//============================================================================

/*
================
Runtime.payWinterReward
================
*/
func (rt *Runtime) payWinterReward(reward winterReward, now time.Time) {
	nowMs := now.UnixMilli()
	division := reward.division
	name := progression.WinterMissionName(reward.completion.ID)
	switch reward.completion.ID {
	case progression.WinterMissionSnowFlake:
		w := &rt.winterMissions
		w.mu.Lock()
		if state := w.divisions[division]; state != nil {
			// Inference: a completion while the bonus runs restarts its
			// hour (the native remaining-time row is registered anew),
			// never stacks a second +20%.
			state.expUpUntil = now.Add(progression.WinterExpUpDuration)
			state.dirty = true
		}
		w.mu.Unlock()
		rt.announce(division, announceWinterMission, fmt.Sprintf(winterNoticeClear, name), wire.NoticeLink{}, 0, nowMs)
		rt.announce(division, announceWinterMission, fmt.Sprintf(winterNoticeIncrease,
			int(progression.WinterExpUpDuration/time.Minute), progression.WinterExpUpPercent), wire.NoticeLink{}, 0, nowMs)
	case progression.WinterMissionSnowSlave:
		rt.grantWinterWinner(division, reward.completion.Winner, nowMs)
		rt.announce(division, announceWinterMission, fmt.Sprintf(winterNoticeWinner, reward.completion.Winner, name), wire.NoticeLink{}, 0, nowMs)
		rt.announce(division, announceWinterMission, fmt.Sprintf(winterNoticeWinnerBoost,
			int(progression.WinterWinnerBuffDuration/time.Minute), progression.WinterWinnerBuffPercent), wire.NoticeLink{}, 0, nowMs)
	case progression.WinterMissionKnights, progression.WinterMissionPrincesses:
		for range reward.completion.Times {
			rt.landWinterSummons(division, reward.completion.ID, now)
		}
		rt.announce(division, announceWinterMission, fmt.Sprintf(winterNoticeRecall, name, progression.WinterSummonName(reward.completion.ID)), wire.NoticeLink{}, 0, nowMs)
	}
}

/*
================
Runtime.grantWinterWinner

The winner's +100% EXP and skill EXP for three hours, as two param jobs
the kill reward sums (paramJobRewardBonus). The native reward is a buff
skill the winner's user must be online for (the native grant refuses a
missing user); an absent winner gets nothing. The board row shows the snow
flake, the skill's own icon.
================
*/
func (rt *Runtime) grantWinterWinner(division, winner string, nowMs int64) {
	character := rt.findCharacter(division, winner)
	if character == nil || rt.RewardActorPresent == nil || !rt.RewardActorPresent(division, winner) {
		log.Infof("winter missions: winner %s is not in the world; no buff", winner)
		return
	}
	var boardRef uint32
	if ref, ok := rt.deps.ItemReferences().ItemRefByCodename(progression.ChristmasSnowflakeCodename); ok && ref != nil {
		boardRef = ref.RefObjID
	}
	endMs := nowMs + progression.WinterWinnerBuffDuration.Milliseconds()
	placed := false
	rt.deps.Update(character, "winter-winner", func() bool {
		jobs := character.ParamJobs
		for _, param := range []uint16{paramExpRate, paramSkillExpRate} {
			var ok bool
			jobs, ok = upsertParamJob(jobs, domain.ParamJob{ItemRefObjID: boardRef, Codename: winterWinnerSkill,
				Param: param, Value: progression.WinterWinnerBuffPercent, EndUnixMs: endMs})
			if !ok {
				return false
			}
		}
		character.ParamJobs = jobs
		placed = true
		return true
	})
	if !placed {
		log.Infof("winter missions: winner %s has no free param job; no buff", winner)
		return
	}
	rt.paramJobOwners.track(division, character.Name)
	if boardRef != 0 && rt.PushCharacterFrames != nil {
		frame := wire.Frame{Opcode: wire.OpParamJobStart, Payload: wire.EncodeParamJobRow(enterworld.ObjectIDForCharacter(character),
			uint32(progression.WinterWinnerBuffDuration/time.Second), boardRef)}
		rt.PushCharacterFrames(division, character.Name, []wire.Frame{frame})
	}
}

/*
================
Runtime.landWinterSummons

One summon mission's 40 monsters on the native points of the division's
open world, each standing until the event ends (a reward monster has no
nest, so nothing else would remove it). A point whose kind the data
lacks, or whose ground refuses it, lands nothing and is logged.
================
*/
func (rt *Runtime) landWinterSummons(division string, mission uint32, now time.Time) {
	var population simulation.WorldPopulation
	for _, candidate := range rt.Monsters.OpenWorldPopulations() {
		if candidate.Division == division {
			population = candidate
		}
	}
	if population.Division == "" {
		return
	}
	lifetime := rt.Christmas.EndsAt().Sub(now)
	landed, refused := 0, 0
	for _, point := range progression.WinterSummonPoints() {
		request := simulation.QuestMonsterSpawn{
			Division: division, Population: population.Lease, Codename: progression.WinterSummonCodename(mission, point.Level),
			Position:      simulation.Spawn{RegionID: point.RegionID, X: point.X, Y: point.Y, Z: point.Z},
			FixedPosition: true, NowMs: now.UnixMilli(), LifetimeMs: lifetime.Milliseconds(),
		}
		if rt.Monsters.SpawnQuestMonster(request) {
			landed++
		} else {
			refused++
		}
	}
	log.Infof("winter missions: %s landed %d monsters (%d refused) in %s", progression.WinterMissionName(mission), landed, refused, division)
}

/*
================
Runtime.winterExpUpBonus

The Snow Flake Event's +20% while it runs in the division, on the kill's
own award (base, baseSkill) added to what the award already carries.
================
*/
func (rt *Runtime) winterExpUpBonus(division string, base, baseSkill, exp, skillExp int64, nowMs int64) (int64, int64) {
	w := &rt.winterMissions
	w.mu.Lock()
	state := w.divisions[division]
	running := state != nil && nowMs < state.expUpUntil.UnixMilli()
	w.mu.Unlock()
	if !running || !rt.Christmas.ActiveAt(time.UnixMilli(nowMs)) {
		return exp, skillExp
	}
	if base > 0 {
		exp += base * progression.WinterExpUpPercent / 100
	}
	if baseSkill > 0 {
		skillExp += baseSkill * progression.WinterExpUpPercent / 100
	}
	return exp, skillExp
}

//============================================================================
// What players see
//============================================================================

/*
================
Runtime.WinterMissionProgress

The division's mission rows (count, goal, completions, and the gauge
flag only the Snow Flake Event sets): what the native client's gauge
shows (the native sync message) and what a gauge here would read. Nil when the
division has none.
================
*/
func (rt *Runtime) WinterMissionProgress(division string) []progression.WinterMissionState {
	w := &rt.winterMissions
	w.mu.Lock()
	defer w.mu.Unlock()
	if state := w.divisions[division]; state != nil {
		return state.missions.Snapshot()
	}
	return nil
}

/*
================
Runtime.ChristmasEntryFrames

The notices a character entering the world gets while the event runs
(port-only wording; the native client reads the rows from its gauge
packet instead): until when it runs, the EXP bonus when one runs, and the
two hunts' progress. The event off sends nothing.
================
*/
func (rt *Runtime) ChristmasEntryFrames(division string, character *enterworld.Character) []enterworld.Packet {
	now := rt.Now()
	if character == nil || !rt.Christmas.ActiveAt(now) {
		return nil
	}
	lines := []string{fmt.Sprintf(winterEntryNoticeOn, rt.Christmas.EndsAt().UTC().Format(winterEntryTimeShape))}
	w := &rt.winterMissions
	w.mu.Lock()
	if state := w.divisions[division]; state != nil && now.Before(state.expUpUntil) {
		minutes := int((state.expUpUntil.Sub(now) + time.Minute - 1) / time.Minute)
		lines = append(lines, fmt.Sprintf(winterEntryNoticeExpUp, progression.WinterExpUpPercent, minutes))
	}
	w.mu.Unlock()
	for _, row := range rt.WinterMissionProgress(division) {
		if row.ID == progression.WinterMissionSnowFlake || row.ID == progression.WinterMissionSnowSlave {
			lines = append(lines, fmt.Sprintf(winterEntryNoticeMission, progression.WinterMissionName(row.ID), row.Count, row.Goal))
		}
	}
	packets := make([]enterworld.Packet, 0, len(lines))
	for _, line := range lines {
		frame := wire.DropPlusNotificationFrame(clampNotice(line), wire.NoticeLink{})
		packets = append(packets, enterworld.NewPacket(frame.Opcode, frame.Payload))
	}
	return packets
}

/*
================
Runtime.WinterSpawnReplacements

The special-target replace table the monster owner asks at each open-world
nest spawn (the winter event's snowman row): while the event
runs, half of the champions become Snow Slaves at the ordinary grade. The
Snow Slave's reference is resolved once by the caller (the table runs
under the population lock); without it the table stays empty.
================
*/
func (rt *Runtime) WinterSpawnReplacements(snowman monster.MonsterRef, found bool) func(nowMs int64) []monster.SpawnReplacement {
	rows := []monster.SpawnReplacement{{Ref: snowman, Grade: winterReplaceGrade, NewGrade: 0, RatePercent: winterReplaceRate}}
	return func(nowMs int64) []monster.SpawnReplacement {
		if !found || !rt.Christmas.ActiveAt(time.UnixMilli(nowMs)) {
			return nil
		}
		return rows
	}
}

// The winter replace row (the v1.188 snowman row): the champion grade
// nibble, at rate 50.0.
const (
	winterReplaceGrade uint8   = 1
	winterReplaceRate  float32 = 50
)
