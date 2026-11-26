/*
===========================================================================

pandorabox.go - Pandora's Box: ten waves of strong monsters

Port-only, not native to v1.150: the item ships in the v1.150 data
(ITEM_ETC_E060517_MON_GENERATION_BOX, "Pandora's Box", 3/3/15/1) and the
survival arena drops it as a ground prop (sookarena_powerups.go), but no
v1.150 server opens it from the bag. The rules are the v1.188 server's
(its Pandora's Box quest event, traced 2026-11-16 and read again
2026-11-26 for #4540; the PR's native-claims table holds the evidence):

  - The refusals are the summon scroll's, in its order: a
    town region ("You cannot use Pandora's Box.", the v1.150 text's
    SN_TALK_QEV_CH_EVENT_KISAENG_060517_N), a user below level 30, the
    battle arena; none spends the box. An opened box answers
    SN_TALK_QEV_CH_EVENT_KISAENG_060517_M.

  - The box opens at the user's position and runs a job ticking every
    5 s for 240 ticks. The first tick runs as the job registers and
    only sets the box up, so the first wave lands 5 s after the
    opening and the last tick falls 239 ticks (1195 s) after it.

  - Wave k (0..9) is four monsters of the strong table's kind at level
    min(user level, 110) - 9 + k, at grade 7, on the event spawner's
    ring of 50 around the box (half in the outer third,
    simulation.EventSpawnerRing). The next wave
    lands only once every monster of the last is dead, checked on the
    job's tick (the spawner's live count). A wave none of whose
    monsters could land still counts: the counter advances and the next
    tick brings the next wave.

  - A grade-7 monster leaves after 300 s; when one does, the event
    ends (the box dies with it and the job ends). So does the tenth
    wave's clearing, and the job's last tick.

  - No owner: anyone may fight the waves; their kills pay ordinary loot
    and experience (the special drop handler returns for this event).

  - The box itself is MOB_EVENT_PANDORA (the Christmas import stages
    the v1.696 row, scripts/winter_event_import.py). It lands on the
    monster factory's ring of 20 around the opener and stands
    untouchable (body mode 2, never restored), so no player kills it.
    Each tick drains its HP toward the clock: HP permille falls to
    1000 - tick * 1000 / 240, through the native resource cost that
    never takes the last point, so its HP bar counts the twenty minutes
    down. It casts MSKILL_EVENT_PANDORA_ATTACK01 as each wave lands, and
    leaves when the event ends.

A shard without the box row (its build ran without the v1.696 client)
keeps the event at the opening point without a box and tells the opener
each step in a notice instead (port-only). The kinds are the summon
scroll's table
(partysummonscroll.go); the box reads its keys from 21, and the rows
below 30 are pandoraLowKinds. A level-30 opener's first waves read keys
21..29; below key 21 the native lookup pads with key 21. The waves'
tactics are chosen with their grade 7, unlike the scroll's grade 0.

Like the scroll, the box reaches players only through So-Ok's Christmas
Gift, so it is served while that event's mode is selected.

===========================================================================
*/
package action

