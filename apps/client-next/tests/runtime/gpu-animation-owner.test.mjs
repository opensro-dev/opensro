/*
===========================================================================

gpu-animation-owner.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createGpuAnimationResources } = { ...(await load( "src/engine/runtime/renderer/device/animation.ts" )) };
globalThis.GPUBufferUsage = { UNIFORM: 1, COPY_DST: 2, STORAGE: 4 };
// WebGPU's default minStorageBufferOffsetAlignment.
const ALIGNMENT = 256;
/*
================
I
================
*/
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
/*
================
fixture

The fake device keeps every buffer's bytes, so a test reads what a
dispatch's bound input range holds, as the shader would at submit.
================
*/
function fixture( { reject = false, failBinding = false } = {} ) {
	const buffers = [],
		writes = [],
		gpu = {
			/*
			================
			createShaderModule
			================
			*/
			createShaderModule() {
				return {};
			},
			/*
			================
			createComputePipelineAsync
			================
			*/
			createComputePipelineAsync() {
				return reject ? Promise.reject( Error( "pipeline failed" ) ) : Promise.resolve( {
					/*
					================
					getBindGroupLayout
					================
					*/
					getBindGroupLayout() {
						return {};
					}
				} );
			},
			/*
			================
			createBuffer
			================
			*/
			createBuffer( { size, label } ) {
				const b = {
					size,
					label,
					bytes: new Uint8Array( size ),
					destroyed: 0,
					/*
					================
					destroy
					================
					*/
					destroy() {
						this.destroyed++;
					}
				};
				buffers.push( b );
				return b;
			},
			/*
			================
			createBindGroup
			================
			*/
			createBindGroup( { entries } ) {
				if ( failBinding ) throw Error( "binding failed" );
				return { entries };
			},
			queue: {
				/*
				================
				writeBuffer
				================
				*/
				writeBuffer( buffer, offset, data, start = 0, length = data.byteLength ) {
					buffer.bytes.set( new Uint8Array( data, start, length ), offset );
					writes.push( { label: buffer.label, offset, length } );
				}
			}
		};
	const primitive = { joints: [ 0 ], inverseBind: I() },
		clip = {
			name: "step",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "STEP",
				times: Float32Array.of( 0, .5, 1 ),
				values: Float32Array.of( 0, 0, 0, 1, 0, 0, 2, 0, 0 )
			} ]
		},
		model = {
			nodes: [ {
				name: "root",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} ],
			clips: [ clip ],
			primitives: [ primitive ]
		};
	const owner = createGpuAnimationResources( gpu );
	/*
	================
	encode

	Encodes the pending streams. Each dispatch reports its bound input
	range and the rows it holds when the pass is encoded: every write comes
	before it, as every write comes before the frame's submit.
	================
	*/
	function encode() {
		const dispatches = [];
		let group;
		owner.encode( {
			/*
			================
			beginComputePass
			================
			*/
			beginComputePass() {
				return {
					setPipeline() {},
					/*
					================
					setBindGroup
					================
					*/
					setBindGroup( _index, bound ) {
						group = bound;
					},
					/*
					================
					dispatchWorkgroups
					================
					*/
					dispatchWorkgroups( count ) {
						const { buffer, offset, size } = group.entries.find( e => e.binding === 2 ).resource;
						const values = [ ...new Float32Array( buffer.bytes.buffer, offset, count * 4 ) ];
						dispatches.push( { count, buffer, offset, size, values } );
					},
					end() {}
				};
			}
		} );
		return dispatches;
	}
	return {
		buffers,
		writes,
		primitive,
		clip,
		model,
		owner,
		encode,
		source: new Float32Array( 16 ),
		output: { size: 64 }
	};
}
test("an appended clip rebinds compute inputs while retaining the geometry palette", async () => {
	const f = fixture();
	await f.owner.ready;
	assert.equal( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: .2 } ] ), true );
	const clip = { ...f.clip, name: "new-action" }, model = { ...f.model, clips: [ f.clip, clip ] };
	assert.equal( f.owner.prepare( f.source, f.output, model, f.primitive, [ { clip, time: .3 } ] ), true );
	assert.equal( f.owner.stats().models, 1 );
	assert.equal( f.encode()[0].values[1], 1, "new catalog selects the appended clip" );
	assert.throws( () =>
		f.owner.prepare(
			f.source,
			f.output,
			{ ...model, nodes: [ ...model.nodes ] },
			f.primitive,
			[ { clip, time: .3 } ]
		), /stream identity/ );
	f.owner.release( f.source );
	f.owner.dispose();
	assert.ok( f.buffers.every( buffer => buffer.destroyed === 1 ) );
});

