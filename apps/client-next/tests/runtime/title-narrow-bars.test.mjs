/*
===========================================================================

title-narrow-bars.test.mjs - the phone title bars and version line

Port-only (#1402): narrower than 4:3 the title's top and bottom bars
keep one uniform scale and show the centred window of their 1600-wide
art (never the squeezed native stretch), drawn through the area-sampled
path, and the version line moves up beside the project credit. 4:3 and
wider keep the native stretch exactly. Loads the production title UI
with retail resources, like title-tiny-viewport.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createTitleUi } = await import( "../../src/engine/runtime/ui/title/title.ts" );
const { initialCreation } = await import( "../../src/engine/foundation/ui/character-create.ts" );

// The bars' authored width and the version line's bottom distances
// (title.ts SCREEN_BAR_WIDTH / NARROW_VERSION_BOTTOM / the native 25).
const BAR_WIDTH = 1600;

/*
================
titleFixture

Complete real metadata requests synchronously at the asset boundary,
then render the login screen at the given size.
================
*/
function titleFixture() {
	let serial = 0;
	const jobs = new Map();
	const owner = createTitleUi( {
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
	/** @type {import("../../src/engine/contracts/frontend").FrontendSnapshot} */
	const state = {
		phase: "login",
		generation: 1,
		elapsed: 10,
		alpha: 1,
		logoAlpha: 0,
		error: null,
		race: 0,
		dialog: null,
		creation: {
			selection: { ...initialCreation( 0, 1 ), name: "Fixture" },
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
	const render = ( /** @type {number} */ w, /** @type {number} */ h ) =>
		owner.render( state, w, h, "", "", [], "", false, false, {
			hover: null,
			pressed: null,
			focus: null,
			now: 1000,
			draft: "",
			offset: 0,
			message: ""
		} );
	render( 1024, 768 );
	render( 1024, 768 );
	return { owner, render };
}

/*
================
bars
================
*/
/**
 * @param {import("../../src/engine/contracts/ui").UiQuad[]} quads
 */
function bars( quads ) {
	return quads.filter( q =>
		q.texture.endsWith( "blackbar_up_europe.png" ) || q.texture.endsWith( "blackbar_down_copyright_europe.png" )
	);
}

/*
================
versionBottom

The version run's distance above the bottom edge (its glyph run sits
6 px below the text box). Native: 25 up; narrow: 45 up.
================
*/
/**
 * @param {import("../../src/engine/contracts/ui").UiQuad[]} quads
 * @param {number} h
 */
function versionBottom( quads, h ) {
	const run = quads.find( q => q.color[0] === 1 && q.color[1] === 128 / 255 );
	assert.ok( run, "the version line is drawn" );
	return h - run.rect[1];
}

test("a phone's bars show their centred window unsqueezed and the version rides up", t => {
	const { owner, render } = titleFixture();
	t.after( () => owner.dispose() );
	const w = 375, h = 812, scale = h / 1200;
	const output = render( w, h );
	assert.ok( output.ready );
	const found = bars( output.quads );
	assert.equal( found.length, 2, "both bars drawn" );
	// The centred window of the 1600-wide art that the uniform scale
	// shows, through the area-sampled path.
	const texels = w / scale,
		uv = [ (BAR_WIDTH - texels) / 2 / BAR_WIDTH, 0, texels / BAR_WIDTH, 1 ];
	for ( const bar of found ) {
		assert.deepEqual( bar.uv, uv );
		assert.equal( bar.sampling, "nearest" );
	}
	assert.deepEqual( found[0].rect, [ 0, 0, w, Math.round( 172 * scale ) ] );
	assert.deepEqual( found[1].rect, [ 0, Math.round( 1030 * scale ), w, Math.round( 172 * scale ) ] );
	// The version sits on the project credit's row (45 above the bottom,
	// the glyph run 6 below its box), clear of the copyright line.
	assert.equal( versionBottom( output.quads, h ), 45 - 6 );
});

test("4:3 and wider keep the native stretch and the native version row", t => {
	const { owner, render } = titleFixture();
	t.after( () => owner.dispose() );
	for ( const [w, h] of [ [ 1024, 768 ], [ 1920, 1080 ] ] ) {
		const output = render( w, h );
		const found = bars( output.quads );
		assert.equal( found.length, 2, "both bars drawn" );
		for ( const bar of found ) {
			assert.deepEqual( bar.uv, [ 0, 0, 1, 1 ], "the whole art stretched, as native" );
			assert.equal( bar.sampling, undefined, "no area-sampled override" );
		}
		assert.equal( versionBottom( output.quads, h ), 25 - 6 );
	}
});
