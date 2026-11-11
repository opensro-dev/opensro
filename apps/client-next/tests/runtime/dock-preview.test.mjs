/*
===========================================================================

dock-preview.test.mjs - the character select dock wears its items' glows

Native 8EADC0 is vtable slot 7 of CPSCharacterSelect::SCharacterInfo and runs
RefreshVisualAttachments 8EAA90, whose ApplyWeaponGlow 8EA960 attaches a rare
item's itemrare.txt effects: a roster character shows its SoX glow.

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
async function load( entry ) {
	return import( sourceFileUrl( entry ).href );
}
const { createDockPreview } = await load( "src/engine/runtime/characters/dock-preview.ts" );
const { createModelEmission } = await load( "src/engine/foundation/animation/model-emission.ts" );
const { createSceneryEmission } = await load( "src/engine/foundation/animation/scenery-emission.ts" );
const { equipmentSocket } = await load( "src/engine/foundation/animation/equipment-sockets.ts" );

// ITEM_CH_SWORD_01_A, ITEM_CH_SWORD_01_A_RARE, ITEM_CH_SWORD_01_B_RARE.
const SWORD = 71, RARE_SWORD = 4014, OTHER_RARE_SWORD = 4015;
const WEAPON_SLOT = 6;
const CHARACTER_ID = 7;
const BODY = "CHAR_CH_MAN_ADVENTURER";
const BODY_GLB = "/assets/char/body-preview.glb";
const SWORD_GLB = "/assets/char/equipment/sword.glb";
const GLOW_BONE = "ai_end";
const GLOW_EFFECT = "system/system_raretype_a_step1.efp";
const SPARK_EFFECT = "system/system_enchantspark_step1.efp";
const OTHER_GLOW_EFFECT = "system/system_raretype_b_step1.efp";
const PROGRAMS = "/assets/effects/programs.json#";

/*
================
dress

A plain sword and two rare swords as the build publishes them for a Chinese
man: the rare ones name their effect in specialGlows.
================
*/
function dress() {
	const record = () => ({
		slot: WEAPON_SLOT,
		armorClass: 0,
		thiefSuit: false,
		visualMask: 1 << WEAPON_SLOT,
		visualPriority: 0,
		model: "res/item/china/weapon/sword_01.bsr",
		source: null,
		bodies: {
			CH_M: {
				glb: SWORD_GLB,
				parts: [ "EQ0" ],
				branches: [ { part: "EQ0", nodes: [ { name: GLOW_BONE } ] } ]
			}
		}
	});
	const glow = effectPath => [ { effectPath, bone: GLOW_BONE, scale: 1.3, offset: [ 0, 0, 0 ], root: false } ];
	return {
		equipment: { [SWORD]: record(), [RARE_SWORD]: record(), [OTHER_RARE_SWORD]: record() },
		// An unarmoured Chinese body wears the default chest and leg garments.
		defaultWear: {
			CH_M_clothes_BA: { glb: "/assets/char/default-chest.glb", parts: [ "BA" ] },
			CH_M_clothes_LA: { glb: "/assets/char/default-legs.glb", parts: [ "LA" ] }
		},
		specialGlows: { [RARE_SWORD]: glow( GLOW_EFFECT ), [OTHER_RARE_SWORD]: glow( OTHER_GLOW_EFFECT ) },
		// Port-only, not native: the Experimental enchant sparks for the
		// rare sword, priced at any plus so the fixture's plus-0 row wears
		// them whenever the option is on.
		enchantSparks: { [RARE_SWORD]: [ { ...glow( SPARK_EFFECT )[0], threshold: 0 } ] }
	};
}

/*
================
createDock

One dock owner over the real emission owners, publishing into frames, with
ready answering for every path the test has not withheld.
================
*/
function createDock( withheld = new Set(), sparksOn = false ) {
	const frames = [], assemblies = [];
	let nextId = -1;
	const allocate = () => nextId--;
	const preview = createDockPreview( {
		renderer: {
			enchantSparks: () => sparksOn,
			setCharacterAssembly( model, base, parts ) {
				assemblies.push( parts );
			},
			setCharacterActors( actors ) {
				frames.push( actors );
			},
			scenery: () => null
		},
		resources: {
			plan() {},
			ready: path => !withheld.has( path ),
			duration: () => 0,
			retainWanted() {}
		},
		scenery: createSceneryEmission( allocate ),
		modelEmission: createModelEmission( allocate ),
		lizardGid: allocate()
	} );
	const published = {
		catalog: new Map( [ [ 1, {
			codename: BODY,
			previewGlb: BODY_GLB,
			previewClips: [ "stand" ]
		} ] ] ),
		dress: dress(),
		itemIds: new Map(),
		manifest: 1
	};
	const output = { previewReady: false, dockReady: false, displayed: new Map(), failure: null, cameraTarget: null };
	return {
		output,
		assemblies,
		frame( seconds, refItemId ) {
			const dock = [ {
				id: CHARACTER_ID,
				name: "Test",
				deletePending: false,
				bodyShapeByte: 0,
				volumeIndex: 0,
				visualLoadout: {
					modelCodename: BODY,
					items: [ { refObjId: refItemId, plus: 0 } ],
					avatars: [],
					animationSetName: "",
					heightScale: 1,
					volumeScale: 1
				}
			} ];
			preview.step( { seconds, dock, preview: null, lizard: false }, output, published );
			assert.equal( output.failure, null );
			return frames.at( -1 );
		}
	};
}

