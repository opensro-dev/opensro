/*
===========================================================================

movement-reaim.test.mjs - tests for movement.ts, motion.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const pose = { regionId: 257, x: 100, y: 10, z: 100, angle: 0 };
test("captured live approach reaches the server stop without accumulated refresh drift", async () => {
	const data = JSON.parse( await readFile( "tests/fixtures/movement-reaim-capture.json", "utf8" ) ),
		m = createMovement( () => {} );
	m.seed( data.pose );
	m.speeds( data.pose.walkSpeed, data.pose.runSpeed, 0 );
	const first = data.packets[0].now, last = data.packets.at( -1 ).now;
	let at = 0;
	for ( let now = first; now <= last; now += 16 ) {
		while ( data.packets[at]?.now === now ) {
			const packet = data.packets[at++], p = Buffer.from( packet.hex, "hex" );
			if ( packet.opcode === 0xb738 ) m.native( p, now, data.pose.gid );
			else {
				m.step( now );
				const position = m.state().pose,
					dx = defined( position ).x - p.readFloatLE( 6 ),
					dz = defined( position ).z - p.readFloatLE( 14 );
				assert.ok( Math.hypot( dx, dz ) < 1, "approach correction residual " + Math.hypot( dx, dz ) );
			}
		}
		m.step( now );
	}
	assert.equal( at, data.packets.length );
});
function packet( x, source, angular = false ) {
	const offset = angular ? 8 : 13, p = Buffer.alloc( offset + 1 + (source ? 10 : 0) );
	p.writeUInt32LE( 7 );
	if ( angular ) p.writeUInt16LE( 1234, 6 );
	else {
		p[4] = 1;
		p.writeUInt16LE( 257, 5 );
		p.writeInt16LE( x, 7 );
		p.writeInt16LE( 10, 9 );
		p.writeInt16LE( 100, 11 );
	}
	if ( source ) {
		p[offset] = 1;
		p.writeUInt16LE( source.regionId, offset + 1 );
		p.writeInt16LE( source.x * 10, offset + 3 );
		p.writeFloatLE( source.y, offset + 5 );
		p.writeInt16LE( source.z * 10, offset + 9 );
	}
	return p;
}
for ( const lane of [ "local", "monster" ] ) {
	test( lane + " source-less re-aim before the tick preserves every elapsed movement step", () => {
		const local = createMovement( () => {} ), remote = createEntityMotion();
		let entity = { ...pose, gid: 7, heading: 0, movementMode: 3, walkSpeed: 20, runSpeed: 50 };
		local.seed( pose );
		const receive = ( p, now ) => lane === "local" ? local.native( p, now, 7 ) : remote.receive( p, entity, now );
		const step = now => {
			if ( lane === "local" ) {
				local.step( now );
				return local.state().pose;
			}
			const row = remote.step( now )[0];
			if ( row ) {
				entity = { ...entity, ...row };
			}
			return entity;
		};
		receive( packet( 1500 ), 0 );
		for ( let now = 16; now <= 9600; now += 16 ) {
			if ( now % 96 === 0 ) receive( packet( 1500 + (now % 192 ? 1 : 0) ), now );
			const p = step( now );
			assert.ok(
				Math.abs( defined( p ).x - (100 + 50 * now / 1000) ) < 1e-7,
				`${lane} discarded elapsed time at ${now}: ${defined( p ).x}`
			);
		}
		// A source-less angular acknowledgement only enters action state 9
		// (0x776200): the path in progress keeps running from where it is.
		receive( packet( 0, undefined, true ), 9616 );
		assert.ok( Math.abs( defined( step( 9616 ) ).x - 580.8 ) < 1e-7 );
		assert.ok( Math.abs( defined( step( 10000 ) ).x - 600 ) < 1e-7, "the path continues after a keep" );
	} );
}
for ( const lane of [ "local", "monster" ] ) {
	test( lane + " explicit source and end-of-path re-aim retain authority", () => {
		const local = createMovement( () => {} ), remote = createEntityMotion();
		let entity = { ...pose, gid: 7, heading: 0, movementMode: 3, walkSpeed: 20, runSpeed: 50 };
		local.seed( pose );
		const receive = ( p, now ) => lane === "local" ? local.native( p, now, 7 ) : remote.receive( p, entity, now );
		const step = now => {
			if ( lane === "local" ) {
				local.step( now );
				return local.state().pose;
			}
			const row = remote.step( now )[0];
			if ( row ) {
				entity = { ...entity, ...row };
			}
			return entity;
		};
		receive( packet( 110 ), 0 );
		step( 192 );
		receive( packet( 1500 ), 250 );
		assert.ok( Math.abs( defined( step( 250 ) ).x - 110 ) < 1e-8, "completed prior path clamps at its endpoint" );
		receive( packet( 1500, { ...pose, x: 300 } ), 300 );
		assert.equal( defined( step( 300 ) ).x, 300, "explicit wire source overrides predicted position" );
		assert.ok( Math.abs( defined( step( 400 ) ).x - 305 ) < 1e-8 );
	} );
}
