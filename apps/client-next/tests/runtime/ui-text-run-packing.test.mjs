/*
===========================================================================

ui-text-run-packing.test.mjs - text runs pack the bytes their glyphs would

The UI publishes a label as one text run; the GPU UI packer writes one
record per glyph. Fed runs, it must write exactly the bytes and draw
ranges it writes for the same scene expanded into glyph quads
(expandTextRuns), across retained, replaced and shifted frames. A
retained label projection must pack what a fresh one packs, frame by
frame, while writing nothing for a frame where no label moved.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const { createUiResources } = await import( "../../src/engine/runtime/renderer/device/ui.ts" );
const { createUiText } = await import( "../../src/engine/runtime/ui/text/text.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
const { resolveTextOverlaps } = await import( "../../src/engine/foundation/rendering/ui-glyphs.ts" );
const { createUiPreparation } = await import( "../../src/engine/foundation/ui/ui.ts" );
const { createLabelProjection, projectCharacterLabels } = await import(
	"../../src/engine/foundation/ui/character-labels.ts"
);

const FONT = "/assets/fonts/native-ui-font-atlas.png";
const RECORD_FLOATS = 28;

/*
================
fakeDevice

A device whose buffers keep the bytes written to them, so two packers can
be compared byte for byte, and which logs every write.
================
*/
function fakeDevice() {
	/** @type {{ label: string; bytes: Uint8Array; destroy(): void; }[]} */
	const buffers = [];
	/** @type {{ label: string; offset: number; bytes: number; }[]} */
	const writes = [];
	/** @type {{ id: number; }[]} */
	const groups = [];
	/** @type {any} */
	const device = {
		createBindGroupLayout: () => ({}),
		createPipelineLayout: () => ({}),
		createShaderModule: () => ({}),
		createRenderPipelineAsync: async () => ({ getBindGroupLayout: () => ({}) }),
		/*
		================
		createBuffer
		================
		*/
		createBuffer( options ) {
			const buffer = { label: options.label, bytes: new Uint8Array( options.size ), destroy() {} };
			buffers.push( buffer );
			return buffer;
		},
		createSampler: () => ({}),
		createTexture: () => ({ createView: () => ({}), destroy() {} }),
		/*
		================
		createBindGroup
		================
		*/
		createBindGroup() {
			const group = { id: groups.length };
			groups.push( group );
			return group;
		},
		queue: {
			writeTexture() {},
			copyExternalImageToTexture() {},
			/*
			================
			writeBuffer
			================
			*/
			writeBuffer( buffer, offset, data ) {
				writes.push( { label: buffer.label, offset, bytes: data.byteLength } );
				buffer.bytes.set( new Uint8Array( data.buffer, data.byteOffset, data.byteLength ), offset );
			}
		}
	};
	return { buffers, groups, writes, device };
}

/*
================
drawShape

Draw ranges with bindings named by creation order, comparable across devices.
================
*/
function drawShape( draws, groups ) {
	return draws.map( d => ({ first: d.first, count: d.count, layer: d.layer, binding: groups.indexOf( d.binding ) }) );
}

/*
================
gpuFlags

Node has no WebGPU: the packer reads only these usage flags.
================
*/
function gpuFlags( t ) {
	/** @type {any} */
	const gpu = globalThis;
	const saved = [ gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage ];
	gpu.GPUBufferUsage = { STORAGE: 1, COPY_DST: 2, UNIFORM: 4 };
	gpu.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
	gpu.GPUTextureUsage = { TEXTURE_BINDING: 1, COPY_DST: 2, RENDER_ATTACHMENT: 4 };
	t.after( () => {
		[gpu.GPUBufferUsage, gpu.GPUShaderStage, gpu.GPUTextureUsage] = saved;
	} );
}

