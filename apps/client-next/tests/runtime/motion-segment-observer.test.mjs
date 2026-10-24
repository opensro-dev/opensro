/*
===========================================================================

motion-segment-observer.test.mjs - the motion segment seam reports installs

The browser pursuit recorder observes movement through the seam motion.ts
exports (setMotionSegmentObserver), never through the module's source.
These tests pin the contract: every segment installed is reported with its
entity, and silence returns when the observer is cleared.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createEntityMotion, setMotionSegmentObserver } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);

const GID = 7;
const REGION = 25000;

/*
================
entity
================
*/
function entity( extra = {} ) {
	return {
		gid: GID,
		regionId: REGION,
		x: 100,
		y: 10,
		z: 200,
		heading: 0,
		movementMode: 3,
		walkSpeed: 10,
		runSpeed: 50,
		...extra
	};
}

/*
================
destinationPacket

A 0xB738 destination body without a source block (decodeNativeMovement):
gid, mode 1, then the destination region and coordinates.
================
*/
function destinationPacket( x, z ) {
	const p = new Uint8Array( 14 ), v = new DataView( p.buffer );
	v.setUint32( 0, GID, true );
	p[4] = 1;
	v.setUint16( 5, REGION, true );
	v.setInt16( 7, x, true );
	v.setInt16( 9, 10, true );
	v.setInt16( 11, z, true );
	return p;
}

test("the observer hears every installed segment, then silence when cleared", () => {
	const seen = [];
	setMotionSegmentObserver( ( gid, segment ) => seen.push( { gid, segment } ) );
	try {
		const motion = createEntityMotion();
		motion.spawn(
			entity( { spawnDestination: { regionId: REGION, x: 160, y: 10, z: 200 } } ),
			1000
		);
		motion.receive( destinationPacket( 160, 260 ), entity(), 2000 );
	} finally {
		setMotionSegmentObserver( null );
	}
	assert.equal( seen.length, 2 );
	assert.equal( seen[0].gid, GID );
	assert.equal( seen[0].segment.start, 1000 );
	assert.equal( seen[0].segment.to.x, 160 );
	assert.equal( seen[0].segment.duration, 1200 );
	assert.equal( seen[1].gid, GID );
	assert.equal( seen[1].segment.start, 2000 );
	assert.equal( seen[1].segment.to.x, 160 );
	assert.equal( seen[1].segment.to.z, 260 );

	const silent = [];
	setMotionSegmentObserver( ( gid, segment ) => silent.push( { gid, segment } ) );
	setMotionSegmentObserver( null );
	createEntityMotion().receive( destinationPacket( 160, 260 ), entity(), 3000 );
	assert.equal( silent.length, 0 );
});
