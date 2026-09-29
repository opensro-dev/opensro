/*
===========================================================================

return-scroll.ts - native delay-row geometry shared by travel and gathering

CIFDelayInfo owns a 192 by 36 row. Kinds select their authored gauge texture;
the caller owns lifetime and cancellation. This module only projects quads.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

const ROOT = "/assets/images/Media_extracted/interface/ifcommon/";
const ROW_WIDTH = 192;
const ROW_HEIGHT = 36;
const ROW_SPACING = 38;
const BOTTOM_OFFSET = 89;
const GAUGE_WIDTH = 184;
const GAUGE_HEIGHT = 8;
const CANCEL_SIZE = 20;

/*
================
DelayBarOptions

The UI supplies its worker-clock sample and explicit row interaction state.
================
*/
export interface DelayBarOptions {
	readonly now: number;
	readonly pressed?: boolean;
	readonly focused?: boolean;
	readonly collection?: boolean;
	readonly row?: number;
}

/*
================
returnScrollBar

6B13B0 retains the special 800-wide position. 6B18A0 leaves zero-duration
gauges untouched; 6B1B30 selects the collection texture for quest gathering.
================
*/
export function returnScrollBar(
	cast: { readonly startedAtMs: number; readonly durationMs: number; },
	width: number,
	height: number,
	options: DelayBarOptions
) {
	const x = width === 800 ? 405 : Math.trunc( (width - ROW_WIDTH) / 2 );
	const y = height - BOTTOM_OFFSET - (options.row ?? 0) * ROW_SPACING;
	const frame: UiRect = [ x, y, ROW_WIDTH, ROW_HEIGHT ];
	const cancel: UiRect = [ x + 171, y + 4, CANCEL_SIZE, CANCEL_SIZE ];
	const name: UiRect = [ x, y + 7, 167, 12 ];
	const ratio = cast.durationMs === 0 ?
		0 :
		Math.max( 0, Math.min( 1, (options.now - cast.startedAtMs) / cast.durationMs ) );
	const quads: UiQuad[] = [
		{
			rect: frame,
			clip: [ 0, 0, width, height ],
			texture: ROOT + "com_casting_window.png",
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		},
		{
			rect: cancel,
			clip: frame,
			texture: ROOT + "com_casting_cancel" + (options.pressed ? "_press" : options.focused ? "_focus" : "") +
				".png",
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		}
	];
	if ( ratio > 0 ) {
		quads.push( {
			rect: [ x + 6, y + 27, GAUGE_WIDTH * ratio, GAUGE_HEIGHT ],
			clip: frame,
			texture: ROOT + (options.collection ? "com_casting_gauge_collection.png" : "com_casting_gauge_return.png"),
			uv: [ 0, 0, ratio, 1 ],
			color: [ 1, 1, 1, 1 ]
		} );
	}
	return { frame, cancel, name, quads };
}
