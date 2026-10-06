import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";
import { build } from "esbuild";
import { parseCharacterBsr } from "../../../scripts/build/char/formats.mjs";
import { dataAssetPath } from "../../../scripts/build/shared/jmxAssetIO.mjs";
import { readPublishedAssetBytesSync, readPackedAssetBytesSync } from "../../../scripts/lib/publishedAsset.mjs";
const root = path.resolve( import.meta.dirname, ".." ), publicRoot = CLIENT_PUBLIC_ROOT;
const output = path.join( root, "temp/artifacts/bsr-parity" );
fs.mkdirSync( output, { recursive: true } );
const hash = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
const read = url =>
	process.argv.includes( "--packed" ) ?
		readPackedAssetBytesSync( url, publicRoot ) :
		readPublishedAssetBytesSync( url, publicRoot );
async function load( relative, name ) {
	const outfile = path.join( output, name + ".mjs" );
	await build( {
		entryPoints: [ path.join( root, relative ) ],
		outfile,
		bundle: true,
		platform: "node",
		format: "esm"
	} );
	return import( pathToFileURL( outfile ) );
}
const { modelAmbientParticles } = await load( "src/engine/foundation/animation/model-emission.ts", "admission" );
const { modelAnimationParticles } = await load(
	"src/engine/foundation/animation/animation-emission.ts",
	"animation-admission"
);
const { createEffectDecoder } = await load( "src/engine/runtime/assets/worker/effects/effects.ts", "decoder" );
const decoder = createEffectDecoder(),
	programBytes = read( "/assets/effects/programs.json" ),
	programs = JSON.parse( programBytes );
