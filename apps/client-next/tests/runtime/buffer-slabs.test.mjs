/*
===========================================================================

buffer-slabs.test.mjs - tests for buffer-slabs.ts

Slab ranges bind at aligned offsets with their exact size, a retired range
returns to its slab only when the open frame closes (so no recorded pass
reads bytes a later draw owns), freed neighbours merge, large requests
keep a buffer of their own, and emptied slabs past one are destroyed. A
staged set uploads each written buffer's dirty span in one write.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { root } from "../../tools/project.mjs";

const source = name => pathToFileURL( path.join( root, "src/engine/runtime/renderer/device", name ) ).href;
const { createBufferSlabs, createStagedSlabs } = await import( source( "buffer-slabs.ts" ) );
const { createRetirement } = await import( source( "retirement.ts" ) );

/*
================
fakeDevice
================
*/
function fakeDevice() {
	const buffers = [], writes = [];
	return {
		buffers,
		writes,
		limits: {},
		queue: {
			writeBuffer( buffer, offset, data, start, size ) {
				if ( buffer.destroyed ) throw Error( "written while destroyed" );
				writes.push( { buffer, offset, start, size } );
			}
		},
		createBuffer( descriptor ) {
			const buffer = {
				...descriptor,
				destroyed: 0,
				destroy() {
					this.destroyed++;
				}
			};
			buffers.push( buffer );
			return buffer;
		}
	};
}

test("ranges bind at 256-byte aligned offsets with their exact size, in shared slabs", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createBufferSlabs( device, "geometry-material", 72, retirement.retire );
	const a = slabs.allocate( 448 ), b = slabs.allocate( 64 ), c = slabs.allocate( 160 );
	assert.deepEqual( [ a.binding.offset, b.binding.offset, c.binding.offset ], [ 0, 512, 768 ] );
	assert.deepEqual( [ a.binding.size, b.binding.size, c.binding.size ], [ 448, 64, 160 ] );
	assert.equal( a.binding.buffer, c.binding.buffer );
	assert.equal( device.buffers.length, 1, "one slab holds all three" );
	assert.equal( device.buffers[0].label, "geometry-material" );
	assert.equal( device.buffers[0].usage, 72 );
});

test("a range retired during a frame is not handed out again until the frame closes", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createBufferSlabs( device, "geometry-instances", 0, retirement.retire );
	const first = slabs.allocate( 160 );
	retirement.open();
	slabs.free( first );
	const during = slabs.allocate( 160 );
	assert.notEqual( during.binding.offset, first.binding.offset, "the open frame may still read the old range" );
	retirement.close();
	const after = slabs.allocate( 160 );
	assert.equal( after.binding.offset, first.binding.offset, "the closed frame's range is reused" );
	assert.equal( device.buffers[0].destroyed, 0, "returning a range never destroys the slab" );
});

test("freed neighbours merge back into one range", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createBufferSlabs( device, "geometry-transform", 0, retirement.retire );
	const ranges = [ slabs.allocate( 256 ), slabs.allocate( 256 ), slabs.allocate( 256 ) ];
	slabs.free( ranges[0] );
	slabs.free( ranges[2] );
	slabs.free( ranges[1] );
	const whole = slabs.allocate( 4 << 20 >> 2 );
	assert.equal( whole.binding.offset, 0 );
	assert.equal( device.buffers.length, 1 );
	assert.equal( slabs.stats().usedBytes, 4 << 20 >> 2 );
});

test("a large request keeps its own buffer and destroys it on release", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createBufferSlabs( device, "geometry-instances", 0, retirement.retire );
	const large = slabs.allocate( (1 << 20) + 160 );
	assert.equal( large.binding.offset, 0 );
	assert.equal( large.binding.buffer.size, (1 << 20) + 160 );
	slabs.free( large );
	assert.equal( large.binding.buffer.destroyed, 1 );
	slabs.allocate( 64 );
	slabs.dispose();
	assert.ok( device.buffers.every( buffer => buffer === large.binding.buffer || buffer.destroyed === 1 ) );
});

test("a set keeps one empty slab and destroys the others once their frame closes", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createBufferSlabs( device, "geometry-material", 0, retirement.retire );
	// Three slabs' worth: a scene's high-water mark.
	const ranges = [ 0, 1, 2 ].map( () => slabs.allocate( 1 << 20 ) );
	const filled = [ ...ranges, ...[ 0, 1, 2 ].flatMap( () => [ 0, 1, 2 ].map( () => slabs.allocate( 1 << 20 ) ) ) ];
	assert.equal( slabs.stats().slabs, 3 );
	retirement.open();
	for ( const range of filled ) slabs.free( range );
	assert.ok( device.buffers.every( buffer => !buffer.destroyed ), "the open frame may still read every slab" );
	retirement.close();
	assert.equal( slabs.stats().slabs, 1, "one empty slab stays for the next scene" );
	assert.equal( device.buffers.filter( buffer => buffer.destroyed ).length, 2 );
	assert.equal( slabs.allocate( 64 ).binding.buffer.destroyed, 0, "the next scene reuses the kept slab" );
	assert.equal( device.buffers.length, 3 );
});

test("a staged set writes each dirty buffer once, never a destroyed one", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createStagedSlabs( device, "geometry-material", 0, retirement.retire );
	const ranges = [ 0, 1, 2, 3, 4 ].map( () => slabs.allocate( 1 << 20 ) );
	const [a, b, c, d, e] = ranges, row = Float32Array.of( 1, 2, 3, 4 );
	assert.notEqual( e.binding.buffer, a.binding.buffer, "the fifth range opens a second slab" );
	slabs.write( c, row, 16 );
	slabs.write( a, row, 8 );
	slabs.write( e, row, 16 );
	assert.equal( slabs.flush(), 2 );
	assert.deepEqual( device.writes.map( w => [ w.buffer, w.offset, w.size ] ), [
		[ a.binding.buffer, 0, c.binding.offset + 16 ],
		[ e.binding.buffer, 0, 16 ]
	] );
	assert.equal( slabs.flush(), 0, "nothing written since" );
	// A written range whose slab empties, beside another empty one, and is
	// destroyed before the flush.
	slabs.write( a, row, 16 );
	slabs.free( e );
	for ( const range of [ a, b, c, d ] ) slabs.free( range );
	assert.equal( a.binding.buffer.destroyed, 1 );
	assert.equal( slabs.flush(), 0 );
});

test("a staged write inside a range lands at its offset and never past the range", () => {
	const device = fakeDevice(), retirement = createRetirement();
	const slabs = createStagedSlabs( device, "geometry-material-dynamic", 0, retirement.retire );
	const a = slabs.allocate( 256 ), b = slabs.allocate( 256 ), row = Float32Array.of( 1, 2, 3, 4 );
	slabs.write( b, row, 16, 192 );
	slabs.write( b, row.subarray( 0, 3 ), 12, 128 );
	assert.equal( slabs.flush(), 1 );
	assert.deepEqual( device.writes.map( w => [ w.offset, w.size ] ), [ [ b.binding.offset + 128, 80 ] ] );
	assert.throws( () => slabs.write( a, row, 16, 244 ), /Invalid staged slab write/ );
	assert.throws( () => slabs.write( a, row, 16, 2 ), /Invalid staged slab write/ );
	assert.equal( slabs.flush(), 0, "a refused write marks nothing" );
});