/*
================
packers

Two UI packers on their own fake devices, holding the font and a sprite.
================
*/
async function packers() {
	const a = fakeDevice(), b = fakeDevice();
	const first = createUiResources( a.device, "rgba8unorm", () => "rgba8unorm" ),
		second = createUiResources( b.device, "rgba8unorm", () => "rgba8unorm" );
	await first.ready;
	await second.ready;
	for ( const owner of [ first, second ] ) {
		for ( const id of [ FONT, "sprite" ] ) {
			owner.texture( id, { data: new Uint8ClampedArray( 16 ), width: 2, height: 2, colorSpace: "srgb" } );
		}
	}
	return { a, b, first, second };
}

/*
================
uiText

The production text owner over the generated font atlas.
================
*/
function uiText() {
	const atlas = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json" );
	const text = createUiText( {
		available: () => 1,
		request: () => 1,
		take: () => ({
			kind: "bytes",
			id: 1,
			buffer: atlas.buffer.slice( atlas.byteOffset, atlas.byteOffset + atlas.length )
		}),
		cancel() {}
	}, "http://fixture.invalid/" );
	for ( let i = 0; i < 3; i++ ) text.step();
	return text;
}

test("text runs pack byte-identical records and draws to their expanded glyph quads", async t => {
	gpuFlags( t );
	const { a, b, first: runs, second: glyphs } = await packers();
	const text = uiText();
	let seed = 23;
	const random = () => ((seed = (seed * 1103515245 + 12345) >>> 0) / 4294967296);
	const words = [ "Jangan", "Hotan Bazaar", "", "Ox", "Western China Donwhang Gate", "123" ];
	let previous = null, glyphRecords = 0;
	for ( let frame = 0; frame < 600; frame++ ) {
		/** @type {any[]} */
		let quads;
		if ( previous && random() < .3 ) quads = previous;
		else {
			quads = [];
			const count = 1 + Math.floor( random() * 12 );
			for ( let n = 0; n < count; n++ ) {
				if ( random() < .55 ) {
					const x = Math.floor( random() * 400 ) + (random() < .5 ? .5 : 0);
					quads.push( ...text.quads(
						words[Math.floor( random() * words.length )],
						[ x, Math.floor( random() * 300 ), 20 + Math.floor( random() * 200 ), 14 ],
						random() < .5 ? [ 0, 0, 800, 600 ] : [ x - 5, 0, 60 + random() * 100, 600 ],
						[ 1, random(), 1, 1 ],
						{ hAlign: Math.floor( random() * 3 ), overflow: random() < .2 ? "clip" : undefined }
					) );
				} else {
					quads.push( {
						texture: random() < .1 ? "absent" : random() < .5 ? "sprite" : "",
						rect: [ random() * 500, random() * 400, 5 + random() * 50, 5 + random() * 20 ],
						uv: [ 0, 0, 1, 1 ],
						clip: [ 0, 0, 800, 600 ],
						color: [ random(), 1, 1, 1 ],
						...(random() < .2 ? { depth: random() } : {})
					} );
				}
			}
			quads = resolveTextOverlaps( quads );
		}
		previous = quads;
		const expanded = expandTextRuns( quads );
		const drawsA = runs.prepare( { revision: frame, width: 800, height: 600, quads } ),
			drawsB = glyphs.prepare( { revision: frame, width: 800, height: 600, quads: expanded } );
		const used = expanded.length * RECORD_FLOATS * 4;
		glyphRecords += expanded.length;
		assert.deepStrictEqual( drawShape( drawsA, a.groups ), drawShape( drawsB, b.groups ), `draws frame ${frame}` );
		assert.deepStrictEqual(
			a.buffers[0].bytes.subarray( 0, used ),
			b.buffers[0].bytes.subarray( 0, used ),
			`bytes frame ${frame}`
		);
	}
	assert.ok( glyphRecords > 10000, "the random scenes must exercise many glyph records" );
	// The same run object one record later: record r held glyph r of the run and
	// must now hold glyph r - 1. A cache keyed only by quad identity misses this.
	const run = text.quads( "Hotan Bazaar", [ 10, 10, 200, 14 ], [ 0, 0, 800, 600 ], [ 1, 1, 1, 1 ] )[0];
	const plain = {
		texture: "",
		rect: [ 1, 2, 3, 4 ],
		uv: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 800, 600 ],
		color: [ 1, 1, 1, 1 ]
	};
	for ( const quads of /** @type {any[][]} */ ([ [ run ], [ plain, run ] ]) ) {
		runs.prepare( { revision: 9000 + quads.length, width: 800, height: 600, quads } );
		const expanded = expandTextRuns( quads );
		glyphs.prepare( { revision: 9000 + quads.length, width: 800, height: 600, quads: expanded } );
		const used = expanded.length * RECORD_FLOATS * 4;
		assert.deepStrictEqual( a.buffers[0].bytes.subarray( 0, used ), b.buffers[0].bytes.subarray( 0, used ) );
	}
});

