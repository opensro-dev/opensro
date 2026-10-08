// Split verbatim from resourcePipeline.mjs (2026-07-28): font cluster -
// packaged TTF repair, JMX glyph metrics and the native UI font atlas.
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";

import {
	assertExists,
	fontPublicRoot,
	fontSourceRoot,
	gameRoot,
	publicRoot,
	publishFileFromTemp,
	rebuildRoot,
	toGameRelative,
	writeJson
} from "./resourceIo.mjs";
import { runPython } from "./pythonRun.mjs";

const fontsToCopy = [
	{
		fontIndex: 0,
		role: "default",
		family: "SRO Default",
		sourceFile: "\uAE30\uBCF8\uC11C\uCCB4.ttf",
		publicFile: "sro-default.ttf"
	},
	{
		fontIndex: 1,
		role: "chat",
		family: "SRO Chat",
		sourceFile: "\uCC44\uD305\uC11C\uCCB4.ttf",
		publicFile: "sro-chat.ttf"
	},
	{
		fontIndex: 2,
		role: "english",
		family: "SRO English",
		sourceFile: "\uC601\uBB38\uC11C\uCCB4.ttf",
		publicFile: "sro-english.ttf"
	}
];
const nativeUiFontsByIndex = [
	{ fontIndex: 0, role: "ui-default", family: "Arial", nativeHeight: 9 },
	{ fontIndex: 1, role: "ui-small", family: "Arial", nativeHeight: 8 },
	{ fontIndex: 2, role: "ui-title", family: "Arial", nativeHeight: 12 },
	{ fontIndex: 3, role: "ui-medium", family: "Arial", nativeHeight: 11 },
	{ fontIndex: 4, role: "ui-large", family: "Arial", nativeHeight: 15 }
];
const nativeGdiPixelHeight = ( height ) => Math.round( (height * 0x60) / 0x48 );
const fontRepairScript = [
	"import sys",
	"from fontTools import subset",
	"from fontTools.ttLib import TTFont",
	"",
	"source, target = sys.argv[1], sys.argv[2]",
	"font = TTFont(source, recalcBBoxes=True, recalcTimestamp=False)",
	"",
	"bad_glyphs = set()",
	"for glyph_name in font.getGlyphOrder():",
	"    try:",
	"        _ = font['glyf'][glyph_name].numberOfContours",
	"    except Exception:",
	"        bad_glyphs.add(glyph_name)",
	"",
	"unicodes = set()",
	"for table in font['cmap'].tables:",
	"    for codepoint, glyph_name in table.cmap.items():",
	"        if glyph_name not in bad_glyphs:",
	"            unicodes.add(codepoint)",
	"",
	"options = subset.Options()",
	"options.drop_tables += ['DSIG']",
	"options.recalc_bounds = True",
	"options.recalc_timestamp = False",
	"subsetter = subset.Subsetter(options=options)",
	"subsetter.populate(unicodes=unicodes)",
	"subsetter.subset(font)",
	"",
	"font['name'].names = sorted(",
	"    font['name'].names,",
	"    key=lambda record: (record.platformID, record.platEncID, record.langID, record.nameID)",
	")",
	"font.save(target, reorderTables=True)"
].join( "\n" );
export async function buildFontResources() {
	const fontCatalog = await buildFontAssets();
	await writeJson( path.join( publicRoot, "assets", "fonts", "fonts.json" ), fontCatalog );
	return fontCatalog;
}
async function buildFontAssets() {
	await mkdir( fontPublicRoot, { recursive: true } );

	const glyphMetrics = {
		"0": await readJmxGlyphMetric( "0.dat" ),
		i: await readJmxGlyphMetric( "i.dat" ),
		y: await readJmxGlyphMetric( "y.dat" )
	};
	const defaultLineHeight = Math.max( ...Object.values( glyphMetrics ).map( ( metric ) => metric.height ) );
	const packagedFonts = {};

	for ( const font of fontsToCopy ) {
		const source = path.join( fontSourceRoot, font.sourceFile );
		const target = path.join( fontPublicRoot, font.publicFile );
		await assertExists( source );
		await repairFontForBrowser( source, target );

		packagedFonts[font.role] = {
			sourcePath: toGameRelative( source ),
			publicPath: `/assets/fonts/${font.publicFile}`,
			family: font.family,
			role: font.role,
			lineHeight: defaultLineHeight
		};
	}

	const fontsByIndex = {};
	for ( const font of nativeUiFontsByIndex ) {
		fontsByIndex[String( font.fontIndex )] = {
			sourcePath: "extracted/Media_extracted/server_dep/silkroad/textdata/textuisystem.txt:UIC_STT_FONTNAME",
			publicPath: "",
			family: font.family,
			role: font.role,
			lineHeight: nativeGdiPixelHeight( font.nativeHeight ),
			nativeHeight: font.nativeHeight
		};
	}
	await buildNativeUiFontAtlas();

	return {
		sourcePath: toGameRelative( fontSourceRoot ),
		defaultLineHeight,
		glyphMetrics,
		packagedFonts,
		fontsByIndex,
		nativeUiAtlas: {
			sourcePath:
				"SRO_Client.exe v1.150 native font path: sub_a122b0 -> sub_a17690 -> GetGlyphOutlineW(GGO_BITMAP)",
			jsonPublicPath: "/assets/fonts/native-ui-font-atlas.json",
			imagePublicPath: "/assets/fonts/native-ui-font-atlas.png",
			face: "Arial",
			availableStyleSlots: [ 0, 2 ]
		}
	};
}

