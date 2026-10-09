// Split verbatim from resourcePipeline.mjs (2026-07-28): audio cluster -
// music/sfx copies plus the effectenvsnd/effectsound/regioninfo catalogs.
import { copyIntoPublicTree } from "./publicWrite.mjs";
import { copyFile, mkdir, readdir, stat, readFile } from "node:fs/promises";
import path from "node:path";

import {
	audioPublicRoot,
	dataRoot,
	exists,
	musicSourceRoot,
	normalizeAssetPath,
	readText,
	resinfoDir,
	splitHierarchyTokens,
	splitIndexedRowCells,
	stripQuotes,
	textDataDir,
	toGameRelative,
	writeJson
} from "./resourceIo.mjs";
import { iterateTextDataLines } from "./textDataIo.mjs";

const effectEnvSndPath = path.join( textDataDir, "effectenvsnd.txt" );
const effectSoundPath = path.join( textDataDir, "effectsound.txt" );
const clientEffectSoundPath = path.join( resinfoDir, "effectsound.txt" );
const regionInfoPath = path.join( textDataDir, "regioninfo.txt" );
// The textdata copy is authoritative for sounds, same as effectsound.txt: the
// resinfo copy's skilleffectset sound set is a strict subset (measured
// 2026-07-29: 35 unique wavs, all also present among textdata's 169).
const skillEffectPath = path.join( textDataDir, "skilleffect.txt" );
export async function buildAudioResources() {
	const audioCatalog = await buildAudioCatalog();
	await writeJson( path.join( audioPublicRoot, "catalog.json" ), audioCatalog );

	// Shared across the env and effectsound catalogs so a wav referenced by
	// both tables is copied into public/assets/audio/sfx/ exactly once. The
	// env catalog runs first; the effectsound pass then sees the entry in the
	// set and skips the second copy.
	const copiedSfxAssets = new Set();
	await buildAlarmSoundResource( copiedSfxAssets );
	await buildWeatherSoundResources( copiedSfxAssets );
	await buildNativeDirectSoundResources( copiedSfxAssets );

	const effectEnvSndCatalog = await buildEffectEnvSndCatalog( copiedSfxAssets );
	await writeJson( path.join( audioPublicRoot, "effectenvsnd.json" ), effectEnvSndCatalog );

	const effectSoundCatalog = await buildEffectSoundCatalog( copiedSfxAssets );
	await writeJson( path.join( audioPublicRoot, "effectsound.json" ), effectSoundCatalog );

	const skillEffectSoundCatalog = await buildSkillEffectSoundCatalog( copiedSfxAssets );
	await writeJson( path.join( audioPublicRoot, "skilleffectsound.json" ), skillEffectSoundCatalog );

	const regionInfoCatalog = await buildRegionInfoCatalog();
	await writeJson( path.join( audioPublicRoot, "regioninfo.json" ), regionInfoCatalog );

	return {
		audioCatalog,
		effectEnvSndCatalog,
		effectSoundCatalog,
		skillEffectSoundCatalog,
		regionInfoCatalog
	};
}
async function buildAudioCatalog() {
	const musicPublicRoot = path.join( audioPublicRoot, "music" );
	await mkdir( musicPublicRoot, { recursive: true } );

	const musicByName = {};
	const musicByOriginalName = {};
	const entries = await readdir( musicSourceRoot, { withFileTypes: true } );

	for ( const entry of entries ) {
		if ( !entry.isFile() || path.extname( entry.name ).toLowerCase() !== ".mp3" ) {
			continue;
		}

		const source = path.join( musicSourceRoot, entry.name );
		const target = path.join( musicPublicRoot, entry.name );
		await copyIntoPublicTree( source, target );

		const normalizedMp3 = normalizeAudioFileName( entry.name );
		const normalizedOgg = normalizedMp3.replace( /\.mp3$/, ".ogg" );
		const publicPath = `/assets/audio/music/${entry.name}`;
		const track = {
			sourcePath: toGameRelative( source ),
			publicPath,
			kind: "music",
			codec: "mp3",
			bytes: (await stat( source )).size
		};

		musicByName[normalizedMp3] = track;
		musicByOriginalName[normalizedMp3] = publicPath;
		musicByOriginalName[normalizedOgg] = publicPath;
	}

	return {
		sourcePath: toGameRelative( musicSourceRoot ),
		musicByName,
		musicByOriginalName
	};
}

