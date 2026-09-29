/*
===========================================================================

character-resources.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { createLoader } = await load( "src/engine/runtime/assets/worker/loader.ts" );
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterDecoder } = await load( "src/engine/runtime/assets/worker/model/character/character.ts" );
const { decodeNativeClip, bindNativeClip } = await load( "src/engine/foundation/animation/native-clip.ts" );
const { skillMotionRole, skillMotionStateName } = await load( "src/engine/foundation/animation/skill-motion.ts" );

test("skill motion mapping preserves every native action state and authored set identity", () => {
	const registry = readFileSync( "../../scripts/build/char/native/scriptObjAnimationRegistry.ts", "utf8" );
	const rows = [ ...registry.matchAll( /\["(ANI_[A-Z_0-9]+)", (0x[0-9a-f]+)\]/g ) ].filter( ( [, name] ) =>
		/^ANI_(ATTACK[1-9]|READY0[1-4]|WAIT0[1-4]|SKILL_\d+|HAMMER|HANDLOOF|TROW)$/.test( name )
	);
	assert.equal( rows.length, 120 );
	for ( const [, name, id] of rows ) assert.equal( skillMotionStateName( Number( id ) ), name );
	assert.equal( skillMotionStateName( -1 ), undefined );
	assert.throws( () => skillMotionRole( 0xCCCCC0, "ANI_ATTACK1=2|ANI_ATTACK2" ), /animation state/ );
	const sets = [
		"default",
		"sword",
		"spear",
		"bow",
		"cart",
		"onehand_staff",
		"onehand_sword",
		"twohand_sword",
		"dagger",
		"dual_axe",
		"harf",
		"twohand_staff",
		"onehand_staff"
	];
	for ( const [index, set] of sets.entries() ) {
		for ( const [, name, id] of rows ) {
			assert.equal( skillMotionRole( 0xCCCCC0 + 28 * index, name ), `native:${set}:${Number( id )}` );
		}
	}
	assert.throws( () => skillMotionRole( 0xCCCCC1, "ANI_SKILL_1" ), /animation set/ );
	assert.throws( () => skillMotionRole( 0xCCCCC0, "ANI_UNKNOWN" ), /animation state/ );
});

test("published player skill BANs decode with native set/state identity and valid named tracks", () => {
	const manifest = JSON.parse( readFileSync( "../../.generated/client-public/assets/anim/manifest.json", "utf8" ) ),
		paths = new Set();
	assert.equal( skillMotionRole( 0xccccdc, "ANI_SKILL_1" ), "native:sword:26" );
	assert.equal( skillMotionRole( 0xcccd84, "ANI_SKILL_1" ), "native:twohand_sword:26" );
	for (
		const name of [
			"CHAR_CH_MAN_ADVENTURER",
			"CHAR_CH_WOMAN_ADVENTURER",
			"CHAR_EU_MAN_ADVENTURER",
			"CHAR_EU_WOMAN_ADVENTURER"
		]
	) {
		const model = manifest.models[name];
		assert.ok( model, name );
		for ( const states of Object.values( model.animationSets ) ) {
			for ( const row of Object.values( states ) ) {
				if ( paths.has( row.url ) ) continue;
				paths.add( row.url );
				const bytes = readFileSync( "../../.generated/client-public" + row.url ),
					clip = decodeNativeClip( bytes );
				assert.equal( clip.duration, row.durationMs / 1000, row.url );
				assert.ok( clip.channels.length, row.url );
				const bone = clip.channels[0].bone,
					linked = bindNativeClip( clip, "motion", [ {
						name: bone,
						parent: -1,
						translation: [ 0, 0, 0 ],
						rotation: [ 0, 0, 0, 1 ],
						scale: [ 1, 1, 1 ]
					} ] );
				assert.ok( linked.channels.every( c => c.node === 0 ) );
				assert.throws( () => decodeNativeClip( bytes.subarray( 0, bytes.length - 1 ) ), /Truncated/ );
			}
		}
	}
	assert.ok( paths.size > 200, paths.size + " unique motions" );
});
test("GLB basis conversion is inherited by animated roots and preserves triangle orientation", async () => {
	const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
	const positions = Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
		times = Float32Array.of( 0, 1 ),
		values = Float32Array.of( 2, 0, 0, 4, 0, 0 );
	const binary = Buffer.concat( [
		Buffer.from( positions.buffer ),
		Buffer.from( times.buffer ),
		Buffer.from( values.buffer ),
		Buffer.from( Uint16Array.of( 0, 1, 2 ).buffer )
	] );
	const json = {
		nodes: [ { name: "root", mesh: 0 } ],
		meshes: [ { primitives: [ { attributes: { POSITION: 0 }, indices: 3 } ] } ],
		bufferViews: [ { buffer: 0, byteOffset: 0, byteLength: 36 }, { buffer: 0, byteOffset: 36, byteLength: 8 }, {
			buffer: 0,
			byteOffset: 44,
			byteLength: 24
		}, { buffer: 0, byteOffset: 68, byteLength: 6 } ],
		accessors: [
			{ bufferView: 0, componentType: 5126, count: 3, type: "VEC3" },
			{ bufferView: 1, componentType: 5126, count: 2, type: "SCALAR" },
			{ bufferView: 2, componentType: 5126, count: 2, type: "VEC3" },
			{ bufferView: 3, componentType: 5123, count: 3, type: "SCALAR" }
		],
		animations: [ {
			name: "move",
			samplers: [ { input: 1, output: 2 } ],
			channels: [ { sampler: 0, target: { node: 0, path: "translation" } } ]
		} ]
	};
	const model = createCharacterDecoder().decode( {
			json,
			binary: binary.buffer.slice( binary.byteOffset, binary.byteOffset + binary.byteLength )
		} ),
		pose = createCharacterPose( model );
	pose.evaluate( "move", .5, false );
	assert.equal( pose.socket( "root" )[12], -3 );
	assert.deepEqual( [ ...model.primitives[0].geometry.indices ], [ 0, 2, 1 ] );
	assert.equal( model.nodes.length, 2 );
	assert.equal( model.nodes[0].parent, 1 );
	assert.throws(
		() =>
			createCharacterDecoder().decode( {
				json: { nodes: Array.from( { length: 1024 }, () => ({}) ), meshes: [] },
				binary: new ArrayBuffer( 0 )
			} ),
		/budget/
	);
});
const settle = () => new Promise( resolve => setImmediate( resolve ) );
/*
================
png
================
*/
function png( width, height ) {
	const bytes = new Uint8Array( 24 ), v = new DataView( bytes.buffer );
	[ 0x89504e47, 0x0d0a1a0a, 13, 0x49484452, width, height ].forEach( ( n, i ) => v.setUint32( i * 4, n ) );
	return bytes;
}
/*
================
glb
================
*/
function glb( images ) {
	const binary = Buffer.concat( images ),
		json = {
			asset: { version: "2.0" },
			nodes: [],
			meshes: [],
			buffers: [ { byteLength: binary.length } ],
			bufferViews: images.map( ( bytes, i ) => ({
				buffer: 0,
				byteOffset: images.slice( 0, i ).reduce( ( n, b ) => n + b.length, 0 ),
				byteLength: bytes.length
			}) ),
			images: images.map( ( _, bufferView ) => ({ bufferView, mimeType: "image/png" }) )
		};
	const text = Buffer.from( JSON.stringify( json ) ),
		length = Math.ceil( text.length / 4 ) * 4,
		bytes = new Uint8Array( 28 + length + binary.length ),
		v = new DataView( bytes.buffer );
	v.setUint32( 0, 0x46546c67, true );
	v.setUint32( 4, 2, true );
	v.setUint32( 8, bytes.length, true );
	v.setUint32( 12, length, true );
	v.setUint32( 16, 0x4e4f534a, true );
	bytes.fill( 32, 20, 20 + length );
	bytes.set( text, 20 );
	v.setUint32( 20 + length, binary.length, true );
	v.setUint32( 24 + length, 0x004e4942, true );
	bytes.set( binary, 28 + length );
	return bytes;
}
test("standalone and embedded PNGs reject oversized dimensions before bitmap allocation", async t => {
	let bytes, calls = 0;
	const prior = Object.getOwnPropertyDescriptor( globalThis, "createImageBitmap" );
	Object.defineProperty( globalThis, "createImageBitmap", {
		configurable: true,
		writable: true,
		value: async () => {
			calls++;
			throw new Error( "Unexpected allocation" );
		}
	} );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "createImageBitmap", prior );
		else delete globalThis.createImageBitmap;
	} );
	t.mock.method( globalThis, "fetch", async () => new Response( bytes ) );
	for ( const decode of [ "png", "character" ] ) {
		bytes = decode === "png" ? png( 8192, 1 ) : glb( [ png( 8192, 1 ) ] );
		const results = [],
			loader = createLoader( result => {
				if ( result.kind !== "progress" ) results.push( result );
			} );
		loader.receive( { kind: "load", id: 1, url: "http://localhost/model", limit: 1 << 20, decode } );
		await settle();
		assert.equal( results[0].kind, "error" );
		assert.match( results[0].error, /image budget/ );
		loader.dispose();
	}
	assert.equal( calls, 0 );
});
test("embedded image budgets apply to the entire model before its first bitmap", async t => {
	t.mock.method( globalThis, "fetch", async () => new Response( glb( [ png( 4096, 4096 ), png( 4096, 4096 ) ] ) ) );
	const results = [],
		loader = createLoader( result => {
			if ( result.kind !== "progress" ) results.push( result );
		} );
	loader.receive( { kind: "load", id: 1, url: "http://localhost/model", limit: 1 << 20, decode: "character" } );
	await settle();
	assert.equal( results[0].kind, "error" );
	assert.match( results[0].error, /Character images exceed/ );
	loader.dispose();
});
test("failed bitmap transactions close already decoded images", async t => {
	let calls = 0, closed = 0;
	const prior = Object.getOwnPropertyDescriptor( globalThis, "createImageBitmap" );
	Object.defineProperty( globalThis, "createImageBitmap", {
		configurable: true,
		writable: true,
		value: async () => {
			if ( ++calls === 2 ) throw new Error( "Decode failed" );
			return {
				/*
================
close
================
				*/
				close() {
					closed++;
				}
			};
		}
	} );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "createImageBitmap", prior );
		else delete globalThis.createImageBitmap;
	} );
	t.mock.method( globalThis, "fetch", async () => new Response( glb( [ png( 32, 32 ), png( 32, 32 ) ] ) ) );
	const results = [],
		loader = createLoader( result => {
			if ( result.kind !== "progress" ) results.push( result );
		} );
	loader.receive( { kind: "load", id: 1, url: "http://localhost/model", limit: 1 << 20, decode: "character" } );
	await settle();
	assert.match( results[0].error, /Decode failed/ );
	assert.equal( closed, 1 );
	loader.dispose();
});
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
const measured = ( images = 0 ) => ({
	nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
	clips: [],
	images: Array.from( { length: images }, () => ({ bytes: new Uint8Array( 0 ), mime: "image/png" }) ),
	primitives: [ {
		name: "mesh",
		node: 0,
		joints: [ 0 ],
		inverseBind: identity(),
		image: -1,
		geometry: {
			positions: new Float32Array( 3 * 4096 ),
			indices: new Uint32Array( 3 * 2048 ),
			transform: identity()
		}
	} ]
});
const measuredBytes = 3 * 4096 * 4 + 3 * 2048 * 4 + 64;
/*
================
model
================
*/
function model() {
	return {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: [],
		images: [ { bytes: png( 1, 1 ), mime: "image/png" } ],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: 0,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
}

test("equipment reflection images preserve item ownership through assembly, fade and device restoration", () => {
	const renderer = createCharacters(), body = model(), weapon = model();
	weapon.images.push( { bytes: png( 1, 1 ), mime: "image/png" } );
	weapon.primitives[0].name = "part:WA";
	weapon.primitives[0].environmentImage = 1;
	let closed = 0;
	const pixel = name => ({
			name,
			width: 1,
			height: 1,
			/*
================
close
================
			*/
			close() {
				closed++;
			}
		}),
		bodyPixel = pixel( "body" ),
		base = pixel( "axe" ),
		sphere = pixel( "sphere" );
	renderer.model( "body", body, [ bodyPixel ] );
	renderer.model( "weapon", weapon, [ base, sphere ] );
	renderer.assembly( "outfit", "body", [ { model: "weapon", parts: [ "WA" ], covers: [] } ] );
	const uploads = [],
		geometry = {
			/*
================
upload
================
			*/
			upload( data, image, offsets, environment ) {
				uploads.push( { image, environment } );
				return {};
			},
			/*
================
updateBones
================
			*/
			updateBones() {},
			updateInstances: d => d,
			/*
================
release
================
			*/
			release() {}
		},
		images = {
			upload: image => image, /*
================
release
================
			*/
			release() {}
		};
	const actor = {
		gid: 1,
		model: "outfit",
		pose: { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "",
		time: 0,
		loop: true,
		scale: 1
	};
	for ( const opacity of [ 1, .5, 1 ] ) {
		renderer.actors( [ { ...actor, opacity } ] );
		renderer.prepare( geometry, images, 1 );
		const item = uploads.findLast( row => row.image === base );
		assert.equal( item.environment, sphere );
	}
	renderer.invalidate();
	renderer.prepare( geometry, images, 1 );
	assert.equal( uploads.at( -1 ).environment, sphere );
	renderer.dispose( geometry, images );
	assert.equal( closed, 3, "borrowed assembly must not close item images twice" );
});

test("native clip admission updates existing equipment assemblies and retires stale palette owners", () => {
	const renderer = createCharacters(), source = model();
	source.images = [];
	source.clips = [ { name: "stand", duration: 1, channels: [] } ];
	source.primitives[0].image = -1;
	renderer.model( "body", source, [] );
	renderer.assembly( "equipped", "body", [] );
	let releases = 0;
	const geometry = {
			/*
================
upload
================
			*/
			upload( data ) {
				return { bones: data.bones.slice() };
			},
			/*
================
updateBones
================
			*/
			updateBones( draw, bones ) {
				draw.bones = bones.slice();
			},
			/*
================
updateInstances
================
			*/
			updateInstances( draw ) {
				return draw;
			},
			/*
================
release
================
			*/
			release() {
				releases++;
			}
		},
		images = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			/*
================
release
================
			*/
			release() {}
		};
	const row = {
		gid: 1,
		model: "equipped",
		pose: { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	};
	renderer.actors( [ row ] );
	renderer.prepare( geometry, images, 1 );
	const native = {
		duration: 1,
		channels: [ {
			bone: "root",
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 10, 0, 0 )
		} ]
	};
	const bytes = renderer.animation( "body", "native:sword:26", native );
	native.channels[0].values[3] = 999;
	renderer.actors( [ { ...row, clip: "native:sword:26", time: .5, loop: false } ] );
	const draws = renderer.prepare( geometry, images, 1 );
	assert.equal(
		draws[0].bones[12],
		5,
		"existing assembly consumes the new native clip; caller cannot mutate admission"
	);
	assert.equal( releases, 1, "old palette stream is retired before replacing its model identity" );
	assert.equal(
		renderer.animation( "body", "native:sword:26", native ),
		bytes,
		"duplicate role admission is stable"
	);
	renderer.prepare( geometry, images, 1 );
	assert.equal( releases, 1 );
	renderer.dispose( geometry, images );
});
const actor = model => ({
	gid: 1,
	model,
	pose: { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 },
	clip: "",
	time: 0,
	loop: true,
	scale: 1
});
test("shared frame poses remain exact when actors diverge, seek sockets and rejoin", async () => {
	const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
	const source = model();
	source.images = [];
	source.primitives[0].image = -1;
	source.clips = [ {
		name: "move",
		duration: 1,
		channels: [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 12, 6, -3 )
		} ]
	} ];
	const renderer = createCharacters();
	renderer.model( "shared", source, [] );
	const geometry = {
			/*
================
upload
================
			*/
			upload( data ) {
				return { bones: data.bones.slice() };
			},
			/*
================
updateBones
================
			*/
			updateBones( draw, bones ) {
				draw.bones = bones.slice();
			},
			/*
================
updateInstances
================
			*/
			updateInstances( draw ) {
				return draw;
			},
			/*
================
release
================
			*/
			release() {}
		},
		images = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			/*
================
release
================
			*/
			release() {}
		};
	const oracle = [ createCharacterPose( source ), createCharacterPose( source ), createCharacterPose( source ) ];
	for ( let frame = 0; frame < 80; frame++ ) {
		const rows = [ 1, 2, 3 ].map( ( gid, i ) => ({
			...actor( "shared" ),
			gid,
			clip: "move",
			time: frame * .031 + (frame % 4 === 0 ? i * .17 : 0),
			layers: frame % 7 === 0 ?
				[ { clip: "move", time: i * .23, loop: false, weight: .4, lane: "event" }, {
					clip: "move",
					time: frame * .031,
					loop: true,
					weight: 1,
					lane: "timed"
				} ] :
				undefined
		}) );
		if ( frame ) renderer.socket( rows.map( row => ({ ...row, time: row.time + .19 }) ), 2, "root", [ 0, 0, 0 ] );
		renderer.actors( rows );
		const draws = renderer.prepare( geometry, images, frame % 2 ? 1 : 2 );
		assert.equal( draws.length, 1 );
		const expected = new Float32Array( 48 );
		for ( let i = 0; i < 3; i++ ) {
			const row = rows[i];
			oracle[i].evaluate( row.clip, row.time, row.loop, row.layers );
			oracle[i].palette( source.primitives[0], expected, i * 16 );
		}
		assert.deepEqual( draws[0].bones.subarray( 0, expected.length ), expected, `active palette frame ${frame}` );
		if ( frame === 40 ) renderer.invalidate();
	}
	renderer.dispose( geometry, images );
});
test("stationary animation retains instance buffers while motion and recovery publish current transforms", () => {
	const source = model();
	source.images = [];
	source.primitives[0].image = -1;
	source.clips = [ {
		name: "move",
		duration: 1,
		channels: [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 12, 6, -3 )
		} ]
	} ];
	const renderer = createCharacters();
	renderer.model( "animated", source, [] );
	let instanceWrites = 0, boneWrites = 0;
	const geometry = {
		/*
================
upload
================
		*/
		upload( data ) {
			return { instances: data.instances.slice(), bones: data.bones.slice() };
		},
		/*
================
updateBones
================
		*/
		updateBones( draw, bones ) {
			boneWrites++;
			draw.bones = bones.slice();
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, instances ) {
			instanceWrites++;
			draw.instances = instances.slice();
			return draw;
		},
		/*
================
release
================
		*/
		release() {}
	};
	const images = {
		/*
================
upload
================
		*/
		upload() {
			return {};
		},
		/*
================
release
================
		*/
		release() {}
	};
	/*
================
frame
================
	*/
	function frame( time, x = 0 ) {
		renderer.actors( [ { ...actor( "animated" ), clip: "move", time, pose: { ...actor( "" ).pose, x } } ] );
		return renderer.prepare( geometry, images, 1 )[0];
	}
	const initial = frame( 0 ).instances.slice();
	for ( let i = 1; i <= 30; i++ ) {
		const draw = frame( i / 100 );
		assert.deepEqual( draw.instances, initial );
		assert.ok( Math.abs( draw.bones[12] - 12 * i / 100 ) < 1e-6 );
	}
	assert.equal( instanceWrites, 0 );
	assert.equal( boneWrites, 30 );
	assert.equal( frame( .3, 42 ).instances[12], 42 );
	assert.equal( instanceWrites, 1 );
	frame( .3, 42 );
	const settledWrites = boneWrites;
	frame( .3, 42 );
	assert.equal( boneWrites, settledWrites );
	renderer.invalidate();
	const recovered = frame( .3, 42 );
	assert.equal( recovered.instances[12], 42 );
	assert.ok( Math.abs( recovered.bones[12] - 3.6 ) < 1e-6 );
	assert.equal( frame( .4, 43 ).instances[12], 43 );
	assert.equal( instanceWrites, 2 );
	renderer.dispose( geometry, images );
});

