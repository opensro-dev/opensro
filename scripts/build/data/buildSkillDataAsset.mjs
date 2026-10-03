/*
===========================================================================

buildSkillDataAsset.mjs - publish the skill data planes, one file per runtime owner

Projects the native skilldata shards and the client characterInfo table
into skillData.json (the HUD's skill catalogue), skillAudioData.json
(skill sound identities) and characterActionData.json (the character
owner's action effects, shadow sizes and appearance references).

===========================================================================
*/
import { loadCharacterDataRows } from "../char/resolveCharRoster.mjs";
import { expandCharacterInfoCodenames, resolveCharacterInfoRows } from "../shared/characterInfo.mjs";
// Publish the skilldata shard tables as PROJECTED raw record rows: the
// native CGlobalDataManager boot loader (sub_722e20 @0x722f78) formats
// "%stextdata\skilldata.txt" and hands the manifest to the table dispatcher
// sub_7f22d0(0xcec870, ..., kind=1, enc=1); each shard line is parsed by
// CSkillData's sub_7f9310 into the +0x694 info block (GetData() =
// sub_7f8560 = record+0x694). The CIFSkill pane's slot plane consumes only
// the columns pinned below. The same native +0x16c owner is also queried by
// CIFBuffViewer for pet-only and non-board timed skills, so the projected
// record set MUST cover every service-enabled CSkillData row; filtering by
// board membership created a parallel partial owner and made those tooltips
// impossible. Values stay RAW strings (tab-joined, in source order);
// the browser bridge (bridge/ui/panes/skillPanePlane.ts) decodes them in exactly
// one place, the buildSkillMasteryDataAsset.mjs precedent.
//
// Pinned column contract (source column index -> native parse offset; the
// table loader consumes column 0 "Service" before sub_7f9310 runs, so parse
// token N is file column N+1):
//   col  1 -> +0x694 skill id        (sub_7f1090 slot payload / by-id map key)
//   col  2 -> +0x698 group id        (the "slot->dwGroupID == dwGroupID"
//                                     assert @0x7f111a, globaldatamanager.cpp)
//   col  7 -> +0x6f4 basic level     (GetData()+0x60: slot level digits,
//                                     sub_7efff0 level-1 base lookup)
//   col  8 -> +0x6f5 basic activity  (GetData()+0x61: capture gate @0x58911d)
//   col 34 -> +0x738 req mastery 1   (GetData()+0xa4: the row-population key,
//                                     sub_7f1320 @0x7f13a2)
//   col 36 -> +0x740 req mastery lv1 (GetData()+0xac: the >0x5a insert skip
//                                     @0x7f1158 and the sub_8500a0 level gate)
//   col 38 -> +0x744 req STR         (GetData()+0xb0: the sub_588af0 add-button
//                                     gate vs CICPlayer+0x834 @0x588ddb)
//   col 39 -> +0x748 req INT         (GetData()+0xb4: the gate vs +0x836
//                                     @0x588dfb)
//   col 40..42 -> +0x74c..+0x754     req skill GROUP ids 1..3 (GetData()
//                                     +0xb8/+0xbc/+0xc0: the sub_7f1680
//                                     relation-link pass reads the trio
//                                     @0x7f19a0 and pushes the group's row at
//                                     the required basic level into the
//                                     record's +0x9d8 prerequisite list)
//   col 43..45 -> +0x758..+0x75a     req skill group LEVEL bytes 1..3
//                                     (GetData()+0xc4/+0xc5/+0xc6: the
//                                     basic-level match @0x7f1a02)
//   col 46 -> +0x75c req learn SP    (GetData()+0xc8 m_nReq_Sp: parse writes
//                                     the +0x8fc enabled byte sub_7f8570 gates
//                                     slot insertion on)
//   col 57 -> +0x784 tab value       (GetData()+0xf0: the sub_584f40 learned
//                                     walk's masterydata+0x5c tab-key match)
//   col 59 -> +0x78c UI row          (GetData()+0xf8: 0xff = not on the board)
//   col 60 -> +0x790 UI column       (GetData()+0xfc: the 37+36c slot x)
//   col 61 -> +0x79c icon path       (GetData()+0x108, icon-root relative,
//                                     lowercased by sub_811890 flag 1)
//   col 62 -> +0x7b8 name symbol     (the SN_ textdataname code)
//
// Tooltip closure (sub_55fe60 -> sub_806ee0/sub_806240):
//   col  9 -> +0x64 chain-next skill id (effect/attack aggregation walk)
//   col 22 -> +0x98 target-required / flat record+0x72c enable byte
//   col 50/51 -> +0xd8/+0xdc required weapon kinds (0xff = none)
//   col 52..55 -> +0xe0/+0xe4/+0xe8/+0xea HP/MP absolute/ratio costs
//   col 64 -> +0x15c localized tooltip-description symbol
//   col 65 -> +0x178 study/explanation symbol (sub_5de040 translates it into
//             GDR_SKLPB_DESCRIPTION @0x5de161..0x5de1bf)
//   col 69..117 -> the complete decoder-consumed parameter tail. File col
//                  68 is Param1, which CSkillData_DecodeParamBlocks skips
//                  because data_ccb530 is forced to one @0x84b2f7; cols
//                  69..117 are the 49 dwords it actually walks. Keeping the
//                  entire tagged stream is required for both race families:
//                  e.g. EU starter attacks put `mc` and `da` after `att`,
//                  while CH passives commonly start with `dura`/`defp`.
//
// Output, one file per runtime owner so no owner parses another's data:
//   skillData.json           the projected skill catalogue (columns/rows):
//                            the HUD's skill pane and tooltips;
//   skillAudioData.json      the skill sound identity rows: audio and the
//                            character owner's skill sound profiles;
//   characterActionData.json characterInfo action-effect rows, shadow sizes,
//                            usable-resource appearance stores and the msch
//                            appearance references: the character owner.
// One 15 MB file used to be parsed by all three owners at login.

