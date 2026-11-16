/*
===========================================================================

buildNpcModelAssets.mjs - bake the mission NPC, monster and COS models

Bake the mission NPC roster's models to GLB, mirroring buildRoster.mjs for
the title-crowd chars: one GLB per NPC model + a manifest keyed by
codename. The runtime (ciCharactorDrawVisualSystem.ts, the remote-entity
slice) publishes only these authored models; the WIP shadow world stays
the data authority (position/yaw/lifecycle), the GLB is twin-side visuals.

The monster roster is DERIVED at bake time from the server's spawnable
join (sro-evidence creatable-monsters; see npcModelRoster.mjs) - the same set the
bootstrap refObjSnapshot seeds per session, so bake coverage tracks the
server mechanically. NPCs come from sro-evidence spawnable-npcs, which exports
the unique RefObj types in mission.LoadNpcWorldRoster instead of requiring a
second hand-maintained list. Position duplicates remain runtime instances,
not duplicate model builds.

NPC resources are media-driven rather than assumed to share one animation
shape. Smith and Advice expose state 0 idle, which is exported as "stand";
the fixed gacha machine has no animation set and is emitted with an explicit
staticPose contract. Mission NPCs remain stationary (movement plan rev. 62).

MOB_* .bsr files are DIFFERENT: they carry a full locomotion/combat set
(mangnyang.bsr: 15 clips over one "default" animation set — stand01 stateId
0, walk stateId 1, run stateId 7, attacks/damage/die/down; MONSTER-LIVE Q3
finding, probe temp/asset-probe-bsr-anim.mjs). Monsters assemble with the
native default animation-set state ids now pinned by the complete spawnable
BSR census: stand=0, walk=1, run=7. Those exact records select the GLB
movement clips and carry their BSR ModDataSound cursor metadata.

Output:
  .generated/client-public/assets/npc/<native path below res/>.glb
  .generated/client-public/assets/npc/manifest.json (codename -> glb + meta)
  .generated/client-public/assets/npc/animation-catalog.json (BSR-authored lab data)

Textures: .ddj under prim/mtrl are converted by scripts/convert_images.py
(same pass buildRoster runs); reuse --skip-textures when they're in.

===========================================================================
*/

import { publishCrowEgg } from "./crowEgg.mjs";
import { loadBoothModelRoster } from "./boothModelRoster.mjs";
import { bakeNpcSecondaryResources } from "./npcSecondaryResources.mjs";
import { preserveFreshNpcVatReferences } from "./buildNpcVatAssets.mjs";
import fs from "node:fs";
import { parseWeatherEvents } from "./weatherEvents.mjs";
import path from "node:path";
import { loadCharacterDataRows } from "./resolveCharRoster.mjs";
import {
	enabledCosReferences,
	loadFortressStructureRoster,
	loadModelledMobRoster,
	loadSpawnableNpcRoster
} from "./npcModelRoster.mjs";
import { parseStructureEffects } from "./structureEffects.mjs";
import { openStore } from "../graph/store.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { convertTextureTrees } from "../shared/convertImagesRunner.mjs";
import { readJsonOrNullSync, writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { loadOptionalDataAsset } from "../shared/optionalDataAsset.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import {
	claimResourceOutput,
	readPreviousResourceGlbPaths,
	removeSupersededResourceOutputs,
	resourceGlbOutput
} from "./resourceGlbOutput.mjs";
import { gameRoot, publicAssetsRoot, publicRoot, retailTextdataRoot } from "../world/paths.mjs";
import { NPC_MANIFEST_FORMAT, NPC_MANIFEST_VERSION, splitNpcManifestModels } from "../shared/npcManifest.mjs";
import {
	characterInfoResolver,
	expandCharacterInfoCodenames,
	loadCharacterInfo,
	normalizeCharacterInfoBsrPath
} from "../shared/characterInfo.mjs";
import { BODY_REQUIRED_STATES, bakeTables, cachedBake, DEATH_REQUIRED_STATES } from "./npcResourceBake.mjs";

const textdataDir = retailTextdataRoot;
const publicAssets = publicAssetsRoot;

export { expandCharacterInfoCodenames };

