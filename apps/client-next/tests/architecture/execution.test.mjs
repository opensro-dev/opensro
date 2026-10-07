import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { root, ts, visit } from "../../tools/project.mjs";
import { executionFlow } from "../../tools/execution-flow.mjs";
import { verifyExecution } from "../../tools/verify-execution.mjs";
import { serializeExecutionMap } from "../../tools/generate-execution-map.mjs";
import { defined } from "../helpers/defined.mjs";
let applicationGraph;
const realGraph = () => applicationGraph ??= executionFlow();
const runtime = "src/engine/runtime/runtime.ts";
// Source mutations exercise the analyzer on a closed, minimal project. Keep
// application-wide grants and call resolution covered by realGraph below.
function fixture( edit, check, application = false ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-execution-" ) );
	const write = ( name, text ) => {
		const file = path.join( dir, name );
		fs.mkdirSync( path.dirname( file ), { recursive: true } );
		fs.writeFileSync( file, text );
	};
	try {
		if ( application ) {
			for ( const name of [ "src/engine/ownership.json", runtime, "execution-contract.json" ] ) {
				write( name, fs.readFileSync( path.join( root, name ) ) );
			}
		} else {
			write(
				"tsconfig.json",
				JSON.stringify( {
					compilerOptions: {
						target: "ES2022",
						module: "ESNext",
						moduleResolution: "Bundler",
						noLib: true,
						types: []
					},
					include: [ "src/**/*.ts" ]
				} )
			);
			write( "src/globals.d.ts", "declare function fetch(url: string): void;" );
			write(
				"src/engine/ownership.json",
				JSON.stringify( { bootstrap: runtime, workers: {}, modules: {}, internals: {} } )
			);
			write(
				runtime,
				`import {createRenderer} from './renderer/renderer';
import {createInput} from './input/input';
const renderer = createRenderer(), input = createInput();
const simulation = {poll() { return 1; }};
export function frame() {
 const snapshot = simulation.poll();
 input.drain();
 renderer.frame(snapshot);
}`
			);
			write(
				"src/engine/runtime/renderer/renderer.ts",
				"export function createRenderer() { return {frame(snapshot: number) {}}; }"
			);
			write( "src/engine/runtime/input/input.ts", "export function createInput() { return {drain() {}}; }" );
			write( "src/engine/runtime/platform/platform.ts", "export {};" );
			write( "proof.json", JSON.stringify( { functions: [ { va: 1 } ] } ) );
			write( "regression.mjs", "" );
			write(
				"execution-contract.json",
				JSON.stringify( {
					capabilities: [ [ "renderer/renderer", "createRenderer", "frame" ], [
						"input/input",
						"createInput",
						"drain"
					] ].map( ( [file, ...functions] ) => ({
						consumer: runtime,
						provider: `src/engine/runtime/${file}.ts`,
						functions
					}) ),
					boundaries: [],
					sequences: [ {
						file: runtime,
						function: "frame",
						calls: [
							"src/engine/runtime/runtime.ts#poll",
							"src/engine/runtime/input/input.ts#drain",
							"src/engine/runtime/renderer/renderer.ts#frame"
						]
					} ],
					frameBarriers: [ {
						file: runtime,
						function: "frame",
						sourceSha256: createHash( "sha256" ).update( fs.readFileSync( path.join( dir, runtime ) ) )
							.digest( "hex" ),
						reason: "Fixture seal",
						evidence: "proof.json",
						nativeFunction: 1,
						tests: [ "regression.mjs" ]
					} ]
				} )
			);
			assert.deepEqual( verifyExecution( dir ), [], "mutation fixture starts valid" );
		}
		// Sealed frame barriers also own evidence and regression dependencies. A
		// detached source fixture must include them before testing execution edits.
		const contract = JSON.parse( fs.readFileSync( path.join( dir, "execution-contract.json" ), "utf8" ) );
		for ( const barrier of application ? contract.frameBarriers ?? [] : [] ) {
			for ( const name of [ barrier.evidence, ...barrier.tests ] ) {
				const destination = path.join( dir, name );
				fs.mkdirSync( path.dirname( destination ), { recursive: true } );
				fs.copyFileSync( path.join( root, name ), destination );
			}
		}
		edit( dir );
		check( dir );
	} finally {
		const resolved = fs.realpathSync( dir );
		assert.equal( path.dirname( resolved ), fs.realpathSync( os.tmpdir() ) );
		assert.ok( path.basename( resolved ).startsWith( "sro-execution-" ) );
		fs.rmSync( resolved, { recursive: true, force: true } );
	}
}
test("current application execution contract passes", () =>
	assert.deepEqual( verifyExecution( root, realGraph() ), [] ));