test("equipment churn retires assemblies while preserving their base images", () => {
	const characters = createCharacters();
	let closed = 0, uploads = 0, releases = 0, draws = 0, drawReleases = 0;
	const images = {
			/*
================
upload
================
			*/
			upload() {
				uploads++;
				return {};
			},
			/*
================
release
================
			*/
			release() {
				releases++;
			}
		},
		geometry = {
			/*
================
upload
================
			*/
			upload() {
				draws++;
				return {};
			},
			/*
================
updateInstances
================
			*/
			updateInstances( d ) {
				return d;
			},
			/*
================
updateBones
================
			*/
			updateBones() {},
			/*
================
release
================
			*/
			release() {
				drawReleases++;
			}
		};
	characters.model( "base", model(), [ {
		width: 1,
		height: 1,
		/*
================
close
================
		*/
		close() {
			closed++;
		}
	} ] );
	for ( let n = 0; n < 100; n++ ) {
		const id = "outfit" + n;
		characters.assembly( id, "base", [] );
		characters.actors( [ actor( id ) ] );
		characters.retain( [ id ] );
		characters.prepare( geometry, images, 1 );
		assert.equal( closed, 0 );
		assert.equal( draws - drawReleases, 1 );
		assert.equal( uploads - releases, 1 );
	}
	// Derived assemblies must not consume the base-model admission count.
	characters.model( "next", model(), [ {
		width: 1,
		height: 1,
		/*
================
close
================
		*/
		close() {
			closed++;
		}
	} ] );
	characters.actors( [] );
	characters.retain( [] );
	characters.prepare( geometry, images, 1 );
	assert.equal( closed, 2 );
	assert.equal( uploads, releases );
	assert.equal( draws, drawReleases );
	characters.dispose( geometry, images );
	assert.equal( closed, 2 );
});
test("residency follows model membership across warm frames, late admission and reused actor publications", () => {
	const renderer = createCharacters(), closed = [], released = [];
	const geometry = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			updateInstances: d => d,
			/*
================
updateBones
================
			*/
			updateBones() {},
			/*
================
release
================
			*/
			release() {}
		},
		images = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			/*
================
release
================
			*/
			release( draw ) {
				released.push( draw );
			}
		};
	const admit = id =>
		renderer.model( id, model(), [ {
			width: 1,
			height: 1,
			/*
================
close
================
			*/
			close() {
				closed.push( id );
			}
		} ] );
	const rows = [ actor( "a" ) ], keep = [ "b", "b" ];
	admit( "a" );
	admit( "b" );
	renderer.actors( rows );
	renderer.retain( keep );
	const step = () => renderer.prepare( geometry, images, 1 );
	for ( let i = 0; i < 10; i++ ) {
		rows[0].time = i;
		renderer.actors( rows );
		renderer.retain( keep );
		assert.equal( step().length, 1 );
	}
	assert.equal(
		renderer.stats().residencyPasses,
		1,
		"duplicate source/assembly retention does not rebuild the resource graph each frame"
	);
	admit( "unused" );
	step();
	assert.deepEqual( closed, [ "unused" ], "late admission triggers retirement even with unchanged actor membership" );
	rows[0].model = "b";
	renderer.actors( rows );
	step();
	assert.deepEqual( closed, [ "unused", "a" ] );
	renderer.invalidate();
	assert.equal( step().length, 1, "device recovery recreates the remaining model" );
	keep.length = 0;
	renderer.retain( keep );
	step();
	assert.deepEqual( closed, [ "unused", "a" ], "active actor owns its model after explicit retention ends" );
	rows.length = 0;
	renderer.actors( rows );
	assert.equal( step().length, 0 );
	assert.deepEqual( closed, [ "unused", "a", "b" ] );
	renderer.dispose( geometry, images );
	assert.deepEqual( closed, [ "unused", "a", "b" ] );
	assert.equal( released.length, 2 );
});

