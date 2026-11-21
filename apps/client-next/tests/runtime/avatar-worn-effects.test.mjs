/*
===========================================================================

avatar-worn-effects.test.mjs - the later mall avatars' worn auras

Port-only, not native. The later client keeps many mall avatars' particles
outside their BSRs, in skilleffectset rows named after the avatar (BlueSoul
Knight: nine programs on its forearms, calves, thighs, spine, eyes and
neck). The avatar import lists them in its marker; the equipment-particle
build publishes them by item id; a worn avatar draws them on the wearer's
own bones, and a removed one draws none.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { avatarWornEffects } from "../../../../scripts/build/char/equipmentParticles.mjs";

/*
================
load
================
*/
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { assembleEquipmentAppearance } = await load( "src/engine/foundation/animation/equipment-appearance.ts" );
const { validateEquipmentParticles } = await load( "src/engine/foundation/animation/equipment-particles.ts" );

const ARMOR = 40822, CODE = "ITEM_MALL_AVATAR_M_BLUE_SOULKNIGHT";
const FOREARM = { effectPath: "system/avatar_m_bluesoulknight_a.efp", bone: "Bip01 L Forearm", root: false };
const FOOT = { effectPath: "system/avatar_m_mummy_foot.efp", bone: "", root: true };

/*
================
avatarDress

One worn dress avatar as the visuals build publishes it for a Chinese man.
================
*/
function avatarDress( avatarEffects ) {
	return {
		avatarEffects,
		equipment: {
			[ARMOR]: {
				slot: null,
				avatarSlot: 0,
				armorClass: 0,
				thiefSuit: false,
				visualMask: 0,
				visualPriority: 70,
				model: "res/item/avatar/avatar_m_blue_soulknight.bsr",
				source: null,
				bodies: { CH_M: { glb: "/assets/char/equipment/bluesoul.glb", parts: [ "AV0" ] } }
			}
		}
	};
}

/*
================
assemble
================
*/
function assemble( dress, avatars ) {
	return assembleEquipmentAppearance( {
		resource: { codename: "CHAR_CH_MAN_ADVENTURER" },
		dress,
		equipment: [],
		avatars,
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
}

test("a worn later avatar draws its aura on the wearer's bones, and a removed one none", () => {
	const aura = { ...FOREARM, offset: [ 0, 0, 0 ] };
	const dress = avatarDress( { [ARMOR]: [ aura ] } );
	const worn = assemble( dress, [ { refObjId: ARMOR } ] );
	assert.deepEqual( worn.avatarIds, [ ARMOR ] );
	assert.deepEqual( worn.particles, [ aura ] );
	assert.deepEqual( assemble( dress, [] ).particles, [] );
	// A catalog without the table (a v1.150 build) draws nothing extra.
	assert.deepEqual( assemble( avatarDress( undefined ), [ { refObjId: ARMOR } ] ).particles, [] );
});

test("the avatar table admits an origin anchor and the wearer's scale; item tables do not", () => {
	const root = [ { ...FOOT, offset: [ 0, 0, 0 ] } ];
	validateEquipmentParticles( { [ARMOR]: root }, true );
	assert.throws( () => validateEquipmentParticles( { [ARMOR]: root } ), /Invalid equipment particle row/ );
	assert.throws(
		() => validateEquipmentParticles( { [ARMOR]: [ { ...FOREARM, bone: "", offset: [ 0, 0, 0 ] } ] }, true ),
		/Invalid equipment particle row/
	);
});

test("the build publishes an enabled marker's auras by item id and ignores an off one", () => {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "opensro-avatar-effects-" ) );
	try {
		const records = new Map( [ [ ARMOR, { id: ARMOR, code: CODE } ] ] );
		const marker = ( enabled ) =>
			JSON.stringify( {
				enabled,
				effects: {
					[CODE]: [ { ...FOREARM, scale: 1 }, { ...FOOT, scale: 1.2 }, {
						...FOREARM,
						scale: 1,
						offset: [ 1.1, 0.3, 1 ]
					} ]
				}
			} );
		fs.writeFileSync( path.join( dir, ".opensro-mall-avatars-v1696.json" ), marker( true ) );
		assert.deepEqual( avatarWornEffects( records, dir ), {
			[ARMOR]: [
				{ ...FOREARM, offset: [ 0, 0, 0 ] },
				{ ...FOOT, scale: Math.fround( 1.2 ), offset: [ 0, 0, 0 ] },
				// A "#Bone" offset takes the BSR particle Z flip.
				{ ...FOREARM, offset: [ Math.fround( 1.1 ), Math.fround( 0.3 ), Math.fround( -1 ) ] }
			]
		} );
		fs.writeFileSync( path.join( dir, ".opensro-mall-avatars-v1696.json" ), marker( false ) );
		assert.deepEqual( avatarWornEffects( records, dir ), {} );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
});