test("camera sequence binds both the call site and resolved owner despite conservative step unions", () => {
	const graph = realGraph(), defs = new Map( graph.functions.map( f => [ f.id, f ] ) );
	const pump = graph.calls.find( c => c.file === runtime && c.expression === "world.pumpCameraScripts" );
	const character = graph.calls.find( c => c.file === runtime && c.expression === "characters.step" );
	const world = graph.calls.find( c => c.file === runtime && c.expression === "world.step" );
	assert.ok( pump && character && world );
	const reordered = graph.calls.filter( c => c !== pump );
	reordered.splice( reordered.indexOf( world ) + 1, 0, pump );
	assert.ok(
		verifyExecution( root, { ...graph, calls: reordered } ).some( s =>
			s.includes( "required order" ) && s.includes( "pumpCameraScripts" )
		)
	);
	const wrong = graph.calls.map( c =>
		c === character ?
			{
				...c,
				targets: c.targets.filter( id => defs.get( id ).file !== "src/engine/runtime/characters/characters.ts" )
			} :
			c
	);
	assert.ok(
		verifyExecution( root, { ...graph, calls: wrong } ).some( s =>
			s.includes( "required order" ) && s.includes( "characters.step" )
		),
		"the right spelling cannot replace the required provider"
	);
});
test("comments and blank lines do not change execution identities or freshness", () => {
	let before;
	fixture( dir => {
		before = serializeExecutionMap( executionFlow( dir ) );
		for (
			const name of [ runtime, "src/engine/runtime/input/input.ts", "src/engine/runtime/renderer/renderer.ts" ]
		) {
			const file = path.join( dir, name );
			fs.writeFileSync(
				file,
				"// Formatting-only change\n\n" + fs.readFileSync( file, "utf8" ).replaceAll( "\n", "\n\n" )
			);
		}
	}, dir => {
		const graph = executionFlow( dir );
		assert.equal( serializeExecutionMap( graph ), before );
		assert.deepEqual(
			verifyExecution( dir, graph ),
			[ `Unverified native frame barrier: ${runtime}#frame` ],
			"semantic identities survive formatting, but the byte seal must still reject changed source"
		);
	} );
});
test("resolves issued GPU methods and input callback implementations", () => {
	const graph = realGraph();
	const defs = new Map( graph.functions.map( f => [ f.id, f ] ) );
	const move = graph.calls.find( c =>
		c.file.endsWith( "/renderer/world/world.ts" ) && c.expression === "lease.takeWorld"
	);
	assert.ok(
		move.targets.some( t =>
			defs.get( t ).file.endsWith( "/assets/world-lease.ts" ) && defs.get( t ).name === "takeWorld"
		),
		"Map-held job capabilities retain their actual provider"
	);
	const call = graph.calls.find( c => c.file.endsWith( "/frame/frame.ts" ) && c.expression.endsWith( ".submit" ) );
	assert.ok( call.targets.some( t => defs.get( t ).file.endsWith( "/device/device.ts" ) ) );
	assert.ok(
		graph.calls.some( c =>
			c.file.endsWith( "/platform/platform.ts" ) &&
			c.targets.some( t => defs.get( t ).file.endsWith( "/input/input.ts" ) )
		)
	);
});

