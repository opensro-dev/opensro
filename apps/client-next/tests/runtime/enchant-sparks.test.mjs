/*
===========================================================================

enchant-sparks.test.mjs - the Experimental enchant sparks on +8 and higher

Port-only, not native (owner, 2026-10-13; default on, 2026-10-17). v1.150
ships the system_enchant*_a_01.efp programs but never names them, so the
original draws none. With the Experimental row on (the default), a weapon
or shield at +8 or higher wears its class's sparks on the bones and scales
its rare twin authors; with it off (the native-look opt-out), or below +8,
nothing changes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
	equipmentEnchantSparks,
	equipmentParticleCatalog,
	publishEquipmentParticleMetadata
} from "../../../../scripts/build/char/equipmentParticles.mjs";

/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { assembleEquipmentAppearance, ENCHANT_SPARK_PLUS } = await load(
	"src/engine/foundation/animation/equipment-appearance.ts"
);
const { validateEquipmentParticles } = await load( "src/engine/foundation/animation/equipment-particles.ts" );
const { equipmentSocket, validateEquipmentBranches } = await load(
	"src/engine/foundation/animation/equipment-sockets.ts"
);

// ITEM_CH_SWORD_01_A, ITEM_CH_SHIELD_01_A, ITEM_CH_BOW_01_A, ITEM_EU_HARP_01_A,
// ITEM_CH_SWORD_01_A_DEF, ITEM_CH_SWORD_01_A_RARE, ITEM_FORT_FORTRESS_HAMMER_06.
const SWORD = 71, SHIELD = 251, BOW = 215, HARP = 10991, BEGINNER_SWORD = 3632, RARE_SWORD = 4014, HAMMER = 19227;
const WEAPON_SLOT = 6, SHIELD_SLOT = 7;
const SPARK = "system/system_enchant";
// A folder with no later-client tables: the itemrare-twin fallback.
const NO_VSRO = path.join( os.tmpdir(), "opensro-no-vsro-item-glow" );

/*
================
armamentDress

A sword and a shield as the visuals build publishes them for a Chinese
man, before the particle metadata adds their branches and catalogs.
================
*/
function armamentDress() {
	const record = ( slot, model, glb ) => ({
		slot,
		armorClass: 0,
		thiefSuit: false,
		visualMask: 1 << slot,
		visualPriority: 0,
		model,
		source: null,
		bodies: { CH_M: { glb, parts: [ "EQ0" ] } }
	});
	return {
		equipment: {
			[SWORD]: record( WEAPON_SLOT, "res/item/china/weapon/sword_01.bsr", "/assets/char/equipment/sword.glb" ),
			[RARE_SWORD]: record(
				WEAPON_SLOT,
				"res/item/china/weapon/sword_01.bsr",
				"/assets/char/equipment/sword.glb"
			),
			[SHIELD]: record( SHIELD_SLOT, "res/item/china/shield/shield_01.bsr", "/assets/char/equipment/shield.glb" )
		}
	};
}

/*
================
sparksOf

The spark particles of one assembly, as [effect, bone] pairs.
================
*/
function sparksOf( dress, plus, enchantSparks, weapon = SWORD ) {
	const assembly = assembleEquipmentAppearance( {
		resource: { codename: "CHAR_CH_MAN_ADVENTURER" },
		dress,
		equipment: [
			{ slot: WEAPON_SLOT, refObjId: weapon, plus },
			{ slot: SHIELD_SLOT, refObjId: SHIELD, plus }
		],
		avatars: [],
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: true,
		enchantSparks
	} );
	return assembly.particles.filter( p => p.effectPath.startsWith( SPARK ) ).map( p => [ p.effectPath, p.bone ] )
		.sort();
}

