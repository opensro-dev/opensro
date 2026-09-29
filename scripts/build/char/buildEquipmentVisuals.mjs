/*
===========================================================================

buildEquipmentVisuals.mjs - the per-item equipment catalog (dress.equipment)

One record per itemdata row, keyed by RefItemID, carrying its codename and
one converted attachment per wearable body. It is the only catalog every
appearance path uses: RefItemIDs from the world, the character list and
creation, and codenames from the reference appearance and the title crowd
(the native client resolves those through GlobalDataManager_GetItemRecordByCodeName).

===========================================================================
*/
import { buildDefaultWear } from "./buildDefaultWear.mjs";
import { defaultWearLanguage } from "./defaultWearPolicy.mjs";
import { createHash } from "node:crypto";
import path from "node:path";
import { retailTextdataRoot, clientV150ResinfoRoot } from "../world/paths.mjs";
import { loadAvatarVisualOverrides } from "./avatarVisualOverrides.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import { buildItemSetGlb, createDonorPool, buildAuxiliaryAvatarSets } from "./itemAttachments.mjs";
import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { equipmentGlowCatalog, equipmentGlowModelIds, equipmentGlowGlb } from "./equipmentGlowMetadata.mjs";
import { publishEquipmentParticleMetadata } from "./equipmentParticles.mjs";
import { publicRoot } from "../world/paths.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
import { publishBytesAtomically } from "../shared/atomicPublish.mjs";

/*
================
buildEquipmentVisuals

Converts every equippable and avatar item for each body it fits, sharing one
conversion between items with the same native source paths, then builds the
default and fortress wear and the enhancement glow metadata.
================
*/
export async function buildEquipmentVisuals( dress ) {
	dress.defaultWearLanguage = defaultWearLanguage();
	const rows = loadEquipmentRecords( retailTextdataRoot ),
		getDonor = createDonorPool( resolveRoster( retailTextdataRoot ).resolved ),
		cache = new Map(),
		out = {};
	// Separate native table: never inherit this from an item's linked model row.
	dress.avatarVisualOverrides = loadAvatarVisualOverrides(
		path.join( clientV150ResinfoRoot, "avataritemdata.txt" ),
		rows
	);
	dress.avatarAuxiliary = await buildAuxiliaryAvatarSets( dress.avatarVisualOverrides );
	const cacheKey = ( body, paths ) => body + ":" + paths.join( "|" );
	// Items sharing native source paths share one conversion.
	let built = 0;
	for ( const row of rows.values() ) {
		const avatar = row.tid[0] === 3 && row.tid[1] === 1 && row.tid[2] === 13;
		const record = {
			code: row.code,
			slot: row.slot,
			armorClass: row.armorClass,
			thiefSuit: row.thiefSuit,
			visualMask: row.visualMask,
			visualPriority: row.visualPriority,
			source: row.modelSource,
			model: row.resolvedModel,
			...(avatar ? { avatarSlot: row.tid[3] - 1 } : {}),
			bodies: {}
		};
		out[row.id] = record;
		if ( row.slot === null && !avatar ) continue;
		for ( const race of [ "CH", "EU" ] ) {
			for ( const sex of [ "M", "W" ] ) {
				if ( row.country !== 3 && row.country !== (race === "CH" ? 0 : 1) ) continue;
				if ( row.sex !== 2 && row.sex !== (sex === "M" ? 1 : 0) ) continue;
				const body = `${race}_${sex}`, model = row.resolvedModel;
				if ( !model ) {
					record.bodies[body] = null;
					continue;
				}
				// 870040: EU axes/daggers are paired; female harps use their authored _f resource.
				const dual = row.tid[2] === 6 && [ 9, 13 ].includes( row.tid[3] );
				const paths = dual ?
					[ model.replace( /\.bsr$/, "_r.bsr" ), model.replace( /\.bsr$/, "_l.bsr" ) ] :
					[
						row.tid[2] === 6 && row.tid[3] === 14 && sex === "W" ?
							model.replace( /\.bsr$/, "_f.bsr" ) :
							model
					];
				const key = cacheKey( body, paths );
				let entry = cache.get( key );
				if ( !entry ) {
					const donor = await getDonor( race, sex );
					if ( !donor ) throw Error( `Missing donor ${body}` );
					entry = await buildItemSetGlb( {
						tag: "equipment",
						key: body + "_" + createHash( "sha256" ).update( key ).digest( "hex" ).slice( 0, 20 ),
						...donor,
						pieces: paths.map( ( itemBsrPath, i ) => ({ part: `EQ${i}`, itemBsrPath }) ),
						outSubdir: "equipment"
					} );
					if ( !entry ) throw Error( `Empty equipment conversion ${row.code}: ${paths}` );
					cache.set( key, entry );
					built++;
				}
				record.bodies[body] = entry;
			}
		}
	}
	console.log( `[equipment] ${Object.keys( out ).length} native item mappings; ${built} additional models` );
	await buildDefaultWear( dress );
	// Preserve native enhancement metadata in normal rebuilds as well as focused
	// material publication. Reused and freshly converted weapons share this gate.
	const glows = equipmentGlowCatalog();
	for ( const [asset, ids] of equipmentGlowModelIds( out, glows ) ) {
		await publishBytesAtomically(
			path.join( publicRoot, asset ),
			equipmentGlowGlb( readPublishedAssetBytesSync( asset, publicRoot ), ids, glows )
		);
	}
	const metadata = { ...dress, equipment: out };
	await publishEquipmentParticleMetadata( metadata );
	dress.specialGlows = metadata.specialGlows;
	return out;
}