test("Map-held scene handoffs still require their explicit consumer grant", () =>
	fixture( dir => {
		const p = path.join( dir, "execution-contract.json" ), contract = JSON.parse( fs.readFileSync( p, "utf8" ) );
		contract.capabilities = contract.capabilities.filter( g =>
			!(g.consumer.endsWith( "/renderer/world/world.ts" ) && g.provider.endsWith( "/assets/world-lease.ts" ))
		);
		fs.writeFileSync( p, JSON.stringify( contract ) );
	}, dir => {
		assert.ok(
			verifyExecution( dir, realGraph() ).some( issue =>
				issue.includes( "undeclared capability" ) && issue.includes( "world-lease.ts#takeWorld" )
			)
		);
	}, true ));
test("rejects unresolvable indirect execution", () =>
	fixture(
		dir =>
			fs.appendFileSync( path.join( dir, runtime ), "\nconst untracked:any=globalThis; untracked.arbitrary();" ),
		dir => assert.ok( verifyExecution( dir ).some( x => x.includes( "unresolved execution" ) ) )
	));

test("scenery admission, warnings, material updates and frontend logout require their exact grants", () =>
	fixture( dir => {
		const p = path.join( dir, "execution-contract.json" ), contract = JSON.parse( fs.readFileSync( p, "utf8" ) );
		for ( const grant of contract.capabilities ) {
			if ( grant.consumer === "src/engine/foundation/animation/scenery-emission.ts" ) {
				grant.functions = grant.functions.filter( n => n !== "ready" );
			}
			if ( grant.consumer === "src/engine/foundation/animation/animation-dispatch.ts" ) {
				grant.functions = grant.functions.filter( n => n !== "callback:step:2" );
			}
			if (
				grant.consumer === "src/engine/foundation/animation/model-emission.ts" &&
				grant.provider === "src/engine/runtime/characters/presentation-finalize.ts"
			) grant.functions = grant.functions.filter( n => n !== "callback:step:4" );
			if (
				grant.consumer === "src/engine/runtime/characters/characters.ts" &&
				grant.provider === "src/engine/runtime/renderer/renderer.ts"
			) grant.functions = grant.functions.filter( n => n !== "presentationCamera" );
			if ( grant.consumer === "src/engine/foundation/rendering/scenery-particles.ts" ) {
				grant.functions = grant.functions.filter( n => n !== "callback:sceneryParticles:5" );
			}
			if (
				grant.consumer === "src/engine/runtime/characters/characters.ts" &&
				grant.provider === "src/engine/runtime/renderer/world/world.ts"
			) grant.functions = grant.functions.filter( n => n !== "scenery" );
			if (
				grant.consumer === "src/engine/runtime/renderer/world/world.ts" &&
				grant.provider === "src/engine/runtime/renderer/device/geometry.ts"
			) grant.functions = grant.functions.filter( n => n !== "updateMaterialColors" );
			if ( grant.consumer === "src/engine/runtime/frontend/frontend.ts" && grant.provider === runtime ) {
				grant.functions = grant.functions.filter( n => n !== "callback:createFrontend:3" );
			}
		}
		contract.boundaries = contract.boundaries.filter( boundary =>
			!(boundary.file === "src/engine/foundation/animation/scenery-emission.ts" &&
				boundary.expression === "ready" && boundary.function === "step") &&
			!(boundary.file === "src/engine/runtime/characters/presentation-finalize.ts" &&
				boundary.expression === "renderer.scenery" && boundary.function === "presentEmission")
		);
		fs.writeFileSync( p, JSON.stringify( contract ) );
	}, dir => {
		const issues = verifyExecution( dir, realGraph() );
		for (
			const target of [
				"characters/characters.ts#callback:step:2",
				"characters/presentation-finalize.ts#callback:step:4",
				"renderer/renderer.ts#presentationCamera",
				"world/world.ts#callback:sceneryParticles:5",
				"device/geometry.ts#updateMaterialColors",
				"runtime.ts#callback:createFrontend:3"
			]
		) assert.ok( issues.some( s => s.includes( "undeclared capability" ) && s.endsWith( target ) ), target );
		for (
			const [file, expression] of [
				[ "foundation/animation/scenery-emission.ts", "ready" ],
				[ "runtime/characters/presentation-finalize.ts", "renderer.scenery" ]
			]
		) {
			assert.ok(
				issues.some( issue =>
					issue.includes( file ) && issue.endsWith( `unresolved execution ${expression}` )
				),
				`${file}: ${expression}`
			);
		}
	}, true ));
