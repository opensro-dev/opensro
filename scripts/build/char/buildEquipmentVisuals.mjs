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
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { retailTextdataRoot, clientV150ResinfoRoot } from "../world/paths.mjs";
import { loadAvatarVisualOverrides } from "./avatarVisualOverrides.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import {
	buildAuxiliaryAvatarSets,
	buildAuxiliarySet,
	buildItemSetGlb,
	chooseDonor,
	createDonorLoader
} from "./itemAttachments.mjs";
import { dataRoot, loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { runGraph } from "../graph/graph.mjs";
import { noteRead, trackReads } from "../graph/reads.mjs";
import { openStore } from "../graph/store.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { buildStatePath } from "../../lib/generatedRoot.mjs";
import { parseCharacterBsr } from "./formats.mjs";
import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { equipmentGlowCatalog, equipmentGlowModelIds, equipmentGlowGlb } from "./equipmentGlowMetadata.mjs";
import { publishEquipmentParticleMetadata } from "./equipmentParticles.mjs";
import { imageSourceRoot, publicAssetsRoot, publicRoot } from "../world/paths.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
import { publishBytesAtomically } from "../shared/atomicPublish.mjs";
import { MOONLIT_MESHES, moonlitGlb, moonlitModelPath } from "./moonlitThiefSuit.mjs";

// The equipment slots equipmentGlowModelIds reads glows for.
const GLOW_SLOT_WEAPON = 6;
const GLOW_SLOT_SHIELD = 7;

const MODULE_FILE = fileURLToPath( import.meta.url );
// The trees an item set's build reads: extracted game data and converted images.
const ITEM_SET_READ_ROOTS = { data: dataRoot, images: imageSourceRoot };
// Weapon and shield sets are written once their glows are known; their plain
// GLBs are kept here between builds so a hit can hand them to that step.
const PLAIN_ITEM_SET_ROOT = buildStatePath( "equipment-plain" );

/*
================
itemSetAction

One equipment item set (a body and its item BSRs) as a cached action
(graph/action.mjs). Its reads are discovered as it runs. The donor body
is chosen before the action (chooseDonor reads the roster's textdata,
outside every root), so its BSR path is a parameter: a roster change
that moves a body to another donor moves the key. The donor's own reads
come from donorWithReads, recorded once per donor BSR and replayed into
every set that wears it, so a change to the donor's files misses them
all. A
held set (a weapon or shield) writes its plain GLB under
PLAIN_ITEM_SET_ROOT for the glow step instead of publishing it. The
result is the catalog entry ({ glb, parts, covers, sources }).
================
*/
export function itemSetAction( set, donorWithReads ) {
	const fileName = `${set.key.toLowerCase()}.glb`;
	return {
		name: `equipment:${set.key}`,
		outputRoot: set.held ? PLAIN_ITEM_SET_ROOT : path.join( publicAssetsRoot, "char", "equipment" ),
		outputs: [ fileName ],
		code: [ MODULE_FILE ],
		params: { key: set.key, donorBsr: set.donor.bsrPath, paths: set.paths, held: set.held },
		discoverRoots: ITEM_SET_READ_ROOTS,
		run: async ( { outputRoot } ) => {
			const { donor: found, reads } = await donorWithReads( set.donor );
			for ( const file of reads ) noteRead( file );
			const entry = await buildItemSetGlb( {
				tag: "equipment",
				key: set.key,
				...found,
				pieces: set.paths.map( ( itemBsrPath, i ) => ({ part: `EQ${i}`, itemBsrPath }) ),
				outSubdir: "equipment",
				...(set.held ?
					{ write: ( _diskPath, bytes ) => writePlainItemSet( path.join( outputRoot, fileName ), bytes ) } :
					{})
			} );
			if ( !entry ) throw Error( `Empty equipment conversion ${set.code}: ${set.paths}` );
			return entry;
		}
	};
}

/*
================
glowedSetAction

A held set's published GLB: its plain GLB (declared input) with the
enhancement glows of ids embedded (equipmentGlowGlb), or unchanged when
no row of the catalog glows on it. The ids and their catalog rows are
the parameters; the glow textures it reads are discovered.
================
*/
function glowedSetAction( fileName, ids, glows ) {
	const glowIds = ids ? [ ...ids ] : [];
	return {
		name: `equipment-glow:${fileName}`,
		inputRoot: PLAIN_ITEM_SET_ROOT,
		inputs: [ fileName ],
		outputRoot: path.join( publicAssetsRoot, "char", "equipment" ),
		outputs: [ fileName ],
		code: [ MODULE_FILE ],
		params: { ids: glowIds, rows: glowIds.map( id => [ id, glows.get( Number( id ) ) ?? null ] ) },
		discoverRoots: ITEM_SET_READ_ROOTS,
		run: async ( { outputRoot } ) => {
			const plain = readFileSync( path.join( PLAIN_ITEM_SET_ROOT, fileName ) );
			writeFileSync(
				path.join( outputRoot, fileName ),
				glowIds.length ? equipmentGlowGlb( plain, glowIds, glows ) : plain
			);
		}
	};
}

/*
================
writePlainItemSet

A held set's plain GLB, into the action's output folder (the graph made
it); the store hashes the file once the run returns.
================
*/
function writePlainItemSet( file, bytes ) {
	writeFileSync( file, bytes );
}

/*
================
animatedEquipmentEntry

A worn resource with its own skeleton AND animation is drawn as its own
animated handle, like an avatar's auxiliary resource. In v1.150 that is the
18 EU shoulder garments, whose crystals spin through clothes_sa.ban, and
the two Santa hat avatars. Baking it
onto the wearer would freeze it. Returns null for every other resource.
Weapons never take this path: their private markers are skill launch
points (equipmentParticles.mjs refuses an animated weapon branch).

INFERENCE: the item is linked as a CRTBranch under its .bsr socket
(ABC680) and its installation plays the resource's own BAN, as an avatar's
second handle does (8E9DD0).
================
*/
async function animatedEquipmentEntry( row, paths, cache ) {
	if ( paths.length !== 1 || row.slot === GLOW_SLOT_WEAPON || row.slot === GLOW_SLOT_SHIELD ) return null;
	const source = paths[0], key = "auxiliary:" + source;
	if ( cache.has( key ) ) return cache.get( key );
	const bsr = parseCharacterBsr( await loadDataAsset( source ), source );
	let entry = null;
	if ( bsr.skeletonPath && bsr.skeletonAttachBone && bsr.animationPaths.length ) {
		const name = createHash( "sha256" ).update( source ).digest( "hex" ).slice( 0, 20 ),
			auxiliary = await buildAuxiliarySet( source, `/assets/char/equipment/equipment_aux_${name}.glb`, "EQ0" );
		// The body draws nothing of its own; the auxiliary handle is the item.
		entry = { glb: auxiliary.glb, parts: [], covers: {}, sources: auxiliary.sources, auxiliary };
	}
	cache.set( key, entry );
	return entry;
}

/*
================
publishMoonlitVariants

Port-only, not native: each level-20 thief suit's body entry gains a
moonlit entry, the higher suit's retail mesh with the Moonlit Ash texture
(moonlitThiefSuit.mjs). The higher suit's own entry is untouched, so its
wearers keep the retail look. One variant model per mesh and body.
================
*/
async function publishMoonlitVariants( records ) {
	const meshes = new Map(), variants = new Map();
	for ( const record of Object.values( records ) ) {
		for ( const [body, entry] of Object.entries( record.bodies ) ) {
			if ( entry ) meshes.set( record.model + "|" + body, entry );
		}
	}
	for ( const record of Object.values( records ) ) {
		const moonlit = MOONLIT_MESHES.get( record.model );
		if ( !moonlit ) continue;
		for ( const [body, entry] of Object.entries( record.bodies ) ) {
			if ( !entry ) continue;
			const target = meshes.get( moonlit.mesh + "|" + body );
			if ( !target || target.auxiliary ) throw Error( `Moonlit: no static ${moonlit.mesh} model for ${body}` );
			let variant = variants.get( target.glb );
			if ( !variant ) {
				variant = { ...target, glb: moonlitModelPath( target.glb ) };
				await publishBytesAtomically(
					path.join( publicRoot, variant.glb ),
					moonlitGlb( readPublishedAssetBytesSync( target.glb, publicRoot ), moonlit.warmCloth )
				);
				variants.set( target.glb, variant );
			}
			entry.moonlit = variant;
		}
	}
	if ( variants.size !== MOONLIT_MESHES.size ) throw Error( "Moonlit: expected one model per level-20 thief mesh" );
	console.log( `[equipment] ${variants.size} Moonlit Ash thief suit models` );
}

/*
================
buildItemSets

Runs every unique item set as an action and fills its shared entry.
Published sets are claimed; held sets go to hold with their plain GLB's
name under PLAIN_ITEM_SET_ROOT (glowedSetAction). Returns how many were
hits.
================
*/
async function buildItemSets( sets, hold, options ) {
	const loadDonor = createDonorLoader(), donors = new Map();
	// The loader reads a donor's BSR and skeleton once per donor BSR;
	// trackReads keeps those reads, under the same key, so every set that
	// wears the donor records them, whichever body chose it.
	const donorWithReads = ( donor ) => {
		if ( !donors.has( donor.bsrPath ) ) {
			donors.set(
				donor.bsrPath,
				trackReads( () => loadDonor( donor ) ).then( ( { result, reads } ) => ({ donor: result, reads }) )
			);
		}
		return donors.get( donor.bsrPath );
	};
	const store = options.store ?? (await openStore());
	let results;
	try {
		results = await runGraph( sets.map( set => itemSetAction( set, donorWithReads ) ), { store } );
	} finally {
		if ( !options.store ) await store.shutdown();
	}
	let stored = 0;
	for ( const set of sets ) {
		const outcome = results[`equipment:${set.key}`];
		if ( outcome.hit ) stored += 1;
		Object.assign( set.entry, outcome.result );
		const fileName = `${set.key.toLowerCase()}.glb`;
		const diskPath = path.join( publicAssetsRoot, "char", "equipment", fileName );
		if ( set.held ) hold( diskPath, fileName, set.entry.glb );
		else claimPublicFile( diskPath );
	}
	return stored;
}

/*
================
buildEquipmentVisuals

Converts every equippable and avatar item for each body it fits, sharing one
conversion between items with the same native source paths, then builds the
default and fortress wear and the enhancement glow metadata.
================
*/
export async function buildEquipmentVisuals( dress, options = {} ) {
	dress.defaultWearLanguage = defaultWearLanguage();
	const rows = loadEquipmentRecords( retailTextdataRoot ),
		roster = resolveRoster( retailTextdataRoot ).resolved,
		cache = new Map(),
		out = {};
	// Separate native table: never inherit this from an item's linked model row.
	dress.avatarVisualOverrides = loadAvatarVisualOverrides(
		path.join( clientV150ResinfoRoot, "avataritemdata.txt" ),
		rows
	);
	dress.avatarAuxiliary = await buildAuxiliaryAvatarSets( dress.avatarVisualOverrides );
	const cacheKey = ( body, paths ) => body + ":" + paths.join( "|" );
	// Weapons and shields may carry enhancement glows, known only once every
	// row is mapped: their GLBs are held and written once, glow included,
	// instead of written plain and rewritten with the glow on every build.
	const glows = equipmentGlowCatalog(), held = new Map();
	const hold = ( diskPath, fileName, publicPath ) => held.set( publicPath, { diskPath, fileName } );
	// Items sharing native source paths share one conversion.
	const sets = [];
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
				const animated = await animatedEquipmentEntry( row, paths, cache );
				if ( animated ) {
					record.bodies[body] = animated;
					continue;
				}
				const key = cacheKey( body, paths );
				let entry = cache.get( key );
				if ( !entry ) {
					// A placeholder every record of this set shares; the set's
					// action fills it once the graph has run.
					const donor = chooseDonor( roster, race, sex );
					if ( !donor ) throw Error( `Missing donor ${body}` );
					entry = {};
					cache.set( key, entry );
					sets.push( {
						entry,
						body,
						donor,
						paths,
						code: row.code,
						key: body + "_" + createHash( "sha256" ).update( key ).digest( "hex" ).slice( 0, 20 ),
						held: row.slot === GLOW_SLOT_WEAPON || row.slot === GLOW_SLOT_SHIELD
					} );
				}
				record.bodies[body] = entry;
			}
		}
	}
	const stored = await buildItemSets( sets, hold, options );
	console.log(
		`[equipment] ${Object.keys( out ).length} native item mappings; ${sets.length} additional models ` +
			`(${stored} from the action store)`
	);
	await buildDefaultWear( dress );
	// Preserve native enhancement metadata in normal rebuilds as well as focused
	// material publication. Reused and freshly converted weapons share this gate.
	const glowing = equipmentGlowModelIds( out, glows );
	const glowed = [ ...held ].map( ( [asset, { fileName }] ) =>
		glowedSetAction( fileName, glowing.get( asset ), glows )
	);
	const store = options.store ?? (await openStore());
	try {
		await runGraph( glowed, { store } );
	} finally {
		if ( !options.store ) await store.shutdown();
	}
	for ( const [asset, { diskPath }] of held ) {
		claimPublicFile( diskPath );
		glowing.delete( asset );
	}
	// A glowing model this pass did not build keeps its published bytes.
	for ( const [asset, ids] of glowing ) {
		await publishBytesAtomically(
			path.join( publicRoot, asset ),
			equipmentGlowGlb( readPublishedAssetBytesSync( asset, publicRoot ), ids, glows )
		);
	}
	await publishMoonlitVariants( out );
	const metadata = { ...dress, equipment: out };
	await publishEquipmentParticleMetadata( metadata );
	dress.specialGlows = metadata.specialGlows;
	dress.enchantSparks = metadata.enchantSparks;
	return out;
}
