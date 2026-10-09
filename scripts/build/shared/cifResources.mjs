/*
===========================================================================

cifResources.mjs - the CIF/UI cluster of the resource build

Resinfo layout bundles, the runtime image allowlists, the sprite catalog
and the image copy pass that publishes every UI image they reference.
Split verbatim from resourcePipeline.mjs (2026-07-28).

===========================================================================
*/
// Split verbatim from resourcePipeline.mjs (2026-07-28): CIF/UI cluster -
// resinfo layout bundles, runtime image allowlists and the image copy pass.
import { mkdir, readFile } from "node:fs/promises";
import { copyIntoPublicTree } from "./publicWrite.mjs";
import path from "node:path";
import { toPublicImagePath } from "./assetPaths.mjs";

import {
	exists,
	imagePublicRoot,
	imageSourceRoot,
	normalizeAssetPath,
	publicRoot,
	readText,
	resinfoDir,
	textDataDir,
	toGameRelative,
	writeJson
} from "./resourceIo.mjs";
import { discoverCifLayouts } from "./cifLayoutCatalog.mjs";
import { applyCifPreprocessor, loadCifDefines } from "./cifPreprocessor.mjs";
import { runtimeCifButtonImageReferences, runtimeCifImageReferences } from "./cifRuntimeImageCatalog.mjs";
import { listTextDataShardNames } from "./textDataIo.mjs";
import {
	collectSkillGroupIconDdjReferencesFromRows,
	collectSkillMasteryIconDdjReferencesFromRows
} from "./skillPaneImageReferences.mjs";
import { collectWorldMapImageReferences } from "./worldMapImageReferences.mjs";
import { collectGuideImageReferences } from "./guideImageReferences.mjs";
import { mapWithConcurrency } from "./asyncUtils.mjs";

// Icon copies in flight at once; each is a stat, a compare and rarely a copy.
const ICON_COPY_CONCURRENCY = 32;

/*
================
buildCifResources
================
*/
export async function buildCifResources() {
	const copiedImages = new Set();
	const spriteCatalog = { resourcesByDdjPath: {} };
	const layoutOutputDir = path.join( publicRoot, "assets", "cif", "layouts" );
	const layoutsToBuild = await discoverCifLayouts( resinfoDir );
	const cifDefines = await loadCifDefines();
	await mkdir( layoutOutputDir, { recursive: true } );

	for ( const fileName of layoutsToBuild ) {
		const layout = await buildLayoutBundle( path.join( resinfoDir, fileName ), copiedImages, cifDefines );
		await writeJson( path.join( layoutOutputDir, `${path.basename( fileName, ".txt" )}.json` ), layout );
		Object.assign( spriteCatalog.resourcesByDdjPath, layout.resourcesByDdjPath );
	}

	for ( const ddjPath of runtimeCifImageReferences ) {
		await copyImageReference( ddjPath, copiedImages );
		await registerSpriteResource( spriteCatalog, ddjPath );
	}

	for ( const ddjPath of runtimeCifButtonImageReferences ) {
		await copyImageReference( ddjPath, copiedImages );
		await copyButtonStateImages( ddjPath, copiedImages );
		for ( const statePath of buttonStateDdjPaths( ddjPath ) ) {
			await registerSpriteResource( spriteCatalog, statePath );
		}
	}

	await buildWorldMapImageResources( { copiedImages, spriteCatalog } );
	await buildGuideImageResources( { copiedImages, spriteCatalog } );

	const skillPaneImageReferences = new Set( [
		...(await collectSkillMasteryIconDdjReferences()),
		...(await collectSkillGroupIconDdjReferences())
	] );
	for ( const ddjPath of [ ...skillPaneImageReferences ].sort() ) {
		await copyItemDataIconImage( ddjPath, copiedImages );
		await registerSpriteResource( spriteCatalog, ddjPath );
	}

	await writeJson(
		path.join( publicRoot, "assets", "cif", "cif-sprite-catalog.json" ),
		spriteCatalog
	);

	for (
		const collect of [
			collectItemDataIconDdjReferences,
			collectActionWndDataIconDdjReferences,
			collectSkillDataIconDdjReferences,
			collectCosCharacterIconDdjReferences
		]
	) {
		await copyItemDataIconImages( await collect(), copiedImages );
	}

	return {
		layoutsBuilt: layoutsToBuild.length,
		copiedImages
	};
}

