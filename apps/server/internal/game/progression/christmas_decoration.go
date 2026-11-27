/*
===========================================================================

christmas_decoration.go - the towns' Christmas decoration and carols alone

Port-only, not native (SRO_CHRISTMAS_EVENT in mode decoration, default
off). The owner wanted the winter look in every city without an event
(2026-11-27): the later client's Christmas decoration layer and the
browser's town carols for the whole window, and nothing else - no event
snow or its start and end notices, no Snow Slaves or flakes, no Jewel
Boxes, no Server Missions. It shares the Christmas switch and window,
in its own mode, so it never runs beside another Christmas event
(christmas_mode.go).

===========================================================================
*/
package progression

/*
================
ChristmasDecoration

The decoration's switch and window. The zero value is off: the native game.
================
*/
type ChristmasDecoration struct {
	SeasonalEvent
}

/*
================
ChristmasDecorationFromEnv

On only in its own mode, like each Christmas event.
================
*/
func ChristmasDecorationFromEnv() ChristmasDecoration {
	event := seasonalEventFromEnv(EnvChristmasEvent, EnvChristmasEventWindow)
	if ChristmasModeFromEnv() != ChristmasModeDecoration {
		event.enabled = false
	}
	return ChristmasDecoration{event}
}
