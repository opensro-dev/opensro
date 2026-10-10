import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { gunzipSync } from "node:zlib";

import {
	findDefaultAnimationState,
	pickDefaultSetSoundEvents,
	pickDefaultSetStateClip,
	pickSetSoundEvents,
	pickSetTrackEvents
} from "../../build/char/animationUtils.mjs";
import {
	normalizeAssetPath,
	normalizePublicAssetPath,
	normalizePublicPath,
	publicPathToFile,
	toPublicImagePath,
	toPublicPath
} from "../../build/shared/assetPaths.mjs";
import { publishBytesAtomically, publishFileFromTemp } from "../../build/shared/atomicPublish.mjs";
import { mapWithConcurrency } from "../../build/shared/asyncUtils.mjs";
import { typedArrayToBase64 } from "../../build/shared/binaryUtils.mjs";
import { compressGzipSync, DEFAULT_GZIP_LEVEL } from "../../build/shared/compressionUtils.mjs";
import { exportDataAsset } from "../../build/shared/dataAssetExport.mjs";
import { archiveGeneratedArtifact } from "../../build/artifacts/generatedArtifactArchive.mjs";
import { isMainScript, listFiles, pathExists } from "../../build/shared/fsUtils.mjs";
import { sha256Hex } from "../../build/shared/hash.mjs";
import {
	BinaryReader,
	readCountedString,
	readJmxSignature,
	readUInt32Array
} from "../../build/shared/jmxBinaryReader.mjs";
import { readJsonOrNullSync, readJsonOrUndefined, writeJsonIfChanged } from "../../build/shared/jsonOut.mjs";
import { quatMultiply, quatRotateVector } from "../../build/shared/math3d.mjs";
import { extractResinfoSection, parseResinfoSummary } from "../../build/shared/resinfoParser.mjs";
import {
	listTextDataShardNames,
	listTextDataShardNamesSync,
	iterateTextDataLines,
	readTextDataLinesSync,
	readTextDataRowsSync,
	splitTextDataRow
} from "../../build/shared/textDataIo.mjs";
import { normalizeRegionId, regionIdFromSectorCoordinates } from "../../build/world/paths.mjs";
import { readDungeonInfoRows } from "../../build/world/assets/dungeonInfo.mjs";

test("maintained generated-artifact lifecycle source is visible to version control", () => {
	const repoRoot = path.resolve( import.meta.dirname, "../../.." );
	for (
		const relative of [
			"scripts/build/artifacts/README.md",
			"scripts/build/artifacts/generatedArtifactArchive.mjs"
		]
	) {
		const result = spawnSync( "git", [ "check-ignore", "-v", "--", relative ], {
			cwd: repoRoot,
			encoding: "utf8"
		} );
		assert.equal(
			result.status,
			1,
			`${relative} is maintained source but is ignored by ${result.stdout.trim() || result.stderr.trim()}`
		);
	}
});

test("shared textdata helpers preserve row bytes and shard matcher semantics", async ( t ) => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-textdata-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );

	const tablePath = path.join( root, "skilldata_5000.txt" );
	const source = "\ufeff// heading\r\n   // indented comment\r\n\r\n\t1\tVALUE  \r\n";
	await writeFile( tablePath, Buffer.from( source, "utf16le" ) );
	await writeFile( path.join( root, "skilldata.txt" ), "manifest" );
	await writeFile( path.join( root, "itemdata_5000.txt" ), "item" );

	assert.deepEqual( readTextDataLinesSync( tablePath ), [ "\t1\tVALUE  " ] );
	assert.deepEqual( readTextDataRowsSync( tablePath ), [ [ "", "1", "VALUE  " ] ] );
	assert.deepEqual( [ ...iterateTextDataLines( " // note\n  value  \n", { trim: true } ) ], [ "value" ] );
	assert.deepEqual( splitTextDataRow( "1\t\t3" ), [ "1", "", "3" ] );
	assert.deepEqual(
		listTextDataShardNamesSync( root, /^skilldata_\d+\.txt$/i ).sort(),
		[ "skilldata_5000.txt" ]
	);
	assert.deepEqual(
		(await listTextDataShardNames( root, /^itemdata.*\.txt$/i )).sort(),
		[ "itemdata_5000.txt" ]
	);
});