/*
================
buildWorldMapImageResources

Publish the complete data-driven/tiled world-map image closure. Unlike the
broad historical CIF allowlists, every dependency here is required: a
missing converted source is a broken resource build, not an optional skin.
================
*/
/**
 * @param {{ copiedImages?: Set<string>, spriteCatalog?: { resourcesByDdjPath: Record<string, unknown> } }} [options]
 */
export async function buildWorldMapImageResources( {
	copiedImages = new Set(),
	spriteCatalog
} = {} ) {
	const closure = await collectWorldMapImageReferences();
	for ( const ddjPath of closure.references ) {
		await copyRequiredImageReference( ddjPath, copiedImages );
		if ( spriteCatalog ) await registerSpriteResource( spriteCatalog, ddjPath );
	}
	return { ...closure, copiedImages };
}

/*
================
buildGuideImageResources
================
*/
/**
 * @param {{ copiedImages?: Set<string>, spriteCatalog?: { resourcesByDdjPath: Record<string, unknown> } }} [options]
 */
export async function buildGuideImageResources( { copiedImages = new Set(), spriteCatalog } = {} ) {
	// 6668f0 swaps the collapsed handle; the index opens categories dynamically.
	// These images are absent from static resinfo and inline PML references.
	const runtimeReferences = [
		...[ "", "_focus", "_press", "_disable" ].map( s => `interface/ifcommon/com_side02_button${s}.ddj` ),
		...[ "", "_focus", "_press" ].map( s => `interface/guide/gd_index_button_open${s}.ddj` ),
		"interface/guide/gd_contents_lamp.ddj",
		"interface/guide/gd_index.ddj",
		"interface/guide/gd_contents.ddj",
		"interface/guide/gd_contents_event.ddj"
	];
	const references = [
		...new Set( [
			...collectGuideImageReferences( await readText( path.join( textDataDir, "texthelp.txt" ) ) ),
			...runtimeReferences
		] )
	].sort();
	for ( const ddjPath of references ) {
		await copyRequiredImageReference( ddjPath, copiedImages );
		if ( spriteCatalog ) await registerSpriteResource( spriteCatalog, ddjPath );
	}
	return { references, copiedImages };
}

/*
================
registerSpriteResource
================
*/
export async function registerSpriteResource( catalog, ddjPath ) {
	const resource = await describeSpriteResource( ddjPath );
	if ( resource ) {
		catalog.resourcesByDdjPath[normalizeAssetPath( ddjPath )] = resource;
	}
}

/*
================
buttonStateDdjPaths
================
*/
function buttonStateDdjPaths( ddjPath ) {
	const normalized = normalizeAssetPath( ddjPath );
	const withoutExt = normalized.replace( /\.[^.]+$/, "" );
	return [
		normalized,
		`${withoutExt}_focus.ddj`,
		`${withoutExt}_press.ddj`,
		`${withoutExt}_disable.ddj`
	];
}

/*
================
collectItemDataIconDdjReferences

Wire-item icons, data-driven: every icon the extracted itemdata rows can
reference - the FIRST .ddj column per row (referenceData.mjs reads the
same field), rooted under the native `icon\` base exactly like the wire
snapshot emitters (`icon\${row.icon}`). The native client resolves any of
these through the PK2 at draw time, so the browser build publishes them
all instead of the hand-carried starter subset in
runtimeCifImageReferences; referenced icons whose DDJ never shipped in
this Media.pk2 stay absent and ride the native icon_default fallback
(CIFSlotWithHelp_SetSpritePathWithFallback sub_55b450), same as retail.
================
*/
async function collectItemDataIconDdjReferences() {
	const iconPaths = new Set();
	for ( const fileName of await listTextDataShardNames( textDataDir, /^itemdata.*\.txt$/i ) ) {
		const raw = await readText( path.join( textDataDir, fileName ) );
		for ( const line of raw.split( /\r?\n/ ) ) {
			if ( !line || line.startsWith( "//" ) ) {
				continue;
			}
			const icon = line.split( "\t" ).find( ( field ) => /\.ddj$/i.test( field.trim() ) );
			if ( icon ) {
				iconPaths.add( `icon/${normalizeAssetPath( icon.trim() )}` );
			}
		}
	}
	return [ ...iconPaths ].sort();
}

