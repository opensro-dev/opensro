/*
===========================================================================

hwan_arena.go - the private arenas of the Blue Berserk chains

Port-only, not native. Four quests of each race send the player into an
instance of their own, on the instanced-world framework the Goria and Rahid
programmes use (instance_handlers.go): the Army / Military Test of level 95
and of level 100, and the two soul rituals. Nothing admits anyone else (the
original's enter request names one player), so the layer is opened solo.

The trial (QNO_*_HWAN_1_1 and 2_1). Time is the layer's age. After a 10 s
grace the commander appears on a ring 90 to 100 around the arena centre;
chain 1 sends an archer every 30 s from then on, chain 2 a healer at the start.
At 5 minutes "5 minutes remaining", at 10 minutes the test is passed, ten
seconds later the player goes home. The commander's death passes it at once.
The test fails only when the player is dead or gone (everyone, in the original's
list), then the layer ends after 15 s.

The ritual (QNO_*_HWAN_1_5 and 2_5). The dialog hands out the Shaman's Talisman
and teleports the player in. After 10 s the Old Ghost appears on the ring; at
5 minutes the warning, at 10 the ritual fails. The ghost's death counts only
if its killer was in Berserk at that moment: every resident holding a talisman
trades it for a Soul Fragment, anything else ends the ritual with "failed to
obtain", keeping the talisman for another try. Either way the player goes home
ten seconds after the kill.

The return point is where the quest was entered (the instance transfer's
anchor). The later client's cutscene scripts for the rituals are not ported
(docs/features/CAP110.md).

===========================================================================
*/
package quest

import (
	"fmt"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// hwanEntryPrefix marks the dialogue token of an arena's entry row.
const hwanEntryPrefix = "hwan-arena:"

// Timing, from the quest timer functions (milliseconds of layer age).
const (
	hwanGraceMs       = 10_000
	hwanHalfMs        = 300_000
	hwanArcherEveryMs = 30_000
	hwanHealerEveryMs = 600_000
	hwanPassReturnMs  = 10_000 // inference: the pass leaves the player ten seconds, like the ritual's kill
	hwanFailReturnMs  = 15_000
	hwanKillReturnMs  = 10_000
	hwanLayerLifeMs   = 720_000
	hwanLayerSlackMs  = 60_000
	hwanRingMin       = 90
	hwanRingSpan      = 10
	hwanMonsterLifeMs = hwanLayerLifeMs
)

type hwanArenaKind uint8

const (
	hwanTrial hwanArenaKind = iota
	hwanRitual
)

// hwanArena is one of the eight instance definitions.
type hwanArena struct {
	definition instance.DefinitionID
	race       hwanRace
	chain      int
	kind       hwanArenaKind
	// entry is where the player lands, centre where the commander or ghost appears,
	// support where the archers and the healer appear (the same point for the ritual).
	entry, centre, support simulation.Spawn
}

func (a *hwanArena) quest() string {
	if a.kind == hwanTrial {
		return hwanQuest(a.race, a.chain, "1")
	}
	return hwanQuest(a.race, a.chain, "5")
}

// commander is the trial's commander or the ritual's ghost.
func (a *hwanArena) commander() string {
	if a.kind == hwanRitual {
		return fmt.Sprintf("MOB_QT_CH_CHARGHOST_%02d", a.chain)
	}
	return hwanCommander(a.race, a.chain)
}

// helper is the archer (chain 1) or the healer (chain 2) of the trial.
func (a *hwanArena) helper() string {
	if a.chain == 2 {
		return "MOB_QT_" + a.race.tag + "_SOLDIER_02_2"
	}
	return "MOB_QT_" + a.race.tag + "_SOLDIER_01_2"
}

// hwanArenas lists the eight definitions (gameworlddata rows 32 to 39) with the
// start points of the shard's world table and the spawn centres of the quest timers.
var hwanArenas = []hwanArena{
	{36, hwanRaces[0], 1, hwanTrial, sp(22966, 1043, 52, 1087), sp(22966, 893, 52, 1087), sp(22966, 893, 52, 1087)},
	{37, hwanRaces[0], 2, hwanTrial, sp(22966, 1043, 52, 1087), sp(22966, 893, 52, 1087), sp(22966, 893, 52, 1087)},
	{38, hwanRaces[1], 1, hwanTrial, sp(22454, 1195, 91, 653), sp(22454, 1102, 91, 704), sp(22454, 1149, 91, 653)},
	{39, hwanRaces[1], 2, hwanTrial, sp(22454, 1195, 91, 653), sp(22454, 1102, 91, 704), sp(22454, 1149, 91, 653)},
	{32, hwanRaces[0], 1, hwanRitual, sp(25253, 152, 241, 811), sp(25253, 177, 233, 684), sp(25253, 177, 233, 684)},
	{34, hwanRaces[0], 2, hwanRitual, sp(25253, 152, 241, 811), sp(25253, 177, 233, 684), sp(25253, 177, 233, 684)},
	{33, hwanRaces[1], 1, hwanRitual, sp(27728, 1779, 724, 1596), sp(27728, 1752, 693, 1673), sp(27728, 1752, 693, 1673)},
	{35, hwanRaces[1], 2, hwanRitual, sp(27728, 1779, 724, 1596), sp(27728, 1752, 693, 1673), sp(27728, 1752, 693, 1673)},
}

func sp(region uint16, x, y, z float64) simulation.Spawn {
	return simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}
}

