/*
===========================================================================

layout.ts - fitting a control group into the screen

Authored panels and HUD groups share one fitting transform. On a smaller
window the artwork, text, hit targets and optional blocking rectangles move
and shrink together; screen-wide modal coverage remains on the screen.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect, UiTextLayout } from "@/engine/contracts/ui";
import { scaleTextRun } from "@/engine/foundation/rendering/text-run";

/*
================
FitUiGroupOptions

Block indices use the same append-only group boundary as quads and controls.
Disable dragging when the caller cannot invert fitted pointer coordinates.
================
*/
export interface FitUiGroupOptions {
	// Visible frame and controls; clipped content outside it must not choose the scale.
	readonly sourceBounds?: UiRect;
	readonly blocks?: UiRect[];
	readonly firstBlock?: number;
	readonly disableDrag?: boolean | (( control: UiControl ) => boolean);
}

/*
================
fitUiGroup

Fit an entire control group with one similarity transform. Text, clips and
semantic hit targets must undergo the same transform as their artwork; a
text run's glyphs scale with its rect (scaleTextRun). Optional blocks start
at firstBlock (default zero). sourceBounds overrides the measured artwork
extent without cropping its content. disableDrag applies even to an already-
fitting group; a predicate can preserve gameplay carries and binding drags.
================
*/
export function fitUiGroup(
	quads: UiQuad[],
	controls: UiControl[],
	firstQuad: number,
	firstControl: number,
	bounds: UiRect,
	screen: UiRect,
	options: FitUiGroupOptions = {}
): void {
	if ( options.disableDrag ) {
		for ( let i = firstControl; i < controls.length; i++ ) {
			const control = controls[i]!;
			if ( control.draggable && (options.disableDrag === true || options.disableDrag( control )) ) {
				controls[i] = { ...control, draggable: false };
			}
		}
	}
	const group = quads.slice( firstQuad );
	if ( !group.length && !options.sourceBounds ) return;
	const source = options.sourceBounds,
		left = source ? source[0] : Math.min( ...group.map( q => q.rect[0] ) ),
		top = source ? source[1] : Math.min( ...group.map( q => q.rect[1] ) ),
		right = source ? source[0] + source[2] : Math.max( ...group.map( q => q.rect[0] + q.rect[2] ) ),
		bottom = source ? source[1] + source[3] : Math.max( ...group.map( q => q.rect[1] + q.rect[3] ) );
	// A viewport smaller than the caller's padding (a window resized to 1x1)
	// leaves negative space: at least one pixel keeps the scale positive,
	// where a negative one flips every rect and the renderer refuses it.
	const width = Math.max( 1, bounds[2] ), height = Math.max( 1, bounds[3] );
	const scale = Math.min( 1, width / Math.max( 1, right - left ), height / Math.max( 1, bottom - top ) );
	const x = Math.max(
		bounds[0],
		Math.min( (left + right) / 2 - (right - left) * scale / 2, bounds[0] + width - (right - left) * scale )
	);
	const y = Math.max(
		bounds[1],
		Math.min( (top + bottom) / 2 - (bottom - top) * scale / 2, bounds[1] + height - (bottom - top) * scale )
	);
	if ( scale === 1 && x === left && y === top ) return;
	/*
	================
	transform
	================
	*/
	const transform = ( r: UiRect ): UiRect => [
		x + (r[0] - left) * scale,
		y + (r[1] - top) * scale,
		r[2] * scale,
		r[3] * scale
	];
	const layouts = new Map<UiTextLayout, UiTextLayout>();
	/*
	================
	transformQuad

	Overlap resolution runs after fitting. Its replacement runs and shared
	boxes must stay in the same space as the visible glyphs.
	================
	*/
	function transformQuad( q: UiQuad ): UiQuad {
		let textLayout = q.textLayout && layouts.get( q.textLayout );
		if ( q.textLayout && !textLayout ) {
			textLayout = {
				...q.textLayout,
				box: transform( q.textLayout.box ),
				...(q.textLayout.fitted ? { fitted: transformQuad( q.textLayout.fitted ) } : {})
			};
			layouts.set( q.textLayout, textLayout );
		}
		return {
			...q,
			rect: transform( q.rect ),
			// The text owner freezes a copy of its clip; viewport identity is geometric.
			clip: q.clip.every( ( value, axis ) => value === screen[axis] ) ? screen : transform( q.clip ),
			...(q.run ? { run: scaleTextRun( q.run, scale ) } : {}),
			// Fractional shrinking drops bitmap strokes with nearest sampling.
			// Keep native-size glyphs and artwork on their existing sampling path.
			...(q.run && q.sampling === "nearest" && scale < 1 ? { sampling: "linear" as const } : {}),
			...(q.mask ? { mask: { ...q.mask, rect: transform( q.mask.rect ) } } : {}),
			...(textLayout ? { textLayout } : {})
		};
	}
	for ( let i = firstQuad; i < quads.length; i++ ) {
		quads[i] = transformQuad( quads[i]! );
	}
	for ( let i = firstControl; i < controls.length; i++ ) {
		const c = controls[i]!;
		controls[i] = { ...c, rect: transform( c.rect ) };
	}
	const blocks = options.blocks;
	if ( blocks ) {
		for ( let i = options.firstBlock ?? 0; i < blocks.length; i++ ) {
			const block = blocks[i]!;
			// Modal coverage belongs to the screen, not the fitted window.
			if ( block.every( ( value, axis ) => value === screen[axis] ) ) continue;
			blocks[i] = transform( block );
		}
	}
}
