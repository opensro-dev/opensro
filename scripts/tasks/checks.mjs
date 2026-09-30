/*
===========================================================================

checks.mjs - repository check tasks and pipelines

CHECK_TASKS registers each gate with the task runner. CHECK_PIPELINES orders
them: "source" runs the gates that need no game data (CI and pre-push),
"full" adds the asset build and every asset-dependent suite.

===========================================================================
*/

import { commandTask, pipelineTask, seriesTask } from "./define.mjs";

export const CHECK_TASKS = [
	commandTask( {
		name: "check:loot-catalog",
		description: "Verify generated loot against committed versioned evidence",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "python",
		args: [ "-B", "scripts/build/generate_loot_catalog.py", "--check" ]
	} ),
	commandTask( {
		name: "check:client-preparation",
		description: "Test the PK2 reader and the client preparation tool on synthetic archives",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "python",
		args: [ "-B", "-m", "unittest", "discover", "-s", "scripts/test/python" ]
	} ),
	commandTask( {
		name: "check:release",
		description: "Test release admission, publication recovery and monitoring",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_release.mjs" ]
	} ),
	pipelineTask( {
		name: "check",
		description: "Run the complete concurrent repository check pipeline",
		kind: "check",
		ci: true,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		pipeline: "full"
	} ),
	pipelineTask( {
		name: "check:source",
		description: "Run the source-only repository policy pipeline",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "long",
		pipeline: "source"
	} ),
	commandTask( {
		name: "check:source-size",
		description: "Enforce source-file size policy",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_source_file_size.mjs" ]
	} ),
	commandTask( {
		name: "check:source-encoding",
		description: "Enforce UTF-8 without BOM and LF line endings on maintained source",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_source_encoding.mjs" ]
	} ),
	commandTask( {
		name: "check:shared-fixtures",
		description: "Keep the Go server's copies of shared native captures identical to the client's canonical files",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_shared_fixtures.mjs" ]
	} ),
	commandTask( {
		name: "check:format",
		description: "Enforce dprint (id Software style) formatting outside the shrinking baseline ledger",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_formatting.mjs" ]
	} ),
	commandTask( {
		name: "check:precompressed",
		description: "Validate freshness of precompressed assets",
		kind: "check",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/checks/check_precompressed_freshness.mjs" ]
	} ),
	commandTask( {
		name: "check:scripts",
		description: "Typecheck the scripts core allowlist",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		args: [
			"exec",
			"tsc",
			"--noEmit",
			"-p",
			"tsconfig.scripts.json",
			"--pretty",
			"false",
			"--incremental",
			"--tsBuildInfoFile",
			".state/tsc/scripts.tsbuildinfo"
		]
	} ),
	commandTask( {
		name: "check:scripts-tests",
		description: "Typecheck the scripts test allowlist",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		args: [
			"exec",
			"tsc",
			"--noEmit",
			"-p",
			"tsconfig.scripts.tests.json",
			"--pretty",
			"false",
			"--incremental",
			"--tsBuildInfoFile",
			".state/tsc/scripts-tests.tsbuildinfo"
		]
	} ),
	commandTask( {
		name: "check:server",
		description: "Run Go server checks",
		kind: "check",
		ci: true,
		requires: [ "go" ],
		timeoutClass: "long",
		command: "node",
		// Skipped when nothing it reads changed since its last pass (run_if_changed.mjs).
		args: [
			"scripts/checks/run_if_changed.mjs",
			"server",
			"apps/server",
			"scripts/checks/check_go_server.mjs",
			"@go,version",
			"!apps/server/.generated/game-data/1.150/server/manifest.json",
			"!.generated/client-public/assets/packs/manifest.json",
			"--",
			"node",
			"scripts/checks/check_go_server.mjs"
		]
	} ),
	commandTask( {
		name: "typecheck",
		description: "Typecheck every non-root workspace package",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		args: [ "-r", "--filter=!sro-browser-rebuild", "typecheck" ]
	} ),
	commandTask( {
		name: "workspace:check",
		description: "Run each non-root workspace package check script",
		kind: "internal",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		args: [ "-r", "--filter=!sro-browser-rebuild", "check" ]
	} )
];

const sourceTasks = [
	"check:release",
	"check:loot-catalog",
	"check:client-preparation",
	"check:source-size",
	"check:source-encoding",
	"check:shared-fixtures",
	"check:format",
	"check:scripts",
	"check:scripts-tests",
	"typecheck",
	"check:server"
];

export const CHECK_PIPELINES = Object.freeze( {
	// The source gates are independent, so they all start at once; the Go
	// gate is the critical path and the rest finish inside it.
	source: sourceTasks.map( ( task ) => ({
		id: task.replaceAll( ":", "-" ),
		task,
		after: []
	}) ),
	tests: [
		{ id: "build-resources", task: "assets:build:full", after: [] },
		{ id: "test-assets", task: "test:assets", after: [ "build-resources" ] },
		{ id: "test-cif", task: "test:cif", after: [ "build-resources" ] },
		{ id: "test-world", task: "test:world", after: [ "build-resources" ] },
		{ id: "test-region", task: "test:region", after: [ "build-resources" ] }
	],
	full: [
		{ id: "release", task: "check:release", after: [] },
		{ id: "loot-catalog", task: "check:loot-catalog", after: [] },
		{ id: "client-preparation", task: "check:client-preparation", after: [] },
		{ id: "server", task: "check:server", after: [] },
		{ id: "source-size", task: "check:source-size", after: [] },
		{ id: "source-encoding", task: "check:source-encoding", after: [] },
		{ id: "shared-fixtures", task: "check:shared-fixtures", after: [] },
		{ id: "format", task: "check:format", after: [] },
		{ id: "typecheck-packages", task: "workspace:check", after: [] },
		{ id: "typecheck-scripts", task: "check:scripts", after: [] },
		{ id: "typecheck-scripts-tests", task: "check:scripts-tests", after: [] },
		{ id: "build-resources", task: "assets:build:full", after: [] },
		{ id: "test-assets", task: "test:assets", after: [ "build-resources" ] },
		{ id: "asset-pack-integrity", task: "assets:check:integrity", after: [ "build-resources" ] },
		{ id: "test-cif", task: "test:cif", after: [ "build-resources" ] },
		{ id: "test-world", task: "test:world", after: [ "build-resources" ] },
		{ id: "test-region", task: "test:region", after: [ "build-resources" ] }
	]
} );
