/*
===========================================================================

caravan-star-nameplate.test.mjs - the 5-star mark over a caravan

Port-only (#2958): the server's roster of 5-star transport gids puts five
stars over the transport's nameplate, in the bag window's art and tint.
No roster (the flag off) draws exactly the native stack.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const { overheadLayout, withoutBoardPass } = await import(
	"../../src/engine/foundation/ui/overhead-layout.ts"
);
const { createCaravanStars, OP_CARAVAN_STARS } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/caravan-stars/caravan-stars.ts"
);
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { tradeStarTint } = await import( "../../src/engine/foundation/gameplay/cos-trade-info.ts" );
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const options = defaultGameOptions();

/*
================
layoutFor

The layout reads the whole GameplayState type; the fixtures below carry
only the fields the overhead rows consult.
================
*/
function layoutFor( entity, game ) {
	return overheadLayout( entity, local, /** @type {any} */ (game), options );
}

const local = { gid: 1, refObjId: 1907, kind: "local-player", name: "Me", heading: 0, regionId: 257, x: 0, y: 0, z: 0 };
const transport = {
	...local,
	gid: 9,
	refObjId: 100,
	kind: "cos",
	name: "Camel",
	tidWord: (5 << 11) | 0x1c6,
	ownerGid: 2,
	ownerName: "Trader"
};

test("a rostered transport carries the five-star mark above its stack", () => {
	const game = { localGid: 1, social: emptySocial( "Me" ), vitals: [], caravanStars: [ transport.gid ] };
	const layout = layoutFor( transport, game );
	assert.ok( layout.caravanMark, "the mark is set" );
	assert.match( layout.caravanMark.path, /com_star\.png$/ );
	assert.deepEqual( layout.caravanMark.tint, tradeStarTint( 5 ) );
	// The owner row rides at -20; the stars sit one row above it, and
	// speech rises above the stars.
	assert.equal( layout.caravanMark.y, -40 );
	assert.equal( layout.speechY, layout.caravanMark.y );
});

test("no roster, no mark: the native stack is untouched", () => {
	const bare = { localGid: 1, social: emptySocial( "Me" ), vitals: [] };
	for ( const game of [ bare, { ...bare, caravanStars: [] }, { ...bare, caravanStars: [ 77 ] } ] ) {
		const layout = layoutFor( transport, game );
		assert.equal( layout.caravanMark, null );
		assert.equal( layout.speechY, -20 );
	}
	// Only a COS carries the mark, whatever the roster names.
	const monster = { ...transport, kind: "monster" };
	assert.equal(
		layoutFor( monster, { ...bare, caravanStars: [ transport.gid ] } ).caravanMark,
		null
	);
	// The /id-off pass hides it with the rest of the kind-4 rows.
	const layout = layoutFor( transport, { ...bare, caravanStars: [ transport.gid ] } );
	assert.equal( withoutBoardPass( layout ).caravanMark, null );
});

test("the roster frame decodes by gid and replaces wholesale", () => {
	const stars = createCaravanStars();
	const payload = new Uint8Array( 2 + 8 );
	const view = new DataView( payload.buffer );
	view.setUint16( 0, 2, true );
	view.setUint32( 2, 9, true );
	view.setUint32( 6, 4242, true );
	assert.equal( stars.receive( { opcode: 0x3015, payload } ), false );
	assert.equal( stars.receive( { opcode: OP_CARAVAN_STARS, payload } ), true );
	assert.deepEqual( stars.gids(), [ 9, 4242 ] );
	const empty = new Uint8Array( 2 );
	new DataView( empty.buffer ).setUint16( 0, 0, true );
	assert.equal( stars.receive( { opcode: OP_CARAVAN_STARS, payload: empty } ), true );
	assert.deepEqual( stars.gids(), [], "an empty roster clears every mark" );
	assert.throws( () => stars.receive( { opcode: OP_CARAVAN_STARS, payload: new Uint8Array( 3 ) } ) );
	stars.clear();
	assert.deepEqual( stars.gids(), [] );
});

/*
================
flush

Take one journal batch off an entities owner and ack it, as
world.test.mjs does.
================
*/
function flush( owner ) {
	const batch = owner.take();
	if ( batch ) owner.ack( batch.sequence );
	return batch;
}

test("the crow frame and the roster frame decode only in their own owner", () => {
	const entities = createEntities();
	const stars = createCaravanStars();
	// The pet-crow visual owns 0x3FBB (entities.ts): u32 gid, u8 cue, u8
	// book, u16 region, three f32. While the roster shared that opcode
	// (#2968 review) this body threw in the roster owner; now the roster
	// owner refuses it by opcode, and the crow's owner still decodes it.
	const crow = new Uint8Array( 20 );
	const crowView = new DataView( crow.buffer );
	crowView.setUint32( 0, 100002, true );
	crow[4] = 1;
	crow[5] = 3;
	crowView.setUint16( 6, 257, true );
	crowView.setFloat32( 8, 12, true );
	crowView.setFloat32( 12, 3, true );
	crowView.setFloat32( 16, 34, true );
	assert.equal( stars.receive( { opcode: 0x3fbb, payload: crow } ), false );
	assert.deepEqual( stars.gids(), [] );
	entities.receive( { opcode: 0x3fbb, payload: crow } );
	assert.equal( flush( entities ).events.at( -1 ).kind, "pet-crow-effect" );
	// The roster under 0x3FBF decodes in its own owner; the entities
	// owner never reads it as a crow frame, it only journals it onward.
	const payload = new Uint8Array( 2 + 4 );
	const view = new DataView( payload.buffer );
	view.setUint16( 0, 1, true );
	view.setUint32( 2, 9, true );
	assert.equal( stars.receive( { opcode: OP_CARAVAN_STARS, payload } ), true );
	assert.deepEqual( stars.gids(), [ 9 ] );
	entities.receive( { opcode: OP_CARAVAN_STARS, payload } );
	assert.ok(
		flush( entities ).events.every( e => e.kind !== "pet-crow-effect" ),
		"no crow event from a roster frame"
	);
	assert.notEqual( OP_CARAVAN_STARS, 0x3fbb, "the roster must never take the crow's opcode again" );
});

test("the production UI paints five stars over a rostered transport only", () => {
	const fixture = uiFixture();
	try {
		const base = fixture.state.entities[0];
		const state = {
			...fixture.state,
			entities: [ base, { ...base, gid: 9, kind: "cos", name: "Camel", ownerGid: 1, ownerName: "Player" } ],
			gameplay: { ...fixture.state.gameplay, caravanStars: [ 9 ] }
		};
		// A few steps let the star texture finish loading.
		fixture.ui.step( state, 0 );
		fixture.ui.step( state, 50 );
		fixture.ui.step( state, 100 );
		const stars = fixture.scenes.at( -1 ).quads.filter( q =>
			q.characterAnchor === 9 && q.texture.endsWith( "com_star.png" )
		);
		assert.equal( stars.length, 5 );
		assert.ok( stars.every( q => q.color.every( ( c, i ) => c === tradeStarTint( 5 )[i] ) ) );
		fixture.ui.step( { ...state, gameplay: { ...state.gameplay, caravanStars: [] } }, 150 );
		assert.equal(
			fixture.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "com_star.png" ) ).length,
			0,
			"an empty roster paints no stars"
		);
	} finally {
		fixture.dispose();
	}
});