async function buildNativeUiFontAtlas() {
	const scriptPath = path.join( rebuildRoot, "scripts", "build", "native_ui_font_atlas.py" );
	const atlasJsonPath = path.join( fontPublicRoot, "native-ui-font-atlas.json" );
	const atlasImagePath = path.join( fontPublicRoot, "native-ui-font-atlas.png" );
	// GDI renders into temp siblings, never into the served atlas files directly:
	// the same dev-server hold that turned fontTools' in-place ttf save into a
	// PermissionError (see repairFontForBrowser) applies to any served destination
	// a tool opens for write. publishFileFromTemp drops unchanged bytes without
	// touching the destination (atlas output is deterministic per machine) and
	// retry-renames the rest, falling back to an in-place write with a warning.
	const temporaryJsonPath = `${atlasJsonPath}.tmp`;
	const temporaryImagePath = `${atlasImagePath}.tmp`;

	// runPython owns the py -3 -> python fallback and its failure attribution
	// (see pythonRun.mjs); the publish runs once, AFTER the interpreter question
	// is settled, so a Node-side publish failure can never trigger a pointless
	// second Python run.
	await runPython( [ scriptPath, temporaryJsonPath, temporaryImagePath ], {
		task: "Build the native UI font atlas with Win32 GDI",
		context: [ "The atlas is part of the CTextBoard style-slot contract; do not reuse a stale normal-only atlas." ],
		cwd: gameRoot
	} );
	await publishFileFromTemp( temporaryJsonPath, atlasJsonPath );
	await publishFileFromTemp( temporaryImagePath, atlasImagePath );
}

async function readJmxGlyphMetric( fileName ) {
	const source = path.join( fontSourceRoot, fileName );
	const bytes = await readFile( source );
	const magic = bytes.subarray( 0, 12 ).toString( "ascii" );

	if ( magic !== "JMXVIMG11000" || bytes.length < 16 ) {
		throw new Error( `Unsupported JMX font metric file: ${source}` );
	}

	return {
		sourcePath: toGameRelative( source ),
		width: bytes.readUInt16LE( 12 ),
		height: bytes.readUInt16LE( 14 )
	};
}

async function repairFontForBrowser( source, target ) {
	// fontTools saves to a temp sibling, never to the served TTF directly: a dev
	// server holding the font (Windows readers without write/delete sharing)
	// made the previous in-place save die with PermissionError at the end of a
	// multi-minute build. publishFileFromTemp then skips unchanged bytes
	// (fontTools output is deterministic here: recalcTimestamp=False, sorted
	// name records, reorderTables) and retry-renames the rest.
	const temporaryTarget = `${target}.tmp`;

	// runPython owns the py -3 -> python fallback and its failure attribution
	// (see pythonRun.mjs): a PermissionError from a WORKING fontTools is an
	// operation failure and is reported as the held file it is, never as a
	// missing-fontTools environment problem. The publish runs once, after the
	// interpreter question is settled.
	await runPython( [ "-c", fontRepairScript, source, temporaryTarget ], {
		task: `Repair font for browser use: ${source}`,
		cwd: gameRoot
	} );
	await publishFileFromTemp( temporaryTarget, target );
}