/*
================
collectActionWndDataIconDdjReferences

CIFAction record icons, data-driven: the icon .ddj column of every
actionwnddata.txt record (the 0xcec870 action-data table the REAL
CIFAction_OnCreate sub_58b720 folds over). Natively these are runtime
references the record loader resolves through the PK2 at
SetupActionButton time, so the resinfo copy pass never sees them
(ifaction.txt authors every slot with DDJ="") - publish them from the
same shipped table the bridge record seed consumes. The commented-out
record rows (// prefix, e.g. 1005 autotarget) are skipped exactly like
the native parser skips them.
================
*/
async function collectActionWndDataIconDdjReferences() {
	const iconPaths = new Set();
	const raw = await readText( path.join( textDataDir, "actionwnddata.txt" ) );
	for ( const line of raw.split( /\r?\n/ ) ) {
		if ( !line.trim() || line.trimStart().startsWith( "//" ) ) {
			continue;
		}
		const icon = line.split( "\t" ).find( ( field ) => /\.ddj$/i.test( field.trim() ) );
		if ( icon ) {
			// Unlike itemdata rows, the actionwnddata icon column already carries
			// the native icon\ root (icon\action\...).
			iconPaths.add( normalizeAssetPath( icon.trim() ) );
		}
	}
	return [ ...iconPaths ].sort();
}

/*
================
collectSkillDataIconDdjReferences

CIFSkillSlot icons, data-driven: the UI icon-path column (source column
61 -> CSkillData GetData()+0x108, the exact column buildSkillDataAsset.mjs
ships) of every skilldata_*.txt record. Natively the slot bind sub_589050
(@0x005891ca) assigns that path onto the slot's icon child and the sprite
loader resolves it through the PK2 under the icon\ root, lowercased by the
parse (sub_811890 flag 1 @0x007f97e2) - publish every icon the shipped
rows can reference instead of a hand-carried subset. Icons whose DDJ never
shipped in this Media.pk2 (four europe *_base.ddj) stay absent and ride
the native icon_default fallback, same as retail.

(Plain block comment, not JSDoc: the checkJs parser reads the @0x...
native offsets above as malformed JSDoc tags - TS1003.)
================
*/
async function collectSkillDataIconDdjReferences() {
	const iconPaths = new Set();
	for ( const fileName of await listTextDataShardNames( textDataDir, /^skilldata_.*\.txt$/i ) ) {
		const raw = await readText( path.join( textDataDir, fileName ) );
		for ( const line of raw.split( /\r?\n/ ) ) {
			if ( !line || line.startsWith( "//" ) ) {
				continue;
			}
			const icon = line.split( "\t" )[61]?.trim().toLowerCase();
			if ( icon && icon.endsWith( ".ddj" ) ) {
				iconPaths.add( `icon/${normalizeAssetPath( icon )}` );
			}
		}
	}
	return [ ...iconPaths ].sort();
}

/*
================
collectCosCharacterIconDdjReferences

COS character icons, data-driven: the RefObjCommon icon column (source
column 54 -> +0x154) of every COS reference (TypeID 1/2/3, TID4 band 1..6).
CIFCOSStatus_BindCompanion (6AA290) puts it on the status icon slot (id 11)
and CIFPetMiniInfo (6B3AD0) on its picture (id 0x14); the sprite loader
resolves it under the icon\ root at bind time, so the resinfo pass never
sees it.
================
*/
async function collectCosCharacterIconDdjReferences() {
	const COLUMN_ICON = 54;
	const iconPaths = new Set();
	for ( const fileName of await listTextDataShardNames( textDataDir, /^characterdata.*\.txt$/i ) ) {
		const raw = await readText( path.join( textDataDir, fileName ) );
		for ( const line of raw.split( /\r?\n/ ) ) {
			if ( !line || line.startsWith( "//" ) ) {
				continue;
			}
			const cols = line.split( "\t" );
			const band = Number( cols[12] );
			if ( cols[0] !== "1" || cols[9] !== "1" || cols[10] !== "2" || cols[11] !== "3" || band < 1 || band > 6 ) {
				continue;
			}
			const icon = cols[COLUMN_ICON]?.trim().toLowerCase();
			if ( icon && icon.endsWith( ".ddj" ) ) {
				iconPaths.add( `icon/${normalizeAssetPath( icon )}` );
			}
		}
	}
	return [ ...iconPaths ].sort();
}

