/*
===========================================================================

parseSkillEffect.mjs - native skill animation and effect record compilation

Owns the authored name-to-skill join and native resource descriptors. Both
combat stages and persistent skill objects consume this one record table.
The v1.150 client determines discriminators; newer rows require evidence
before they can be translated into that version's execution vocabulary.

===========================================================================
*/

// Parser for the skill-effect DATA plane: the f0902c GlobalEffectManager
// effect-record map the WIP skill folds read through
// sub_917240 GlobalEffectManager_FindEffectRecordBySkillId.
//
// RE (Revtool SkillEffectData_LoadAll / SkillAnimationSet_ParseRow; Rizin
// 0x0092008f and 0x00920304..0x00920465, audited 2026-08-05):
//   The native loader sub_920020 opens textdata\skilleffect.txt. Both
//   #section skillaniset and #section skillaniset2 dispatch to sub_91dae0;
//   the latter is the service-column form shipped by this v1.150 archive.
//   The parser inserts a CIDecoSkillRecord into the f0902c std::map per
//   enabled row, keyed by the id sub_9169d0 assigns the row's skill NAME.
//   The record fields sub_8e06e0 SkillEffectObj_Init
//   consumes: +0x04 anim-slot key (AniGroup), +0x08/+0x09/+0x0a per-phase
//   anim-table COUNTS + +0x0e/+0x16/+0x1e the tables (AniReady/AniWait/
//   AniShot), +0x3e target count, +0x02 flags.
//
// THE ID-SPACE JOIN (evidence: SkillData_*.txt + skilleffect.txt inspection,
// rev. 92, corrected rev. 98):
//   - skilleffect.txt #section skillaniset{,2} keys records by a BASE skill
//     name (col 'SkillID' = SKILL_CH_SWORD_SMASH_A, no level suffix).
//   - SkillData_*.txt: col[1] = the numeric skill id (the B245 wire skillId),
//     col[3] = the level skill name (SKILL_CH_SWORD_SMASH_A_01), col[5] = the
//     BASE skill name (the anim-set reference; EXACT-matches a skillaniset
//     row for skills with a custom anim set).
//   - Monster rows are authored differently: col[5] is `xxx`, while the
//     skillaniset key EXACT-matches col[3] (for example skill 160,
//     MSKILL_CH_MANGNYANG_ATTACK01). The native name-id resolver supports
//     both forms; filtering to the SKILL_ prefix dropped the entire monster
//     animation/result plane.
//   So: resolve col[5] first, then exact col[3]. Rows with neither authored
//   name have no CIDecoSkillRecord and remain absent.
//
// This build step produces a { skillId(number) -> record } table so the WIP
// loader host can fill g_effectRecordMap and the REAL 917240 find resolves
// shipped skills without the registerSkillRecord test stub.

import fs from "node:fs";
import path from "node:path";
import { animationSetKeyForName } from "./native/animationSetNames.ts";
import { SKILL_EFFECT_ANIMATION_ID_BY_NAME } from "./native/skillEffectAnimationRegistry.ts";
import { SKILL_EFFECT_MOVE_TYPE_ID_BY_NAME } from "./native/skillEffectStage.ts";
import { SkillEffectSet_ParseRow } from "./native/skillEffectSetParseRow.ts";
import {
	listTextDataShardNamesSync,
	readTextDataLinesSync,
	splitTextDataRow as tabCols
} from "../shared/textDataIo.mjs";

const SKILLDATA_COL_ID = 1;
const SKILLDATA_COL_LEVEL_NAME = 3;
const SKILLDATA_COL_ANIMSET_BASE = 5;

/**
 * Native data_ccdca8: the built-in effect-name/id pairs registered into the
 * same name resolver as ordinary skill effects by sub_bbeab0.  The ids are
 * signed int32 keys in g_effectRecordMap; keeping that representation here is
 * important because sub_917240 deliberately skips the positive-SkillData
 * override for them.
 */
