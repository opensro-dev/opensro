// Split verbatim from resourcePipeline.mjs (2026-07-28): config cluster -
// option.txt settings, cameradata.txt and the raw command.txt copy.
import { copyIntoPublicTree } from "./publicWrite.mjs";
import { copyFile, mkdir } from "node:fs/promises";
import path from "node:path";

import {
	formatHexRegionId,
	mediaRoot,
	normalizeAssetPath,
	publicRoot,
	readText,
	toGameRelative,
	writeJson
} from "./resourceIo.mjs";
import { iterateTextDataLines } from "./textDataIo.mjs";
import { deriveTitleAreaFromIntroName, listBuiltTitleIntroNames, titleSceneLabel } from "./titleResources.mjs";
import { resolveMusicPublicPath } from "./audioResources.mjs";

const optionPath = path.join( mediaRoot, "config", "option.txt" );
const cameraDataPath = path.join( mediaRoot, "config", "cameradata.txt" );
const commandConfigPath = path.join( mediaRoot, "config", "command.txt" );
export async function buildConfigResources() {
	const optionConfig = await buildClientOptionConfig();
	const missionCameraData = await buildMissionCameraDataConfig();
	await writeJson( path.join( publicRoot, "assets", "config", "option.json" ), optionConfig );
	await writeJson( path.join( publicRoot, "assets", "config", "cameradata.json" ), missionCameraData );

	// Native sub_68d9c0 / CGInterface_LoadCommandBindings reads config\command.txt
	// through the GFXFileManager; the browser twin fetches the same bytes as a
	// public asset. Copied raw (the WIP twin performs the native token-stream
	// parse itself, comments and all).
	const commandConfigTarget = path.join( publicRoot, "assets", "config", "command.txt" );
	await copyIntoPublicTree( commandConfigPath, commandConfigTarget );

	return {
		optionConfig,
		missionCameraData
	};
}
async function buildClientOptionConfig() {
	const settings = await readClientOptionSettings();
	const introBgm = settings.IntroBGM;
	const titleScenes = Object.fromEntries(
		listBuiltTitleIntroNames( settings.IntroName ).map( ( introName ) => {
			const id = deriveTitleAreaFromIntroName( introName );
			return [
				id,
				{
					id,
					introName: normalizeAssetPath( introName ),
					manifestPublicPath: `/assets/title/${id}/manifest.json`,
					label: titleSceneLabel( id )
				}
			];
		} )
	);

	return {
		sourcePath: toGameRelative( optionPath ),
		settings,
		startProcess: settings.StartProcess,
		startCharacter: settings.StartCharacter,
		startWeapon: settings.StartWeapon,
		introName: settings.IntroName,
		introBgm,
		introBgmPublicPath: introBgm ? resolveMusicPublicPath( introBgm ) : undefined,
		titleScenes,
		nativeOptions: {
			// sub_5cb550 initializes the CIFOption_Game control at +0x20 to 1; sub_5cb180 copies
			// that value into sub_4b3890()+0xA, and sub_722e20 gates StartEuropIntroScript on it.
			europeIntroScript: true
		}
	};
}
async function buildMissionCameraDataConfig() {
	const raw = await readText( cameraDataPath );
	const rows = [];
	const sentinelRows = [];

	for ( const line of iterateTextDataLines( raw, { trim: true } ) ) {
		if ( line === "-1" ) {
			sentinelRows.push( line );
			continue;
		}

		// Strict single-tab split preserving empty cells: the old \s+ split
		// collapsed an empty field and shifted every later pose column. An
		// empty numeric cell parses as Number("") = 0 (the native atoi view).
		const tokens = line.split( "\t" ).map( Number );
		if ( tokens.length < 9 || tokens.some( ( value ) => !Number.isFinite( value ) ) ) {
			continue;
		}

		const [sectorX, sectorY, x, y, z, distanceGate, rotationX, rotationY, scalar] = tokens;
		const regionId = (((sectorY & 0xff) << 8) | (sectorX & 0xff)) >>> 0;
		rows.push( {
			regionId: formatHexRegionId( regionId ),
			sectorX,
			sectorY,
			position: { x, y, z },
			distanceGate,
			rotationDegrees: { x: rotationX, y: rotationY, z: 0 },
			scalar,
			sourceLine: line
		} );
	}

	return {
		format: "sro-mission-camera-data",
		version: 1,
		sourcePath: toGameRelative( cameraDataPath ),
		reconstructionSources: [
			"sub_7789a0_CPSMission_LoadCameraDataForRegion",
			"sub_878e20_RegionAwareDistance3D"
		],
		sentinelRows,
		rows
	};
}

async function readClientOptionSettings() {
	const raw = await readText( optionPath );
	const settings = {};

	for ( const line of iterateTextDataLines( raw, { trim: true } ) ) {
		const match = /^([A-Za-z0-9_]+)\s*=\s*"([^"]*)"/.exec( line );
		if ( match ) {
			settings[match[1]] = match[2];
		}
	}

	return settings;
}
export { optionPath, readClientOptionSettings };
