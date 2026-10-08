// Split verbatim from resourcePipeline.mjs (2026-07-28): title cluster -
// intro scripts, the verified native camera table and the title manifests.
import path from "node:path";

import {
	assertExists,
	cleanNumber,
	extractedRoot,
	mediaRoot,
	normalizeAssetPath,
	publicRoot,
	readText,
	toGameRelative,
	uniqueStrings,
	writeJson
} from "./resourceIo.mjs";
import { optionPath, readClientOptionSettings } from "./configResources.mjs";

const builtTitleIntroNames = [
	"script/intro/constantinople.txt",
	"script/intro/china_wharf.txt"
];
// Verified from SRO_Client.exe v1.150 sub_bbb710 static initializer and
// sub_746fe0's data_cc9f5c..0x00cca11c camera insertion loop. The native title
// path uses this compiled table, not the rounded values in script/intro/*.txt.
const verifiedNativeTitleCameraTables = {
	constantinople: [
		{
			timeSeconds: 0,
			position: { x: 8319.4248, y: 504.605438, z: 468.392426 },
			rotation: { x: -0.979038, y: 4.33498001, z: -0.00683999993 },
			mode: 10
		},
		{
			timeSeconds: 1,
			position: { x: 7380.31787, y: 517.609314, z: 190.489334 },
			rotation: { x: 0.350961, y: 4.55998182, z: -0.00683999993 },
			mode: 10
		},
		{
			timeSeconds: 2,
			position: { x: 6254.55322, y: 147.516434, z: -34.0301743 },
			rotation: { x: 0.0909610018, y: 4.50498104, z: -0.418004006 },
			mode: 10
		},
		{
			timeSeconds: 3,
			position: { x: 5498.979, y: 128.208206, z: -99.3505402 },
			rotation: { x: -0.0340389982, y: 5.11998701, z: -0.418004006 },
			mode: 10
		},
		{
			timeSeconds: 4,
			position: { x: 4754.89893, y: 313.844391, z: 729.904358 },
			rotation: { x: -0.154038996, y: 5.60498714, z: 0.0710719973 },
			mode: 10
		},
		{
			timeSeconds: 5,
			position: { x: 4497.96582, y: 281.481995, z: 1009.14856 },
			rotation: { x: 0.195960999, y: 4.83498383, z: 0.527531981 },
			mode: 10
		},
		{
			timeSeconds: 6,
			position: { x: 3892.28491, y: 133.262436, z: 1054.60107 },
			rotation: { x: 0.0159610007, y: 4.77498293, z: -0.227724001 },
			mode: 10
		},
		{
			timeSeconds: 7,
			position: { x: 3245.52368, y: 116.632248, z: 1084.50061 },
			rotation: { x: 0.000961999991, y: 4.3799839, z: 0.0150680002 },
			mode: 10
		},
		{
			timeSeconds: 8,
			position: { x: 3038.24243, y: 100.669861, z: 670.116943 },
			rotation: { x: -0.0340379998, y: 5.21998215, z: -0.0587880015 },
			mode: 10
		},
		{
			timeSeconds: 9,
			position: { x: 2289.49219, y: 124.655785, z: 474.017609 },
			rotation: { x: -0.0790380016, y: 6.97498608, z: -0.0587880015 },
			mode: 10
		},
		{
			timeSeconds: 10,
			position: { x: 1994.69971, y: 274.240784, z: 962.891968 },
			rotation: { x: 0.180961996, y: 8.04498863, z: 0.0178720001 },
			mode: 10
		},
		{
			timeSeconds: 11,
			position: { x: 1839.13782, y: 488.471619, z: 1603.72937 },
			rotation: { x: 0.325962007, y: 8.63499355, z: 0.0178720001 },
			mode: 10
		},
		{
			timeSeconds: 12,
			position: { x: 1522.07251, y: 147.674103, z: 2158.02734 },
			rotation: { x: -0.159202993, y: 7.67497301, z: 0.0178720001 },
			mode: 10
		},
		{
			timeSeconds: 13,
			position: { x: 1035.66858, y: 126.855377, z: 2134.76587 },
			rotation: { x: -0.334203005, y: 5.89497519, z: 0.0178720001 },
			mode: 10
		}
	]
};
export async function buildTitleResources() {
	const optionSettings = await readClientOptionSettings();
	const introNames = listBuiltTitleIntroNames( optionSettings.IntroName );
	const manifests = [];

	for ( const introName of introNames ) {
		const titleScriptPath = resolveTitleIntroScriptPath( introName );
		const titleManifest = await buildCPSTitleWorldManifest( titleScriptPath, introName );
		await writeJson(
			path.join( publicRoot, "assets", "title", titleManifest.area, "manifest.json" ),
			titleManifest
		);
		manifests.push( titleManifest );
	}

	const primaryArea = deriveTitleAreaFromIntroName( optionSettings.IntroName );
	const primary = manifests.find( ( manifest ) => manifest.area === primaryArea ) ?? manifests[0];
	if ( !primary ) {
		throw new Error( "No title manifests were built" );
	}

	return {
		primary,
		manifests
	};
}
function listBuiltTitleIntroNames( activeIntroName ) {
	return uniqueStrings( [ activeIntroName, ...builtTitleIntroNames ].filter( Boolean ).map( normalizeAssetPath ) );
}