/*
================
firstModel

A characterdata model cell's first model. v1.696 lets one cell name
several, comma-joined (the Mad General's three forms; arabia_import
copies each). INFERENCE: the v1.150 client reads one path per kind and
has no form switch, so a kind is drawn as its first form; the other
forms are still shipped by the import for a later form contract.
================
*/
export function firstModel( cell ) {
	return cell.split( "," )[0].trim();
}

/*
================
resolveNpcModel

A roster codename's model (its own or its base chain's .bsr/.cpd), or
null; exported so the contracts can prove a planned species resolves.
================
*/
export function resolveNpcModel( codename, rows ) {
	const cols = rows.get( codename );
	if ( !cols ) return null;
	let bsrCols = cols;
	let baseCodename = null;
	const visited = new Set( [ codename ] );
	while ( bsrCols.findIndex( ( value ) => /\.(?:bsr|cpd)$/i.test( value ) ) < 0 ) {
		// _CLON rows carry a base codename at col[4]. Follow the identity chain
		// defensively: current retail data is one hop, but cycles and missing
		// links must fail rather than turn into order-dependent unresolved pegs.
		const link = bsrCols[4];
		if ( !link || visited.has( link ) ) {
			throw new Error(
				`[npc] ${codename}: invalid characterdata base chain at ${link || "<empty>"}`
			);
		}
		const baseRow = rows.get( link );
		if ( !baseRow ) {
			throw new Error( `[npc] ${codename}: characterdata base ${link} is missing` );
		}
		visited.add( link );
		bsrCols = baseRow;
		baseCodename = link;
	}
	const bsrIndex = bsrCols.findIndex( ( v ) => /\.(?:bsr|cpd)$/i.test( v ) );
	if ( bsrIndex < 0 ) return null;
	const scalePercent = Number( cols[48] );
	return {
		codename,
		refObjId: Number( cols[1] ),
		baseCodename,
		materialKind: Number( cols[109] ),
		scalePercent: Number.isFinite( scalePercent ) && scalePercent > 0 ? scalePercent : 100,
		bsrPath: `res/${firstModel( bsrCols[bsrIndex] ).replaceAll( "\\", "/" ).toLowerCase()}`
	};
}

/*
================
monsterPolicyBsrs

The BSRs that bake with the monster clip policy: every one a monster, a
COS or a ride names. The native client loads a BSR as one resource
whichever object asks for it (CResChar), so a town NPC that shares a
monster's or a pet's model (v1.696's lamp genie on res/cos/p_jinn.bsr)
plays its states from that same resource. The monster bake is the NPC
bake's superset (the same stand, idle and emote states plus the combat
motions), so the shared BSR bakes once with the monster policy and every
consumer reuses it, whatever the roster order.
================
*/
export function monsterPolicyBsrs( roster, { resolveModel, isMonster, rideModelPath } ) {
	const paths = new Set();
	for ( const { codename } of roster ) {
		const ride = rideModelPath( codename );
		if ( ride ) paths.add( ride );
		if ( !isMonster( codename ) ) continue;
		const model = resolveModel( codename );
		if ( model ) paths.add( model.bsrPath );
	}
	return paths;
}

/*
================
buildNpcModelAssets
================
*/
export async function buildNpcModelAssets( options = {} ) {
	const store = options.store ?? (await openStore());
	try {
		return await publishNpcModels( options, store );
	} finally {
		if ( !options.store ) await store.shutdown();
	}
}

