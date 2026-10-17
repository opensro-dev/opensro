/*
===========================================================================

frame-hdr-order.test.mjs - frame formats, retained depth and overlay ordering

The command fake checks attachment compatibility and executes depth tests.
It distinguishes stored scene depth from discarded or cleared attachments,
including across the asynchronous particle continuation's second encoder.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createFrame } = await import( "../../src/engine/runtime/renderer/frame/frame.ts" );

const CANVAS_FORMAT = "bgra8unorm";
const HDR_FORMAT = "rgba16float";
const SCENE_DEPTH = 0.4;
const HIDDEN_LABEL_DEPTH = 0.8;

/*
================
fixture
================
*/
function fixture( hdr, scaled = false ) {
	const sceneFormat = hdr ? HDR_FORMAT : CANVAS_FORMAT;
	const view = { format: CANVAS_FORMAT }, scene = { format: sceneFormat }, depth = { value: undefined };
	const fullDepth = { value: undefined };
	const passes = [], visible = [], submits = [];
	let nextEncoder = 0;
	/*
	================
	geometry
	================
	*/
	function geometry( name ) {
		return { pipeline: { name, format: sceneFormat }, binding: {}, indexCount: 3, instanceCount: 1 };
	}
	const ui = [
		{ pipeline: { name: "label", format: CANVAS_FORMAT, depth: HIDDEN_LABEL_DEPTH }, layer: "world" },
		{ pipeline: { name: "background", format: CANVAS_FORMAT }, layer: "background" },
		{ pipeline: { name: "foreground", format: CANVAS_FORMAT } }
	].map( ( draw, first ) => ({ ...draw, binding: {}, first, count: 1 }) );
	const commands = {
		/*
		================
		createBundleEncoder
		================
		*/
		createBundleEncoder( _depth = true, scene = false ) {
			const format = scene ? sceneFormat : CANVAS_FORMAT;
			let pipeline;
			const draws = [];
			return {
				setPipeline( value ) {
					assert.equal( value.format, format );
					pipeline = value;
				},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				draw() {
					draws.push( pipeline );
				},
				drawIndexed() {
					draws.push( pipeline );
				},
				finish() {
					return { format, draws };
				}
			};
		},
		/*
		================
		createEncoder
		================
		*/
		createEncoder() {
			const id = ++nextEncoder;
			let finished = false;
			return {
				/*
				================
				beginComputePass

				Flare visibility consumes the main scene depth before UI clears it.
				================
				*/
				beginComputePass() {
					assert.equal( finished, false );
					return {
						setPipeline() {},
						setBindGroup() {},
						dispatchWorkgroups() {
							assert.equal( depth.value, SCENE_DEPTH );
						},
						end() {}
					};
				},
				/*
				================
				beginRenderPass
				================
				*/
				beginRenderPass( descriptor ) {
					assert.equal( finished, false, "cannot append to a submitted encoder" );
					const attachment = descriptor.depthStencilAttachment;
					if ( attachment?.depthLoadOp === "clear" ) attachment.view.value = attachment.depthClearValue;
					if ( attachment?.depthLoadOp === "load" ) {
						assert.notEqual( attachment.view.value, undefined, "cannot load discarded scene depth" );
					}
					const row = { descriptor, id, draws: /** @type {string[]} */ ([]) };
					passes.push( row );
					const format = descriptor.colorAttachments[0]?.view.format;
					let pipeline;
					/*
					================
					execute
					================
					*/
					function execute( draw ) {
						assert.equal( draw.format, format, descriptor.label + " format" );
						row.draws.push( draw.name );
						if ( draw.name === "world" ) attachment.view.value = SCENE_DEPTH;
						if ( draw.depth === undefined || draw.depth <= attachment.view.value ) {
							visible.push( draw.name );
						}
					}
					return {
						setPipeline( value ) {
							pipeline = value;
						},
						setBindGroup() {},
						setVertexBuffer() {},
						setIndexBuffer() {},
						setBlendConstant() {},
						draw() {
							execute( pipeline );
						},
						drawIndexed() {
							execute( pipeline );
						},
						executeBundles( bundles ) {
							for ( const bundle of bundles ) {
								assert.equal( bundle.format, format, descriptor.label + " bundle format" );
								for ( const draw of bundle.draws ) execute( draw );
							}
						},
						end() {
							if ( attachment?.depthStoreOp === "discard" ) attachment.view.value = undefined;
						}
					};
				},
				finish() {
					finished = true;
					return id;
				}
			};
		},
		submit( id ) {
			submits.push( id );
		}
	};
	const frame = createFrame( /** @type {any} */ (commands) );
	/*
	================
	resolve
	================
	*/
	function resolve( label ) {
		return {
			view: scene,
			encodePreview( encoder, target ) {
				const pass = encoder.beginRenderPass( {
					label: "preview-resolve",
					colorAttachments: [ { view: target, loadOp: "load", storeOp: "store" } ]
				} );
				pass.end();
			},
			encode( encoder, target ) {
				const pass = encoder.beginRenderPass( {
					label,
					colorAttachments: [ { view: target, loadOp: "clear", storeOp: "store" } ]
				} );
				pass.end();
			}
		};
	}
	return {
		passes,
		visible,
		submits,
		geometry,
		/*
		================
		draw
		================
		*/
		/** @param {{ deferred?: any, bloom?: boolean, reflection?: any, sunShadow?: any, preview?: any[], flares?: boolean, thunder?: boolean }} options */
		draw( { deferred, bloom = false, reflection, sunShadow, preview = [], flares = false, thunder = false } = {} ) {
			return frame.draw(
				/** @type {any} */ (view),
				undefined,
				undefined,
				/** @type {any} */ (depth),
				/** @type {any} */ ([ geometry( "world" ) ]),
				/** @type {any} */ (ui),
				preview,
				/** @type {any} */ (flares ? { compute: {}, binding: {}, entries: [] } : undefined),
				/** @type {any} */ (thunder ? geometry( "thunder" ) : undefined),
				undefined,
				undefined,
				[],
				undefined,
				deferred,
				/** @type {any} */ (bloom ?
					{ ...resolve( "bloom-resolve" ), view: { format: sceneFormat } } :
					undefined),
				reflection,
				/** @type {any} */ ({
					hdr: hdr ? resolve( "hdr-resolve" ) : undefined,
					sunShadow,
					sceneScale: scaled ?
						{
							view: scene,
							frameDepth: fullDepth,
							resolve: resolve( "scale-resolve" ).encode,
							resolveDepth() {
								assert.equal( depth.value, SCENE_DEPTH, "resolve must read preserved scene depth" );
								fullDepth.value = depth.value;
							}
						} :
						undefined
				})
			);
		}
	};
}

