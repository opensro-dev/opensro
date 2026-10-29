/*
===========================================================================

characterModelAssetContracts.test.mjs - the character and NPC model contracts

Checks the published character, NPC, monster and COS models against their
rosters and native sources: manifest coverage, RefObj identity, model and
animation policy, byte metadata and VAT reuse.

Needs the full asset build and the extracted client data.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import {
	claimResourceOutput,
	removeSupersededResourceOutputs,
	resourceGlbOutput
} from "../../build/char/resourceGlbOutput.mjs";
import {
	bakeCharacterResource,
	expandCharacterInfoCodenames,
	resolveNpcModel
} from "../../build/char/buildNpcModelAssets.mjs";
import { preserveFreshNpcVatReferences } from "../../build/char/buildNpcVatAssets.mjs";
import {
	enabledCosReferences,
	loadFortressStructureRoster,
	loadModelledMobRoster,
	loadSpawnableNpcRoster
} from "../../build/char/npcModelRoster.mjs";
import { loadCharacterDataRows } from "../../build/char/resolveCharRoster.mjs";
import { loadBoothModelRoster } from "../../build/char/boothModelRoster.mjs";
import { buildEffectRecordTable } from "../../build/char/parseSkillEffect.mjs";
import { parseCharacterBsr, parseSkinnedBms } from "../../build/char/formats.mjs";
import { loadDataAsset } from "../../build/shared/jmxAssetIO.mjs";
import { loadQinShiTombCharacterRows, loadQinShiTombSkillRows } from "../../build/shared/qinshiTomb.mjs";
import { sha256Hex } from "../../build/shared/hash.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../lib/publishedAsset.mjs";
import { gameRoot, publicAssetsRoot, publicRoot, retailTextdataRoot } from "../../build/world/paths.mjs";
import { joinedNpcManifest, NPC_MANIFEST_VERSION } from "../../build/shared/npcManifest.mjs";

const textdataDir = retailTextdataRoot;
const publicAssets = publicAssetsRoot;

/*
================
readJson
================
*/
function readJson( filePath ) {
	return readPublishedAssetJsonSync( filePath );
}

/*
================
diskPath
================
*/
function diskPath( publicPath ) {
	return path.join( publicRoot, ...publicPath.replace( /^\/+/, "" ).split( "/" ) );
}

/*
================
writeCountedString
================
*/
function writeCountedString( buffer, offset, value ) {
	const bytes = Buffer.from( value, "latin1" );
	buffer.writeUInt32LE( bytes.length, offset );
	bytes.copy( buffer, offset + 4 );
	return offset + 4 + bytes.length;
}

/*
================
align4
================
*/
function align4( offset ) {
	return (offset + 3) & ~3;
}

/*
================
buildCharacterBsrFixture
================
*/
function buildCharacterBsrFixture() {
	const buffer = Buffer.alloc( 512 );
	const pointers = new Array( 13 ).fill( 0 );
	buffer.write( "JMXVRES 0109", 0, "ascii" );
	writeCountedString( buffer, 0x44, "fixture-character" );

	let offset = 0x60;
	pointers[1] = offset;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	offset = writeCountedString( buffer, offset, "prim/fixture.bms" );

	offset = align4( offset );
	pointers[2] = offset;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	offset = writeCountedString( buffer, offset, "skeleton/fixture.bsk" );
	offset = writeCountedString( buffer, offset, "Bip01 Spine" );

	offset = align4( offset );
	pointers[3] = offset;
	buffer.writeUInt32LE( 0, offset );
	offset += 4;
	buffer.writeUInt32LE( 0, offset );
	offset += 4;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	offset = writeCountedString( buffer, offset, "animation/idle.ban" );

	offset = align4( offset );
	pointers[5] = offset;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	offset = writeCountedString( buffer, offset, "default" );
	buffer.writeUInt32LE( 0, offset );
	offset += 4;

	offset = align4( offset );
	pointers[6] = offset;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	buffer.writeInt32LE( 0, offset );
	offset += 4;
	offset = writeCountedString( buffer, offset, "idle-state" );
	buffer.writeUInt32LE( 1, offset );
	offset += 4;
	buffer.writeUInt32LE( 0x00060000, offset );
	offset += 4;
	offset += 28;
	buffer.writeUInt32LE( 0, offset );
	offset += 4;

	pointers.forEach( ( pointer, index ) => buffer.writeUInt32LE( pointer, 0x0c + index * 4 ) );
	return buffer.subarray( 0, offset );
}