/**
 * @typedef {{ fileName: string, sourcePath: string, publicPath: string | undefined, min: number, max: number, always: boolean }} EnvAmbienceLayer
 * @typedef {{ name: string, bgmTrack: string, bgmPublicPath: string | undefined, ambience: { day: EnvAmbienceLayer[], night: EnvAmbienceLayer[] }, rawPeriods: Record<string, string> }} EnvSoundProfile
 */
/**
 * @param {Set<string> | null} [copiedSfxAssets] shared copy ledger from the
 *   resource build; pass null (the default) to resolve layer publicPaths
 *   without copying any wav, which the byte-identity probe relies on.
 */
async function buildEffectEnvSndCatalog( copiedSfxAssets = null ) {
	const raw = await readText( effectEnvSndPath );
	const profiles = [];
	/** @type {EnvSoundProfile | null} */
	let currentProfile = null;
	/** @type {"day" | "night" | null} */
	let currentPeriod = null;

	for ( const rawLine of raw.split( /\r?\n/ ) ) {
		// Hierarchical indented format: leading tabs are indentation, so the
		// drop-empties tokenizer is REQUIRED here (the strict indexed splitter
		// would bury the <1>/<2>/<3> markers behind the indentation cells).
		const cells = splitHierarchyTokens( rawLine );
		if ( cells.length === 0 ) {
			continue;
		}

		if ( cells[0] === "<1>" ) {
			currentProfile = {
				name: cells[1] ?? "",
				bgmTrack: "",
				bgmPublicPath: undefined,
				ambience: {
					day: [],
					night: []
				},
				rawPeriods: {}
			};
			profiles.push( currentProfile );
			currentPeriod = null;
			continue;
		}

		if ( !currentProfile ) {
			continue;
		}

		if ( cells[0] === "<2>" ) {
			currentPeriod = normalizeDayPeriod( cells[1] );
			if ( !currentProfile.rawPeriods[currentPeriod] ) {
				currentProfile.rawPeriods[currentPeriod] = cells[1] ?? currentPeriod;
			}
			continue;
		}

		if ( cells[0] === "<3>" && currentPeriod ) {
			const fileName = stripQuotes( cells[1] ?? "" );
			const range = parseAudioRange( cells[2] ?? "0~0" );
			const sourcePath = normalizeAssetPath( `prim/snd/env/${fileName}` );
			const layer = {
				fileName,
				sourcePath,
				publicPath: await copySfxReference( sourcePath, copiedSfxAssets ),
				min: range.min,
				max: range.max,
				always: range.min === 0 && range.max === 0
			};
			currentProfile.ambience[currentPeriod === "night" ? "night" : "day"].push( layer );
			continue;
		}

		if ( !currentProfile.bgmTrack && cells[0]?.startsWith( '"' ) ) {
			currentProfile.bgmTrack = stripQuotes( cells[0] );
			currentProfile.bgmPublicPath = resolveMusicPublicPath( currentProfile.bgmTrack );
		}
	}

	const profilesByName = Object.fromEntries( profiles.map( ( profile ) => [ profile.name, profile ] ) );

	return {
		sourcePath: toGameRelative( effectEnvSndPath ),
		profiles,
		profilesByName
	};
}

/**
 * @param {Set<string>} [copiedSfxAssets] shared copy ledger from the resource
 *   build so wavs also referenced by the env catalog are copied only once.
 */
export async function buildEffectSoundCatalog( copiedSfxAssets = null ) {
	const raw = await readText( effectSoundPath );
	const rules = [];
	/** @type {Record<string, number[]>} */
	const rulesByObjectHandle = {};
	const ruleKeys = new Set();
	const supplementalSourcePaths = [];

	await appendEffectSoundRules( raw, {
		copiedSfxAssets,
		rules,
		rulesByObjectHandle,
		ruleKeys
	} );

	if ( await exists( clientEffectSoundPath ) ) {
		const clientRaw = await readText( clientEffectSoundPath );
		supplementalSourcePaths.push( toGameRelative( clientEffectSoundPath ) );
		await appendEffectSoundRules( clientRaw, {
			copiedSfxAssets,
			includeRule: ( cells ) => normalizeEffectSoundKeyPart( cells[0] ?? "" ) === "UI",
			rules,
			rulesByObjectHandle,
			ruleKeys,
			skipExisting: true
		} );
	}

	return {
		sourcePath: toGameRelative( effectSoundPath ),
		supplementalSourcePaths,
		rules,
		rulesByObjectHandle
	};
}

