/*
===========================================================================

lifecycle.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { defined } from "../helpers/defined.mjs";
function mockGlobal( t, name, value ) {
	const original = Object.getOwnPropertyDescriptor( globalThis, name );
	Object.defineProperty( globalThis, name, { configurable: true, value } );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, name, original );
		else delete globalThis[name];
	} );
}
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createFrame } = await load( "src/engine/runtime/renderer/frame/frame.ts" );
const { createSurface } = await load( "src/engine/runtime/renderer/surface/surface.ts" );
const { createDevice } = await load( "src/engine/runtime/renderer/device/device.ts" );
const { writeSnapshot, readSnapshot, SNAPSHOT_BYTES } = await load( "src/engine/contracts/simulation.ts" );
test("one pass ends before one submit", () => {
	const log = [];
	const frame = createFrame( {
		createEncoder() {
			log.push( "encoder" );
			return {
				beginRenderPass() {
					log.push( "begin" );
					return {
						setBlendConstant( color ) {
							log.push( "blend " + color.join() );
						},
						end() {
							log.push( "end" );
						}
					};
				},
				finish() {
					log.push( "finish" );
					return "buffer";
				}
			};
		},
		submit( buffer ) {
			assert.equal( buffer, "buffer" );
			log.push( "submit" );
		}
	} );
	frame.draw( {} );
	// D3DRS_BLENDFACTOR is white until a bloom composite sets it.
	assert.deepEqual( log, [ "encoder", "begin", "blend 1,1,1,1", "end", "finish", "submit" ] );
});
test("creation preview overlays cinematic art and stays below foreground controls", () => {
	const log = [];
	const frame = createFrame( {
		createBundleEncoder() {
			let pipeline;
			return {
				setPipeline( p ) {
					pipeline = p;
				},
				setBindGroup() {},
				draw() {},
				finish() {
					return pipeline;
				}
			};
		},
		createEncoder() {
			return {
				beginRenderPass( { label } ) {
					log.push( label );
					return {
						executeBundles( bundles ) {
							log.push( ...bundles );
						},
						setPipeline( p ) {
							log.push( p );
						},
						setBindGroup() {},
						setVertexBuffer() {},
						setIndexBuffer() {},
						drawIndexed() {},
						setBlendConstant() {},
						end() {}
					};
				},
				finish() {}
			};
		},
		submit() {}
	} );
	const ui = [ { pipeline: "bars", layer: "background", count: 1, first: 0 }, {
		pipeline: "controls",
		count: 1,
		first: 1
	} ];
	frame.draw( {}, undefined, undefined, {}, [], ui, [ { pipeline: "character" } ] );
	assert.deepEqual( log, [ "main-pass", "bars", "character-preview", "character", "controls" ] );
	log.length = 0;
	frame.draw( {}, undefined, undefined, {}, [], ui, [] );
	assert.deepEqual( log, [ "main-pass", "bars", "controls" ] );
});
test("surface configures on resize and forbids use after disposal", () => {
	let configured = 0, disposed = 0;
	const canvas = {
		getContext() {
			return {
				getCurrentTexture() {
					return {
						createView() {
							return {};
						}
					};
				},
				unconfigure() {
					disposed++;
				}
			};
		}
	};
	const surface = createSurface( canvas, {
		createDepth() {
			return { view: {}, dispose() {} };
		},
		configure() {
			configured++;
		}
	}, "bgra8unorm" );
	surface.acquire( { width: 100, height: 100 } );
	surface.acquire( { width: 100, height: 100 } );
	assert.equal( configured, 1 );
	surface.acquire( { width: 200, height: 100 } );
	assert.equal( configured, 2 );
	surface.dispose();
	surface.dispose();
	assert.equal( disposed, 1 );
	assert.throws( () => surface.acquire( { width: 1, height: 1 } ) );
});
test("snapshot contract rejects wrong version and size", () => {
	const b = new ArrayBuffer( SNAPSHOT_BYTES );
	writeSnapshot( b, 12, 192, 200 );
	assert.deepEqual( readSnapshot( b ), { sequence: 12, timeMs: 192, publishedAtMs: 200, acceptedInputSequence: 0 } );
	new DataView( b ).setUint32( 0, 99, true );
	assert.throws( () => readSnapshot( b ) );
	assert.throws( () => readSnapshot( new ArrayBuffer( 1 ) ) );
});
test("device resolved after disposal is destroyed, never activated", async () => {
	const old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	let resolve;
	let destroyed = 0;
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			gpu: {
				requestAdapter: () =>
					Promise.resolve( {
						features: new Set(),
						requestDevice: () => new Promise( r => resolve = r )
					} ),
				getPreferredCanvasFormat: () => "bgra8unorm"
			}
		}
	} );
	try {
		const owner = createDevice();
		await Promise.resolve();
		owner.dispose();
		defined( resolve )( {
			destroy() {
				destroyed++;
			}
		} );
		await new Promise( r => setImmediate( r ) );
		assert.equal( owner.phase(), "disposed" );
		assert.equal( owner.commands(), null );
		assert.equal( destroyed, 1 );
	} finally {
		if ( old ) {
			Object.defineProperty( globalThis, "navigator", old );
		} else {
			delete globalThis.navigator;
		}
	}
});

test("a device failure reports its cause, not the errors that follow it (BUG-022)", async () => {
	const old = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	let uncaptured;
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			gpu: {
				requestAdapter: async () => ({
					features: new Set(),
					requestDevice: async () => ({
						lost: new Promise( () => {} ),
						addEventListener( name, handler ) {
							if ( name === "uncapturederror" ) uncaptured = handler;
						},
						destroy() {},
						createShaderModule() {
							throw new Error( "createTexture: format bgra8unorm-srgb is not renderable" );
						}
					})
				}),
				getPreferredCanvasFormat: () => "bgra8unorm"
			}
		}
	} );
	try {
		const owner = createDevice();
		await new Promise( r => setImmediate( r ) );
		assert.equal( owner.phase(), "failed" );
		defined( uncaptured )( { error: { message: "GPUTexture.createView: texture is not valid" } } );
		assert.match( String( owner.error() ), /not renderable/, "the cause survives the cascade" );
		owner.dispose();
	} finally {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else delete globalThis.navigator;
	}
});

const { createRenderer } = await load( "src/engine/runtime/renderer/renderer.ts" );
test("device loss rebuilds only renderer resources and bounds repeated recovery", async t => {
	mockGlobal( t, "GPUShaderStage", { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } );
	mockGlobal( t, "GPUBufferUsage", { UNIFORM: 64, COPY_DST: 8, STORAGE: 128 } );
	const old = Object.getOwnPropertyDescriptor( globalThis, "navigator" ),
		oldUsage = Object.getOwnPropertyDescriptor( globalThis, "GPUTextureUsage" );
	const devices = [], textures = [];
	let configured = 0,
		unconfigured = 0,
		submissions = 0,
		mipSubmissions = 0,
		uploads = 0,
		textureDisposals = 0,
		imageDisposals = 0;
	Object.defineProperty( globalThis, "GPUTextureUsage", {
		configurable: true,
		value: { TEXTURE_BINDING: 4, COPY_DST: 2, RENDER_ATTACHMENT: 16 }
	} );
	const context = {
		configure() {
			configured++;
		},
		unconfigure() {
			unconfigured++;
		},
		getCurrentTexture() {
			return {
				createView() {
					return {};
				}
			};
		}
	};
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			gpu: {
				requestAdapter: async () => ({
					features: new Set(),
					requestDevice: async () => {
						let lose;
						const device = {
							lost: new Promise( resolve => lose = resolve ),
							addEventListener() {},
							destroyed: false,
							destroy() {
								this.destroyed = true;
							},
							pushErrorScope() {},
							popErrorScope: async () => null,
							createShaderModule() {
								return { getCompilationInfo: async () => ({ messages: [] }) };
							},
							createBindGroupLayout() {
								return {};
							},
							createPipelineLayout() {
								return {};
							},
							createComputePipelineAsync: async () => ({}),
							createRenderPipeline() {
								return {
									getBindGroupLayout() {
										return {};
									}
								};
							},
							async createRenderPipelineAsync() {
								return {
									getBindGroupLayout() {
										return {};
									}
								};
							},
							createSampler() {
								return {};
							},
							createBuffer() {
								return { destroy() {} };
							},
							createTexture() {
								const texture = {
									destroyed: false,
									createView() {
										return {};
									},
									destroy() {
										assert.equal( this.destroyed, false, "texture disposed twice" );
										this.destroyed = true;
										textureDisposals++;
									}
								};
								textures.push( texture );
								return texture;
							},
							createBindGroup() {
								return {};
							},
							createRenderBundleEncoder() {
								return {
									setPipeline() {},
									setBindGroup() {},
									draw() {},
									finish() {
										return {};
									}
								};
							},
							createCommandEncoder( descriptor ) {
								return {
									beginRenderPass() {
										return {
											end() {},
											setBlendConstant() {},
											executeBundles() {},
											setPipeline() {},
											setBindGroup() {},
											draw() {}
										};
									},
									finish() {
										return { label: descriptor.label };
									}
								};
							},
							queue: {
								writeBuffer() {},
								writeTexture() {},
								submit( buffers ) {
									for ( const buffer of buffers ) {
										if ( buffer.label === "sro-frame" ) submissions++;
										else if ( buffer.label === "upload-texture-mips" ) mipSubmissions++;
										else assert.fail( "Unexpected submission" );
									}
								},
								copyExternalImageToTexture() {
									uploads++;
								}
							}
						};
						devices.push( { device, lose } );
						return device;
					}
				}),
				getPreferredCanvasFormat: () => "bgra8unorm"
			}
		}
	} );
	try {
		const renderer = createRenderer( { getContext: () => context } );
		renderer.setImage( {
			width: 16,
			height: 16,
			close() {
				imageDisposals++;
			}
		} );
		const settle = () => new Promise( resolve => setImmediate( resolve ) );
		await settle();
		renderer.frame( { width: 100, height: 100 } );
		assert.equal( renderer.phase(), "running", renderer.error() );
		assert.equal( submissions, 1 );
		for ( let attempt = 0; attempt < 3; attempt++ ) {
			devices[attempt].lose( { reason: "unknown", message: "fixture loss" } );
			await settle();
			renderer.frame( { width: 100, height: 100 } );
			assert.ok( devices[attempt].device.destroyed );
			await settle();
			renderer.frame( { width: 100, height: 100 } );
			assert.equal( renderer.phase(), "running" );
		}
		assert.equal( configured, 4 );
		assert.equal( unconfigured, 3 );
		assert.equal( submissions, 4 );
		assert.equal( mipSubmissions, 4 );
		devices[3].lose( { reason: "unknown", message: "repeated loss" } );
		await settle();
		renderer.frame( { width: 100, height: 100 } );
		assert.equal( renderer.phase(), "failed" );
		assert.equal( devices.length, 4 );
		assert.equal( uploads, 4 );
		assert.equal( imageDisposals, 0 );
		renderer.dispose();
		assert.equal( imageDisposals, 1 );
		assert.equal( textureDisposals, 16 );
		renderer.dispose();
		assert.equal( textureDisposals, 16 );
		assert.equal( imageDisposals, 1 );
	} finally {
		if ( old ) Object.defineProperty( globalThis, "navigator", old );
		else delete globalThis.navigator;
		if ( oldUsage ) Object.defineProperty( globalThis, "GPUTextureUsage", oldUsage );
		else delete globalThis.GPUTextureUsage;
	}
});

test("retained geometry records once, reuses commands, and rebuilds changed bindings", () => {
	const log = [];
	let recordings = 0, executions = 0, submissions = 0;
	const frame = createFrame( {
		createBundleEncoder() {
			recordings++;
			return {
				setPipeline: x => log.push( [ "pipeline", x ] ),
				setBindGroup: ( i, x ) => log.push( [ "bind", i, x ] ),
				setVertexBuffer: ( i, x ) => log.push( [ "vertex", i, x ] ),
				setIndexBuffer: ( x, format ) => log.push( [ "index", x, format ] ),
				drawIndexed: ( ...args ) => log.push( [ "direct", ...args ] ),
				finish: () => "bundle"
			};
		},
		createEncoder: () => ({
			beginRenderPass: () => ({
				executeBundles: bundles => {
					assert.deepEqual( bundles, [ "bundle" ] );
					executions++;
				},
				setBlendConstant() {},
				end() {}
			}),
			finish: () => "commands"
		}),
		submit: () => submissions++
	} );
	const draw = {
		pipeline: "pipeline",
		binding: "binding",
		vertices: "vertices",
		indices: "indices",
		indirect: "arguments",
		instanceCapacity: 1,
		count: 6,
		indexCount: 6,
		instanceCount: 1
	};
	frame.draw( {}, undefined, draw );
	frame.draw( {}, undefined, draw );
	assert.equal( recordings, 1 );
	assert.equal( executions, 2 );
	assert.equal( submissions, 2 );
	assert.deepEqual( log, [ [ "pipeline", "pipeline" ], [ "bind", 0, "binding" ], [ "vertex", 0, "vertices" ], [
		"index",
		"indices",
		"uint32"
	], [ "direct", 6, 1, 0, 0, 0 ] ] );
	frame.draw( {}, undefined, { ...draw, count: 3 } );
	assert.equal( recordings, 2 );
	frame.draw( {} );
	assert.equal( executions, 3 );
	assert.equal( submissions, 4 );
});

test("instance growth retains vertex/index buffers and releases replaced storage", async t => {
	const originalNavigator = Object.getOwnPropertyDescriptor( globalThis, "navigator" ),
		originalUsage = Object.getOwnPropertyDescriptor( globalThis, "GPUBufferUsage" );
	mockGlobal( t, "GPUShaderStage", { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } );
	mockGlobal( t, "GPUTextureUsage", { TEXTURE_BINDING: 4, COPY_DST: 2 } );
	const usages = { COPY_DST: 8, VERTEX: 32, INDEX: 16, UNIFORM: 64, STORAGE: 128, INDIRECT: 256 },
		buffers = [],
		writes = [];
	Object.defineProperty( globalThis, "GPUBufferUsage", { configurable: true, value: usages } );
	const gpu = {
		lost: new Promise( () => {} ),
		addEventListener() {},
		createShaderModule() {
			return { getCompilationInfo: async () => ({ messages: [] }) };
		},
		createBindGroupLayout() {
			return {};
		},
		createPipelineLayout() {
			return {};
		},
		createComputePipelineAsync: async () => ({}),
		createSampler() {
			return {};
		},
		createRenderPipeline() {
			return {
				getBindGroupLayout() {
					return {};
				}
			};
		},
		async createRenderPipelineAsync() {
			return {
				getBindGroupLayout() {
					return {};
				}
			};
		},
		createBuffer( descriptor ) {
			const result = {
				...descriptor,
				destroyed: false,
				destroy() {
					this.destroyed = true;
				}
			};
			buffers.push( result );
			return result;
		},
		createTexture() {
			return {
				createView() {
					return {};
				},
				destroy() {}
			};
		},
		createBindGroup() {
			return {};
		},
		pushErrorScope() {},
		async popErrorScope() {
			return null;
		},
		queue: {
			writeBuffer( buffer, offset, data, start = 0, length = data.byteLength ) {
				writes.push( new Uint8Array( data, start, length ).slice() );
			},
			writeTexture() {}
		},
		destroy() {}
	};
	Object.defineProperty( globalThis, "navigator", {
		configurable: true,
		value: {
			gpu: {
				requestAdapter: async () => ({
					features: new Set(),
					requestDevice: async () => gpu
				}),
				getPreferredCanvasFormat: () => "bgra8unorm"
			}
		}
	} );
	t.after( () => {
		if ( originalNavigator ) Object.defineProperty( globalThis, "navigator", originalNavigator );
		else delete globalThis.navigator;
		if ( originalUsage ) Object.defineProperty( globalThis, "GPUBufferUsage", originalUsage );
		else delete globalThis.GPUBufferUsage;
	} );
	const owner = createDevice();
	await new Promise( resolve => setImmediate( resolve ) );
	assert.equal( owner.phase(), "running", owner.error() );
	const commands = owner.geometry(),
		identity = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	const sharedBuffers = [ ...buffers ];
	let draw = commands.upload( {
		positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
		indices: new Uint32Array( [ 0, 1, 2 ] ),
		transform: identity
	} );
	assert.deepEqual( [ draw.indexCount, draw.instanceCount ], [ 3, 1 ] );
	assert.throws( () => {
		draw.indexCount = 0;
	}, TypeError );
	const allocated = buffers.length;
	const storage = buffers.find( buffer => !sharedBuffers.includes( buffer ) && (buffer.usage & usages.STORAGE) ),
		vertices = draw.vertices,
		indices = draw.indices;
	draw = commands.updateInstances( draw, new Float32Array( [ ...identity, ...identity ] ) );
	assert.equal( buffers.length, allocated + 1 );
	assert.ok( storage.destroyed );
	assert.equal( draw.vertices, vertices );
	assert.equal( draw.indices, indices );
	assert.deepEqual( [ draw.indexCount, draw.instanceCount ], [ 3, 2 ] );
	commands.updateIndices( draw, new Uint32Array() );
	assert.deepEqual( [ draw.indexCount, draw.instanceCount ], [ 0, 1 ] );
	assert.equal( commands.updateInstances( draw, new Float32Array() ), draw );
	assert.equal( buffers.length, allocated + 1 );
	assert.deepEqual( [ draw.indexCount, draw.instanceCount ], [ 3, 0 ] );
	const matrices = new Float32Array( [ ...identity, ...identity ] ),
		opacity = new Float32Array( [ .25, .75 ] ),
		appearance = Float32Array.from( { length: 16 }, ( _, i ) => i / 16 );
	const expected = ( m, a, p ) => {
		const result = [];
		for ( let i = 0; i < m.length / 16; i++ ) {
			result.push(
				...m.slice( i * 16, i * 16 + 16 ),
				a?.[i] ?? 1,
				0,
				0,
				0,
				...(p ? p.slice( i * 8, i * 8 + 8 ) : [ 1, 1, 1, 1, 1, 1, 0, 0 ]),
				...Array( 12 ).fill( 0 )
			);
		}
		return result;
	};
	for (
		const [m, a, p] of [ [ matrices, opacity, appearance ], [ identity, undefined, undefined ], [
			matrices,
			undefined,
			appearance
		], [ matrices, opacity, undefined ] ]
	) {
		const previous = writes.at( -1 )?.slice();
		commands.updateInstances( draw, m, a, p );
		assert.deepEqual( [ ...new Float32Array( writes.at( -1 ).buffer ) ], expected( m, a, p ) );
		if ( previous ) {
			assert.deepEqual(
				writes.at( -2 ),
				previous,
				"later packing cannot alter bytes already passed to the queue"
			);
		}
	}
	commands.release( draw );
	assert.ok( buffers.filter( buffer => !sharedBuffers.includes( buffer ) ).every( buffer => buffer.destroyed ) );
	assert.ok( sharedBuffers.every( buffer => !buffer.destroyed ) );
	owner.dispose();
	assert.ok( buffers.every( buffer => buffer.destroyed ) );
});

test("direct bundles track changing counts on a stable resource and retain unchanged counts", () => {
	let selection = [ 6, 1 ], recordings = 0;
	const direct = [];
	const draw = {
		pipeline: {},
		binding: {},
		vertices: {},
		indices: {},
		get indexCount() {
			return selection[0];
		},
		get instanceCount() {
			return selection[1];
		}
	};
	const frame = createFrame( {
		createBundleEncoder() {
			recordings++;
			return {
				setPipeline() {},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				drawIndexed( ...args ) {
					direct.push( args );
				},
				finish: () => ({})
			};
		},
		createEncoder: () => ({
			beginRenderPass: () => ({ executeBundles() {}, setBlendConstant() {}, end() {} }),
			finish: () => ({})
		}),
		submit() {}
	} );
	for ( const counts of [ [ 6, 1 ], [ 3, 1 ], [ 3, 1 ], [ 3, 0 ], [ 6, 2 ] ] ) {
		selection = counts;
		frame.draw( {}, undefined, undefined, undefined, [ draw ] );
	}
	assert.equal( recordings, 3 );
	assert.deepEqual( direct, [ [ 6, 1, 0, 0, 0 ], [ 3, 1, 0, 0, 0 ], [ 6, 2, 0, 0, 0 ] ] );
});

test("bounded direct bundles preserve the complete ordered command tape through visibility and count changes", () => {
	const pool = Array.from(
		{ length: 96 },
		( _, id ) => ({ pipeline: id, binding: {}, vertices: {}, indices: {}, indexCount: 6, instanceCount: 1 })
	);
	let actual = [], recordedDraws = 0;
	const frame = createFrame( {
		createBundleEncoder() {
			let pipeline;
			const tape = [];
			return {
				setPipeline( value ) {
					pipeline = value;
				},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				drawIndexed( ...args ) {
					recordedDraws++;
					tape.push( [ pipeline, ...args ] );
				},
				finish: () => tape
			};
		},
		createEncoder: () => ({
			beginRenderPass: () => ({
				executeBundles( bundles ) {
					actual.push( ...bundles.flat() );
				},
				setBlendConstant() {},
				end() {}
			}),
			finish: () => ({})
		}),
		submit() {}
	} );
	const check = world => {
		actual = [];
		frame.draw( {}, undefined, undefined, undefined, world );
		assert.deepEqual(
			actual,
			world.filter( d => d.indexCount > 0 && d.instanceCount > 0 ).map(
				d => [ d.pipeline, d.indexCount, d.instanceCount, 0, 0, 0 ]
			)
		);
	};
	check( pool );
	const initial = recordedDraws;
	check( pool );
	assert.equal( recordedDraws, initial );
	pool[7].indexCount = 3;
	check( pool );
	assert.ok( recordedDraws - initial <= 32, "one count change must not re-record the entire world" );
	let seed = 200454;
	for ( let step = 0; step < 200; step++ ) {
		seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0;
		const index = seed % pool.length;
		if ( step % 7 === 0 ) pool[index] = { ...pool[index], binding: {} };
		pool[index].instanceCount = seed % 5;
		pool[(index + 1) % pool.length].indexCount = (seed >>> 4) % 7;
		let world = pool.filter( ( _, i ) => (i + step) % 5 !== 0 );
		if ( step % 3 === 0 ) world.reverse();
		if ( step % 19 === 0 ) world = [];
		check( world );
		check( world );
	}
});

test("retained geometry invalidates in-place binding replacements and omits empty commands", () => {
	let recordings = 0, executed = [];
	const draw = { pipeline: {}, binding: { id: 1 }, vertices: {}, indices: {}, indexCount: 6, instanceCount: 1 };
	const frame = createFrame( {
		createBundleEncoder() {
			recordings++;
			let binding;
			const tape = [];
			return {
				setPipeline() {},
				setBindGroup( _, b ) {
					binding = b;
				},
				setVertexBuffer() {},
				setIndexBuffer() {},
				drawIndexed( n, c ) {
					assert.ok( n > 0 && c > 0 );
					tape.push( binding.id );
				},
				finish: () => tape
			};
		},
		createEncoder: () => ({
			beginRenderPass: () => ({
				executeBundles( b ) {
					executed.push( ...b.flat() );
				},
				setBlendConstant() {},
				end() {}
			}),
			finish: () => ({})
		}),
		submit() {}
	} );
	const render = () => {
		executed = [];
		frame.draw( {}, undefined, undefined, undefined, [ draw ] );
		return executed;
	};
	assert.deepEqual( render(), [ 1 ] );
	assert.deepEqual( render(), [ 1 ] );
	assert.equal( recordings, 1 );
	draw.binding = { id: 2 };
	assert.deepEqual( render(), [ 2 ] );
	assert.equal( recordings, 2 );
	draw.instanceCount = 0;
	assert.deepEqual( render(), [] );
	draw.instanceCount = 1;
	draw.indexCount = 0;
	assert.deepEqual( render(), [] );
	draw.indexCount = 6;
	assert.deepEqual( render(), [ 2 ] );
});