import fs from "node:fs";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataLinesSync, readTextDataRowsSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import { readSkillEffectSourceText } from "../char/parseSkillEffect.mjs";

import { gameRoot, publicRoot, retailTextdataRoot } from "../world/paths.mjs";
import { tooltipAppearanceReferences } from "../../../apps/client-next/src/engine/foundation/ui/skill-tooltip-catalog.ts";

const textdataRoot = retailTextdataRoot;
const manifestPath = path.join( textdataRoot, "skilldata.txt" );
const skillEffectPath = path.join( textdataRoot, "skilleffect.txt" );

// The projected source columns, in shipped order (see the contract above).
const PROJECTED_COLUMNS = [
	1,
	2,
	7,
	8,
	34,
	36,
	38,
	39,
	40,
	41,
	42,
	43,
	44,
	45,
	46,
	57,
	59,
	60,
	61,
	62,
	9,
	50,
	51,
	52,
	53,
	54,
	55,
	64,
	65,
	...Array.from( { length: 49 }, ( _unused, index ) => 69 + index ),
	// Appended to preserve the v7 field indices consumed by the bridge/tests.
	22
];
// The native parse reads 117 tokens after the loader-consumed Service column.
const MIN_COLUMN_COUNT = 118;

/*
================
optionalCharacterInfoText
================
*/
function optionalCharacterInfoText( value ) {
	const text = String( value ?? "" ).trim();
	return text && text.toLowerCase() !== "none" ? text : null;
}

/*
================
parseCharacterInfoOffset
================
*/
function parseCharacterInfoOffset( value, codename ) {
	const parts = String( value ?? "" ).split( "," ).map( ( part ) => Number( part.trim() ) );
	if ( parts.length !== 3 || parts.some( ( part ) => !Number.isFinite( part ) ) ) {
		throw new Error( `[skilldata] ${codename}: invalid characterInfo anchor offset ${value}` );
	}
	return { x: parts[0], y: parts[1], z: parts[2] };
}

/**
 * Native sub_920020 loads this client-owned table from
 * textdata/skilleffect.txt; sub_91b830 writes one 0x40-byte action-effect
 * context per character codename. Keep the projection beside skillData so
 * the app can publish it before any world-entry packet binds CICharactor+710.
 */