/*
================
assertManifestOutputsAreResourceUnique
================
*/
function assertManifestOutputsAreResourceUnique( manifest ) {
	const ownerByOutput = new Map();
	const entryByResource = new Map();
	for ( const entry of Object.values( manifest.models ) ) {
		if ( !entry.glb || entry.error ) continue;
		const outputKey = entry.glb.toLowerCase();
		const sourceKey = entry.bsr.toLowerCase();
		const priorOwner = ownerByOutput.get( outputKey );
		assert.ok(
			priorOwner === undefined || priorOwner === sourceKey,
			`${entry.glb} aliases ${priorOwner} and ${sourceKey}`
		);
		ownerByOutput.set( outputKey, sourceKey );
		entryByResource.set( sourceKey, entry );
	}
	for ( const entry of entryByResource.values() ) {
		assert.equal( readPublishedAssetBytesSync( entry.glb ).length, entry.bytes, entry.glb );
	}
	return { ownerByOutput, entryByResource };
}

test("native characterInfo compact ranges expand before record lookup", () => {
	assert.deepEqual( expandCharacterInfoCodenames( "NPC_EU_SMITH" ), [ "NPC_EU_SMITH" ] );
	assert.deepEqual(
		expandCharacterInfoCodenames( "MOV_EV_WOLF_A_001~010" ),
		Array.from( { length: 10 }, ( _unused, index ) => `MOV_EV_WOLF_A_${String( index + 1 ).padStart( 3, "0" )}` )
	);
	assert.deepEqual( expandCharacterInfoCodenames( "MOV_INVALID_010~001" ), [] );
});

test("character BSR parser advances through the shared counted-string contract", () => {
	const parsed = parseCharacterBsr( buildCharacterBsrFixture(), "fixture-character.bsr" );

	assert.equal( parsed.name, "fixture-character" );
	assert.deepEqual( parsed.meshPaths, [ "prim/fixture.bms" ] );
	assert.equal( parsed.skeletonPath, "skeleton/fixture.bsk" );
	assert.equal( parsed.skeletonAttachBone, "Bip01 Spine" );
	assert.deepEqual( parsed.animationPaths, [ "animation/idle.ban" ] );
	assert.deepEqual( parsed.animationSets, [ { name: "default", states: [] } ] );
	assert.deepEqual( parsed.soundModifiers, [] );
	assert.equal( parsed.partLink, null );
});

test("zero-length BMS skin sections preserve native rigid geometry", async () => {
	const meshPath = "prim\\mesh\\npc\\china\\chinaetc_gamblingplaceman_stick.bms";
	const mesh = parseSkinnedBms( await loadDataAsset( meshPath ), meshPath );
	assert.equal( mesh.rigid, true );
	assert.equal( mesh.boneNames.length, 0 );
	assert.equal( mesh.vertexCount, 66 );
	assert.equal( mesh.triangleCount, 36 );
});

test("a monster bake publishes every authored state a skill can name (Captain Ivy ATTACK05..08)", async t => {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "ivy-bake-" ) );
	t.after( () => fs.rmSync( dir, { recursive: true, force: true } ) );
	const baked = await bakeCharacterResource( "res/mob/asiam/ivy.bsr", {
		publicPath: "/assets/npc/test/ivy.glb",
		diskPath: path.join( dir, "ivy.glb" )
	}, true );
	// MSKILL_AM_IVY_ATTACK05..07 and SUMMON01..03 name ANI_ATTACK5..8 (183..186).
	for ( const id of [ 183, 184, 185, 186 ] ) {
		const role = `native:default:${id}`;
		assert.equal( baked.animationStates[role]?.stateId, id, role );
		assert.ok( baked.clips.includes( role ), role );
	}
});