/*
================
hwanLayer

One open arena. The single resident is the player who entered.
================
*/
type hwanLayer struct {
	arena    *hwanArena
	division string
	name     string
	lease    instance.Lease
	openedMs int64
	step     int
	eventMs  int64 // layer age at which the current step's timer fires
	nextMs   int64 // layer age of the next archer or healer
	killed   bool
	// killedBerserk is the killer's body state at the kill (the ritual's rule).
	killedBerserk bool
	failed        bool
	ended         bool
}

type hwanProgramme struct {
	mu     sync.Mutex
	layers map[instance.ID]*hwanLayer
	leases map[instance.ID]instance.Lease
	opened map[string]instance.ID // by character name
}

func (rt *Runtime) hwanNow() int64 {
	if rt.NowMs == nil {
		return 0
	}
	return rt.NowMs()
}

func hwanArenaFor(definition instance.DefinitionID) *hwanArena {
	for i := range hwanArenas {
		if hwanArenas[i].definition == definition {
			return &hwanArenas[i]
		}
	}
	return nil
}

/*
================
RegisterHwanArenaProgrammes

Subscribes every arena definition to the shared handlers.
================
*/
func (rt *Runtime) RegisterHwanArenaProgrammes() {
	for i := range hwanArenas {
		rt.RegisterInstanceHandler(hwanArenas[i].definition, InstanceHandler{
			Setup: rt.hwanSetup, Enter: rt.hwanEnter, Leave: rt.hwanLeave, Update: rt.hwanUpdate,
		})
	}
}

func (rt *Runtime) hwanLayerFor(event InstanceEvent) *hwanLayer {
	p := &rt.hwan
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.layers == nil {
		p.layers = make(map[instance.ID]*hwanLayer)
	}
	layer := p.layers[event.World]
	if layer == nil {
		arena := hwanArenaFor(event.Definition)
		if arena == nil {
			return nil
		}
		layer = &hwanLayer{arena: arena, division: event.Division, openedMs: rt.hwanNow(), lease: p.leases[event.World]}
		p.layers[event.World] = layer
	}
	return layer
}

func (rt *Runtime) hwanForget(world instance.ID) {
	p := &rt.hwan
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, id := range p.opened {
		if id == world {
			delete(p.opened, name)
		}
	}
	delete(p.layers, world)
	delete(p.leases, world)
}

func (rt *Runtime) hwanSetup(event InstanceEvent) { rt.hwanLayerFor(event) }

func (rt *Runtime) hwanEnter(event InstanceEvent) {
	if layer := rt.hwanLayerFor(event); layer != nil {
		rt.hwan.mu.Lock()
		layer.name = event.Name
		rt.hwan.mu.Unlock()
	}
}

// hwanLeave ends the layer when its resident goes home (or away).
func (rt *Runtime) hwanLeave(event InstanceEvent) {
	rt.hwan.mu.Lock()
	layer := rt.hwan.layers[event.World]
	if layer == nil {
		rt.hwan.mu.Unlock()
		return
	}
	lease := layer.lease
	if lease.ID == 0 {
		lease = rt.hwan.leases[event.World]
	}
	layer.ended = true
	rt.hwan.mu.Unlock()
	rt.hwanForget(event.World)
	if rt.InstanceClose != nil && lease.ID != 0 {
		rt.InstanceClose(lease)
	}
}

func (rt *Runtime) hwanCharacter(division, name string) *enterworld.Character {
	for _, candidate := range rt.deps.CharactersForDivision(division) {
		if candidate != nil && strings.EqualFold(candidate.Name, name) {
			return candidate
		}
	}
	return nil
}

func (rt *Runtime) hwanPush(name string, frames ...wire.Frame) {
	if rt.PushCharacterFrames != nil && name != "" && len(frames) > 0 {
		rt.PushCharacterFrames(name, frames)
	}
}

