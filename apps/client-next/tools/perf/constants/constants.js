/*
===========================================================================

constants.js - what one WebGPU call costs on this browser and machine

A standalone page (index.html beside it, the cases in cases.js) that
measures the fixed cost of the calls the client's crowd frame is made
of: draws with and without a state change, indirect draws and empty
indirect slots, render bundles, small queue writes and compute
dispatches (#3563). It needs no game assets, no build step and no
server code, so a contributor on any GPU backend can open it from a
static file server and paste the result.

Every case runs at several sizes N. One frame encodes and submits the
case's N calls into a tiny offscreen target, then awaits
onSubmittedWorkDone; frames run back to back, not paced by
requestAnimationFrame. Two times are kept per frame: the main thread's
encode plus submit, and the whole loop. performance.now() is coarse
(about 0.1 ms) in a page that is not cross-origin isolated (serve.mjs
isolates it), so each repeat divides a total over K frames. The cost per
call is the slope of a least-squares line through the per-size medians.

Results go to the table, to window.__gpuConstants and to the Copy JSON
button. Query parameters: quick=1, sizes=250,500, frames=K, repeats=R,
warmup=W, only=D1,U3 and nofeature=1 (do not request
indirect-first-instance even when the adapter offers it).

===========================================================================
*/
import { caseList, createCaseResources } from "./cases.js";

const DEFAULT_SIZES = [ 250, 500, 1000, 2000, 4000 ];
const DEFAULT_FRAMES = 100;
const DEFAULT_REPEATS = 7;
const DEFAULT_WARMUP = 5;
const QUICK_SIZES = [ 250, 1000, 4000 ];
const QUICK_FRAMES = 10;
const QUICK_REPEATS = 3;
const QUICK_WARMUP = 2;
// maxComputeWorkgroupsPerDimension's guaranteed minimum bounds C2's N.
const MAX_SIZE = 65535;

// timerResolution watches this many clock steps, reading the clock at most
// TIMER_SAMPLE_LIMIT times.
const TIMER_TICKS = 50;
const TIMER_SAMPLE_LIMIT = 50000000;
const FIRST_INSTANCE_FEATURE = "indirect-first-instance";

const NOTES = [
	"Slopes are microseconds per unit of N: per call for D1 to D4, D6, D7, U1, U2 and C1; per copy for U4 and U5 " +
	"(their one staging write of N x 256 bytes included); per instance for D5, per 256 bytes for U3 and per " +
	"workgroup for C2.",
	"main = the main thread's encode plus submit; loop = the whole frame, including the wait for " +
	"onSubmittedWorkDone. loop minus main estimates what the GPU process and the GPU add per call. It is an " +
	"estimate: the GPU process works while the main thread is still encoding, so part of its cost hides " +
	"inside main.",
	"R squared means little when the slope is close to zero (D5, U3, C2, and main for bundles): the line is " +
	"then flat and the residue is timer noise."
];

// ============================================================================

/*
================
readParams

The page's parameters from its query string. quick=1 picks fewer sizes
and frames for a smoke run; the explicit parameters override either.
================
*/
function readParams( search ) {
	const query = new URLSearchParams( search );
	const quick = query.get( "quick" ) === "1";
	const sizes = readList( query.get( "sizes" ) ) ?? (quick ? QUICK_SIZES : DEFAULT_SIZES);
	const only = query.get( "only" );
	return {
		quick,
		sizes: [ ...new Set( sizes ) ].sort( ( a, b ) => a - b ),
		frames: readCount( query.get( "frames" ) ) ?? (quick ? QUICK_FRAMES : DEFAULT_FRAMES),
		repeats: readCount( query.get( "repeats" ) ) ?? (quick ? QUICK_REPEATS : DEFAULT_REPEATS),
		warmup: readCount( query.get( "warmup" ) ) ?? (quick ? QUICK_WARMUP : DEFAULT_WARMUP),
		only: only ? only.split( "," ).map( id => id.trim().toUpperCase() ) : null,
		requestFirstInstance: query.get( "nofeature" ) !== "1"
	};
}