test("each weapon class wears its own spark program on its rare twin's bones and scale", () => {
	const sparks = equipmentEnchantSparks( undefined, NO_VSRO ), glows = equipmentParticleCatalog();
	assert.deepEqual( sparks[SWORD], [ {
		effectPath: SPARK + "_a_01.efp",
		bone: "ai_end",
		scale: Math.fround( 1.3 ),
		offset: [ 0, 0, 0 ],
		root: false,
		threshold: 8
	} ] );
	assert.equal( sparks[SHIELD][0].effectPath, SPARK + "shield_a_01.efp" );
	assert.equal( sparks[BOW][0].effectPath, SPARK + "bow_a_01.efp" );
	assert.equal( sparks[HARP][0].effectPath, SPARK + "harp_a_01.efp" );
	// A beginner item borrows its sibling's twin; it shares the model.
	assert.deepEqual( sparks[BEGINNER_SWORD], sparks[SWORD] );
	// A Seal (_RARE) item gets the same spark row alongside its own rare glow, and a siege hammer is never enchanted.
	assert.deepEqual( sparks[RARE_SWORD], sparks[SWORD] );
	assert.ok( glows[RARE_SWORD] );
	assert.equal( sparks[HAMMER], undefined );
	for ( const rows of Object.values( sparks ) ) {
		for ( const row of rows ) {
			assert.match( row.effectPath, /^system\/system_enchant(bow|harp|shield|staff)?_a_01\.efp$/ );
			assert.ok( row.scale > 0 );
		}
	}
});

test("sparks show only with the row on and only at +8 or higher, on real private sockets", async () => {
	const dress = armamentDress();
	await publishEquipmentParticleMetadata( dress, NO_VSRO );
	// The shield gains the private branch its sparks ride on.
	const shieldBranches = dress.equipment[SHIELD].bodies.CH_M.branches;
	validateEquipmentBranches( shieldBranches );
	assert.ok( shieldBranches[0].nodes.some( n => n.name === "Bone01" ) );
	assert.equal( ENCHANT_SPARK_PLUS, 8 );
	const on = [
		[ SPARK + "_a_01.efp", equipmentSocket( WEAPON_SLOT, "EQ0", "ai_end" ) ],
		[ SPARK + "shield_a_01.efp", equipmentSocket( SHIELD_SLOT, "EQ0", "Bone01" ) ]
	].sort();
	assert.deepEqual( sparksOf( dress, 8, true ), on );
	assert.deepEqual( sparksOf( dress, 12, true ), on );
	assert.deepEqual( sparksOf( dress, 7, true ), [] );
	assert.deepEqual( sparksOf( dress, 9, true, RARE_SWORD ), on );
	assert.deepEqual( sparksOf( dress, 7, true, RARE_SWORD ), [] );
	// The native-look opt-out (and an unset renderer flag): the row off draws nothing extra.
	assert.deepEqual( sparksOf( dress, 10, false ), [] );
	assert.deepEqual( sparksOf( dress, 10, undefined ), [] );
	assert.deepEqual( sparksOf( dress, 9, false, RARE_SWORD ), [] );
});