test("HDR deferred UI uses the canvas format and retains world-label occlusion with either bloom setting", async () => {
	for ( const bloom of [ false, true ] ) {
		for ( const mode of [ "none", "synchronous", "asynchronous" ] ) {
			const f = fixture( true );
			const deferred = mode === "none" ? undefined : {
				asynchronous: mode === "asynchronous",
				prepare: () => mode === "asynchronous" ? Promise.resolve( [] ) : []
			};
			await f.draw( { bloom, deferred } );
			assert.equal( f.visible.includes( "label" ), false, "labels behind the scene stay occluded" );
			assert.deepEqual( f.visible, [ "world", "background", "foreground" ] );
			const labelPass = f.passes.find( p => p.draws.includes( "label" ) );
			assert.equal( labelPass.descriptor.depthStencilAttachment.depthLoadOp, "load" );
			assert.equal( labelPass.descriptor.colorAttachments[0].view.format, CANVAS_FORMAT );
			assert.deepEqual( f.submits, mode === "asynchronous" ? [ 1, 2 ] : [ 1 ] );
			if ( deferred ) {
				assert.deepEqual( f.passes.find( p => p.descriptor.label === "deferred-particles" ).draws, [] );
			}
		}
	}
});

test("scaled frames retain labels and depth across deferred particles with HDR and bloom", async () => {
	for ( const hdr of [ false, true ] ) {
		for ( const bloom of [ false, true ] ) {
			for ( const asynchronous of [ false, true ] ) {
				const f = fixture( hdr, true );
				await f.draw( {
					bloom,
					flares: true,
					deferred: { asynchronous, prepare: () => asynchronous ? Promise.resolve( [] ) : [] }
				} );
				const labels = f.passes.filter( p => p.draws.includes( "label" ) );
				assert.equal( labels.length, 1, "labels execute exactly once" );
				assert.equal( labels[0].descriptor.depthStencilAttachment.depthLoadOp, "load" );
				assert.equal( f.visible.includes( "label" ), false, "occluded labels stay hidden" );
				assert.deepEqual( f.visible, [ "world", "background", "foreground" ] );
				assert.deepEqual( f.submits, asynchronous ? [ 1, 2 ] : [ 1 ] );
			}
		}
	}
});

