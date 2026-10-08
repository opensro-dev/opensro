/*
===========================================================================

compact-hud.ts - fitting the original HUD controls on small browser screens

Port-only, not native. The owner approved automatic adaptation when the
desktop's 800-pixel underbar or 600-pixel canvas no longer fits. Secondary
panels start collapsed without changing saved desktop preferences.

===========================================================================
*/
import type { UiRect, UiControl } from "@/engine/contracts/ui";

const DESKTOP_WIDTH = 800;
const DESKTOP_HEIGHT = 600;
const SLOT_COUNT = 11;
const SLOT_PITCH = 44;
const TOOL_HEIGHT = 40;
const TOOL_COUNT = 11;
const MARGIN = 4;
const CLOSE_TOUCH_SIZE = 40;

/*
================
compactHudLayout
================
*/
export function compactHudLayout( width: number, height: number ) {
	if ( width >= DESKTOP_WIDTH && height >= DESKTOP_HEIGHT ) return null;
	const capacity = Math.max( 1, Math.floor( (width - MARGIN * 2) / SLOT_PITCH ) );
	const rows = Math.ceil( SLOT_COUNT / capacity ), columns = Math.ceil( SLOT_COUNT / rows );
	const bottom = rows * SLOT_PITCH + TOOL_HEIGHT + MARGIN * 2;
	const left = Math.floor( (width - columns * SLOT_PITCH) / 2 ), top = Math.max( 0, height - bottom );
	const slots: UiRect[] = Array.from( { length: SLOT_COUNT }, ( _, index ) => [
		left + index % columns * SLOT_PITCH,
		top + MARGIN + Math.floor( index / columns ) * SLOT_PITCH,
		SLOT_PITCH,
		SLOT_PITCH
	] );
	const toolWidth = Math.max( 0, Math.min( 48, Math.floor( (width - MARGIN * 2) / TOOL_COUNT ) ) );
	const tools: UiRect[] = Array.from( { length: TOOL_COUNT }, ( _, index ) => [
		Math.floor( (width - toolWidth * TOOL_COUNT) / 2 ) + index * toolWidth,
		height - TOOL_HEIGHT - MARGIN,
		toolWidth,
		TOOL_HEIGHT
	] );
	return { slots, tools, bottom, top, bounds: [ 0, 0, width, top ] as UiRect };
}

/*
================
createCompactHud

One secondary overlay at a time keeps narrow screens usable. Rotation
retains the open overlay; returning from desktop starts collapsed again.
================
*/
export function createCompactHud() {
	let active = false, overlay = "";
	return {
		/*
		================
		layout
		================
		*/
		layout( width: number, height: number ) {
			const layout = compactHudLayout( width, height );
			if ( !!layout !== active ) overlay = "";
			active = !!layout;
			return layout ? { ...layout, overlay } : null;
		},
		/*
		================
		open
		================
		*/
		open( next: string ) {
			if ( active ) overlay = next;
		},
		/*
		================
		toggle
		================
		*/
		toggle( next: string ) {
			overlay = overlay === next ? "" : next;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			active = false;
			overlay = "";
		}
	};
}

/*
================
compactWindowDrag

Window positions remain desktop preferences. Gameplay carries and shortcut
binding drags use the published hit rectangles and must remain enabled.
================
*/
export function compactWindowDrag( control: UiControl ): boolean {
	return control.id.endsWith( "-drag" ) || control.id.startsWith( "window-drag:" ) || control.id === "map-pan";
}

/*
================
compactCloseControl

Fitting large artwork must not make its only exit too small to touch.
The close artwork stays native; its hit area grows into the title bar.
================
*/
export function compactCloseControl( control: UiControl, screen: UiRect ): UiControl {
	if ( control.id !== "close" && !control.id.endsWith( "-close" ) ) return control;
	const [x, y, width, height] = control.rect;
	const touchWidth = Math.min( screen[2], Math.max( width, CLOSE_TOUCH_SIZE ) );
	const touchHeight = Math.min( screen[3], Math.max( height, CLOSE_TOUCH_SIZE ) );
	return {
		...control,
		rect: [
			Math.max( screen[0], Math.min( x + width / 2 - touchWidth / 2, screen[0] + screen[2] - touchWidth ) ),
			Math.max( screen[1], Math.min( y + height / 2 - touchHeight / 2, screen[1] + screen[3] - touchHeight ) ),
			touchWidth,
			touchHeight
		]
	};
}