test("residency accounting is released when models leave and rejected images close once", () => {
	const characters = createCharacters();
	let closed = 0;
	const geometry = {
			/*
================
release
================
			*/
			release() {}
		},
		images = {
			/*
================
release
================
			*/
			release() {}
		};
	for ( let i = 0; i < 100; i++ ) {
		characters.model( String( i ), model(), [ {
			width: 2048,
			height: 1024,
			/*
================
close
================
			*/
			close() {
				closed++;
			}
		} ] );
		characters.retain( [] );
		characters.prepare( geometry, images, 1 );
	}
	assert.equal( closed, 100 );
	assert.throws( () =>
		characters.model( "large", model(), [ {
			width: 8192,
			height: 1,
			/*
================
close
================
			*/
			close() {
				closed++;
			}
		} ] ), /budget/ );
	assert.equal( closed, 101 );
	characters.dispose( geometry, images );
	assert.equal( closed, 101 );
});
test("repeated mesh references share one cumulative decoder expansion budget", () => {
	const count = 20000,
		binary = new ArrayBuffer( count * 3 * 4 ),
		document = {
			binary,
			json: {
				nodes: Array.from( { length: 100 }, () => ({ mesh: 0 }) ),
				meshes: [ { primitives: [ { attributes: { POSITION: 0 }, indices: 1 } ] } ],
				bufferViews: [ { byteLength: binary.byteLength } ],
				accessors: [ { bufferView: 0, componentType: 5126, count, type: "VEC3" }, {
					bufferView: 0,
					componentType: 5125,
					count: 3,
					type: "SCALAR"
				} ]
			}
		};
	assert.throws( () => createCharacterDecoder().decode( document ), /expansion exceeds/ );
});
test("renderer budgets expanded instances across models and frees old batches before replacement", () => {
	const characters = createCharacters(), heavy = model();
	heavy.images = [];
	heavy.nodes = Array.from( { length: 512 }, ( _, i ) => ({ ...heavy.nodes[0], name: String( i ) }) );
	const primitive = {
		...heavy.primitives[0],
		image: -1,
		joints: Array.from( { length: 512 }, ( _, i ) => i ),
		inverseBind: new Float32Array( 512 * 16 )
	};
	for ( let i = 0; i < 512; i++ ) primitive.inverseBind.set( identity(), i * 16 );
	heavy.primitives = Array.from( { length: 128 }, () => primitive );
	characters.model( "a", heavy, [] );
	characters.model( "b", heavy, [] );
	const light = model();
	light.images = [];
	light.primitives[0].image = -1;
	characters.model( "light", light, [] );
	const live = new Set();
	let held = 0, peak = 0;
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			const draw = { bytes: data.bones.byteLength + data.instances.byteLength };
			live.add( draw );
			held += draw.bytes;
			peak = Math.max( peak, held );
			return draw;
		},
		/*
================
release
================
		*/
		release( draw ) {
			assert.ok( live.delete( draw ) );
			held -= draw.bytes;
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw ) {
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {}
	};
	const images = {
		/*
================
upload
================
		*/
		upload() {
			throw new Error( "No textures" );
		},
		/*
================
release
================
		*/
		release() {}
	};
	const population =
		offset => [ ...Array.from( { length: 64 }, ( _, i ) => ({ ...actor( i % 2 ? "a" : "b" ), gid: offset + i }) ), {
			...actor( "light" ),
			gid: offset + 100
		} ];
	characters.actors( population( 1 ) );
	const first = characters.prepare( gpu, images, 1 );
	assert.ok( first.length > 1 );
	assert.ok( characters.stats().deferredActors > 0 );
	assert.ok( characters.stats().renderBytes <= 64 << 20 );
	assert.equal( first.at( -1 ).bytes, 128, "small actor still renders after expensive actors exhaust capacity" );
	const initial = held;
	characters.actors( population( 1000 ) );
	characters.prepare( gpu, images, 1 );
	assert.equal( held, initial );
	assert.equal( peak, initial, "replacement must not overlap old frame storage" );
	characters.actors( [ { ...actor( "light" ), gid: 2 } ] );
	characters.prepare( gpu, images, 1 );
	assert.equal( characters.stats().deferredActors, 0 );
	assert.equal( held, 128 );
	characters.dispose( gpu, images );
	assert.equal( held, 0 );
});