export const BUILTIN_EFFECT_NAME_ID_PAIRS = Object.freeze( [
	[ "SYSTEM_CH_HWANMODE", 0x80000000 | 0 ],
	[ "SYSTEM_HPPOTION", 0x80000001 | 0 ],
	[ "SYSTEM_MPPOTION", 0x80000002 | 0 ],
	[ "SYSTEM_LIFE", 0x80000003 | 0 ],
	[ "SYSTEM_RETURNSCROLL", 0x80000004 | 0 ],
	[ "SYSTEM_RETURNSCROLLRESULT", 0x80000005 | 0 ],
	[ "SYSTEM_LEVELUP", 0x80000006 | 0 ],
	[ "SYSTEM_APPEAR", 0x80000007 | 0 ],
	[ "SYSTEM_UNTOUCHABLE", 0x80000018 | 0 ],
	[ "SYSTEM_EXCLAMATION_START", 0x8000001a | 0 ],
	[ "SYSTEM_EXCLAMATION_END", 0x8000001b | 0 ],
	[ "SYSTEM_EXCLAMATION_GOING", 0x8000001c | 0 ],
	[ "SYSTEM_KNOCKBACK", 0x8000001d | 0 ],
	[ "SYSTEM_HELPERMARK", 0x8000001e | 0 ],
	[ "SYSTEM_QUEST_MARK", 0x8000001f | 0 ],
	[ "SYSTEM_CAPTURE_MARK", 0x80000020 | 0 ],
	[ "SYSTEM_PET_APPEAR", 0x80000021 | 0 ],
	[ "SYSTEM_PET_LEVEL_UP", 0x80000022 | 0 ],
	[ "SYSTEM_COS_HPPOTION", 0x80000023 | 0 ],
	[ "SYSTEM_COS_MPPOTION", 0x80000024 | 0 ],
	[ "SYSTEM_COS_HGPPOTION", 0x80000025 | 0 ],
	[ "STATUS_CURE_COS", 0x80000026 | 0 ],
	[ "SYSTEM_QUEST_MARK2", 0x80000027 | 0 ]
] );

const ANI_COL = {
	priority: 2,
	overlap: 3,
	group: 5,
	ready: 6,
	wait: 7,
	shot: 8,
	actionReady: 9,
	actionWait: 10,
	actionShot: 11,
	damageEffect: 13,
	criticalDamageEffect: 18,
	arrowTrailEffect: 19,
	arrowForceEffect: 20,
	hideWeapon: 4,
	objectResource: 22,
	attackSkillFlagBe: 25
};

/*
================
readSkillEffectSourceText

 * Decode either native skill-effect source representation without changing
 * cell boundaries. The retail textdata table is UTF-16LE; the v1.150 client
 * resinfo mirror is a single-byte codepage. Both are native evidence inputs;
 * production output remains derived from the retail textdata authority.
================
*/
export function readSkillEffectSourceText( skillEffectPath ) {
	const bytes = fs.readFileSync( skillEffectPath );
	const utf16Bom = bytes.length >= 2 && bytes[0] === 0xff && bytes[1] === 0xfe;
	const looksUtf16Le = bytes.length >= 4 &&
		bytes[1] === 0 &&
		bytes[3] === 0;
	return bytes
		.toString( utf16Bom || looksUtf16Le ? "utf16le" : "latin1" )
		.replace( /^\ufeff/, "" );
}

/*
================
optionalResourcePath

Preserve absent native resources as null; normalize only authored paths.
================
*/
function optionalResourcePath( value ) {
	const text = String( value ?? "" ).trim();
	return text && text.toLowerCase() !== "none" ?
		text.replaceAll( "\\", "/" ).toLowerCase() :
		null;
}

/*
================
skillObjectResource

91E5C6 selects the object discriminator by its three-letter extension.
86C440 admits BSR animation objects and EFP compound effects separately.
================
*/
function skillObjectResource( value ) {
	const resource = optionalResourcePath( value );
	if ( !resource ) return null;
	if ( resource.endsWith( ".bsr" ) ) return { kind: "model", path: resource };
	if ( resource.endsWith( ".efp" ) ) return { kind: "effect", path: resource };
	throw new Error( `Unknown native skill object resource: ${resource}` );
}

/*
================
animationNames

91DEF3 admits only names present in the native animation registry. Counts
must describe those admitted tokens, including repeated animation entries.
================
*/
function animationNames( value ) {
	if ( !value || value === "none" ) return [];
	return value.split( "," ).filter( name => name && SKILL_EFFECT_ANIMATION_ID_BY_NAME.has( name ) );
}

