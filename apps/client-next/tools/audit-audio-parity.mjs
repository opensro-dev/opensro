/*
===========================================================================

audit-audio-parity.mjs - census of which runtime code produces each sound

Classifies every row of the native sound table by the lane that can play it
(UI handle, native item-category selector, animation cue, literal call) and
records what is missing. It is a producer census, not a WAV-count parity
claim: native edges it cannot see stay open rather than being assumed.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";
import ts from "typescript";
import { build } from "esbuild";
import { itemSoundSource } from "./generate-item-sounds.mjs";
import { gameRoot } from "../../../scripts/build/world/paths.mjs";
export const root = path.resolve( import.meta.dirname, ".." );
const hash = b => createHash( "sha256" ).update( b ).digest( "hex" );
// resolve, not join: the asset paths are absolute (CLIENT_PUBLIC_ROOT).
const read = p => fs.readFileSync( path.resolve( root, p ) );
const json = p => JSON.parse( read( p ) );
const publicRoot = CLIENT_PUBLIC_ROOT;
const norm = s => s.replaceAll( "\\", "/" ).toUpperCase();

/*
================
sourceSoundCandidates

Parses executable syntax, never comments, documentation, test fixtures or
the old client/WIP lane. Even an executable literal is a candidate, not a
behavioural proof.
================
*/
export function sourceSoundCandidates( source, file ) {
	const tree = ts.createSourceFile( file, source, ts.ScriptTarget.Latest, true ), out = [];
	function visit( n ) {
		if ( ts.isStringLiteralLike( n ) && (/^(SND_|VOC_)/i.test( n.text ) || /\.(wav|mp3|ogg)$/i.test( n.text )) ) {
			let owner = n.parent;
			while ( owner && !ts.isCallExpression( owner ) && !ts.isSourceFile( owner ) ) owner = owner.parent;
			out.push( {
				value: norm( n.text ),
				file,
				line: tree.getLineAndCharacterOfPosition( n.getStart() ).line + 1,
				call: owner && ts.isCallExpression( owner ) ? owner.expression.getText( tree ) : null
			} );
		}
		ts.forEachChild( n, visit );
	}
	visit( tree );
	return out;
}
/*
================
files

Every .ts file under dir, recursively.
================
*/
function files( dir ) {
	return fs.readdirSync( dir, { withFileTypes: true } ).flatMap( e =>
		e.isDirectory() ?
			files( path.join( dir, e.name ) ) :
			e.name.endsWith( ".ts" ) ?
			[ path.join( dir, e.name ) ] :
			[]
	);
}
/*
================
load

Bundles one src/engine module and imports it, so the audit reads the
production catalogs rather than a copy of them.
================
*/
async function load( file ) {
	const b = await build( {
		entryPoints: [ path.join( root, "src/engine", file ) ],
		bundle: true,
		platform: "node",
		format: "esm",
		write: false
	} );
	return import( "data:text/javascript;base64," + Buffer.from( b.outputFiles[0].contents ).toString( "base64" ) );
}
/*
================
classifySoundRow

Assigns one native sound row to the lane that can produce it and lists
the reasons it is not yet proven reachable.
================
*/
export function classifySoundRow( row, { itemCategories, uiHandles, animationCues, sourceCandidates } ) {
	const object = norm( row.object ), handle = norm( row.handle ), reasons = [];
	let lane = "unclassified";
	if ( object === "UI" ) {
		lane = "ui";
		if ( !uiHandles.has( handle ) ) reasons.push( "missing-production-catalog" );
		if ( !sourceCandidates.some( c => c.call && c.value === handle && c.file.includes( "/runtime/" ) ) ) {
			reasons.push( "no-literal-production-producer" );
		}
	} else if ( object === "ITEM" && [ "SND_EQUIP", "SND_DROPITEM" ].includes( handle ) ) {
		lane = "item-category";
		if ( !itemCategories.has( row.event1 ) ) reasons.push( "unreachable-by-native-tid-selector" );
	} else if ( animationCues.has( handle ) ) lane = "animation-candidate";
	else if ( sourceCandidates.some( c => c.call && c.value === handle && c.file.includes( "/runtime/" ) ) ) {
		lane = "literal-candidate";
	} else reasons.push( "no-discovered-animation-or-literal-producer" );
	if ( !row.publicPath ) reasons.push( row.folder === "-" ? "native-placeholder" : "unresolved-asset" );
	return { lane, reasons };
}
/*
================
audit

Runs the census over the native sound table. With verify it also executes
the production audio closure tests and the native validator against
SRO_Client.exe, and records a regression if either fails.
================
*/
export async function audit(
	{
		verify = false,
		assetExists = p => fs.existsSync( path.join( publicRoot, p ) ),
		sourceRead = p => fs.readFileSync( p, "utf8" )
	} = {}
) {
	const native = json( "../../scripts/build/reference/native-audio-surface.json" ),
		oracle = json( "tests/fixtures/native/item-sound-selector-cases.json" );
	const catalogBytes = read( CLIENT_PUBLIC_ROOT + "/assets/audio/effectsound.json" ),
		catalog = JSON.parse( catalogBytes ),
		animation = json( CLIENT_PUBLIC_ROOT + "/assets/anim/manifest.json" );
	const { createUiSoundCatalog } = await load( "foundation/ui/sound-catalog.ts" );
	const { itemSoundCategory } = await load( "foundation/audio/item-sounds.ts" );
	const sourceCandidates = [], inputs = {}, issues = [];
	const ownership = json( "src/engine/ownership.json" );
	function owned( file ) {
		const seen = new Set();
		while ( file !== ownership.root ) {
			if ( seen.has( file ) || !ownership.modules[file] ) return false;
			seen.add( file );
			file = ownership.modules[file];
		}
		return true;
	}
	for ( const file of files( path.join( root, "src/engine" ) ) ) {
		const relative = path.relative( root, file ).replaceAll( "\\", "/" );
		if ( relative.includes( "/runtime/" ) && !owned( relative ) ) continue;
		const text = sourceRead( file );
		inputs[relative] = hash( text );
		sourceCandidates.push( ...sourceSoundCandidates( text, relative ) );
	}
	const itemCategories = new Set();
	for ( let tid = 0; tid < 65536; tid++ ) {
		const label = itemSoundCategory( tid );
		if ( label ) itemCategories.add( label );
	}
	const expected = new Map(
		Object.entries( oracle.categories ).flatMap( ( [k, ids] ) => ids.map( id => [ id, k ] ) )
	);
	for ( let tid = 0; tid < 65536; tid++ ) {
		if ( itemSoundCategory( tid ) !== (expected.get( tid ) ?? "") ) {
			issues.push( { kind: "native-selector-mismatch", tid } );
			break;
		}
	}
	if ( oracle.candidateSha256 !== inputs["src/engine/foundation/audio/item-sounds.ts"] ) {
		issues.push( { kind: "native-selector-evidence-stale" } );
	}
	if (
		sourceRead( path.join( root, "src/engine/foundation/audio/item-sound-catalog.ts" ) ) !==
			itemSoundSource( catalogBytes )
	) issues.push( { kind: "item-catalog-drift" } );
	const animationCues = new Set(), animationOccurrences = [];
	for ( const [model, clips] of Object.entries( animation.models ) ) {
		for ( const [clip, data] of Object.entries( clips ) ) {
			for ( const event of data.soundEvents ?? [] ) {
				const handle = norm( event.cue );
				animationCues.add( handle );
				animationOccurrences.push( { model, clip, handle, cursorMs: event.cursorMs } );
			}
		}
	}
	const uiHandles = new Set( Object.keys( createUiSoundCatalog() ) );
	const rows = catalog.rules.map( row => ({
		id: row.id,
		key: [ row.object, row.handle, row.skillId, row.event1, row.event2, row.event3 ].join( ":" ),
		path: row.publicPath ?? null,
		...classifySoundRow( row, { itemCategories, uiHandles, animationCues, sourceCandidates } )
	}) );
	const missingAssets = [];
	for ( const p of new Set( catalog.rules.map( r => r.publicPath ).filter( Boolean ) ) ) {
		if ( !assetExists( p ) ) missingAssets.push( p );
	}
	if ( missingAssets.length ) issues.push( { kind: "missing-published-assets", paths: missingAssets } );
	// Record each authored direct/script path, without promoting asset admission to
	// producer closure. Includes EFP sound programs, effect stages and environment/BGM.
	const resources = [];
	for (
		const file of [
			CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json",
			CLIENT_PUBLIC_ROOT + "/assets/effects/programs.json",
			CLIENT_PUBLIC_ROOT + "/assets/audio/effectenvsnd.json",
			CLIENT_PUBLIC_ROOT + "/assets/audio/skilleffectsound.json",
			CLIENT_PUBLIC_ROOT + "/assets/audio/catalog.json"
		]
	) {
		const bytes = read( file );
		inputs[file] = hash( bytes );
		function visit( value, location ) {
			if ( typeof value === "string" && /\.(wav|mp3|ogg)$/i.test( value ) ) {
				resources.push( { file, location, value } );
			} else if ( value && typeof value === "object" ) {
				for ( const [key, v] of Object.entries( value ) ) visit( v, location + "/" + key );
			}
		}
		visit( JSON.parse( bytes ), "" );
	}
	const nativeSites = native.strings.map( s => ({
		value: s.value,
		va: s.va,
		codeReferences: s.codeReferences,
		dataReferences: s.dataReferences,
		productionCandidates: sourceCandidates.filter( c =>
			c.value === norm( s.value ) || c.value.endsWith( "/" + norm( s.value ).split( "/" ).at( -1 ) )
		),
		status: "requires-callsite-and-branch-review"
	}) );
	const closedInventorySites = [
		"0x75754b",
		"0x7576e2",
		"0x7577c8",
		"0x75791c",
		"0x757f06",
		"0x7578f2",
		"0x759195",
		"0x758038"
	];
	const closed = new Set( closedInventorySites ),
		nativeCallsites = nativeSites.flatMap( s =>
			s.codeReferences.map( r => ({
				handle: s.value,
				...r,
				status: closed.has( r.va ) ? "declared-regression-not-run" : "unresolved-or-separately-evidenced"
			}) )
		);
	const pack = json( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json" ),
		packed = new Set( pack.assets.map( r => r.path ) );
	const directFiles = native.strings.filter( s => /^prim[\\/]snd[\\/].+\.wav$/i.test( s.value ) ).map( s => {
		const publicPath = "/assets/audio/sfx/" + s.value.replaceAll( "\\", "/" ).toLowerCase();
		return {
			native: s.value,
			literalVa: s.va,
			publicPath,
			published: assetExists( publicPath ),
			packed: packed.has( publicPath )
		};
	} );
	for ( const row of directFiles ) {
		if ( !row.published || !row.packed ) issues.push( { kind: "direct-sound-resource-gap", ...row } );
	}
	const brokenResourcePaths = [
		...new Set( resources.map( r => r.value ).filter( p => p.startsWith( "/assets/audio/" ) ) )
	].filter( p => !assetExists( p ) );
	if ( brokenResourcePaths.length ) {
		issues.push( { kind: "authored-resource-sound-gap", paths: brokenResourcePaths } );
	}
	// Closure tests execute the actual production modules. A manifest, source
	// literal, no-op callback or matching filename cannot turn this check green.
	let verification = { executed: false };
	if ( verify ) {
		const run = spawnSync( process.execPath, [
			"--test",
			"tests/runtime/item-sound-events.test.mjs",
			"tests/runtime/ui-sound-events.test.mjs",
			"tests/runtime/sound-presentation.test.mjs",
			"tests/runtime/character-animation-audio.test.mjs",
			"tests/runtime/environment-audio.test.mjs",
			"tests/runtime/runtime-failures.test.mjs",
			"tests/runtime/shop.test.mjs",
			"tests/architecture/audio-parity-audit.test.mjs"
		], { cwd: root, encoding: "utf8", timeout: 120000, windowsHide: true } );
		const nativeRun = spawnSync( process.env.SRO_PYTHON ?? (process.platform === "win32" ? "py.exe" : "python3"), [
			"tools/verify-native-audio.py",
			"--binary",
			path.join( gameRoot, "SRO_Client.exe" ),
			"--output",
			"temp/artifacts/audio-parity/native-validation.json"
		], { cwd: root, encoding: "utf8", timeout: 120000, windowsHide: true } );
		verification = {
			executed: true,
			exitCode: run.status,
			nativeExitCode: nativeRun.status,
			log: run.stdout + "\n" + run.stderr + "\n" + nativeRun.stdout + "\n" + nativeRun.stderr
		};
		if ( run.status !== 0 ) issues.push( { kind: "production-regression-failed" } );
		if ( nativeRun.status !== 0 ) {
			issues.push( { kind: "native-machine-validation-failed", error: nativeRun.error?.message } );
		}
		if ( run.status === 0 && nativeRun.status === 0 ) {
			for ( const row of nativeCallsites ) {
				if ( closed.has( row.va ) ) row.status = "production-regression-covered";
			}
		}
	}
	return {
		schema: "sro-audio-parity-audit-v1",
		qualification:
			"Exhaustive inventory of the supplied native reference export and shipped audio/animation/effect catalogs; not whole-program equivalence. Dynamic dispatch and unclassified call sites prevent parity acceptance.",
		binarySha256: native.binarySha256,
		inputs: {
			...inputs,
			catalog: hash( catalogBytes ),
			nativeSurface: hash( read( "../../scripts/build/reference/native-audio-surface.json" ) )
		},
		nativeSites,
		nativeCallsites,
		ownerReferences: native.ownerReferences,
		rows,
		animationOccurrences,
		resources,
		directFiles,
		sourceCandidates,
		verification,
		issues,
		open: {
			nativeCallsites: nativeCallsites.filter( s => s.status !== "production-regression-covered" ).length,
			managerReferencesRequireReview: native.ownerReferences.length,
			resourceProducerReview: resources.length,
			unclassifiedRules: rows.filter( r => r.reasons.length ).length
		},
		summary: {
			catalogRows: rows.length,
			nativeStrings: nativeSites.length,
			nativeCallsites: nativeCallsites.length,
			managerReferences: native.ownerReferences.length,
			itemCategories: itemCategories.size,
			animationOccurrences: animationOccurrences.length,
			resourceReferences: resources.length,
			issues: issues.length
		}
	};
}
if ( process.argv[1] && import.meta.url === pathToFileURL( process.argv[1] ).href ) {
	const report = await audit( { verify: process.argv.includes( "--verify" ) } );
	const dir = path.join( root, "temp/artifacts/audio-parity" );
	fs.mkdirSync( dir, { recursive: true } );
	fs.writeFileSync( path.join( dir, "report.json" ), JSON.stringify( report, null, 2 ) + "\n" );
	if ( report.verification.executed ) fs.writeFileSync( path.join( dir, "tests.log" ), report.verification.log );
	console.log(
		JSON.stringify(
			{
				summary: report.summary,
				open: report.open,
				issues: report.issues,
				report: path.join( dir, "report.json" )
			},
			null,
			2
		)
	);
	if ( report.issues.length || process.argv.includes( "--strict" ) && Object.values( report.open ).some( Boolean ) ) {
		process.exitCode = 1;
	}
}