test("shared JSON, hash, existence, and atomic publish contracts", async ( t ) => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-build-io-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );

	const jsonPath = path.join( root, "nested", "value.json" );
	assert.equal( await writeJsonIfChanged( jsonPath, { value: 1 } ), true );
	const firstMtime = (await stat( jsonPath )).mtimeMs;
	assert.equal( await writeJsonIfChanged( jsonPath, { value: 1 } ), false );
	assert.equal( (await stat( jsonPath )).mtimeMs, firstMtime );
	assert.equal( await readFile( jsonPath, "utf8" ), '{"value":1}' );
	assert.deepEqual( await readJsonOrUndefined( jsonPath ), { value: 1 } );
	assert.deepEqual( readJsonOrNullSync( jsonPath ), { value: 1 } );
	assert.equal( await readJsonOrUndefined( path.join( root, "missing.json" ) ), undefined );
	assert.equal( readJsonOrNullSync( path.join( root, "missing.json" ) ), null );

	const targetPath = path.join( root, "published.bin" );
	const unchangedTempPath = path.join( root, "unchanged.tmp" );
	await writeFile( targetPath, "old" );
	await writeFile( unchangedTempPath, "old" );
	assert.equal( await publishFileFromTemp( unchangedTempPath, targetPath ), false );
	assert.equal( await pathExists( unchangedTempPath ), false );

	assert.equal( await publishBytesAtomically( targetPath, Buffer.from( "new" ) ), true );
	assert.equal( await readFile( targetPath, "utf8" ), "new" );
	assert.equal( await pathExists( targetPath ), true );
	assert.equal( sha256Hex( "abc" ), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" );

	const exported = exportDataAsset( {
		publicRoot: root,
		outputFileName: "catalog.json",
		value: { rows: 3 }
	} );
	assert.equal( exported.changed, true );
	assert.deepEqual( JSON.parse( await readFile( exported.outPath, "utf8" ) ), { rows: 3 } );
	assert.equal(
		exportDataAsset( {
			publicRoot: root,
			outputFileName: "catalog.json",
			value: { rows: 3 }
		} ).changed,
		false
	);
});

test("superseded generated artifacts move to a recoverable archive with provenance", async ( t ) => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-generated-archive-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const scopeRoot = path.join( root, "public" );
	const archiveRoot = path.join( root, "archive" );
	const sourcePath = path.join( scopeRoot, "assets", "packs", "stale.bin" );
	await mkdir( path.dirname( sourcePath ), { recursive: true } );
	await writeFile( sourcePath, "recoverable-pack-bytes" );

	const archived = await archiveGeneratedArtifact( sourcePath, {
		scopeRoot,
		archiveRoot,
		reason: "superseded test pack"
	} );
	assert.ok( archived );
	assert.equal( await pathExists( sourcePath ), false );
	assert.equal( await readFile( archived.destination, "utf8" ), "recoverable-pack-bytes" );
	const record = JSON.parse( await readFile( `${archived.destination}.archive.json`, "utf8" ) );
	assert.equal( record.reason, "superseded-test-pack" );
	assert.equal( record.originalPath, "assets/packs/stale.bin" );
	assert.equal( record.bytes, 22 );
});