func (rt *Runtime) hwanNotice(layer *hwanLayer, step string) {
	rt.hwanPush(layer.name, questNotification(hwanTalk(layer.arena.quest())+step))
}

/*
================
noteHwanKill

A kill reached the quest updater: when it is the commander or the ghost of the
killer's own arena, mark the layer (the credit runs at its next update).
================
*/
func (rt *Runtime) noteHwanKill(c *enterworld.Character, codename string) {
	if c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	arena := hwanArenaFor(world.Definition())
	if arena == nil || codename != arena.commander() {
		return
	}
	rt.hwan.mu.Lock()
	if layer := rt.hwan.layers[world]; layer != nil && !layer.killed {
		layer.killed = true
		layer.killedBerserk = c.NativeBodyStatus == 1
	}
	rt.hwan.mu.Unlock()
}

/*
================
hwanUpdate

The arena clock, one pass per layer tick, from the layer's age.
================
*/
func (rt *Runtime) hwanUpdate(event InstanceEvent) {
	layer := rt.hwanLayerFor(event)
	if layer == nil {
		return
	}
	rt.hwan.mu.Lock()
	if layer.lease.ID == 0 {
		layer.lease = rt.hwan.leases[event.World]
	}
	ended := layer.ended
	rt.hwan.mu.Unlock()
	if ended || layer.name == "" {
		return
	}
	age := rt.hwanNow() - layer.openedMs
	if layer.arena.kind == hwanTrial {
		rt.hwanTrialUpdate(event, layer, age)
	} else {
		rt.hwanRitualUpdate(event, layer, age)
	}
}

// hwanEnd sends the resident home; the leave event closes the layer.
func (rt *Runtime) hwanEnd(event InstanceEvent, layer *hwanLayer) {
	rt.hwan.mu.Lock()
	if layer.ended {
		rt.hwan.mu.Unlock()
		return
	}
	lease := layer.lease
	rt.hwan.mu.Unlock()
	if character := rt.hwanCharacter(event.Division, layer.name); character != nil && rt.InstanceReturn != nil {
		rt.InstanceReturn(character)
	}
	// A resident who could not be returned (gone) still closes the layer.
	rt.hwan.mu.Lock()
	done := layer.ended
	layer.ended = true
	rt.hwan.mu.Unlock()
	if !done {
		rt.hwanForget(event.World)
		if rt.InstanceClose != nil && lease.ID != 0 {
			rt.InstanceClose(lease)
		}
	}
}

// hwanSpawn places one arena monster on the ring around a centre of the layer.
func (rt *Runtime) hwanSpawn(event InstanceEvent, layer *hwanLayer, code string, centre simulation.Spawn) {
	character := rt.hwanCharacter(event.Division, layer.name)
	if character == nil || rt.SpawnQuestMonster == nil {
		return
	}
	request := simulation.QuestMonsterSpawn{
		Division: event.Division, Population: layer.lease, Codename: code, Position: centre, FixedPosition: true,
		RadiusMin: hwanRingMin, RadiusSpan: hwanRingSpan, LifetimeMs: hwanMonsterLifeMs, NowMs: rt.hwanNow(),
	}
	rt.deps.Update(character, "hwan-arena-spawn", func() bool { return rt.SpawnQuestMonster(character, request) })
}

// hwanResidentGone is the fail test: the resident is dead or no longer in the world.
func (rt *Runtime) hwanResidentGone(event InstanceEvent, layer *hwanLayer) bool {
	character := rt.hwanCharacter(event.Division, layer.name)
	return character == nil || !enterworld.CharacterAlive(character)
}

