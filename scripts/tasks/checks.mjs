/*
===========================================================================

checks.mjs - repository check tasks and pipelines

CHECK_TASKS registers each gate with the task runner. CHECK_PIPELINES orders
them: "source" runs the gates that need no game data (CI and pre-push),
"full" adds the asset build, the focused publishers, the server projection
and every suite that reads them.

===========================================================================
*/

import { commandTask, pipelineTask, seriesTask } from "./define.mjs";

// The operator console and its sign-in service: no game data, so their tests
// are source gates (check:console), not left to the full workspace check.
const CONSOLE_PACKAGES = [ "@sro/server-observatory", "@sro/server-console-auth" ];

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
		name: "check:compiled-quests",
		description: "Verify the compiled quest catalog against its committed evidence",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "python",
		args: [ "-B", "scripts/build/generate_compiled_quests.py", "--check" ]
	} ),
	commandTask( {
		name: "check:pet-design",
		description: "Verify the client's pet design module matches the server's design JSON",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "python",
		args: [ "-B", "scripts/tools/pet_design_export.py", "--check" ]
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
		name: "check:pipeline-contracts",
		description: "Run the asset pipeline's ordering contracts with stub steps (no game data)",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "--test", "scripts/test/pipeline/*.test.mjs" ]
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
	pipelineTask( {
		name: "check:source-portable",
		description: "Run the source pipeline without the Go gate (CI runs that on Linux)",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "long",
		pipeline: "source-portable"
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
		name: "check:generated-root",
		description: "Refuse generated-tree paths built outside scripts/lib/generatedRoot.mjs and its twins",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_generated_root.mjs" ]
	} ),
	commandTask( {
		name: "check:world-space",
		description: "Refuse open-coded * 1920 sector math and raw 0x8000 region-bit tests outside world-space.ts",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_world_space.mjs" ]
	} ),
	commandTask( {
		name: "check:plane-branches",
		description: "Refuse new dungeon region-bit branches outside the server's plane owner (#1646)",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_plane_branches.mjs" ]
	} ),
	commandTask( {
		name: "check:server-citations",
		description: "Refuse new reference-server class and function names in the server's Go (#1701)",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_server_citations.mjs" ]
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
		name: "check:server:long",
		description: "Run the Go long-tier tests (evidence sweeps); every release freeze runs it",
		kind: "check",
		ci: false,
		requires: [ "go" ],
		timeoutClass: "long",
		command: "node",
		// Go's test cache keeps unchanged packages free, so it always runs.
		args: [ "scripts/checks/check_go_server.mjs", "--long" ]
	} ),
	commandTask( {
		name: "check:server:changed",
		description: "Run CI's Go server checks on the packages this branch changes (pre-push; no heavy slot)",
		kind: "check",
		ci: false,
		requires: [ "go" ],
		timeoutClass: "long",
		command: "node",
		// Never stamps: a scoped pass is no evidence for the full gate.
		args: [ "scripts/checks/check_go_server.mjs", "--changed" ]
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
		name: "check:console",
		description: "Run the operator console and console sign-in tests",
		kind: "check",
		ci: true,
		requires: [],
		timeoutClass: "medium",
		command: "pnpm",
		args: [ "--filter", CONSOLE_PACKAGES[0], "--filter", CONSOLE_PACKAGES[1], "check" ]
	} ),
	commandTask( {
		name: "workspace:check",
		description: "Run each non-root workspace package check script",
		kind: "internal",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		// check:console already runs the console packages in the source gates.
		args: [
			"-r",
			"--filter=!sro-browser-rebuild",
			...CONSOLE_PACKAGES.map( ( name ) => "--filter=!" + name ),
			"check"
		]
	} )
];

const sourceTasks = [
	"check:release",
	"check:console",
	"check:loot-catalog",
	"check:compiled-quests",
	"check:pet-design",
	"check:client-preparation",
	"check:pipeline-contracts",
	"check:source-size",
	"check:source-encoding",
	"check:generated-root",
	"check:world-space",
	"check:plane-branches",
	"check:server-citations",
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
	// CI's Windows job: the Go gate runs once, on the faster Linux runner
	// (source-linux.yml), with a GOOS=windows vet for the Windows build.
	"source-portable": sourceTasks.filter( ( task ) => task !== "check:server" ).map( ( task ) => ({
		id: task.replaceAll( ":", "-" ),
		task,
		after: []
	}) ),
	tests: [
		{ id: "build-resources", task: "assets:build:full", after: [] },
		// The full build publishes every family too (families/looseFamilies.mjs),
		// so every reader of the published tree waits for it, never a half-built one.
		{ id: "server-game-data", task: "build:server-game-data", after: [ "build-resources" ] },
		{ id: "test-assets", task: "test:assets", after: [ "build-resources" ] },
		{ id: "test-cif", task: "test:cif", after: [ "build-resources" ] },
		{ id: "test-world", task: "test:world", after: [ "build-resources" ] },
		{ id: "test-region", task: "test:region", after: [ "build-resources" ] }
	],
	full: [
		{ id: "release", task: "check:release", after: [] },
		{ id: "loot-catalog", task: "check:loot-catalog", after: [] },
		{ id: "compiled-quests", task: "check:compiled-quests", after: [] },
		{ id: "pet-design", task: "check:pet-design", after: [] },
		{ id: "client-preparation", task: "check:client-preparation", after: [] },
		{ id: "pipeline-contracts", task: "check:pipeline-contracts", after: [] },
		{ id: "source-size", task: "check:source-size", after: [] },
		{ id: "source-encoding", task: "check:source-encoding", after: [] },
		{ id: "generated-root", task: "check:generated-root", after: [] },
		{ id: "world-space", task: "check:world-space", after: [] },
		{ id: "plane-branches", task: "check:plane-branches", after: [] },
		{ id: "server-citations", task: "check:server-citations", after: [] },
		{ id: "shared-fixtures", task: "check:shared-fixtures", after: [] },
		{ id: "format", task: "check:format", after: [] },
		{ id: "typecheck-scripts", task: "check:scripts", after: [] },
		{ id: "typecheck-scripts-tests", task: "check:scripts-tests", after: [] },
		{ id: "build-resources", task: "assets:build:full", after: [] },
		// The full build publishes every family too (families/looseFamilies.mjs),
		// so every reader of the published tree waits for it, never a half-built one.
		{ id: "server-game-data", task: "build:server-game-data", after: [ "build-resources" ] },
		// The Go tests and the package checks read the server projection.
		{ id: "server", task: "check:server", after: [ "server-game-data" ] },
		{ id: "typecheck-packages", task: "workspace:check", after: [ "server-game-data" ] },
		// The console's item test reads the observatory catalog that build writes.
		{ id: "console", task: "check:console", after: [ "server-game-data" ] },
		{ id: "test-assets", task: "test:assets", after: [ "build-resources" ] },
		{ id: "asset-pack-integrity", task: "assets:check:integrity", after: [ "build-resources" ] },
		{ id: "test-cif", task: "test:cif", after: [ "build-resources" ] },
		{ id: "test-world", task: "test:world", after: [ "build-resources" ] },
		{ id: "test-region", task: "test:region", after: [ "build-resources" ] }
	]
} );
