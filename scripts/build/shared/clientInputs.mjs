/*
===========================================================================

clientInputs.mjs - the licensed client inputs the asset build reads

One definition of what a prepared game root holds, shared by the build's
preflight (build_sro_resources.mjs) and `pnpm assets doctor`. The folders
are produced by scripts/prepare_client_resources.py, whose archive list is
the Python side of this table.

===========================================================================
*/
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { extractedRoot, gameRoot } from "../world/paths.mjs";

// Client files the build itself opens: the executable (cursors, effect
// literals) and the particle archive (effect evidence).
export const BUILD_CLIENT_FILES = [ "SRO_Client.exe", "Particles.pk2" ];
// Archives `pnpm assets prepare` extracts.
export const CLIENT_ARCHIVES = [ "Media.pk2", "Data.pk2", "Map.pk2", "Particles.pk2", "Music.pk2" ];
// Folders under extracted/ the build reads.
export const EXTRACTED_FOLDERS = [
	"Media_extracted",
	"Data_extracted",
	"Map_extracted",
	"Particles_extracted",
	"Music_mp3"
];
export const PREPARATION_MANIFEST = path.join( extractedRoot, ".opensro-preparation.json" );
const PREPARE_HINT = "run `pnpm assets prepare` (see docs/GETTING_STARTED.md)";

/*
================
folderHasFiles

True when the folder exists and holds at least one entry. An empty folder
passes a directory read but produces an empty catalog, so it is missing.
================
*/
function folderHasFiles( folder ) {
	return existsSync( folder ) && statSync( folder ).isDirectory() && readdirSync( folder ).length > 0;
}

/*
================
missingClientInputs

Problems that stop the asset build before it starts: absent client files
and absent or empty extracted folders. Returns an empty list when the game
root is usable.
================
*/
export function missingClientInputs() {
	const problems = [];
	for ( const name of BUILD_CLIENT_FILES ) {
		if ( !existsSync( path.join( gameRoot, name ) ) ) {
			problems.push( `${name} is not in the game root ${gameRoot}` );
		}
	}
	for ( const folder of EXTRACTED_FOLDERS ) {
		if ( !folderHasFiles( path.join( extractedRoot, folder ) ) ) {
			problems.push( `extracted/${folder} is missing or empty; ${PREPARE_HINT}` );
		}
	}
	return problems;
}

/*
================
assertClientInputs

Refuse to start a build whose inputs are missing, listing all of them. A
missing input would otherwise surface deep in a lane, or leave a catalog
silently empty.
================
*/
export function assertClientInputs( build ) {
	const missing = missingClientInputs();
	if ( missing.length ) {
		throw new Error(
			`The ${build} cannot start:\n  ${missing.join( "\n  " )}\nRun \`pnpm assets doctor\` for the full report.`
		);
	}
}

/*
================
readPreparationManifest

The record scripts/prepare_client_resources.py writes, or null for an
extraction made some other way.
================
*/
export function readPreparationManifest() {
	if ( !existsSync( PREPARATION_MANIFEST ) ) return null;
	return JSON.parse( readFileSync( PREPARATION_MANIFEST, "utf8" ) );
}