test("shared asset paths and region ids preserve each caller's formatting contract", () => {
	const root = path.resolve( "C:/public-fixture" );
	const filePath = path.join( root, "assets", "images", "icon.png" );
	assert.equal( normalizeAssetPath( " Res\\Mob//Wolf.BSR " ), "res/mob/wolf.bsr" );
	assert.equal( normalizePublicPath( "\\assets\\images//icon.png" ), "/assets/images/icon.png" );
	assert.equal( normalizePublicAssetPath( "assets/images/icon.png" ), "/assets/images/icon.png" );
	assert.throws( () => normalizePublicAssetPath( "outside/icon.png" ), /only accept public \/assets/ );
	assert.equal( toPublicPath( filePath, root ), "/assets/images/icon.png" );
	assert.equal( toPublicPath( filePath, root, { leadingSlash: false } ), "assets/images/icon.png" );
	assert.equal( publicPathToFile( "/assets/images/icon.png", root ), filePath );
	assert.equal(
		toPublicImagePath( "Media_extracted", "Interface\\Guild\\Mark.DDJ" ),
		"/assets/images/Media_extracted/interface/guild/mark.png"
	);
	assert.equal(
		toPublicImagePath( "Map_extracted/tile2d", "Tile.DDJ.PNG", { replaceExtension: false } ),
		"/assets/images/Map_extracted/tile2d/tile.ddj.png"
	);
	assert.equal( normalizeRegionId( "0X694E" ), "0x694e" );
	assert.equal( normalizeRegionId( 0x1694e ), "0x694e" );
	assert.equal( regionIdFromSectorCoordinates( 0x4e, 0x69 ), "0x694e" );
	assert.throws( () => normalizeRegionId( "region-694e" ), /Invalid 16-bit region id/ );
});

test("shared concurrency, recursive walking, binary, and dungeon readers retain contracts", async ( t ) => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-shared-utils-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );

	await writeFile( path.join( root, "top.JSON" ), "{}" );
	await writeFile( path.join( root, "ignored.txt" ), "x" );
	await writeFile( path.join( root, "dungeoninfo.txt" ), '// comment\r\n17 "dungeon\\alpha.dof"\r\n' );
	const nested = path.join( root, "nested" );
	await writeFile( path.join( root, "invalid.json" ), "{" );
	await mkdir( nested );
	await writeFile( path.join( nested, "lower.json" ), "{}" );

	assert.deepEqual(
		(await listFiles( root, { extensions: [ ".json" ], sort: true } )).map( ( filePath ) =>
			path.relative( root, filePath ).replaceAll( "\\", "/" )
		),
		[ "invalid.json", "nested/lower.json", "top.JSON" ]
	);
	assert.deepEqual(
		await listFiles( path.join( root, "absent" ), { missing: "empty" } ),
		[]
	);
	assert.equal( await readJsonOrUndefined( path.join( root, "invalid.json" ) ), undefined );
	assert.deepEqual( await readDungeonInfoRows( path.join( root, "dungeoninfo.txt" ) ), [
		{ regionId: 17, dofName: "dungeon\\alpha.dof" }
	] );

	let active = 0;
	let maximumActive = 0;
	const mapped = await mapWithConcurrency( [ 3, 1, 2, 0 ], 2, async ( value, index ) => {
		active += 1;
		maximumActive = Math.max( maximumActive, active );
		await new Promise( ( resolve ) => setTimeout( resolve, value ) );
		active -= 1;
		return `${index}:${value}`;
	} );
	assert.deepEqual( mapped, [ "0:3", "1:1", "2:2", "3:0" ] );
	assert.equal( maximumActive, 2 );

	const backing = new Uint8Array( [ 9, 1, 2, 3, 8 ] );
	assert.equal( typedArrayToBase64( backing.subarray( 1, 4 ) ), Buffer.from( [ 1, 2, 3 ] ).toString( "base64" ) );
	assert.throws( () => typedArrayToBase64( new ArrayBuffer( 2 ) ), /ArrayBuffer view/ );
});