func (rt *Runtime) hwanTrialUpdate(event InstanceEvent, layer *hwanLayer, age int64) {
	arena := layer.arena
	passSymbol, failSymbol, halfSymbol := "09", "08", "11"
	if arena.chain == 2 {
		passSymbol, failSymbol, halfSymbol = "12", "11", "14"
	}
	rt.hwan.mu.Lock()
	killed := layer.killed
	rt.hwan.mu.Unlock()
	if killed && layer.step < 3 {
		layer.step, layer.eventMs = 3, age
	}
	if layer.step < 3 && !layer.failed && rt.hwanResidentGone(event, layer) {
		layer.failed, layer.step, layer.eventMs = true, 4, age+hwanFailReturnMs
		rt.hwanNotice(layer, failSymbol)
		return
	}
	switch layer.step {
	case 0:
		if age >= hwanGraceMs {
			layer.step = 1
		}
	case 1:
		rt.hwanSpawn(event, layer, arena.commander(), arena.centre)
		layer.eventMs = age + hwanHalfMs
		if arena.chain == 2 {
			rt.hwanSpawn(event, layer, arena.helper(), arena.support)
			layer.nextMs = age + hwanHealerEveryMs
		}
		layer.step = 2
	case 2:
		if arena.chain == 1 && age >= layer.nextMs {
			rt.hwanSpawn(event, layer, arena.helper(), arena.support)
			layer.nextMs = age + hwanArcherEveryMs
		}
		if age >= layer.eventMs {
			rt.hwanNotice(layer, halfSymbol)
			layer.eventMs, layer.step = age+hwanHalfMs, 3
		}
	case 3:
		if age >= layer.eventMs {
			rt.hwanPassTrial(event, layer)
			rt.hwanNotice(layer, passSymbol)
			layer.eventMs, layer.step = age+hwanPassReturnMs, 4
		}
	case 4:
		if age >= layer.eventMs {
			rt.hwanEnd(event, layer)
		}
	}
}

// hwanPassTrial credits the commander's kill to a player who survived the clock
// (a kill already counted through the ordinary kill updater is not counted twice).
func (rt *Runtime) hwanPassTrial(event InstanceEvent, layer *hwanLayer) {
	rt.hwan.mu.Lock()
	killed := layer.killed
	rt.hwan.mu.Unlock()
	if killed {
		return
	}
	def, ok := rt.Defs.ByCodename(layer.arena.quest())
	character := rt.hwanCharacter(event.Division, layer.name)
	if !ok || character == nil {
		return
	}
	var frames []wire.Frame
	rt.deps.Update(character, "hwan-trial-pass", func() bool {
		at := activeQuestIndex(character, def.RefID)
		if at < 0 || character.DeletePending {
			return false
		}
		updated, changed := refreshMissions(character, def, character.ActiveQuests[at], layer.arena.commander(), 0)
		if changed {
			frames = missionProgressFrames(def, character.ActiveQuests[at], updated)
			character.ActiveQuests[at] = updated
		}
		return changed
	})
	rt.hwanPush(layer.name, frames...)
}

func (rt *Runtime) hwanRitualUpdate(event InstanceEvent, layer *hwanLayer, age int64) {
	arena := layer.arena
	rt.hwan.mu.Lock()
	killed, berserk := layer.killed, layer.killedBerserk
	rt.hwan.mu.Unlock()
	if killed && layer.step < 4 {
		layer.step, layer.eventMs = 4, age+hwanKillReturnMs
		if berserk {
			rt.hwanGrantFragment(event, layer)
		} else {
			rt.hwanNotice(layer, "09")
		}
		return
	}
	switch layer.step {
	case 0:
		if age >= hwanGraceMs {
			rt.hwanSpawn(event, layer, arena.commander(), arena.centre)
			layer.eventMs, layer.step = age+hwanHalfMs, 1
		}
	case 1:
		if age >= layer.eventMs {
			rt.hwanNotice(layer, "10")
			layer.eventMs, layer.step = age+hwanHalfMs, 2
		}
	case 2:
		if age >= layer.eventMs {
			rt.hwanNotice(layer, "09")
			layer.eventMs, layer.step = age, 4
		}
	case 4:
		if age >= layer.eventMs {
			rt.hwanEnd(event, layer)
		}
	}
}

// hwanGrantFragment trades the resident's talisman for a Soul Fragment.
func (rt *Runtime) hwanGrantFragment(event InstanceEvent, layer *hwanLayer) {
	arena := layer.arena
	character := rt.hwanCharacter(event.Division, layer.name)
	if character == nil || rt.PlanInventory == nil {
		return
	}
	orb, fragment := arena.race.ritualOrb1, hwanSoulFragment
	if arena.chain == 2 {
		orb, fragment = arena.race.orb2, hwanUnderworldFragment
	}
	var frames []wire.Frame
	rt.deps.Update(character, "hwan-ritual-fragment", func() bool {
		if character.DeletePending || captureItemCount(character, orb) == 0 {
			return false
		}
		rows, updates, err := rt.PlanInventory(character,
			[]inventory.ItemAmount{{Codename: orb, Count: 1}}, []inventory.ItemAmount{{Codename: fragment, Count: 1}})
		if err != nil {
			return false
		}
		character.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(character)
		frames = append(updates, objectives...)
		return true
	})
	rt.hwanPush(layer.name, frames...)
}

