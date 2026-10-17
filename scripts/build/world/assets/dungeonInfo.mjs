/*
===========================================================================

dungeonInfo.mjs - read dungeoninfo.txt admission rows for dungeon regions

Reads Data_extracted/dungeon/dungeoninfo.txt (CP949 text) and appends the
six Qin-Shi Tomb (jinsi) dungeon floor entries (regions 7..2 ->
Dungeon\china\jinsi_floor01..06.dof) when their retail JMXVDOF files are
present alongside dungeoninfo.txt. In v1.150, regioninfo.txt (#FIELD
jinsi: 2..7 128 ALL -> 0x8002..0x8007) and teleportdata.txt (gates 49..135
on -32761..-32766) already declare all six floors, and Data.pk2 ships
china/jinsi_floor01..06.dof with embedded generalRegionId headers
32775..32770 (0x8007..0x8002), while dungeoninfo.txt omitted those six
rows before the Qin-Shi Tomb opening.

===========================================================================
*/

import { access, readFile } from "node:fs/promises";
import path from "node:path";
import { decodeJmxText } from "../../shared/jmxBinaryReader.mjs";

const QINSHI_TOMB_DUNGEON_INFO_ROWS = Object.freeze( [
	{ regionId: 7, dofName: "Dungeon\\china\\jinsi_floor01.dof", relativeDofPath: "china/jinsi_floor01.dof" },
	{ regionId: 6, dofName: "Dungeon\\china\\jinsi_floor02.dof", relativeDofPath: "china/jinsi_floor02.dof" },
	{ regionId: 5, dofName: "Dungeon\\china\\jinsi_floor03.dof", relativeDofPath: "china/jinsi_floor03.dof" },
	{ regionId: 4, dofName: "Dungeon\\china\\jinsi_floor04.dof", relativeDofPath: "china/jinsi_floor04.dof" },
	{ regionId: 3, dofName: "Dungeon\\china\\jinsi_floor05.dof", relativeDofPath: "china/jinsi_floor05.dof" },
	{ regionId: 2, dofName: "Dungeon\\china\\jinsi_floor06.dof", relativeDofPath: "china/jinsi_floor06.dof" }
] );

/*
================
fileExists
================
*/
async function fileExists( candidatePath ) {
	try {
		await access( candidatePath );
		return true;
	} catch {
		return false;
	}
}

/*
================
readDungeonInfoRows

Read native dungeoninfo rows, treating a missing optional source as an
empty table, and append the six Qin-Shi Tomb floor rows when their retail
china/jinsi_floor01..06.dof files exist in the same dungeon directory.
================
*/
export async function readDungeonInfoRows( filePath ) {
	let text;
	try {
		// Paths are CP949 byte strings in this retail table, decoded like every other
		// JMX name so they match the extracted tree (ASCII rows are unchanged).
		text = decodeJmxText( await readFile( filePath ) );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return [];
		throw error;
	}

	const rows = [];
	const seenRegionIds = new Set();
	for ( const line of text.split( /\r?\n/ ) ) {
		const trimmed = line.trim();
		if ( !trimmed || trimmed.startsWith( "//" ) ) continue;
		const match = /^(\d+)\s+"([^"]+)"/.exec( trimmed );
		if ( !match ) continue;
		const regionId = Number.parseInt( match[1], 10 );
		rows.push( {
			regionId,
			dofName: match[2]
		} );
		seenRegionIds.add( regionId );
	}

	const dungeonDir = path.dirname( filePath );
	for ( const entry of QINSHI_TOMB_DUNGEON_INFO_ROWS ) {
		if ( seenRegionIds.has( entry.regionId ) ) continue;
		if ( await fileExists( path.join( dungeonDir, entry.relativeDofPath ) ) ) {
			rows.push( {
				regionId: entry.regionId,
				dofName: entry.dofName
			} );
			seenRegionIds.add( entry.regionId );
		}
	}
	return rows;
}