test("cold effects reserve only the active load slot and cannot displace resident actors", async () => {
	const { createCharacterResources } = await load( "src/engine/runtime/characters/resources/resources.ts" );
	const pending = new Map(), requests = [], retentions = [];
	let serial = 0;
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request( url ) {
			requests.push( url );
			pending.set( ++serial, url );
			return serial;
		},
		/*
================
take
================
		*/
		take( id ) {
			if ( !pending.delete( id ) ) return null;
			return { kind: "character", model: measured(), images: [] };
		},
		/*
================
cancel
================
		*/
		cancel( id ) {
			pending.delete( id );
		}
	};
	const owner = createCharacterResources( assets, {
		/*
================
setCharacterModel
================
		*/
		setCharacterModel() {},
		/*
================
retainCharacterModels
================
		*/
		retainCharacterModels( ids ) {
			retentions.push( ids );
		}
	}, "http://localhost" );
	owner.begin( 0 );
	assert.equal( owner.ready( "/assets/body.glb" ), false );
	owner.poll();
	assert.equal( owner.ready( "/assets/body.glb" ), true );
	owner.retainWanted( [] );
	// The incident frame: presentation pre-plans the local player before transient
	// effects compete, the attack asks for two cold effect programs, and neither
	// holds a load slot. None of that may unadmit the decoded body.
	owner.begin( 1 );
	assert.equal( owner.plan( [ "/assets/cold-player.glb" ] ), true );
	assert.equal( owner.ready( "/assets/effects/programs.json#hit-a" ), false );
	assert.equal( owner.ready( "/assets/effects/programs.json#hit-b" ), false );
	assert.equal( owner.ready( "/assets/body.glb" ), true, "a decoded source is never displaced by an undecoded one" );
	assert.equal( owner.ready( "/assets/cold-player.glb" ), false );
	owner.retainWanted( [] );
	assert.ok( retentions.at( -1 ).includes( "/assets/body.glb" ) );
	assert.equal( requests.length, 2, "the pre-planned player owns the only load slot" );
	owner.begin( 2 );
	owner.poll();
	assert.equal( owner.ready( "/assets/cold-player.glb" ), true );
	assert.equal( owner.ready( "/assets/body.glb" ), true );
	assert.equal( owner.ready( "/assets/effects/programs.json#hit-a" ), false );
	owner.retainWanted( [] );
	owner.begin( 3 );
	owner.poll();
	assert.equal( owner.ready( "/assets/effects/programs.json#hit-a" ), true );
	assert.equal( owner.ready( "/assets/body.glb" ), true );
	owner.retainWanted( [] );
	owner.begin( 4 );
	owner.ready( "/assets/body.glb" );
	owner.retainWanted( [] );
	owner.begin( 5 );
	assert.equal(
		owner.ready( "/assets/effects/programs.json#hit-a" ),
		true,
		"a repeated attack reuses decoded resources"
	);
	assert.equal( requests.length, 3 );
	owner.retainWanted( [] );
	owner.reset();
	assert.deepEqual( retentions.at( -1 ), [] );
	assert.equal( owner.ready( "/assets/body.glb" ), false );
	owner.dispose();
	assert.equal( pending.size, 0 );
});