/*
================
collectSkillMasteryIconDdjReferences
================
*/
async function collectSkillMasteryIconDdjReferences() {
	const raw = await readText( path.join( textDataDir, "skillmasterydata.txt" ) );
	return collectSkillMasteryIconDdjReferencesFromRows( raw.split( /\r?\n/ ) );
}

/*
================
collectSkillGroupIconDdjReferences
================
*/
async function collectSkillGroupIconDdjReferences() {
	const raw = await readText( path.join( textDataDir, "skillgroup.txt" ) );
	return collectSkillGroupIconDdjReferencesFromRows( raw.split( /\r?\n/ ) );
}

/*
================
copyItemDataIconImage
================
*/
async function copyItemDataIconImage( ddjPath, copiedImages ) {
	await copyImageReference( ddjPath, copiedImages );
	const publicPath = imagePublicPath( ddjPath );
	if ( copiedImages.has( publicPath ) ) {
		return;
	}
	// convert_images.py output-name collision (a sibling .tga shares the PNG
	// stem, e.g. icon/item/etc/qsp_all_potion_1_01.{ddj,tga}): the DDJ's PNG
	// is written as <stem>.ddj.png; publish it under the plain .png name the
	// icon-column public-path contract expects.
	const relativePublic = publicPath.replace( /^\/assets\/images\//, "" );
	const collisionSource = path.join( imageSourceRoot, relativePublic.replace( /\.png$/i, ".ddj.png" ) );
	if ( !(await exists( collisionSource )) ) {
		return;
	}
	await copyIntoPublicTree( collisionSource, path.join( imagePublicRoot, relativePublic ) );
	copiedImages.add( publicPath );
}

/*
================
copyItemDataIconImages

copyItemDataIconImage over a list, ICON_COPY_CONCURRENCY at a time: one
await per icon in sequence made this the slowest step of a warm build under
load. Each public path is copied once and recorded in list order, so the
result does not depend on which copy finishes first.
================
*/
async function copyItemDataIconImages( ddjPaths, copiedImages ) {
	const pending = new Map();
	for ( const ddjPath of ddjPaths ) {
		const publicPath = imagePublicPath( ddjPath );
		if ( !copiedImages.has( publicPath ) && !pending.has( publicPath ) ) pending.set( publicPath, ddjPath );
	}
	const copied = new Set();
	await mapWithConcurrency(
		[ ...pending.values() ],
		ICON_COPY_CONCURRENCY,
		ddjPath => copyItemDataIconImage( ddjPath, copied )
	);
	for ( const publicPath of pending.keys() ) {
		if ( copied.has( publicPath ) ) copiedImages.add( publicPath );
	}
}

/*
================
buildLayoutBundle
================
*/
async function buildLayoutBundle( sourcePath, copiedImages, cifDefines ) {
	const raw = await readText( sourcePath );
	const text = applyCifPreprocessor( raw, cifDefines );
	const layout = parseCifLayout( text, sourcePath );
	const spritePaths = new Set();

	for ( const section of layout.sections ) {
		for ( const node of section.nodes ) {
			if ( !node.ddj ) {
				continue;
			}

			for ( const ddjPath of expandAuthoredDdjPaths( node ) ) {
				spritePaths.add( ddjPath );
				await copyImageReference( ddjPath, copiedImages );
			}
		}
	}

	layout.resourcesByDdjPath = {};
	for ( const ddjPath of spritePaths ) {
		const resource = await describeSpriteResource( ddjPath );
		if ( resource ) {
			layout.resourcesByDdjPath[normalizeAssetPath( ddjPath )] = resource;
		}
	}
	for ( const section of layout.sections ) {
		for ( const node of section.nodes ) {
			const resource = node.ddj ?
				layout.resourcesByDdjPath[normalizeAssetPath( node.ddj.sourcePath )] :
				undefined;
			if ( resource ) {
				node.ddj = { ...resource };
			}
		}
	}

	return layout;
}

/*
================
expandAuthoredDdjPaths

The polymorphic vt+0x34 DDJ/content landing:
- complete .ddj paths load that sprite;
- CIFButton additionally derives its three visual-state siblings;
- prefix paths reach the shared CIFFrame suffix expansion. Missing suffix
  files remain absent from the catalog, matching the native null-handle
  gate instead of manufacturing a resource.
================
*/
function expandAuthoredDdjPaths( node ) {
	const sourcePath = normalizeAssetPath( node.ddj.sourcePath );
	if ( /\.ddj$/i.test( sourcePath ) ) {
		if ( node.type !== "CIFButton" ) {
			return [ sourcePath ];
		}
		const stem = sourcePath.replace( /\.ddj$/i, "" );
		return [
			sourcePath,
			`${stem}_focus.ddj`,
			`${stem}_press.ddj`,
			`${stem}_disable.ddj`
		];
	}

	return [
		"left_up.ddj",
		"right_up.ddj",
		"right_down.ddj",
		"left_down.ddj",
		"left_side.ddj",
		"mid_up.ddj",
		"right_side.ddj",
		"mid_down.ddj"
	].map( ( suffix ) => `${sourcePath}${suffix}` );
}

/*
================
describeSpriteResource
================
*/
async function describeSpriteResource( ddjPath ) {
	const publicPath = imagePublicPath( ddjPath );
	const relativePublic = publicPath.replace( /^\/assets\/images\//, "" );
	const source = path.join( imageSourceRoot, relativePublic );

	if ( !(await exists( source )) ) {
		return null;
	}
	const bytes = await readFile( source );
	if (
		bytes.length < 24 ||
		bytes[0] !== 0x89 ||
		bytes.subarray( 1, 4 ).toString( "ascii" ) !== "PNG"
	) {
		throw new Error( `CIF sprite is not a PNG: ${source}` );
	}
	return {
		sourcePath: normalizeAssetPath( ddjPath ),
		publicPath,
		width: bytes.readUInt32BE( 16 ),
		height: bytes.readUInt32BE( 20 )
	};
}

// Directive lines and inactive branches become EMPTY lines instead of being
// removed: the parser skips blank lines anyway, and preserving the line count
// lets parseCifLayout report diagnostics against the original file's line
// numbers.

/*
================
parseCifLayout

  name: string,
  type: string,
  properties: Record<string, CifLayoutProperty>,
  id?: number,
  rect?: CifLayoutRect,
  clientRect?: CifLayoutRect,
  style?: number,
  text?: string,
  ddj?: { sourcePath: string, publicPath: string },
  fontColor?: CifLayoutColor,
  color?: CifLayoutColor,
  fontIndex?: number,
  hAlign?: number,
  vAlign?: number,
  subSection?: string
}} CifLayoutNode
================
*/
/**
 * @typedef {{ x: number, y: number, width: number, height: number }} CifLayoutRect
 * @typedef {{ a: number, r: number, g: number, b: number }} CifLayoutColor
 * @typedef {{ kind: string, raw: string, value: any }} CifLayoutProperty
 * @typedef {{
 * @typedef {{ name: string, rect: CifLayoutRect, style: number, nodes: CifLayoutNode[], rawLines: string[] }} CifLayoutSection
 */
function parseCifLayout( text, sourcePath ) {
	const sections = [];
	const lines = text.split( /\r?\n/ );
	/** @type {CifLayoutSection | null} */
	let currentSection = null;
	/** @type {CifLayoutNode | null} */
	let currentNode = null;

	for ( let lineIndex = 0; lineIndex < lines.length; lineIndex += 1 ) {
		const line = lines[lineIndex].trim();
		if ( !line || line === "Interface Text" || line === "{" ) {
			continue;
		}

		const sectionMatch = /^Section\s*=\s*([^,]+),\s*"([^"]*)",\s*"([^"]*)"/.exec( line );
		if ( sectionMatch ) {
			currentSection = {
				name: sectionMatch[1].trim(),
				rect: parseRect( sectionMatch[2] ),
				style: Number( sectionMatch[3] ) || 0,
				nodes: [],
				rawLines: [ line ]
			};
			sections.push( currentSection );
			currentNode = null;
			continue;
		}

		if ( currentSection ) {
			currentSection.rawLines.push( line );
		}

		const nodeMatch = /^([A-Za-z0-9_]+):([A-Za-z0-9_]+)/.exec( line );
		if ( nodeMatch && currentSection ) {
			currentNode = {
				name: nodeMatch[1],
				type: nodeMatch[2],
				properties: {}
			};
			currentSection.nodes.push( currentNode );
			continue;
		}

		if ( line === "}" ) {
			if ( currentNode ) {
				currentNode = null;
			} else {
				currentSection = null;
			}
			continue;
		}

		let propertyMatch = /^([A-Za-z0-9_]+)=([A-Z_]+),"([^"]*)"/.exec( line );
		if ( !propertyMatch ) {
			// Native tolerance for an UNTERMINATED quote: the property walk
			// (sub_9bb6a0 @ 0x9bb6a0) allocates the property from the `Key=TYPE,`
			// prefix FIRST, then the shared quote scanner (sub_9b64b0 @ 0x9b64b0)
			// fills the value - on a missing closing quote it collects characters
			// to end-of-line/NUL and STILL assigns the collected buffer. Shipped
			// data holds exactly one such line (ifoption_audio.txt:246,
			// `Text=STRING,"` -> empty string); mirroring the scanner keeps retail
			// parity instead of dropping a property native keeps.
			propertyMatch = /^([A-Za-z0-9_]+)=([A-Z_]+),"(.*)$/.exec( line );
		}
		if ( !propertyMatch || !currentNode ) {
			// A property-shaped line even the tolerant parse rejects (no opening
			// quote at all) would otherwise vanish with no trace. Warn-and-continue
			// rather than fail: the diagnostic is a pure tripwire - expected count
			// on today's shipped data is ZERO, so any hit is a NEW malformed shape
			// worth a look. applyPreprocessor preserves line positions, so
			// lineIndex + 1 is the line number in the original file.
			if ( !propertyMatch && /^[A-Za-z0-9_]+=/.test( line ) ) {
				console.warn(
					`[cif-layout] ${toGameRelative( sourcePath )}:${
						lineIndex + 1
					} dropped property-shaped line even the tolerant parser rejects: ${line}`
				);
			}
			continue;
		}

		const [, key, rawKind, rawValue] = propertyMatch;
		const property = parseProperty( rawKind, rawValue );
		currentNode.properties[key] = property;
		assignKnownNodeProperty( currentNode, key, property );
	}

	const controlsByName = {};
	const controlsById = {};
	for ( const section of sections ) {
		for ( const node of section.nodes ) {
			controlsByName[node.name] = node;
			if ( typeof node.id === "number" ) {
				controlsById[String( node.id )] = node;
			}
		}
	}

	return {
		sourcePath: toGameRelative( sourcePath ),
		variant: "global",
		virtualSize: { x: 0, y: 0, width: 1600, height: 1200 },
		sections,
		controlsByName,
		controlsById,
		resourcesByDdjPath: {}
	};
}