test("BSR outputs preserve native hierarchy and reject cross-resource collisions", () => {
	const europe = resourceGlbOutput( "res/mob/europe/wolf.bsr", {
		namespace: "npc",
		publicAssetsRoot: publicAssets
	} );
	const asia = resourceGlbOutput( "res\\mob\\asiam\\wolf.bsr", {
		namespace: "npc",
		publicAssetsRoot: publicAssets
	} );
	assert.equal( europe.publicPath, "/assets/npc/mob/europe/wolf.glb" );
	assert.equal( asia.publicPath, "/assets/npc/mob/asiam/wolf.glb" );
	assert.notEqual( europe.diskPath, asia.diskPath );

	const owners = new Map();
	claimResourceOutput( owners, europe.sourcePath, europe.publicPath );
	claimResourceOutput( owners, europe.sourcePath, europe.publicPath );
	assert.throws(
		() => claimResourceOutput( owners, asia.sourcePath, europe.publicPath ),
		/output collision/
	);
	assert.throws(
		() =>
			resourceGlbOutput( "res/mob/../wolf.bsr", {
				namespace: "npc",
				publicAssetsRoot: publicAssets
			} ),
		/Unsafe BSR resource path/
	);
});

test("generated-output cleanup stays inside its namespace and preserves retained files", ( t ) => {
	const temporaryAssets = fs.mkdtempSync( path.join( os.tmpdir(), "sro-resource-output-" ) );
	t.after( () => fs.rmSync( temporaryAssets, { recursive: true, force: true } ) );
	const removedOutput = resourceGlbOutput( "res/mob/europe/wolf.bsr", {
		namespace: "npc",
		publicAssetsRoot: temporaryAssets
	} );
	const retainedOutput = resourceGlbOutput( "res/mob/asiam/wolf.bsr", {
		namespace: "npc",
		publicAssetsRoot: temporaryAssets
	} );
	for ( const output of [ removedOutput, retainedOutput ] ) {
		fs.mkdirSync( path.dirname( output.diskPath ), { recursive: true } );
		fs.writeFileSync( output.diskPath, output.sourcePath );
	}

	const removed = removeSupersededResourceOutputs( {
		previousPublicPaths: [ removedOutput.publicPath, retainedOutput.publicPath ],
		currentPublicPaths: [ retainedOutput.publicPath ],
		namespace: "npc",
		publicAssetsRoot: temporaryAssets
	} );
	assert.deepEqual( removed, [ removedOutput.publicPath ] );
	assert.equal( fs.existsSync( removedOutput.diskPath ), false );
	assert.equal( fs.existsSync( retainedOutput.diskPath ), true );
	assert.throws(
		() =>
			removeSupersededResourceOutputs( {
				previousPublicPaths: [ "/assets/itemdrop/outside.glb" ],
				currentPublicPaths: [],
				namespace: "npc",
				publicAssetsRoot: temporaryAssets
			} ),
		/outside \/assets\/npc\//
	);
});

