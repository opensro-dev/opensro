/*
===========================================================================

pandorabox.go - Pandora's Box: ten waves of strong monsters

Port-only, not native to v1.150: the item ships in the v1.150 data
(ITEM_ETC_E060517_MON_GENERATION_BOX, "Pandora's Box", 3/3/15/1) and the
survival arena drops it as a ground prop (sookarena_powerups.go), but no
v1.150 server opens it from the bag. The rules are the v1.188 server's
(its Pandora's Box quest event, traced 2026-11-16):

  - The refusals are the summon scroll's: a town region, a user below
    level 30, the battle arena (the two items share one event base);
    none spends the box.
  - The box opens at the user's position and runs a job ticking every
    5 s for 240 ticks: at most 20 minutes.
  - Wave k (0..9) is four monsters of the strong table's kind at level
    min(user level, 110) - 9 + k, within about 50 units of
    the box, at grade 7. The next wave lands only once every monster of
    the last is dead, checked on the job's tick.
  - A grade-7 monster leaves after 300 s; when one does, the event ends
    (the box dies with it and the job ends). So does the tenth wave's
    clearing, and the job's last tick.
  - No owner: anyone may fight the waves; their kills pay ordinary loot
    and experience (the special drop handler returns for this event).

The box itself is MOB_EVENT_PANDORA, a monster whose HP bar counts the
twenty minutes down; v1.150 ships no such reference, so the port keeps
the event at the opening point without a box and says each step to the
opener in a notice instead (port-only). The kinds are the summon
scroll's table (partysummonscroll.go); the box reads its keys from 21,
and the rows below 30 are pandoraLowKinds. A level-30 opener's first
waves read keys 21..29; below key 21 the native lookup pads with key 21.
The waves' tactics are chosen with their grade 7, unlike the
scroll's grade 0.

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
	"opensro.online/server/internal/game/world"
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
	// ticks: the event lasts at most twenty minutes.
	pandoraTickMs = 5000
	pandoraTicks  = 240
	// pandoraRadius is the wave's spread around the box.
	pandoraRadius = 50
)

// pandoraLowKinds are the table's keys 21 to 29, below the summon
// scroll's: the v1.188 server's level map (first insert per key; key
// 28's second insert, MOB_EVE_STRONG_AM_SOLDIER, does not overwrite), as
// the v1.150 MOB_<X> each MOB_EVE_STRONG_<X> copies.
var pandoraLowKinds = [partySummonMinLevel - pandoraLowKey]string{
	"MOB_AM_CRAB_CLON", "MOB_WC_DEVILBUG", "MOB_AM_CRAB", "MOB_AM_WOLF_CLON", "MOB_WC_DEMONHORSE",
	"MOB_AM_ARCHER_CLON", "MOB_WC_EARTHGHOST", "MOB_WC_EARTHGHOST_CLON", "MOB_AM_PUNISHER_CLON",
}

// The opener's notices. Port-only: the native box shows its countdown on
// its own HP bar, which v1.150 cannot spawn.
const (
	pandoraOpenLine     = "Pandora's Box is open: ten waves of monsters will come. Clear each one within 5 minutes."
	pandoraWaveLine     = "Pandora's Box: wave %d of %d."
	pandoraClearedLine  = "Pandora's Box: every wave is cleared!"
	pandoraEscapedLine  = "Pandora's Box: the monsters were not cleared in time. The box has closed."
	pandoraTimeLine     = "Pandora's Box: time is up. The box has closed."
	pandoraNoGroundLine = "Pandora's Box: no monster could land here. The box has closed."
)

/*
================
pandoraEvent

One opened box: where it stands, whose level sets its waves, the wave
standing now and when it landed, and when the job's last tick falls.
================
*/
type pandoraEvent struct {
	division string
	lease    instance.Lease
	anchor   simulation.Spawn
	opener   string
	level    int64
	wave     int
	gids     []uint32
	waveAtMs int64
	nextMs   int64
	endsAtMs int64
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
	key := min(level, partySummonMaxLevel) - pandoraLevelSpan + int64(wave)
	key = max(key, pandoraLowKey)
	row := func(at int64) string {
		if at < partySummonMinLevel {
			return pandoraLowKinds[at-pandoraLowKey]
		}
		return partySummonKinds[at-partySummonMinLevel]
	}
	for at := key; at >= pandoraLowKey; at-- {
		if kind := row(at); kind != "" && has(kind) {
			return kind, true
		}
	}
	for at := key + 1; at <= partySummonMaxLevel; at++ {
		if kind := row(at); kind != "" && has(kind) {
			return kind, true
		}
	}
	return "", false
}

