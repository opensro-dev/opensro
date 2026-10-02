/*
===========================================================================

buildCosPresentationAsset.mjs - the COS reference fields the HUD draws

The COS status icon, command bar and info page (CIFCOSStatus 6AA290 /
6A9C50, CosCommand_ResolveIconPathByKind 6A1BE0, CIFCosInfo_RefreshHP
6A4280) read three RefObjChar fields of the companion's reference:

	column 54 -> +0x154 icon (relative to Media\icon)
	column 59 -> +0x1B0 max HP
	column 68 -> +0x1D4 non-zero lets the Ride command board it

Every in-service COS reference (TypeID 1/2/3, TID4 band 1..6) ships, keyed by
its RefObj id: refObjId -> [icon, maxHp, rideable].

===========================================================================
*/
import path from "node:path";
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
const COLUMN_ICON = 54;
const COLUMN_MAX_HP = 59;
const COLUMN_RIDEABLE = 68;
const COS_BAND_FIRST = 1;
const COS_BAND_LAST = 6;

/*
================
buildCosPresentationAsset
================
*/
export function buildCosPresentationAsset( options = {} ) {
	const textdataRoot = options.textdataRoot ?? retailTextdataRoot;
	const rows = {};
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
			rows[String( id )] = [ /\.ddj$/.test( icon ) ? icon : "", maxHp, rideable !== 0 ];
		}
	}
	const { outPath } = exportDataAsset( {
		publicRoot: options.publicRoot ?? publicRoot,
		outputFileName: "cosPresentation.json",
		value: { format: "sro-cos-presentation", version: 1, rows }
	} );
	console.log( `[cos-presentation] wrote ${Object.keys( rows ).length} COS reference row(s)` );
	return { outPath, rows: Object.keys( rows ).length };
}

if ( isMainScript( import.meta.url ) ) buildCosPresentationAsset();