test("model-only rebuild preserves only byte-verified VAT enrichments", ( t ) => {
	const temporaryRoot = fs.mkdtempSync( path.join( os.tmpdir(), "sro-npc-vat-preserve-" ) );
	t.after( () => fs.rmSync( temporaryRoot, { recursive: true, force: true } ) );
	const temporaryAssets = path.join( temporaryRoot, "assets" );

	const glb = "/assets/npc/mob/china/test.glb";
	const vatManifest = "/assets/npc/vat/mob/china/test.vat.json";
	const vatBin = "/assets/npc/vat/mob/china/test.vat.bin";
	const glbBytes = Buffer.from( "stable-glb", "utf8" );
	const vatBytes = Buffer.from( "stable-vat", "utf8" );
	for ( const publicPath of [ glb, vatManifest, vatBin ] ) {
		fs.mkdirSync( path.dirname( path.join( temporaryAssets, publicPath.slice( "/assets/".length ) ) ), {
			recursive: true
		} );
	}
	fs.writeFileSync( path.join( temporaryAssets, glb.slice( "/assets/".length ) ), glbBytes );
	fs.writeFileSync( path.join( temporaryAssets, vatBin.slice( "/assets/".length ) ), vatBytes );

	const contract = {
		format: "sro-avatar-vat",
		version: 1,
		compilerVersion: "fixture-compiler",
		clipRoles: [ "stand" ],
		materialMode: "native-object-preview"
	};
	const reference = {
		manifest: vatManifest,
		bin: vatBin,
		bytes: vatBytes.byteLength,
		frames: 2,
		clips: [ "stand" ],
		compilerVersion: contract.compilerVersion,
		materialMode: contract.materialMode
	};
	fs.writeFileSync(
		path.join( temporaryAssets, vatManifest.slice( "/assets/".length ) ),
		JSON.stringify( {
			format: contract.format,
			version: contract.version,
			compilerVersion: contract.compilerVersion,
			settings: {
				materialMode: contract.materialMode,
				clipRoles: contract.clipRoles
			},
			source: {
				glb,
				byteLength: glbBytes.byteLength,
				sha256: sha256Hex( glbBytes )
			},
			bin: {
				path: vatBin,
				byteLength: vatBytes.byteLength,
				sha256: sha256Hex( vatBytes )
			},
			texture: { frameCount: 2 },
			clips: { stand: { from: 0, to: 1 } }
		} )
	);

	const previousManifest = {
		vat: contract,
		models: {
			MOB_BASE: { codename: "MOB_BASE", glb, vat: reference }
		}
	};
	const freshModels = [
		{ codename: "MOB_BASE", glb },
		{ codename: "MOB_CLONE", glb }
	];
	assert.deepEqual(
		preserveFreshNpcVatReferences( freshModels, previousManifest, {
			publicRoot: temporaryRoot
		} ),
		{ contract, preserved: 2, stale: 0 }
	);
	assert.deepEqual( freshModels.map( ( model ) => model.vat ), [ reference, reference ] );

	fs.writeFileSync(
		path.join( temporaryAssets, vatBin.slice( "/assets/".length ) ),
		Buffer.from( "tampered!!", "utf8" )
	);
	const staleModels = [
		{ codename: "MOB_BASE", glb },
		{ codename: "MOB_CLONE", glb }
	];
	assert.deepEqual(
		preserveFreshNpcVatReferences( staleModels, previousManifest, {
			publicRoot: temporaryRoot
		} ),
		{ contract: null, preserved: 0, stale: 2 }
	);
	assert.deepEqual( staleModels.map( ( model ) => model.vat ), [ undefined, undefined ] );
});

test("characterdata identity duplicates fail instead of taking shard order", ( t ) => {
	const temporaryTextdata = fs.mkdtempSync( path.join( os.tmpdir(), "sro-characterdata-" ) );
	t.after( () => fs.rmSync( temporaryTextdata, { recursive: true, force: true } ) );
	fs.writeFileSync(
		path.join( temporaryTextdata, "characterdata_10000.txt" ),
		"1\t7495\tNPC_DUPLICATE\r\n",
		"utf16le"
	);
	fs.writeFileSync(
		path.join( temporaryTextdata, "characterdata_20000.txt" ),
		"1\t9251\tNPC_DUPLICATE\r\n",
		"utf16le"
	);
	assert.throws(
		() => loadCharacterDataRows( temporaryTextdata, { codenamePattern: /^NPC_/ } ),
		/Duplicate characterdata codename NPC_DUPLICATE/
	);
});