test("native-off frame and preview keep their original pass and UI order", async () => {
	const f = fixture( false );
	await f.draw( { preview: /** @type {any} */ ([ f.geometry( "preview" ) ]) } );
	assert.deepEqual( f.passes.map( p => [ p.descriptor.label, p.draws ] ), [
		[ "main-pass", [ "world", "label", "background" ] ],
		[ "character-preview", [ "preview", "foreground" ] ]
	] );
	assert.equal( f.visible.includes( "label" ), false );
});

test("bloom without HDR or scene scaling retains scene depth for world labels", async () => {
	for ( const mode of [ "none", "synchronous", "asynchronous" ] ) {
		const f = fixture( false );
		const deferred = mode === "none" ? undefined : {
			asynchronous: mode === "asynchronous",
			prepare: () => mode === "asynchronous" ? Promise.resolve( [] ) : []
		};
		await f.draw( { bloom: true, deferred } );
		assert.equal(
			f.visible.includes( "label" ),
			false,
			"occluded labels behind 3D geometry stay hidden under bloom"
		);
		assert.deepEqual( f.visible, [ "world", "background", "foreground" ] );
		const labelPass = f.passes.find( p => p.draws.includes( "label" ) );
		assert.equal( labelPass.descriptor.label, "hud-after-flares" );
		assert.equal( labelPass.descriptor.depthStencilAttachment.depthLoadOp, "load" );
	}
});

test("HDR weather and flares finish before depth-tested labels and foreground UI", async () => {
	for ( const asynchronous of [ false, true ] ) {
		const f = fixture( true );
		await f.draw( {
			flares: true,
			thunder: true,
			deferred: { asynchronous, prepare: () => asynchronous ? Promise.resolve( [] ) : [] }
		} );
		assert.deepEqual( f.passes.map( p => p.descriptor.label ), [
			"main-pass",
			"deferred-particles",
			"weather-thunder",
			"hdr-resolve",
			"flare-chain",
			"hdr-world-ui",
			"hud-after-flares"
		] );
		assert.deepEqual( f.visible, [ "world", "thunder", "background", "foreground" ] );
	}
});

test("HDR preview preserves scene depth consumers and composites between background and foreground", async () => {
	for ( const bloom of [ false, true ] ) {
		for ( const asynchronous of [ false, true ] ) {
			const f = fixture( true );
			await f.draw( {
				bloom,
				flares: true,
				preview: /** @type {any} */ ([ f.geometry( "preview" ) ]),
				deferred: { asynchronous, prepare: () => asynchronous ? Promise.resolve( [] ) : [] }
			} );
			assert.deepEqual( f.visible, [ "world", "background", "preview", "foreground" ] );
			const rows = f.passes.map( p => p.descriptor.label );
			assert.ok( rows.indexOf( "hdr-world-ui" ) < rows.indexOf( "character-preview" ) );
			assert.deepEqual( rows.slice( -4 ), [
				"hud-after-flares",
				"character-preview",
				"preview-resolve",
				"hdr-preview-ui"
			] );
			const preview = f.passes.find( p => p.descriptor.label === "character-preview" );
			assert.deepEqual( preview.descriptor.colorAttachments[0].clearValue, [ 0, 0, 0, 0 ] );
			assert.equal( preview.descriptor.colorAttachments[0].loadOp, "clear" );
			if ( bloom ) {
				assert.notEqual(
					preview.descriptor.colorAttachments[0].view,
					f.passes[0].descriptor.colorAttachments[0].view
				);
			}
			assert.equal( preview.id, asynchronous ? 2 : 1 );
		}
	}
});

test("sun cascade completes before reflection receivers encode", async () => {
	const f = fixture( false ), order = [];
	await f.draw( {
		sunShadow: {
			encode() {
				order.push( "cascade" );
			}
		},
		reflection: {
			encode() {
				order.push( "reflection" );
			}
		}
	} );
	assert.deepEqual( order, [ "cascade", "reflection" ] );
});
