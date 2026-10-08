/*
===========================================================================
copySkyImages.mjs - publish sky images and native lens mip resources

Every world producer shares the lens prerequisite, including standalone
region builds that do not pass through the full resource build entry point.
===========================================================================
*/
import { copyIntoPublicTree } from "../../shared/publicWrite.mjs";
import { buildNativeSkyStarPrimitive } from "../../../../apps/client-next/src/engine/foundation/rendering/star-construction.ts";
export { buildNativeSkyStarPrimitive };
import { copyFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { toPublicImagePath } from "../../shared/assetPaths.mjs";
import { exists } from "../io.mjs";
import { imagePublicRoot, imageSourceRoot, normalizeAssetPath } from "../paths.mjs";
import { resolveLoginSkyEnvironment } from "./environment/index.mjs";
import { buildNativeLensResources } from "../../shared/nativeLensResources.mjs";

// Title/login renders the Constantinople city, which belongs to the "동유럽" (E.Europe) zone,
// so its sky uses that zone's town env ("Env8" family, env key 33), derived (not hardcoded)
// from the environment.ifo region->env tree. Unlike the unused authored "로그인" node (env 24 / "Env23":
// cyan, star-less night), Env8 has a violet starry night, a yellow dawn, and no midday stars -
// matching the original night login (bright day/night star field via env track 0x36c). The
// general per-region catalog lives in environment.json; any other map resolves its own key.

// Sun disc binds native MapRenderer+0x297f = the 2nd lens texture (sun\lens2.ddj); the moon
// cycles 29 lunar-phase textures (sun\moon01..29.ddj, phase index 0..28 < 0x1d). 30 phase files
// exist on disk; only 01..29 are reachable by the draw gate.
const NATIVE_SUN_TEXTURE_SOURCE = "sun/lens2.ddj";
const FLARE_TEXTURE_SOURCES = Array.from( { length: 8 }, ( _, index ) => `sun/lens${index + 1}.ddj` );
const NATIVE_MOON_PHASE_COUNT = 29;
const MOON_TEXTURE_SOURCES = Array.from(
	{ length: NATIVE_MOON_PHASE_COUNT },
	( _unused, index ) => `sun/moon${String( index + 1 ).padStart( 2, "0" )}.ddj`
);

const SKYBOX_TEXTURES = [
	...[ "rain1", "rain2", "rain3", "snow1", "snow2" ].map( name => ({
		role: "weather",
		sourcePath: `weather/${name}.ddj`
	}) ),
	{ role: "glow", sourcePath: "skybox/glow.ddj" },
	{ role: "cloud", sourcePath: "skybox/cloud1.ddj" },
	{ role: "shadowSphere", sourcePath: "skybox/shadowsphere.ddj" },
	{ role: "sun", sourcePath: NATIVE_SUN_TEXTURE_SOURCE },
	...FLARE_TEXTURE_SOURCES.filter( sourcePath => sourcePath !== NATIVE_SUN_TEXTURE_SOURCE ).map( sourcePath => ({
		role: "flare",
		sourcePath
	}) ),
	...MOON_TEXTURE_SOURCES.map( ( sourcePath ) => ({ role: "moon", sourcePath }) )
];

/*
================
resolveSkyTextures
================
*/
export function resolveSkyTextures() {
	return {
		nativeSkyDistance: 3500,
		textures: SKYBOX_TEXTURES.map( ( texture ) => ({
			...texture,
			publicPath: skyImagePublicPath( texture.sourcePath )
		}) ),
		glowTexturePublicPath: skyImagePublicPath( "skybox/glow.ddj" ),
		cloudTexturePublicPath: skyImagePublicPath( "skybox/cloud1.ddj" ),
		cloudTextureFactorAlpha: null,
		cloudTextureFactorAlphaByte: null,
		shadowSphereTexturePublicPath: skyImagePublicPath( "skybox/shadowsphere.ddj" ),
		sunTexturePublicPath: skyImagePublicPath( NATIVE_SUN_TEXTURE_SOURCE ),
		flareTexturePublicPaths: FLARE_TEXTURE_SOURCES.map( source =>
			skyImagePublicPath( source ).replace( /\.png$/, ".texture" )
		),
		moonTexturePublicPaths: MOON_TEXTURE_SOURCES.map( ( sourcePath ) => skyImagePublicPath( sourcePath ) ),
		starPrimitive: buildNativeSkyStarPrimitive(),
		environment: resolveLoginSkyEnvironment()
	};
}

/*
================
copyReferencedSkyImages
================
*/
export async function copyReferencedSkyImages( skyTextures ) {
	await buildNativeLensResources();
	for ( const texture of skyTextures.textures ) {
		const relativeImagePath = skyImageRelativePath( texture.sourcePath );
		const source = path.join( imageSourceRoot, "Map_extracted", ...relativeImagePath.split( "/" ) );
		const target = path.join( imagePublicRoot, "Map_extracted", ...relativeImagePath.split( "/" ) );

		if ( !(await exists( source )) ) {
			throw new Error( `Missing converted sky texture ${source}; run the DDJ image conversion first.` );
		}

		await copyIntoPublicTree( source, target );
		if ( FLARE_TEXTURE_SOURCES.includes( texture.sourcePath ) ) {
			const resource = source.replace( /\.png$/, ".texture" );
			if ( !(await exists( resource )) ) throw Error( `Native lens generation did not publish ${resource}` );
			await copyIntoPublicTree( resource, target.replace( /\.png$/, ".texture" ) );
		}
	}
}

/*
================
skyImagePublicPath
================
*/
function skyImagePublicPath( sourcePath ) {
	return toPublicImagePath( "Map_extracted", sourcePath );
}

/*
================
skyImageRelativePath
================
*/
function skyImageRelativePath( sourcePath ) {
	return normalizeAssetPath( sourcePath ).replace( /\.[^.]+$/, ".png" );
}
