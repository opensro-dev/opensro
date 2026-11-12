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

The same existence rule appends the four Mirror Dimension temple
interiors (regions 32787..32790) when the later client's DOF tree the
Jupiter import copies (SRO_JUPITER_TEMPLE) sits alongside dungeoninfo.txt
in the variant build (#3295). Each DOF's embedded generalRegionId header
names its region (32787..32790, matching regioncode RN_JUPITER_A_THEEARTH,
RN_JUPITER_A_UNO, RN_JUPITER_A_UPI, RN_JUPITER_B), and a build without the
import has no such files, so its rows - and every byte built from them -
are unchanged. The native dungeoninfo entries remain the admission
authority: these rows publish geometry for regions the server admits
through the temple worlds; no gate or entry is created here.

===========================================================================
*/

import { access, readFile } from "node:fs/promises";
import path from "node:path";
import { decodeJmxText } from "../../shared/jmxBinaryReader.mjs";

// Port-only, not native (#3295): the Mirror Dimension temple interiors
// of the Jupiter import. The DOF file names are the later client's own
// (dungeon/jupiter/** is the whole DOF tree it ships), and each file's
// embedded generalRegionId header carries the region id below, so the
// binding is read from the data, not authored.
const JUPITER_TEMPLE_DUNGEON_INFO_ROWS = Object.freeze( [
	{
		regionId: 32787,
		dofName: "Dungeon\\jupiter\\piety\\piety_boss_di.dof",
		relativeDofPath: "jupiter/piety/piety_boss_di.dof"
	},
	{
		regionId: 32788,
		dofName: "Dungeon\\jupiter\\piety\\piety_boss_uno.dof",
		relativeDofPath: "jupiter/piety/piety_boss_uno.dof"
	},
	{
		regionId: 32789,
		dofName: "Dungeon\\jupiter\\piety\\piety_boss_upi.dof",
		relativeDofPath: "jupiter/piety/piety_boss_upi.dof"
	},
	{ regionId: 32790, dofName: "Dungeon\\jupiter\\hide\\hide.dof", relativeDofPath: "jupiter/hide/hide.dof" }
] );

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
empty table, and append the Qin-Shi Tomb floor rows and the Mirror
Dimension temple interior rows whose DOF files exist in the same dungeon
directory.
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
	await appendRowsWithExistingDofs( rows, seenRegionIds, dungeonDir, QINSHI_TOMB_DUNGEON_INFO_ROWS );
	await appendRowsWithExistingDofs( rows, seenRegionIds, dungeonDir, JUPITER_TEMPLE_DUNGEON_INFO_ROWS );
	return rows;
}

/*
================
appendRowsWithExistingDofs

Append one table's rows whose DOF files exist in the dungeon directory.
A region the table itself already declares keeps its authored row, and
a table whose files are absent (a build without that import) appends
nothing.
================
*/
async function appendRowsWithExistingDofs( rows, seenRegionIds, dungeonDir, table ) {
	for ( const entry of table ) {
		if ( seenRegionIds.has( entry.regionId ) ) continue;
		if ( await fileExists( path.join( dungeonDir, entry.relativeDofPath ) ) ) {
			rows.push( {
				regionId: entry.regionId,
				dofName: entry.dofName
			} );
			seenRegionIds.add( entry.regionId );
		}
	}
}