/*
================
parseProperty
================
*/
function parseProperty( rawKind, rawValue ) {
	switch ( rawKind ) {
		case "RECT":
			return { kind: "rect", raw: rawValue, value: parseRect( rawValue ) };
		case "POINT":
			return { kind: "point", raw: rawValue, value: parsePoint( rawValue ) };
		case "COLOR":
			return { kind: "color", raw: rawValue, value: parseColor( rawValue ) };
		case "INTEGER":
			return { kind: "integer", raw: rawValue, value: Number( rawValue ) || 0 };
		case "STRING":
			return { kind: "string", raw: rawValue, value: rawValue };
		default:
			return { kind: "raw", raw: rawValue, value: rawValue };
	}
}

/*
================
assignKnownNodeProperty
================
*/
function assignKnownNodeProperty( node, key, property ) {
	const value = property.value;

	if ( key === "ID" && typeof value === "number" ) {
		node.id = value;
	} else if ( key === "Rect" && typeof value === "object" ) {
		node.rect = value;
	} else if ( key === "ClientRect" && typeof value === "object" ) {
		node.clientRect = value;
	} else if ( key === "Style" && typeof value === "number" ) {
		node.style = value;
	} else if ( key === "Text" && typeof value === "string" ) {
		node.text = value;
	} else if ( key === "DDJ" && typeof value === "string" && value.length > 0 ) {
		node.ddj = {
			sourcePath: normalizeAssetPath( value ),
			publicPath: imagePublicPath( value )
		};
	} else if ( key === "FontColor" && typeof value === "object" ) {
		node.fontColor = value;
	} else if ( key === "Color" && typeof value === "object" ) {
		node.color = value;
	} else if ( key === "FontIndex" && typeof value === "number" ) {
		node.fontIndex = value;
	} else if ( key === "HAlign" && typeof value === "number" ) {
		node.hAlign = value;
	} else if ( key === "VAlign" && typeof value === "number" ) {
		node.vAlign = value;
	} else if ( key === "SubSection" && typeof value === "string" ) {
		node.subSection = value;
	}
}