/*
================
parseSkillLights

Resolve the shared light table before animation sets reference it.
================
*/
function parseSkillLights( lines ) {
	const lights = new Map();
	let inSection = false;
	for ( const line of lines ) {
		if ( line.startsWith( "#section" ) ) {
			inSection = /^#section\s+light\b/i.test( line );
			continue;
		}
		if ( !inSection || !line || line.startsWith( "//" ) ) continue;
		const columns = tabCols( line );
		const argb = columns[2]?.split( "," ).map( Number );
		if ( !argb || argb.length !== 4 || argb.some( value => !Number.isInteger( value ) ) ) {
			throw new Error( "Invalid native light color" );
		}
		lights.set( columns[0], {
			color: argb.slice( 1 ).map( value => Math.fround( (value & 255) / 255 ) ),
			duration: Math.fround( Number( columns[3] ) / 1000 ),
			range: Number( columns[4] ),
			attenuation: Math.fround( Number( columns[5] ) )
		} );
	}
	return lights;
}

/*
================
parseSkillAniSet

 * Parse both native animation-set section versions -> Map<baseName, aniRow>.
 * `skillaniset2` prepends the loader-consumed Service column. Native
 * SkillEffectData_LoadAll marks that section as the version-2 form and routes
 * its enabled rows through the same SkillAnimationSet_ParseRow function.
================
*/
export function parseSkillAniSet( skillEffectPath ) {
	const text = readSkillEffectSourceText( skillEffectPath );
	const lines = text.split( /\r?\n/ );
	const lights = parseSkillLights( lines );
	const out = new Map();
	/** @type {string | null} */
	let sectionName = null;
	for ( const line of lines ) {
		if ( line.startsWith( "#section" ) ) {
			const match = /^#section\s+(skillaniset2?)\b/i.exec( line );
			sectionName = match?.[1]?.toLowerCase() ?? null;
			continue;
		}
		if ( !sectionName || !line || line.startsWith( "//" ) ) continue;
		const authored = tabCols( line );
		const serviceColumnCount = sectionName === "skillaniset2" ? 1 : 0;
		if ( serviceColumnCount && Number( authored[0] ?? 0 ) === 0 ) {
			continue;
		}
		const c = authored.slice( serviceColumnCount );
		const baseName = c[1];
		// Native sub_9169d0 resolves the authored name; it is not restricted to
		// the player SKILL_ namespace. MSKILL_ and PSKILL_ rows share this table.
		if ( !baseName ) continue;
		// Native 0x91def3..0x91df0a resolves each comma token through the
		// data_ccd620-backed map at 0xf090c8 and increments the phase count only
		// on a hit. Keep malformed/version-mismatched names out of both the table
		// and its cardinality exactly (the shipped DOWN_UP rows intentionally miss;
		// only ANI_DOWN_UP exists in the larger script-object registry).
		const ready = animationNames( c[ANI_COL.ready] );
		const wait = animationNames( c[ANI_COL.wait] );
		const shot = animationNames( c[ANI_COL.shot] );
		const actionReady = optionalResourcePath( c[ANI_COL.actionReady] );
		const actionWait = optionalResourcePath( c[ANI_COL.actionWait] );
		const actionShot = optionalResourcePath( c[ANI_COL.actionShot] );
		out.set( baseName, {
			baseName,
			priority: Number( c[ANI_COL.priority] ) || 0,
			// 91DAE0 boolean lookup: unknown names resolve -1, hence true.
			overlap: ![ "FALSE", "false" ].includes( c[ANI_COL.overlap] ),
			aniGroup: c[ANI_COL.group] ?? "DEFAULT",
			// The three per-phase anim tables the record carries; counts drive
			// the 8e06e0 haveAnim gate that enters motion state 2.
			readyAnims: ready,
			waitAnims: wait,
			shotAnims: shot,
			actionReady,
			actionWait,
			actionShot,
			actionReadyAnims: animationNames( c[ANI_COL.actionReady] ),
			actionWaitAnims: animationNames( c[ANI_COL.actionWait] ),
			actionShotAnims: animationNames( c[ANI_COL.actionShot] ),
			objectResource: skillObjectResource( c[ANI_COL.objectResource] ),
			defenseEffectPath: optionalResourcePath( c[12] ),
			hitLight: lights.get( c[21] ) ?? null,
			// Native CIDecoSkillRecord's authored resource names. These are
			// semantic additions to the browser record, not replacements for the
			// +0x00..+0x80 folded layout above: the packet host uses them at the
			// external EasyFX render boundary after the exact B245/B505 lifecycle.
			damageEffectPath: optionalResourcePath( c[ANI_COL.damageEffect] ),
			criticalDamageEffectPath: optionalResourcePath( c[ANI_COL.criticalDamageEffect] ),
			arrowTrailEffectPath: optionalResourcePath( c[ANI_COL.arrowTrailEffect] ),
			arrowForceEffectPath: optionalResourcePath( c[ANI_COL.arrowForceEffect] ),
			hideWeapon: Number( c[ANI_COL.hideWeapon] ) || 0,
			// sub_91dae0 @0x91e6f4 stores the final authored byte at record+0xbe.
			// Single-projectile 8ddde0 snapshots that record into stage+0x1b4;
			// 8d7fd0 uses +0xbe to gate the target's secondary action effect.
			attackSkillFlagBe: Number( c[ANI_COL.attackSkillFlagBe] ) || 0
		} );
	}
	return out;
}