test("STEP phase cannot round forward across a key; cancellation removes queued work", async () => {
	const f = fixture();
	await f.owner.ready;
	assert.ok(
		f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: .5 - Number.EPSILON } ] )
	);
	assert.ok( f.encode()[0].values[0] < .5 );
	f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: .2 } ] );
	f.owner.cancel( f.source );
	f.owner.encode( {
		/*
		================
		beginComputePass
		================
		*/
		beginComputePass() {
			throw Error( "cancelled dispatch" );
		}
	} );
	f.owner.release( f.source );
	assert.equal( f.owner.stats().models, 0 );
	assert.equal( f.owner.stats().streamBytes, 0 );
	f.owner.dispose();
	assert.ok( f.buffers.every( b => b.destroyed === 1 ) );
});
test("failed pipeline permits CPU fallback and partial allocation failure retires owned buffers", async () => {
	const f = fixture( { reject: true } );
	await f.owner.ready;
	assert.equal( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] ), false );
	assert.match( f.owner.stats().failure, /pipeline failed/ );
	f.owner.dispose();
	const g = fixture( { failBinding: true } );
	await g.owner.ready;
	assert.throws(
		() => g.owner.prepare( g.source, g.output, g.model, g.primitive, [ { clip: g.clip, time: 0 } ] ),
		/binding failed/
	);
	assert.equal( g.owner.stats().models, 0 );
	assert.equal( g.owner.stats().staticBytes, 0 );
	g.owner.dispose();
	assert.ok( g.buffers.every( b => b.destroyed === 1 ) );
});
test("invalid requests cannot admit orphan model storage; disposal invalidates pending dispatch", async () => {
	const f = fixture();
	await f.owner.ready;
	assert.throws(
		() => f.owner.prepare( f.source, { size: 1 }, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] ),
		/capacity/
	);
	assert.equal( f.owner.stats().models, 0 );
	assert.equal( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: NaN } ] ), false );
	assert.equal( f.owner.stats().models, 0 );
	f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: 0 } ] );
	f.owner.dispose();
	assert.throws( () => f.owner.encode( {} ), /Disposed/ );
	assert.ok( f.buffers.every( b => b.destroyed === 1 ) );
});

test("sparse GPU inputs preserve destination indices around CPU-owned slots", async () => {
	const f = fixture();
	await f.owner.ready;
	f.owner.prepare( new Float32Array( 64 ), { size: 256 }, f.model, f.primitive, [
		null,
		{ clip: f.clip, time: .1 },
		null,
		{ clip: f.clip, time: .6 }
	] );
	const [dispatch] = f.encode();
	const inputs = dispatch.values;
	assert.equal( inputs.length, 8 );
	assert.equal( inputs[2], 1 );
	assert.equal( inputs[6], 3 );
	assert.equal( dispatch.count, 2 );
	f.owner.dispose();
});

/*
================
sharedClipSet

An assembled character is a new model on its body's clips. It must bind
the clip set already resident, upload only its skeleton, and the clip set
must outlive every model but the last.
================
*/
test("models on the same clips share one clip buffer until the last one is released", async () => {
	const f = fixture();
	await f.owner.ready;
	const assembled = { ...f.model, nodes: [ ...f.model.nodes ], primitives: [ { ...f.primitive } ] };
	const other = new Float32Array( 16 );
	assert.ok( f.owner.prepare( f.source, f.output, f.model, f.primitive, [ { clip: f.clip, time: .1 } ] ) );
	assert.ok( f.owner.prepare( other, f.output, assembled, assembled.primitives[0], [ { clip: f.clip, time: .2 } ] ) );
	assert.equal( f.owner.stats().models, 2 );
	assert.equal( f.owner.stats().clipSets, 1 );
	assert.equal( f.buffers.filter( b => b.label === "animation-clips" ).length, 1 );
	assert.equal( f.buffers.filter( b => b.label === "animation-skeleton" ).length, 2 );
	f.owner.release( f.source );
	const clips = f.buffers.find( b => b.label === "animation-clips" );
	assert.equal( clips.destroyed, 0, "the other model still binds the clip set" );
	f.owner.release( other );
	assert.equal( clips.destroyed, 1 );
	assert.equal( f.owner.stats().clipSets, 0 );
	assert.equal( f.owner.stats().staticBytes, 0 );
	f.owner.dispose();
});

