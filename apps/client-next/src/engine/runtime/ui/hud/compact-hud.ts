/*
===========================================================================

compact-hud.ts - fitting the original HUD controls on small browser screens

Port-only, not native. The owner approved automatic adaptation when the
desktop's 800-pixel underbar or 600-pixel canvas no longer fits. Two
arrangements share that gate (#2406): a coarse primary pointer (a finger)
gets the phone layout - 44px slots, an 11-button tool row, one overlay at
a time. A fine pointer (mouse, pen) keeps the native HUD and degrades it
instead: the underbar scales to the window, the party board packs a
bounded left band without buff rows, and a window that cannot fit the
viewport scales into it. Secondary panels start collapsed without
changing saved desktop preferences.

===========================================================================
*/
import type { UiRect, UiControl } from "@/engine/contracts/ui";
import { closedPanel } from "@/engine/foundation/ui/panels";

const DESKTOP_WIDTH = 800;
const DESKTOP_HEIGHT = 600;
// The authored underbar the small arrangement scales: it spans the native
// canvas (800x52) at its bottom, and its scaled height sets the band the
// party board and the chat clear.
export const UNDERBAR_WIDTH = 800;
export const UNDERBAR_HEIGHT = 52;
const SLOT_COUNT = 11;
const SLOT_PITCH = 44;
const EXP_HEIGHT = 20;
const TOOL_HEIGHT = 40;
const TOOL_COUNT = 11;
const MARGIN = 4;
const CLOSE_TOUCH_SIZE = 40;

/*
================
compactHudLayout

The phone arrangement: below the original's smallest mode (800x600) with
a coarse primary pointer. A fine pointer never gets it, however small
the window - it keeps the native HUD (smallHudLayout).
================
*/
export function compactHudLayout( width: number, height: number, coarsePointer: boolean ) {
	if ( !coarsePointer || width >= DESKTOP_WIDTH && height >= DESKTOP_HEIGHT ) return null;
	const capacity = Math.max( 1, Math.floor( (width - MARGIN * 2) / SLOT_PITCH ) );
	const rows = Math.ceil( SLOT_COUNT / capacity ), columns = Math.ceil( SLOT_COUNT / rows );
	const expHeight = height >= 240 ? EXP_HEIGHT : 0;
	const bottom = rows * SLOT_PITCH + expHeight + TOOL_HEIGHT + MARGIN * 2;
	const left = Math.floor( (width - columns * SLOT_PITCH) / 2 ), top = Math.max( 0, height - bottom );
	const slots: UiRect[] = Array.from( { length: SLOT_COUNT }, ( _, index ) => [
		left + index % columns * SLOT_PITCH,
		top + MARGIN + Math.floor( index / columns ) * SLOT_PITCH,
		SLOT_PITCH,
		SLOT_PITCH
	] );
	const expWidth = width >= 412 ? 406 : width >= 260 ? 256 : 200;
	const exp: UiRect | null = expHeight > 0 ?
		[
			Math.max( 0, Math.floor( (width - expWidth) / 2 ) ),
			top + MARGIN + rows * SLOT_PITCH,
			Math.min( width, expWidth ),
			expHeight
		] :
		null;
	const toolWidth = Math.max( 0, Math.min( 48, Math.floor( (width - MARGIN * 2) / TOOL_COUNT ) ) );
	const tools: UiRect[] = Array.from( { length: TOOL_COUNT }, ( _, index ) => [
		Math.floor( (width - toolWidth * TOOL_COUNT) / 2 ) + index * toolWidth,
		height - TOOL_HEIGHT - MARGIN,
		toolWidth,
		TOOL_HEIGHT
	] );
	return { slots, exp, tools, bottom, top, bounds: [ 0, 0, width, top ] as UiRect };
}

/*
================
smallHudLayout

The fine-pointer arrangement below the smallest mode: the native HUD,
degraded. The one fact the frame needs is the underbar's scale - the
native 800px bar across the actual window, never above one (a viewport
wider than 800 but shorter than 600 keeps the bar at native size). The
party band's reserve and the chat lift derive from the same scale
(52 * scale is the scaled bar's height), so they live with their owners.
================
*/
export function smallHudLayout( width: number, height: number, coarsePointer: boolean ) {
	if ( coarsePointer || width >= DESKTOP_WIDTH && height >= DESKTOP_HEIGHT ) return null;
	return { scale: Math.min( 1, width / DESKTOP_WIDTH ) };
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
		layout( width: number, height: number, coarsePointer: boolean ) {
			const layout = compactHudLayout( width, height, coarsePointer );
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
binding drags use the published hit rectangles and must remain enabled. The
extended bar is no window position: its handle moves a saved quickslot
preference, so it stays draggable on compact.
================
*/
export function compactWindowDrag( control: UiControl ): boolean {
	if ( control.id === "ext-drag" ) return false;
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
	if ( closedPanel( control.id ) === undefined && !control.id.endsWith( "-close" ) ) return control;
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