/**
 * @param {string} raw
 * @param {{
 *   copiedSfxAssets: Set<string>,
 *   includeRule?: (cells: string[]) => boolean,
 *   rules: unknown[],
 *   rulesByObjectHandle: Record<string, number[]>,
 *   ruleKeys: Set<string>,
 *   skipExisting?: boolean
 * }} options
 */
async function appendEffectSoundRules(
	raw,
	{
		copiedSfxAssets,
		includeRule = () => true,
		rules,
		rulesByObjectHandle,
		ruleKeys,
		skipExisting = false
	}
) {
	for ( const rawLine of iterateTextDataLines( raw ) ) {
		const columns = rawLine.split( "\t" ).map( ( column ) => column.trim() );
		const offset = columns[0] ? 0 : 1;
		const cells = columns.slice( offset );

		if ( !cells[0] || cells[0].startsWith( "//" ) ) {
			continue;
		}

		const [object, handle, skillId, event1, event2, event3, skip, folder, fileName, volume, description] = cells;
		if ( !object || !handle || !folder || !fileName ) {
			continue;
		}

		if ( !includeRule( cells ) ) {
			continue;
		}

		const ruleKey = effectSoundRuleKey( { object, handle, skillId, event1, event2, event3, folder, fileName } );
		if ( skipExisting && ruleKeys.has( ruleKey ) ) {
			continue;
		}

		const sourcePath = normalizeAssetPath( `prim/snd/${folder}${fileName}` );
		const publicPath = await copySfxReference( sourcePath, copiedSfxAssets );
		const rule = {
			id: rules.length,
			object,
			handle,
			skillId,
			event1,
			event2,
			event3,
			// Column 7: the rule's skip count. CGEffSoundBody_PlayNamedSound (8F9280)
			// swallows that many triggers between plays (COS_P_CAT SND_STAND 23).
			skip: Math.max( 0, Math.trunc( Number( skip ) ) || 0 ),
			folder: normalizeAssetPath( folder ),
			fileName,
			sourcePath,
			publicPath,
			volume: Number( volume ) || 0,
			description: description ?? ""
		};

		ruleKeys.add( ruleKey );
		rules.push( rule );
		const key = `${normalizeEffectSoundKeyPart( object )}:${normalizeEffectSoundKeyPart( handle )}`;
		if ( !rulesByObjectHandle[key] ) {
			rulesByObjectHandle[key] = [];
		}
		rulesByObjectHandle[key].push( rule.id );
	}
}

function effectSoundRuleKey( { object, handle, skillId, event1, event2, event3, folder, fileName } ) {
	return [ object, handle, skillId, event1, event2, event3, folder, fileName ]
		.map( ( part ) => normalizeEffectSoundKeyPart( part ?? "" ) )
		.join( "\t" );
}

/**
 * @typedef {{ fileName: string, sourcePath: string, publicPath: string | undefined }} SkillEffectSoundRef
 * @typedef {{ id: number, skillEffectId: string, aniType: string, begin: SkillEffectSoundRef | undefined, end: SkillEffectSoundRef | undefined }} SkillEffectSoundEntry
 */
/**
 * skilleffect.txt #section skilleffectset: cast/end sounds per skill effect
 * row. The header pins SndBegin at column 26 and SndEnd at column 27 (29-cell
 * rows); paths are authored relative to prim/snd/ (e.g. player\hwanchange.wav)
 * and "none" means no sound. Column 1 (SkillEffectID) is the BASE skill name
 * space - SKILL_* and SYSTEM_* names without a level suffix, the same
 * id-space skilldata col[5] joins for the char-plane effect records
 * (build/char/parseSkillEffect.mjs) - so the runtime resolves a cast skill to
 * its base name and looks entries up here. Only rows carrying at least one
 * sound are emitted; keys are normalized like the effectsound object/handle
 * keys (trim + uppercase).
 *
 * @param {Set<string> | null} [copiedSfxAssets] shared copy ledger from the
 *   resource build; pass null (the default) to resolve publicPaths without
 *   copying any wav.
 */