/*
================
publishNpcModels

The bakes and the manifests. Every resource bake goes through the action
store (cachedBake); the manifests are rebuilt from the bakes' results on
every run.
================
*/
async function publishNpcModels( options, store ) {
	// A scratch output root lets a branch verify its bake without rewriting
	// the shared tree's manifest; the default is the published tree.
	const publicAssets = options.publicAssetsRoot ?? publicAssetsRoot;
	const stats = { stored: 0 };
	const eventRain = parseWeatherEvents( fs.readFileSync( path.join( textdataDir, "skilleffect.txt" ), "utf16le" ) );
	const skipTextures = options.skipTextures ?? false;
	if ( !skipTextures ) await convertTextureTrees( "npc", [ "prim/mtrl" ] );

	// Runtime rosters, not codename prefixes, decide what is built. Load the
	// complete RefObjChar identity table so native NPC-band structure rows and
	// clone base links remain resolvable without a second classifier.
	const rows = loadCharacterDataRows( textdataDir, { codenamePattern: /./, qinshiTombOverlay: true } );
	const characterInfo = loadCharacterInfo( textdataDir );
	const resolveCharacterInfo = characterInfoResolver( characterInfo, rows );
	const npcRoster = loadSpawnableNpcRoster();
	const mobRoster = loadModelledMobRoster();
	const mobRosterByCodename = new Map( mobRoster.map( ( ref ) => [ ref.codename, ref ] ) );
	// 582110: growth pets and hidden transports route action 1 to state 50.
	// Publish every enabled reference, sharing the native BSR bake across levels.
	const cosRoster = enabledCosReferences( rows ).map( ( { codename, refObjId } ) => ({
		codename,
		refObjId,
		rideModelPath: resolveCharacterInfo( codename )?.rideModelPath,
		riderTransformMode: resolveCharacterInfo( codename )?.riderTransformMode
	}) );
	const cosNames = new Set( cosRoster.map( row => row.codename ) );
	const structureRoster = loadFortressStructureRoster();
	const structureNames = new Set( structureRoster.map( row => row.codename ) );
	const structureEffects = parseStructureEffects(
		fs.readFileSync( path.join( textdataDir, "atstructeffect.txt" ), "utf16le" ),
		codename => structureNames.has( codename )
	);
	const roster = [ ...npcRoster, ...mobRoster, ...cosRoster, ...structureRoster ];
	if ( new Set( roster.map( ( ref ) => ref.codename ) ).size !== roster.length ) {
		throw new Error( "[npc] server NPC and monster rosters contain an overlapping codename" );
	}
	if ( new Set( roster.map( ( ref ) => ref.refObjId ) ).size !== roster.length ) {
		throw new Error( "[npc] server NPC and monster rosters contain an overlapping RefObj identity" );
	}
	const manifestPath = path.join( publicAssets, "npc", "manifest.json" );
	const previousManifest = readJsonOrNullSync( manifestPath );
	const previousGlbPaths = readPreviousResourceGlbPaths( manifestPath );
	const models = [];
	let builtResources = 0;
	let coveredModels = 0;
	let reusedModels = 0;
	/** bsrPath -> policy + baked result, so _CLON codenames reuse the base GLB. */
	const bakedByBsr = new Map();
	/** Browser output -> BSR identity, guarding every primary and ride write. */
	const outputOwners = new Map();
	/** Full BSR animation metadata stays outside the production-hot model manifest. */
	const retailAnimationResources = new Map();
	const retailAnimationModels = new Map();
	/** Secondary BSR -> the characterInfo records that require it. */
	const rideResources = new Map();
	/** characterInfo death BSR -> the codenames whose record names it. */
	const deathResources = new Map();
	const bake = cachedBake( { store, publicAssetsRoot: publicAssets, tables: bakeTables(), stats } );
	const monsterBsrs = monsterPolicyBsrs( roster, {
		resolveModel: codename => resolveNpcModel( codename, rows ),
		isMonster: codename => mobRosterByCodename.has( codename ) || cosNames.has( codename ),
		// The media ride path; the loop below refuses one that drifts from the server's.
		rideModelPath: codename => resolveCharacterInfo( codename )?.rideModelPath ?? null
	} );
	for ( const rosterRef of roster ) {
		const { codename } = rosterRef;
		const model = resolveNpcModel( codename, rows );
		if ( !model ) {
			console.warn( `[npc] ${codename}: no characterdata row / bsr path` );
			models.push( { codename, error: "unresolved" } );
			continue;
		}
		// characterdata enables the small guard towers (STRUCTURE_SMALL_*_TOWER),
		// whose BSRs the v1.150 Data.pk2 does not hold: the client cannot draw
		// them, so they are not part of this client's world.
		if ( structureNames.has( codename ) && (await loadOptionalDataAsset( model.bsrPath )) === null ) {
			console.log( `[npc] SKIP ${codename}: v1.150 data ships no ${model.bsrPath}` );
			continue;
		}
		const isCos = cosNames.has( codename );
		const isMob = mobRosterByCodename.has( codename ) || isCos;
		const output = resourceGlbOutput( model.bsrPath, {
			namespace: "npc",
			publicAssetsRoot: publicAssets
		} );
		claimResourceOutput( outputOwners, model.bsrPath, output.publicPath );
		const publicPath = output.publicPath;
		const info = resolveCharacterInfo( codename );
		const soundProfileName = info?.soundProfileName;
		if ( !soundProfileName && isMob ) {
			throw new Error( `[npc] ${codename}: no skilleffect characterInfo ResourceTypeName` );
		}
		if ( !soundProfileName ) {
			console.warn(
				`[npc] ${codename}: no skilleffect characterInfo row; ` +
					"publishing the visual without an invented sound/action profile"
			);
		}
		const mediaRideModelPath = info?.rideModelPath ?? null;
		const serverRideModelPath = normalizeCharacterInfoBsrPath( rosterRef?.rideModelPath ) ?? null;
		const mediaTransformMode = Number( info?.riderTransformMode ?? 0 );
		const serverTransformMode = Number( rosterRef?.riderTransformMode ?? 0 );
		if (
			mediaRideModelPath !== serverRideModelPath ||
			(mediaRideModelPath !== null && mediaTransformMode !== serverTransformMode)
		) {
			throw new Error(
				`[npc] ${codename}: server/media ride contract drift ` +
					`(server=${serverRideModelPath ?? "none"}/${serverTransformMode}, ` +
					`media=${mediaRideModelPath ?? "none"}/${mediaTransformMode})`
			);
		}
		if ( serverRideModelPath ) {
			const priorRide = rideResources.get( serverRideModelPath ) ?? {
				bsrPath: serverRideModelPath,
				requiredBy: [],
				transformModes: new Set()
			};
			priorRide.requiredBy.push( codename );
			priorRide.transformModes.add( serverTransformMode );
			rideResources.set( serverRideModelPath, priorRide );
		}
		const deathModelPath = info?.deathModelPath ?? null;
		if ( deathModelPath ) {
			const priorDeath = deathResources.get( deathModelPath ) ?? { bsrPath: deathModelPath, requiredBy: [] };
			priorDeath.requiredBy.push( codename );
			deathResources.set( deathModelPath, priorDeath );
		}
		const kind = isCos ? "cos" : isMob ? "monster" : structureNames.has( codename ) ? "structure" : "npc";
		const entry = {
			codename,
			refObjId: model.refObjId,
			eventRain: eventRain.get( codename ) ?? eventRain.get( model.baseCodename ) ?? false,
			kind,
			bsr: model.bsrPath,
			glb: publicPath,
			...(soundProfileName ? { soundProfileName } : {}),
			...(deathModelPath ? { deathModel: deathModelPath } : {})
		};
		if ( model.refObjId !== rosterRef.refObjId ) {
			throw new Error(
				`[npc] ${codename}: server/media RefObj identity drift ` +
					`(server=${rosterRef.refObjId}, media=${model.refObjId})`
			);
		}
		retailAnimationModels.set( codename, {
			codename,
			refObjId: model.refObjId,
			kind,
			bsr: model.bsrPath
		} );
		if ( isMob ) {
			// Scale percent from characterdata col[48] (tiger 100 / tiger_clon
			// 80). Data passthrough only - applying it is the spawn plane's leg.
			entry.scalePercent = model.scalePercent;
			entry.materialKind = model.materialKind;
			if ( model.baseCodename ) entry.baseCodename = model.baseCodename;
		}
		// The bake's clip policy is the BSR's (monsterPolicyBsrs); the entry
		// above keeps this consumer's own kind and fields.
		const bakeAsMob = isMob || monsterBsrs.has( model.bsrPath );
		const prior = bakedByBsr.get( model.bsrPath );
		if ( prior ) {
			if ( prior.isMob !== bakeAsMob ) {
				throw new Error(
					`[npc] ${model.bsrPath}: shared by NPC and monster consumers with incompatible clip policies`
				);
			}
			Object.assign( entry, prior.baked );
			coveredModels += 1;
			reusedModels += 1;
			console.log( `[npc] OK   ${codename.padEnd( 20 )} -> ${publicPath} (shared bake)` );
			models.push( entry );
			continue;
		}
		try {
			const baked = await bake( model.bsrPath, output, bakeAsMob );
			const { retailAnimationCatalog, ...missionBaked } = baked;
			Object.assign( entry, missionBaked );
			bakedByBsr.set( model.bsrPath, { isMob: bakeAsMob, baked: missionBaked } );
			retailAnimationResources.set( model.bsrPath, {
				bsr: model.bsrPath,
				glb: missionBaked.glb,
				animations: retailAnimationCatalog
			} );
			builtResources += 1;
			coveredModels += 1;
			console.log(
				`[npc] OK   ${codename.padEnd( 20 )} -> ${entry.glb} (${entry.bytes} B, clips=[${entry.clips}])`
			);
		} catch ( error ) {
			entry.error = String( error?.message ?? error );
			console.warn( `[npc] FAIL ${codename.padEnd( 20 )} ${entry.error}` );
		}
		models.push( entry );
	}

	// Secondary models are first-class resource entries keyed by normalized
	// BSR path. This mirrors ResourceManager lookup at action-effect +0x24 and
	// avoids inventing a second RefObj/codename identity for packetless rides.
	// CICharactor_Action_KnockdownDie (8E64F0) reloads a dying body from its
	// characterInfo death BSR the same way; death models bake with the monster
	// clip policy but require death 4 and deathLoop 36 instead of stand.
	const secondaryResources = [
		...[ ...rideResources.values() ].map( ride => ({
			bsrPath: ride.bsrPath,
			kind: "ride",
			isMob: true,
			requiredStates: BODY_REQUIRED_STATES,
			fields: {
				requiredBy: ride.requiredBy,
				riderTransformModes: [ ...ride.transformModes ].sort( ( a, b ) => a - b )
			}
		}) ),
		...[ ...deathResources.values() ].map( death => ({
			bsrPath: death.bsrPath,
			kind: "death",
			isMob: true,
			requiredStates: DEATH_REQUIRED_STATES,
			fields: { requiredBy: death.requiredBy }
		}) ),
		...loadBoothModelRoster( textdataDir )
	];
	const secondary = await bakeNpcSecondaryResources( {
		publicAssetsRoot: publicAssets,
		models,
		bakedByBsr,
		outputOwners,
		retailAnimationModels,
		retailAnimationResources,
		bake
	}, secondaryResources );
	builtResources += secondary.built;
	coveredModels += secondary.covered;
	reusedModels += secondary.reused;

	// CICATStruct_SetVisualStage (4F78A0) reloads the model from the stage's
	// atstructeffect BSR, falling back to the record's own; stage models are
	// path-keyed resources like the rides, baked with the static NPC policy.
	for ( const entry of models ) {
		const effects = entry.kind === "structure" && !entry.error ? structureEffects.get( entry.codename ) : undefined;
		if ( !effects ) continue;
		const stages = {};
		for ( const [stage, bsrPath] of Object.entries( effects.stages ) ) {
			const prior = bakedByBsr.get( bsrPath );
			if ( prior ) {
				if ( prior.isMob ) throw new Error( `[npc] ${bsrPath}: structure stage shared with a monster bake` );
				stages[stage] = { glb: prior.baked.glb, particleModifiers: prior.baked.particleModifiers };
				reusedModels += 1;
				continue;
			}
			const output = resourceGlbOutput( bsrPath, { namespace: "npc", publicAssetsRoot: publicAssets } );
			claimResourceOutput( outputOwners, bsrPath, output.publicPath );
			try {
				const { retailAnimationCatalog: _catalog, ...baked } = await bake( bsrPath, output, false );
				bakedByBsr.set( bsrPath, { isMob: false, baked } );
				stages[stage] = { glb: baked.glb, particleModifiers: baked.particleModifiers };
				builtResources += 1;
				console.log( `[npc] OK   ${entry.codename} stage ${stage} -> ${baked.glb}` );
			} catch ( error ) {
				entry.error = `stage ${stage} ${bsrPath}: ${error?.message ?? error}`;
				console.warn( `[npc] FAIL ${entry.codename} ${entry.error}` );
			}
		}
		entry.structureStages = stages;
		entry.structureSounds = effects.sounds;
		entry.structureDamageEffects = effects.levels;
	}

	models.push( publishCrowEgg( publicAssets ) );
	builtResources++;
	coveredModels++;
	const preservedVat = preserveFreshNpcVatReferences( models, previousManifest, { publicRoot } );
	const { models: referenceRows, resources, boothModels } = splitNpcManifestModels( models );
	const manifest = {
		format: NPC_MANIFEST_FORMAT,
		version: NPC_MANIFEST_VERSION,
		source:
			"server NPC spawn roster + server-exported spawnable monster roster; PathCtl-owned in-place horizontal locomotion, complete native default CResAnimationStateTable event-map/time-warp payloads, BSR ModDataSound cursor tracks, and the unified skilleffect characterInfo sound + CICRide resource contract",
		count: models.length,
		builtCount: builtResources,
		coveredCount: coveredModels,
		reusedCount: reusedModels,
		models: referenceRows,
		resources,
		boothModels,
		...(preservedVat.contract ? { vat: preservedVat.contract } : {})
	};
	const failures = models.filter( ( model ) => model.error );
	if ( failures.length > 0 ) {
		throw new Error(
			`[npc] required model coverage incomplete; valid manifests were not replaced: ${
				failures
					.map( ( model ) => `${model.codename}: ${model.error}` )
					.join( "; " )
			}`
		);
	}
	fs.mkdirSync( path.dirname( manifestPath ), { recursive: true } );
	writeJsonIfChangedSync( manifestPath, manifest );
	console.log(
		`[npc] covered ${coveredModels}/${models.length} entries from ${builtResources} unique GLBs ` +
			`(${reusedModels} shared, ${stats.stored} from the action store); ` +
			`manifest -> ${path.relative( gameRoot, manifestPath )}`
	);
	if ( preservedVat.preserved > 0 || preservedVat.stale > 0 ) {
		console.log(
			`[npc] preserved ${preservedVat.preserved} fresh VAT reference(s); ` +
				`dropped ${preservedVat.stale} stale reference(s)`
		);
	}
	const animationCatalog = {
		format: "sro-retail-character-animation-catalog",
		version: 2,
		source: "native BSR default animation-set state records joined to unique exported GLB clips and BAN loopType",
		modelCount: retailAnimationModels.size,
		resourceCount: retailAnimationResources.size,
		models: Object.fromEntries( retailAnimationModels ),
		resources: Object.fromEntries( retailAnimationResources )
	};
	const animationCatalogPath = path.join( publicAssets, "npc", "animation-catalog.json" );
	writeJsonIfChangedSync( animationCatalogPath, animationCatalog );
	await refreshPrecompressedSidecars( [ manifestPath, animationCatalogPath ], { onlyWhenStale: true } );
	console.log( `[npc] retail animation catalog -> ${path.relative( gameRoot, animationCatalogPath )}` );
	const removed = removeSupersededResourceOutputs( {
		previousPublicPaths: previousGlbPaths,
		currentPublicPaths: models.flatMap( (
			model
		) => [
			model.glb,
			...Object.values( model.materialVariants ?? {} ),
			...Object.values( model.structureStages ?? {} ).map( stage => stage.glb )
		] ).filter( Boolean ),
		namespace: "npc",
		publicAssetsRoot: publicAssets
	} );
	if ( removed.length > 0 ) {
		console.log( `[npc] removed ${removed.length} superseded GLB output(s)` );
	}
	return {
		built: builtResources,
		covered: coveredModels,
		reused: reusedModels,
		stored: stats.stored,
		modelCount: models.length,
		manifestPath,
		animationCatalogPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	const rootFlag = process.argv.find( arg => arg.startsWith( "--public-assets-root=" ) );
	await buildNpcModelAssets( {
		skipTextures: process.argv.includes( "--skip-textures" ),
		...(rootFlag ? { publicAssetsRoot: path.resolve( rootFlag.slice( "--public-assets-root=".length ) ) } : {})
	} );
}
