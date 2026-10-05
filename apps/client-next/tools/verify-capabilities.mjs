/*
===========================================================================

verify-capabilities.mjs - who may touch the GPU, the network, time and async

Walks the client sources and rejects any use of a guarded capability (GPU
device, command encoders, timers, network, workers, dynamic execution,
asynchronous code) outside the module that owns it. The owners are declared
here, in `rules` and ASYNC_OWNERS; adding one is a reviewed change.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { project, root, visit, ts, main } from "./project.mjs";
const runtime = "src/engine/runtime/";

// Modules whose job is asynchronous I/O. Everything else runs synchronously
// inside the frame or its owner's step.
const ASYNC_OWNERS = [
	runtime + "simulation/worker/session/http/http.ts",
	runtime + "assets/worker/loader.ts",
	"src/engine/foundation/assets/read-bytes.ts",
	runtime + "assets/worker/packs/packs.ts",
	runtime + "assets/worker/packs/blocks.ts",
	runtime + "assets/worker/packs/persistent.ts",
	runtime + "assets/worker/world/resources/resources.ts",
	runtime + "assets/worker/navigation/navigation.ts",
	// Background install of the combat presentation set; worker-only I/O.
	runtime + "assets/worker/install.ts",
	// The bug reporter: its settings and upload, the capture stream's
	// playback and the report window's send (issue #90).
	runtime + "bug-report/bug-report.ts",
	runtime + "bug-report/recorder.ts",
	runtime + "bug-report/dialog.ts",
	runtime + "bug-report/trimmer.ts",
	runtime + "bug-report/transcode.ts",
	runtime + "bug-report/archive.ts",
	runtime + "bug-report/journal.ts"
];
const device = runtime + "renderer/device/device.ts",
	frame = runtime + "renderer/frame/frame.ts",
	surface = runtime + "renderer/surface/surface.ts";
const pipelines = runtime + "renderer/device/pipelines.ts",
	images = runtime + "renderer/device/images.ts",
	geometry = runtime + "renderer/device/geometry.ts";
const ui = runtime + "renderer/device/ui.ts", uiBridge = runtime + "platform/ui/ui.ts";
const flares = runtime + "renderer/device/flares.ts";
const bugReport = runtime + "bug-report/bug-report.ts",
	onboarding = runtime + "onboarding/onboarding.ts",
	bugRecorder = runtime + "bug-report/recorder.ts",
	bugDialog = runtime + "bug-report/dialog.ts",
	bugTrimmer = runtime + "bug-report/trimmer.ts",
	bugTranscode = runtime + "bug-report/transcode.ts",
	bugJournal = runtime + "bug-report/journal.ts";
const thunder = runtime + "renderer/device/thunder.ts";
const timing = runtime + "renderer/device/timing.ts";
const bloom = runtime + "renderer/device/bloom.ts";
const shadows = runtime + "renderer/device/character-shadows.ts";
const animation = runtime + "renderer/device/animation.ts";
const particleQuery = runtime + "renderer/device/particle-query.ts";
const particles = runtime + "renderer/device/particles.ts";
const deviceInternals = [
	bloom,
	device,
	pipelines,
	images,
	geometry,
	ui,
	flares,
	thunder,
	timing,
	animation,
	particleQuery,
	particles,
	shadows
];
export const rules = {
	createQuerySet: [ timing ],
	resolveQuerySet: [ frame ],
	mapAsync: [ timing ],
	getMappedRange: [ timing ],
	Audio: [ runtime + "audio/music/music.ts" ],
	createElement: [ uiBridge, runtime + "platform/ui/cursor.ts", bugRecorder, bugDialog, bugTrimmer, onboarding ],
	// The picking mask readback, shared by the renderer and the asset worker.
	OffscreenCanvas: [ "src/engine/foundation/rendering/pick-alpha.ts", bugRecorder ],
	AudioContext: [ runtime + "audio/audio.ts" ],
	decodeAudioData: [ runtime + "audio/audio.ts" ],
	createPanner: [ runtime + "audio/audio.ts" ],
	createBufferSource: [ runtime + "audio/audio.ts" ],
	createGain: [ runtime + "audio/audio.ts" ],
	requestAnimationFrame: [ runtime + "runtime.ts" ],
	cancelAnimationFrame: [ runtime + "runtime.ts" ],
	// The asset loader owns one trailing progress timer, cleared on dispose.
	setTimeout: [ runtime + "simulation/worker/clock/clock.ts", runtime + "assets/worker/loader.ts" ],
	clearTimeout: [ runtime + "simulation/worker/clock/clock.ts", runtime + "assets/worker/loader.ts" ],
	setInterval: [],
	createRenderBundleEncoder: [ device ],
	createBundleEncoder: [ device, frame ],
	executeBundles: [ frame ],
	requestAdapter: [ device ],
	requestDevice: [ device ],
	createCommandEncoder: [ device ],
	createEncoder: [ frame, device ],
	beginRenderPass: [ frame, device ],
	beginComputePass: [ frame ],
	finish: [ frame, device ],
	submit: [ frame, device ],
	createImageBitmap: [ runtime + "assets/worker/loader.ts" ],
	createSampler: [ device ],
	getContext: [ surface, "src/engine/foundation/rendering/pick-alpha.ts", bugRecorder, bugTrimmer ],
	createBuffer: [ device ],
	createTexture: [ device ],
	createBindGroup: [ device ],
	createBindGroupLayout: [ device ],
	createPipelineLayout: [ device ],
	createShaderModule: [ device ],
	createRenderPipeline: [ device ],
	createRenderPipelineAsync: [ device ],
	createComputePipeline: [ device ],
	createComputePipelineAsync: [ device ],
	writeBuffer: [ device ],
	writeTexture: [ device ],
	copyExternalImageToTexture: [ device ],
	pushErrorScope: [ device ],
	popErrorScope: [ device ],
	getCurrentTexture: [ surface ],
	// The recorder and transcoder configure WebCodecs, not a canvas.
	configure: [ surface, device, bugRecorder, bugTranscode ],
	unconfigure: [ surface ],
	addEventListener: [
		runtime + "platform/platform.ts",
		device,
		uiBridge,
		runtime + "platform/ui/cursor.ts",
		bugReport,
		bugDialog,
		bugTrimmer,
		bugTranscode,
		bugJournal,
		onboarding
	],
	fetch: [
		runtime + "simulation/worker/session/http/http.ts",
		runtime + "assets/worker/loader.ts",
		bugReport,
		// The Agent's one GET /title/onboarding: whether the tour runs.
		onboarding
	],
	WebSocket: [ runtime + "simulation/worker/network/network.ts" ],
	WebTransport: [],
	Worker: [ runtime + "simulation/host.ts", runtime + "assets/assets.ts" ]
};
// A geometry pipeline for a native state no scene precompiles is compiled
// at its first upload, as D3D9 applies any render state at once.
for ( const name of [ "createShaderModule", "createRenderPipeline", "createRenderPipelineAsync", "createSampler" ] ) {
	rules[name].push( pipelines );
}
for (
	const name of [
		"createTexture",
		"createBindGroup",
		"writeTexture",
		"copyExternalImageToTexture",
		"pushErrorScope",
		"popErrorScope"
	]
) rules[name].push( images );
for (
	const name of [
		"createBuffer",
		"createTexture",
		"createBindGroup",
		"writeBuffer",
		"writeTexture",
		"pushErrorScope",
		"popErrorScope"
	]
) rules[name].push( geometry );
for (
	const name of [
		"createShaderModule",
		"createRenderPipelineAsync",
		"createBindGroupLayout",
		"createPipelineLayout",
		"createBuffer",
		"createTexture",
		"createSampler",
		"createBindGroup",
		"writeBuffer",
		"writeTexture",
		"copyExternalImageToTexture"
	]
) rules[name].push( ui );
for (
	const name of [
		"createShaderModule",
		"createComputePipelineAsync",
		"createRenderPipelineAsync",
		"createBuffer",
		"createSampler",
		"createBindGroup",
		"createBindGroupLayout",
		"createPipelineLayout",
		"writeBuffer"
	]
) rules[name].push( flares );
for (
	const name of [
		"createShaderModule",
		"createRenderPipelineAsync",
		"createBuffer",
		"createBindGroup",
		"writeBuffer"
	]
) rules[name].push( thunder );
for (
	const name of [
		"createShaderModule",
		"createComputePipelineAsync",
		"createBuffer",
		"createBindGroup",
		"writeBuffer",
		"beginComputePass"
	]
) rules[name].push( animation );
// The particle presentation pass, like skinning, is encoded by geometry
// preparation on the frame encoder into the draws' own buffers.
for (
	const name of [
		"createShaderModule",
		"createComputePipelineAsync",
		"createBuffer",
		"createBindGroup",
		"writeBuffer",
		"beginComputePass"
	]
) rules[name].push( particles );
// Geometry preparation encodes device-owned skinning and shadow prepasses on
// the frame encoder; it cannot create, submit, or finish an encoder.
for (
	const name of [
		"createShaderModule",
		"createRenderPipeline",
		"createSampler",
		"createBuffer",
		"createTexture",
		"createBindGroup",
		"writeBuffer",
		"beginRenderPass"
	]
) rules[name].push( shadows );
for (
	const name of [
		"createShaderModule",
		"createRenderPipeline",
		"createSampler",
		"createTexture",
		"createBindGroup",
		"createBindGroupLayout",
		"createPipelineLayout",
		"beginRenderPass"
	]
) rules[name].push( bloom );
rules.createBuffer.push( timing );
for (
	const name of [
		"createShaderModule",
		"createRenderPipelineAsync",
		"createBuffer",
		"createQuerySet",
		"createBindGroup",
		"writeBuffer",
		"createCommandEncoder",
		"beginRenderPass",
		"resolveQuerySet",
		"submit",
		"finish",
		"mapAsync",
		"getMappedRange"
	]
) rules[name].push( particleQuery );
/*
================
verifyCapabilities
================
*/
export function verifyCapabilities( base = root ) {
	const model = project( base ), issues = [];
	const contractPath = path.join( base, "execution-contract.json" ),
		barriers = fs.existsSync( contractPath ) ?
			JSON.parse( fs.readFileSync( contractPath, "utf8" ) ).frameBarriers ?? [] :
			[];
	/*
	================
	nativeBarrier
	================
	*/
	function nativeBarrier( file, n ) {
		let fn = n;
		while ( fn && !ts.isFunctionLike( fn ) ) fn = fn.parent;
		return barriers.some( b => b.file === file && b.function === fn?.name?.getText() );
	}
	for ( const [file, source] of model.files ) {
		visit( source, n => {
			const neutral = file.startsWith( "src/engine/contracts/" ) || file.startsWith( "src/engine/foundation/" );
			if ( neutral && ts.isIdentifier( n ) && /^GPU[A-Z]/.test( n.text ) ) {
				issues.push( `${file}: backend GPU type in renderer-neutral contract` );
			}
			if (
				ts.isElementAccessExpression( n ) && ts.isIdentifier( n.expression ) &&
				[ "globalThis", "window", "self", "navigator" ].includes( n.expression.text ) &&
				!ts.isStringLiteralLike( n.argumentExpression )
			) {
				issues.push( `${file}: computed global capability access forbidden` );
			}
			if (
				ts.isPropertyAccessExpression( n ) && ts.isIdentifier( n.expression ) &&
				n.expression.text === "navigator" && n.name.text === "gpu" && file !== device
			) {
				issues.push( `${file}: navigator.gpu belongs to the device owner` );
			}
			// Reject captured aliases and destructuring as well as direct calls.
			if (
				(ts.isIdentifier( n ) || ts.isStringLiteralLike( n )) && Object.hasOwn( rules, n.text ) &&
				!rules[n.text].includes( file )
			) {
				const parent = n.parent;
				const capabilityReference =
					ts.isIdentifier( n ) && !ts.isPropertySignature( parent ) && !ts.isMethodSignature( parent ) ||
					ts.isStringLiteralLike( n ) && ts.isElementAccessExpression( parent ) &&
						parent.argumentExpression === n;
				if ( capabilityReference && !file.endsWith( "/internal/gpu-contract.ts" ) ) {
					issues.push( `${file}: reference to ${n.text} escapes its owner` );
				}
			}
			if (
				ts.isIdentifier( n ) && [ "GPUDevice", "GPUQueue", "GPUAdapter" ].includes( n.text ) &&
				!(n.text === "GPUDevice" ? deviceInternals.includes( file ) : file === device)
			) {
				issues.push( `${file}: raw ${n.text} outside device owner` );
			}
			if (
				ts.isIdentifier( n ) && [ "GPUCanvasContext" ].includes( n.text ) &&
				![ device, surface, runtime + "renderer/internal/gpu-contract.ts" ].includes( file )
			) {
				issues.push( `${file}: raw canvas context outside surface capability` );
			}
			if (
				ts.isIdentifier( n ) && [ "GPUCommandEncoder", "GPURenderBundleEncoder" ].includes( n.text ) &&
				!([ animation, particles, geometry, shadows ].includes( file ) && n.text === "GPUCommandEncoder") &&
				![ frame, runtime + "renderer/internal/gpu-contract.ts" ].includes( file )
			) {
				issues.push( `${file}: raw command encoder outside frame capability` );
			}
			if (
				!nativeBarrier( file, n ) &&
				!ASYNC_OWNERS.includes( file ) &&
				(ts.isAwaitExpression( n ) ||
					ts.isFunctionLike( n ) && n.modifiers?.some( m => m.kind === ts.SyntaxKind.AsyncKeyword ))
			) {
				issues.push( `${file}: asynchronous execution requires an explicit owner contract` );
			}
			if ( ts.isIdentifier( n ) && [ "eval", "Function" ].includes( n.text ) ) {
				issues.push( `${file}: dynamic execution forbidden` );
			}
			if ( ts.isCallExpression( n ) || ts.isNewExpression( n ) ) {
				const e = n.expression;
				const name = ts.isIdentifier( e ) ?
					e.text :
					ts.isPropertyAccessExpression( e ) ?
					e.name.text :
					ts.isElementAccessExpression( e ) && ts.isStringLiteralLike( e.argumentExpression ) ?
					e.argumentExpression.text :
					null;
				if ( name && Object.hasOwn( rules, name ) && !rules[name].includes( file ) ) {
					issues.push( `${file}: ${name} belongs to a different owner` );
				}
			}
		} );
	}
	return issues;
}
if ( process.argv[1] === import.meta.filename ) {
	main( () => verifyCapabilities() );
}