test("every characterInfo death model is published with native motions 4 and 36", () => {
	const manifest = joinedNpcManifest( readJson( path.join( publicAssets, "npc", "manifest.json" ) ) );
	const catalog = readJson( path.join( publicAssets, "npc", "animation-catalog.json" ) );
	const entries = Object.values( manifest.models );
	const owners = entries.filter( ( entry ) => entry.deathModel );
	const deaths = entries.filter( ( entry ) => entry.kind === "death" );
	assert.ok( owners.length > 0, "no model carries a characterInfo death model" );
	for ( const owner of owners ) {
		const death = manifest.models[owner.deathModel];
		assert.ok( death, `${owner.codename}: death model ${owner.deathModel} is not published` );
		assert.equal( death.kind, "death", `${owner.deathModel} is not a death resource` );
		assert.ok( death.requiredBy.includes( owner.codename ), `${owner.deathModel} does not list ${owner.codename}` );
	}
	for ( const death of deaths ) {
		assert.ok( !death.error && death.glb, `${death.codename}: ${death.error}` );
		// 8E64F0 plays only motion 0x24 (deathLoop) and 4 (death) on the swapped mesh.
		assert.ok( death.clips.includes( "death" ), `${death.codename} lacks death (4)` );
		assert.ok( death.clips.includes( "deathLoop" ), `${death.codename} lacks deathLoop (36)` );
		const states = catalog.resources[death.bsr]?.animations.flatMap( ( row ) => row.stateIds ) ?? [];
		assert.ok( states.includes( 4 ) && states.includes( 36 ), `${death.codename} catalog states ${states}` );
		for ( const codename of death.requiredBy ) {
			assert.equal( manifest.models[codename]?.deathModel, death.codename, `${codename} -> ${death.codename}` );
		}
	}
	// The ten v1.150 characterInfo death BSRs, plus any death model only
	// the Qin-Shi overlay requires (the whitesnake's, port-only) when the
	// tomb bake is on.
	const tombDeaths = deaths.filter( ( entry ) => {
		const requiredBy = entry.requiredBy ?? [];
		return requiredBy.length > 0 &&
			requiredBy.every( ( codename ) => codename.startsWith( "MOB_TQ_" ) || codename.startsWith( "MOB_QT_" ) );
	} );
	assert.equal( deaths.length, 10 + tombDeaths.length, "the v1.150 characterInfo death BSRs" );
	assert.equal( manifest.models.MOB_KK_PENON_F.deathModel, "res/mob/common/penon_f_die.bsr" );
	assert.equal( manifest.models.MOB_KK_PENON_W.deathModel, "res/mob/common/penon_w_die.bsr" );
});

test("mission NPC, monster and COS manifest exactly covers its rosters", () => {
	const manifest = joinedNpcManifest( readJson( path.join( publicAssets, "npc", "manifest.json" ) ) );
	const rows = loadCharacterDataRows( textdataDir, { codenamePattern: /./, qinshiTombOverlay: true } );
	const npcRoster = loadSpawnableNpcRoster();
	const mobRoster = loadModelledMobRoster();
	const cosRoster = enabledCosReferences( rows );
	const roster = [ ...npcRoster, ...mobRoster, ...cosRoster ];
	const deathRows = Object.values( manifest.models ).filter( ( entry ) => entry.kind === "death" );

	assert.equal( manifest.format, "sro-mission-npc-models" );
	assert.equal( manifest.version, NPC_MANIFEST_VERSION );
	// Fortress structures whose v1.150 BSR ships (the small guard towers do not).
	const structureNames = new Set( loadFortressStructureRoster().map( ( ref ) => ref.codename ) );
	const structureRows = Object.values( manifest.models ).filter( ( entry ) => entry.kind === "structure" );
	for ( const entry of structureRows ) assert.ok( structureNames.has( entry.codename ), entry.codename );
	// Stall booths (#490): every booth model the item table names, and only
	// those (boothModelRoster.mjs).
	const boothRows = Object.values( manifest.models ).filter( ( entry ) => entry.kind === "booth" );
	assert.deepEqual(
		boothRows.map( ( entry ) => entry.bsr ).sort(),
		loadBoothModelRoster( textdataDir ).map( ( ref ) => ref.bsrPath ).sort(),
		"every stall booth model, and only those, is published as kind booth"
	);
	assert.equal(
		manifest.count,
		roster.length + structureRows.length + boothRows.length + 4 + deathRows.length,
		"the rosters, structures and booths plus four packetless CICRide resources and the characterInfo death models"
	);
	assert.equal(
		Object.values( manifest.models ).filter( ( entry ) => entry.kind === "cos" ).length,
		cosRoster.length,
		"every enabled COS reference, and only those, is published as kind cos"
	);
	for ( const ref of roster ) {
		const entry = manifest.models[ref.codename];
		assert.ok( entry, `${ref.codename} is absent from the model manifest` );
		assert.equal( entry.refObjId, ref.refObjId, `${ref.codename} RefObj identity drift` );
	}
	// 174 npcpos types, plus #976's Magic Pang machine and Exchanger Gori
	// (NPC_CH_GACHA_MACHINE 9251, NPC_CH_GACHA_OPERATOR 9252), placed from
	// the later server's nest table.
	assert.equal( npcRoster.length, 176, "the authored CH/EU town roster lost model coverage" );
	for ( const codename of [ "NPC_CH_SMITH", "NPC_EU_SMITH", "NPC_EU_ADVICE3", "STRUCTURE_GATE_PULLEY_JA_01" ] ) {
		assert.ok( npcRoster.some( ( ref ) => ref.codename === codename ), `${codename} left the server roster` );
	}
});