test("shared JMX readers expose one counted-string contract and validate bounds", () => {
	const signature = Buffer.from( "JMXVRES 0109", "latin1" );
	assert.equal( readJmxSignature( signature, "JMXVRES 0109", "fixture.bsr" ), "JMXVRES 0109" );

	const counted = Buffer.alloc( 11 );
	counted.writeUInt32LE( 3, 0 );
	counted.write( "abc", 4, "latin1" );
	counted.writeUInt32LE( 42, 7 );
	assert.deepEqual( readCountedString( counted, 0, "fixture.bsr" ), {
		value: "abc",
		byteLength: 3,
		byteOffset: 0,
		nextOffset: 7
	} );
	assert.deepEqual( readUInt32Array( counted, 7, 1, "fixture.bsr" ), [ 42 ] );
	assert.throws( () => readCountedString( counted.subarray( 0, 5 ), 0, "short.bsr" ), /past 5/ );

	const primitives = Buffer.alloc( 15 );
	primitives.writeUInt8( 7, 0 );
	primitives.writeInt16LE( -2, 1 );
	primitives.writeUInt32LE( 3, 3 );
	primitives.write( "abc", 7, "latin1" );
	primitives.writeFloatLE( 1.5, 10 );
	const reader = new BinaryReader( primitives, "reader.bin" );
	assert.equal( reader.u8(), 7 );
	assert.equal( reader.i16(), -2 );
	assert.equal( reader.str(), "abc" );
	assert.equal( reader.f32(), 1.5 );
	assert.equal( reader.offset, primitives.length - 1 );
	assert.throws( () => reader.u16(), /truncated u16/ );
});

test("JMX names decode as CP949 and asset paths fold ASCII case only", () => {
	// seventop rock material as stored in its BMT: CP949 C1DF BDC9 BAAE = 중심벽.
	// Latin-1 plus Unicode lowercase used to publish it as `áß½éº®.png`.
	const name = Buffer.concat( [
		Buffer.from( "Seventop\\", "latin1" ),
		Buffer.from( [ 0xc1, 0xdf, 0xbd, 0xc9, 0xba, 0xae ] ),
		Buffer.from( ".DDJ", "latin1" )
	] );
	const counted = Buffer.alloc( 4 + name.length );
	counted.writeUInt32LE( name.length, 0 );
	name.copy( counted, 4 );
	const { value } = readCountedString( counted, 0, "seventop.bmt" );
	assert.equal( value, "Seventop\\중심벽.DDJ" );
	assert.equal( new BinaryReader( counted, "seventop.bmt" ).str(), value );
	assert.equal( normalizeAssetPath( value ), "seventop/중심벽.ddj" );
	assert.equal( normalizeAssetPath( "ÁÉ/ABC" ), "ÁÉ/abc" );
	assert.equal(
		toPublicImagePath( "Media_extracted", "Icon\\ÁB.DDJ" ),
		"/assets/images/Media_extracted/icon/Áb.png"
	);
});

test("shared CLI, compression, and quaternion primitives preserve behavior", () => {
	const entryPath = path.resolve( "fixture-entry.mjs" );
	assert.equal( isMainScript( pathToFileURL( entryPath ).href, entryPath ), true );
	assert.equal( isMainScript( pathToFileURL( entryPath ).href, undefined ), false );

	assert.equal( DEFAULT_GZIP_LEVEL, 9 );
	const bytes = Buffer.from( "shared compression contract" );
	assert.deepEqual( gunzipSync( compressGzipSync( bytes ) ), bytes );

	assert.deepEqual( quatMultiply( [ 0, 0, 0, 1 ], [ 1, 2, 3, 4 ] ), [ 1, 2, 3, 4 ] );
	const halfSqrt = Math.SQRT1_2;
	const rotated = quatRotateVector( [ 0, 0, halfSqrt, halfSqrt ], [ 1, 0, 0 ] );
	assert.ok( Math.abs( rotated[0] ) < 1e-12 );
	assert.ok( Math.abs( rotated[1] - 1 ) < 1e-12 );
	assert.ok( Math.abs( rotated[2] ) < 1e-12 );
});

