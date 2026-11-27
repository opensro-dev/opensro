/*
===========================================================================

wiring_christmas.go - the Christmas events' composition

Reads the switch, the UTC window and the mode once at boot (port-only,
not native; progression/christmas.go and sook_christmas.go own the
rules, progression/christmas_mode.go which one runs,
action/event_groups.go the event groups they run). On without a valid
window an event stays off and the boot log says so.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wireChristmasEvent

The Christmas events' share of the gameplay plane: SRO_CHRISTMAS_EVENT's
switch and UTC window, So-Ok's Christmas Gift on the same switch
(SRO_CHRISTMAS_EVENT_MODE=so_ok_2006), the towns' decoration alone
(mode decoration), and the winter Server Missions,
whose rows the authority's metadata keeps across a restart.
================
*/
func wireChristmasEvent(items *action.Runtime, refs enterworld.ItemRefSource, missions action.WinterMissionStore) {
	items.Christmas = christmasEventFromEnv()
	items.SoOkChristmas = sookChristmasFromEnv(refs)
	items.ChristmasDecoration = christmasDecorationFromEnv()
	items.WinterMissionStore = missions
	items.WinterMissionGoals = winterMissionGoalsFromEnv()
}

/*
================
christmasDecorationFromEnv

The composition read of the decoration-only mode, with its boot-log lines.
================
*/
func christmasDecorationFromEnv() progression.ChristmasDecoration {
	event := progression.ChristmasDecorationFromEnv()
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("christmas: %s is on in mode decoration but %s is not a valid START/END UTC window; the towns stay undecorated",
			progression.EnvChristmasEvent, progression.EnvChristmasEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("christmas: the towns wear their Christmas decoration and play carols inside the configured window (%s, mode decoration)", progression.EnvChristmasEvent)
	}
	return event
}

/*
================
christmasEventFromEnv

The composition read of SRO_CHRISTMAS_EVENT and its window, with the
boot-log lines every other port-only rule gets.
================
*/
func christmasEventFromEnv() progression.ChristmasEvent {
	event := progression.ChristmasEventFromEnv()
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("christmas: %s is on but %s is not a valid START/END UTC window; the event stays off",
			progression.EnvChristmasEvent, progression.EnvChristmasEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("christmas: the Christmas event runs inside its configured window (%s)", progression.EnvChristmasEvent)
	}
	return event
}

/*
================
winterMissionGoalsFromEnv

SRO_WINTER_MISSION_GOALS (port-only, not native): fixed test goals for the
winter Server Missions. A value that does not parse keeps the native goals
and says so.
================
*/
func winterMissionGoalsFromEnv() progression.WinterMissionGoals {
	goals, err := progression.WinterMissionGoalsFromEnv()
	if err != nil {
		log.Warnf("christmas: %v; the native mission goals are kept", err)
		return nil
	}
	if goals != nil {
		log.Infof("christmas: the winter Server Missions run on fixed test goals (%s=%v)", progression.EnvWinterMissionGoals, map[uint32]int(goals))
	}
	return goals
}

/*
================
installWinterSpawnReplacements

The winter event's Snow Slaves come from the open world's nests: while it
runs, half of the champions are born Snow Slaves (action/winter_missions.go).
With the event off nothing is installed: the native spawn. A data build
without the winter monsters installs an empty table and says so.
================
*/
func installWinterSpawnReplacements(monsters *simulation.MonsterState, items *action.Runtime) {
	if !items.Christmas.Enabled() {
		return
	}
	snowman, found := monsters.ReferenceByCodename(progression.ChristmasSnowSlaveCodename)
	if !found {
		log.Warnf("christmas: the monster data holds no %s; no Snow Slave will spawn", progression.ChristmasSnowSlaveCodename)
	}
	monsters.SetSpawnReplacements(items.WinterSpawnReplacements(snowman, found))
}

/*
================
sookChristmasFromEnv

The composition read of So-Ok's Christmas Gift (SRO_CHRISTMAS_EVENT in
mode so_ok_2006) with its boot-log lines. A box chance or gift table that
does not parse, or a gift naming an item the data does not hold, keeps
the event's defaults and says so: a typo never hands out nothing.
================
*/
func sookChristmasFromEnv(items enterworld.ItemRefSource) progression.SoOkChristmas {
	mode := progression.ChristmasModeFromEnv()
	if mode == progression.ChristmasModeUnknown {
		log.Warnf("christmas: %s names no Christmas event (winter, so_ok_2006 or decoration); no Christmas event runs", progression.EnvChristmasEventMode)
	}
	event := progression.SoOkChristmasFromEnv()
	if mode != progression.ChristmasModeSoOk2006 {
		return event
	}
	if err := event.ConfigErr(); err != nil {
		log.Warnf("christmas: %v; the default is kept", err)
	}
	if items != nil {
		for _, gift := range event.Gifts() {
			if _, ok := items.ItemRefByCodename(gift.Codename); !ok {
				log.Warnf("christmas: %s names %s, which the item data does not hold; So-Ok's default gift table is kept", progression.EnvSoOkChristmasGifts, gift.Codename)
				event = event.WithGifts(progression.SoOkChristmasDefaultGifts())
				break
			}
		}
	}
	if event.Enabled() && !event.WindowValid() {
		log.Warnf("christmas: %s is on but %s is not a valid START/END UTC window; So-Ok's Christmas Gift stays off",
			progression.EnvChristmasEvent, progression.EnvChristmasEventWindow)
	}
	if event.Enabled() && event.WindowValid() {
		log.Infof("christmas: So-Ok's Christmas Gift runs inside its configured window (%s=%s, box chance %.2f%%)",
			progression.EnvChristmasEventMode, progression.ChristmasModeSoOk2006, event.BoxChancePercent())
	}
	return event
}

/*
================
sookChristmasNpcLook

So-ok's Santa look for the running Christmas event's window, or nil when
neither runs in this boot's posture (the dressed roster is built once, so
the window read at boot is the one that dresses her): So-Ok's Christmas
Gift, or the winter event (port-only, owner 2026-11-16: the host of
Winter Princess and Knights wears the Santa dress too).
================
*/
func sookChristmasNpcLook() *enterworld.NpcLookConfig {
	event := progression.SoOkChristmasFromEnv().SeasonalEvent
	if !event.Enabled() {
		event = progression.ChristmasEventFromEnv().SeasonalEvent
	}
	startsAt, endsAt, valid := event.Window()
	if !event.Enabled() || !valid {
		return nil
	}
	return &enterworld.NpcLookConfig{
		Codename:     progression.SoOkChristmasHostCodename,
		LookCodename: progression.SoOkChristmasLookCodename,
		StartsAt:     startsAt,
		EndsAt:       endsAt,
	}
}