test("one source class bounds both the decode and the reservation an undecoded source may hold", async () => {
	const { createCharacterResources } = await load( "src/engine/runtime/characters/resources/resources.ts" );
	const { CHARACTER_SOURCE_BYTES, CHARACTER_RESIDENT_BYTES } = await load(
		"src/engine/foundation/animation/character-budget.ts"
	);
	assert.throws(
		() =>
			createCharacters().model( "over", measured( 1 ), [ {
				width: 2048,
				height: 2048, /*
================
close
================
				*/
				close() {}
			} ] ),
		/budget/,
		"a source may not decode past the class admission reserves"
	);
	const pending = new Map();
	let serial = 0;
	const image = {
		width: 2048,
		height: 1024, /*
================
close
================
		*/
		close() {}
	};
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request() {
			pending.set( ++serial, true );
			return serial;
		},
		/*
================
take
================
		*/
		take( id ) {
			if ( !pending.delete( id ) ) return null;
			return { kind: "character", model: measured( 1 ), images: [ image ] };
		},
		/*
================
cancel
================
		*/
		cancel( id ) {
			pending.delete( id );
		}
	};
	const owner = createCharacterResources(
		assets,
		{
			/*
================
setCharacterModel
================
			*/
			setCharacterModel() {}, /*
================
retainCharacterModels
================
			*/
			retainCharacterModels() {}
		},
		"http://localhost"
	);
	const cost = image.width * image.height * 5 + measuredBytes,
		resident = Math.floor( (CHARACTER_RESIDENT_BYTES - CHARACTER_SOURCE_BYTES) / cost ) + 1;
	for ( let i = 0; i < resident; i++ ) {
		owner.begin( i );
		owner.ready( "/assets/" + i + ".glb" );
		owner.poll();
		assert.equal( owner.ready( "/assets/" + i + ".glb" ), true );
		owner.retainWanted( [] );
	}
	owner.begin( resident );
	for ( let i = 0; i < resident; i++ ) {
		assert.equal(
			owner.ready( "/assets/" + i + ".glb" ),
			true,
			"measured residency stays admitted under pressure"
		);
	}
	assert.equal( owner.ready( "/assets/cold.glb" ), false );
	owner.begin( resident + 1 );
	assert.equal(
		owner.ready( "/assets/cold.glb" ),
		false,
		"an undecoded source waits for a full class of headroom, it never part-loads"
	);
	for ( let i = 0; i < resident; i++ ) assert.equal( owner.ready( "/assets/" + i + ".glb" ), true );
	owner.dispose();
});

