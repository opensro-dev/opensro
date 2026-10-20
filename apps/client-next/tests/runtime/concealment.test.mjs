/*
===========================================================================

concealment.test.mjs - tests for concealment.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { concealmentState, concealmentAlpha, hasConcealmentEffects, seenAlpha } = await import(
	sourceFileUrl( "src/engine/foundation/gameplay/concealment.ts" ).href
);
const { applyCharacterVisibility } = await import(
	sourceFileUrl( "src/engine/runtime/characters/presentation-finalize.ts" ).href
);

// Shipped levels: rogue stealth hide 1/3, wizard invisibility hide 2/3,
// rogue detect dtt 5/3 range 100, wizard detect dttp 6/3 range 100.
const rows = new Map( [
	[ 7929, { id: 7929, hide: { mask: 1, level: 3 } } ],
	[ 8703, { id: 8703, hide: { mask: 2, level: 3 } } ],
	[ 7939, { id: 7939, sight: { mask: 5, level: 3 }, detectRange: 100 } ],
	[ 7940, { id: 7940, sight: { mask: 5, level: 2 }, detectRange: 100 } ],
	[ 7122, { id: 7122, sight: { mask: 7, level: 3 }, detectRange: 0 } ],
	[ 8929, { id: 8929, detect: { mask: 6, level: 3 }, detectRange: 100 } ]
] );
const skill = id => rows.get( id );
const on = ( gid, id ) => ({ gid, skill: id, token: id, phase: 2 });
const HIDDEN = 1, VIEWER = 2;

test("85D890 hides a stealthed character until a sight or reveal covers its level in range", () => {
	const stealth = [ on( HIDDEN, 7929 ) ];
	assert.equal( concealmentState( 0, stealth, [], 50, skill ), "open", "body 0 is never concealed" );
	assert.equal( concealmentState( 6, [], [], 50, skill ), "open", "body 6 without a hide buff has no levels" );
	assert.equal( concealmentState( 6, stealth, [], 50, skill ), "hidden" );
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7939 ) ], 50, skill ),
		"seen",
		"dtt 5/3 covers stealth 1/3 at 50"
	);
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7939 ) ], 100, skill ),
		"seen",
		"the range bound is inclusive"
	);
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7939 ) ], 150, skill ),
		"hidden",
		"beyond the efr radius"
	);
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7940 ) ], 50, skill ),
		"hidden",
		"level 2 does not reach 3"
	);
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7122 ) ], 1000, skill ),
		"seen",
		"range 0 reaches any distance"
	);
	assert.equal(
		concealmentState( 6, stealth, [ on( VIEWER, 7939 ) ], 1000, skill ),
		"hidden",
		"an unmeasured distance (1000) stays hidden"
	);
});

test("the mask bits must meet, and a reveal on the hidden character counts too", () => {
	const invisible = [ on( HIDDEN, 8703 ) ];
	assert.equal(
		concealmentState( 7, invisible, [ on( VIEWER, 7939 ) ], 50, skill ),
		"hidden",
		"dtt 5 (1|4) misses invisibility bit 2"
	);
	assert.equal(
		concealmentState( 7, [ ...invisible, on( HIDDEN, 8929 ) ], [], 50, skill ),
		"seen",
		"dttp 6 laid on it covers bit 2"
	);
	assert.equal(
		concealmentState( 7, [ ...invisible, on( HIDDEN, 8929 ) ], [], 150, skill ),
		"hidden",
		"the reveal has a range as well"
	);
});

test("alpha follows the state, and party members stay translucent", () => {
	assert.equal( concealmentAlpha( "open", false ), undefined );
	assert.equal( concealmentAlpha( "seen", false ), seenAlpha() );
	assert.equal( concealmentAlpha( "hidden", false ), 0 );
	assert.equal( concealmentAlpha( "hidden", true ), seenAlpha() );
	assert.equal( seenAlpha(), 0x50 / 255 );
});

test("866B90 and 85CBD0 keep the local player at seenAlpha (0x50/255) in body 6 or 7 with an active hide effect", () => {
	assert.equal( hasConcealmentEffects( 0, [ on( HIDDEN, 8703 ) ], skill ), false );
	assert.equal( hasConcealmentEffects( 7, [], skill ), false );
	assert.equal( hasConcealmentEffects( 6, [ on( HIDDEN, 7929 ) ], skill ), true );
	assert.equal( hasConcealmentEffects( 7, [ on( HIDDEN, 8703 ) ], skill ), true );

	const runVisibility = (
		body,
		attachedEffects,
		cameraPitch = 0,
		output = { cameraTarget: null, cameraFade: null }
	) => {
		const local = { gid: HIDDEN, name: "Self", appearanceState: [ 0, 0, body, 0 ] };
		const next = new Map( [ [ HIDDEN, { gid: HIDDEN, opacity: /** @type {number | undefined} */ (undefined) } ] ] );
		applyCharacterVisibility(
			{
				entities: [ local ],
				next,
				local,
				gameplay: { attachedEffects, skillCatalog: [] },
				seconds: 1,
				cameraPitch,
				concealmentSkills: () => skill,
				entityLod: { distance: () => 0 }
			},
			output
		);
		return { opacity: next.get( HIDDEN )?.opacity, output };
	};

	// Body 7 (Wizard Invisibility) and body 6 (Rogue Stealth) with their hide buff keep 0x50/255 when camera fade is steady.
	assert.equal( runVisibility( 7, [ on( HIDDEN, 8703 ) ] ).opacity, seenAlpha() );
	assert.equal( runVisibility( 6, [ on( HIDDEN, 7929 ) ] ).opacity, seenAlpha() );
	// Body 7 without a hide effect falls through to 0xFF (1.0), matching 85CBD0 returning 0.
	assert.equal( runVisibility( 7, [] ).opacity, 1 );
	// Body 0 with a leftover hide effect stays at 1.0.
	assert.equal( runVisibility( 0, [ on( HIDDEN, 8703 ) ] ).opacity, 1 );
	// Body 4 (invulnerable/spawn) still keeps 0x50/255 without any hide effect.
	assert.equal( runVisibility( 4, [] ).opacity, seenAlpha() );
	// During an active camera-pitch fade transition (cameraPitch < -0.9), camera fade overrides until the transition finishes.
	const steady = runVisibility( 7, [ on( HIDDEN, 8703 ) ], 0 );
	const transitioning = runVisibility( 7, [ on( HIDDEN, 8703 ) ], -0.95, steady.output );
	assert.notEqual( transitioning.opacity, seenAlpha() );
});