async function buildSkillEffectSoundCatalog( copiedSfxAssets = null ) {
	const raw = await readText( skillEffectPath );
	/** @type {SkillEffectSoundEntry[]} */
	const entries = [];
	/** @type {Record<string, number[]>} */
	const entriesBySkillEffectId = {};
	let inSkillEffectSet = false;

	for ( const rawLine of raw.split( /\r?\n/ ) ) {
		// Column-indexed table: SndBegin/SndEnd live at fixed indices 26/27, so
		// the strict splitter that PRESERVES empty cells is REQUIRED here.
		const cells = splitIndexedRowCells( rawLine );
		if ( cells[0] === "#section" ) {
			inSkillEffectSet = cells[1] === "skilleffectset";
			continue;
		}

		if ( !inSkillEffectSet || !cells[1] || cells[0].startsWith( "//" ) ) {
			continue;
		}

		const begin = await resolveSkillEffectSound( cells[26], copiedSfxAssets );
		const end = await resolveSkillEffectSound( cells[27], copiedSfxAssets );
		if ( !begin && !end ) {
			continue;
		}

		const entry = {
			id: entries.length,
			skillEffectId: cells[1],
			aniType: cells[2] ?? "",
			begin,
			end
		};
		entries.push( entry );

		const key = normalizeEffectSoundKeyPart( entry.skillEffectId );
		if ( !entriesBySkillEffectId[key] ) {
			entriesBySkillEffectId[key] = [];
		}
		entriesBySkillEffectId[key].push( entry.id );
	}

	return {
		sourcePath: toGameRelative( skillEffectPath ),
		entries,
		entriesBySkillEffectId
	};
}

/**
 * @param {string | undefined} cell raw SndBegin/SndEnd cell
 * @param {Set<string> | null} copiedSfxAssets
 * @returns {Promise<SkillEffectSoundRef | undefined>}
 */
async function resolveSkillEffectSound( cell, copiedSfxAssets ) {
	const fileName = cell ?? "";
	if ( !fileName || fileName.toLowerCase() === "none" ) {
		return undefined;
	}

	const sourcePath = normalizeAssetPath( `prim/snd/${fileName}` );
	return {
		fileName,
		sourcePath,
		publicPath: await copySfxReference( sourcePath, copiedSfxAssets )
	};
}

/**
 * @param {string} sourcePath
 * @param {Set<string> | null} copiedSfxAssets copy ledger; pass null to only
 *   resolve the publicPath (existence check, no copy) for in-memory probes.
 * @returns {Promise<string | undefined>} undefined when the source wav is
 *   absent from the extract - callers ship no path rather than a broken one.
 */
async function copySfxReference( sourcePath, copiedSfxAssets ) {
	const normalized = normalizeAssetPath( sourcePath );
	const publicPath = `/assets/audio/sfx/${normalized}`;
	const source = path.join( dataRoot, normalized );
	const target = path.join( audioPublicRoot, "sfx", normalized );

	if ( !(await exists( source )) ) {
		return undefined;
	}

	if ( copiedSfxAssets && !copiedSfxAssets.has( publicPath ) ) {
		await copyIntoPublicTree( source, target );
		copiedSfxAssets.add( publicPath );
	}

	return publicPath;
}

async function buildRegionInfoCatalog() {
	const raw = await readText( regionInfoPath );

	return {
		sourcePath: toGameRelative( regionInfoPath ),
		regions: parseRegionInfoRegions( raw )
	};
}

/**
 * @typedef {{ sectorX: number, sectorY: number, coverage: "rect" | "all", rect?: { x: number, y: number, width: number, height: number } }} RegionInfoEntry
 * @typedef {{ kind: "town" | "field", name: string, alias: string | undefined, entries: RegionInfoEntry[] }} RegionInfoRegion
 */