test("no interleaving of frames, loads and effects unadmits a decoded source", async () => {
	const fc = await import( "fast-check" ),
		{ createCharacterResources } = await load( "src/engine/runtime/characters/resources/resources.ts" );
	const bodies = [ "/assets/body-0.glb", "/assets/body-1.glb" ],
		effects = [
			"/assets/effects/programs.json#a",
			"/assets/effects/programs.json#b",
			"/assets/effects/programs.json#c"
		];
	fc.assert(
		fc.property(
			fc.array(
				fc.record( {
					effects: fc.subarray( effects ),
					poll: fc.boolean(),
					plan: fc.subarray( [ ...bodies, "/assets/cold.glb" ] )
				} ),
				{ minLength: 1, maxLength: 40 }
			),
			frames => {
				const pending = new Map();
				let serial = 0;
				const assets = {
					available: () => 4,
					/*
================
request
================
					*/
					request( url ) {
						pending.set( ++serial, url );
						return serial;
					},
					/*
================
take
================
					*/
					take( id ) {
						if ( !pending.delete( id ) ) return null;
						return { kind: "character", model: measured(), images: [] };
					},
					/*
================
cancel
================
					*/
					cancel( id ) {
						pending.delete( id );
					}
				};
				const owner = createCharacterResources(
					assets,
					{
						/*
================
setCharacterModel
================
						*/
						setCharacterModel() {}, /*
================
retainCharacterModels
================
						*/
						retainCharacterModels() {}
					},
					"http://localhost"
				);
				const admitted = new Set();
				for ( const [index, frame] of frames.entries() ) {
					owner.begin( index );
					if ( frame.poll ) owner.poll();
					// Presentation order: priority dependencies, then transient effects, then actors.
					for ( const path of frame.plan ) {
						owner.plan( [ path ] );
					}
					for ( const path of frame.effects ) owner.ready( path );
					for ( const path of bodies ) {
						const ready = owner.ready( path );
						if ( admitted.has( path ) && !ready ) return false;
						if ( ready ) admitted.add( path );
					}
					owner.retainWanted( [] );
				}
				owner.dispose();
				return true;
			}
		),
		{ numRuns: 200, seed: 20260910 }
	);
});

