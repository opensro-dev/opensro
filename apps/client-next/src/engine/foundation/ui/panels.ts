/*
===========================================================================

panels.ts - every window the HUD opens as its current panel

The one list of panel names (uiPanels). setPanel and the HUD's current
panel are typed against it, so a window name that is not registered fails
the type check, and the panel sweep test (ui-panel-sweep.test.mjs) opens every entry through
the production HUD to prove each window publishes unique control ids.

===========================================================================
*/

/*
================
uiPanels
================
*/
export function uiPanels() {
	return [
		"Academy",
		"Academy Matching",
		"Actions",
		"Alchemy",
		"Auto Potion",
		"Blocking",
		"COS inventory",
		"Character",
		"Chat",
		"Experimental",
		"Fortress war application",
		"Fortress war schedule",
		"Fortress tax",
		"Game Guide",
		"Guild",
		"Guild tools",
		"Inventory",
		"Job ranking",
		"Magic Pop",
		"Magic option",
		"Map",
		"Option",
		"Party",
		"Party Matching",
		"Quests",
		"Shop",
		"Skills",
		"Skin change",
		"Stall network",
		"Storage",
		"System"
	] as const;
}

export type UiPanel = ReturnType<typeof uiPanels>[number];

/*
================
isUiPanel

Admit a window name that arrives as text (an open-window control id).
================
*/
export function isUiPanel( name: string ): name is UiPanel {
	return (uiPanels() as readonly string[]).includes( name );
}