/*
================
stream

A stream of capacity poses on the fixture's one-joint primitive, with
the rows its samples must put in the bound input range.
================
*/
function stream( f, capacity, times ) {
	const samples = times.map( time => time === null ? null : { clip: f.clip, time } );
	const rows = times.flatMap( ( time, i ) => time === null ? [] : [ time, 0, i, 0 ] );
	return {
		source: new Float32Array( capacity * 16 ),
		output: { size: capacity * 64 },
		capacity,
		samples,
		rows
	};
}

/*
================
inputWrites

The arena writes the fixture recorded since count writes.
================
*/
const inputWrites = ( f, count ) => f.writes.slice( count ).filter( w => w.label === "animation-inputs" ).length;

/*
================
sharedInputs

Every stream binds a range of one input slab. The bytes a dispatch's
range holds are the rows prepare was given, beside a released neighbour
and in its reused range; each encode uploads them in one write, or none
when nothing is pending.
================
*/
test("stream inputs share one slab, uploaded once an encode, through release and reuse", async () => {
	const f = fixture();
	await f.owner.ready;
	const check = ( streams, label ) => {
		const before = f.writes.length;
		for ( const s of streams ) assert.ok( f.owner.prepare( s.source, s.output, f.model, f.primitive, s.samples ) );
		assert.equal( inputWrites( f, before ), 0, `${label}: prepare writes only the mirror` );
		const dispatches = f.encode();
		assert.equal( inputWrites( f, before ), 1, `${label}: one input write an encode` );
		assert.equal( dispatches.length, streams.length );
		dispatches.forEach( ( d, i ) => {
			assert.equal( d.size, streams[i].capacity * 16, `${label}: the binding keeps its own buffer's size` );
			assert.equal( d.offset % ALIGNMENT, 0 );
			assert.deepEqual( d.values, streams[i].rows, `${label}: stream ${i} reads its own rows` );
		} );
		assert.equal( new Set( dispatches.map( d => d.buffer ) ).size, 1, `${label}: one input buffer` );
		return dispatches;
	};
	const a = stream( f, 1, [ .25 ] ),
		b = stream( f, 20, [ null, .5, .75, null, .125 ] ),
		c = stream( f, 3, [ .375, null, .625 ] );
	const first = check( [ a, b, c ], "first frame" );
	f.owner.release( b.source );
	const d = stream( f, 17, [ .5, .25 ] );
	const second = check( [ a, c, d ], "reused range" );
	assert.equal( second[2].offset, first[1].offset, "the released range is handed out again" );
	assert.deepEqual( [ second[0].offset, second[1].offset ], [ first[0].offset, first[2].offset ] );
	assert.equal( f.owner.stats().inputUploads, 2 );
	const idle = f.writes.length;
	assert.deepEqual( f.encode(), [] );
	f.owner.prepare( a.source, a.output, f.model, f.primitive, a.samples );
	f.owner.cancel( a.source );
	assert.deepEqual( f.encode(), [] );
	assert.equal( inputWrites( f, idle ), 0, "nothing pending, nothing written" );
	for ( const s of [ a, c, d ] ) f.owner.release( s.source );
	f.owner.dispose();
	assert.ok( f.buffers.every( buffer => buffer.destroyed === 1 ) );
});

/*
================
streamBudget

The stream budget refuses what it refused with a buffer a stream: one
stream at the budget fits the arena at its limit, one pose more does not.
================
*/
test("the stream budget admits a stream at the limit and refuses one past it", async () => {
	const f = fixture();
	await f.owner.ready;
	// 16 bytes a pose plus the 48-byte configuration uniform, within 8 MB.
	const fits = Math.floor( ((8 << 20) - 48) / 16 );
	const over = stream( f, fits + 1, [ .5 ] );
	assert.equal( f.owner.prepare( over.source, over.output, f.model, f.primitive, over.samples ), false );
	assert.equal( f.owner.stats().models, 0 );
	const most = stream( f, fits, [ .5 ] );
	assert.ok( f.owner.prepare( most.source, most.output, f.model, f.primitive, most.samples ) );
	assert.deepEqual( f.encode()[0].values, most.rows );
	f.owner.release( most.source );
	f.owner.dispose();
	assert.ok( f.buffers.every( buffer => buffer.destroyed === 1 ) );
});
