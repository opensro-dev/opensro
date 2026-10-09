/*
===========================================================================

mall-notify.ts - the Item Mall notice after world entry (CIFMallNotifyWnd)

CGInterface_InitializeWorldEntryUi (683B40) creates child 0x7537 on the
first world entry of a session (+0x6FC latches it). CIFMallNotifyWnd_OnCreate
(6CCC20) sizes it 0xE8 x 0xBC, fills its text board from event\mall_notify.txt
and moves it to (screen width - 0xEB, screen height - 0x118); a notice whose
Open field is 0 closes itself instead. The build publishes the file as
assets/data/mall-notify.json (textResources.mjs buildMallNotifyData).

===========================================================================
*/

// 6CCC20's size and its offsets from the screen's right and bottom edges.
export const MALL_NOTIFY_WIDTH = 0xe8;
export const MALL_NOTIFY_HEIGHT = 0xbc;
const MALL_NOTIFY_RIGHT = 0xeb;
const MALL_NOTIFY_BOTTOM = 0x118;

/*
================
MallNotify

The published notice: whether it opens, its text and the extra pixels
between its lines (TextMargin).
================
*/
export interface MallNotify {
	readonly open: boolean;
	readonly text: string;
	readonly lineSpacing: number;
}

/*
================
decodeMallNotify
================
*/
export function decodeMallNotify( raw: unknown ): MallNotify {
	const v = raw as { open?: unknown; textString?: unknown; textLineSpacing?: unknown; } | null;
	if (
		!v || typeof v.open !== "boolean" || typeof v.textString !== "string" ||
		!Number.isInteger( v.textLineSpacing ) || (v.textLineSpacing as number) < 0
	) throw Error( "Invalid mall notice" );
	return { open: v.open, text: v.textString, lineSpacing: v.textLineSpacing as number };
}

/*
================
mallNotifyOrigin

6CCC20's position for a w x h screen, kept on screen.
================
*/
export function mallNotifyOrigin( w: number, h: number ): readonly [number, number] {
	return [ Math.max( 0, w - MALL_NOTIFY_RIGHT ), Math.max( 0, h - MALL_NOTIFY_BOTTOM ) ];
}
