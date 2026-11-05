/*
===========================================================================

scene-golden.mjs - the canonical form of one published UI scene

Wave 0 of the ui.ts split (#2514): a move is safe only if the scene it
publishes is byte-identical before and after. This module is the one
place that decides what "identical" means: a published UiScene plus its
semantics, canonicalised (object keys sorted, floats rounded to three
decimals half away from zero, undefined dropped) and written as compact
JSON, one quad and one control per line, so a golden diff reads like
code and a failure can name the first differing quad.

Goldens regenerate only from the update mode (SRO_SCENE_GOLDENS_UPDATE=1),
never as a side effect of the change they vouch for; a regeneration lands
in its own commit with no production change beside it.

===========================================================================
*/
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The one switch that may rewrite a golden. Anything else is a compare.
export const SCENE_GOLDENS_UPDATE = process.env.SRO_SCENE_GOLDENS_UPDATE === "1";

/*
================
sceneGoldenPath

The golden file for one scene, beside the suite that owns it.
================
*/
export function sceneGoldenPath( name ) {
	return fileURLToPath( new URL( `../runtime/goldens/${name}.json`, import.meta.url ) );
}

/*
================
roundSceneNumber

The float rule, pinned: three decimals, half away from zero, shortest
form, and no negative zero. 0.001 is below every authored rect step in
the if*.txt layouts, so no native distinction is lost, and the output is
identical on every machine.
================
*/
export function roundSceneNumber( value ) {
	const rounded = Math.sign( value ) * Math.round( Math.abs( value ) * 1000 ) / 1000;
	return rounded === 0 ? 0 : rounded;
}

/*
================
canonicalSceneValue

One value in canonical form: arrays keep their order (draw order is
meaning), object keys are sorted, numbers take the pinned float rule,
undefined is dropped, and a glyph quad's layout internals never reach
the golden (the published scene already expanded them to glyphs).
================
*/
export function canonicalSceneValue( value ) {
	if ( typeof value === "number" ) return Number.isFinite( value ) ? roundSceneNumber( value ) : String( value );
	if ( Array.isArray( value ) ) return value.map( canonicalSceneValue );
	if ( value !== null && typeof value === "object" ) {
		const out = {};
		for ( const key of Object.keys( value ).sort() ) {
			if ( value[key] === undefined ) continue;
			out[key] = canonicalSceneValue( value[key] );
		}
		return out;
	}
	return value;
}

/*
================
canonicalQuadLine

One published quad as one compact JSON line. The text-run and layout
objects a quad may still carry are authoring detail; the draw the
renderer performs is rect, uv, colour, texture and clip.
================
*/
function canonicalQuadLine( quad ) {
	const { textLayout, run, ...draw } = canonicalSceneValue( quad );
	return JSON.stringify( draw );
}

/*
================
canonicalSceneText

The golden text for one settled scene: the published quads, the control
list, and the semantics beside them, one element per line.
================
*/
export function canonicalSceneText( scene, semantics ) {
	const { controls = [], ...rest } = semantics ?? {};
	const golden = {
		width: scene?.width ?? 0,
		height: scene?.height ?? 0,
		...(scene?.damageText ? { damageText: true } : {}),
		semantics: canonicalSceneValue( rest ),
		controls: controls.map( control => JSON.stringify( canonicalSceneValue( control ) ) ),
		quads: (scene?.quads ?? []).map( canonicalQuadLine )
	};
	return JSON.stringify( golden, null, 1 ) + "\n";
}

/*
================
canonicalStreamText

The golden text for an input tape: one canonical scene per beat, so a
gesture's whole published stream must match, not only where it settles.
================
*/
export function canonicalStreamText( beats ) {
	const golden = {
		beats: beats.map( beat => ({
			beat: beat.label,
			text: canonicalSceneText( beat.scene, beat.semantics )
		}) )
	};
	return JSON.stringify( golden, null, 1 ) + "\n";
}

/*
================
firstGoldenDifference

Where two golden texts first part: the line, and both sides of it. A
quad line names its index in the draw order, which is the quad a
reviewer goes to look at.
================
*/
export function firstGoldenDifference( expected, actual ) {
	const expectedLines = expected.split( "\n" ), actualLines = actual.split( "\n" );
	const count = Math.max( expectedLines.length, actualLines.length );
	for ( let i = 0; i < count; i++ ) {
		if ( expectedLines[i] !== actualLines[i] ) {
			return { line: i + 1, expected: expectedLines[i] ?? "<missing>", actual: actualLines[i] ?? "<missing>" };
		}
	}
	return null;
}

/*
================
checkSceneGolden

Compare one scene's canonical text with its golden, or record it in
update mode. Returns a failure sentence, or null when the scene stands.
================
*/
export function checkSceneGolden( name, text ) {
	const path = sceneGoldenPath( name );
	if ( SCENE_GOLDENS_UPDATE ) {
		writeFileSync( path, text );
		return null;
	}
	if ( !existsSync( path ) ) return `scene ${name}: no golden recorded (run with SRO_SCENE_GOLDENS_UPDATE=1)`;
	const expected = readFileSync( path, "utf-8" );
	if ( expected === text ) return null;
	const diff = firstGoldenDifference( expected, text );
	return `scene ${name} differs at line ${diff?.line}: expected ${diff?.expected} but published ${diff?.actual} ` +
		`(regenerate deliberately with SRO_SCENE_GOLDENS_UPDATE=1 and review the diff)`;
}