/*
================
parseSkillEffectSet

 * Parse the native `skilleffectset` section through the folded sub_91e720
 * field reader.  The output map preserves file order because rows sharing one
 * SkillEffectID are ordered stages, not a last-write-wins table.
================
*/
export function parseSkillEffectSet( skillEffectPath, primitiveSoundRoot = null ) {
	const text = readSkillEffectSourceText( skillEffectPath );
	const out = new Map();
	let inSection = false;
	for ( const line of text.split( /\r?\n/ ) ) {
		if ( line.startsWith( "#section" ) ) {
			inSection = /^#section\s+skilleffectset\b/.test( line );
			continue;
		}
		// The native text reader discards whole-line comments. A display-name
		// token of "//" therefore denotes a disabled row, not a visible stage.
		if ( !inSection || !line || line.startsWith( "//" ) ) continue;
		const columns = tabCols( line );
		if ( columns.length < 28 || !columns[1] ) continue;
		const record = SkillEffectSet_ParseRow( columns );
		if ( !record.skillEffectId ) continue;
		if ( primitiveSoundRoot ) {
			/*
		================
		resolveSoundPublicPath

		Only publish sound handles backed by an extracted native resource.
		================
		*/
			const resolveSoundPublicPath = ( soundPath ) => {
				if ( !soundPath ) return null;
				const normalized = soundPath.replaceAll( "\\", "/" ).toLowerCase();
				return fs.existsSync( path.join( primitiveSoundRoot, ...normalized.split( "/" ) ) ) ?
					`/assets/audio/sfx/prim/snd/${normalized}` :
					null;
			};
			record.soundBeginPublicPath = resolveSoundPublicPath( record.soundBegin );
			record.soundEndPublicPath = resolveSoundPublicPath( record.soundEnd );
		}
		const rows = out.get( record.skillEffectId ) ?? [];
		rows.push( record );
		out.set( record.skillEffectId, rows );
	}
	return out;
}

/*
================
skillEffectFamilyName

Strip the authored phase suffix when matching cross-version homologs.
================
*/
function skillEffectFamilyName( skillEffectId ) {
	return String( skillEffectId ).replace( /_[A-Z]$/, "" );
}

/*
================
adaptSkillEffectSetsToClientNative

 * Reconcile newer server-owned rows against the v1.150 client's verified
 * discriminator tables. Unknown values are never assigned a guessed enum.
 * A translation is admitted only when shipped v1.150 rows from the same
 * skill family, phase, and action unanimously establish the client value.
================
*/
export function adaptSkillEffectSetsToClientNative( effectSets, clientEffectSets ) {
	const clientRows = [ ...clientEffectSets.values() ].flat();
	const adapted = new Map();
	for ( const [owner, rows] of effectSets ) {
		adapted.set(
			owner,
			rows.map( ( row ) => {
				if ( SKILL_EFFECT_MOVE_TYPE_ID_BY_NAME.has( row.move.kind ) ) return row;
				const family = skillEffectFamilyName( row.skillEffectId );
				const homologs = clientRows.filter( ( candidate ) =>
					skillEffectFamilyName( candidate.skillEffectId ) === family &&
					candidate.animationPhase === row.animationPhase &&
					candidate.actionType === row.actionType &&
					SKILL_EFFECT_MOVE_TYPE_ID_BY_NAME.has( candidate.move.kind )
				);
				const clientKinds = [ ...new Set( homologs.map( ( candidate ) => candidate.move.kind ) ) ];
				if ( clientKinds.length !== 1 ) {
					throw new Error(
						`skill effect ${row.skillEffectId}: newer move kind ${row.move.kind} has no unique v1.150 family homolog`
					);
				}
				return {
					...row,
					move: { ...row.move, kind: clientKinds[0] },
					v150TranslationEvidence: {
						field: "move.kind",
						sourceValue: row.move.kind,
						clientValue: clientKinds[0],
						evidenceSkillEffectIds: homologs.map( ( candidate ) => candidate.skillEffectId )
					}
				};
			} )
		);
	}
	return adapted;
}

