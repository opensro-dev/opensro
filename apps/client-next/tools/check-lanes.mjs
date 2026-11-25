/*
===========================================================================

check-lanes.mjs - the client check's gates and the three lanes over them

The full check (every gate), CI's two source lanes (no retail assets) and
the release's asset lane, which is their complement: what the source
lanes leave out. check.mjs runs a lane; this module only names them, so
a test can prove the source and asset lanes cover the full check once.

===========================================================================
*/

import { readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { LIST_PATH as ASSET_BACKED_LIST } from "./list-asset-backed-tests.mjs";

const clientRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const SOURCE_FLAG = "--source";
// The release's lane: what CI's source lanes cannot run (assetGates).
const ASSETS_FLAG = "--assets";
// The test runner's timing lines: its run summary, its slowest files and
export const ASSET_ARCHITECTURE_TESTS = new Set( [ "audio-parity-audit.test.mjs" ] );
// verify:ownership and verify:capabilities read only the source tree. Outside
// this list a split file with no manifest row reached main unseen (#3740's
// label-fit.ts): ownership.test.mjs checks fixtures, not the real manifest.
export const SOURCE_GATE_NAMES = new Set( [
	"verify:test-types",
	"typecheck",
	"typecheck:gallery",
	"verify:layout",
	"verify:dependencies",
	"verify:ui-census",
	"verify:ownership",
	"verify:capabilities"
] );

// Longest first, so the critical path starts immediately.
export const GATES = [
	// Reuses each file's pass while every input it read is unchanged.
	[ "test", [ "tools/run-tests.mjs", "tests/architecture", "tests/runtime" ] ],
	// The joint-longest gate: keys its own inputs (ui.ts's import closure,
	// the tool, the committed census, the built assets) and reuses the
	// last pass while they are unchanged (ui-action-census-gate.mjs).
	[ "verify:ui-action-census", [ "tools/ui-action-census-gate.mjs" ] ],
	[ "verify:test-types", [ "tools/verify-test-types.mjs" ] ],
	[ "typecheck", [
		path.join( clientRoot, "node_modules", "typescript", "bin", "tsc" ),
		"--noEmit",
		"-p",
		"tsconfig.json"
	] ],
	[ "typecheck:gallery", [
		path.join( clientRoot, "node_modules", "typescript", "bin", "tsc" ),
		"--noEmit",
		"-p",
		"tools/ui-gallery/tsconfig.json"
	] ],
	[ "verify:execution", [ "tools/generate-execution-map.mjs" ] ],
	[ "verify:ownership", [ "tools/verify-ownership.mjs" ] ],
	[ "verify:dependencies", [ "tools/verify-dependencies.mjs" ] ],
	[ "verify:allocations", [ "--expose-gc", "tools/perf/bench/ui-allocation-gauge.mjs" ] ],
	[ "verify:ui-census", [ "tools/ui-census.mjs" ] ],
	[ "verify:capabilities", [ "tools/verify-capabilities.mjs" ] ],
	[ "verify:layout", [ "tools/verify-layout.mjs" ] ],
	[ "verify:retail-panels", [ "tools/audit-retail-panels.mjs" ] ],
	[ "verify:delivery", [ "../../scripts/checks/check_asset_delivery.mjs" ] ],
	[ "verify:item-sounds", [ "tools/generate-item-sounds.mjs" ] ],
	[ "audit:audio", [ "tools/audit-audio-parity.mjs" ] ]
];

/*
================
sourcePart

The source lane's part an argument names: --source runs both, CI runs
--source=gates and --source=runtime as two jobs on two runners, so the
runtime files' ~600 processes never share a two-core runner with the
typechecks (together they took the job from 87-132 s to 220-230 s and
pushed execution.test.mjs past its cap; ops #335 row 1). Undefined for
anything else.
================
*/
const SOURCE_PARTS = {
	all: ( gates, runtime ) => [ ...gates, ...runtime ],
	gates: ( gates ) => gates,
	runtime: ( _gates, runtime ) => runtime
};

export function sourcePart( args ) {
	if ( args[0] === SOURCE_FLAG ) return "all";
	const named = args[0]?.startsWith( SOURCE_FLAG + "=" ) ? args[0].slice( SOURCE_FLAG.length + 1 ) : undefined;
	return named !== undefined && Object.hasOwn( SOURCE_PARTS, named ) && named !== "all" ? named : undefined;
}

/*
================
sourceGates

The audio parity audit consumes the extracted native sound and animation tables.
Keep that file in the full asset lane; do not silently skip missing files inside
its tests. Every other architecture suite runs from a clean source checkout.
================
*/
export function sourceGates( part ) {
	const architecture = readdirSync( path.join( clientRoot, "tests/architecture" ) )
		.filter( ( name ) => name.endsWith( ".test.mjs" ) && !ASSET_ARCHITECTURE_TESTS.has( name ) )
		.sort()
		.map( ( name ) => `tests/architecture/${name}` );
	// Every runtime file but the asset-backed ones (ops #335 row 1: the lane
	// ran a hand-picked 17 of 766, so the rest regressed unseen until a
	// release build). The list is derived from what each file read
	// (tools/list-asset-backed-tests.mjs).
	const skip = "--skip=" + path.relative( clientRoot, ASSET_BACKED_LIST ).replaceAll( "\\", "/" );
	const gates = [
		[ "source tests", [ "tools/run-tests.mjs", ...architecture ] ],
		...GATES.filter( ( [name] ) => SOURCE_GATE_NAMES.has( name ) )
	];
	const runtime = [ [ "source runtime tests", [ "tools/run-tests.mjs", skip, "tests/runtime" ] ] ];
	return SOURCE_PARTS[part]( gates, runtime );
}

/*
================
assetGates

The full check less CI's two source lanes (sourceGates): the asset-backed
runtime files, the asset architecture suite, and every gate outside
SOURCE_GATE_NAMES (the action census, the execution map, delivery, audio
and the rest read the built assets). The release runs this lane only:
its ci_state step refuses a freeze unless every check run at that commit
is green, the source lanes included, and re-running them in the package
step cost r204m's re-freeze 213 s of test types, 93 s of gallery types and
277 test files, all on the 16 cores the 450 s action census needed.
================
*/
export function assetGates() {
	const only = "--only=" + path.relative( clientRoot, ASSET_BACKED_LIST ).replaceAll( "\\", "/" );
	const architecture = [ ...ASSET_ARCHITECTURE_TESTS ].sort().map( ( name ) => `tests/architecture/${name}` );
	return [
		[ "asset tests", [ "tools/run-tests.mjs", only, "tests/runtime", ...architecture ] ],
		...GATES.filter( ( [name] ) => name !== "test" && !SOURCE_GATE_NAMES.has( name ) )
	];
}

/*
================
checkLane

The lane the arguments name: a source part, the asset lane or the full check.
================
*/
export function checkLane( args ) {
	if ( args.length === 0 ) return { name: "full", gates: GATES };
	if ( args.length === 1 && args[0] === ASSETS_FLAG ) return { name: "assets", gates: assetGates() };
	const part = sourcePart( args );
	if ( args.length === 1 && part !== undefined ) {
		return { name: "source " + part, gates: sourceGates( part ), source: true };
	}
	throw new Error( `Usage: node tools/check.mjs [--source[=gates|runtime] | --assets]` );
}