test("a default build plans every Qin-Shi tomb species (#2024)", () => {
	// The shipped failure, one seam deeper: the bake's character rows
	// follow the shard's deployed posture (#2043), but the roster export
	// is a Go child that reads only its env — with SRO_QINSHI_TOMB unset
	// the modelled roster planned none of the tomb monsters, so a
	// default data build still published no models for them. Planning
	// must not depend on the build env: with the flag deleted, every one
	// of the 79 overlay species is on a bake roster and resolves to a
	// model.
	const previousTomb = process.env.SRO_QINSHI_TOMB;
	delete process.env.SRO_QINSHI_TOMB;
	try {
		const overlayNames = loadQinShiTombCharacterRows().rows.map( ( cols ) => cols[2] );
		assert.equal( overlayNames.length, 79, "the committed tomb characterdata overlay" );
		const rows = loadCharacterDataRows( textdataDir, { codenamePattern: /./, qinshiTombOverlay: true } );
		for ( const codename of overlayNames ) {
			assert.ok( rows.has( codename ), `${codename} missing from the bake's character rows` );
		}
		const modelled = loadModelledMobRoster().filter( ( ref ) => overlayNames.includes( ref.codename ) );
		const cos = enabledCosReferences( rows ).filter( ( ref ) => overlayNames.includes( ref.codename ) );
		// 78 species ride the modelled monster roster; the 79th,
		// MOB_QT_TQ_THIEF_01_COS, is a quest companion on the COS roster.
		assert.equal( modelled.length, 78, "modelled tomb species" );
		assert.deepEqual( cos.map( ( ref ) => ref.codename ), [ "MOB_QT_TQ_THIEF_01_COS" ] );
		for ( const ref of modelled ) {
			const model = resolveNpcModel( ref.codename, rows );
			assert.ok( model, `${ref.codename} resolves no model` );
			assert.equal( model.refObjId, ref.refObjId, `${ref.codename} RefObj identity drift` );
			assert.match( model.bsrPath, /\.(?:bsr|cpd)$/, `${ref.codename} model path` );
		}
	} finally {
		if ( previousTomb === undefined ) delete process.env.SRO_QINSHI_TOMB;
		else process.env.SRO_QINSHI_TOMB = previousTomb;
	}
});

