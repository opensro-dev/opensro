/*
===========================================================================

title-tiny-viewport.test.mjs - transient viewport fitting and resize recovery

Exercise the production title, dock and both creation layouts with retail
resources. A viewport smaller than the panel margins must not invert quads.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createTitleUi } = await import( "../../src/engine/runtime/ui/title/title.ts" );
const { initialCreation } = await import( "../../src/engine/foundation/ui/character-create.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );

/*
================
titleFixture

Complete real metadata requests synchronously at the asset boundary.
================
*/
function titleFixture() {
	let serial = 0;
	const jobs = new Map();
	return createTitleUi( {
		available: () => 8,
		request: url => {
			const bytes = readFileSync( CLIENT_PUBLIC_ROOT + new URL( url ).pathname );
			jobs.set( ++serial, bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength ) );
			return serial;
		},
		take: id => {
			const buffer = jobs.get( id );
			jobs.delete( id );
			return buffer ? { kind: "bytes", id, buffer } : null;
		},
		cancel: id => jobs.delete( id )
	}, "https://fixture.invalid" );
}

/*
================
assertRect
================
*/
function assertRect( rect, context ) {
	assert.ok( rect.every( Number.isFinite ), `${context}: finite ${rect}` );
	assert.ok( rect[2] >= 0 && rect[3] >= 0, `${context}: nonnegative ${rect}` );
}

for ( const phase of [ "login", "dock", "create", "customize", "delete-character", "restore-character" ] ) {
	for ( const race of /** @type {const} */ ([ 0, 1 ]) ) {
		test(`${phase} race ${race}: tiny viewport keeps geometry valid and recovers desktop layout`, t => {
			const owner = titleFixture();
			t.after( () => owner.dispose() );
			const modal = phase === "delete-character" || phase === "restore-character";
			/** @type {import("../../src/engine/contracts/frontend").FrontendSnapshot} */
			const state = {
				phase: modal ?
					"dock" :
					/** @type {import("../../src/engine/contracts/frontend").FrontendPhase} */ (phase),
				generation: 1,
				elapsed: 10,
				alpha: 1,
				logoAlpha: 0,
				error: null,
				race,
				dialog: modal ?
					{
						kind: phase,
						character: "Fixture",
						id: 1,
						phase: "open",
						alpha: 1
					} :
					null,
				creation: {
					selection: { ...initialCreation( race, 1 ), name: "Fixture" },
					protectorFloor: 1,
					explain: "figure",
					phase: "editing",
					alpha: 1,
					ready: true,
					yaw: 0,
					zoom: false,
					camera: { eye: [ 0, 0, 1 ], target: [ 0, 0, 0 ], fov: 1, near: 1, far: 100 }
				}
			};
			/*
			================
			render
			================
			*/
			const render = ( width, height ) =>
				owner.render( state, width, height, "", "", [], "", false, false, {
					hover: null,
					pressed: null,
					focus: null,
					now: 1000,
					draft: "",
					offset: 0,
					message: ""
				} );
			render( 1024, 768 );
			const desktop = render( 1024, 768 );
			assert.ok( desktop.ready && desktop.controls.length > 0, "real controls were admitted" );
			for (
				const [width, height] of [ [ 1, 1 ], [ 0, 0 ], [ 8, 12 ], [ 16, 16 ], [ 1, 768 ], [ 1024, 1 ], [
					375,
					812
				] ]
			) {
				const output = render( width, height );
				assert.equal( output.controls.length, desktop.controls.length );
				for ( const control of output.controls ) assertRect( control.rect, control.id );
				if ( modal ) {
					assert.deepEqual( output.controls.map( control => control.id ).sort(), [
						"dock:warning-accept",
						"dock:warning-cancel"
					] );
					assert.ok( output.controls.every( control => !control.disabled ) );
					const dim = output.quads.find( q => q.texture === "" && q.color[3] === 128 / 255 );
					assert.ok( dim, "modal dimming is published" );
					assert.deepEqual( dim.rect, [ 0, 0, width, height ], "modal still covers the viewport" );
				}
				for ( const q of expandTextRuns( output.quads ) ) {
					assertRect( q.rect, `${width}x${height} ${q.texture}` );
					assertRect( q.clip, "clip" );
				}
			}
			assert.deepEqual(
				render( 1024, 768 ),
				desktop,
				"transient fitting cannot mutate reusable layout resources"
			);
		});
	}
}