const report = {
	format: "sro-bsr-parity-v1",
	qualification:
		"v1.150 original BSR -> published metadata -> production decoder; admission is not native pixel equivalence",
	inputs: { programs: hash( programBytes ) },
	models: [],
	effects: [],
	issues: [],
	open: [],
	tests: null
};
const cache = new Map(), effects = new Set();
for ( const domain of [ "itemdrop", "npc", "skillfx" ] ) {
	const bytes = read( `/assets/${domain}/manifest.json` ), manifest = JSON.parse( bytes );
	report.inputs[domain] = hash( bytes );
	for ( const [key, row] of Object.entries( manifest.models ) ) {
		const source = row.bsr ?? (key.startsWith( "res/" ) ? key : `res/${key}`);
		if ( !cache.has( source ) ) {
			const bytes = fs.readFileSync( dataAssetPath( source ) );
			cache.set( source, { bsr: parseCharacterBsr( bytes, source ), sha256: hash( bytes ) } );
		}
		const { bsr, sha256 } = cache.get( source ),
			entry = {
				domain,
				key,
				source,
				sha256,
				ambient: 0,
				selectors: bsr.particleModifiers.map( m => ({
					kind: m.kind,
					stateId: m.stateId,
					set: m.animationSetName,
					entries: m.entries.length
				}) ),
				materials: bsr.materialModifiers.length,
				textures: bsr.textureModifiers.length,
				sounds: bsr.soundModifiers.length,
				animationStates: bsr.animationSets.map( s => ({
					name: s.name,
					states: s.states.map( r => r.stateId )
				}) )
			};
		if ( JSON.stringify( row.modifierSets ) !== JSON.stringify( bsr.modifierSets ) ) {
			report.issues.push( { domain, key, reason: "modifier set inventory missing or changed" } );
		}
		try {
			modelAnimationParticles( row.particleModifiers );
		} catch ( e ) {
			report.issues.push( { domain, key, reason: String( e ) } );
		}
		if ( JSON.stringify( row.particleModifiers ) !== JSON.stringify( bsr.particleModifiers ) ) {
			report.issues.push( {
				domain,
				key,
				reason: "original particle payload missing or changed at publication"
			} );
		}
		try {
			const particles = modelAmbientParticles( bsr.particleModifiers );
			entry.ambient = particles.length;
			for ( const p of particles ) effects.add( p.effectPath );
		} catch ( e ) {
			report.issues.push( { domain, key, reason: String( e ) } );
		}
		if ( domain === "npc" || domain === "skillfx" ) {
			const bindings = row.animationBindings;
			if (
				!Array.isArray( bindings ) ||
				bindings.length !== bsr.animationSets.reduce( ( n, s ) => n + s.states.length, 0 )
			) report.issues.push( { domain, key, reason: "incomplete authored animation bindings" } );
			else {for ( const set of bsr.animationSets ) {
					for ( const state of set.states ) {
						const binding = bindings.find( b => b.set === set.name && b.stateId === state.stateId );
						if (
							!binding || (state.animationPath && binding.path !== state.animationPath) ||
							(binding.clip !== null && !row.clips.includes( binding.clip )) ||
							(binding.clip === null && !binding.reason)
						) {
							report.issues.push( {
								domain,
								key,
								reason: "unbound authored animation state",
								set: set.name,
								stateId: state.stateId
							} );
						}
					}
				}}
		}
		for ( const selector of entry.selectors ) {
			if ( selector.kind === 0 ) {
				report.open.push( {
					domain,
					key,
					source,
					reason: "named activation producer requires native closure",
					selector
				} );
			}
		}
		for ( const modifier of [ ...bsr.materialModifiers, ...bsr.textureModifiers ] ) {
			if ( modifier.kind === 0 ) {
				report.open.push( {
					domain,
					key,
					source,
					reason: "named material activation producer requires closure",
					kind: modifier.kind,
					stateId: modifier.stateId,
					set: modifier.animationSetName
				} );
			}
		}
		for ( const field of [ "materialModifiers", "textureModifiers" ] ) {
			if ( JSON.stringify( row[field] ) !== JSON.stringify( bsr[field] ) ) {
				report.issues.push( {
					domain,
					key,
					reason: "original " + field + " missing or changed at publication"
				} );
			}
		}
		report.models.push( entry );
	}
}
const allEffects = new Set( [
	...effects,
	...programs.reachability.entityParticleReferences.map( row => row.effectPath )
] );
for ( const resource of [ ...allEffects ].sort() ) {
	const entry = { resource, ambient: effects.has( resource ) };
	try {
		const { model, imagePaths } = decoder.model( programBytes, resource );
		for ( const url of imagePaths ) read( url );
		entry.primitives = model.primitives.length;
		entry.images = imagePaths.length;
	} catch ( e ) {
		entry.error = String( e );
		report.issues.push( { resource, reason: entry.error } );
	}
	report.effects.push( entry );
}
const archiveEvidence = JSON.parse(
	fs.readFileSync( path.join( root, "../../scripts/build/reference/particle-archive.json" ), "utf8" )
);
if (
	programs.nativeUnavailable?.archiveSha256 !== archiveEvidence.sha256 ||
	JSON.stringify( programs.nativeUnavailable?.effects ) !== JSON.stringify( archiveEvidence.absent )
) report.issues.push( { reason: "published native-empty resource list differs from original archive certificate" } );
for ( const resource of archiveEvidence.absent ) {
	try {
		const { model, imagePaths } = decoder.model( programBytes, resource );
		if ( model.nodes.length || model.primitives.length || imagePaths.length ) {
			throw Error( "Absent native resource produced a visual" );
		}
	} catch ( e ) {
		report.issues.push( { resource, reason: String( e ) } );
	}
}
report.nativeUnavailable = {
	archiveSha256: archiveEvidence.sha256,
	effects: archiveEvidence.absent,
	behavior: "B1F270 reset, failed read; AFF730 retains empty stored object"
};
decoder.dispose();
if ( process.argv.includes( "--verify" ) ) {
	const args = [
		"--test",
		"tests/architecture/bsr-dependency-audit.test.mjs",
		"tests/runtime/bsr-particle-transform.test.mjs",
		"tests/runtime/deferred-particles.test.mjs",
		"tests/runtime/entity-lod.test.mjs",
		"tests/runtime/animation-dispatch.test.mjs",
		"tests/runtime/blended-modifiers.test.mjs",
		"tests/runtime/authored-animation-bindings.test.mjs",
		"tests/runtime/ground-visual.test.mjs",
		"tests/runtime/model-emission.test.mjs",
		"tests/runtime/entity-material.test.mjs",
		"tests/runtime/scenery-modifiers.test.mjs",
		"tests/runtime/world-material.test.mjs",
		"tests/runtime/character-animation-audio.test.mjs",
		"tests/runtime/character-presentation.test.mjs",
		"tests/runtime/ground-items.test.mjs",
		"tests/runtime/native-unavailable-effects.test.mjs"
	];
	const result = spawnSync( process.execPath, args, { cwd: root, encoding: "utf8", timeout: 120000 } );
	fs.writeFileSync( path.join( output, "tests.log" ), (result.stdout ?? "") + (result.stderr ?? "") );
	report.tests = { command: [ process.execPath, ...args ], exit: result.status, error: result.error?.message };
	if ( result.status !== 0 ) report.issues.push( { reason: "production-path verification failed" } );
	report.nativeChecks = [];
	for (
		const script of [
			"tools/verify-modifier-keys.mjs",
			"tools/verify-animation-dispatch.mjs",
			"tools/verify-deferred-alpha.mjs",
			"tools/verify-entity-model-publication.mjs"
		]
	) {
		const args = [
			script,
			...(script.endsWith( "verify-entity-model-publication.mjs" ) && process.argv.includes( "--packed" ) ?
				[ "--packed" ] :
				[])
		];
		const result = spawnSync( process.execPath, args, { cwd: root, encoding: "utf8", timeout: 120000 } );
		fs.writeFileSync(
			path.join( output, path.basename( script ) + ".log" ),
			(result.stdout ?? "") + (result.stderr ?? "")
		);
		report.nativeChecks.push( { script, exit: result.status, error: result.error?.message } );
		if ( result.status !== 0 ) report.issues.push( { reason: "production/native verification failed", script } );
	}
	const transforms = spawnSync( process.execPath, [ "tools/verify-bsr-transforms.mjs" ], {
		cwd: root,
		encoding: "utf8",
		timeout: 120000
	} );
	fs.writeFileSync(
		path.join( output, "transforms-native.log" ),
		(transforms.stdout ?? "") + (transforms.stderr ?? "")
	);
	report.nativeChecks.push( {
		script: "tools/verify-bsr-transforms.mjs",
		exit: transforms.status,
		error: transforms.error?.message
	} );
	if ( transforms.status !== 0 ) report.issues.push( { reason: "original particle transform verification failed" } );
	for ( const script of [ "../../scripts/analysis/verify_particle_archive.py", "tools/verify-native-bsr.py" ] ) {
		const result = spawnSync( process.env.SRO_PYTHON ?? "C:/Program Files/Python312/python.exe", [ script ], {
			cwd: root,
			encoding: "utf8",
			timeout: 120000
		} );
		fs.writeFileSync(
			path.join( output, path.basename( script ) + ".log" ),
			(result.stdout ?? "") + (result.stderr ?? "")
		);
		report.nativeChecks.push( { script, exit: result.status, error: result.error?.message } );
		if ( result.status !== 0 ) {
			report.issues.push( { reason: "original archive/machine verification failed", script } );
		}
	}
}
fs.writeFileSync( path.join( output, "report.json" ), JSON.stringify( report, null, 2 ) + "\n" );
console.log(
	JSON.stringify(
		{
			models: report.models.length,
			ambientModels: report.models.filter( r => r.ambient ).length,
			ambientEffects: effects.size,
			issues: report.issues.length,
			open: report.open.length,
			tests: report.tests?.exit,
			report: path.join( output, "report.json" )
		},
		null,
		2
	)
);
if ( report.issues.length || process.argv.includes( "--strict" ) && report.open.length ) process.exitCode = 1;
