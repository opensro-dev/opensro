/*
===========================================================================

movement-reseed-cell.test.mjs - a movement answer keeps the walker's nav cell

Native CNavigationController_ReseedSourcePosition (86D9D0) walks from the
mover's nav cell to the server's position. For a movement answer (B738) a
walk that reaches it keeps that cell's surface, and a blocked walk keeps the
cell and the position it has; only a server correction (B2F5, 30E3) finds
the cell anew at its exact float Y. Re-guessing the surface from the
answer's Y put a walker on a deck down on the terrain under it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { product } from "../helpers/navigation-fixture.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);

const GID = 7;
// The fixture's deck (a placed object 10 units up) over flat terrain at 0.
const DECK_Y = 10;
const onDeck = { regionId: 257, x: 10, y: DECK_Y, z: 50, angle: 0 };
const STEP_MS = 16;

/*
================
answer

A B738 destination answer with a source: u32 gid, mode 1, the destination
(u16 region, int16 x/y/z), then the source (u16 region, int16 x*10, float
y, int16 z*10).
================
*/
function answer( to, from ) {
	const p = new Uint8Array( 24 ), v = new DataView( p.buffer );
	v.setUint32( 0, GID, true );
	p[4] = 1;
	v.setUint16( 5, to.regionId, true );
	v.setInt16( 7, to.x, true );
	v.setInt16( 9, to.y, true );
	v.setInt16( 11, to.z, true );
	p[13] = 1;
	v.setUint16( 14, from.regionId, true );
	v.setInt16( 16, from.x * 10, true );
	v.setFloat32( 18, from.y, true );
	v.setInt16( 22, from.z * 10, true );
	return p;
}

/*
================
standing

A walker standing on the deck. blocked blocks every terrain tile, so the
deck's edge is a wall.
================
*/
function standing( blocked ) {
	const m = createMovement( () => {} );
	m.seed( onDeck );
	m.navigation( 257, product( 257, blocked ) );
	m.speeds( 20, 50, 0 );
	m.step( 0 );
	return m;
}

test("an answer whose source the walk reaches keeps the deck's height, whatever its Y", () => {
	const m = standing( false );
	// The source and destination lie on the deck; their Y is nearer the terrain.
	m.native( answer( { regionId: 257, x: 60, y: 4, z: 50 }, { regionId: 257, x: 30, y: 4.2, z: 50 } ), STEP_MS, GID );
	assert.equal( defined( m.state().pose ).y, DECK_Y, "the source stays on the deck" );
	for ( let t = 2 * STEP_MS; t < 3000; t += STEP_MS ) m.step( t );
	const end = defined( m.state().pose );
	assert.ok( Math.abs( end.x - 60 ) < 0.5 && end.y === DECK_Y, `walked the deck (${JSON.stringify( end )})` );
});

test("an answer whose source the walk cannot reach keeps the cell and the position", () => {
	const m = standing( true );
	m.native(
		answer( { regionId: 257, x: 60, y: 4, z: 50 }, { regionId: 257, x: 150, y: 10.4, z: 50 } ),
		STEP_MS,
		GID
	);
	const after = defined( m.state().pose );
	assert.ok( after.x === onDeck.x && after.y === DECK_Y, `kept the deck position (${JSON.stringify( after )})` );
});

test("a server correction off the deck still moves the walker there", () => {
	const m = standing( false );
	m.correct( { regionId: 257, x: 150, y: 0, z: 50, angle: 0 }, STEP_MS );
	const after = defined( m.state().pose );
	assert.ok( Math.abs( after.x - 150 ) < 0.5 && after.y === 0, `moved (${JSON.stringify( after )})` );
});