function parseRegionInfoRegions( raw ) {
	const regions = [];
	/** @type {RegionInfoRegion | null} */
	let currentRegion = null;

	for ( const rawLine of raw.split( /\r?\n/ ) ) {
		// Column-indexed table: cells[0..6] are positional, so the strict
		// splitter that PRESERVES empty cells is REQUIRED here (the drop-empties
		// tokenizer would shift every column after an empty middle field).
		const cells = splitIndexedRowCells( rawLine );
		if ( cells.every( ( cell ) => cell.length === 0 ) ) {
			continue;
		}

		if ( cells[0] === "#TOWN" || cells[0] === "#FIELD" ) {
			currentRegion = {
				kind: cells[0] === "#TOWN" ? "town" : "field",
				name: cells[1] ?? "",
				alias: cells[2] || undefined,
				entries: []
			};
			regions.push( currentRegion );
			continue;
		}

		if ( !currentRegion || !/^-?\d+$/.test( cells[0] ?? "" ) ) {
			continue;
		}

		const coverage = cells[2] ?? "ALL";
		/** @type {RegionInfoEntry} */
		const entry = {
			sectorX: Number( cells[0] ) || 0,
			sectorY: Number( cells[1] ) || 0,
			coverage: coverage === "RECT" ? "rect" : "all"
		};

		if ( coverage === "RECT" ) {
			entry.rect = {
				x: Number( cells[3] ) || 0,
				y: Number( cells[4] ) || 0,
				width: Number( cells[5] ) || 0,
				height: Number( cells[6] ) || 0
			};
		}

		currentRegion.entries.push( entry );
	}

	return regions;
}
function parseAudioRange( value ) {
	const [min = 0, max = 0] = value.split( "~" ).map( ( part ) => Number( part.trim() ) || 0 );
	return { min, max };
}

function normalizeDayPeriod( value ) {
	if ( value === "\ubc24" ) {
		return "night";
	}

	return "day";
}

function normalizeAudioFileName( value ) {
	return normalizeAssetPath( stripQuotes( value ) ).split( "/" ).at( -1 ) ?? "";
}

function normalizeEffectSoundKeyPart( value ) {
	return value.trim().toUpperCase();
}

function resolveMusicPublicPath( originalName ) {
	const mp3Name = normalizeAudioFileName( originalName ).replace( /\.ogg$/i, ".mp3" );
	return `/assets/audio/music/${mp3Name}`;
}
export { buildEffectEnvSndCatalog, buildRegionInfoCatalog, parseRegionInfoRegions, resolveMusicPublicPath };

// CGWeatherManager::Initialize 8CF2D0 and SWorld option 25 setter 8A54C0.
export async function buildWeatherSoundResources( copiedSfxAssets = new Set() ) {
	const paths = [];
	for ( const name of [ "lightning1", "lightning2", "lightning3", "rain1" ] ) {
		const source = `prim/snd/etc/${name}.wav`;
		const published = await copySfxReference( source, copiedSfxAssets );
		if ( !published ) throw new Error( `Missing native weather sound ${source}` );
		paths.push( published );
	}
	return paths;
}
// Direct filename producers bypass effectsound.txt. Keep their resource closure
// tied to the extracted native reference census, not a manually growing WAV list.
// A published file is not a claim that its UI/effect/weather trigger is ported.
export async function buildNativeDirectSoundResources( copiedSfxAssets = new Set() ) {
	const source = JSON.parse(
		await readFile( new URL( "../reference/native-audio-surface.json", import.meta.url ), "utf8" )
	);
	if ( source.binarySha256 !== "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" ) {
		throw Error( "Wrong native direct-sound evidence" );
	}
	const rows = [];
	for ( const row of source.strings ) {
		const path = row.value.replaceAll( "\\", "/" ).toLowerCase();
		if ( !/^prim\/snd\/.+\.wav$/.test( path ) ) continue;
		const publicPath = await copySfxReference( path, copiedSfxAssets );
		if ( !publicPath ) throw Error( `Native direct sound absent from extracted data: ${path}` );
		rows.push( {
			sourcePath: path,
			publicPath,
			literalVa: row.va,
			codeReferences: row.codeReferences.map( ref => ref.va )
		} );
	}
	const catalog = {
		format: "sro-native-direct-sounds",
		version: 1,
		binarySha256: source.binarySha256,
		qualification: "Resource publication only; producer closure is audited separately",
		rows
	};
	await writeJson( path.join( audioPublicRoot, "native-direct-sounds.json" ), catalog );
	return catalog;
}

// CIFPlayerMiniInfo 6B6D00 requests this directly, outside effectsound.txt.
export async function buildAlarmSoundResource( copiedSfxAssets = new Set() ) {
	const source = "prim/snd/ui/alarm_sound.wav";
	const published = await copySfxReference( source, copiedSfxAssets );
	if ( !published ) throw new Error( `Missing native alarm sound ${source}` );
	return published;
}
