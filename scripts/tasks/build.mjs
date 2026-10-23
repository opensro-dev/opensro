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
	commandTask( {
		name: "build:server-game-data",
		description: "Build the server game-data projection and bundle",
		kind: "build",
		ci: false,
		requires: [ "server-data" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/build/server/buildServerGameDataBundle.mjs" ]
	} ),
	commandTask( {
		name: "build:catalogue",
		description: "Export the website's game catalogue (items, skills, quests, monsters, NPCs, atlas)",
		kind: "build",
		ci: false,
		requires: [ "server-data", "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/build/catalogue/buildCatalogue.mjs" ]
	} )
];