/*
================
hwanEntryOption

The guide's (or the General's) entry row, offered while the arena quest runs
and its objective stands unmet.
================
*/
func (rt *Runtime) hwanEntryOption(c *enterworld.Character, def *Definition, npc string) (NpcOption, bool) {
	arena := hwanArenaForQuest(def.Codename)
	if arena == nil || npc != def.StartNpcCodename {
		return NpcOption{}, false
	}
	at := activeQuestIndex(c, def.RefID)
	if at < 0 || objectiveMet(c, def, c.ActiveQuests[at]) {
		return NpcOption{}, false
	}
	talk := hwanTalk(def.Codename)
	row, prompt := talk+"05", talk+"04"
	if arena.kind == hwanRitual {
		row, prompt = talk+"04", talk+"03"
	}
	return NpcOption{Codename: hwanEntryPrefix + def.Codename, TitleSymbol: row, PromptSymbol: prompt, Complete: true}, true
}

// hwanArenaForQuest finds the arena of an arena quest by its codename.
func hwanArenaForQuest(codename string) *hwanArena {
	for i := range hwanArenas {
		if hwanArenas[i].quest() == codename {
			return &hwanArenas[i]
		}
	}
	return nil
}

/*
================
advanceHwanEntry

The confirmed entry row. Refusals carry the quest's own line: a job outfit
(trial and first ritual) or a summoned trade transport (second ritual) stays
outside. The ritual first hands out the Shaman's Talisman (a full bag only skips
it, as the original only logs); then the solo layer opens and the player lands in it.
================
*/
func (rt *Runtime) advanceHwanEntry(c *enterworld.Character, token, npc string) (OpResult, error) {
	code := strings.TrimPrefix(token, hwanEntryPrefix)
	def, ok := rt.Defs.ByCodename(code)
	arena := hwanArenaForQuest(code)
	if !ok || arena == nil || npc != def.StartNpcCodename || activeQuestIndex(c, def.RefID) < 0 {
		return OpResult{}, fmt.Errorf("quest %s: no arena entry here", code)
	}
	if rt.InstanceInvite == nil || rt.InstancePartyFacts == nil {
		return OpResult{}, fmt.Errorf("quest %s: instance doors unavailable", code)
	}
	if c.World != nil && c.World.InstanceReturn != nil {
		return OpResult{}, fmt.Errorf("quest %s: already inside an instance", code)
	}
	talk := hwanTalk(def.Codename)
	facts := rt.InstancePartyFacts(c)
	switch {
	case arena.kind == hwanTrial && facts.JobSuit:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("quest %s: job outfit", code), hwanTalk(hwanQuest(arena.race, 1, "1")) + "10"}
	case arena.kind == hwanRitual && arena.chain == 1 && facts.JobSuit:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("quest %s: job outfit", code), talk + "11"}
	case arena.kind == hwanRitual && arena.chain == 2 && facts.Transported:
		return OpResult{}, &dialogueRefusal{fmt.Errorf("quest %s: trade transport", code), talk + "11"}
	}
	rt.hwan.mu.Lock()
	_, open := rt.hwan.opened[c.Name]
	rt.hwan.mu.Unlock()
	if open {
		return OpResult{}, fmt.Errorf("quest %s: an arena is already open", code)
	}
	var frames []wire.Frame
	if arena.kind == hwanRitual {
		orb := arena.race.ritualOrb1
		if arena.chain == 2 {
			orb = arena.race.orb2
		}
		if rt.PlanInventory != nil && captureItemCount(c, orb) == 0 {
			rt.deps.Update(c, "hwan-ritual-orb", func() bool {
				rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: orb, Count: 1}})
				if err != nil {
					return false
				}
				c.MissionInventory = rows
				objectives, _ := rt.applyInventoryChange(c)
				frames = append(updates, objectives...)
				return true
			})
		}
	}
	outcome := rt.InstanceInvite(c, InstanceInviteCall{
		Definition: arena.definition, Entry: arena.entry, Solo: true,
		DeadlineMs: rt.hwanNow() + hwanLayerLifeMs + hwanLayerSlackMs,
	})
	if !outcome.Opened || !outcome.LeaderIn {
		return OpResult{Frames: frames}, fmt.Errorf("quest %s: the arena would not open", code)
	}
	rt.hwan.mu.Lock()
	if rt.hwan.leases == nil {
		rt.hwan.leases = make(map[instance.ID]instance.Lease)
	}
	if rt.hwan.opened == nil {
		rt.hwan.opened = make(map[string]instance.ID)
	}
	rt.hwan.leases[outcome.Lease.ID] = outcome.Lease
	rt.hwan.opened[c.Name] = outcome.Lease.ID
	rt.hwan.mu.Unlock()
	return OpResult{Frames: frames}, nil
}
