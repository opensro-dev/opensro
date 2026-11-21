/*
===========================================================================

effect-programs.ts - where one EasyFX effect program and its meshes live

An effect's identity is its model address, /assets/effects/programs.json#
followed by the encoded EFP path. Actors, residency and tests name effects by
that address. Since #3951 no single programs.json is published: each effect
is its own file, and each BMS mesh it draws is its own file, so one request
carries one effect whatever the content grows to. This module is the one
owner of both spellings: the address the client names and the files the
build publishes and the asset worker reads.

It imports nothing, so the build (scripts/build/effects) and Node tooling
read the same rule the worker ships.

===========================================================================
*/

// The model address of an effect: this prefix plus the encoded EFP path.
export const EFFECT_PROGRAM_PREFIX = "/assets/effects/programs.json#";
// Matches an effect address with or without the /assets mount.
const EFFECT_PROGRAM_MARK = "/effects/programs.json#";
// The published per-effect and per-mesh trees.
const EFFECT_PROGRAM_ROOT = "/assets/effects/programs/";
const EFFECT_MESH_ROOT = "/assets/effects/meshes/";
// The build's whole-catalogue record for tooling (never read at runtime).
export const EFFECT_PROGRAM_INDEX = "/assets/effects/programs/index.json";
// The published layout. Version 4 is the per-effect split.
export const EFFECT_PROGRAM_FORMAT = "sro-jmx-easyfx-programs";
export const EFFECT_PROGRAM_VERSION = 4;
// Native ProcessManager publishes one EasyFX step for each crossed 50 ms
// quantum (data_f0c90c), so EFP timeline frames are 20 Hz.
export const EFFECT_FRAMES_PER_SECOND = 20;
// One effect's BMS meshes, read beside its program file: the cap on one mesh
// file (every reader passes it to its read) and on one effect's whole set.
export const EFFECT_MESH_BYTES = 16777216;
export const EFFECT_MESHES_BYTES = 33554432;

/*
================
effectProgramHeader

The fields every published effect file and the index open with.
================
*/
export function effectProgramHeader(): { format: string; version: number; framesPerSecond: number; } {
	return {
		format: EFFECT_PROGRAM_FORMAT,
		version: EFFECT_PROGRAM_VERSION,
		framesPerSecond: EFFECT_FRAMES_PER_SECOND
	};
}

/*
================
effectProgramKey

A catalogue key: forward slashes, lower case (the archive is case-blind).
================
*/
export function effectProgramKey( path: string ): string {
	return path.replaceAll( "\\", "/" ).toLowerCase();
}

/*
================
effectProgramModel

The model address an actor names to draw the effect at path.
================
*/
export function effectProgramModel( path: string ): string {
	return EFFECT_PROGRAM_PREFIX + encodeURIComponent( path );
}

/*
================
isEffectProgramModel

Whether a model address names an effect program.
================
*/
export function isEffectProgramModel( model: string ): boolean {
	return model.includes( EFFECT_PROGRAM_MARK );
}

/*
================
effectProgramFile

The published file of one effect, by its key.
================
*/
export function effectProgramFile( key: string ): string {
	return EFFECT_PROGRAM_ROOT + safeKey( key ) + ".json";
}

/*
================
effectMeshFile

The published file of one BMS mesh, by its key.
================
*/
export function effectMeshFile( key: string ): string {
	return EFFECT_MESH_ROOT + safeKey( key ) + ".json";
}

/*
================
effectProgramIndexFiles

Every published file an index names: one per effect (archive-absent ones
included) and one per mesh. The packs, the delivery check and the portrait
fingerprint all enumerate the closure through this.
================
*/
export function effectProgramIndexFiles( index: {
	effects: readonly string[];
	nativeUnavailable: { effects: readonly string[]; };
	meshes: readonly string[];
} ): string[] {
	return [
		...[ ...index.effects, ...index.nativeUnavailable.effects ].map( effectProgramFile ),
		...index.meshes.map( effectMeshFile )
	];
}

/*
================
safeKey

The key as a path segment; a key that cannot be one is refused, so a new
archive name fails the build instead of a request.
================
*/
function safeKey( key: string ): string {
	const folded = effectProgramKey( key );
	// A key becomes a URL path: these would end it, or decode into another.
	if ( /[#?%\u0000-\u001f]/.test( folded ) || folded.includes( ".." ) ) {
		throw new Error( `Unsafe effect program key ${key}` );
	}
	return folded;
}
