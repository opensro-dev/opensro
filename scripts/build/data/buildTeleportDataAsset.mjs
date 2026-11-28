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
import { crossCityTeleportEnabled, loadCrossCityTeleportLinkRows } from "../shared/crossCityTeleport.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { janganTombTeleportEnabled, loadJanganTombTeleportLinkRows } from "../shared/janganTombTeleport.mjs";
import { loadMuhanTeleportDataRows, loadMuhanTeleportLinkRows, muhanEnabled } from "../shared/muhan.mjs";
import { loadQinShiTombTeleportLinkRows, qinshiTombEnabled } from "../shared/qinshiTomb.mjs";
import { closedSkytempleIds, skytempleEnabled } from "../shared/skytemple.mjs";
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
linkLevelBand

Port-only, not native (#4136, #3541): the level band of one
teleportlink row - the highest minimum and the lowest maximum among its
five kind-1 (level band) condition triples, kind/min/max at columns
6..20, the same rows the server's portalAdmission checks (it refuses
below the minimum and above the maximum). Both 0 when no kind-1
condition restricts the link. The v1.150 table caps 18 beginner gates
at 1-20 and opens the Donwhang cave exit at 50-999.
================
*/
function linkLevelBand( columns ) {
	let minLevel = 0, maxLevel = 0;
	for ( let at = 6; at + 2 < columns.length; at += 3 ) {
		if ( Number( columns[at] ) !== 1 ) continue;
		const minimum = Number( columns[at + 1] ) >>> 0, maximum = Number( columns[at + 2] ) >>> 0;
		minLevel = Math.max( minLevel, minimum );
		maxLevel = maxLevel === 0 ? maximum : Math.min( maxLevel, maximum );
	}
	return { minLevel, maxLevel };
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
	const rawTeleportRows = [ ...readRows( teleportDataPath ) ];
	// Port-only, not native (#3314): when SRO_MUHAN is enabled, include
	// the arena manager hub and the eight Muhan gate rows (227..235)
	// from the server's overlay; v1.150 teleportdata.txt has none.
	if ( muhanEnabled() ) {
		rawTeleportRows.push( ...loadMuhanTeleportDataRows().rows );
	}
	// Port-only, not native: the Sky Temple fields stay out of the menu
	// while SRO_SKYTEMPLE is off, as the server keeps them closed.
	const closedIds = closedSkytempleIds( rawTeleportRows, skytempleEnabled() );
	const teleportRows = rawTeleportRows
		.filter( ( columns ) => Number( columns[0] ) !== 0 )
		.filter( ( columns ) => !closedIds.has( Number( columns[1] ) >>> 0 ) )
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
	// Port-only, not native (#3314): when SRO_MUHAN is enabled, include
	// the 16 Muhan links (hub 227 to gates 228..235, gates back to the
	// town gate 5). Every source is a Muhan row appended above, so no
	// building-source filter applies.
	if ( muhanEnabled() ) {
		rawLinkRows.push( ...loadMuhanTeleportLinkRows().rows );
	}
	// Port-only, not native: when SRO_JANGAN_TOMB_TELEPORT (and the tomb)
	// is on, include the Jangan gate's tomb floor links; every row
	// they name already exists in v1.150 teleportdata.txt.
	if ( janganTombTeleportEnabled() ) {
		rawLinkRows.push( ...loadJanganTombTeleportLinkRows().rows );
	}
	// Port-only, not native: when SRO_CROSS_CITY_TELEPORT is on, include
	// the direct town-to-town gate links.
	if ( crossCityTeleportEnabled() ) {
		rawLinkRows.push( ...loadCrossCityTeleportLinkRows().rows );
	}
	const linkRows = rawLinkRows
		.filter( ( columns ) => Number( columns[0] ) !== 0 )
		.filter( ( columns ) =>
			!closedIds.has( Number( columns[1] ) >>> 0 ) && !closedIds.has( Number( columns[2] ) >>> 0 )
		)
		.map( ( columns ) => ({
			sourceId: Number( columns[1] ) >>> 0,
			destinationId: Number( columns[2] ) >>> 0,
			fee: Number( columns[3] ) | 0,
			...linkLevelBand( columns )
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