test("NPC model outputs, animation policy, and byte metadata are internally consistent", () => {
	const manifest = joinedNpcManifest( readJson( path.join( publicAssets, "npc", "manifest.json" ) ) );
	const catalog = readJson( path.join( publicAssets, "npc", "animation-catalog.json" ) );
	const entries = Object.values( manifest.models );
	const successful = entries.filter( ( entry ) => !entry.error && entry.glb );
	const { entryByResource } = assertManifestOutputsAreResourceUnique( manifest );

	assert.equal( manifest.coveredCount, successful.length );
	// Structure stage models (atstructeffect, 4F78A0) are baked once each but
	// live inside their structure's entry, not as rows of their own.
	const rowOutputs = new Set( successful.map( ( entry ) => entry.glb.toLowerCase() ) );
	const stageOnly = new Set(
		entries.flatMap( ( entry ) =>
			Object.values( entry.structureStages ?? {} ).map( ( stage ) => stage.glb.toLowerCase() )
		)
			.filter( ( glb ) => !rowOutputs.has( glb ) )
	);
	assert.equal( manifest.builtCount, entryByResource.size + stageOnly.size );
	// A stage that is not baked first is a reuse of an existing bake.
	const stageSlots = entries.reduce( ( sum, entry ) => sum + Object.keys( entry.structureStages ?? {} ).length, 0 );
	assert.equal( manifest.reusedCount, successful.length - entryByResource.size + stageSlots - stageOnly.size );
	assert.equal(
		(fs.existsSync( path.join( publicAssets, "npc" ) ) ? fs.readdirSync( path.join( publicAssets, "npc" ) ) : [])
			.filter( ( name ) => name.toLowerCase().endsWith( ".glb" ) ).length,
		0
	);

	for ( const entry of successful.filter( ( value ) => value.kind === "monster" ) ) {
		assert.ok( entry.allowedClips.includes( "stand" ), `${entry.codename} has no stand clip` );
	}
	assert.deepEqual( manifest.models.STRUCTURE_GATE_PULLEY_JA_01.allowedClips, [] );
	assert.equal( manifest.models.STRUCTURE_GATE_PULLEY_JA_01.staticPose, true );
	assert.ok( manifest.models.NPC_EU_SMITH.allowedClips.includes( "stand" ) );
	assert.ok( manifest.models.NPC_EU_ADVICE3.allowedClips.includes( "stand" ) );

	const mangyang = manifest.models.MOB_CH_MANGNYANG;
	assert.deepEqual(
		mangyang.allowedClips,
		[
			"stand",
			"walk",
			"attack1",
			"hit1",
			"death",
			"attack2",
			"run",
			"stand02",
			"hit2",
			"deathLoop",
			"down",
			"downwait",
			"downdamage",
			"wakeup",
			"downdie",
			"idle122"
		],
		"Mangyang runtime policy must expose every authored retail motion role"
	);

	assert.ok( manifest.vat, "animated NPC manifest lost its VAT compiler contract" );
	for ( const entry of successful.filter( ( value ) => value.allowedClips?.length > 0 ) ) {
		assert.ok( entry.vat, `${entry.codename} silently fell back from its generated VAT artifact` );
		const vat = readJson( diskPath( entry.vat.manifest ) );
		assert.equal( vat.source.sha256, sha256Hex( readPublishedAssetBytesSync( entry.glb ) ), entry.codename );
		assert.equal( vat.bin.path, entry.vat.bin, entry.codename );
		assert.equal( readPublishedAssetBytesSync( entry.vat.bin ).length, entry.vat.bytes, entry.codename );
		assert.deepEqual( Object.keys( vat.clips ), entry.vat.clips, entry.codename );
		if ( entry.animationStates?.deathLoop ) {
			assert.ok(
				entry.vat.clips.includes( "deathLoop" ),
				`${entry.codename} declares retail deathLoop but its VAT index omits the canonical role`
			);
			assert.ok( vat.clips.deathLoop, `${entry.codename} VAT body omits deathLoop` );
			assert.equal( vat.clips.deathloop, undefined, `${entry.codename} leaked a lower-cased role key` );
		}
	}

	const europeWolf = manifest.models.MOB_EU_BARUSWOLF;
	const asiaWolf = manifest.models.MOB_AM_WOLF;
	assert.equal( europeWolf.glb, "/assets/npc/mob/europe/wolf.glb" );
	assert.equal( asiaWolf.glb, "/assets/npc/mob/asiam/wolf.glb" );
	assert.notEqual( europeWolf.bytes, asiaWolf.bytes );
	assert.equal( readPublishedAssetBytesSync( europeWolf.glb ).length, europeWolf.bytes );
	assert.equal( readPublishedAssetBytesSync( asiaWolf.glb ).length, asiaWolf.bytes );

	for ( const [resourcePath, resource] of Object.entries( catalog.resources ) ) {
		assert.equal( resource.glb, entryByResource.get( resourcePath )?.glb, resourcePath );
	}
});

