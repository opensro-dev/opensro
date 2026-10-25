/*
===========================================================================

equipmentParticles.mjs - native item particles and private equipment sockets

Ordinary weapons need unweighted marker bones for skill launch points even
when they have no rare glow, and the shoulder garments carry ambient
particles on their own private bones. Publish all three through the
equipment owner, with the port-only enchant sparks beside the rare glows.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { clientV150ResinfoRoot, retailTextdataRoot } from "../world/paths.mjs";
import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { vsroItemGlowRoot } from "./equipmentGlowMetadata.mjs";
import { dataAssetPath, loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { parseCharacterBsr, parseBsk } from "./formats.mjs";

const WEAPON_TYPE = 6;
const DUAL_WEAPON_TYPES = new Set( [ 9, 13 ] );
const HARP_TYPE = 14;
const SHIELD_TYPE = 4;
// Weapon TypeID4 classes that carry their own enchant spark program.
const BOW_TYPES = new Set( [ 6, 12 ] );
const STAFF_TYPES = new Set( [ 10, 11, 15 ] );
// The fortress hammers are siege tools, never enchanted.
const FORTRESS_HAMMER_TYPE = 16;
// A beginner item names the model of its _01_A sibling.
const BEGINNER_SUFFIX = "_DEF";
const RARE_SUFFIX = "_RARE";
const SPARK_ROOT = "system/system_enchant";
// The fallback sparks' lowest reinforcement (equipment-appearance.ts ENCHANT_SPARK_PLUS).
const ENCHANT_SPARK_PLUS = 8;
// ModDataParticle set kind of the always-on ambient set (model-particles.ts).
export const AMBIENT_MODIFIER_KIND = 2;

/*
================
itemRareRows

itemrare.txt's rows for the special-state-two items, by item code: the
authored effect, its bone and its scale.
================
*/
function itemRareRows( byCode ) {
	const out = new Map();
	for (
		const line of fs.readFileSync( path.join( clientV150ResinfoRoot, "itemrare.txt" ), "utf8" ).split( /\r?\n/ )
	) {
		if ( !line.trim() || line.trim().startsWith( "//" ) ) continue;
		const c = line.trim().split( /\s+/ ), item = byCode.get( c[0] );
		if ( !item || item.specialState !== 2 ) continue;
		if (
			c.length !== 5 || !Number.isFinite( Number( c[2] ) ) || Number( c[2] ) <= 0 || c[4] !== "none"
		) throw Error( "Unsupported itemrare row " + line );
		const effectPath = c[1].replaceAll( "\\", "/" ).toLowerCase();
		if ( !effectPath.endsWith( ".efp" ) || effectPath.includes( ".." ) ) {
			throw Error(
				"Invalid special-state effect"
			);
		}
		if ( !out.has( item.code ) ) out.set( item.code, [] );
		out.get( item.code ).push( { effectPath, bone: c[3], scale: Math.fround( Number( c[2] ) / 100 ) } );
	}
	return out;
}

/*
================
equipmentParticleCatalog

Only special-state two activates itemrare's authored particle descriptors.
================
*/
export function equipmentParticleCatalog( records = loadEquipmentRecords( retailTextdataRoot ) ) {
	const byCode = new Map( [ ...records.values() ].map( r => [ r.code, r ] ) ), out = {};
	for ( const [code, rows] of itemRareRows( byCode ) ) {
		out[byCode.get( code ).id] = rows.map( row => ({ ...row, offset: [ 0, 0, 0 ], root: false }) );
	}
	return out;
}

/*
================
enchantSparkEffect

The spark program for one weapon or shield class, or null for an item
that is never enchanted.
================
*/
function enchantSparkEffect( record ) {
	if ( record.tid[2] === SHIELD_TYPE ) return SPARK_ROOT + "shield_a_01.efp";
	if ( record.tid[2] !== WEAPON_TYPE || record.tid[3] === FORTRESS_HAMMER_TYPE ) return null;
	if ( BOW_TYPES.has( record.tid[3] ) ) return SPARK_ROOT + "bow_a_01.efp";
	if ( record.tid[3] === HARP_TYPE ) return SPARK_ROOT + "harp_a_01.efp";
	if ( STAFF_TYPES.has( record.tid[3] ) ) return SPARK_ROOT + "staff_a_01.efp";
	return SPARK_ROOT + "_a_01.efp";
}