import (
	"fmt"
	"sync"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// PandoraBoxItem is the box the port opens.
const PandoraBoxItem = "ITEM_ETC_E060517_MON_GENERATION_BOX"

const (
	// pandoraWaves and pandoraWaveSize are the native ten waves of four.
	pandoraWaves    = 10
	pandoraWaveSize = 4
	// pandoraLevelSpan is how far below the user's level the first wave
	// stands: wave k reads level L - 9 + k.
	pandoraLevelSpan = 9
	// pandoraLowKey is the table's lowest key.
	pandoraLowKey = 21
	// pandoraTickMs and pandoraTicks are the job's 5 s tick and its 240
	// ticks; the first runs at the opening, so the last falls 239 ticks
	// later.
	pandoraTickMs = 5000
	pandoraTicks  = 240
	// pandoraRadius is the event spawner's ring around the box.
	pandoraRadius = 50
	// pandoraBoxRadius is the factory ring the box lands on around its
	// opener.
	pandoraBoxRadius = 20
	// pandoraPermille is the drain's HP scale.
	pandoraPermille = 1000
)

// The box monster and the cast it plays as each wave lands.
const (
	pandoraBoxMonster = "MOB_EVENT_PANDORA"
	pandoraBoxSkill   = "MSKILL_EVENT_PANDORA_ATTACK01"
)

// pandoraLowKinds are the table's keys 21 to 29, below the summon
// scroll's: the v1.188 server's level map (first insert per key; key
// 28's second insert, MOB_EVE_STRONG_AM_SOLDIER, does not overwrite), as
// the v1.150 MOB_<X> each MOB_EVE_STRONG_<X> copies.
var pandoraLowKinds = [partySummonMinLevel - pandoraLowKey]string{
	"MOB_AM_CRAB_CLON", "MOB_WC_DEVILBUG", "MOB_AM_CRAB", "MOB_AM_WOLF_CLON", "MOB_WC_DEMONHORSE",
	"MOB_AM_ARCHER_CLON", "MOB_WC_EARTHGHOST", "MOB_WC_EARTHGHOST_CLON", "MOB_AM_PUNISHER_CLON",
}

// The native lines, the v1.150 text's English rows.
const (
	pandoraTownLine = "You cannot use Pandora's Box."
	pandoraOpenLine = "Drawn by the box of mysterious power, a strong monster appears."
)

// pandoraGate is the box's refusals: the town line its own, the level the
// party scroll's.
var pandoraGate = summonEventGate{townLine: pandoraTownLine, minLevel: partySummonMinLevel, levelLine: partySummonLevelLine}

// The opener's step notices. Port-only, for a shard without the box: the
// native box shows its countdown on its own HP bar.
const (
	pandoraWaveLine     = "Pandora's Box: wave %d of %d."
	pandoraClearedLine  = "Pandora's Box: every wave is cleared!"
	pandoraEscapedLine  = "Pandora's Box: the monsters were not cleared in time. The box has closed."
	pandoraTimeLine     = "Pandora's Box: time is up. The box has closed."
	pandoraNoGroundLine = "Pandora's Box cannot open here: no monster could land."
	pandoraNoKindLine   = "Pandora's Box: no monster of the next wave is here. The box has closed."
)

/*
================
pandoraEvent

One opened box: where it stands, whose level sets its waves, the box
monster (zero when none stands), the wave standing now and when it
landed, and when the job opened and its next and last ticks fall.
================
*/
type pandoraEvent struct {
	division string
	lease    instance.Lease
	anchor   simulation.Spawn
	opener   string
	level    int64
	box      uint32
	wave     int
	gids     []uint32
	waveAtMs int64
	openedMs int64
	nextMs   int64
	endsAtMs int64
}

/*
================
pandoraEvent.tick

The job's tick number at nowMs: the opening's setup is tick 1.
================
*/
func (event *pandoraEvent) tick(nowMs int64) int64 {
	return min(1+(nowMs-event.openedMs)/pandoraTickMs, pandoraTicks)
}

/*
================
pandoraEvent.tells

Whether the opener hears line: the step notices stand in for a box the
shard could not land, and a closing for a missing kind is always told.
================
*/
func (event *pandoraEvent) tells(line string) bool {
	return line != "" && (event.box == 0 || line == pandoraNoKindLine)
}

/*
================
pandoraEvent.open

The job's clock from the opening at nowMs: its first tick is the
opening's own setup, so the next (the first wave's) falls one tick
later and the last 239 ticks later.
================
*/
func (event *pandoraEvent) open(nowMs int64) {
	event.openedMs = nowMs
	event.nextMs = nowMs + pandoraTickMs
	event.endsAtMs = nowMs + pandoraTickMs*(pandoraTicks-1)
}

/*
================
pandoraBoxes

The open boxes; the item door adds, the tick advances and retires.
================
*/
type pandoraBoxes struct {
	mu     sync.Mutex
	events []*pandoraEvent
}

/*
================
pandoraWaveKind

The kind wave (0-based) of a box opened at level calls: the table's row
at min(level, 110) - 9 + wave, or the nearest lower row the shard holds,
or failing that the nearest higher one.
================
*/
func pandoraWaveKind(level int64, wave int, has func(string) bool) (string, bool) {
	return strongSummonKind(min(level, partySummonMaxLevel)-pandoraLevelSpan+int64(wave), has)
}

/*
================
Runtime.usePandoraBox

The native refusals answer first and spend nothing; then the box opens
where the user stands and is spent: the box monster lands beside it
(the job's first tick), and its first wave lands on the job's next
tick. Port-only: ground where the port lands no monster (a
cave floor, or no kind of the first wave on the shard) refuses the box
without spending it; the native box opens anywhere outside a town.
================
*/
func (rt *Runtime) usePandoraBox(use persistentSummonUse, result *OpResult) bool {
	c := use.character
	if !rt.SoOkChristmas.Selected() {
		result.DiagnosticRefusal = "item-use: Pandora's Box is served only in So-Ok's Christmas Gift's mode"
		return false
	}
	if rt.Monsters == nil || !enterworld.CharacterAlive(c) || c.Level == nil {
		result.DiagnosticRefusal = "item-use: Pandora's Box owner unavailable"
		return false
	}
	at, ok := rt.summonEventRefusal(use, pandoraGate, result)
	if !ok {
		return false
	}
	if _, found := pandoraWaveKind(*c.Level, 0, rt.holdsMonster); !found || !simulation.FeaturesAt(at.RegionID).QuestMonsterSpawns {
		result.Frames = append(result.Frames, exchangeBanner(pandoraNoGroundLine))
		return false
	}
	lease, admitted := rt.EntryPopulationLease(use.division, c.Name)
	if !admitted {
		result.DiagnosticRefusal = "item-use: Pandora's Box opener is not in the world"
		return false
	}
	event := &pandoraEvent{division: use.division, lease: lease, anchor: at, opener: c.Name, level: *c.Level}
	event.open(use.nowMs)
	event.box = rt.landPandoraBox(event, use.nowMs)
	rt.pandora.mu.Lock()
	rt.pandora.events = append(rt.pandora.events, event)
	rt.pandora.mu.Unlock()
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, exchangeBanner(pandoraOpenLine))
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

/*
================
Runtime.landPandoraBox

The box monster on the factory ring of 20 around the opener, untouchable
for good; zero when the shard holds no box row or no point lands it.
================
*/
func (rt *Runtime) landPandoraBox(event *pandoraEvent, nowMs int64) uint32 {
	if !rt.holdsMonster(pandoraBoxMonster) {
		return 0
	}
	gid, landed := rt.Monsters.SpawnQuestMonsterGID(simulation.QuestMonsterSpawn{
		Division: event.division, Population: event.lease, Codename: pandoraBoxMonster,
		Position: event.anchor, FixedPosition: true, NowMs: nowMs,
		Ring: simulation.FactoryRing(pandoraBoxRadius), BodyStatus: untouchableBodyStatus,
	})
	if !landed {
		return 0
	}
	return gid
}

/*
================
pandoraDrainCost

The HP the job's tick takes from a box at hp of maxHP: whatever stands
above the clock's permille, 1000 - tick * 1000 / 240, in the native
integer steps.
================
*/
func pandoraDrainCost(hp, maxHP uint32, tick int64) uint32 {
	if maxHP == 0 {
		return 0
	}
	target := pandoraPermille - tick*pandoraPermille/pandoraTicks
	current := int64(hp) * pandoraPermille / int64(maxHP)
	if current <= target {
		return 0
	}
	return uint32(int64(maxHP) * (current - target) / pandoraPermille)
}

/*
================
Runtime.drainPandoraBox

One tick's drain of the event's box. A box that no longer stands (its
population retired) is dropped, as the native job drops an invalid box
and runs on.
================
*/
func (rt *Runtime) drainPandoraBox(event *pandoraEvent, nowMs int64) {
	if event.box == 0 {
		return
	}
	box, ok := rt.Monsters.Get(event.division, event.box)
	if !ok || box.CurrentHP == 0 {
		event.box = 0
		return
	}
	if cost := pandoraDrainCost(box.CurrentHP, box.EffectiveMaxHP(), event.tick(nowMs)); cost > 0 {
		rt.Monsters.ConsumeMonsterHP(event.division, event.box, cost)
	}
}

/*
================
Runtime.pandoraBoxCast

The box's cast as a wave lands, the frames due now; none without a box
or the skill row.
================
*/
func (rt *Runtime) pandoraBoxCast(event *pandoraEvent, nowMs int64) []simulation.Frame {
	if event.box == 0 {
		return nil
	}
	skill, ok := rt.skillByCodename(pandoraBoxSkill)
	if !ok {
		return nil
	}
	duration, valid := skill.ActionLifecycleMs()
	if !valid {
		return nil
	}
	return rt.monsterUntargetedCast(event.division, event.box, skill, duration, nowMs)
}

/*
================
Runtime.landPandoraWave

Lands the event's next wave: four monsters of its kind on the ring
around the box, grade 7, aggressive, each with the grade's 300 s. The
wave counts whatever landed, as natively; false only when the shard
holds no kind for it (inference: the native table always resolves, so a
port shard without one closes the box rather than tick on empty).
================
*/
func (rt *Runtime) landPandoraWave(event *pandoraEvent, nowMs int64) bool {
	kind, found := pandoraWaveKind(event.level, event.wave, rt.holdsMonster)
	if !found {
		return false
	}
	request := simulation.QuestMonsterSpawn{
		Division: event.division, Population: event.lease, Codename: kind,
		Position: event.anchor, FixedPosition: true, NowMs: nowMs, Ring: simulation.EventSpawnerRing(pandoraRadius),
		LifetimeMs: partySummonLifetimeMs, Rarity: partySummonGrade, HasRarity: true, Aggressive: true,
		TacticsGrade: partySummonGrade,
	}
	event.gids = event.gids[:0]
	for range pandoraWaveSize {
		if gid, landed := rt.Monsters.SpawnQuestMonsterGID(request); landed {
			event.gids = append(event.gids, gid)
		}
	}
	event.wave++
	event.waveAtMs = nowMs
	event.nextMs = nowMs + pandoraTickMs
	return true
}

/*
================
Runtime.advancePandoraBoxes

Each open box's job tick, every 5 s: the box drains toward the clock;
the job's end closes it; a wave still standing at its 300 s closes it
(its monsters leave, natively the box dies with them); a cleared wave
brings the next, with the box's cast, or ends the event after the
tenth. A closed event's box leaves. The casts go out from the box to
whoever sees it; a box-less event tells its opener each step instead.
================
*/
func (rt *Runtime) advancePandoraBoxes(nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil {
		return nil
	}
	rt.pandora.mu.Lock()
	defer rt.pandora.mu.Unlock()
	var out []simulation.DivisionFrames
	kept := rt.pandora.events[:0]
	for _, event := range rt.pandora.events {
		if nowMs < event.nextMs {
			kept = append(kept, event)
			continue
		}
		event.nextMs = nowMs + pandoraTickMs
		rt.drainPandoraBox(event, nowMs)
		line, open, cast := rt.stepPandoraBox(event, nowMs)
		if len(cast) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: event.division, SourceGID: event.box, Frames: cast})
		}
		if event.tells(line) && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(event.division, event.opener, []wire.Frame{exchangeBanner(line)})
		}
		if open {
			kept = append(kept, event)
		} else if event.box != 0 {
			rt.Monsters.ExpireQuestMonster(event.division, event.box, nowMs)
		}
	}
	clear(rt.pandora.events[len(kept):])
	rt.pandora.events = kept
	return out
}