/*
================
Runtime.usePandoraBox

The native refusals answer first and spend nothing; then the box opens
where the user stands, its first wave lands at once, and the box is
spent.
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
	at := rt.liveSpawn(simulation.WorldKey(use.division, c.Name), c, use.nowMs)
	if battlefield, known := world.RegionPlayerCombat(at.RegionID); !known || !battlefield {
		result.Frames = append(result.Frames, exchangeBanner(partySummonTownLine))
		return false
	}
	if *c.Level < partySummonMinLevel {
		result.Frames = append(result.Frames, exchangeBanner(partySummonLevelLine))
		return false
	}
	if rt.sookRun(use.division, c) != nil {
		result.Frames = append(result.Frames, exchangeBanner(partySummonArenaLine))
		return false
	}
	lease, admitted := rt.EntryPopulationLease(use.division, c.Name)
	if !admitted {
		result.DiagnosticRefusal = "item-use: Pandora's Box opener is not in the world"
		return false
	}
	event := &pandoraEvent{
		division: use.division, lease: lease, anchor: at, opener: c.Name, level: *c.Level,
		endsAtMs: use.nowMs + pandoraTickMs*pandoraTicks,
	}
	if !rt.landPandoraWave(event, use.nowMs) {
		result.Frames = append(result.Frames, exchangeBanner(pandoraNoGroundLine))
		return false
	}
	rt.pandora.mu.Lock()
	rt.pandora.events = append(rt.pandora.events, event)
	rt.pandora.mu.Unlock()
	remaining := rt.consumeItemUseRow(c, use.row)
	*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)}}}
	result.Frames = append(result.Frames, exchangeBanner(pandoraOpenLine), exchangeBanner(fmt.Sprintf(pandoraWaveLine, 1, pandoraWaves)))
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

/*
================
Runtime.landPandoraWave

Lands the event's next wave: four monsters of its kind around the box,
grade 7, aggressive, each with the grade's 300 s. False when no monster
of it landed.
================
*/
func (rt *Runtime) landPandoraWave(event *pandoraEvent, nowMs int64) bool {
	kind, found := pandoraWaveKind(event.level, event.wave, func(codename string) bool {
		_, held := rt.Monsters.ReferenceByCodename(codename)
		return held
	})
	if !found {
		return false
	}
	request := simulation.QuestMonsterSpawn{
		Division: event.division, Population: event.lease, Codename: kind,
		Position: event.anchor, FixedPosition: true, NowMs: nowMs, RadiusSpan: pandoraRadius,
		LifetimeMs: partySummonLifetimeMs, Rarity: partySummonGrade, HasRarity: true, Aggressive: true,
		TacticsGrade: partySummonGrade,
	}
	event.gids = event.gids[:0]
	for range pandoraWaveSize {
		if gid, landed := rt.Monsters.SpawnQuestMonsterGID(request); landed {
			event.gids = append(event.gids, gid)
		}
	}
	if len(event.gids) == 0 {
		return false
	}
	event.wave++
	event.waveAtMs = nowMs
	event.nextMs = nowMs + pandoraTickMs
	return true
}

/*
================
Runtime.advancePandoraBoxes

Each open box's job tick, every 5 s: the job's end closes it; a wave
still standing at its 300 s closes it (its monsters leave, natively the
box dies with them); a cleared wave brings the next, or ends the event
after the tenth. The opener hears each step.
================
*/
func (rt *Runtime) advancePandoraBoxes() {
	if rt.Monsters == nil {
		return
	}
	nowMs := rt.Now().UnixMilli()
	rt.pandora.mu.Lock()
	defer rt.pandora.mu.Unlock()
	kept := rt.pandora.events[:0]
	for _, event := range rt.pandora.events {
		if nowMs < event.nextMs {
			kept = append(kept, event)
			continue
		}
		event.nextMs = nowMs + pandoraTickMs
		line, open := rt.stepPandoraBox(event, nowMs)
		if line != "" && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(event.division, event.opener, []wire.Frame{exchangeBanner(line)})
		}
		if open {
			kept = append(kept, event)
		}
	}
	clear(rt.pandora.events[len(kept):])
	rt.pandora.events = kept
}

/*
================
Runtime.stepPandoraBox

One tick of one box: the line to tell its opener, and whether it stays
open.
================
*/
func (rt *Runtime) stepPandoraBox(event *pandoraEvent, nowMs int64) (string, bool) {
	alive := 0
	for _, gid := range event.gids {
		if rt.Monsters.QuestMonsterAlive(event.division, gid) {
			alive++
		}
	}
	if nowMs >= event.endsAtMs {
		return pandoraTimeLine, false
	}
	// A wave still standing when its 300 s would end before the next
	// tick escapes: the box closes now, so a wave found empty later was
	// always killed, never timed out (the lifetime removes it unseen).
	if alive > 0 {
		if nowMs+pandoraTickMs >= event.waveAtMs+partySummonLifetimeMs {
			return pandoraEscapedLine, false
		}
		return "", true
	}
	if event.wave >= pandoraWaves {
		return pandoraClearedLine, false
	}
	if !rt.landPandoraWave(event, nowMs) {
		return pandoraNoGroundLine, false
	}
	return fmt.Sprintf(pandoraWaveLine, event.wave, pandoraWaves), true
}