/*
================
officialEnchantSparks

Port-only, not native: the later client's per-item spark table
(resinfo/itemoptionefp.txt, imported by scripts/import_vsro_item_glow.py):
item codename, effect, scale percent, bone, offset ("none"), and the
lowest reinforcement that draws it. Null when the table is not imported.
================
*/
function officialEnchantSparks( byCode, vsroRoot ) {
	const file = path.join( vsroRoot, "itemoptionefp.txt" );
	if ( !fs.existsSync( file ) ) return null;
	const out = {};
	for ( const line of fs.readFileSync( file, "latin1" ).split( /\r?\n/ ) ) {
		if ( !line.trim() || line.trim().startsWith( "//" ) ) continue;
		const c = line.trim().split( /\t/ ), item = byCode.get( c[0] );
		// A later-client item the v1.150 data does not have draws nothing.
		if ( !item || item.slot === null ) continue;
		// The field count is checked before any field is read, so a short
		// row fails with this error instead of a TypeError on a missing one.
		if ( c.length < 6 ) throw Error( "Unsupported itemoptionefp row " + line );
		const effectPath = c[1].replaceAll( "\\", "/" ).toLowerCase(),
			scale = Number( c[2] ),
			threshold = Number( c[5] );
		if (
			!effectPath.endsWith( ".efp" ) || effectPath.includes( ".." ) ||
			!Number.isFinite( scale ) || scale <= 0 || !c[3] || c[4] !== "none" ||
			!Number.isInteger( threshold ) || threshold < 1 || threshold > 255
		) throw Error( "Unsupported itemoptionefp row " + line );
		(out[item.id] ??= []).push( {
			effectPath,
			bone: c[3],
			scale: Math.fround( scale / 100 ),
			offset: [ 0, 0, 0 ],
			root: false,
			threshold
		} );
	}
	return out;
}

/*
================
equipmentEnchantSparks

Port-only, not native (owner, 2026-10-13): the enchant sparks the
Experimental "Enhanced item glow" row draws on a reinforced weapon or
shield. v1.150 ships system_enchant*_a_01.efp in Particles.pk2/system, but
neither SRO_Client.exe nor any v1.150 resinfo or textdata table names
them, so the original never drew them. With the later client's
itemoptionefp.txt imported, each item takes its authored effect, bone,
scale and threshold. Without it, each item wears its class's program on
the bones and at the scales itemrare.txt authors for its rare twin (the
same model): its own itemrare row, code_RARE, or for a beginner _DEF item
its _01_A sibling's twin when the model matches; an item without a twin
gets none, and the threshold is ENCHANT_SPARK_PLUS.
================
*/
export function equipmentEnchantSparks(
	records = loadEquipmentRecords( retailTextdataRoot ),
	vsroRoot = vsroItemGlowRoot
) {
	const byCode = new Map( [ ...records.values() ].map( r => [ r.code, r ] ) );
	const official = officialEnchantSparks( byCode, vsroRoot );
	if ( official ) return official;
	const rare = itemRareRows( byCode ), out = {};
	for ( const record of records.values() ) {
		if ( record.slot === null ) continue;
		const effectPath = enchantSparkEffect( record );
		if ( !effectPath ) continue;
		let twin = rare.get( record.code ) ?? rare.get( record.code + RARE_SUFFIX );
		if ( !twin && record.code.endsWith( BEGINNER_SUFFIX ) ) {
			const base = byCode.get( record.code.slice( 0, -BEGINNER_SUFFIX.length ) );
			if ( base && base.resolvedModel === record.resolvedModel ) twin = rare.get( base.code + RARE_SUFFIX );
		}
		if ( !twin ) continue;
		const bones = new Map();
		for ( const row of twin ) if ( !bones.has( row.bone ) ) bones.set( row.bone, row.scale );
		out[record.id] = [ ...bones ].map( ( [bone, scale] ) => ({
			effectPath,
			bone,
			scale,
			offset: [ 0, 0, 0 ],
			root: false,
			threshold: ENCHANT_SPARK_PLUS
		}) );
	}
	return out;
}

/*
================
equipmentSources

The native resources of one item on one body: 870040 pairs EU axes and
daggers, and a female harp uses its authored _f resource.
================
*/
export function equipmentSources( record, female ) {
	const model = record.resolvedModel;
	if ( !model ) return [];
	if ( record.tid[2] === WEAPON_TYPE && DUAL_WEAPON_TYPES.has( record.tid[3] ) ) {
		return [ model.replace( /\.bsr$/, "_r.bsr" ), model.replace( /\.bsr$/, "_l.bsr" ) ];
	}
	if ( record.tid[2] === WEAPON_TYPE && record.tid[3] === HARP_TYPE && female ) {
		return [ model.replace( /\.bsr$/, "_f.bsr" ) ];
	}
	return [ model ];
}