export function loadCharacterActionEffectRows( sourcePath = skillEffectPath ) {
	if ( !fs.existsSync( sourcePath ) ) return [];

	const rows = new Map();
	let inCharacterInfo = false;
	for ( const line of readSkillEffectSourceText( sourcePath ).split( /\r?\n/ ) ) {
		if ( line.startsWith( "#section" ) ) {
			inCharacterInfo = /^#section\s+characterInfo\b/i.test( line );
			continue;
		}
		if ( !inCharacterInfo || !line || line.startsWith( "//" ) ) continue;

		const cols = splitTextDataRow( line );
		const codename = String( cols[0] ?? "" ).trim();
		if ( !codename ) continue;

		const sourceHeight = Number( cols[2] );
		if ( !Number.isFinite( sourceHeight ) ) {
			throw new Error( `[skilldata] ${codename}: invalid characterInfo height ${cols[2]}` );
		}
		const rideType = String( cols[3] ?? "none" ).trim().toUpperCase();
		const record = {
			codename,
			soundProfileName: optionalCharacterInfoText( cols[1] ),
			// Rizin 0x91b96a: fld source float, fmul 0.5, store record+0x08.
			heightFactor: sourceHeight * 0.5,
			riderTransformMode: rideType === "RT_FIXED" ? 1 : rideType === "RT_DUMMY" ? 2 : 0,
			rideModelPath: optionalCharacterInfoText( cols[4] ),
			modelPath: optionalCharacterInfoText( cols[5] ),
			effectCodeName: optionalCharacterInfoText( cols[6] ),
			anchorSocketName: optionalCharacterInfoText( cols[7] ),
			anchorOffset: parseCharacterInfoOffset( cols[8], codename ),
			bloodEffects: optionalCharacterInfoText( cols[9] ) ?
				[ `hiteffect/${cols[9].toLowerCase()}.efp`, "hiteffect/hit_2_greenblood.efp" ] :
				[ null, null ]
		};
		for ( const expanded of expandCharacterInfoCodenames( codename ) ) {
			rows.set( expanded, { ...record, codename: expanded } );
		}
	}
	return [ ...rows.values() ];
}