/*
================
resolveSkillEffectSetRows

 * Resolve the stage owner exactly as the shipped v1.150 table is authored.
 * Most effect records use an exact SkillID match. Basic-attack families use
 * the level-one `_01` owner form, so the deterministic suffix lookup is the
 * native name-id join for both Chinese and European weapon bases.
================
*/
export function resolveSkillEffectSetRows( baseName, effectSets ) {
	const exact = effectSets.get( baseName );
	if ( exact ) return exact;
	const suffixedName = `${baseName}_01`;
	const suffixed = effectSets.get( suffixedName );
	return suffixed ?? [];
}

/*
================
deriveTargetResultCapacity

 * Reproduce sub_916aa0's post-load damage-stage census for
 * CIDecoSkillRecord+0x3e. Retail scans animation phases 0..9, rejects more
 * than one damage row in any phase, then stores the total in one byte.
================
*/
function deriveTargetResultCapacity( baseName, authoredStages ) {
	const damageRowsByPhase = new Map();
	for ( const stage of authoredStages ) {
		if ( !stage.damageEvent ) continue;
		const phase = Number( stage.startEvent );
		if ( !Number.isInteger( phase ) || phase < 0 || phase > 9 ) {
			throw new Error( `skill effect ${baseName}: damage event phase ${stage.startEvent} is outside 0..9` );
		}
		const count = (damageRowsByPhase.get( phase ) ?? 0) + 1;
		if ( count > 1 ) {
			throw new Error( `skill effect ${baseName}: phase ${phase} owns multiple damage events` );
		}
		damageRowsByPhase.set( phase, count );
	}
	return [ ...damageRowsByPhase.values() ].reduce( ( sum, count ) => sum + count, 0 );
}

/*
================
loadSkillDataRows

Retain both authored names: player ranks and monster skills join differently.
================
*/
export function loadSkillDataRows( textdataDir ) {
	const files = listTextDataShardNamesSync( textdataDir, /^skilldata_\d+\.txt$/i );
	const rows = [];
	for ( const file of files ) {
		for ( const line of readTextDataLinesSync( path.join( textdataDir, file ) ) ) {
			const c = tabCols( line );
			const name = c[SKILLDATA_COL_LEVEL_NAME];
			if ( !name ) continue;
			const id = Number( c[SKILLDATA_COL_ID] );
			if ( !Number.isInteger( id ) || id <= 0 ) continue;
			rows.push( {
				id,
				levelName: name,
				animBaseName: c[SKILLDATA_COL_ANIMSET_BASE] ?? ""
			} );
		}
	}
	return rows;
}

