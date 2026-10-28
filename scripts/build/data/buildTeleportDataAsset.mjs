/*
===========================================================================

buildTeleportDataAsset.mjs - publish the teleportdata/teleportlink catalogue

Publishes the v1.150 teleportdata.txt + teleportlink.txt relation used by
CIFNPCTalk_BuildTeleportDestinationMenu (CIFNPCTalk_AppendTeleportDestinationRows). The native lookup is:
bound runtime NPC gid -> RefObj id -> teleportdata source row -> linked
destination rows. Keep those identities explicit so the browser never
substitutes a runtime gid for a media-table key.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { loadQinShiTombTeleportLinkRows, qinshiTombEnabled } from "../shared/qinshiTomb.mjs";
import { readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const textDataRoot = path.join(
	extractedRoot,
	"Media_extracted",
	"server_dep",
	"silkroad",
	"textdata"
);
const teleportDataPath = path.join( textDataRoot, "teleportdata.txt" );
const teleportLinkPath = path.join( textDataRoot, "teleportlink.txt" );
const teleportBuildingPath = path.join( textDataRoot, "teleportbuilding.txt" );

/*
================
readRows
================
*/
function readRows( filePath ) {
	return readTextDataLinesSync( filePath ).map( ( line ) => splitTextDataRow( line.trim() ) );
}

/*
================
buildTeleportDataAsset
================
*/
export function buildTeleportDataAsset() {
	if (
		!fs.existsSync( teleportDataPath ) ||
		!fs.existsSync( teleportLinkPath ) ||
		!fs.existsSync( teleportBuildingPath )
	) {
		console.warn( "[teleportdata] source table missing - skipping" );
		return { written: false, teleportRows: 0, linkRows: 0 };
	}

	const fortressCodeByNpcRefObjId = new Map(
		readRows( teleportBuildingPath )
			.filter( ( columns ) => Number( columns[0] ) !== 0 )
			.map( ( columns ) => [
				Number( columns[1] ) >>> 0,
				columns[55] === "xxx" ? "" : columns[55] ?? ""
			] )
	);
	const teleportRows = readRows( teleportDataPath )
		.filter( ( columns ) => Number( columns[0] ) !== 0 )
		.map( ( columns ) => ({
			id: Number( columns[1] ) >>> 0,
			codeName: columns[2] ?? "",
			npcRefObjId: Number( columns[3] ) >>> 0,
			fortressCode: fortressCodeByNpcRefObjId.get( Number( columns[3] ) >>> 0 ) ?? "",
			nameSymbol: columns[4] ?? "",
			regionId: Number( columns[5] ) & 0xffff
		}) );
	const buildingSourceIds = new Set(
		teleportRows.filter( ( row ) => row.npcRefObjId !== 0 ).map( ( row ) => row.id )
	);
	const rawLinkRows = [ ...readRows( teleportLinkPath ) ];
	// Port-only, not native: when SRO_QINSHI_TOMB is enabled, include the
	// clickable Tomb building links (STORE_TQ_SNAKEGATE 125->130 and
	// STORE_TQ_6FGATE1..4 131..134) whose source/destination rows already
	// exist in v1.150 teleportdata.txt.
	if ( qinshiTombEnabled() ) {
		for ( const columns of loadQinShiTombTeleportLinkRows().rows ) {
			if ( buildingSourceIds.has( Number( columns[1] ) >>> 0 ) ) {
				rawLinkRows.push( columns );
			}
		}
	}
	const linkRows = rawLinkRows
		.filter( ( columns ) => Number( columns[0] ) !== 0 )
		.map( ( columns ) => ({
			sourceId: Number( columns[1] ) >>> 0,
			destinationId: Number( columns[2] ) >>> 0,
			fee: Number( columns[3] ) | 0
		}) );
	const catalog = {
		sourcePaths: [
			path.relative( gameRoot, teleportDataPath ).replaceAll( "\\", "/" ),
			path.relative( gameRoot, teleportLinkPath ).replaceAll( "\\", "/" ),
			path.relative( gameRoot, teleportBuildingPath ).replaceAll( "\\", "/" )
		],
		format: "sro-teleportdata",
		version: 1,
		teleportRows,
		linkRows
	};
	const { outPath } = exportDataAsset( { publicRoot, outputFileName: "teleportData.json", value: catalog } );
	console.log(
		`[teleportdata] wrote ${teleportRows.length} teleport + ${linkRows.length} link row(s) -> ` +
			`${path.relative( publicRoot, outPath )}`
	);
	return {
		written: true,
		teleportRows: teleportRows.length,
		linkRows: linkRows.length,
		outPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	buildTeleportDataAsset();
}
