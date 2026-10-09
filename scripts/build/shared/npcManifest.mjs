/*
===========================================================================

npcManifest.mjs - the shape of /assets/npc/manifest.json

Many references share one baked model: every clone, level variant and
unique encounter variant aliases its base's BSR. Version 9 stores each
bake result once under resources[bsr] (the GLB, clips, animation and
particle tables, VAT reference) and keeps models[codename] to the fields a
reference owns. Every reader joins the two through npcManifestModels, so
no reader depends on how the file is laid out; the browser's presentation
catalog performs the same join on admission.

===========================================================================
*/

export const NPC_MANIFEST_FORMAT = "sro-mission-npc-models";
// v9: resources keyed by BSR, slim per-reference rows. v8 added the
// characterInfo death models (kind "death", 8E64F0).
export const NPC_MANIFEST_VERSION = 9;

// The fields one reference owns. Everything else in a joined entry is the
// bake result of its BSR and lives in resources.
const REFERENCE_FIELDS = new Set( [
	"codename",
	"refObjId",
	"kind",
	"bsr",
	"eventRain",
	"soundProfileName",
	"deathModel",
	"scalePercent",
	"materialKind",
	"baseCodename",
	"error",
	"requiredBy",
	"riderTransformModes",
	"structureStages",
	"structureSounds",
	"structureDamageEffects"
] );

/*
================
npcManifestModels

The manifest's entries joined with their resources, keyed by codename, in
file order: each value is the reference row over its BSR's bake result.
A row whose bake failed carries no resource and stays as written.
================
*/
export function npcManifestModels( manifest ) {
	const resources = manifest?.resources ?? {};
	const joined = {};
	for ( const [codename, row] of Object.entries( manifest?.models ?? {} ) ) {
		const resource = row?.bsr ? resources[row.bsr] : undefined;
		joined[codename] = resource ? { ...resource, ...row } : row;
	}
	return joined;
}

/*
================
joinedNpcManifest

The whole manifest with its models joined: the shape readers of v8 saw.
================
*/
export function joinedNpcManifest( manifest ) {
	return { ...manifest, models: npcManifestModels( manifest ) };
}

/*
================
splitNpcManifestModels

The inverse of npcManifestModels for the bake: the joined entries become
slim reference rows and one resource per BSR. Two entries of one BSR must
carry the same bake result; a difference is a bake defect, not a choice.
================
*/
export function splitNpcManifestModels( entries ) {
	const models = {};
	const resources = {};
	const resourceJson = new Map();
	for ( const entry of entries ) {
		const row = {};
		const resource = {};
		for ( const [key, value] of Object.entries( entry ) ) {
			if ( REFERENCE_FIELDS.has( key ) ) row[key] = value;
			else resource[key] = value;
		}
		models[entry.codename] = row;
		if ( !entry.bsr || entry.error || Object.keys( resource ).length === 0 ) continue;
		const json = JSON.stringify( resource );
		const prior = resourceJson.get( entry.bsr );
		if ( prior !== undefined && prior !== json ) {
			throw new Error( `[npc] ${entry.bsr}: references of one BSR carry different bake results` );
		}
		resourceJson.set( entry.bsr, json );
		resources[entry.bsr] = resource;
	}
	return { models, resources };
}
