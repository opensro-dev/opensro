/*
===========================================================================

buildMissionPresentationAsset.mjs - the browser's presentation projection

Enriches the semantic EnterWorld v2 rows. The GameWorld sends stable
RefObj/item identities and gameplay records; native resource paths and the
client-side textdata tables stay in this client artifact
(/assets/data/missionPresentation.json): character models, item icons,
worn/drop models and type words, recovery periods, and the trade bandits'
skin pools.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { isMainScript } from "../shared/fsUtils.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { PORT_ITEM_TEMPLATE, portItemColumns } from "../shared/portItemRows.mjs";
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
// itemdata columns of TypeID1..4, the packed type word's fields.
const COL_ITEM_TYPE_ID1 = 9;
// 861720 reads the bandit's weapon skill at common base +0x210: allocation
// +0x214, characterdata column 89, the first of the 10-skill loop (808670).
const COL_TRADE_SKILL = 89;
// characterdata TypeID1..4 (the itemdata columns): a thief or hunter
// monster is 1/2/1 with TypeID4 2 or 3 (type words 0x10C6 / 0x18C6).
const COL_CHARACTER_TYPE_ID1 = 9;
const TRADE_BANDIT_TID = [ 1, 2, 1 ], TID4_THIEF = 2, TID4_HUNTER = 3;
// skilldata columns: id, codename, the weapon route (+0xD8) and Param1.
const COL_SKILL_ID = 1, COL_SKILL_CODENAME = 3, COL_SKILL_WEAPON = 50, COL_SKILL_PARAM1 = 69;
// CSkillData_IndexParameterTags (84B2F0) tags: cold fz/fb, fire bu, lightning es.
const TAG_FZ = 0x667a, TAG_FB = 0x6662, TAG_BU = 0x6275, TAG_ES = 0x6573;
// 7D3550: the nearest imbue key must lie under this distance.
const IMBUE_KEY_RANGE = 1000;

/*
================
itemTypeFlags

The item's packed type word exactly as the server packs it (enterworld
ItemRef.TypeFlags, the native RefItemData word): TID1<<2 | TID2<<5 |
TID3<<7 | TID4<<11, kept to 16 bits; change both together. A look's weapon
picks its idle stance from bits 11..15 (weaponAnimationSet). Undefined for
a row whose TypeIDs are not whole numbers.
================
*/
function itemTypeFlags( columns ) {
	const [tid1, tid2, tid3, tid4] = [ 0, 1, 2, 3 ].map( i => Number( columns[COL_ITEM_TYPE_ID1 + i]?.trim() ) );
	if ( ![ tid1, tid2, tid3, tid4 ].every( v => Number.isInteger( v ) && v >= 0 ) ) return undefined;
	return (tid1 << 2 | tid2 << 5 | tid3 << 7 | tid4 << 11) & 0xffff;
}

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
				country: Number( columns[COL_COUNTRY] ),
				typeIds: [ 0, 1, 2, 3 ].map( i => Number( columns[COL_CHARACTER_TYPE_ID1 + i] ) ),
				tradeSkill: Number( columns[COL_TRADE_SKILL] )
			} );
		}
	}
	const shardNames = listTextDataShardNamesSync( textdataRoot, /^itemdata.*\.txt$/i ).sort( ( left, right ) =>
		left.localeCompare( right )
	);
	for ( const shardName of shardNames ) {
		// The port-only rows (portItemRows.mjs) follow their template, as the
		// server bundle projects them.
		const rows = readTextDataRowsSync( path.join( textdataRoot, shardName ) ).flatMap( columns =>
			columns[2]?.trim() === PORT_ITEM_TEMPLATE ? [ columns, ...portItemColumns( columns ) ] : [ columns ]
		);
		for ( const columns of rows ) {
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
			const typeFlags = itemTypeFlags( columns );
			itemsByRefObjId[String( refObjId )] = {
				codename,
				...(typeFlags !== undefined ? { typeFlags } : {}),
				// Native RefObjCommon worn resource, distinct from field53 ground-drop resource.
				// Preserve authored absence; it is not a failed conversion.
				wornModelPath: readWornModelPath( columns[52] ),
				...(iconDdjPath ? { iconDdjPath } : {}),
				...(dropModelPath ? { dropModelPath } : {})
			};
		}
	}

	const tradeSkinPools = readTradeSkinPools( textdataRoot, charactersById );
	const tradeBandits = readTradeBandits( textdataRoot, charactersById );
	const value = {
		format: "sro-mission-presentation",
		version: 1,
		protocolVersion: 2,
		charactersByCodename,
		recoveryByCodename,
		itemsByRefObjId,
		tradeSkinPools,
		tradeBandits
	};
	writeJsonIfChangedSync( targetPath, value );
	console.log(
		`[mission-presentation] wrote ${Object.keys( charactersByCodename ).length} character and ` +
			`${Object.keys( itemsByRefObjId ).length} item presentation row(s), ` +
			`${tradeSkinPools.china.length}/${tradeSkinPools.europe.length} trade skins, ` +
			`${tradeBandits.length} trade bandit weapon(s)`
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
skillTagValue

The first value after a parameter tag (84B2F0 points the tag's field at
&params[i + 1]), or undefined when the skill has no such tag.
================
*/
function skillTagValue( params, tag ) {
	const at = params.indexOf( tag );
	return at < 0 || at + 1 >= params.length ? undefined : params[at + 1];
}

/*
================
readTradeBandits

Each thief or hunter monster's [refObjId, weapon route, imbue skill], as
CICMonster_InitializeTradeEquipmentAndSkill (861720) dresses it from its
column-89 skill: the skill's weapon route (+0xD8) picks the weapon, and a
T/H-SKILL's imbue (+0x264, CSkillData_InitExtendedInfo 7D3550) is the
COLD/FIRE/LIGHTNING_GIGONGTA skill whose own tag value lies nearest the
skill's fz, fb, bu or es value (one shared bound of 1000 across the four,
first key on a tie). The maps keep the first skill per key in id order
(CGlobalData_IndexSkillGroupsMasteriesAndImbues 7E8C90). 0 is no imbue.
================
*/
function readTradeBandits( textdataRoot, charactersById ) {
	const skills = new Map();
	for ( const shard of listTextDataShardNamesSync( textdataRoot, /^skilldata.*\.txt$/i ) ) {
		for ( const columns of readTextDataRowsSync( path.join( textdataRoot, shard ) ) ) {
			if ( columns[COL_SERVICE] !== "1" ) continue;
			const id = Number( columns[COL_SKILL_ID] );
			if ( !Number.isInteger( id ) || id <= 0 ) continue;
			skills.set( id, {
				codename: columns[COL_SKILL_CODENAME]?.trim() ?? "",
				weapon: Number( columns[COL_SKILL_WEAPON] ),
				params: columns.slice( COL_SKILL_PARAM1 ).map( value => Number( value ) )
			} );
		}
	}
	const cold = new Map(), fire = new Map(), lightning = new Map();
	for ( const id of [ ...skills.keys() ].sort( ( a, b ) => a - b ) ) {
		const skill = skills.get( id );
		const file = ( map, tag, name ) => {
			const key = skillTagValue( skill.params, tag );
			if ( key !== undefined && skill.codename.includes( name ) && !map.has( key ) ) map.set( key, id );
		};
		file( cold, TAG_FZ, "COLD_GIGONGTA" );
		file( cold, TAG_FB, "COLD_GIGONGTA" );
		file( fire, TAG_BU, "FIRE_GIGONGTA" );
		file( lightning, TAG_ES, "LIGHTNING_GIGONGTA" );
	}
	const imbue = skill => {
		if ( !/^[TH]SKILL/.test( skill.codename ) ) return 0;
		let best = 0, distance = IMBUE_KEY_RANGE;
		for ( const [tag, map] of [ [ TAG_FZ, cold ], [ TAG_FB, cold ], [ TAG_BU, fire ], [ TAG_ES, lightning ] ] ) {
			const value = skillTagValue( skill.params, tag );
			if ( value === undefined ) continue;
			for ( const key of [ ...map.keys() ].sort( ( a, b ) => a - b ) ) {
				if ( Math.abs( key - value ) < distance ) {
					distance = Math.abs( key - value );
					best = map.get( key );
				}
			}
		}
		return best;
	};
	const rows = [];
	for ( const [id, character] of charactersById ) {
		const tids = character.typeIds;
		if ( TRADE_BANDIT_TID.some( ( tid, i ) => tids[i] !== tid ) ) continue;
		if ( tids[3] !== TID4_THIEF && tids[3] !== TID4_HUNTER ) continue;
		const skill = skills.get( character.tradeSkill );
		if ( !skill || !Number.isInteger( skill.weapon ) ) continue;
		rows.push( [ Number( id ), skill.weapon, imbue( skill ) ] );
	}
	return rows.sort( ( a, b ) => a[0] - b[0] );
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
