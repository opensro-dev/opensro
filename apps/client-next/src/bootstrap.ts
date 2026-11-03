/*
===========================================================================

bootstrap.ts - page entry: canvas to live runtime

Finds the runtime surfaces in the document and starts the engine. The
runtime is not published on window: page scripts and extensions get no
handle on it in any build. Everything else the client does is owned by
the modules under engine/.

===========================================================================
*/

import { requestPersistentStorage } from "./engine/foundation/assets/persistent-storage";
import { startRuntime, startViewerRuntime } from "./engine/runtime/runtime";

/*
================
diagnosticsFromQuery

Release builds pin the diagnostics profile; every other build reads it
from the page URL so a local run can flip probes without a rebuild.
================
*/
function diagnosticsFromQuery() {
	if ( import.meta.env.MODE === "beta" ) {
		return { gpuAnimation: true, stages: false, gpuTiming: false, hoverPicking: true };
	}
	const query = new URLSearchParams( location.search );
	return {
		gpuAnimation: query.get( "gpu-animation" ) !== "0",
		stages: query.get( "frame-stages" ) === "1",
		gpuTiming: query.get( "gpu-timing" ) === "1",
		hoverPicking: query.get( "hover-picking" ) !== "0"
	};
}

/*
================
startGame
================
*/
function startGame() {
	const canvas = document.querySelector( "canvas" );
	const status = document.querySelector( "output" );
	if ( !(canvas instanceof HTMLCanvasElement) || !status ) {
		throw new Error( "Missing runtime surface" );
	}
	const started = startRuntime( canvas, status, undefined, diagnosticsFromQuery() );
	// Keep the verified game files through disk pressure, asked once per page
	// load (persistent-storage.ts); the runtime's frame never runs async work.
	void requestPersistentStorage();
	return started;
}

/*
================
startViewer

viewer.html (docs/guides/VIEWER.md) marks its root data-page="viewer": the same
entry starts the 3D viewer instead of the game. Port-only, not native.
================
*/
function startViewer() {
	const canvas = document.querySelector( "canvas[data-viewer=render]" ),
		picture = document.querySelector( "canvas[data-viewer=picture]" );
	if ( !(canvas instanceof HTMLCanvasElement) || !(picture instanceof HTMLCanvasElement) ) {
		throw new Error( "Missing viewer canvas" );
	}
	startViewerRuntime( canvas, picture, location.search );
	return null;
}

export const runtime = document.documentElement.dataset.page === "viewer" ? startViewer() : startGame();