test("inactive character resource cache remains bounded and evicts least recently used sources", async () => {
	const { createCharacterResources } = await load( "src/engine/runtime/characters/resources/resources.ts" );
	const { CHARACTER_MODELS } = await load( "src/engine/foundation/animation/character-budget.ts" );
	let serial = 0, retained = [];
	const pending = new Set(), requests = [];
	const owner = createCharacterResources( {
		available: () => 4,
		/*
================
request
================
		*/
		request( url ) {
			requests.push( url );
			pending.add( ++serial );
			return serial;
		},
		/*
================
take
================
		*/
		take( id ) {
			if ( !pending.delete( id ) ) return null;
			return { kind: "character", model: { nodes: [], primitives: [], images: [], clips: [] }, images: [] };
		},
		/*
================
cancel
================
		*/
		cancel( id ) {
			pending.delete( id );
		}
	}, {
		/*
================
setCharacterModel
================
		*/
		setCharacterModel() {},
		/*
================
retainCharacterModels
================
		*/
		retainCharacterModels( ids ) {
			retained = ids;
		}
	}, "http://localhost" );
	for ( let i = 0; i < CHARACTER_MODELS; i++ ) {
		owner.begin( i );
		owner.ready( "/assets/" + i + ".glb" );
		owner.poll();
		owner.ready( "/assets/" + i + ".glb" );
		owner.retainWanted( [] );
		assert.ok( new Set( retained ).size <= CHARACTER_MODELS );
	}
	owner.begin( CHARACTER_MODELS );
	assert.equal( owner.ready( "/assets/0.glb" ), true );
	owner.ready( "/assets/new.glb" );
	owner.retainWanted( [] );
	assert.ok( retained.includes( "/assets/0.glb" ) );
	assert.ok( !retained.includes( "/assets/1.glb" ), "oldest unused source leaves before incoming decode" );
	assert.ok( new Set( retained ).size <= CHARACTER_MODELS );
	owner.poll();
	owner.retainWanted( [] );
	owner.dispose();
});

