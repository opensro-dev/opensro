// Build-time VAT payloads for repeated crowd actors.
//
// The browser runtime still imports the GLB for geometry/materials, but it no longer
// samples every animation frame and creates VAT data during the title loading screen.
// This builder imports the already-exported GLB under Babylon's NullEngine and serializes
// the same skeleton matrix texture bytes the runtime would otherwise bake.

import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import "@babylonjs/loaders/glTF/index.js";
import "@babylonjs/core/Shaders/rgbdDecode.fragment.js";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine.js";
import { Scene } from "@babylonjs/core/scene.js";
import { SceneLoader } from "@babylonjs/core/Loading/sceneLoader.js";
import { Mesh } from "@babylonjs/core/Meshes/mesh.js";
import { Logger } from "@babylonjs/core/Misc/logger.js";
import fs from "node:fs";
import { normalizePublicPath, publicPathToFile } from "../shared/assetPaths.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readJsonOrNullSync } from "../shared/jsonOut.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runVatPipeline } from "./vatPipeline.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const rosterPath = path.join( publicRoot, "assets", "char", "roster.json" );

export const CROWD_VAT_FORMAT = "sro-avatar-vat";
export const CROWD_VAT_VERSION = 1;
// v6: the manifest carries no generatedAt, so the same GLB bakes to the same bytes everywhere.
export const CROWD_VAT_COMPILER_VERSION = "babylon-nullengine-v6-bone-coverage-f16-reproducible";
export const CROWD_VAT_STAND_FRAME_CAP = 2;
export const CROWD_VAT_CLIP_ROLES = [ "walk", "stand", "ride" ];
const CROWD_VAT_MOUNT_CLIP_ROLES = [ "walk", "stand" ];

// Half-float precision census thresholds: a
// model whose max |f32 - f16| exceeds the warn threshold is reported loudly;
// beyond the admissible error the model stays float32 (the manifest is
// per-model, so mixed componentType packs are natural and quality is preserved).
export const VAT_F16_WARN_ABS_ERROR = 0.01;
export const VAT_F16_MAX_ABS_ERROR = 0.05;

// The crowd baker's own settings. buildNpcVatAssets.mjs shares the bake core
// below with different settings (full stand clips, native-object-preview
// material metadata), so everything payload-shaping is parameterized here.
const CROWD_VAT_SETTINGS = {
	format: CROWD_VAT_FORMAT,
	version: CROWD_VAT_VERSION,
	compilerVersion: CROWD_VAT_COMPILER_VERSION,
	clipRoles: CROWD_VAT_CLIP_ROLES,
	requiredClipRoles: CROWD_VAT_CLIP_ROLES,
	standFrameCap: CROWD_VAT_STAND_FRAME_CAP,
	materialMode: "world-unlit"
};

Logger.LogLevels = Logger.NoneLogLevel;

export { sha256Hex };

// IEEE 754 binary32 -> binary16 with round-to-nearest-even. Bone matrix
// elements are local transforms whose magnitudes sit far inside half range,
// but the conversion stays defensive: NaN encodes a quiet half NaN, and
// Infinity / overflow / rounding-into-infinity all clamp to the largest
// finite half (65504) so no texture texel ever carries an infinity.
const f32Scratch = new Float32Array( 1 );
const u32Scratch = new Uint32Array( f32Scratch.buffer );
const F16_MAX_FINITE_BITS = 0x7bff; // 65504

