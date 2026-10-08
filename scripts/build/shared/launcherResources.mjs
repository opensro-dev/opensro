// Split verbatim from resourcePipeline.mjs (2026-07-28): launcher cluster -
// refresources/reflinkurl manifest, skin definitions and the PNG copy pass.
import { copyIntoPublicTree } from "./convertedImages.mjs";
import path from "node:path";

import {
	exists,
	extractedRoot,
	gameRoot,
	imagePublicRoot,
	imageSourceRoot,
	listFiles,
	publicRoot,
	readText,
	stripQuotes,
	toGameRelative,
	writeJson
} from "./resourceIo.mjs";

const launcherRefResourcesPath = path.join( extractedRoot, "Media_extracted", "launcher", "refresources.txt" );
const launcherRefLinkUrlPath = path.join( extractedRoot, "Media_extracted", "launcher", "reflinkurl.txt" );
const launcherSkinDefinitions = [
	{
		id: "global-1",
		kind: "global",
		backgroundPublicPath: "/assets/images/Media_extracted/launcher/bg_1.png",
		homepagePublicPath: "/assets/images/Media_extracted/launcher/homepage_1.png",
		divisionPanelPublicPath: "/assets/images/Media_extracted/launcher/bg_division_1.png"
	},
	{
		id: "global-2",
		kind: "global",
		backgroundPublicPath: "/assets/images/Media_extracted/launcher/bg_2.png",
		homepagePublicPath: "/assets/images/Media_extracted/launcher/homepage_2.png",
		divisionPanelPublicPath: "/assets/images/Media_extracted/launcher/bg_division_2.png"
	},
	{
		id: "global-3",
		kind: "global",
		backgroundPublicPath: "/assets/images/Media_extracted/launcher/bg_3.png",
		homepagePublicPath: "/assets/images/Media_extracted/launcher/homepage_3.png",
		divisionPanelPublicPath: "/assets/images/Media_extracted/launcher/bg_division_3.png"
	},
	{
		id: "europe-4",
		kind: "europe",
		backgroundPublicPath: "/assets/images/Media_extracted/launcher_europe/bg_4.png",
		homepagePublicPath: "/assets/images/Media_extracted/launcher_europe/homepage_4.png",
		divisionPanelPublicPath: "/assets/images/Media_extracted/launcher_europe/bg_division_4.png"
	},
	{
		id: "europe-5",
		kind: "europe",
		backgroundPublicPath: "/assets/images/Media_extracted/launcher_europe/bg_5.png",
		homepagePublicPath: "/assets/images/Media_extracted/launcher_europe/homepage_5.png",
		divisionPanelPublicPath: "/assets/images/Media_extracted/launcher_europe/bg_division_5.png"
	}
];
export async function buildLauncherResources( textCatalog ) {
	const launcherManifest = await buildLauncherManifest( textCatalog );
	await writeJson( path.join( publicRoot, "assets", "launcher", "manifest.json" ), launcherManifest );
	return launcherManifest;
}
async function buildLauncherManifest( textCatalog ) {
	const rectsById = {};
	const linksById = {};
	const resourcesRaw = await readText( launcherRefResourcesPath );
	const linksRaw = await readText( launcherRefLinkUrlPath );

	for ( const line of resourcesRaw.split( /\r?\n/ ) ) {
		const parsed = parseLauncherRecord( line, 5 );
		if ( !parsed ) {
			continue;
		}

		const [id, x, y, width, height] = parsed.values;
		rectsById[String( id )] = {
			id,
			x,
			y,
			width,
			height,
			name: parsed.comment
		};
	}

	for ( const line of linksRaw.split( /\r?\n/ ) ) {
		const parsed = parseLauncherRecord( line, 2 );
		if ( !parsed ) {
			continue;
		}

		const [id, url] = parsed.values;
		linksById[String( id )] = {
			id,
			url,
			name: parsed.comment
		};
	}

	return {
		sourcePath: toGameRelative( launcherRefResourcesPath ),
		linksSourcePath: toGameRelative( launcherRefLinkUrlPath ),
		skinRoot: "/assets/images/Media_extracted/launcher",
		europeSkinRoot: "/assets/images/Media_extracted/launcher_europe",
		skins: launcherSkinDefinitions,
		version: buildLauncherVersionInfo(),
		rectsById,
		linksById,
		font: {
			sourceKey: "UIC_STT_FONTNAME",
			family: textCatalog.entries.UIC_STT_FONTNAME ?? "Arial"
		}
	};
}

function buildLauncherVersionInfo() {
	const workspaceVersion = gameRoot.match( /v(\d+\.\d+)/i )?.[1];

	return {
		displayText: workspaceVersion ? `ver ${workspaceVersion}` : "ver ?",
		value: workspaceVersion,
		source: workspaceVersion ? "workspace-folder" : "unknown",
		formatStringSource: "Silkroad.exe:ver %.3f",
		encodedVersionSourcePath: "Media_extracted/SV.T"
	};
}

function parseLauncherRecord( line, minParts ) {
	const match = line.match( /\{([^}]+)\}/ );
	if ( !match ) {
		return undefined;
	}

	const commentStart = line.indexOf( "//", (match.index ?? 0) + match[0].length );
	const commentPart = commentStart === -1 ? "" : line.slice( commentStart + 2 );
	const parts = match[1].split( "," ).map( ( part ) => part.trim() ).filter( Boolean );
	if ( parts.length < minParts ) {
		return undefined;
	}

	return {
		values: parts.map( parseLauncherValue ),
		comment: commentPart.trim()
	};
}

function parseLauncherValue( value ) {
	if ( /^".*"$/.test( value ) ) {
		return stripQuotes( value );
	}

	if ( /^\d+(?:\s*-\s*\d+)*$/.test( value ) ) {
		return value.split( "-" ).map( ( part ) => Number( part.trim() ) ).reduce( ( left, right ) => left - right );
	}

	const number = Number( value );
	return Number.isFinite( number ) ? number : value;
}
export async function copyLauncherAssets() {
	let copied = 0;

	for ( const folderName of [ "launcher", "launcher_europe" ] ) {
		const sourceRoot = path.join( imageSourceRoot, "Media_extracted", folderName );
		if ( !(await exists( sourceRoot )) ) {
			continue;
		}

		const files = await listFiles( sourceRoot );
		for ( const source of files ) {
			if ( path.extname( source ).toLowerCase() !== ".png" ) {
				continue;
			}

			const relative = path.relative( sourceRoot, source );
			const target = path.join( imagePublicRoot, "Media_extracted", folderName, relative );
			await copyIntoPublicTree( source, target );
			copied += 1;
		}
	}

	return copied;
}
