/*
===========================================================================

event-interface.ts - the event window at world entry (CIFEventInterface)

CGInterface_InitializeWorldEntryUi (683B40) creates child 0x7530 on the
session's first world entry (+0x6FC latches it) at 540x232 and calls
CIFEventInterface_OnCreate (547750) with type 0; when the local player is
PC-room eligible (CICPlayer_PcRoomEventEligible_862a80, bit 1 of the
player flags) it also creates child 0x7535 with type 1. OnCreate loads
one layout file per type (546B50): event\event_interface.txt (0),
event\pcroom_event_interface.txt (1), event\winter_event_interface.txt
(2, which no v1.150 caller passes). A missing file or an Open of 0 makes
the load fail and OnCreate destroy the window before it is ever seen;
otherwise the window is built from resinfo\ifeventgate.txt, its texts,
fonts, colors and pictures come from the layout file, and the window is
centered on screen. The build publishes the native layout as
assets/data/event-interface.json (textResources.mjs
buildEventInterfaceData); the two later layouts ride the pinned VSRO
import as assets/data/event-interface-later.json
(import_vsro_event_interface.py), absent when never imported.

===========================================================================
*/

// 683B40 creates both event windows at this size (0x21c x 0xe8), the
// GDR_EVENT_GATE_PIC rect of ifeventgate.txt.
export const EVENT_INTERFACE_WIDTH = 0x21c;
export const EVENT_INTERFACE_HEIGHT = 0xe8;

type EventInterfaceColor = readonly [number, number, number, number];

/*
================
EventInterfaceLayout

One published layout file: whether it opens, the title and body texts
(or the string codes they resolve through), their fonts, colors and
positions, the TextMargin between body lines, the background and close
button pictures, and the button's authored rect when the file moves it
(ButtonPosition; the ifeventgate rect stands otherwise).
================
*/
export interface EventInterfaceLayout {
	readonly open: boolean;
	readonly titleFont: number;
	readonly titleColor: EventInterfaceColor;
	readonly titlePosition: readonly [number, number];
	readonly title: string;
	readonly titleCode: string;
	readonly textFont: number;
	readonly textColor: EventInterfaceColor;
	readonly textPosition: readonly [number, number];
	readonly textMargin: number;
	readonly text: string;
	readonly textCode: string;
	readonly picBg: string | undefined;
	readonly picButton: string | undefined;
	readonly buttonRect: readonly [number, number, number, number] | undefined;
}

/*
================
EventInterfaceData

The three layouts by their native type: gate (0, always published from
the native file), pcroom (1) and winter (2, from the VSRO import; null
when the import never ran, which loads exactly like the native file
being absent).
================
*/
export interface EventInterfaceData {
	readonly gate: EventInterfaceLayout | null;
	readonly pcroom: EventInterfaceLayout | null;
	readonly winter: EventInterfaceLayout | null;
}

/*
================
eventInterfaceColor
================
*/
function eventInterfaceColor( raw: unknown, where: string ): EventInterfaceColor {
	if (
		!Array.isArray( raw ) || raw.length !== 4 ||
		raw.some( c => typeof c !== "number" || !(c >= 0 && c <= 1) )
	) throw Error( "Invalid event interface color: " + where );
	return [ raw[0], raw[1], raw[2], raw[3] ] as EventInterfaceColor;
}

/*
================
eventInterfaceInts
================
*/
function eventInterfaceInts( raw: unknown, count: number, where: string ): readonly number[] {
	if (
		!Array.isArray( raw ) || raw.length !== count ||
		raw.some( n => !Number.isInteger( n ) )
	) throw Error( "Invalid event interface geometry: " + where );
	return raw as readonly number[];
}

/*
================
decodeEventInterfaceLayout

One layout as the builders publish it; throws on anything malformed so
a bad publication fails the HUD load instead of opening a wrong window.
================
*/
export function decodeEventInterfaceLayout( raw: unknown ): EventInterfaceLayout {
	const v = raw as Partial<EventInterfaceLayout> | null;
	if (
		!v || typeof v !== "object" || typeof v.open !== "boolean" ||
		!Number.isInteger( v.titleFont ) || (v.titleFont as number) < 0 ||
		!Number.isInteger( v.textFont ) || (v.textFont as number) < 0 ||
		!Number.isInteger( v.textMargin ) || (v.textMargin as number) < 0 ||
		typeof v.title !== "string" || typeof v.titleCode !== "string" ||
		typeof v.text !== "string" || typeof v.textCode !== "string" ||
		(v.picBg !== undefined && typeof v.picBg !== "string") ||
		(v.picButton !== undefined && typeof v.picButton !== "string")
	) throw Error( "Invalid event interface layout" );
	return {
		open: v.open,
		titleFont: v.titleFont as number,
		titleColor: eventInterfaceColor( v.titleColor, "title" ),
		titlePosition: eventInterfaceInts( v.titlePosition, 2, "title" ) as readonly [number, number],
		title: v.title,
		titleCode: v.titleCode,
		textFont: v.textFont as number,
		textColor: eventInterfaceColor( v.textColor, "text" ),
		textPosition: eventInterfaceInts( v.textPosition, 2, "text" ) as readonly [number, number],
		textMargin: v.textMargin as number,
		text: v.text,
		textCode: v.textCode,
		picBg: v.picBg,
		picButton: v.picButton,
		buttonRect: v.buttonRect === undefined ?
			undefined :
			eventInterfaceInts( v.buttonRect, 4, "button" ) as readonly [number, number, number, number]
	};
}

/*
================
decodeEventInterface

The native publication (its one gate layout) merged with the later one
(PC-room and winter), either of which may be absent whole.
================
*/
export function decodeEventInterface( nativeRaw: unknown, laterRaw: unknown ): EventInterfaceData {
	const native = nativeRaw as { layout?: unknown; } | null,
		later = laterRaw as { layouts?: { pcroom?: unknown; winter?: unknown; }; } | null;
	if ( !native || typeof native !== "object" || !("layout" in native) ) {
		throw Error( "Invalid event interface data" );
	}
	return {
		gate: native.layout == null ? null : decodeEventInterfaceLayout( native.layout ),
		pcroom: later?.layouts?.pcroom == null ? null : decodeEventInterfaceLayout( later.layouts.pcroom ),
		winter: later?.layouts?.winter == null ? null : decodeEventInterfaceLayout( later.layouts.winter )
	};
}

/*
================
eventInterfaceOrigin

CGInterface_CenterWindowOnScreen: the 540x232 window centered, kept on
screen like the mall notice's origin.
================
*/
export function eventInterfaceOrigin( w: number, h: number ): readonly [number, number] {
	return [
		Math.max( 0, Math.floor( (w - EVENT_INTERFACE_WIDTH) / 2 ) ),
		Math.max( 0, Math.floor( (h - EVENT_INTERFACE_HEIGHT) / 2 ) )
	];
}

/*
================
eventInterfaceShown

546B50's verdict, as published: a layout that is absent (the native
load failing on a missing file) or closed (Open of 0) shows nothing.
================
*/
export function eventInterfaceShown( layout: EventInterfaceLayout | null ): boolean {
	return layout !== null && layout.open;
}
