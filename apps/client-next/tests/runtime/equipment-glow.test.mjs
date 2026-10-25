/*
===========================================================================

equipment-glow.test.mjs - tests for the client modules it imports

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
import { equipmentGlowCatalog } from "../../../../scripts/build/char/equipmentGlowMetadata.mjs";
import { readPublishedAssetJsonSync, readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { selectEquipmentGlow, createEquipmentGlowClock, validateEquipmentGlows } = await load(
	"src/engine/foundation/rendering/equipment-glow.ts"
);
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterDecoder } = await load( "src/engine/runtime/assets/worker/model/character/character.ts" );
const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
const row = {
	threshold: 3,
	image: 0,
	uv: [ .005, -.005 ],
	color1: [ 1, 0, 0 ],
	color2: [ 0, 1, 0 ],
	period: 1000,
	gain: 2,
	alphaTest: true
};
test("TypeScript update matches 176 retail x87 machine executions, including the broken HLIL V expression", () => {
	const evidence = JSON.parse( readFileSync( "tests/fixtures/native/attachment-glow-update-20260923.json", "utf8" ) );
	for ( const { row, frames } of evidence.cases ) {
		const clock = createEquipmentGlowClock( row );
		for ( const f of frames ) {
			clock.step( f.delta, f.enabled );
			assert.deepEqual( [ ...clock.color ], f.color.map( c => Math.fround( c / 255 ) ) );
			assert.deepEqual( [ ...clock.uv ], f.uv );
		}
	}
});
test("all enhancement boundaries, stable ties, invalid data and absent table", () => {
	const rows = [ 9, 3, 7, 5, 5 ].map( ( threshold, index ) => ({ ...row, threshold, index }) );
	for ( let plus = 0; plus < 256; plus++ ) {
		const eligible = rows.filter( r => plus > 0 && r.threshold <= plus ),
			best = eligible.length ? Math.max( ...eligible.map( r => r.threshold ) ) : -1;
		assert.equal( selectEquipmentGlow( rows, plus ), rows.find( r => r.threshold === best ) );
	}
	assert.equal( selectEquipmentGlow( undefined, 9 ), undefined );
	assert.throws( () => selectEquipmentGlow( rows, NaN ) );
	assert.throws( () => validateEquipmentGlows( { 1: [ { ...row, image: 1 } ] }, 1 ) );
	assert.throws( () => validateEquipmentGlows( { 1: [ { ...row, period: -1 } ] }, 1 ) );
});
/*
================
enhanced tiers

Port-only, not native: an item that carries the later client's rows draws
them only with the Experimental "Enhanced item glow" row on; off, and for
an item without them, the native v1.150 rows decide exactly as before.
================
*/
test("enhanced rows replace the native tiers only with the row on, and only for items that have them", () => {
	const native = [ 3, 5, 7, 9 ].map( threshold => ({ ...row, threshold, enhanced: false }) );
	const enhanced = [ 3, 5, 6, 7, 8, 9, 10 ].map( threshold => ({ ...row, threshold, enhanced: true }) );
	const both = [ ...native, ...enhanced ];
	for ( let plus = 0; plus <= 12; plus++ ) {
		assert.equal( selectEquipmentGlow( both, plus, false ), selectEquipmentGlow( native, plus ) );
	}
	assert.equal( selectEquipmentGlow( both, 6, true ), enhanced[2] );
	assert.equal( selectEquipmentGlow( both, 6, false ), native[1] );
	assert.equal( selectEquipmentGlow( both, 12, true ), enhanced[6] );
	assert.equal( selectEquipmentGlow( both, 2, true ), undefined );
	// An item the later client does not list keeps its native rows with the row on.
	assert.equal( selectEquipmentGlow( native, 8, true ), native[2] );
	assert.throws( () => validateEquipmentGlows( { 1: [ { ...row, enhanced: "yes" } ] }, 1 ) );
	validateEquipmentGlows( { 1: both }, 1 );
});
test("native glow ping-pong discards overshoot, packs color, scrolls signed UV, and pauses at LOD/opacity", () => {
	const c = createEquipmentGlowClock( row );
	assert.deepEqual( [ ...c.color ], [ 1, 0, 0 ] );
	c.step( 500, true );
	assert.deepEqual( [ ...c.color ], [ Math.fround( 127 / 255 ), Math.fround( 127 / 255 ), 0 ] );
	assert.deepEqual( [ ...c.uv ], [ .125, -.125 ] );
	c.step( 750, true );
	assert.deepEqual( [ ...c.color ], [ 0, 1, 0 ] );
	c.step( 250, true );
	assert.deepEqual( [ ...c.color ], [ Math.fround( 63 / 255 ), Math.fround( 191 / 255 ), 0 ] );
	const before = [ ...c.color, ...c.uv ];
	c.step( 3000, false );
	assert.deepEqual( [ ...c.color, ...c.uv ], before );
	c.step( 3000, true );
	assert.deepEqual( [ ...c.color ], [ 1, 0, 0 ] );
	const fixed = createEquipmentGlowClock( { ...row, period: 0 } );
	fixed.step( 1000, true );
	assert.deepEqual( [ ...fixed.color ], [ 1, 0, 0 ] );
	assert.deepEqual( [ ...fixed.uv ], [ .25, -.25 ] );
});
test("real assembly applies both handles, owns clocks, replaces enhancement and restores original secondary texture", () => {
	const root = { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
		primitive = name => ({
			name,
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			environmentImage: 1,
			geometry: {
				positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: { color: [ 1, 1, 1, 1 ], environmentReflection: true }
			}
		});
	const owner = createCharacters(), images = [ 0, 1 ].map( id => ({ id, width: 1, height: 1, close() {} }) );
	owner.model( "body", {
		nodes: [ root ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: []
	}, [] );
	owner.model( "weapon", {
		nodes: [ root ],
		images: images.map( ( { width, height } ) => ({ width, height }) ),
		clips: [],
		primitives: [ primitive( "part:WA" ), primitive( "part:WL" ) ],
		equipmentGlows: { 1: [ row, { ...row, threshold: 9, gain: 1 } ] }
	}, images );
	for ( const plus of [ 0, 3, 9 ] ) {
		owner.assembly( "plus" + plus, "body", [ {
			model: "weapon",
			parts: [ "WA", "WL" ],
			covers: [],
			equipment: { refObjId: 1, plus }
		} ] );
	}
	let released = 0;
	const gpu = {
		upload( data, image, offsets, secondary ) {
			return { secondary, data };
		},
		release() {
			released++;
		},
		updateInstances: d => d,
		updateBones() {},
		updateMaterialColors() {},
		updateTextureTransform() {},
		updateEquipmentGlow( d, color, uv, gain, alpha, enabled ) {
			d.glow = { color: [ ...color ], uv: [ ...uv ], gain, alpha, enabled };
		}
	};
	const imageOwner = { upload: image => image, release() {} },
		actor = ( model, extra = {} ) => ({
			gid: 1,
			modifierId: 1,
			model,
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
			scale: 1,
			clip: "stand",
			time: 0,
			loop: true,
			...extra
		});
	const step = ( seconds, model, extra ) => {
		owner.actors( [ actor( model, extra ) ] );
		return owner.prepare( gpu, imageOwner, 257, undefined, false, seconds );
	};
	let draws = step( 0, "plus3" );
	assert.equal( draws.length, 2 );
	assert.ok( draws.every( d => d.glow.enabled && d.secondary.id === 0 ) );
	draws = step( .5, "plus3" );
	assert.deepEqual( draws[0].glow.uv, [ .125, -.125 ] );
	assert.deepEqual( draws[1].glow, draws[0].glow );
	draws = step( .75, "plus3", { animationLod: { fraction: .6, crowded: false } } );
	assert.ok( draws.every( d => !d.glow.enabled && d.secondary.id === 1 ) );
	draws = step( 1, "plus3", { opacity: .5 } );
	assert.ok( draws.every( d => !d.glow.enabled && d.secondary.id === 1 ) );
	draws = step( 1.25, "plus9" );
	assert.ok( draws.every( d => d.glow.gain === 1 && d.secondary.id === 0 ) );
	draws = step( 1.5, "plus0" );
	assert.ok( draws.every( d => !d.glow && d.secondary.id === 1 ) );
	owner.actors( [] );
	assert.equal( owner.prepare( gpu, imageOwner, 257, undefined, false, 2 ).length, 0 );
	assert.ok( released >= 10 );
	owner.dispose( gpu, imageOwner );
});
test("the assembly's enhanced flag reaches the glow selection", () => {
	// equipment-appearance.ts puts the Experimental row on the part as
	// equipment.enhanced; characters.ts forwards it into the selection.
	// The foundation seam (selectEquipmentGlow) is pinned above; this
	// observes the flag arriving (a forced-off read at the assembly site
	// leaves every foundation test green and this one red).
	const root = { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
		primitive = name => ({
			name,
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			environmentImage: 1,
			geometry: {
				positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: { color: [ 1, 1, 1, 1 ], environmentReflection: true }
			}
		});
	const owner = createCharacters(), images = [ 0 ].map( id => ({ id, width: 1, height: 1, close() {} }) );
	owner.model( "body", {
		nodes: [ root ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: []
	}, [] );
	owner.model( "weapon", {
		nodes: [ root ],
		images: images.map( ( { width, height } ) => ({ width, height }) ),
		clips: [],
		primitives: [ primitive( "part:WA" ) ],
		equipmentGlows: {
			1: [
				{ ...row, threshold: 5, gain: 1, enhanced: false },
				{ ...row, threshold: 5, gain: 2, enhanced: true },
				{ ...row, threshold: 7, gain: 3, enhanced: true }
			]
		}
	}, images );
	const arm = ( id, equipment ) =>
		owner.assembly( id, "body", [ {
			model: "weapon",
			parts: [ "WA" ],
			covers: [],
			equipment
		} ] );
	arm( "on8", { refObjId: 1, plus: 8, enhanced: true } );
	arm( "on6", { refObjId: 1, plus: 6, enhanced: true } );
	arm( "off8", { refObjId: 1, plus: 8 } );
	arm( "flag8", { refObjId: 1, plus: 8, enhanced: false } );
	const gpu = {
		upload( data, image, offsets, secondary ) {
			return { secondary, data };
		},
		release() {},
		updateInstances: d => d,
		updateBones() {},
		updateMaterialColors() {},
		updateTextureTransform() {},
		updateEquipmentGlow( d, color, uv, gain, alpha, enabled ) {
			d.glow = { gain, enabled };
		}
	};
	const imageOwner = { upload: image => image, release() {} };
	const gain = model => {
		owner.actors( [ {
			gid: 1,
			modifierId: 1,
			model,
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
			scale: 1,
			clip: "stand",
			time: 0,
			loop: true
		} ] );
		const draws = owner.prepare( gpu, imageOwner, 257, undefined, false, 0 );
		assert.equal( draws.length, 1 );
		return draws[0].glow?.gain;
	};
	assert.equal( gain( "on8" ), 3, "the enhanced tiers decide at +8 with the row on" );
	assert.equal( gain( "on6" ), 2, "the enhanced row wins its threshold at +6" );
	assert.equal( gain( "off8" ), 1, "no flag: the native tiers decide" );
	assert.equal( gain( "flag8" ), 1, "flag off: the native tiers decide" );
	owner.dispose( gpu, imageOwner );
});
test("a missing attachment bone stays on the owner root and only a missing owner is omitted", () => {
	const root = { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
		hand = { name: "Bip01", parent: 0, translation: [ 4, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
		primitive = {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			environmentImage: -1,
			geometry: {
				positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: { color: [ 1, 1, 1, 1 ] }
			}
		};
	const owner = createCharacters();
	owner.model( "body", {
		nodes: [ root ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: [ primitive ]
	}, [] );
	owner.model( "mount", {
		nodes: [ root, hand ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: [ primitive ]
	}, [] );
	const pose = { regionId: 257, x: 10, y: 2, z: 3, yaw: 0 },
		body = { gid: 1, model: "body", pose, scale: 1, clip: "stand", time: 0, loop: true };
	const effect = {
		...body,
		gid: 2,
		model: "body",
		attachment: { gid: 1, bone: "Bip01", offset: [ 5, 0, 0 ], basis: "native" }
	};
	const rows = [ body, effect ];
	const placed = owner.matrix( rows, 2 );
	assert.ok( placed );
	assert.ok( placed.some( ( v, i ) => i < 12 ? v !== 0 : v !== 0 || i === 15 ) );
	const rootOnly = owner.matrix( [ body, {
		...effect,
		attachment: { ...effect.attachment, bone: "", root: true, offset: [ 0, 0, 0 ] }
	} ], 2 );
	assert.ok( rootOnly );
	// Offset is applied on the root orientation, not dropped.
	assert.notDeepEqual( [ ...placed ].slice( 12, 15 ), [ ...rootOnly ].slice( 12, 15 ) );
	const mount = { gid: 3, model: "mount", pose: { ...pose, x: 40 }, scale: 1, clip: "stand", time: 0, loop: true };
	const rider = { ...body, mountedOn: 3 };
	const onMount = owner.matrix( [ mount, rider, {
		...effect,
		attachment: { gid: 1, bone: "Bip01", offset: [ 0, 0, 0 ] }
	} ], 2 );
	const direct = owner.matrix( [ mount, {
		...effect,
		gid: 4,
		attachment: { gid: 3, bone: "Bip01", offset: [ 0, 0, 0 ] }
	} ], 4 );
	assert.ok( onMount && direct );
	assert.deepEqual( [ ...onMount ], [ ...direct ] );
	assert.equal(
		owner.matrix( [ { ...effect, attachment: { gid: 9, bone: "Bip01", offset: [ 1, 0, 0 ] } } ], 2 ),
		null
	);
	owner.dispose( {
		upload() {
			return {};
		},
		release() {},
		updateInstances( d ) {
			return d;
		},
		updateBones() {},
		updateMaterialColors() {},
		updateTextureTransform() {},
		updateEquipmentGlow() {}
	}, { upload: image => image, release() {} } );
});
test("published CH/EU enhancement catalog reaches the real model decoder for every admitted item/body", () => {
	const catalog = equipmentGlowCatalog(),
		roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot ),
		models = new Map();
	let cases = 0;
	for ( const [id, item] of Object.entries( roster.dress.equipment ) ) {
		if ( (item.slot === 6 || item.slot === 7) && catalog.has( Number( id ) ) ) {
			for ( const body of Object.values( item.bodies ) ) {
				if ( !body ) {
					continue;
				}
				let m = models.get( body.glb );
				if ( !m ) {
					const bytes = readPublishedAssetBytesSync( body.glb, publicRoot ),
						end = 20 + bytes.readUInt32LE( 12 ),
						binary = bytes.subarray( end + 8 );
					m = createCharacterDecoder().decode( {
						json: JSON.parse( bytes.subarray( 20, end ) ),
						binary: binary.buffer.slice( binary.byteOffset, binary.byteOffset + binary.length )
					} );
					models.set( body.glb, m );
				}
				assert.ok( m.equipmentGlows?.[id], body.glb + " item " + id );
				assert.deepEqual(
					m.equipmentGlows[id].map( ( { image, ...r } ) => r ),
					catalog.get( Number( id ) ).map( ( { texture, ...r } ) => r )
				);
				cases++;
			}
		}
	}
	assert.ok( cases > 1000 );
	assert.ok( models.size > 100 );
});

/*
================
enhanced flag wiring

Port-only, not native: an assembly part's equipment.enhanced (the
Experimental row state, carried by the appearance assembly) is what makes
characters select an item's enhanced glow tiers; without it the native
tiers decide. Pin the flag's arrival at the renderer, not only the
partition logic at the foundation seam (#1536).
================
*/
test("an assembly part's enhanced flag selects the enhanced glow tier, and only then", () => {
	const root = { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
		primitive = name => ({
			name,
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			environmentImage: 1,
			geometry: {
				positions: Float32Array.of( 0, 0, 0, 1, 0, 0, 0, 1, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: { color: [ 1, 1, 1, 1 ], environmentReflection: true }
			}
		});
	const owner = createCharacters(), images = [ 0, 1 ].map( id => ({ id, width: 1, height: 1, close() {} }) );
	owner.model( "body", {
		nodes: [ root ],
		images: [],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		primitives: []
	}, [] );
	owner.model( "weapon", {
		nodes: [ root ],
		images: images.map( ( { width, height } ) => ({ width, height }) ),
		clips: [],
		primitives: [ primitive( "part:WA" ), primitive( "part:WL" ) ],
		equipmentGlows: {
			1: [ { ...row, threshold: 5, gain: 1, enhanced: false }, { ...row, threshold: 6, gain: 2, enhanced: true } ]
		}
	}, images );
	owner.assembly( "glowOn", "body", [ {
		model: "weapon",
		parts: [ "WA", "WL" ],
		covers: [],
		equipment: { refObjId: 1, plus: 6, enhanced: true }
	} ] );
	owner.assembly( "glowOff", "body", [ {
		model: "weapon",
		parts: [ "WA", "WL" ],
		covers: [],
		equipment: { refObjId: 1, plus: 6 }
	} ] );
	const gpu = {
			upload( data, image, offsets, secondary ) {
				return { secondary, data };
			},
			release() {},
			updateInstances: d => d,
			updateBones() {},
			updateMaterialColors() {},
			updateTextureTransform() {},
			updateEquipmentGlow( d, color, uv, gain, alpha, enabled ) {
				d.glow = { color: [ ...color ], uv: [ ...uv ], gain, alpha, enabled };
			}
		},
		imageOwner = { upload: image => image, release() {} },
		step = ( model ) => {
			owner.actors( [ {
				gid: 1,
				modifierId: 1,
				model,
				pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
				scale: 1,
				clip: "stand",
				time: 0,
				loop: true
			} ] );
			return owner.prepare( gpu, imageOwner, 257, undefined, false, 0 );
		};
	// Plus 6 reaches the native tier (5) and the enhanced tier (6) alike;
	// the flag alone decides which one draws.
	assert.ok( step( "glowOn" ).every( d => d.glow.gain === 2 ) );
	assert.ok( step( "glowOff" ).every( d => d.glow.gain === 1 ) );
	owner.dispose( gpu, imageOwner );
});