export function float32ToFloat16Bits( value ) {
	f32Scratch[0] = value;
	const bits = u32Scratch[0];
	const sign = (bits >>> 16) & 0x8000;
	const exponent = (bits >>> 23) & 0xff;
	const mantissa = bits & 0x7fffff;

	if ( exponent === 0xff ) {
		return mantissa !== 0 ? sign | 0x7e00 : sign | F16_MAX_FINITE_BITS;
	}

	const halfExponent = exponent - 112; // rebias: 2^(e-127) -> 2^(h-15)
	if ( halfExponent >= 0x1f ) {
		return sign | F16_MAX_FINITE_BITS;
	}
	if ( halfExponent <= 0 ) {
		if ( halfExponent < -10 ) {
			return sign; // below half subnormal range: flush to signed zero
		}
		const fullMantissa = mantissa | 0x800000;
		const shift = 14 - halfExponent;
		let half = fullMantissa >>> shift;
		const remainder = fullMantissa & ((1 << shift) - 1);
		const halfway = 1 << (shift - 1);
		if ( remainder > halfway || (remainder === halfway && (half & 1) !== 0) ) {
			half += 1; // a carry lands on the smallest normal encoding, still valid
		}
		return sign | half;
	}

	let half = (halfExponent << 10) | (mantissa >>> 13);
	const remainder = mantissa & 0x1fff;
	if ( remainder > 0x1000 || (remainder === 0x1000 && (half & 1) !== 0) ) {
		half += 1; // may carry into the exponent (round up to the next binade)
	}
	if ( (half & 0x7fff) >= 0x7c00 ) {
		return sign | F16_MAX_FINITE_BITS; // rounding overflowed into infinity
	}
	return sign | half;
}

export function float16BitsToFloat32( bits ) {
	const sign = (bits & 0x8000) !== 0 ? -1 : 1;
	const exponent = (bits >>> 10) & 0x1f;
	const mantissa = bits & 0x3ff;
	if ( exponent === 0 ) {
		return sign * mantissa * 2 ** -24;
	}
	if ( exponent === 0x1f ) {
		return mantissa !== 0 ? Number.NaN : sign * Infinity;
	}
	return sign * (1 + mantissa / 1024) * 2 ** (exponent - 15);
}

// Convert a sampled float32 VAT payload to half floats and measure the
// round-trip damage. A non-finite source value (never expected from a bake)
// forces maxAbsError to Infinity, which selects the precision-preserving
// float32 path instead of silently shipping a clamped texel.
export function convertVatFloat32ToFloat16( data ) {
	const half = new Uint16Array( data.length );
	let maxAbsError = 0;
	for ( let i = 0; i < data.length; i += 1 ) {
		const value = data[i];
		const bits = float32ToFloat16Bits( value );
		half[i] = bits;
		const error = Number.isFinite( value ) ?
			Math.abs( value - float16BitsToFloat32( bits ) ) :
			Infinity;
		if ( error > maxAbsError ) {
			maxAbsError = error;
		}
	}
	return { half, maxAbsError };
}

export { normalizePublicPath };

export function publicPathToDisk( publicPath ) {
	return publicPathToFile( publicPath, publicRoot );
}