/*
================
buildEffectRecordTable

 * Build the f0902c effect-record table: skilldata numeric id -> the
 * WipSkillEffectRecord-shaped record referenced by the row's base anim-set
 * name or, for directly-authored monster/pet rows, its exact level name.
 * Rows with no authored skillaniset record remain absent.
================
*/
export function buildEffectRecordTable(
	textdataDir,
	skillEffectPath,
	primitiveSoundRoot = null,
	clientSkillEffectPath = null
) {
	const aniSets = parseSkillAniSet( skillEffectPath );
	const parsedEffectSets = parseSkillEffectSet( skillEffectPath, primitiveSoundRoot );
	const effectSets = clientSkillEffectPath ?
		adaptSkillEffectSetsToClientNative(
			parsedEffectSets,
			parseSkillEffectSet( clientSkillEffectPath )
		) :
		parsedEffectSets;
	const rows = loadSkillDataRows( textdataDir );
	const table = {};
	let matched = 0;
	let builtinRegistered = 0;

	/*
  ================
  addRecord

  Compile the shared record once for numeric and named lookup tables.
  ================
  */
	const addRecord = ( id, ani, destination = table ) => {
		// Native sub_91dae0 appends every comma-separated ANI_* token into one
		// of six fixed four-word tables.  At 0x91defd it reads the table's byte
		// count from record+0x08[index], writes the next u16 at
		// record+0x0e + (index * 4 + count) * 2, then increments that SAME count
		// byte at 0x91df0a.  The count is therefore the table cardinality, not a
		// boolean presence flag: SkillEffectObj_Init later selects
		// table[animPick % count].  Collapsing a four-entry basic-attack table to
		// one made every cast select only ANI_ATTACK1.
		const c0 = ani.readyAnims.length;
		const c1 = ani.waitAnims.length;
		const c2 = ani.shotAnims.length;
		const animSlotKey = animationSetKeyForName( ani.aniGroup );
		if ( animSlotKey === null ) {
			throw new Error(
				`skillaniset ${ani.baseName}: unknown native AniGroup ${ani.aniGroup}`
			);
		}
		const authoredStages = resolveSkillEffectSetRows( ani.baseName, effectSets );
		destination[id] = {
			// The record fields SkillEffectObj_Init (8e06e0) reads. Anim ids are
			// authored group names (the twin owns the actual anim playback); the
			// COUNTS gate haveAnim -> SetMotionState(2). Native record+0x04 is the
			// pointer returned by StdStringAtomPool_FindOrIntern; the four authored
			// v1.150 names resolve to the matching global std::string object
			// addresses used by the animation-set maps. The map comparator is the
			// std::string content, so the two native ONEHAND_STAFF globals are
			// semantically the same animation-set name.
			animSlotKey,
			objectResource: ani.objectResource,
			attachedMotion: ani.actionWaitAnims.length ?
				{
					set: ani.aniGroup.toLowerCase(),
					id: SKILL_EFFECT_ANIMATION_ID_BY_NAME.get( ani.actionWaitAnims[0] )
				} :
				null,
			flags02: Number( ani.hideWeapon ) & 0xff,
			priority00: ani.priority,
			overlap01: ani.overlap,
			// 91DAE0 stores six animation tables before resource slots +44/+48.
			// These legacy numeric placeholders are not resource handles. Portable
			// consumers use defenseEffectPath/damageEffectPath for eligibility;
			// authoredActionNames retains the old animation-name projection only.
			applyValue44: 0,
			gate48: 0,
			gate58: 0,
			authoredActionNames: {
				ready: ani.actionReady,
				wait: ani.actionWait,
				shot: ani.actionShot
			},
			animCount0: c0,
			animCount1: c1,
			animCount2: c2,
			animTable0: ani.readyAnims,
			animTable1: ani.waitAnims,
			animTable2: ani.shotAnims,
			animTable3: ani.actionReadyAnims,
			animTable4: ani.actionWaitAnims,
			animTable5: ani.actionShotAnims,
			animCount3: ani.actionReadyAnims.length,
			animCount4: ani.actionWaitAnims.length,
			animCount5: ani.actionShotAnims.length,
			defenseEffectPath: ani.defenseEffectPath,
			authoredShotAnimationNames: ani.shotAnims,
			impactRowCapacity: deriveTargetResultCapacity( ani.baseName, authoredStages ),
			hasInitialOffset: false,
			animBaseName: ani.baseName,
			damageEffectPath: ani.damageEffectPath,
			hitLight: ani.hitLight,
			criticalDamageEffectPath: ani.criticalDamageEffectPath,
			arrowTrailEffectPath: ani.arrowTrailEffectPath,
			arrowForceEffectPath: ani.arrowForceEffectPath,
			hideWeapon: ani.hideWeapon,
			byteBe: Number( ani.attackSkillFlagBe ?? 0 ) & 0xff,
			authoredStages
		};
	};

	for ( const row of rows ) {
		const ani = aniSets.get( row.animBaseName ) ?? aniSets.get( row.levelName );
		if ( !ani ) continue;
		matched++;
		addRecord( row.id, ani );
	}

	// sub_bbeab0 registers these names before sub_91e720 parses
	// skilleffectset. They do not have positive SkillData rows, so joining only
	// through SkillData silently dropped the complete SYSTEM_* record family.
	// A registered name without a v1.150 skillaniset row remains absent, just
	// like native (SYSTEM_COS_MPPOTION is one such archive/version mismatch).
	for ( const [name, id] of BUILTIN_EFFECT_NAME_ID_PAIRS ) {
		const ani = aniSets.get( name );
		if ( !ani ) continue;
		addRecord( id, ani );
		builtinRegistered++;
	}
	// 917340 has a distinct name-keyed map, including externally used items.
	const namedTable = {};
	for ( const [name, ani] of aniSets ) addRecord( name, ani, namedTable );
	return {
		table,
		namedTable,
		skillDataRows: rows.length,
		aniSets: aniSets.size,
		effectSets: effectSets.size,
		matched,
		builtinRegistered
	};
}