/*
================
parseRect
================
*/
function parseRect( raw ) {
	const [x = 0, y = 0, width = 0, height = 0] = parseNumberList( raw );
	return { x, y, width, height };
}

/*
================
parsePoint
================
*/
function parsePoint( raw ) {
	const [x = 0, y = 0] = parseNumberList( raw );
	return { x, y };
}

/*
================
parseColor
================
*/
function parseColor( raw ) {
	const [a = 255, r = 255, g = 255, b = 255] = parseNumberList( raw );
	return { a, r, g, b };
}

/*
================
parseNumberList
================
*/
function parseNumberList( raw ) {
	return raw.split( "," ).map( ( part ) => Number( part.trim().replace( /f$/i, "" ) ) || 0 );
}

/*
================
copyImageReference
================
*/
async function copyImageReference( ddjPath, copiedImages ) {
	const publicPath = imagePublicPath( ddjPath );
	const relativePublic = publicPath.replace( /^\/assets\/images\//, "" );
	const source = path.join( imageSourceRoot, relativePublic );
	const target = path.join( imagePublicRoot, relativePublic );

	if ( copiedImages.has( publicPath ) || !(await exists( source )) ) {
		return;
	}

	await copyIntoPublicTree( source, target );
	copiedImages.add( publicPath );
}

/*
================
copyRequiredImageReference
================
*/
async function copyRequiredImageReference( ddjPath, copiedImages ) {
	const publicPath = imagePublicPath( ddjPath );
	const relativePublic = publicPath.replace( /^\/assets\/images\//, "" );
	const source = path.join( imageSourceRoot, relativePublic );
	if ( !(await exists( source )) ) {
		throw new Error( `Required UI image was not converted: ${ddjPath} (expected ${source}).` );
	}
	if ( copiedImages.has( publicPath ) ) return;

	const target = path.join( imagePublicRoot, relativePublic );
	await copyIntoPublicTree( source, target );
	copiedImages.add( publicPath );
}

/*
================
copyButtonStateImages
================
*/
async function copyButtonStateImages( ddjPath, copiedImages ) {
	for ( const statePath of buttonStateDdjPaths( ddjPath ).slice( 1 ) ) {
		await copyImageReference( statePath, copiedImages );
	}
}

/*
================
imagePublicPath
================
*/
function imagePublicPath( ddjPath ) {
	return toPublicImagePath( "Media_extracted", ddjPath );
}
const applyPreprocessor = applyCifPreprocessor;

export { applyPreprocessor, imagePublicPath, parseCifLayout };
