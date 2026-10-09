/*
===========================================================================

buildMissionPresentationAsset.mjs - the browser's presentation projection

Enriches the semantic EnterWorld v2 rows. The GameWorld sends stable
RefObj/item identities and gameplay records; native resource paths and the
client-side textdata tables stay in this client artifact
(/assets/data/missionPresentation.json): character models, item icons and
worn/drop models, recovery periods, and the trade bandits' skin pools.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { isMainScript } from "../shared/fsUtils.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";

const npcManifestPath = path.join( publicRoot, "assets", "npc", "manifest.json" );
const outputPath = path.join( publicRoot, "assets", "data", "missionPresentation.json" );

// characterdata columns: service, RefObjID, codename, country (14), and the
// column 88 the recovery period comes from.
const COL_SERVICE = 0, COL_REF_OBJ_ID = 1, COL_CODENAME = 2, COL_COUNTRY = 14, COL_RECOVERY = 88;
// The skin-pool countries: CGlobalDataManager_LoadTextDataFile (7F22D0, file
// type 7) files a usable id under +0x220 when its record's country byte
// (+0x9c) is 0 and under +0x248 when it is 1.
const COUNTRY_CHINA = 0, COUNTRY_EUROPE = 1;
// The native sex selector (+0x1ac): 0 female, 1 male.
const SEX_FEMALE = 0, SEX_MALE = 1;

/*
================
buildMissionPresentationAsset
================
*/
export function buildMissionPresentationAsset( options = {} ) {
	const textdataRoot = options.textdataRoot ?? retailTextdataRoot;
	const sourceNpcManifest = options.npcManifestPath ?? npcManifestPath;
	const targetPath = options.outputPath ?? outputPath;
	const npcManifest = JSON.parse( fs.readFileSync( sourceNpcManifest, "utf8" ) );

	const charactersByCodename = {};
	for (
		const [codename, model] of Object.entries( npcManifest.models ?? {} ).sort( ( [left], [right] ) =>
			left.localeCompare( right )
		)
	) {
		const modelPath = normalizeGamePath( model?.bsr );
		if ( modelPath ) {
			charactersByCodename[codename] = { modelPath };
		}
	}

	const itemsByRefObjId = {};
	const recoveryByCodename = {};
	const charactersById = new Map();
	// 808670 parses RefObjChar; its common-data base is allocation +4.
	// Column 88 -> allocation +210 -> common base +20c, read by 8E6720.
	for ( const shard of listTextDataShardNamesSync( textdataRoot, /^characterdata.*\.txt$/i ) ) {
		for ( const columns of readTextDataRowsSync( path.join( textdataRoot, shard ) ) ) {
			if ( columns[COL_SERVICE] !== "1" ) continue;
			const name = columns[COL_CODENAME]?.trim(), period = Number( columns[COL_RECOVERY] );
			if ( !name || !Number.isInteger( period ) || period < 0 || period > 0x7fffffff - 500 ) {
				throw Error( `Invalid native recovery period ${name}` );
			}
			recoveryByCodename[name] = period;
			charactersById.set( columns[COL_REF_OBJ_ID]?.trim(), {
				codename: name,
				country: Number( columns[COL_COUNTRY] )
			} );
		}
	}
	const shardNames = listTextDataShardNamesSync( textdataRoot, /^itemdata.*\.txt$/i ).sort( ( left, right ) =>
		left.localeCompare( right )
	);
	for ( const shardName of shardNames ) {
		for ( const columns of readTextDataRowsSync( path.join( textdataRoot, shardName ) ) ) {
			const refObjId = Number.parseInt( columns[1]?.trim() ?? "", 10 );
			const codename = columns[2]?.trim() ?? "";
			if ( !Number.isSafeInteger( refObjId ) || refObjId <= 0 || !codename.startsWith( "ITEM_" ) ) {
				continue;
			}
			let iconDdjPath;
			let dropModelPath;
			for ( const rawValue of columns ) {
				const value = rawValue.trim();
				if ( !iconDdjPath && /\.ddj$/i.test( value ) ) {
					iconDdjPath = normalizeGamePath( `icon/${value}` );
				}
				if ( /\.bsr$/i.test( value ) ) {
					dropModelPath = normalizeGamePath( value );
				}
			}
			itemsByRefObjId[String( refObjId )] = {
				codename,
				// Native RefObjCommon worn resource, distinct from field53 ground-drop resource.
				// Preserve authored absence; it is not a failed conversion.
				wornModelPath: readWornModelPath( columns[52] ),
				...(iconDdjPath ? { iconDdjPath } : {}),
				...(dropModelPath ? { dropModelPath } : {})
			};
		}
	}

	const tradeSkinPools = readTradeSkinPools( textdataRoot, charactersById );
	const value = {
		format: "sro-mission-presentation",
		version: 1,
		protocolVersion: 2,
		charactersByCodename,
		recoveryByCodename,
		itemsByRefObjId,
		tradeSkinPools
	};
	writeJsonIfChangedSync( targetPath, value );
	console.log(
		`[mission-presentation] wrote ${Object.keys( charactersByCodename ).length} character and ` +
			`${Object.keys( itemsByRefObjId ).length} item presentation row(s), ` +
			`${tradeSkinPools.china.length}/${tradeSkinPools.europe.length} trade skins`
	);
	return {
		outPath: targetPath,
		characterCount: Object.keys( charactersByCodename ).length,
		itemCount: Object.keys( itemsByRefObjId ).length
	};
}

/*
================
readTradeSkinPools

The bodies a thief or hunter is dressed in (CICMonster_InitializeTradeEquipmentAndSkill
861720 picks selector % count). The client loads textdata/usableresobjiddata.txt
(service, RefObjID) and files every served id by its record's country, in
file order: 0 -> GlobalDataManager_GetChinaSkinPool (+0x220), 1 ->
GetEuropeSkinPool (+0x248). Each entry is [refObjId, sex]; the sex selector
follows the model's codename, as the server's player snapshot does
(_WOMAN_ is 0, every other body 1).
================
*/
function readTradeSkinPools( textdataRoot, charactersById ) {
	const pools = { china: [], europe: [] };
	for ( const columns of readTextDataRowsSync( path.join( textdataRoot, "usableresobjiddata.txt" ) ) ) {
		if ( columns[0]?.trim() !== "1" ) continue;
		const id = columns[1]?.trim(), character = charactersById.get( id );
		if ( !character ) continue;
		const entry = [ Number( id ), character.codename.toUpperCase().includes( "_WOMAN_" ) ? SEX_FEMALE : SEX_MALE ];
		if ( character.country === COUNTRY_CHINA ) pools.china.push( entry );
		else if ( character.country === COUNTRY_EUROPE ) pools.europe.push( entry );
	}
	if ( pools.china.length === 0 || pools.europe.length === 0 ) {
		throw Error( "usableresobjiddata.txt names no served China or Europe skin" );
	}
	return pools;
}

/*
================
readWornModelPath
================
*/
function readWornModelPath( value ) {
	const path = String( value ?? "" ).trim();
	if ( !path || path.toLowerCase() === "xxx" ) return null;
	if ( !/\.bsr$/i.test( path ) ) throw Error( `Invalid worn model resource: ${path}` );
	return normalizeGamePath( path );
}

/*
================
normalizeGamePath
================
*/
function normalizeGamePath( value ) {
	const normalized = String( value ?? "" ).trim().replaceAll( "\\", "/" ).replace( /^\/+/, "" );
	return normalized || undefined;
}

if ( isMainScript( import.meta.url ) ) {
	buildMissionPresentationAsset();
}