function titleSceneLabel( id ) {
	switch ( id ) {
		case "constantinople":
			return "Constantinople";
		case "china_wharf":
			return "China wharf";
		default:
			return id
				.split( /[_-]+/ )
				.filter( Boolean )
				.map( ( part ) => `${part.charAt( 0 ).toUpperCase()}${part.slice( 1 )}` )
				.join( " " );
	}
}
async function buildCPSTitleWorldManifest( titleScriptPath, introName ) {
	const raw = await readText( titleScriptPath );
	const sourceCamera = [];

	for ( const line of raw.split( /\r?\n/ ) ) {
		const parts = line.trim().split( /\s+/ );
		if ( parts[1] !== "S_CameraInsert" ) {
			continue;
		}

		sourceCamera.push( {
			timeSeconds: cleanNumber( parts[2] ),
			sourceTimeSeconds: cleanNumber( parts[2] ),
			sectorX: cleanNumber( parts[3] ),
			sectorY: cleanNumber( parts[4] ),
			position: {
				x: cleanNumber( parts[5] ),
				y: cleanNumber( parts[6] ),
				z: cleanNumber( parts[7] )
			},
			rotation: {
				x: cleanNumber( parts[8] ),
				y: cleanNumber( parts[9] ),
				z: cleanNumber( parts[10] )
			},
			mode: cleanNumber( parts[11] )
		} );
	}

	const area = deriveTitleAreaFromIntroName( introName );
	const camera = resolveVerifiedTitleCamera( area, sourceCamera );
	const firstCamera = camera[0];
	if ( !firstCamera ) {
		throw new Error( `No title camera keys found in ${titleScriptPath}` );
	}

	const sectorX = firstCamera.sectorX;
	const sectorY = firstCamera.sectorY;
	const sectorHex = `${sectorY.toString( 16 ).padStart( 2, "0" )}${sectorX.toString( 16 ).padStart( 2, "0" )}`;
	const mapBase = path.join( extractedRoot, "Map_extracted", String( sectorY ), String( sectorX ) );
	const navmeshName = `nv_${sectorHex}.nvm`;
	const navmeshPath = path.join( extractedRoot, "Data_extracted", "navmesh", navmeshName );

	await assertExists( `${mapBase}.m` );
	await assertExists( `${mapBase}.t` );
	await assertExists( `${mapBase}.o` );
	await assertExists( `${mapBase}.o2` );
	await assertExists( navmeshPath );

	return {
		sourcePath: toGameRelative( titleScriptPath ),
		introName: normalizeAssetPath( introName ),
		area,
		camera,
		cameraControllerTargetTimeSeconds: camera.at( -1 ).timeSeconds,
		mapSector: {
			sectorX,
			sectorY,
			mapFiles: {
				terrain: toGameRelative( `${mapBase}.m` ),
				texture: toGameRelative( `${mapBase}.t` ),
				objects: toGameRelative( `${mapBase}.o` ),
				objects2: toGameRelative( `${mapBase}.o2` )
			},
			navmesh: toGameRelative( navmeshPath )
		},
		regionBundlePublicPath: `/assets/world/${area}/region-${sectorHex}.json`,
		worldRegionsPublicPath: `/assets/world/${area}/world-regions-${sectorHex}.json`,
		seedRegionId: `0x${sectorHex}`,
		// Emitted later in the same build by buildWorldAnimatedObjects (hawk/fish/
		// boats/trees/scenery people GLB routing); the client loads it fail-soft.
		animatedObjectsPublicPath: `/assets/world/${area}/animated-objects.json`,
		seedResources: []
	};
}

function resolveVerifiedTitleCamera( area, sourceCamera ) {
	const verifiedCamera = verifiedNativeTitleCameraTables[area];
	if ( !verifiedCamera ) {
		return sourceCamera;
	}

	if ( verifiedCamera.length !== sourceCamera.length ) {
		throw new Error(
			`${area} native title camera table has ${verifiedCamera.length} rows, ` +
				`but script has ${sourceCamera.length} S_CameraInsert rows`
		);
	}

	return verifiedCamera.map( ( nativeKey, index ) => {
		const sourceKey = sourceCamera[index];

		return {
			timeSeconds: nativeKey.timeSeconds,
			sourceTimeSeconds: sourceKey.sourceTimeSeconds,
			sectorX: sourceKey.sectorX,
			sectorY: sourceKey.sectorY,
			position: nativeKey.position,
			rotation: nativeKey.rotation,
			mode: nativeKey.mode
		};
	} );
}

export function deriveTitleAreaFromIntroName( introName ) {
	if ( !introName ) {
		throw new Error( `${optionPath}: IntroName is required to build title resources` );
	}

	const normalized = normalizeAssetPath( introName );
	const fileName = normalized.split( "/" ).at( -1 );
	if ( !fileName || !fileName.endsWith( ".txt" ) ) {
		throw new Error( `${optionPath}: IntroName must point to an intro .txt script, got ${introName}` );
	}

	return fileName.slice( 0, -".txt".length );
}

function resolveTitleIntroScriptPath( introName ) {
	if ( !introName ) {
		throw new Error( `${optionPath}: IntroName is required to build title resources` );
	}

	const normalized = normalizeAssetPath( introName );
	const resolvedPath = path.resolve( mediaRoot, normalized );
	const mediaRootWithSeparator = `${mediaRoot}${path.sep}`;

	if ( resolvedPath !== mediaRoot && !resolvedPath.startsWith( mediaRootWithSeparator ) ) {
		throw new Error( `${optionPath}: IntroName resolves outside Media_extracted: ${introName}` );
	}

	return resolvedPath;
}
export { listBuiltTitleIntroNames, titleSceneLabel };
