/*
===========================================================================

layout.ts - fitting a control group into the screen

Title and character-creation panels are authored for one resolution. On a
smaller window a whole group (artwork, text and hit targets) is shrunk and
moved by one similarity transform so it stays on screen.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import { scaleTextRun } from "@/engine/foundation/rendering/text-run";

/*
================
fitUiGroup

Fit an entire control group with one similarity transform. Text, clips and
semantic hit targets must undergo the same transform as their artwork; a
text run's glyphs scale with its rect (scaleTextRun).
================
*/
export function fitUiGroup(
	quads: UiQuad[],
	controls: UiControl[],
	firstQuad: number,
	firstControl: number,
	bounds: UiRect,
	screen: UiRect
): void {
	const group = quads.slice( firstQuad );
	if ( !group.length ) return;
	const left = Math.min( ...group.map( q => q.rect[0] ) ), top = Math.min( ...group.map( q => q.rect[1] ) );
	const right = Math.max( ...group.map( q => q.rect[0] + q.rect[2] ) ),
		bottom = Math.max( ...group.map( q => q.rect[1] + q.rect[3] ) );
	const scale = Math.min( 1, bounds[2] / Math.max( 1, right - left ), bounds[3] / Math.max( 1, bottom - top ) );
	const x = Math.max(
		bounds[0],
		Math.min( (left + right) / 2 - (right - left) * scale / 2, bounds[0] + bounds[2] - (right - left) * scale )
	);
	const y = Math.max(
		bounds[1],
		Math.min( (top + bottom) / 2 - (bottom - top) * scale / 2, bounds[1] + bounds[3] - (bottom - top) * scale )
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
	for ( let i = firstQuad; i < quads.length; i++ ) {
		const q = quads[i]!;
		quads[i] = {
			...q,
			rect: transform( q.rect ),
			clip: q.clip === screen ? screen : transform( q.clip ),
			...(q.run ? { run: scaleTextRun( q.run, scale ) } : {})
		};
	}
	for ( let i = firstControl; i < controls.length; i++ ) {
		const c = controls[i]!;
		controls[i] = { ...c, rect: transform( c.rect ) };
	}
}