/*
================
loadEffectAppearanceStores
================
*/
export function loadEffectAppearanceStores( root = textdataRoot ) {
	const byId = new Map(
		[ ...loadCharacterDataRows( root, { codenamePattern: /.*/ } ).values() ].map( row => [ Number( row[1] ), row ] )
	);
	const stores = [ [], [] ];
	for ( const line of readTextDataLinesSync( path.join( root, "usableresobjiddata.txt" ) ) ) {
		const cols = splitTextDataRow( line );
		if ( !Number( cols[0] ) ) continue;
		const id = Number( cols[1] ), row = byId.get( id );
		if ( row && (Number( row[14] ) === 0 || Number( row[14] ) === 1) ) stores[Number( row[14] )].push( id );
	}
	return stores;
}
/*
================
buildSkillDataAsset
================
*/
export async function buildSkillDataAsset() {
	if ( !fs.existsSync( manifestPath ) || !fs.existsSync( skillEffectPath ) ) {
		console.warn( `[skilldata] source missing (${manifestPath} / ${skillEffectPath}) - skipping` );
		return { written: false, rows: 0 };
	}

	// The manifest lists the shard files exactly like the native enc-kind-1
	// loader consumes them (SkillData_5000.txt ... SkillData_35000.txt).
	const shardPaths = readTextDataLinesSync( manifestPath )
		.map( ( name ) => path.join( textdataRoot, name.trim().toLowerCase() ) )
		.filter( ( shardPath ) => fs.existsSync( shardPath ) );

	const sourceRows = [];
	for ( const shardPath of shardPaths ) {
		for ( const cols of readTextDataRowsSync( shardPath ) ) {
			// The table loader keeps Service==1 rows; short rows would underrun the
			// native 117-token parse.
			if ( cols.length < MIN_COLUMN_COUNT || cols[0].trim() !== "1" ) {
				continue;
			}
			sourceRows.push( cols );
		}
	}

	// Every folded skill-driven sound profile resolves the active CIDecoSkill
	// id through CSkillData. sub_7f9310 writes source columns 1/3/5/6 to the
	// info block's +0x00/+0x08/+0x40/+0x5c fields respectively; sub_8edfa0
	// follows the +0x5c root override before returning one of the two names.
	// Keep that complete identity plane, not an effectsound-handle-specific
	// subset. The authoritative skill id remains the join key.
	const audioRowsBySkillId = new Map();
	for ( const cols of sourceRows ) {
		const skillId = cols[1].trim();
		const row = [ skillId, cols[6], cols[3], cols[5] ].join( "\t" );
		const previous = audioRowsBySkillId.get( skillId );

		if ( previous !== undefined && previous !== row ) {
			throw new Error( `[skilldata] conflicting audio identity rows for skill id ${skillId}` );
		}
		audioRowsBySkillId.set( skillId, row );
	}
	const skillAudioRows = [ ...audioRowsBySkillId.values() ];
	const characterActionEffectRows = resolveCharacterInfoRows(
		loadCharacterActionEffectRows(),
		loadCharacterDataRows( textdataRoot, { codenamePattern: /.*/ } )
	);

	// Native owns one complete data_cec870+0x16c table. Keep every service row
	// in that canonical projected owner; UI-row filtering belongs downstream
	// in SkillPane_BuildSlotTable, not at the data lifecycle boundary.
	const rows = sourceRows.map( ( cols ) => PROJECTED_COLUMNS.map( ( index ) => cols[index] ).join( "\t" ) );

	const sourcePaths = [ manifestPath, ...shardPaths, skillEffectPath ].map( ( sourcePath ) =>
		path.relative( gameRoot, sourcePath ).replaceAll( "\\", "/" )
	);
	const catalog = {
		sourcePaths,
		format: "sro-skilldata",
		// v11: the audio and character planes moved to their own files.
		// v10: resolve characterInfo through direct/original/default registration.
		// v9: characterInfo now comes from the native textdata file opened by
		// sub_920020, not the stale resinfo mirror (anchors/effects differ).
		// v8: complete +0x16c row ownership (pet/non-board timed skills included)
		// and append col22 target-required without disturbing the v7 indices.
		// v7: replace the CH-biased first-`att` slice with the complete
		// sub_84b2f0 decoder-consumed parameter tail.
		version: 11,
		// Source column index per shipped tab-separated field (the contract in
		// the banner above; decode semantics live in skill-tooltip-catalog.ts).
		columns: PROJECTED_COLUMNS,
		rows
	};
	const audio = { sourcePaths, format: "sro-skill-audio", version: 1, skillAudioRows };
	// The msch (CSkillData+0x268) references are walked once here with the
	// client's own decoder, so the character owner never parses the catalogue.
	const references = tooltipAppearanceReferences( catalog );
	const characterAction = {
		sourcePaths,
		format: "sro-character-action",
		version: 1,
		characterActionEffectRows,
		// CRefObjCommon_ParseTextRow 808AD0: field +110 is column 50.
		characterShadowSizes: [ ...loadCharacterDataRows( textdataRoot, { codenamePattern: /.*/ } ).values() ].filter(
			row => Number( row[0] ) === 1
		).map( row => [ Number( row[1] ), Number( row[50] ) ] ),
		effectAppearanceStores: loadEffectAppearanceStores(),
		// [skill id, appearance type, cap] per skill carrying an msch block.
		effectAppearanceReferences: [ ...references ].map( ( [id, { type, cap }] ) => [ id, type, cap ] )
	};

	const outPaths = [
		exportDataAsset( { publicRoot, outputFileName: "skillData.json", value: catalog } ).outPath,
		exportDataAsset( { publicRoot, outputFileName: "skillAudioData.json", value: audio } ).outPath,
		exportDataAsset( { publicRoot, outputFileName: "characterActionData.json", value: characterAction } ).outPath
	];
	const outPath = outPaths[0];

	await refreshPrecompressedSidecars( outPaths, { onlyWhenStale: true } );

	console.log(
		`[skilldata] wrote ${rows.length} canonical skill rows, ${skillAudioRows.length} skill audio identities, ${characterActionEffectRows.length} client character action-effect rows and ${references.size} appearance references (${shardPaths.length} shards) -> ${
			outPaths.map( ( p ) => path.relative( publicRoot, p ) ).join( ", " )
		}`
	);
	return {
		written: true,
		rows: rows.length,
		groups: new Set( sourceRows.map( ( cols ) => cols[2].trim() ) ).size,
		outPath,
		outPaths
	};
}

// Run directly (node scripts/build/data/buildSkillDataAsset.mjs) or via the
// resource build aggregator.
if ( isMainScript( import.meta.url ) ) {
	await buildSkillDataAsset();
}