test("shared animation and ResInfo helpers retain authored selection/order", () => {
	const bsr = {
		animationSets: [ { name: "DEFAULT", states: [ { stateId: 7, animationPath: "run.ban" } ] } ],
		soundModifiers: [
			{
				kind: 1,
				stateId: 7,
				animationSetName: "DEFAULT",
				entries: [
					{
						animationName: "default",
						tracks: [
							{ triggerFrame: 125, cueName: "SND_RUN1" },
							{ triggerFrame: 250, cueName: "VOC_BREATH" }
						]
					}
				]
			}
		]
	};
	assert.equal( findDefaultAnimationState( bsr, 7 )?.animationPath, "run.ban" );
	assert.equal( pickDefaultSetStateClip( bsr, 7 ), "run.ban" );
	assert.deepEqual( pickDefaultSetSoundEvents( bsr, 7, "snd_run1" ), [
		{ cursorMs: 125, cue: "SND_RUN1" }
	] );

	const lines = [
		"Section = Create,0,0,",
		"{",
		"Root:CIFFrame",
		"{",
		"Child:CIFButton",
		"}",
		"}",
		"Section = Other,0,0,",
		"{",
		"OtherRoot:CIFStatic",
		"}"
	];
	assert.deepEqual( extractResinfoSection( lines, "Create" ), lines.slice( 0, 7 ) );
	assert.deepEqual( parseResinfoSummary( lines.join( "\n" ) ), {
		sections: [ "Create", "Other" ],
		controlTypes: [ "CIFButton", "CIFFrame", "CIFStatic" ],
		rootControlTypes: [
			{ section: "Create", name: "Root", type: "CIFFrame" },
			{ section: "Other", name: "OtherRoot", type: "CIFStatic" }
		],
		sectionControlTypes: [
			{ section: "Create", name: "Root", type: "CIFFrame" },
			{ section: "Create", name: "Child", type: "CIFButton" },
			{ section: "Other", name: "OtherRoot", type: "CIFStatic" }
		]
	} );
});

test("a weapon set without its own ModDataSound plays the default set's (BindTrackMarkers AE1770)", () => {
	const sound = ( animationSetName, stateId, tracks ) => ({
		kind: 1,
		stateId,
		animationSetName,
		entries: [ { animationName: "default", tracks } ]
	});
	const bsr = {
		soundModifiers: [
			sound( "default", 7, [ { triggerFrame: 289, cueName: "snd_run1" }, {
				triggerFrame: 619,
				cueName: "snd_run1"
			} ] ),
			sound( "bow", 7, [ { triggerFrame: 100, cueName: "snd_bow_run" } ] ),
			sound( "cart", 7, [] )
		]
	};
	const footsteps = [ { cursorMs: 289, cue: "snd_run1" }, { cursorMs: 619, cue: "snd_run1" } ];
	// twohand_staff authors no run sound: the default set's footsteps apply.
	assert.deepEqual( pickSetSoundEvents( bsr, "twohand_staff", 7 ), footsteps );
	assert.deepEqual( pickSetSoundEvents( bsr, "TwoHand_Staff", 7 ), footsteps );
	// A set's own ModDataSound wins, including an authored silent one.
	assert.deepEqual( pickSetSoundEvents( bsr, "bow", 7 ), [ { cursorMs: 100, cue: "snd_bow_run" } ] );
	assert.deepEqual( pickSetSoundEvents( bsr, "cart", 7 ), [] );
	// No sound anywhere for the state stays silent.
	assert.deepEqual( pickSetSoundEvents( bsr, "twohand_staff", 1 ), [] );
	assert.deepEqual( pickSetSoundEvents( { soundModifiers: undefined }, "bow", 7 ), [] );
});

test("a set state without foot contacts takes the default state's", () => {
	const contacts = [ { cursorMs: 293, eventCode: 2, param0: 0, param1: 1 } ];
	const bsr = {
		animationSets: [ {
			name: "default",
			states: [ { stateId: 7, animationPath: "run.ban", trackEvents: contacts } ]
		} ]
	};
	assert.deepEqual( pickSetTrackEvents( bsr, { stateId: 7, trackEvents: [] }, 7 ), contacts );
	assert.deepEqual(
		pickSetTrackEvents( bsr, { stateId: 7, trackEvents: [ { cursorMs: 0, eventCode: 0 } ] }, 7 ),
		contacts
	);
	const own = [ { cursorMs: 291, eventCode: 2, param0: 0, param1: 1 } ];
	assert.deepEqual( pickSetTrackEvents( bsr, { stateId: 7, trackEvents: own }, 7 ), own );
	assert.deepEqual( pickSetTrackEvents( bsr, { stateId: 1, trackEvents: [] }, 1 ), [] );
});