/*
================
Runtime.stepPandoraBox

One tick of one box: the step's line, whether it stays open, and the
box's cast when a wave landed.
================
*/
func (rt *Runtime) stepPandoraBox(event *pandoraEvent, nowMs int64) (string, bool, []simulation.Frame) {
	alive := 0
	for _, gid := range event.gids {
		if rt.Monsters.QuestMonsterAlive(event.division, gid) {
			alive++
		}
	}
	if nowMs >= event.endsAtMs {
		return pandoraTimeLine, false, nil
	}
	// A wave still standing when its 300 s would end before the next
	// tick escapes: the box closes now, so a wave found empty later was
	// always killed, never timed out (the lifetime removes it unseen).
	if alive > 0 {
		if nowMs+pandoraTickMs >= event.waveAtMs+partySummonLifetimeMs {
			return pandoraEscapedLine, false, nil
		}
		return "", true, nil
	}
	if event.wave >= pandoraWaves {
		return pandoraClearedLine, false, nil
	}
	if !rt.landPandoraWave(event, nowMs) {
		return pandoraNoKindLine, false, nil
	}
	return fmt.Sprintf(pandoraWaveLine, event.wave, pandoraWaves), true, rt.pandoraBoxCast(event, nowMs)
}

/*
================
Runtime.holdsMonster

Whether the shard holds the monster codename, for the wave table's
fallbacks.
================
*/
func (rt *Runtime) holdsMonster(codename string) bool {
	_, held := rt.Monsters.ReferenceByCodename(codename)
	return held
}
