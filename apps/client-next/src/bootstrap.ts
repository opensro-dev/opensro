/*
===========================================================================

bootstrap.ts - page entry: canvas to live runtime, plus the page bridge

Finds the runtime surfaces in the document, starts the engine and exposes
the one supported page-side control surface. Everything else the client
does is owned by the modules under engine/.

===========================================================================
*/

import { startRuntime } from "./engine/runtime/runtime";

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
		hoverPicking: query.get( "hover-picking" ) !== "0",
		postProcessing: query.get( "post-processing" ) !== "0"
	};
}

const canvas = document.querySelector( "canvas" );
const status = document.querySelector( "output" );
if ( !(canvas instanceof HTMLCanvasElement) || !status ) {
	throw new Error( "Missing runtime surface" );
}
export const runtime = startRuntime( canvas, status, undefined, diagnosticsFromQuery() );

/*
================
__sroRuntime

The shipped page bridge: the hunt-bot extension (apps/bot-extension)
drives the game through this surface. The module export above serves
dev-time imports; bundlers drop an entry export nothing imports, so the
global is the one path that survives release builds. Not a diagnostic
global - the release policy's forbidden list (tools/beta/policy.mjs)
names those separately.
================
*/
declare global {
	interface Window {
		__sroRuntime?: typeof runtime;
	}
}
window.__sroRuntime = runtime;