test("item-drop and skill-stage BSR outputs share the collision-free path contract", () => {
	for ( const [directory, version] of [ [ "itemdrop", 3 ], [ "skillfx", 2 ] ] ) {
		const manifest = readJson( path.join( publicAssets, directory, "manifest.json" ) );
		assert.equal( manifest.version, version );
		assertManifestOutputsAreResourceUnique( manifest );
		assert.equal(
			(fs.existsSync( path.join( publicAssets, directory ) ) ?
				fs.readdirSync( path.join( publicAssets, directory ) ) :
				[])
				.filter( ( name ) => name.toLowerCase().endsWith( ".glb" ) ).length,
			0
		);
	}
});

test("Qin-Shi Emperor Tomb .cpd compound models and skillaniset2 overlay bake cleanly when SRO_QINSHI_TOMB=1", async () => {
	const previousTomb = process.env.SRO_QINSHI_TOMB;
	const scratchRoot = fs.mkdtempSync( path.join( os.tmpdir(), "sro-tomb-cpd-contract-" ) );
	const skillEffectPath = path.join( textdataDir, "skilleffect.txt" );
	try {
		process.env.SRO_QINSHI_TOMB = "off";
		const offEffectTable = buildEffectRecordTable( textdataDir, skillEffectPath );
		const tombSkillRows = loadQinShiTombSkillRows().rows;
		for ( const cols of tombSkillRows ) {
			assert.equal(
				offEffectTable.table[Number( cols[1] )],
				undefined,
				`SRO_QINSHI_TOMB off leaked Tomb skill ${cols[1]} (${cols[3]})`
			);
		}

		process.env.SRO_QINSHI_TOMB = "1";
		const onEffectTable = buildEffectRecordTable( textdataDir, skillEffectPath );
		for ( const cols of tombSkillRows ) {
			assert.ok(
				onEffectTable.table[Number( cols[1] )] !== undefined,
				`SRO_QINSHI_TOMB=1 missing effect record for ${cols[1]} (${cols[3]})`
			);
		}

		const thiefOut = resourceGlbOutput( "res/mob/qinshi/thief_01.cpd", {
			namespace: "npc",
			publicAssetsRoot: scratchRoot
		} );
		const thiefBaked = await bakeCharacterResource( "res/mob/qinshi/thief_01.cpd", thiefOut, true );
		assert.equal( thiefBaked.glb, "/assets/npc/mob/qinshi/thief_01.glb" );
		assert.ok( fs.existsSync( thiefOut.diskPath ) );
		for ( const role of [ "stand", "walk", "run", "attack1", "death", "native:sword:33", "native:sword:102" ] ) {
			assert.ok( thiefBaked.allowedClips.includes( role ), `thief_01.cpd missing ${role}` );
		}

		const shinmuOut = resourceGlbOutput( "res/char/china/shinmu.cpd", {
			namespace: "npc",
			publicAssetsRoot: scratchRoot
		} );
		const shinmuBaked = await bakeCharacterResource( "res/char/china/shinmu.cpd", shinmuOut, true );
		assert.equal( shinmuBaked.glb, "/assets/npc/char/china/shinmu.glb" );
		assert.ok( fs.existsSync( shinmuOut.diskPath ) );
		for (
			const role of [
				"stand",
				"walk",
				"run",
				"attack1",
				"death",
				"native:default:31",
				"native:spear:35",
				"native:spear:69",
				"native:spear:73",
				"native:spear:75"
			]
		) {
			assert.ok( shinmuBaked.allowedClips.includes( role ), `shinmu.cpd missing ${role}` );
		}
	} finally {
		if ( previousTomb === undefined ) {
			delete process.env.SRO_QINSHI_TOMB;
		} else {
			process.env.SRO_QINSHI_TOMB = previousTomb;
		}
		fs.rmSync( scratchRoot, { recursive: true, force: true } );
	}
});
