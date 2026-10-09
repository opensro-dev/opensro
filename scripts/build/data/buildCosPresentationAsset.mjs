/*
===========================================================================

buildCosPresentationAsset.mjs - the COS reference fields the HUD draws

The COS status icon, command bar and info page (CIFCOSStatus 6AA290 /
6A9C50, CosCommand_ResolveIconPathByKind 6A1BE0, CIFCosInfo_RefreshHP
6A4280) read three RefObjChar fields of the companion's reference:

	column 54 -> +0x154 icon (relative to Media\icon)
	column 59 -> +0x1B0 max HP
	column 68 -> +0x1D4 non-zero lets the Ride command board it

An attack pet's info page also derives its ability table from the reference
(CCosDataManager_RecalculateSatietyDependentStats 8301A0):

	columns 77/78 -> +0x1E0/+0x1E4 physical and magical defence
	column 81     -> +0x1F0 parry
	column 83     -> +0x1F8 hit
	columns 89..98 -> +0x210 default skills, up to the first zero; the first
	                 physical and magical attack blocks give the attack ranges

The trade window's scale (CIFSpecialtyDeal_ComputeTradeScale 649050) reads
the transport's RefObjCommon run speed:

	column 47 -> +0x104 Speed2 (m_nSpeed2, asserted nonzero there)

Every in-service COS reference (TypeID 1/2/3, TID4 band 1..6) ships, keyed by
its RefObj id: refObjId -> [icon, maxHp, rideable, physicalDefence,
magicalDefence, parry, hit, skills]. The run speeds sit in their own
refObjId -> speed2 map, so a reader of the rows alone keeps working.

===========================================================================
*/
import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";

const COLUMN_SERVICE = 0;
const COLUMN_ID = 1;
const COLUMN_TYPE_ID1 = 9;
const COLUMN_TYPE_ID2 = 10;
const COLUMN_TYPE_ID3 = 11;
const COLUMN_TYPE_ID4 = 12;
const COLUMN_SPEED2 = 47;
const COLUMN_ICON = 54;
const COLUMN_MAX_HP = 59;
const COLUMN_RIDEABLE = 68;
const COLUMN_PHYSICAL_DEFENCE = 77;
const COLUMN_MAGICAL_DEFENCE = 78;
const COLUMN_PARRY = 81;
const COLUMN_HIT = 83;
const COLUMN_SKILL_FIRST = 89;
const SKILL_SLOTS = 10;
const COS_BAND_FIRST = 1;
const COS_BAND_LAST = 6;

/*
================
buildCosPresentationAsset
================
*/
export async function buildCosPresentationAsset( options = {} ) {
	const textdataRoot = options.textdataRoot ?? retailTextdataRoot;
	const rows = {}, speed2 = {};
	for ( const shard of listTextDataShardNamesSync( textdataRoot, /^characterdata.*\.txt$/i ) ) {
		for ( const cols of readTextDataRowsSync( path.join( textdataRoot, shard ) ) ) {
			const band = Number( cols[COLUMN_TYPE_ID4] );
			if (
				cols[COLUMN_SERVICE]?.trim() !== "1" || cols[COLUMN_TYPE_ID1]?.trim() !== "1" ||
				cols[COLUMN_TYPE_ID2]?.trim() !== "2" || cols[COLUMN_TYPE_ID3]?.trim() !== "3" ||
				!(band >= COS_BAND_FIRST && band <= COS_BAND_LAST)
			) continue;
			const id = Number( cols[COLUMN_ID] ),
				maxHp = Number( cols[COLUMN_MAX_HP] ),
				rideable = Number( cols[COLUMN_RIDEABLE] );
			if ( !Number.isSafeInteger( id ) || id <= 0 || !Number.isSafeInteger( maxHp ) || maxHp < 0 ) {
				throw new Error( `[cos-presentation] invalid COS reference row ${cols[2]}` );
			}
			const icon = cols[COLUMN_ICON]?.trim().toLowerCase() ?? "";
			const stat = column => {
				const value = Number( cols[column] );
				if ( !Number.isSafeInteger( value ) ) {
					throw new Error( `[cos-presentation] ${cols[2]}: column ${column}` );
				}
				return value;
			};
			const skills = [];
			for ( let i = 0; i < SKILL_SLOTS; i++ ) {
				const skill = stat( COLUMN_SKILL_FIRST + i );
				if ( skill === 0 ) break;
				skills.push( skill );
			}
			rows[String( id )] = [
				/\.ddj$/.test( icon ) ? icon : "",
				maxHp,
				rideable !== 0,
				stat( COLUMN_PHYSICAL_DEFENCE ),
				stat( COLUMN_MAGICAL_DEFENCE ),
				stat( COLUMN_PARRY ),
				stat( COLUMN_HIT ),
				skills
			];
			speed2[String( id )] = stat( COLUMN_SPEED2 );
		}
	}
	const { outPath } = exportDataAsset( {
		publicRoot: options.publicRoot ?? publicRoot,
		outputFileName: "cosPresentation.json",
		value: { format: "sro-cos-presentation", version: 1, rows, speed2 }
	} );
	// The packs carry the compressed sidecars; a stale one would ship the
	// previous rows.
	await refreshPrecompressedSidecars( [ outPath ], { onlyWhenStale: true } );
	console.log( `[cos-presentation] wrote ${Object.keys( rows ).length} COS reference row(s)` );
	return { outPath, rows: Object.keys( rows ).length };
}

if ( isMainScript( import.meta.url ) ) await buildCosPresentationAsset();
