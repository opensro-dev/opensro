/*
===========================================================================

teleportModelRoster.mjs - the teleport buildings that carry their own model

The v1.150 client draws a teleport building's own model when its row names
one: CITeleportGate_ParseGroupedPublication (8762E0) loads record+0x11C
through CCObjAnimation_LoadModel when that string is not empty (its length
at +0x130). CRefObjCommon_ParseTextRow (808AD0) fills +0x11C from the first
of the five AssocFile columns, AssocFileObj (teleportbuilding column 52).
Every town gate names xxx there: its architecture is a map placement and
the gate draws nothing of its own. The quest exit gate (INS_QUEST_TELEPORT)
and the later imports' gates whose model is not on the map name a .bsr.

===========================================================================
*/

import path from "node:path";

import { readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";

// RefObjCommon AssocFileObj128 (CRefObjCommon_ParseTextRow, record+0x11C).
export const ASSOC_FILE_OBJ_COLUMN = 52;
const MODEL_PATTERN = /\.(?:bsr|cpd)$/i;

/*
================
collectTeleportModelRows

The in-service teleportbuilding rows whose AssocFileObj names a model.
================
*/
export function collectTeleportModelRows( rows ) {
	return rows.filter( ( cols ) =>
		cols[0] === "1" && MODEL_PATTERN.test( (cols[ASSOC_FILE_OBJ_COLUMN] ?? "").trim() )
	);
}

/*
================
loadTeleportModelRows
================
*/
export function loadTeleportModelRows( textdataDir ) {
	const rows = readTextDataLinesSync( path.join( textdataDir, "teleportbuilding.txt" ) )
		.map( ( line ) => splitTextDataRow( line.trim() ) );
	return collectTeleportModelRows( rows );
}