function vatPublicPathsForGlb( glbPublicPath ) {
	const normalized = normalizePublicPath( glbPublicPath );
	const rel = (normalized.startsWith( "/assets/char/" ) ?
		normalized.replace( /^\/assets\/char\//, "" ) :
		normalized.replace( /^\/assets\//, "" )).replace( /\.glb$/i, "" );
	return {
		manifest: `/assets/char/vat/${rel}.vat.json`,
		bin: `/assets/char/vat/${rel}.vat.bin`
	};
}

function clipMeta( group, frameCap ) {
	const from = Math.floor( group.from );
	const to = Math.ceil( group.to );
	const sourceFrameCount = Math.max( 2, to - from + 1 );
	const frameCount = Math.max( 2, Math.min( sourceFrameCount, frameCap ?? sourceFrameCount ) );
	const baseFps = group.targetedAnimations[0]?.animation.framePerSecond ?? 30;
	const durationSeconds = Math.max( 0.1, (group.to - group.from) / Math.max( 1, baseFps ) );
	return {
		role: group.name.toLowerCase(),
		from,
		sourceFrameCount,
		frameCount,
		fps: frameCount / durationSeconds,
		sourceFps: baseFps
	};
}

// Per-model precision report shared by both bakers: prints the max-abs-error
// census line and warns loudly at the thresholds above.
export function reportVatPrecision( logTag, label, baked ) {
	const errorText = baked.f16MaxAbsError.toExponential( 2 );
	if ( baked.componentType === "float32" ) {
		console.warn(
			`[${logTag}] ${label}: f16 max-abs-error ${errorText} > ${VAT_F16_MAX_ABS_ERROR}; keeping float32`
		);
	} else if ( baked.f16MaxAbsError > VAT_F16_WARN_ABS_ERROR ) {
		console.warn(
			`[${logTag}] ${label}: WARNING f16 max-abs-error ${errorText} > ${VAT_F16_WARN_ABS_ERROR} (shipping float16)`
		);
	} else {
		console.log( `[${logTag}] ${label}: float16 max-abs-error ${errorText}` );
	}
}

export function createVatPrecisionCensus() {
	return { baked: 0, maxAbsError: 0, warned: [], float32Assets: [] };
}

export function recordVatPrecision( census, label, baked ) {
	census.baked += 1;
	if ( baked.f16MaxAbsError > census.maxAbsError ) {
		census.maxAbsError = baked.f16MaxAbsError;
	}
	if ( baked.componentType === "float32" ) {
		census.float32Assets.push( label );
	} else if ( baked.f16MaxAbsError > VAT_F16_WARN_ABS_ERROR ) {
		census.warned.push( label );
	}
}

export function formatVatPrecisionCensus( census ) {
	if ( census.baked === 0 ) {
		return "no models rebaked";
	}
	return (
		`max-abs-error ${census.maxAbsError.toExponential( 2 )} over ${census.baked} baked, ` +
		`${census.warned.length} above warn (${census.warned.join( ", " ) || "none"}), ` +
		`${census.float32Assets.length} float32 asset (${census.float32Assets.join( ", " ) || "none"})`
	);
}

export function readExistingManifest( filePath ) {
	return readJsonOrNullSync( filePath );
}

export function isExistingVatFresh( manifest, binPath, details, settings = CROWD_VAT_SETTINGS ) {
	if ( !manifest || !fs.existsSync( binPath ) ) return false;
	return (
		manifest.format === settings.format &&
		manifest.version === settings.version &&
		manifest.compilerVersion === settings.compilerVersion &&
		manifest.source?.sha256 === details.glbSha256 &&
		manifest.settings?.materialMode === settings.materialMode &&
		(manifest.settings?.standFrameCap ?? null) === (settings.standFrameCap ?? null) &&
		JSON.stringify( manifest.settings?.clipRoles ?? [] ) === JSON.stringify( settings.clipRoles ) &&
		manifest.bin?.byteLength === fs.statSync( binPath ).size
	);
}

async function importGlbIntoNullScene( glbBytes ) {
	const engine = new NullEngine( { renderWidth: 64, renderHeight: 64, textureSize: 64 } );
	const scene = new Scene( engine );
	try {
		const dataUrl = `data:model/gltf-binary;base64,${glbBytes.toString( "base64" )}`;
		const result = await SceneLoader.ImportMeshAsync( undefined, "", dataUrl, scene, undefined, ".glb" );
		return { engine, scene, result };
	} catch ( error ) {
		engine.dispose();
		throw error;
	}
}

export async function bakeVatFromGlb( { glbBytes, glbPublicPath, glbSha256 }, settings = CROWD_VAT_SETTINGS ) {
	const { engine, result } = await importGlbIntoNullScene( glbBytes );
	try {
		const skinned = result.meshes.filter( ( mesh ) =>
			mesh instanceof Mesh && mesh.skeleton && mesh.getTotalVertices() > 0
		);
		if ( skinned.length === 0 || result.animationGroups.length === 0 ) {
			throw new Error( `${glbPublicPath}: missing skinned meshes or animation groups` );
		}

		const skeleton = skinned[0].skeleton;
		const boneCount = skeleton.bones.length;
		const floatsPerFrame = (boneCount + 1) * 16;
		const groupsByRole = new Map( result.animationGroups.map( ( group ) => [ group.name.toLowerCase(), group ] ) );
		const missingRequiredRoles = (settings.requiredClipRoles ?? []).filter(
			( role ) => !groupsByRole.has( role.toLowerCase() )
		);
		if ( missingRequiredRoles.length > 0 ) {
			throw new Error( `${glbPublicPath}: missing required VAT clip(s): ${missingRequiredRoles.join( ", " )}` );
		}
		const metas = [];
		for ( const role of settings.clipRoles ) {
			// Imported glTF animation names are matched case-insensitively. Keep
			// both sides of the lookup in the same canonical key space; otherwise
			// camel-cased roles such as deathLoop are present in the GLB but are
			// silently omitted from every baked VAT artifact.
			const group = groupsByRole.get( role.toLowerCase() );
			if ( !group ) continue;
			// Babylon normalizes imported AnimationGroup names to lower case. The
			// artifact schema must retain the canonical contract role (deathLoop),
			// not leak that loader normalization as a new role named deathloop.
			metas.push( {
				...clipMeta( group, role === "stand" ? (settings.standFrameCap ?? undefined) : undefined ),
				role
			} );
		}
		if ( metas.length === 0 ) {
			// No requested clip in this GLB (attack/die-only bakes): the caller
			// counts it as skipped rather than publishing a VAT nothing would play.
			return null;
		}

		// Native models clip COVERAGE structurally: CAniMixer_SamplePose (adda10,
		// raw dump 0x00addae0..0x00addb41) walks the animation's OWN track-binding
		// list, so a clip only ever pushes a pose into the sockets it actually
		// drives. v1.150 relies on this - the ordinary hit reaction drives 4 bones
		// and layers over a 42-bone run. A VAT frame is a full matrix per bone and
		// cannot express "this clip is silent here", so coverage is published as a
		// dedicated row per clip and re-applied per bone at sample time.
		const coveredBoneIndicesByRole = new Map();
		const boneIndexByName = new Map( skeleton.bones.map( ( bone, index ) => [ bone.name, index ] ) );
		for ( const meta of metas ) {
			const group = groupsByRole.get( meta.role.toLowerCase() );
			const covered = new Set();
			for ( const targeted of group?.targetedAnimations ?? [] ) {
				// glTF import drives the bone's linked transform node; both carry the
				// authored bone name, which is the only stable key across that seam.
				const index = boneIndexByName.get( targeted.target?.name );
				if ( index !== undefined ) covered.add( index );
			}
			coveredBoneIndicesByRole.set( meta.role, covered );
		}

		// One coverage row per clip, immediately BEFORE its frames, so the sampler
		// can address it as (clipStartFrame - 1) without spending a channel of the
		// already-full lane settings vec4.
		const totalFrames = metas.reduce( ( sum, meta ) => sum + meta.frameCount + 1, 0 );
		const data = new Float32Array( floatsPerFrame * totalFrames );
		const mesh = skinned[0];
		let frameCursor = 0;
		const clips = {};

		for ( const meta of metas ) {
			const group = groupsByRole.get( meta.role.toLowerCase() ) ?? result.animationGroups[0];
			const coverageFrame = frameCursor;
			const startFrame = frameCursor + 1;

			// Coverage row: 1.0 in the bone matrix's m[15] slot for every socket this
			// clip drives, 0.0 otherwise. m[15] is the homogeneous row's w component,
			// which the skinning path never reads, so the row costs no new sampler.
			const coveredBoneIndices = coveredBoneIndicesByRole.get( meta.role ) ?? new Set();
			for ( const boneIndex of coveredBoneIndices ) {
				data[coverageFrame * floatsPerFrame + boneIndex * 16 + 15] = 1;
			}

			// A partial clip leaves every bone it does not drive exactly where the
			// PREVIOUS clip's bake left the skeleton, which made the baked pose depend
			// on clip ORDER (measured: mangnyang hit1 rows carried attack1's final leg
			// pose byte-identically). Rest the skeleton first so the untargeted bones
			// are a defined bind pose; coverage then masks them out at sample time.
			skeleton.returnToRest();
			group.start( false );
			group.pause();
			for ( let i = 0; i < meta.frameCount; i += 1 ) {
				group.goToFrame( meta.from + i );
				skeleton.prepare( true );
				data.set( skeleton.getTransformMatrices( mesh ), (startFrame + i) * floatsPerFrame );
			}
			group.stop();

			clips[meta.role] = {
				coverageFrame,
				startFrame,
				endFrame: startFrame + meta.frameCount - 1,
				frameCount: meta.frameCount,
				fps: meta.fps,
				sourceFrom: meta.from,
				sourceFrameCount: meta.sourceFrameCount,
				sourceFps: meta.sourceFps,
				coveredBoneCount: coveredBoneIndices.size,
				coversWholeSkeleton: coveredBoneIndices.size === boneCount
			};
			frameCursor = startFrame + meta.frameCount;
		}

		// Halve the payload: half floats unless this model's matrices round-trip
		// badly enough to cross the admissible error (then it stays float32).
		const { half, maxAbsError } = convertVatFloat32ToFloat16( data );
		const useFloat16 = maxAbsError <= VAT_F16_MAX_ABS_ERROR;
		const componentType = useFloat16 ? "float16" : "float32";

		return {
			data: useFloat16 ? half : data,
			componentType,
			f16MaxAbsError: maxAbsError,
			manifestCore: {
				format: settings.format,
				version: settings.version,
				compilerVersion: settings.compilerVersion,
				source: {
					glb: glbPublicPath,
					sha256: glbSha256,
					byteLength: glbBytes.length
				},
				settings: {
					componentType,
					materialMode: settings.materialMode,
					standFrameCap: settings.standFrameCap ?? null,
					clipRoles: settings.clipRoles
				},
				skeleton: {
					boneCount,
					bones: skeleton.bones.map( ( bone, index ) => ({ index, name: bone.name }) )
				},
				meshes: skinned.map( ( mesh, index ) => ({
					index,
					name: mesh.name,
					vertexCount: mesh.getTotalVertices()
				}) ),
				texture: {
					width: (boneCount + 1) * 4,
					height: totalFrames,
					floatsPerFrame,
					frameCount: totalFrames
				},
				clips,
				// Consumers gate per-bone lane weighting on this. Absent (older
				// artifacts) means "no coverage rows"; the runtime then falls back to
				// whole-clip lane blending exactly as before.
				boneCoverage: {
					encoding: "per-clip-row-m15",
					rowBeforeClipStart: true
				},
				sharedAcrossMeshes: true
			}
		};
	} finally {
		engine.dispose();
	}
}

export async function buildCrowdVatAssets( options = {} ) {
	const inputRosterPath = options.rosterPath ?? rosterPath;
	const result = await runVatPipeline( {
		manifestPath: inputRosterPath,
		missingResult: { built: 0, reused: 0, failed: 0, assetCount: 0 },
		settings: CROWD_VAT_SETTINGS,
		settingsForModel: ( model, settings ) =>
			model.isMount === true ?
				{ ...settings, clipRoles: CROWD_VAT_MOUNT_CLIP_ROLES, requiredClipRoles: CROWD_VAT_MOUNT_CLIP_ROLES } :
				settings,
		logTag: "crowd-vat",
		getModels: ( roster ) => (Array.isArray( roster.models ) ? roster.models : []),
		classifyModel: ( model ) => (!model?.glb || model.error ? "ignore" : "include"),
		vatPublicPathsForGlb,
		publicPathToDisk,
		bakeVatFromGlb,
		isExistingVatFresh,
		createVatPrecisionCensus,
		reportVatPrecision,
		recordVatPrecision,
		createVatReference: ( manifest, vatPublic ) => ({
			manifest: vatPublic.manifest,
			bin: vatPublic.bin,
			bytes: manifest.bin.byteLength,
			frames: manifest.texture.frameCount,
			clips: Object.keys( manifest.clips ),
			standFrameCap: manifest.settings.standFrameCap,
			compilerVersion: manifest.compilerVersion
		}),
		updateManifest: ( roster ) => {
			roster.vat = {
				format: CROWD_VAT_FORMAT,
				version: CROWD_VAT_VERSION,
				compilerVersion: CROWD_VAT_COMPILER_VERSION,
				standFrameCap: CROWD_VAT_STAND_FRAME_CAP,
				clipRoles: CROWD_VAT_CLIP_ROLES
			};
		},
		refreshSidecars: true
	} );
	if ( result.failed > 0 ) {
		throw new Error( `[crowd-vat] ${result.failed} required crowd VAT asset(s) failed to build` );
	}
	if ( !result.precision ) return result;
	return {
		built: result.built,
		reused: result.reused,
		failed: result.failed,
		precision: result.precision,
		assetCount: result.assetCount
	};
}

if ( isMainScript( import.meta.url ) ) {
	const result = await buildCrowdVatAssets();
	console.log(
		`[crowd-vat] built ${result.built}, reused ${result.reused}, failed ${result.failed}, indexed ${result.assetCount}`
	);
	console.log( `[crowd-vat] precision census: ${formatVatPrecisionCensus( result.precision )}` );
}
