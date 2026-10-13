/*
===========================================================================

panels.ts - every window the HUD opens as its current panel

The one list of panel names (uiPanels). setPanel and the HUD's current
panel are typed against it, so a window name that is not registered fails
the type check, and the panel sweep test (ui-panel-sweep.test.mjs) opens every entry through
the production HUD to prove each window publishes unique control ids.

panelCloseId names each panel window's close control after the window.
A shared default ("close") was unique only while one window showed; five
crashes (BUG-064, #499, #627, the shop beside its transport) came from two.

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
		"Fortress production",
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

// The panel's close control: "close-window:" + its name, beside the
// select-window / open-window / toggle-window ids that name panels.
const CLOSE_WINDOW_PREFIX = "close-window:";

export type PanelCloseId = `${typeof CLOSE_WINDOW_PREFIX}${UiPanel}`;

/*
================
panelCloseId
================
*/
export function panelCloseId( panel: UiPanel ): PanelCloseId {
	return `${CLOSE_WINDOW_PREFIX}${panel}`;
}

/*
================
closedPanel

The panel a close control id names, or undefined for any other control.
================
*/
export function closedPanel( id: string ): UiPanel | undefined {
	if ( !id.startsWith( CLOSE_WINDOW_PREFIX ) ) return undefined;
	const name = id.slice( CLOSE_WINDOW_PREFIX.length );
	return isUiPanel( name ) ? name : undefined;
}
