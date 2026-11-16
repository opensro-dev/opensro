/*
===========================================================================

ui-crowd-records.test.mjs - a crowd's name tags all reach the GPU

The original draws every name tag. The port's GPU UI buffer used to hold a
fixed 8192 records (one per glyph), and a few hundred players' tags past
it made renderer admission refuse the scene ("Invalid UI scene"), which
ended the session. The HUD publishes the crowd's tags as it always did;
admission (foundation/ui/ui.ts) takes the scene, and the instance buffer
(device/ui.ts) grows to it in powers of two, retires the old buffer through
the frame, rebinds and packs every record again.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );
const { createUiPreparation } = await import( "../../src/engine/foundation/ui/ui.ts" );
const { createUiResources } = await import( "../../src/engine/runtime/renderer/device/ui.ts" );
const { uiRecordCount } = await import( "../../src/engine/foundation/rendering/text-run.ts" );

const RECORD_FLOATS = 28, RECORD_BYTES = 112;

/*
================
crowdScene

The HUD's own product for the local player among `count` players standing
close around it, as the world-overheads golden places its neighbour.
================
*/
function crowdScene( count ) {
	const fx = uiFixture();
	try {
		for ( let i = 0; i < count; i++ ) {
			const angle = i * 2.399963, radius = 6 + Math.sqrt( i ) * 2.2;
			fx.state.entities.push( {
				gid: 100 + i,
				regionId: 1,
				x: Math.cos( angle ) * radius,
				y: 0,
				z: 20 + Math.sin( angle ) * radius,
				heading: 0,
				kind: "player",
				name: `Crowd_Player_${i}`,
				mountedOn: 0
			} );
		}
		let now = 0;
		for ( let tick = 0; tick < 16; tick++ ) fx.ui.step( fx.state, now += 100 );
		const product = fx.products.at( -1 );
		assert.ok( product, "the HUD published no scene" );
		return product;
	} finally {
		fx.dispose?.();
	}
}

/*
================
fakeDevice

Buffers keep their bytes; bind groups record the buffer they name.
================
*/
function fakeDevice() {
	const buffers = [], groups = [];
	/** @type {any} */
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		createBuffer( options ) {
			const buffer = {
				label: options.label,
				size: options.size,
				bytes: new Uint8Array( options.size ),
				destroyed: false,
				destroy() {
					this.destroyed = true;
				}
			};
			buffers.push( buffer );
			return buffer;
		},
		createSampler: () => ({}),
		createTexture: () => ({ createView: () => ({}), destroy() {} }),
		createBindGroup( options ) {
			const group = { storage: options.entries[0].resource.buffer };
			groups.push( group );
			return group;
		},
		queue: {
			writeTexture() {},
			copyExternalImageToTexture() {},
			writeBuffer( buffer, offset, data ) {
				assert.ok( offset + data.byteLength <= buffer.size, `write past ${buffer.label}` );
				buffer.bytes.set( new Uint8Array( data.buffer, data.byteOffset, data.byteLength ), offset );
			}
		}
	};
	return { buffers, groups, device };
}

test("seven hundred players' name tags pass admission and all reach the instance buffer", async t => {
	/** @type {any} */
	const gpu = globalThis;
	const saved = [ gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage ];
	gpu.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	gpu.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	gpu.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	t.after( () => {
		[gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage] = saved;
	} );

	const quiet = crowdScene( 0 ), crowd = crowdScene( 700 );
	const quietRecords = uiRecordCount( quiet.quads ), records = uiRecordCount( crowd.quads );
	const tags = new Set( crowd.quads.map( q => q.characterAnchor ).filter( gid => gid !== undefined ) );
	assert.ok( tags.size >= 700, `${tags.size} characters carry overhead quads` );
	assert.ok( quietRecords < 8192 && records > 8192, `${quietRecords} records alone, ${records} in the crowd` );

	// Renderer admission: the step that refused the scene and ended the session.
	const preparation = createUiPreparation();
	const admit = ( scene ) => {
		const prepared = preparation.prepare( scene );
		assert.ok( prepared );
		return prepared.scene;
	};
	const admitted = admit( crowd );
	assert.equal( uiRecordCount( admitted.quads ), records );

	const { buffers, groups, device } = fakeDevice(), retired = [];
	const owner = createUiResources( device, "rgba8unorm", () => "rgba8unorm", buffer => retired.push( buffer ) );
	await owner.ready;
	for ( const id of new Set( admitted.quads.flatMap( q => [ q.texture, q.mask?.texture ?? "" ] ) ) ) {
		owner.texture( id, { data: new Uint8ClampedArray( 16 ), width: 2, height: 2, colorSpace: "srgb" } );
	}
	const instances = () => buffers.filter( b => b.label === "ui-instances" );
	assert.equal( instances()[0].size, 8192 * RECORD_BYTES );

	// The quiet HUD fits the first buffer; the crowd grows it once.
	owner.prepare( admit( quiet ) );
	const first = instances()[0];
	const draws = owner.prepare( admitted );
	const storage = instances().at( -1 );
	assert.equal( instances().length, 2 );
	assert.equal( first.destroyed, false, "the old buffer was destroyed while a frame may name it" );
	assert.equal( storage.size, 2 ** Math.ceil( Math.log2( records ) ) * RECORD_BYTES );
	assert.deepStrictEqual(
		retired.filter( r => r.label === "ui-instances" ),
		[ first ],
		"the old buffer is left to the frame to destroy"
	);
	assert.equal( draws.reduce( ( sum, d ) => sum + d.count, 0 ), records, "every record is drawn" );
	for ( const draw of draws ) {
		assert.equal( /** @type {any} */ (draw.binding).storage, storage, "a draw names the retired buffer" );
	}
	assert.ok( groups.length > 0 );

	// Every record was packed into the new buffer, the ones the quiet HUD
	// had already uploaded to the old buffer included.
	const floats = new Float32Array( storage.bytes.buffer, 0, records * RECORD_FLOATS );
	let record = 0;
	for ( const quad of admitted.quads ) {
		const glyphs = quad.run ? quad.run.glyphs : [ null ];
		for ( const glyph of glyphs ) {
			const at = record * RECORD_FLOATS;
			const x = glyph ? quad.rect[0] + glyph.x : quad.rect[0], y = glyph ? quad.rect[1] + glyph.y : quad.rect[1];
			assert.equal( floats[at], Math.fround( x ), `record ${record} x` );
			assert.equal( floats[at + 1], Math.fround( y ), `record ${record} y` );
			assert.equal( floats[at + 8], Math.fround( quad.color[0] ), `record ${record} colour` );
			record++;
		}
	}

	// Back to the quiet HUD: the grown buffer stays and nothing is retired.
	owner.prepare( admit( quiet ) );
	assert.equal( instances().length, 2 );
	assert.equal( retired.filter( r => r.label === "ui-instances" ).length, 1 );
	owner.dispose();
	assert.equal( storage.destroyed, true );
});