/*
================
labelScene

A HUD product as the UI publishes it: static text and a sprite, then a
board (plate and name) per character anchor, one world-anchored marker and
more static text. Each board's colour names its gid, so a test can tell
whose quads changed; the highlighted gid's plate is dimmed.
================
*/
function labelScene( text, revision, width, height, gids, names, highlight ) {
	const clip = [ 0, 0, width, height ], uv = [ 0, 0, 1, 1 ];
	const quads = [
		...text.quads( "Hotan", [ 10, 10, 100, 14 ], clip, [ 1, 1, 1, 1 ] ),
		{ texture: "sprite", rect: [ 20, 30, 40, 40 ], uv, clip, color: [ 1, 1, 1, 1 ] }
	];
	for ( const gid of gids ) {
		const color = [ 1, gid / 1000, 1, 1 ], plate = [ gid === highlight ? .5 : 1, gid / 1000, 1, 1 ];
		quads.push( { texture: "sprite", rect: [ -40, -22, 80, 18 ], uv, clip, color: plate, characterAnchor: gid } );
		for ( const q of text.quads( names.get( gid ) ?? "Crowd_Player_" + gid, [ -40, -20, 80, 14 ], clip, color ) ) {
			quads.push( { ...q, characterAnchor: gid } );
		}
	}
	quads.push( {
		texture: "sprite",
		rect: [ -8, -8, 16, 16 ],
		uv,
		clip,
		color: [ 1, 1, 1, 1 ],
		worldAnchor: { regionId: 1, x: 50, y: 2, z: 30 }
	} );
	quads.push( ...text.quads( "Lv 80", [ 10, 40, 100, 14 ], clip, [ 1, 1, 1, 1 ] ) );
	return { revision, width, height, quads };
}

/*
================
records

The GPU record range [first, end) each output quad occupies.
================
*/
function records( quads ) {
	let record = 0;
	return quads.map( q => {
		const first = record;
		record += q.run ? q.run.glyphs.length : 1;
		return { first, end: record };
	} );
}

