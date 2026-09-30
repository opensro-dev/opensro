/*
===========================================================================

build.mjs - build tasks

Resource and workspace builds, the server game-data projection (with the
dashboard item catalog that reads it) and the release workspace.

===========================================================================
*/
import { commandTask, seriesTask } from "./define.mjs";

export const BUILD_TASKS = [
	seriesTask( {
		name: "build",
		description: "Build resources, then every workspace package",
		kind: "build",
		ci: true,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		tasks: [ "assets:build", "workspace:build" ]
	} ),
	commandTask( {
		name: "workspace:build",
		description: "Build every non-root workspace package",
		kind: "internal",
		ci: true,
		requires: [],
		timeoutClass: "long",
		command: "pnpm",
		args: [ "-r", "--filter=!sro-browser-rebuild", "build" ]
	} ),
	seriesTask( {
		name: "build:server-game-data",
		description: "Build the server game-data bundle, then the dashboard item catalog read from it",
		kind: "build",
		ci: false,
		requires: [ "server-data" ],
		timeoutClass: "long",
		tasks: [ "build:server-game-data:bundle", "build:observatory-items" ]
	} ),
	commandTask( {
		name: "build:server-game-data:bundle",
		description: "Build the server game-data projection and bundle",
		kind: "internal",
		ci: false,
		requires: [ "server-data" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/build/server/buildServerGameDataBundle.mjs" ]
	} ),
	commandTask( {
		name: "build:observatory-items",
		description: "Export the server item references for the operations dashboard",
		kind: "internal",
		ci: false,
		requires: [ "server-data" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "apps/server-observatory/tools/item-catalog.mjs" ]
	} ),
	commandTask( {
		name: "release",
		description: "Prepare a clean release workspace using the repository release tool",
		kind: "release",
		ci: false,
		requires: [ "release-workspace" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/clean_release_workspace.mjs" ]
	} )
];