/*
================
equipmentAmbientParticles

The ambient ModDataParticle sets authored inside worn equipment resources,
by source path. In v1.150 only the 36 shoulder garments carry one: their
orbs and crystals glow through item_equip_*shoulder*.efp on bones of the
garment's private skeleton.

INFERENCE: an equipped item is a CRes like any other model, so its ambient
set starts through the same ACD4D0 -> AE3840 path as a body's or a drop's;
those programs have no other owner in the data.
================
*/
export function equipmentAmbientParticles( records = loadEquipmentRecords( retailTextdataRoot ) ) {
	const out = new Map();
	for ( const record of records.values() ) {
		if ( record.slot === null ) continue;
		for ( const female of [ false, true ] ) {
			for ( const source of equipmentSources( record, female ) ) {
				if ( out.has( source ) || !fs.existsSync( dataAssetPath( source ) ) ) continue;
				const bsr = parseCharacterBsr( fs.readFileSync( dataAssetPath( source ) ), source );
				out.set( source, bsr.particleModifiers.filter( m => m.kind === AMBIENT_MODIFIER_KIND ) );
			}
		}
	}
	for ( const [source, modifiers] of out ) if ( !modifiers.length ) out.delete( source );
	return out;
}

/*
================
publishEquipmentParticleMetadata

ABC680 links the private hierarchy; A981A0 searches its markers for skill
attachments. Mesh weights and rarity do not determine whether a bone exists.
Namespaced handles let item particles continue to select an exact branch.
A branch also carries its resource's ambient particle set, whose bones are
private nodes of that branch; an animated resource is an auxiliary handle
instead and carries the set itself.
================
*/
export async function publishEquipmentParticleMetadata( dress, vsroRoot = vsroItemGlowRoot ) {
	const records = loadEquipmentRecords( retailTextdataRoot ),
		particles = equipmentParticleCatalog( records ),
		sparks = equipmentEnchantSparks( records, vsroRoot ),
		ambient = equipmentAmbientParticles( records ),
		cache = new Map();
	dress.specialGlows = particles;
	dress.enchantSparks = sparks;
	for ( const [id, record] of records ) {
		// A shield needs its private branch for its sparks' bones too.
		const effects = [ ...(particles[id] ?? []), ...(sparks[id] ?? []) ], visual = dress.equipment[id];
		if ( !visual ) continue;
		for ( const [body, entry] of Object.entries( visual.bodies ) ) {
			// An auxiliary entry carries its own ambient set on its own bones.
			if ( !entry || entry.auxiliary ) continue;
			const paths = equipmentSources( record, body.endsWith( "_W" ) );
			if ( record.tid[2] !== WEAPON_TYPE && effects.length === 0 && !paths.some( p => ambient.has( p ) ) ) {
				continue;
			}
			if ( entry.parts.length !== paths.length ) throw Error( "Equipment private branch/part mismatch " + id );
			entry.branches = [];
			for ( let i = 0; i < paths.length; i++ ) {
				const source = paths[i];
				let branch = cache.get( source );
				if ( branch === undefined ) {
					const bsr = parseCharacterBsr( await loadDataAsset( source ), source );
					if ( !bsr.skeletonPath || !bsr.skeletonAttachBone ) branch = null;
					else {
						// An animated resource is its own auxiliary handle
						// (buildEquipmentVisuals animatedEquipmentEntry); a
						// branch here would freeze its bones at the bind pose.
						if ( bsr.animationPaths.length ) {
							throw Error( "Unresolved animated equipment branch " + source );
						}
						const skeleton = parseBsk( await loadDataAsset( bsr.skeletonPath ), bsr.skeletonPath );
						branch = {
							attachBone: bsr.skeletonAttachBone,
							nodes: skeleton.bones.map( b => ({
								name: b.name,
								parent: b.parentIndex,
								translation: [ b.local.t[0], b.local.t[1], -b.local.t[2] ],
								rotation: [ -b.local.q[0], -b.local.q[1], b.local.q[2], b.local.q[3] ],
								scale: [ 1, 1, 1 ]
							}) ),
							...(ambient.has( source ) ? { particleModifiers: ambient.get( source ) } : {})
						};
					}
					if ( ambient.has( source ) ) {
						for ( const entry of ambient.get( source ).flatMap( m => m.entries ) ) {
							if ( !branch?.nodes.some( n => n.name === entry.boneName ) ) {
								throw Error(
									"Missing native ambient particle socket " + source + ":" + entry.boneName
								);
							}
						}
					}
					cache.set( source, branch );
				}
				for ( const effect of particles[id] ?? [] ) {
					if ( !branch?.nodes.some( n => n.name === effect.bone ) ) {
						throw Error( "Missing native special-state socket " + source + ":" + effect.bone );
					}
				}
				// Port-only sparks name the later client's bones; one a v1.150
				// model lacks is left out rather than failing the build.
				if ( sparks[id] ) {
					sparks[id] = sparks[id].filter( effect => branch?.nodes.some( n => n.name === effect.bone ) );
					if ( !sparks[id].length ) delete sparks[id];
				}
				if ( branch ) entry.branches.push( { ...branch, part: entry.parts[i] } );
			}
		}
	}
	return {
		items: Object.keys( particles ).length,
		sparks: Object.keys( sparks ).length,
		ambient: ambient.size,
		sources: cache.size
	};
}