test("rejects a renamed external capability through a computed key", () =>
	fixture(
		dir =>
			fs.appendFileSync(
				path.join( dir, "src/engine/runtime/platform/platform.ts" ),
				'\nconst browser = globalThis; const key = ("fet" + "ch") as "fetch"; const stolen = browser[key]; stolen("/unexpected");'
			),
		dir =>
			assert.ok( verifyExecution( dir ).some( x => x.includes( "resolved external fetch escapes its owner" ) ) )
	));
test("rejects async helper reached through a returned method", () =>
	fixture( dir => {
		const p = path.join( dir, "src/engine/runtime/input/input.ts" );
		let text = fs.readFileSync( p, "utf8" );
		// Returning a Promise from drain violates both the type gate and frame closure.
		text = text.replace( /drain\(\)/, "async drain()" );
		fs.writeFileSync( p, text );
	}, dir => assert.ok( verifyExecution( dir ).some( x => x.includes( "Async function in frame closure" ) ) ) ));
test("rejects frame sequence drift", () =>
	fixture( dir => {
		const p = path.join( dir, runtime );
		fs.writeFileSync(
			p,
			fs.readFileSync( p, "utf8" ).replace(
				"const snapshot = simulation.poll();",
				"renderer.frame(0);\n        const snapshot = simulation.poll();"
			)
		);
	}, dir => assert.ok( verifyExecution( dir ).some( x => x.includes( "required order" ) ) ) ));
test("rejects undeclared issued capability even with valid imports", () =>
	fixture( dir => {
		const p = path.join( dir, "execution-contract.json" ), contract = JSON.parse( fs.readFileSync( p, "utf8" ) );
		contract.capabilities = contract.capabilities.filter( c => !c.consumer.endsWith( "/frame/frame.ts" ) );
		fs.writeFileSync( p, JSON.stringify( contract ) );
	}, dir =>
		assert.ok( verifyExecution( dir, realGraph() ).some( x => x.includes( "undeclared capability" ) ) ), true ));
test("follows local aliases instead of trusting spelling", () =>
	fixture( dir => {
		const p = path.join( dir, runtime ),
			text = fs.readFileSync( p, "utf8" ),
			source = ts.createSourceFile( runtime, text, ts.ScriptTarget.Latest, true );
		let call;
		visit( source, node => {
			if ( ts.isCallExpression( node ) && node.expression.getText() === "renderer.frame" ) call = node;
		} );
		assert.ok( call, "runtime invokes renderer.frame" );
		fs.writeFileSync(
			p,
			text.slice( 0, call.getStart() ) + "const submitScene = renderer.frame; submitScene" +
				text.slice( call.expression.end )
		);
	}, dir => {
		const graph = executionFlow( dir ), defs = new Map( graph.functions.map( f => [ f.id, f ] ) );
		assert.ok(
			defined( graph.calls.find( c => c.expression === "submitScene" ) ).targets.some( t =>
				defs.get( t ).file.endsWith( "/renderer/renderer.ts" )
			)
		);
		assert.deepEqual(
			verifyExecution( dir, graph ),
			[ `Unverified native frame barrier: ${runtime}#frame` ],
			"alias resolves correctly but does not bypass the source seal"
		);
	} ));