/*
================
readCount
================
*/
function readCount( text ) {
	if ( text === null ) return null;
	const value = Number( text );
	if ( !Number.isInteger( value ) || value < 1 ) throw Error( `not a positive integer: ${text}` );
	return value;
}

/*
================
readList
================
*/
function readList( text ) {
	if ( text === null ) return null;
	return text.split( "," ).map( readCount );
}

/*
================
checkParams

A fit needs two sizes; C2 dispatches N workgroups in one dimension.
================
*/
function checkParams( params ) {
	if ( params.sizes.length < 2 ) throw Error( "sizes needs at least two different values" );
	const largest = params.sizes[params.sizes.length - 1];
	if ( largest > MAX_SIZE ) throw Error( `sizes above ${MAX_SIZE} are not supported` );
}

// ============================================================================

/*
================
median
================
*/
function median( values ) {
	const sorted = [ ...values ].sort( ( a, b ) => a - b );
	const middle = sorted.length >> 1;
	return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

/*
================
fitLine

Ordinary least squares of ys against xs. r2 is null when ys do not vary
at all (every time rounded to the same value): no line explains nothing.
================
*/
function fitLine( xs, ys ) {
	const count = xs.length;
	let meanX = 0, meanY = 0;
	for ( let i = 0; i < count; i++ ) {
		meanX += xs[i] / count;
		meanY += ys[i] / count;
	}
	let sxx = 0, sxy = 0, syy = 0;
	for ( let i = 0; i < count; i++ ) {
		const dx = xs[i] - meanX, dy = ys[i] - meanY;
		sxx += dx * dx;
		sxy += dx * dy;
		syy += dy * dy;
	}
	const slope = sxy / sxx;
	return {
		slope,
		intercept: meanY - slope * meanX,
		r2: syy > 0 ? (sxy * sxy) / (sxx * syy) : null
	};
}

/*
================
timerResolution

The smallest step performance.now() takes on this page: about 0.1 ms
without cross-origin isolation, a few microseconds with it.
================
*/
function timerResolution() {
	let smallest = Infinity, last = performance.now(), ticks = 0;
	for ( let i = 0; i < TIMER_SAMPLE_LIMIT && ticks < TIMER_TICKS; i++ ) {
		const now = performance.now();
		if ( now > last ) {
			smallest = Math.min( smallest, now - last );
			last = now;
			ticks++;
		}
	}
	return Number.isFinite( smallest ) ? smallest : null;
}

// ============================================================================

/*
================
timeFrames

frames back to back: encode and submit, then wait for the GPU. Returns
the main thread's encode plus submit and the whole loop, each per frame,
from totals over the frames.
================
*/
async function timeFrames( device, prepared, frames ) {
	let mainMs = 0;
	const start = performance.now();
	for ( let f = 0; f < frames; f++ ) {
		const before = performance.now();
		prepared.run();
		mainMs += performance.now() - before;
		await device.queue.onSubmittedWorkDone();
	}
	return { mainMs: mainMs / frames, loopMs: (performance.now() - start) / frames };
}

/*
================
checked

Runs work inside a validation error scope and throws the first error, so
a case that WebGPU refuses fails by name instead of timing nothing.
================
*/
async function checked( device, work ) {
	device.pushErrorScope( "validation" );
	let value;
	try {
		value = await work();
	} finally {
		const error = await device.popErrorScope();
		if ( error ) throw Error( error.message );
	}
	return value;
}

/*
================
yieldToPage

Lets the page paint the progress line between sizes.
================
*/
function yieldToPage() {
	return new Promise( resolve => setTimeout( resolve, 0 ) );
}

/*
================
measureCase

Every size of one case. All sizes are prepared first, then each repeat
walks every size in turn (with its warm-up frames) so that a slow drift
of the machine spreads over the sizes instead of tilting the slope.
================
*/
async function measureCase( gpu, spec, params, report ) {
	const { device } = gpu;
	const prepared = [];
	try {
		for ( const count of params.sizes ) {
			prepared.push(
				await checked( device, async () => {
					const one = spec.prepare( gpu, spec, count );
					await timeFrames( device, one, 1 );
					return one;
				} )
			);
		}
		const runs = params.sizes.map( () => ({ mainMs: [], loopMs: [] }) );
		for ( let repeat = 0; repeat < params.repeats; repeat++ ) {
			for ( let s = 0; s < params.sizes.length; s++ ) {
				report( `repeat ${repeat + 1} of ${params.repeats}, N = ${params.sizes[s]}` );
				await yieldToPage();
				await timeFrames( device, prepared[s], params.warmup );
				const timed = await timeFrames( device, prepared[s], params.frames );
				runs[s].mainMs.push( timed.mainMs );
				runs[s].loopMs.push( timed.loopMs );
			}
		}
		return summarizeCase( spec, params.sizes, runs );
	} finally {
		for ( const one of prepared ) one.release();
	}
}

/*
================
summarizeFit
================
*/
function summarizeFit( fit ) {
	return { usPerUnit: fit.slope * 1000, interceptMs: fit.intercept, r2: fit.r2 };
}

/*
================
summarizeCase

The per-size medians and the two fits of one case.
================
*/
function summarizeCase( spec, sizes, runs ) {
	const mainMs = runs.map( run => median( run.mainMs ) );
	const loopMs = runs.map( run => median( run.loopMs ) );
	const main = summarizeFit( fitLine( sizes, mainMs ) );
	const loop = summarizeFit( fitLine( sizes, loopMs ) );
	return {
		id: spec.id,
		form: spec.form,
		per: spec.per,
		what: spec.what,
		sizes,
		mainMs,
		loopMs,
		repeats: runs,
		main,
		loop,
		gpuSideUsPerUnit: loop.usPerUnit - main.usPerUnit
	};
}

// ============================================================================

/*
================
openDevice

The default adapter, as the client asks for it, with
indirect-first-instance when offered and not refused by nofeature=1.
================
*/
async function openDevice( params ) {
	const adapter = await navigator.gpu.requestAdapter();
	if ( !adapter ) return null;
	const offered = adapter.features.has( FIRST_INSTANCE_FEATURE );
	const firstInstance = offered && params.requestFirstInstance;
	const device = await adapter.requestDevice( { requiredFeatures: firstInstance ? [ FIRST_INSTANCE_FEATURE ] : [] } );
	return { adapter, device, offered, firstInstance };
}

/*
================
adapterFacts

GPUAdapterInfo keeps its fields on the prototype, so JSON.stringify sees
none of them; copy them out.
================
*/
function adapterFacts( adapter ) {
	const info = adapter.info ?? {};
	return {
		vendor: info.vendor ?? "",
		architecture: info.architecture ?? "",
		device: info.device ?? "",
		description: info.description ?? "",
		isFallbackAdapter: info.isFallbackAdapter ?? adapter.isFallbackAdapter ?? null
	};
}

/*
================
runAll

Opens the device, measures every selected case and fills the result as it
goes. A case that fails records its error and the run goes on.
================
*/
async function runAll( result, params, view ) {
	if ( !navigator.gpu ) {
		finish( result, "unsupported", "WebGPU is not available in this browser.", view );
		return;
	}
	const opened = await openDevice( params );
	if ( !opened ) {
		finish( result, "unsupported", "WebGPU is available, but the browser offered no adapter.", view );
		return;
	}
	const { adapter, device, offered, firstInstance } = opened;
	let lost = null;
	device.lost.then( info => {
		lost = info.message || info.reason;
	} );
	result.adapter = adapterFacts( adapter );
	result.features = {
		used: [ ...device.features ].sort(),
		indirectFirstInstance: { offered, requested: params.requestFirstInstance, used: firstInstance }
	};
	if ( !firstInstance ) {
		result.notes.push(
			`${FIRST_INSTANCE_FEATURE} is ${offered ? "not requested (nofeature=1)" : "not offered by the adapter"}: ` +
				"D3 and D4 records use firstInstance 0 (D1, D2, D6 and D7 still pass firstInstance i)."
		);
	}
	const largest = params.sizes[params.sizes.length - 1];
	const cases = caseList().filter( spec => !params.only || params.only.includes( spec.id ) );
	const gpu = createCaseResources( device, largest, firstInstance );
	for ( let c = 0; c < cases.length; c++ ) {
		const spec = cases[c];
		const label = `${spec.id} ${spec.form} (${c + 1} of ${cases.length})`;
		try {
			const measured = await measureCase( gpu, spec, params, text => view.progress( `${label}: ${text}` ) );
			result.cases.push( measured );
		} catch ( error ) {
			result.cases.push( {
				id: spec.id,
				form: spec.form,
				per: spec.per,
				what: spec.what,
				error: String( error )
			} );
		}
		if ( lost !== null ) {
			finish( result, "error", `The GPU device was lost: ${lost}`, view );
			return;
		}
		view.table( result );
	}
	device.destroy();
	finish( result, "done", `Done: ${cases.length} cases.`, view );
}

/*
================
finish
================
*/
function finish( result, state, message, view ) {
	result.state = state;
	result.message = message;
	view.progress( message );
	view.table( result );
}

// ============================================================================

/*
================
formatNumber
================
*/
function formatNumber( value, digits ) {
	return value === null || value === undefined || !Number.isFinite( value ) ? "-" : value.toFixed( digits );
}

/*
================
createView

The page's DOM: the progress line, the result table and the JSON.
================
*/
function createView( document ) {
	const progressLine = document.getElementById( "progress" );
	const body = document.getElementById( "results" );
	const json = document.getElementById( "json" );
	const notes = document.getElementById( "notes" );
	return {
		progress( text ) {
			progressLine.textContent = text;
		},
		table( result ) {
			body.replaceChildren();
			for ( const row of result.cases ) {
				const largest = row.sizes ? row.sizes.length - 1 : -1;
				const cells = row.error ?
					[ row.id, row.form, row.per, row.what, `failed: ${row.error}` ] :
					[
						row.id,
						row.form,
						row.per,
						row.what,
						formatNumber( row.main.usPerUnit, 3 ),
						formatNumber( row.main.r2, 3 ),
						formatNumber( row.loop.usPerUnit, 3 ),
						formatNumber( row.loop.r2, 3 ),
						formatNumber( row.gpuSideUsPerUnit, 3 ),
						formatNumber( row.mainMs[largest], 3 ),
						formatNumber( row.loopMs[largest], 3 )
					];
				const tr = document.createElement( "tr" );
				for ( const text of cells ) {
					const td = document.createElement( "td" );
					td.textContent = text;
					tr.append( td );
				}
				if ( row.error ) tr.lastChild.colSpan = 7;
				body.append( tr );
			}
			notes.replaceChildren( ...result.notes.map( text => {
				const li = document.createElement( "li" );
				li.textContent = text;
				return li;
			} ) );
			json.textContent = JSON.stringify( result, null, "\t" );
		}
	};
}

/*
================
main
================
*/
async function main() {
	const view = createView( document );
	const result = {
		schema: 1,
		state: "running",
		message: "",
		page: location.pathname,
		userAgent: navigator.userAgent,
		crossOriginIsolated: globalThis.crossOriginIsolated === true,
		timerResolutionMs: timerResolution(),
		params: null,
		adapter: null,
		features: null,
		notes: [ ...NOTES ],
		cases: []
	};
	if ( !result.crossOriginIsolated ) {
		result.notes.push(
			"The page is not cross-origin isolated, so the timer steps in about 0.1 ms and small main-thread " +
				"slopes are mostly rounding. Serve it with tools/perf/constants/serve.mjs for a finer timer."
		);
	}
	globalThis.__gpuConstants = result;
	document.getElementById( "copy" ).addEventListener( "click", async () => {
		try {
			await navigator.clipboard.writeText( JSON.stringify( result, null, "\t" ) );
			view.progress( `${result.message || "Running"} (JSON copied)` );
		} catch ( error ) {
			view.progress( `Copy failed (${error}); select the JSON below instead.` );
		}
	} );
	try {
		const params = readParams( location.search );
		checkParams( params );
		result.params = params;
		document.getElementById( "sizes" ).textContent = params.sizes.join( ", " );
		await runAll( result, params, view );
	} catch ( error ) {
		finish( result, "error", `Failed: ${error}`, view );
	}
}

main();
