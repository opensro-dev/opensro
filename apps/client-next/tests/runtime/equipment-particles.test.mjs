/*
===========================================================================

equipment-particles.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetJsonSync, readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
import {
	equipmentAmbientParticles,
	equipmentParticleCatalog,
	publishEquipmentParticleMetadata
} from "../../../../scripts/build/char/equipmentParticles.mjs";
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { appendEquipmentSockets, equipmentSocket, validateEquipmentBranches } = await load(
	"src/engine/foundation/animation/equipment-sockets.ts"
);
const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
const { assembleEquipmentAppearance } = await load( "src/engine/foundation/animation/equipment-appearance.ts" );
const { createModelEmission } = await load( "src/engine/foundation/animation/model-emission.ts" );
const { createCharacterDecoder } = await load( "src/engine/runtime/assets/worker/model/character/character.ts" );
const node = ( name, parent, t = [ 0, 0, 0 ], q = [ 0, 0, 0, 1 ] ) => ({
	name,
	parent,
	translation: t,
	rotation: q,
	scale: [ 1, 1, 1 ]
});
test("private marker namespaces preserve both handles and cancel bind rotation, not current motion", () => {
	const q = Math.SQRT1_2,
		body = [
			node( "root", -1 ),
			node( "handR", 0, [ 10, 0, 0 ], [ 0, 0, q, q ] ),
			node( "handL", 0, [ -10, 0, 0 ] ),
			node( "ai_end", 0, [ 999, 999, 999 ] )
		];
	const branches = [ {
		part: "R",
		attachBone: "handR",
		nodes: [ node( "Bone01", -1 ), node( "ai_end", 0, [ 2, 0, 0 ] ) ]
	}, { part: "L", attachBone: "handL", nodes: [ node( "ai_end", -1, [ 3, 0, 0 ] ) ] } ];
	validateEquipmentBranches( branches );
	const nodes = appendEquipmentSockets( body, branches, 6 );
	const model = {
			nodes,
			primitives: [],
			images: [],
			clips: [ { name: "stand", duration: 1, channels: [] }, {
				name: "turn",
				duration: 1,
				channels: [ {
					node: 1,
					path: "rotation",
					interpolation: "LINEAR",
					times: Float32Array.of( 0 ),
					values: Float32Array.of( 0, 0, 1, 0 )
				} ]
			} ]
		},
		pose = createCharacterPose( model );
	pose.evaluate( "stand", 0 );
	const pos = name => [ ...pose.socket( name ).slice( 12, 15 ) ];
	assert.deepEqual( pos( equipmentSocket( 6, "L", "ai_end" ) ), [ -7, 0, 0 ] );
	assert.ok( Math.abs( pos( equipmentSocket( 6, "R", "ai_end" ) )[0] - 12 ) < 1e-5 );
	pose.evaluate( "turn", 0 );
	const p = pos( equipmentSocket( 6, "R", "ai_end" ) );
	assert.ok( Math.abs( p[0] - 10 ) < 1e-5 && Math.abs( p[1] - 2 ) < 1e-5 );
	assert.equal( pose.socket( "equipment:6:R:missing" ), null );
	assert.deepEqual( pos( "ai_end" ), [ 999, 999, 999 ] );
	assert.throws(
		() => appendEquipmentSockets( body, [ { ...branches[0], attachBone: "missing" } ], 6 ),
		/wearer socket/
	);
	assert.throws(
		() => validateEquipmentBranches( [ { ...branches[0], nodes: [ node( "cycle", 0 ) ] } ] ),
		/private socket/
	);
});
test("equipment particle lifetime survives cold resources and retires on replace, hide, despawn and reset", () => {
	let id = -1;
	const owner = createModelEmission( () => id-- ),
		actor = {
			gid: 1,
			model: "equipped",
			pose: { regionId: 257, x: 1, y: 2, z: 3, yaw: 0 },
			scale: 2,
			modelAnimation: { selected: { override: true } }
		};
	const particles = [ "R", "L" ].map( part => ({
			source: "equipment",
			effectPath: "system/rare.efp",
			bone: equipmentSocket( 6, part, "ai_end" ),
			scale: 1.3,
			offset: [ 0, 0, 0 ],
			root: false
		})
		),
		holders = [ { actor, particles } ];
	assert.deepEqual( owner.step( holders, 0, () => false, 20 ), [] );
	const first = owner.step( holders, 1, () => true, 20 );
	assert.equal( first.length, 2 );
	assert.notEqual( first[0].gid, first[1].gid );
	assert.equal( first[0].attachment.modelScale, 1.3 );
	assert.equal( first[0].deferredParticle.lodHidden, false );
	const next = owner.step( holders, 2, () => true, 20 );
	assert.equal( next[0].gid, first[0].gid );
	assert.equal( next[0].time, 1 );
	const changed = owner.step( [ { actor: { ...actor, model: "replacement" }, particles } ], 3, () => true, 20 );
	assert.notEqual( changed[0].gid, first[0].gid );
	assert.deepEqual( owner.step( [], 4, () => true, 20 ), [] );
	const restored = owner.step( holders, 5, () => true, 20 );
	assert.notEqual( restored[0].gid, changed[0].gid );
	owner.reset();
	assert.notEqual( owner.step( holders, 6, () => true, 20 )[0].gid, restored[0].gid );
});
test("every authored active special-state item has reachable effects and real private sockets on every admitted body", () => {
	const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot ),
		programs = readPublishedAssetJsonSync( "/assets/effects/programs.json", publicRoot ),
		catalog = equipmentParticleCatalog(),
		cache = new Map();
	let cases = 0;
	assert.deepEqual( roster.dress.specialGlows, catalog );
	for ( const [id, rows] of Object.entries( catalog ) ) {
		for ( const [prefix, entry] of Object.entries( roster.dress.equipment[id].bodies ) ) {
			if ( !entry ) {
				continue;
			}
			validateEquipmentBranches( entry.branches );
			const [race, sex] = prefix.split( "_" ),
				body = roster.models.find( r =>
					r.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` )
				);
			let model = cache.get( body.glb );
			if ( !model ) {
				const bytes = readPublishedAssetBytesSync( body.glb, publicRoot ),
					end = 20 + bytes.readUInt32LE( 12 ),
					bin = bytes.subarray( end + 8 );
				model = createCharacterDecoder().decode( {
					json: JSON.parse( bytes.subarray( 20, end ) ),
					binary: bin.buffer.slice( bin.byteOffset, bin.byteOffset + bin.length )
				} );
				cache.set( body.glb, model );
			}
			const nodes = appendEquipmentSockets( model.nodes, entry.branches, roster.dress.equipment[id].slot ),
				pose = createCharacterPose( { ...model, nodes } );
			pose.evaluate( "stand", 0 );
			for ( const row of rows ) {
				assert.ok( programs.effects[row.effectPath], row.effectPath );
				for ( const part of entry.parts ) {
					const matrix = pose.socket( equipmentSocket( roster.dress.equipment[id].slot, part, row.bone ) );
					assert.ok( matrix && [ ...matrix ].every( Number.isFinite ), id + ":" + prefix + ":" + part );
					cases++;
				}
			}
		}
	}
	assert.ok( cases > 900 );
});

/*
================
shoulder garment ambient particles

ITEM_CH_W_CLOTHES_01_SA_A_RARE (5562): clothes_01_sa.bsr links its private
skeleton under Bip01 Neck1 and authors item_equip_shoulder_white.efp on two
of its bones. The garment slot is 2.
================
*/
const SHOULDER_ITEM = 5562, SHOULDER_SLOT = 2;
function shoulderDress() {
	return {
		equipment: {
			[SHOULDER_ITEM]: {
				slot: SHOULDER_SLOT,
				armorClass: 1,
				thiefSuit: false,
				visualMask: 1 << SHOULDER_SLOT,
				visualPriority: 150,
				model: "res/item/china/woman_item/clothes_01_sa.bsr",
				source: null,
				bodies: { CH_W: { glb: "/assets/char/equipment/shoulder.glb", parts: [ "EQ0" ] } }
			}
		}
	};
}
test("a shoulder garment's ambient set rides its private branch onto the wearer", async () => {
	const dress = shoulderDress();
	await publishEquipmentParticleMetadata( dress );
	const branches = dress.equipment[SHOULDER_ITEM].bodies.CH_W.branches;
	validateEquipmentBranches( branches );
	assert.equal( branches.length, 1 );
	assert.equal( branches[0].attachBone, "Bip01 Neck1" );
	const assembly = assembleEquipmentAppearance( {
		resource: { codename: "CHAR_CH_WOMAN_ADVENTURER" },
		dress,
		equipment: [ { slot: SHOULDER_SLOT, refObjId: SHOULDER_ITEM, plus: 0 } ],
		avatars: [],
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: true
	} );
	assert.deepEqual( assembly.particles.map( p => [ p.effectPath, p.bone, p.source ] ).sort(), [
		[ "system/item_equip_shoulder_white.efp", equipmentSocket( SHOULDER_SLOT, "EQ0", "Bone04" ), "equipment" ],
		[ "system/item_equip_shoulder_white.efp", equipmentSocket( SHOULDER_SLOT, "EQ0", "Bone06" ), "equipment" ]
	] );
	const pose = createCharacterPose( {
		nodes: appendEquipmentSockets( [ node( "Bip01 Neck1", -1, [ 0, 150, 0 ] ) ], branches, SHOULDER_SLOT ),
		clips: [],
		primitives: [],
		images: []
	} );
	pose.evaluate( "", 0 );
	for ( const particle of assembly.particles ) {
		const matrix = pose.socket( particle.bone );
		assert.ok( matrix && [ ...matrix ].every( Number.isFinite ), particle.bone );
	}
	// An ambient bone outside the branch is a broken catalog, not a body bone.
	assert.throws(
		() =>
			validateEquipmentBranches( [ {
				...branches[0],
				nodes: branches[0].nodes.filter( n => n.name !== "Bone04" )
			} ] ),
		/ambient particle socket/
	);
});
/*
================
decodePublished

One decoded published character GLB, cached per test.
================
*/
function decodePublished( glb, cache ) {
	let model = cache.get( glb );
	if ( !model ) {
		const bytes = readPublishedAssetBytesSync( glb, publicRoot ),
			end = 20 + bytes.readUInt32LE( 12 ),
			bin = bytes.subarray( end + 8 );
		model = createCharacterDecoder().decode( {
			json: JSON.parse( bytes.subarray( 20, end ) ),
			binary: bin.buffer.slice( bin.byteOffset, bin.byteOffset + bin.length )
		} );
		cache.set( glb, model );
	}
	return model;
}