/*
================
official table

Port-only, not native: with the later client's itemoptionefp.txt imported,
each item takes its authored effect, bone, scale and threshold; a row for
an item v1.150 lacks is skipped, and a bone the v1.150 model lacks is left
out at publish rather than failing the build.
================
*/
test("the later client's itemoptionefp rows set each item's effect, bone, scale and threshold", async () => {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "opensro-vsro-glow-" ) );
	try {
		fs.writeFileSync(
			path.join( root, "itemoptionefp.txt" ),
			[
				[ "//Item Code Name", "Particle", "Scale", "Bone", "Offset", "min", "1.3", "100%" ],
				[ "ITEM_CH_SWORD_01_A", "system/system_enchant_b_01.efp", "150", "ai_end", "none", "5", "100", "100" ],
				[
					"ITEM_CH_SHIELD_01_A",
					"system/system_enchantshield_a_01.efp",
					"90",
					"NoSuchBone",
					"none",
					"8",
					"100",
					"100"
				],
				[
					"ITEM_LATER_CLIENT_ONLY",
					"system/system_enchant_a_01.efp",
					"130",
					"ai_end",
					"none",
					"8",
					"100",
					"100"
				]
			].map( fields => fields.join( "\t" ) ).join( "\r\n" )
		);
		const sparks = equipmentEnchantSparks( undefined, root );
		assert.deepEqual( sparks[SWORD], [ {
			effectPath: SPARK + "_b_01.efp",
			bone: "ai_end",
			scale: Math.fround( 1.5 ),
			offset: [ 0, 0, 0 ],
			root: false,
			threshold: 5
		} ] );
		assert.equal( Object.keys( sparks ).length, 2 );
		const dress = armamentDress();
		await publishEquipmentParticleMetadata( dress, root );
		assert.equal( dress.enchantSparks[SHIELD], undefined );
		const sword = [ [ SPARK + "_b_01.efp", equipmentSocket( WEAPON_SLOT, "EQ0", "ai_end" ) ] ];
		assert.deepEqual( sparksOf( dress, 5, true ), sword );
		assert.deepEqual( sparksOf( dress, 4, true ), [] );
		assert.deepEqual( sparksOf( dress, 9, false ), [] );
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
});

test("a short itemoptionefp row fails with the build's error, not a TypeError", () => {
	const root = fs.mkdtempSync( path.join( os.tmpdir(), "opensro-vsro-glow-short-" ) );
	try {
		// A known item, but the row ends after its codename.
		fs.writeFileSync( path.join( root, "itemoptionefp.txt" ), "ITEM_CH_SWORD_01_A" );
		assert.throws(
			() => equipmentEnchantSparks( undefined, root ),
			/Unsupported itemoptionefp row/
		);
	} finally {
		fs.rmSync( root, { recursive: true, force: true } );
	}
});

/*
================
threshold validation

A published spark's threshold is a whole plus 1..255; anything else fails
the catalog at the presentation boundary.
================
*/
test("a spark row's threshold must be a whole plus from 1 to 255", () => {
	const spark = { effectPath: SPARK + "_a_01.efp", bone: "ai_end", scale: 1, offset: [ 0, 0, 0 ], root: false };
	validateEquipmentParticles( { 1: [ spark ] } );
	validateEquipmentParticles( { 1: [ { ...spark, threshold: 8 } ] } );
	for ( const threshold of [ 0, 256, 7.5, "8" ] ) {
		assert.throws( () => validateEquipmentParticles( { 1: [ { ...spark, threshold } ] } ) );
	}
});

/*
================
enhanced flag wiring

Port-only, not native: the Experimental "Enhanced item glow" row state
reaches the renderer as each armament part's equipment.enhanced, which is
what lets characters select the enhanced glow tiers. Pin the part field
itself, not only the spark particles it also gates (#1536).
================
*/
test("the appearance assembly carries the enhanced-glow row state on armament parts", () => {
	const assemblyOf = ( enchantSparks ) =>
		assembleEquipmentAppearance( {
			resource: { codename: "CHAR_CH_MAN_ADVENTURER" },
			dress: armamentDress(),
			equipment: [
				{ slot: WEAPON_SLOT, refObjId: SWORD, plus: 9 },
				{ slot: SHIELD_SLOT, refObjId: SHIELD, plus: 9 }
			],
			avatars: [],
			hwanHair: false,
			mounted: false,
			weaponHidden: false,
			attachmentsHidden: false,
			fortressIndex: -1,
			player: true,
			ownerless: false,
			committedWear: [],
			freezeWear: true,
			enchantSparks
		} );
	for ( const on of [ true, false ] ) {
		const armaments = assemblyOf( on ).parts.filter( p => p.equipment );
		assert.equal( armaments.length, 2 );
		assert.ok( armaments.every( p => p.equipment.enhanced === on ) );
	}
});