test("actor reordering and duplicate-model count changes do not invalidate model residency", () => {
	const renderer = createCharacters(), closed = [];
	const geometry = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			updateInstances: d => d,
			/*
================
updateBones
================
			*/
			updateBones() {},
			/*
================
release
================
			*/
			release() {}
		},
		images = {
			/*
================
upload
================
			*/
			upload() {
				return {};
			},
			/*
================
release
================
			*/
			release() {}
		};
	for ( const id of [ "a", "b" ] ) {
		renderer.model( id, model(), [ {
			width: 1,
			height: 1,
			/*
================
close
================
			*/
			close() {
				closed.push( id );
			}
		} ] );
	}
	renderer.retain( [] );
	const a = { ...actor( "a" ), gid: 1 }, b = { ...actor( "b" ), gid: 2 }, duplicate = { ...actor( "a" ), gid: 3 };
	const step = rows => {
		renderer.actors( rows );
		return renderer.prepare( geometry, images, 1 );
	};
	step( [ a, b, duplicate ] );
	const initial = renderer.stats().residencyPasses;
	for ( let i = 0; i < 20; i++ ) step( i % 2 ? [ a, b, duplicate ] : [ b, a ] );
	assert.equal(
		renderer.stats().residencyPasses,
		initial,
		"membership is unchanged by admission order or duplicate count"
	);
	assert.deepEqual( closed, [] );
	step( [ a ] );
	assert.equal( renderer.stats().residencyPasses, initial + 1 );
	assert.deepEqual( closed, [ "b" ] );
	renderer.invalidate();
	step( [ a ] );
	assert.deepEqual( closed, [ "b" ] );
	step( [] );
	assert.deepEqual( closed, [ "b", "a" ] );
	renderer.dispose( geometry, images );
});

test("injected character profiling observes pose lifetime without changing palette output", () => {
	const counts = [], stages = [], palettes = [], poseSamples = [];
	const renderer = createCharacters( {
		phases: {
			/*
================
begin
The renderer must pass its observer to real pose materialization.
================
		*/
			begin( model, reason ) {
				poseSamples.push( reason );
				return null;
			}
		}
	} );
	const source = model();
	source.images = [];
	source.primitives[0].image = -1;
	const geometry = {
		/*
================
upload
================
		*/
		upload( data ) {
			palettes.push( [ ...data.bones ] );
			return {};
		},
		updateInstances: draw => draw, /*
================
updateBones
================
		*/
		updateBones( draw, bones ) {
			palettes.push( [ ...bones ] );
		}, /*
================
release
================
		*/
		release() {}
	};
	const images = {
		/*
================
upload
================
		*/
		upload() {
			return {};
		}, /*
================
release
================
		*/
		release() {}
	};
	renderer.model( "body", source, [] );
	renderer.retain( [ "body" ] );
	const probe = {
		/*
================
renderBegin
================
		*/
		renderBegin() {}, /*
================
renderMark
================
		*/
		renderMark() {}, /*
================
characterBegin
================
		*/
		characterBegin() {
			stages.push( "begin" );
		}, /*
================
characterMark
================
		*/
		characterMark( stage ) {
			stages.push( stage );
		}, /*
================
characterCount
================
		*/
		characterCount( name, value = 1 ) {
			counts.push( [ name, value ] );
		}
	};
	try {
		renderer.profile( probe );
		renderer.actors( [ actor( "body" ) ] );
		renderer.prepare( geometry, images, 1 );
		const observed = palettes.at( -1 );
		assert.ok( poseSamples.length > 0, "constructor observer reaches pose materialization" );
		assert.deepEqual( stages, [ "begin", "character-plan", "character-poses", "character-upload" ] );
		assert.ok( counts.some( ( [name, value] ) => name === "pose-created" && value === 1 ) );
		renderer.actors( [] );
		renderer.prepare( geometry, images, 1 );
		assert.ok( counts.some( ( [name, value] ) => name === "pose-retired" && value === 1 ) );
		const previous = counts.length;
		renderer.profile( undefined );
		renderer.actors( [ actor( "body" ) ] );
		renderer.prepare( geometry, images, 1 );
		assert.deepEqual( palettes.at( -1 ), observed );
		assert.equal( counts.length, previous );
	} finally {
		renderer.dispose( geometry, images );
	}
});
