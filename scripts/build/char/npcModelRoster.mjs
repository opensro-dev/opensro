/*
===========================================================================

npcModelRoster.mjs - which references the mission NPC model bake covers

The bake publishes models for three sets, and this module owns all three:

- spawnable NPCs and monsters, exported by the server itself
  (sro-evidence spawnable-npcs / spawnable-monsters), so bake coverage tracks
  what the server can ever stream;
- every enabled COS reference (growth pets and hidden transports) in
  characterdata, per the native action-1 routing at 582110.

buildNpcModelAssets.mjs bakes them; the asset tests check the published
manifest against these same functions.

===========================================================================
*/

import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const repositoryRoot = path.resolve( scriptDir, "..", "..", ".." );
const gameRoot = path.resolve( repositoryRoot, ".." );
const textdataDir = path.join( gameRoot, "extracted", "Media_extracted", "server_dep", "silkroad", "textdata" );

// characterdata columns for the COS gate: service flag, then TypeID1..4.
const CHARACTERDATA_SERVICE = 0;
const CHARACTERDATA_REF_OBJ_ID = 1;
const CHARACTERDATA_TYPE_ID1 = 9;
const CHARACTERDATA_TYPE_ID2 = 10;
const CHARACTERDATA_TYPE_ID3 = 11;
const CHARACTERDATA_TYPE_ID4 = 12;
const ROSTER_EXPORT_MAX_BYTES = 16 * 1024 * 1024;

/*
================
loadServerRoster

Monster roster: DERIVED from the server's spawnable join, never hand
maintained. The retail client has no roster at all - CICMonster spawn
(sub_861b00) binds the refObjId to its characterdata record and loads the
record's .bsr on demand (LoadVisualModelAndCacheBounds 0x853e70); the
offline-bake analog is "bake exactly what the server can ever stream". That
set is monsterpop.LoadTemplate().SpawnableRefs() - the npcpos.txt spawn
points joined with the binary-pinned CICMonster TypeID gate - exported by
server sro-evidence spawnable-monsters and consumed here at bake time. The
classifier lives in Go ONLY; duplicating the TypeID gate here was rejected
as cross-plane drift risk.

History: the previous hand-enumerated list missed MOB_CH_WHITETIGER_CLON and
the human saw a permanent peg (monster-live board seq693/695).

_CLON rows have NO .bsr of their own: characterdata col[4] links the base
codename and col[48] carries the scale percent (tiger 100 vs tiger_clon 80) -
resolveNpcModel follows the guarded base chain and the clone's manifest
entry reuses the base GLB.
================
*/
function loadServerRoster( subcommand, expectedFormat, label ) {
	const serverRoot = path.resolve(
		process.env.SRO_SERVER_SOURCE_ROOT ?? path.join( repositoryRoot, "apps", "server" )
	);
	const result = spawnSync( "go", [ "run", "./cmd/tools/sro-evidence", subcommand, "-textdata-dir", textdataDir ], {
		cwd: serverRoot,
		env: process.env,
		encoding: "utf8",
		maxBuffer: ROSTER_EXPORT_MAX_BYTES
	} );
	if ( result.error || result.status !== 0 ) {
		throw new Error(
			`[npc] ${label} roster export failed (go run ./cmd/tools/sro-evidence ${subcommand} in ${serverRoot}): ` +
				`${result.error ?? ""} ${result.stderr ?? ""}`.trim()
		);
	}
	const roster = JSON.parse( result.stdout );
	if (
		roster.format !== expectedFormat ||
		!Array.isArray( roster.refs ) ||
		roster.refs.length === 0 ||
		roster.count !== roster.refs.length
	) {
		throw new Error(
			`[npc] ${label} roster export returned an unexpected shape ` +
				`(format=${roster.format}, count=${roster.count}, refs=${roster.refs?.length})`
		);
	}
	const codenames = new Set();
	const refObjIds = new Set();
	for ( const ref of roster.refs ) {
		if (
			!ref?.codename ||
			!Number.isInteger( ref.refObjId ) ||
			ref.refObjId <= 0 ||
			codenames.has( ref.codename ) ||
			refObjIds.has( ref.refObjId )
		) {
			throw new Error(
				`[npc] ${label} roster contains an invalid or duplicate identity ` +
					`(codename=${ref?.codename}, refObjId=${ref?.refObjId})`
			);
		}
		codenames.add( ref.codename );
		refObjIds.add( ref.refObjId );
	}
	console.log( `[npc] ${label} roster: ${roster.refs.length} entries from ${roster.source}` );
	return roster.refs;
}

/*
================
loadSpawnableMobRoster
================
*/
export function loadSpawnableMobRoster() {
	return loadServerRoster( "spawnable-monsters", "sro-spawnable-monster-roster", "spawnable monster" );
}

/*
================
loadSpawnableNpcRoster
================
*/
export function loadSpawnableNpcRoster() {
	return loadServerRoster( "spawnable-npcs", "sro-spawnable-npc-roster", "spawnable NPC" );
}

/*
================
enabledCosReferences

Every enabled COS reference in characterdata: in service, TypeID 1/2/3 and
TypeID4 3 or 4. 582110 routes action 1 of growth pets and hidden transports
to state 50, so each enabled reference is published, sharing the native BSR
bake across levels. `rows` is loadCharacterDataRows' codename -> columns map.
================
*/
/**
 * @param {Map<string, string[]>} rows
 * @returns {{ codename: string, refObjId: number }[]}
 */
export function enabledCosReferences( rows ) {
	return [ ...rows ].filter( ( [, cols] ) =>
		Number( cols[CHARACTERDATA_SERVICE] ) === 1 &&
		Number( cols[CHARACTERDATA_TYPE_ID1] ) === 1 &&
		Number( cols[CHARACTERDATA_TYPE_ID2] ) === 2 &&
		Number( cols[CHARACTERDATA_TYPE_ID3] ) === 3 &&
		[ 3, 4 ].includes( Number( cols[CHARACTERDATA_TYPE_ID4] ) )
	).map( ( [codename, cols] ) => ({ codename, refObjId: Number( cols[CHARACTERDATA_REF_OBJ_ID] ) }) );
}