test("a retained label projection packs a fresh projection's bytes and draws, writing nothing when still", async t => {
	gpuFlags( t );
	const { a, b, first: retained, second: fresh } = await packers();
	const text = uiText(), preparation = createUiPreparation(), projection = createLabelProjection();
	const crowd = Array.from( { length: 12 }, ( _, i ) => i + 1 );
	// The tape's world: screen position and depth per gid at 800 x 600, the
	// camera's shift, hidden anchors, the viewport and the damage text.
	const state = {
		revision: 1,
		width: 800,
		height: 600,
		target: 800,
		gids: crowd,
		names: new Map(),
		highlight: 0,
		at: new Map( crowd.map( gid => [ gid, [ 40.25 + gid * 55, 100.25 + gid * 30, .5 - gid * .01 ] ] ) ),
		camera: 0,
		hidden: new Set(),
		damage: 0
	};
	const publish = () => {
		state.revision++;
		const prepared = preparation.prepare(
			labelScene( text, state.revision, state.width, state.height, state.gids, state.names, state.highlight )
		);
		assert.ok( prepared );
		return prepared.scene;
	};
	// A crowd member the tape never moved stands on a grid.
	const position = gid => state.at.get( gid ) ?? [ 20 + gid % 40 * 25, 60 + Math.trunc( gid / 40 ) * 40, .4 ];
	const move = ( gid, dx, dy, dz = 0 ) => {
		const [x, y, z] = position( gid );
		state.at.set( gid, [ x + dx, y + dy, z + dz ] );
	};
	const many = Array.from( { length: 520 }, ( _, i ) => i + 1 );
	// A tape step: what changes, then what the frame must show. "still"
	// returns the previous scene and writes no record; a number is the one
	// gid whose quads alone change; "camera" changes every placed quad;
	// "equal" is a new scene whose records are all written already;
	// "changed" only asks for a new scene.
	/** @typedef {"still" | "changed" | "camera" | "equal" | number} Expect */
	/** @type {( name: string, step: () => unknown, expect: Expect ) => { name: string; step: () => unknown; expect: Expect; }} */
	const frame = ( name, step, expect ) => ({ name, step, expect });
	/** @type {any} */
	let admitted;
	const tape = [
		frame( "first product", () => (admitted = publish()), "changed" ),
		...Array.from( { length: 4 }, () => frame( "nothing changes", () => {}, "still" ) ),
		...Array.from( { length: 3 }, () => frame( "one walker", () => move( 3, 1.5, -.75 ), 3 ) ),
		frame( "the walker within its pixel", () => move( 3, .125, 0 ), "still" ),
		frame( "the camera moves", () => (state.camera += 7.5), "camera" ),
		frame( "the camera moves again", () => (state.camera += 3), "camera" ),
		frame( "still after the camera", () => {}, "still" ),
		frame( "depth only", () => move( 5, 0, 0, .001 ), 5 ),
		frame( "a label leaves the screen", () => state.hidden.add( 7 ), "changed" ),
		frame( "still while it is gone", () => {}, "still" ),
		frame( "it returns where it was", () => state.hidden.delete( 7 ), "changed" ),
		frame( "it returns somewhere else", () => move( 7, -20, 4 ), 7 ),
		frame( "an equal new product", () => (admitted = publish()), "equal" ),
		frame( "still after it", () => {}, "still" ),
		frame( "a changed product", () => {
			state.names.set( 2, "Renamed_2" );
			state.highlight = 4;
			admitted = publish();
		}, "changed" ),
		...Array.from( { length: 3 }, () => frame( "damage text rises", () => (state.damage += 1), "changed" ) ),
		frame( "damage text ends", () => (state.damage = 0), "changed" ),
		frame( "still after damage", () => {}, "still" ),
		frame( "pixel scale", () => (state.target = 1600), "still" ),
		frame( "resize", () => {
			state.width = 1024;
			state.height = 768;
			admitted = publish();
		}, "changed" ),
		frame( "still after resize", () => {}, "still" ),
		frame( "storage grows", () => {
			state.gids = many;
			admitted = publish();
		}, "changed" ),
		frame( "still in the crowd", () => {}, "still" ),
		frame( "one walker in the crowd", () => move( 400, -3, 2 ), 400 ),
		frame( "the crowd leaves", () => {
			state.gids = crowd;
			admitted = publish();
		}, "changed" ),
		frame( "still at the end", () => {}, "still" )
	];
	/** @type {any} */
	let previous = null;
	let stills = 0;
	for ( const { name, step, expect } of tape ) {
		step();
		// labelAnchors and the world argument are new objects every frame.
		const anchors = new Map();
		for ( const gid of state.gids ) {
			const [x, y, z] = position( gid );
			if ( !state.hidden.has( gid ) ) {
				anchors.set( gid, [ (x - state.camera) * state.width / 800, y * state.height / 600, z ] );
			}
		}
		const matrix = new Float32Array( 16 );
		[matrix[0], matrix[5], matrix[10], matrix[12], matrix[14], matrix[15]] = [
			.01,
			.01,
			.001,
			-state.camera * .001,
			.5,
			1
		];
		const world = { origin: 1, matrix };
		/** @type {any[]} */
		const live = state.damage ?
			[ 0, 1 ].map( n => ({
				worldAnchor: { regionId: 1, x: 20, y: 0, z: 10 },
				occlusion: "none",
				rect: [ -5 + n * 10, -10 - state.damage * 3, 10, 12 ],
				clip: [ 0, 0, state.width, state.height ],
				uv: [ 0, 0, 1, 1 ],
				texture: "sprite",
				color: [ 1, 1, 1, 1 - state.damage * .2 ]
			}) ) :
			[];
		const sceneA = projection.project( admitted, anchors, world, live ),
			sceneB = projectCharacterLabels( admitted, anchors, world, live );
		assert.deepStrictEqual( sceneA, sceneB, `${name}: the projected scenes differ` );
		a.writes.length = 0;
		const drawsA = retained.prepare( sceneA, state.target ), drawsB = fresh.prepare( sceneB, state.target );
		assert.deepStrictEqual( drawShape( drawsA, a.groups ), drawShape( drawsB, b.groups ), `${name}: draws` );
		for ( const label of [ "ui-instances", "ui-viewport" ] ) {
			const used = label === "ui-instances" ? records( sceneA.quads ).at( -1 ).end * RECORD_FLOATS * 4 : 16;
			assert.deepStrictEqual(
				a.buffers.filter( buffer => buffer.label === label ).at( -1 )?.bytes.subarray( 0, used ),
				b.buffers.filter( buffer => buffer.label === label ).at( -1 )?.bytes.subarray( 0, used ),
				`${name}: ${label} bytes`
			);
		}
		const instanceWrites = a.writes.filter( w => w.label === "ui-instances" );
		if ( expect === "still" ) {
			stills++;
			assert.equal( sceneA, previous, `${name}: the previous scene object comes back` );
			assert.deepStrictEqual( instanceWrites, [], `${name}: a still frame writes no record` );
		} else {
			assert.notEqual( sceneA, previous, `${name}: a changed frame is a new scene` );
		}
		if ( expect === "equal" ) {
			assert.deepStrictEqual( instanceWrites, [], `${name}: equal records are not written` );
		}
		if ( typeof expect === "number" || expect === "camera" ) {
			// Same quad count and order: compare each output quad with last frame's.
			assert.equal( sceneA.quads.length, previous.quads.length );
			const spans = records( sceneA.quads );
			const changed = sceneA.quads.flatMap( ( q, i ) => q === previous.quads[i] ? [] : [ i ] );
			const expected = sceneA.quads.flatMap( ( q, i ) =>
				expect === "camera" ? q.depth !== undefined ? [ i ] : [] : q.color[1] === expect / 1000 ? [ i ] : []
			);
			assert.deepStrictEqual( changed, expected, `${name}: the quads that changed` );
			if ( typeof expect === "number" ) {
				const first = spans[expected[0]].first * RECORD_FLOATS * 4,
					end = spans[expected[expected.length - 1]].end * RECORD_FLOATS * 4;
				assert.ok( instanceWrites.length > 0, `${name}: the walker's records are written` );
				for ( const w of instanceWrites ) {
					assert.ok( w.offset >= first && w.offset + w.bytes <= end, `${name}: a write outside the walker` );
				}
			}
		}
		previous = sceneA;
	}
	assert.ok( stills >= 10, "the tape must hold still frames" );
	assert.equal(
		a.buffers.filter( buffer => buffer.label === "ui-instances" ).length,
		2,
		"the crowd grew the storage"
	);
});