/*
================
publishedBody

The decoded player body GLB of one prefix ("CH_M").
================
*/
function publishedBody( roster, prefix, cache ) {
	const [race, sex] = prefix.split( "_" ),
		body = roster.models.find( r => r.codename.startsWith( `CHAR_${race}_${sex === "M" ? "MAN" : "WOMAN"}_` ) );
	return decodePublished( body.glb, cache );
}

/*
================
every equipment ambient set

A rigid garment (CH) carries its set on its private branch, and every
socket resolves on the real published body. An animated garment (EU) is
its own auxiliary handle: the set rides that handle, whose "stand" track
turns the crystal bones the particles sit on.
================
*/
test("every equipment ambient set is published with its garment and reachable programs", () => {
	const roster = readPublishedAssetJsonSync( "/assets/char/roster.json", publicRoot ),
		programs = readPublishedAssetJsonSync( "/assets/effects/programs.json", publicRoot ),
		ambient = equipmentAmbientParticles(),
		models = new Map();
	let branches = 0, animated = 0;
	assert.equal( ambient.size, 36 );
	for ( const [source, modifiers] of ambient ) {
		const bones = modifiers.flatMap( m => m.entries ).map( e => e.boneName );
		for ( const entry of modifiers.flatMap( m => m.entries ) ) {
			const effectPath = entry.effectPath.replaceAll( "\\", "/" ).toLowerCase();
			assert.ok( programs.effects[effectPath], effectPath );
		}
		for ( const record of Object.values( roster.dress.equipment ) ) {
			if ( record.model !== source ) continue;
			for ( const [prefix, entry] of Object.entries( record.bodies ) ) {
				if ( !entry ) continue;
				if ( entry.auxiliary ) {
					assert.equal( entry.branches, undefined, source );
					assert.deepEqual( entry.auxiliary.particleModifiers, modifiers, source );
					const model = decodePublished( entry.auxiliary.glb, models ),
						pose = createCharacterPose( model );
					pose.evaluate( "stand", 0 );
					const start = bones.map( bone => [ ...pose.socket( bone ) ] );
					pose.evaluate( "stand", 0.5 );
					assert.ok(
						bones.every( ( bone, i ) =>
							pose.socket( bone ).some( ( v, j ) => Math.abs( v - start[i][j] ) > 1e-3 )
						),
						source + ": the crystal bones do not move"
					);
					animated++;
					continue;
				}
				validateEquipmentBranches( entry.branches );
				assert.deepEqual( entry.branches.map( b => b.particleModifiers ), [ modifiers ], source );
				const body = publishedBody( roster, prefix, models ),
					nodes = appendEquipmentSockets( body.nodes, entry.branches, record.slot ),
					pose = createCharacterPose( { ...body, nodes } );
				pose.evaluate( "stand", 0 );
				for ( const bone of bones ) {
					const matrix = pose.socket( equipmentSocket( record.slot, "EQ0", bone ) );
					assert.ok( matrix && [ ...matrix ].every( Number.isFinite ), source + ":" + prefix );
				}
				branches++;
			}
		}
	}
	assert.ok( branches >= 18 && animated >= 18, branches + " rigid, " + animated + " animated" );
});
