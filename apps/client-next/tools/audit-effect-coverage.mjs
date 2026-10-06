// Whole published-catalog inventory. Admission is not a visual parity claim.
import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";
const root = path.resolve( import.meta.dirname, ".." ), out = path.join( root, "temp/artifacts/effect-coverage" );
fs.mkdirSync( out, { recursive: true } );
const source = path.join( root, "src/engine/runtime/assets/worker/effects/effects.ts" ),
	modulePath = path.join( out, "decoder.mjs" );
await build( { entryPoints: [ source ], outfile: modulePath, bundle: true, platform: "node", format: "esm" } );
const { createEffectDecoder } = await import( pathToFileURL( modulePath ) );
const assetRoot = CLIENT_PUBLIC_ROOT, read = p => fs.readFileSync( path.join( assetRoot, "assets", p ) );
const recordBytes = read( "skill/effectRecords.json" ),
	programBytes = read( "effects/programs.json" ),
	manifest = JSON.parse( read( "skillfx/manifest.json" ) ),
	models = manifest.models;
const decoder = createEffectDecoder(), records = JSON.parse( recordBytes ), catalog = decoder.decode( recordBytes );
const hash = b => createHash( "sha256" ).update( b ).digest( "hex" );
const report = {
	format: "sro-effect-coverage-v1",
	qualification: "catalog admission inventory; runtime semantics and native pixels require independent tests",
	inputs: {
		records: hash( recordBytes ),
		programs: hash( programBytes ),
		decoder: hash( fs.readFileSync( modulePath ) )
	},
	records: Object.keys( records ).length,
	stages: 0,
	actions: {},
	moves: {},
	scripts: {},
	nonDefaultFields: {},
	scriptedRecords: [],
	stageIssues: [],
	resources: [],
	programFailures: []
};
const bump = ( rows, k ) => rows[k] = (rows[k] ?? 0) + 1, paths = new Set(), scripted = new Set();
for (
	const file of [
		"runtime/characters/effects/effects.ts",
		"runtime/renderer/characters/characters.ts",
		"foundation/animation/effect-script.ts",
		"foundation/animation/moving-stage.ts",
		"foundation/animation/hawk.ts",
		"runtime/characters/damage-feedback.ts",
		"contracts/effects.ts"
	]
) report.inputs[file] = hash( fs.readFileSync( path.join( root, "src/engine", file ) ) );
for ( const [id, record] of Object.entries( catalog ) ) {
	for ( let index = 0; index < record.stages.length; index++ ) {
		const s = record.stages[index], raw = records[id].authoredStages[index];
		report.stages++;
		bump( report.actions, s.action );
		bump( report.moves, s.move );
		bump( report.scripts, s.scripts[0] ?? "NONE" );
		if ( s.scripts.length ) scripted.add( id );
		for ( const field of [ "id", "attach", "trade", "kill", "scale", "rotate", "fadeInMs", "fadeOutMs" ] ) {
			if ( raw[field] !== null && raw[field] !== undefined && raw[field] !== 0 ) {
				bump( report.nonDefaultFields, field );
			}
		}
		if ( raw.damageTypes?.length ) {
			bump( report.nonDefaultFields, "damageTypes" );
		}
		if ( raw.actionOptions?.enabled ) bump( report.nonDefaultFields, "actionOptions.enabled" );
		const reasons = [];
		if ( s.script.kind === "unsupported" ) reasons.push( "script:" + s.script.operation );
		if (
			s.count !== 1 &&
			![ "AT_SOURCE", "AT_TARGET", "AT_MOV_1TAR", "AT_MOV_SPLASH", "AT_MOV_OPTION" ].includes( s.action )
		) reasons.push( "multi-instance dispatch requires lane review" );
		if ( reasons.length ) {
			report.stageIssues.push( {
				id,
				index,
				phase: s.phase,
				event: s.startEvent,
				resource: s.resource,
				reasons
			} );
		}
		for ( const resource of [ s.resource, s.arrivalResource ] ) if ( resource ) paths.add( resource );
	}
}
for ( const record of Object.values( catalog ) ) {
	for ( const path of [ ...(record.arrowEffects ?? []), record.damageEffect ] ) {
		if ( path ) paths.add( path );
	}
}
for ( const path of Object.values( manifest.weapons ?? {} ) ) paths.add( path );
report.scriptedRecords = [ ...scripted ].sort( ( a, b ) => Number( a ) - Number( b ) );
for ( const resource of [ ...paths ].sort() ) {
	const row = { resource, admission: null };
	try {
		if ( resource === "weapon" ) {
			if ( !Object.keys( manifest.weapons ?? {} ).length ) throw Error( "No equipped weapon resource mappings" );
			row.admission = { kind: "equipped-weapon", references: Object.keys( manifest.weapons ).length };
		} else if ( resource.endsWith( ".efp" ) ) {
			const { model, imagePaths } = decoder.model( programBytes, resource );
			const missing = imagePaths.filter( p => !fs.existsSync( path.join( assetRoot, p ) ) );
			if ( missing.length ) throw Error( "Missing textures: " + missing.join( "," ) );
			row.admission = { kind: "efp", primitives: model.primitives.length, images: imagePaths.length };
		} else if ( models[resource] ) {
			if ( !fs.existsSync( path.join( assetRoot, models[resource].glb ) ) ) {
				throw Error( "Published model file missing" );
			}
			row.admission = { kind: "model-manifest", path: models[resource].glb };
		} else throw Error( "No model/resource handler" );
	} catch ( error ) {
		row.error = String( error );
		report.programFailures.push( { resource, error: row.error } );
	}
	report.resources.push( row );
	if ( report.resources.length % 250 === 0 ) {
		console.log( "Audited " + report.resources.length + "/" + paths.size + " resources" );
	}
}
decoder.dispose();
const destination = path.join( out, "report.json" );
fs.writeFileSync( destination, JSON.stringify( report, null, 2 ) + "\n" );
console.log(
	JSON.stringify(
		{
			records: report.records,
			stages: report.stages,
			scriptedRecords: scripted.size,
			knownStageIssues: report.stageIssues.length,
			resources: paths.size,
			resourceFailures: report.programFailures.length,
			report: destination
		},
		null,
		2
	)
);
if (
	process.argv.includes( "--strict" ) &&
	(report.stageIssues.length || report.programFailures.length || Object.keys( report.nonDefaultFields ).length)
) {
	console.error(
		"VFX acceptance blocked: unresolved stage branches and native control-field semantics remain. Resource admission is not whole-game visual fidelity."
	);
	process.exitCode = 1;
}