/*
================
glowsOf
================
*/
function glowsOf( actors ) {
	return actors.filter( actor => actor.gid !== CHARACTER_ID );
}

test("a roster character wears its rare item's glow on the item's socket", () => {
	const dock = createDock();
	const actors = dock.frame( 10, RARE_SWORD );
	const glows = glowsOf( actors );
	assert.equal( glows.length, 1 );
	assert.equal( glows[0].model, PROGRAMS + encodeURIComponent( GLOW_EFFECT ) );
	assert.equal( glows[0].attachment.gid, CHARACTER_ID );
	assert.equal( glows[0].attachment.bone, equipmentSocket( WEAPON_SLOT, "EQ0", GLOW_BONE ) );
	assert.ok( actors.some( actor => actor.gid === CHARACTER_ID ) );
});

test("a plain item draws no glow", () => {
	assert.deepEqual( glowsOf( createDock().frame( 10, SWORD ) ), [] );
});

test("the dock carries the world's enchant sparks answer, on and off (#3145)", () => {
	// The selection screen must dress a character exactly as the world
	// does: with the Experimental option on, the enhanced glow tier and
	// the enchant sparks; with it off, the native glow rows alone.
	const on = createDock( new Set(), true ), onActors = on.frame( 10, RARE_SWORD );
	const onModels = glowsOf( onActors ).map( actor => actor.model );
	assert.ok(
		onModels.includes( PROGRAMS + encodeURIComponent( SPARK_EFFECT ) ),
		"the spark shows with the option on"
	);
	assert.ok( onModels.includes( PROGRAMS + encodeURIComponent( GLOW_EFFECT ) ), "the native glow stays under it" );
	assert.equal(
		on.assemblies.at( -1 ).find( part => part.equipment?.refObjId === RARE_SWORD )?.equipment.enhanced,
		true,
		"the weapon part carries the enhanced tier with the option on"
	);
	const off = createDock( new Set(), false ), offActors = off.frame( 10, RARE_SWORD );
	const offModels = glowsOf( offActors ).map( actor => actor.model );
	assert.ok( !offModels.includes( PROGRAMS + encodeURIComponent( SPARK_EFFECT ) ), "no spark with the option off" );
	assert.ok(
		offModels.includes( PROGRAMS + encodeURIComponent( GLOW_EFFECT ) ),
		"the native glow shows with the option off"
	);
	assert.equal(
		off.assemblies.at( -1 ).find( part => part.equipment?.refObjId === RARE_SWORD )?.equipment.enhanced,
		false,
		"the weapon part stays on the native tier with the option off"
	);
});

test("the glow keeps its emitter across dock frames", () => {
	const dock = createDock();
	const first = glowsOf( dock.frame( 10, RARE_SWORD ) )[0];
	const later = glowsOf( dock.frame( 12.5, RARE_SWORD ) )[0];
	assert.equal( later.gid, first.gid );
	assert.equal( first.time, 0 );
	assert.equal( later.time, 2.5 );
});

test("another rare item starts its own emitter", () => {
	const dock = createDock();
	const first = glowsOf( dock.frame( 10, RARE_SWORD ) )[0];
	const other = glowsOf( dock.frame( 11, OTHER_RARE_SWORD ) )[0];
	assert.notEqual( other.gid, first.gid );
	assert.equal( other.model, PROGRAMS + encodeURIComponent( OTHER_GLOW_EFFECT ) );
	assert.equal( other.time, 0 );
});

test("an unloaded effect program holds neither the glow nor the roster back", () => {
	const dock = createDock( new Set( [ PROGRAMS + encodeURIComponent( GLOW_EFFECT ) ] ) );
	const actors = dock.frame( 10, RARE_SWORD );
	assert.deepEqual( glowsOf( actors ), [] );
	assert.ok( actors.some( actor => actor.gid === CHARACTER_ID ) );
	assert.equal( dock.output.dockReady, true );
});

test("a glow never enters the displayed roster", () => {
	const dock = createDock();
	dock.frame( 10, RARE_SWORD );
	assert.deepEqual( [ ...dock.output.displayed.keys() ], [ CHARACTER_ID ] );
});
