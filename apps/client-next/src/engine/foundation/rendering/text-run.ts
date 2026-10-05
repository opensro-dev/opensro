/*
===========================================================================

text-run.ts - laid-out strings carried as one UI quad

Glyph layout produced one quad per glyph, and every interface frame then
copied, compared and resolved each of them: moving text (map labels,
nameplates, damage numbers) cost several objects per glyph per frame. A
text run carries the whole string as one quad holding a frozen glyph
layout relative to its rect origin. Runs are expanded into glyph quads at
exactly one point, and the expansion reproduces the old quads value for
value: glyph positions are whole pixels (a rounded origin plus integer font
metrics), so origin plus relative offset is exact.

===========================================================================
*/

import type { UiQuad, UiRect, UiTextGlyph, UiTextRun } from "@/engine/contracts/ui";

/*
================
textRunQuad

The run quad for glyph quads that share one color, texture and clip (one
layoutGlyphs call). Null for a string with no drawable glyph.
================
*/
export function textRunQuad( quads: readonly UiQuad[] ): UiQuad | null {
	const first = quads[0];
	if ( !first ) return null;
	let left = Infinity, top = Infinity, right = -Infinity, bottom = -Infinity;
	for ( const quad of quads ) {
		left = Math.min( left, quad.rect[0] );
		top = Math.min( top, quad.rect[1] );
		right = Math.max( right, quad.rect[0] + quad.rect[2] );
		bottom = Math.max( bottom, quad.rect[1] + quad.rect[3] );
	}
	const glyphs: UiTextGlyph[] = quads.map( quad =>
		Object.freeze( {
			x: quad.rect[0] - left,
			y: quad.rect[1] - top,
			width: quad.rect[2],
			height: quad.rect[3],
			uv: Object.freeze( [ quad.uv[0], quad.uv[1], quad.uv[2], quad.uv[3] ] ) as UiRect
		} )
	);
	const run: UiTextRun = Object.freeze( { glyphs: Object.freeze( glyphs ) } );
	return Object.freeze( {
		...first,
		rect: [ left, top, right - left, bottom - top ] as UiRect,
		uv: [ 0, 0, 0, 0 ] as UiRect,
		run
	} );
}

/*
================
textRunGlyphRect

The absolute rectangle of glyph i of a run quad.
================
*/
export function textRunGlyphRect( quad: UiQuad, glyph: UiTextGlyph ): UiRect {
	return [ quad.rect[0] + glyph.x, quad.rect[1] + glyph.y, glyph.width, glyph.height ];
}

/*
================
expandTextRuns

Replace every run quad with the glyph quads it stands for, in place in the
paint order. Each glyph keeps the run's other fields (color, clip, anchors).
================
*/
export function expandTextRuns( quads: readonly UiQuad[] ): UiQuad[] {
	const result: UiQuad[] = [];
	for ( const quad of quads ) {
		if ( !quad.run ) {
			result.push( quad );
			continue;
		}
		const { run, ...paint } = quad;
		for ( const glyph of run.glyphs ) {
			result.push( { ...paint, rect: textRunGlyphRect( quad, glyph ), uv: glyph.uv } );
		}
	}
	return result;
}

/*
================
scaleTextRun

The run of a quad whose rect is scaled by scale: glyph offsets and sizes
scale with it, so the run still paints what its scaled glyph quads did.
Moving a rect needs no new run. With whole-pixel glyphs and a scale such
as 1.5 or 0.5 the result is exact; for other factors it can differ from
scaling each glyph quad in the last bit only.
================
*/
export function scaleTextRun( run: UiTextRun, scale: number ): UiTextRun {
	if ( scale === 1 ) return run;
	return Object.freeze( {
		glyphs: Object.freeze( run.glyphs.map( glyph =>
			Object.freeze( {
				x: glyph.x * scale,
				y: glyph.y * scale,
				width: glyph.width * scale,
				height: glyph.height * scale,
				uv: glyph.uv
			} )
		) )
	} );
}

// Records the GPU UI buffer may hold: one per glyph or plain quad.
export const UI_RECORD_LIMIT = 8192;

/*
================
uiRecordCount

GPU records a scene needs: a run contributes one per glyph.
================
*/
export function uiRecordCount( quads: readonly UiQuad[] ): number {
	let records = 0;
	for ( const quad of quads ) records += quad.run ? quad.run.glyphs.length : 1;
	return records;
}

/*
================
sameTextRun

Runs compare by value: the same layout object is the fast path, otherwise
glyph by glyph.
================
*/
export function sameTextRun( a: UiTextRun | undefined, b: UiTextRun | undefined ): boolean {
	if ( a === b ) return true;
	if ( !a || !b || a.glyphs.length !== b.glyphs.length ) return false;
	for ( let i = 0; i < a.glyphs.length; i++ ) {
		const x = a.glyphs[i]!, y = b.glyphs[i]!;
		if (
			!Object.is( x.x, y.x ) || !Object.is( x.y, y.y ) || !Object.is( x.width, y.width ) ||
			!Object.is( x.height, y.height ) || !Object.is( x.uv[0], y.uv[0] ) || !Object.is( x.uv[1], y.uv[1] ) ||
			!Object.is( x.uv[2], y.uv[2] ) || !Object.is( x.uv[3], y.uv[3] )
		) return false;
	}
	return true;
}

/*
================
validTextRun

Whether a run is admissible as a retained command: deeply frozen (no caller
can change it after admission) with finite, non-negative glyph extents.
Callers cache the verdict per run object.
================
*/
export function validTextRun( run: UiTextRun ): boolean {
	if ( !Object.isFrozen( run ) || !Array.isArray( run.glyphs ) || !Object.isFrozen( run.glyphs ) ) return false;
	if ( run.glyphs.length > UI_RECORD_LIMIT ) return false;
	for ( const glyph of run.glyphs ) {
		if ( !Object.isFrozen( glyph ) || !Object.isFrozen( glyph.uv ) || glyph.uv.length !== 4 ) return false;
		if (
			!Number.isFinite( glyph.x ) || !Number.isFinite( glyph.y ) || !Number.isFinite( glyph.width ) ||
			!Number.isFinite( glyph.height ) || glyph.width < 0 || glyph.height < 0 ||
			!Number.isFinite( glyph.uv[0] ) || !Number.isFinite( glyph.uv[1] ) || !Number.isFinite( glyph.uv[2] ) ||
			!Number.isFinite( glyph.uv[3] )
		) return false;
	}
	return true;
}
