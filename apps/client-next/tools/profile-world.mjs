import { acquirePerformanceLease } from "./lib/performance-lease.mjs";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { cpus, totalmem, platform, release as osRelease } from "node:os";
import { build } from "esbuild";
import { launchProbeBrowser } from "../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../scripts/lib/probeSession.mjs";
import { resolveProbeCharacter } from "../../../scripts/lib/probeCharacter.mjs";
import { startChromeTraceCapture } from "../../../scripts/lib/chromeTraceCapture.mjs";
import { holdProbeRuntime } from "../tests/browser/helpers/hold-runtime.mjs";
import { root } from "./project.mjs";
import { probeCharacterFacing } from "./lib/character-facing-probe.mjs";
import { buildReleaseProfile } from "./lib/release-profile.mjs";
import { probeCombat } from "./lib/combat-probe.mjs";
import { captureFrameWindow } from "./lib/frame-window.mjs";
import { createUiProductProbe } from "./lib/ui-product-probe.mjs";
import { createAnimationCeiling } from "./lib/animation-ceiling.mjs";
import { createFrameProfiler } from "./lib/frame-profiler.mjs";
import { createAnimationPhaseProfiler } from "./lib/animation-phase-profiler.mjs";

const character = resolveProbeCharacter( { argvIndex: 2, context: "client-next world performance" } );
const durationArg = process.argv.find( v => v.startsWith( "--duration-ms=" ) ),
	runsArg = process.argv.find( v => v.startsWith( "--warm-runs=" ) );
const durationMs = durationArg ? Number( durationArg.split( "=" )[1] ) : 6000,
	warmRuns = runsArg ? Number( runsArg.split( "=" )[1] ) : 1;
if (
	!Number.isInteger( durationMs ) || durationMs < 1000 || durationMs > 60000 || !Number.isInteger( warmRuns ) ||
	warmRuns < 1 || warmRuns > 10
) throw Error( "Invalid profiling duration/run count" );
const release = process.argv.includes( "--release" );
const metalComparison = process.argv.includes( "--metal-comparison" );
if ( metalComparison && !release ) throw Error( "Metal comparison requires frozen release" );
if ( process.argv.includes( "--status-notices" ) && (!release || !process.argv.includes( "--ui-products" )) ) {
	throw Error( "Status notice probe requires --release --ui-products" );
}
if (
	process.argv.includes( "--animation-ceiling" ) && !release ||
	process.argv.includes( "--freeze-animation" ) && !process.argv.includes( "--animation-ceiling" )
) throw Error( "Animation ceiling requires frozen release; freeze requires --animation-ceiling" );
if (
	process.argv.includes( "--combined-ceiling" ) &&
	(!process.argv.includes( "--animation-ceiling" ) || !process.argv.includes( "--frame-census" ) ||
		process.argv.includes( "--freeze-animation" ) || process.argv.includes( "--alternate-animation" ))
) throw Error( "Combined ceiling requires animation ceiling and frame census, without other modes" );
if (
	process.argv.includes( "--alternate-animation" ) &&
	(!process.argv.includes( "--animation-ceiling" ) || !process.argv.includes( "--frame-census" ) ||
		process.argv.includes( "--freeze-animation" ))
) throw Error( "Alternating animation requires ceiling and frame census, without permanent freeze" );
if ( process.argv.includes( "--ui-products" ) && !release ) {
	throw Error( "UI product capture requires a frozen release" );
}
if ( process.argv.includes( "--camera-panels" ) && !release ) {
	throw Error( "Camera panel comparison requires frozen release observers" );
}
if ( process.argv.includes( "--ui-windows" ) && !release ) {
	throw Error( "UI window capture requires the frozen release UI observer" );
}
if ( process.argv.includes( "--animation-phases" ) && !release ) {
	throw Error( "Animation phases require a frozen release" );
}
if ( process.argv.includes( "--frame-census" ) && !release ) {
	throw Error( "Frame census requires the frozen release instrumentation" );
}
const movementRuns = Number( process.argv.find( v => v.startsWith( "--movement-runs=" ) )?.split( "=" )[1] ?? 1 );
if ( !Number.isInteger( movementRuns ) || movementRuns < 1 || movementRuns > 10 ) {
	throw Error( "Invalid movement run count" );
}
if (
	release &&
	[ "--combat", "--facing", "--allocation-census", "--workload-census", "--pose-census", "--submission-census" ].some(
		flag => process.argv.includes( flag )
	)
) {
	throw Error(
		"This release probe supports camera, trace, GPU and worker timing; other observation modes require explicit release adapters"
	);
}
const sourceArchive = {};
let releaseSources;

const directory = `${root}/temp/artifacts/world-profile-${new Date().toISOString().replace( /[:.]/g, "-" )}`;
await mkdir( directory, { recursive: true } );
const viewport = { width: 1219, height: 953 },
	diagnostics = process.argv.includes( "--diagnostics" ),
	poseCensus = process.argv.includes( "--pose-census" ),
	report = {
		character,
		viewport,
		diagnostics,
		poseCensus,
		durationMs,
		warmRuns,
		phases: [],
		windows: [],
		errors: [],
		sources: {}
	};
const captureLease = await acquirePerformanceLease();
report.captureIsolation = {
	leasePort: captureLease.port,
	legacyProcessCheck: platform() === "win32",
	scope: "Exclusive profile-world probes; unrelated host applications are not controlled"
};
let launched;
try {
	launched = await launchProbeBrowser( { viewport, headed: process.argv.includes( "--headed" ) } );
} catch ( error ) {
	await captureLease.release();
	throw error;
}
const { browser, page } = launched;
if ( process.argv.includes( "--frame-census" ) ) {
	await page.addInitScript( {
		content: `globalThis.__worldProbeFrameProfiler=(${createFrameProfiler.toString()})();`
	} );
}
if ( process.argv.includes( "--animation-phases" ) ) {
	await page.addInitScript( {
		content: `globalThis.__worldProbeAnimationPhases=(${createAnimationPhaseProfiler.toString()})();`
	} );
}
// Pick census: world.ts reports each exact pick test through its frame probe
// (pickCensus); this sink aggregates the rows per world group.
if ( process.argv.includes( "--pick-census" ) ) {
	await page.addInitScript( () => {
		globalThis.__worldProbePickCensus = row => {
			const rows = globalThis.__worldProbePickRows ??= {};
			const entry = rows[row.group] ??= {
				calls: 0,
				ms: 0,
				triangles: 0,
				vertices: row.vertices,
				ranges: row.ranges,
				skinned: row.skinned
			};
			entry.calls++;
			entry.ms += row.ms;
			entry.triangles += row.triangles;
		};
	} );
}
if ( process.argv.includes( "--ui-products" ) ) {
	await page.addInitScript( {
		content: `globalThis.__worldProbeUiProducts=(${createUiProductProbe.toString()})();`
	} );
}
if ( process.argv.includes( "--animation-ceiling" ) ) {
	await page.addInitScript( {
		content: `globalThis.__worldProbeAnimationCeiling=(${createAnimationCeiling.toString()})(${
			process.argv.includes( "--freeze-animation" )
		},${process.argv.includes( "--alternate-animation" )},${process.argv.includes( "--combined-ceiling" )});`
	} );
}
const started = Date.now(),
	phase = name => {
		report.phases.push( { name, ms: Date.now() - started } );
		console.log( name );
	};
await page.addInitScript( () => {
	globalThis.__worldProbeInputEvents = 0;
	for ( const name of [ "pointermove", "pointerdown", "pointerup", "wheel", "keydown", "keyup" ] ) {
		window.addEventListener( name, () => {
			globalThis.__worldProbeInputEvents++;
		}, { capture: true, passive: true } );
	}
} );
async function assetIdentity() {
	const response = await page.context().request.get(
		new URL( "/assets/packs/manifest.json", CLIENT_NEXT_BASE_URL ).href,
		{ timeout: 15000 }
	);
	if ( !response.ok() ) throw Error( "Asset manifest unavailable: HTTP " + response.status() );
	const bytes = await response.body();
	if ( bytes.length > 16 * 1024 * 1024 ) throw Error( "Asset manifest exceeds runtime budget" );
	const manifest = JSON.parse( bytes.toString( "utf8" ) );
	return {
		sha256: createHash( "sha256" ).update( bytes ).digest( "hex" ),
		generatedAt: manifest.generatedAt,
		deliveryVersion: manifest.deliveryVersion,
		assetCount: manifest.assets?.length
	};
}
async function calibrateRaf() {
	return page.evaluate( () =>
		new Promise( resolve => {
			const probe = document.createElement( "div" );
			probe.style.cssText =
				"position:fixed;width:1px;height:1px;top:0;left:0;background:white;will-change:transform";
			document.body.append( probe );
			const times = [], start = performance.now();
			function frame( time ) {
				times.push( time );
				probe.style.transform = `translateX(${times.length % 2}px)`;
				if ( time - start < 2000 ) requestAnimationFrame( frame );
				else {
					probe.remove();
					const intervals = times.slice( 1 ).map( ( t, i ) => t - times[i] ),
						sorted = [ ...intervals ].sort( ( a, b ) => a - b );
					resolve( {
						fps: 1000 * intervals.length / (times.at( -1 ) - times[0]),
						p50: sorted[Math.floor( sorted.length * .5 )],
						p95: sorted[Math.floor( sorted.length * .95 )],
						intervals
					} );
				}
			}
			requestAnimationFrame( frame );
		} )
	);
}
page.on( "pageerror", error => report.errors.push( error.message ) );
const uiResourceLogs = [];
page.on( "console", message => {
	if ( message.text().startsWith( "[ui-assets]" ) && uiResourceLogs.length < 128 ) {
		uiResourceLogs.push(
			Promise.all( message.args().slice( 1 ).map( arg => arg.jsonValue() ) ).then( args => ({
				atMs: Date.now() - started,
				...args[0]
			}) )
		);
	}
} );
if ( process.argv.includes( "--ui-resource-recovery" ) ) {
	const assetPath = "/assets/images/Media_extracted/interface/ifcommon/bg_tile/com_bg_tile_a.png";
	const response = await page.context().request.get(
		new URL( "/assets/packs/manifest.json", CLIENT_NEXT_BASE_URL ).href
	);
	if ( !response.ok() ) throw Error( "Recovery probe manifest unavailable" );
	const manifest = await response.json(), entry = manifest.assets.find( row => row.path === assetPath );
	if ( !entry || entry.transport ) throw Error( "Recovery probe requires the published loose-capable UI member" );
	report.uiResourceFault = {
		assetPath,
		failures: 0,
		qualification: "Controlled HTTP 503 on one UI image; manifest-verified loose delivery after container 404."
	};
	await page.context().route(
		new URL( entry.packPath, CLIENT_NEXT_BASE_URL ).href,
		route => route.fulfill( { status: 404, body: "Probe loose deployment" } )
	);
	await page.context().route( new URL( assetPath, CLIENT_NEXT_BASE_URL ).href, async route => {
		if ( report.uiResourceFault.failures === 0 ) {
			report.uiResourceFault.failures++;
			await route.fulfill( { status: 503, body: "Probe transient image failure" } );
		} else await route.continue();
	} );
}
if ( process.argv.includes( "--inventory" ) ) {
	page.on( "console", message => {
		if ( message.text().startsWith( "[inventory-probe]" ) ) {
			(report.inventoryWire ??= []).push( JSON.parse( message.text().slice( 17 ) ) );
		}
	} );
}
page.on( "console", message => {
	if ( message.type() === "error" && /CORS|access control|address space/i.test( message.text() ) ) {
		(report.browserPolicyErrors ??= []).push( message.text().slice( 0, 1000 ) );
	}
} );
page.context().on( "requestfailed", request => {
	const url = new URL( request.url() );
	(report.networkFailures ??= []).push( {
		origin: url.origin,
		path: url.pathname,
		error: request.failure()?.errorText
	} );
} );
page.context().on( "response", response => {
	if ( response.status() >= 400 ) {
		const url = new URL( response.url() );
		(report.httpFailures ??= []).push( { origin: url.origin, path: url.pathname, status: response.status() } );
	}
} );
try {
	report.environment = {
		browserVersion: browser.version(),
		headless: !process.argv.includes( "--headed" ),
		cpu: cpus()[0]?.model,
		logicalCpus: cpus().length,
		memoryBytes: totalmem(),
		os: platform(),
		osRelease: osRelease(),
		fpsUnlocked: process.env.SRO_PROBE_UNLOCK_FPS === "1"
	};
	const browserCdp = await browser.newBrowserCDPSession();
	try {
		const info = await browserCdp.send( "SystemInfo.getInfo" );
		report.environment.gpu = info.gpu;
	} finally {
		await browserCdp.detach();
	}
	if ( process.argv.includes( "--calibrate" ) ) {
		phase( "calibrate-before" );
		report.calibration = { before: await calibrateRaf() };
	}
	if ( release ) {
		phase( "build-release" );
		const frozen = await buildReleaseProfile( root, directory, {
			metalComparison,
			statusNotices: process.argv.includes( "--status-notices" ),
			gpuAnimation: !process.argv.includes( "--cpu-animation" ),
			combinedCeiling: process.argv.includes( "--combined-ceiling" ),
			alternateAnimation: process.argv.includes( "--alternate-animation" ),
			animationCeiling: process.argv.includes( "--animation-ceiling" ),
			freezeAnimation: process.argv.includes( "--freeze-animation" ),
			uiProducts: process.argv.includes( "--ui-products" ),
			animationPhases: process.argv.includes( "--animation-phases" ),
			frameCensus: process.argv.includes( "--frame-census" ),
			pickCensus: process.argv.includes( "--pick-census" ),
			hoverPicking: !process.argv.includes( "--no-hover" ),
			gpuTiming: process.argv.includes( "--gpu-timing" ),
			workerTiming: process.argv.includes( "--worker-timing" ),
			sourceArchivePath: process.argv.find( v => v.startsWith( "--release-source=" ) )?.slice(
				"--release-source=".length
			),
			presentationSeed: process.argv.some( v => v.startsWith( "--seed=" ) ) ?
				Number( process.argv.find( v => v.startsWith( "--seed=" ) ).slice( "--seed=".length ) ) :
				undefined
		} );
		releaseSources = frozen.sourceArchive;
		report.release = frozen.manifest;
		report.workerTiming = process.argv.includes( "--worker-timing" );
		report.gpuTiming = process.argv.includes( "--gpu-timing" );
		await frozen.install( page.context(), CLIENT_NEXT_BASE_URL );
	} else {
		await holdProbeRuntime( page );
		report.workerTiming = process.argv.includes( "--worker-timing" );
		if ( report.workerTiming ) {
			await page.route( "**/src/engine/runtime/simulation/host.ts*", async route => {
				const response = await route.fetch(),
					source = await response.text(),
					marker = "sequence = data.sequence;";
				if ( !source.includes( marker ) ) throw Error( "Worker timing observation did not match Vite module" );
				const observed = source.replace(
					marker,
					`${marker}
const samples=globalThis.__worldProbeWorkerTiming??=[];if(samples.length<4096)samples.push({...data,clock,receivedAtMs,appliedAtMs:performance.timeOrigin+performance.now()});`
				);
				await route.fulfill( { response, body: observed } );
			} );
		}
		if ( process.argv.includes( "--gpu-timing" ) || process.argv.includes( "--submission-census" ) ) {
			await page.route( "**/src/engine/runtime/renderer/device/device.ts*", async route => {
				const response = await route.fetch(), source = await response.text();
				let observed = source;
				if ( process.argv.includes( "--gpu-timing" ) ) {
					observed = observed.replace(
						/function createDevice\(timingEnabled = false\)/,
						"function createDevice(timingEnabled = true)"
					);
				}
				observed = observed.replace(
					"device = created;",
					`device = created;globalThis.__worldProbeGpuTiming=()=>timing?.stats()??null;
${
						process.argv.includes( "--submission-census" ) ?
							`const originalWrite=created.queue.writeBuffer.bind(created.queue),originalSubmit=created.queue.submit.bind(created.queue);
created.queue.writeBuffer=(buffer,offset,data,dataOffset,size)=>{const stats=globalThis.__worldProbeSubmission??={writes:0,bytes:0,submits:0,labels:{}};const unit=data.BYTES_PER_ELEMENT??1,bytes=size===undefined?data.byteLength-(dataOffset??0)*unit:size*unit;stats.writes++;stats.bytes+=bytes;const row=stats.labels[buffer.label||'unlabelled']??={writes:0,bytes:0};row.writes++;row.bytes+=bytes;return originalWrite(buffer,offset,data,dataOffset,size);};
created.queue.submit=buffers=>{const stats=globalThis.__worldProbeSubmission??={writes:0,bytes:0,submits:0,labels:{}};stats.submits++;return originalSubmit(buffers);};` :
							""
					}`
				);
				if (
					observed === source ||
					process.argv.includes( "--gpu-timing" ) && !observed.includes( "timingEnabled = true" )
				) throw Error( "GPU timing instrumentation did not match Vite module" );
				await route.fulfill( { response, body: observed } );
			} );
			report.gpuTiming = process.argv.includes( "--gpu-timing" );
			report.submissionCensus = process.argv.includes( "--submission-census" );
		}

		if ( process.argv.includes( "--combat" ) ) {
			page.on( "console", message => {
				const value = message.text();
				if ( value.startsWith( "[combat-wire]" ) ) {
					(report.combatWire ??= []).push( JSON.parse( value.slice( 13 ) ) );
				}
			} );
			const wireSource = await readFile(
				`${root}/src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts`,
				"utf8"
			);
			const wireObserved = wireSource.replace(
				"        receive(op: number, p: Uint8Array, now = 0) {",
				`        receive(op: number, p: Uint8Array, now = 0) {if([0xb245,0xb505].includes(op))console.log('[combat-wire]'+JSON.stringify({op,payload:Array.from(p.slice(0,64)),now}));`
			).replace(
				"attack(gid: number) {",
				'attack(gid: number) {console.log("[combat-wire]"+JSON.stringify({attack:gid}));'
			);
			if ( wireObserved === wireSource ) throw Error( "Combat wire recorder did not match" );
			const wireCompiled = await build( {
				stdin: {
					contents: wireObserved,
					resolveDir: `${root}/src/engine/runtime/simulation/worker/session/world/gameplay/combat`,
					loader: "ts"
				},
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: wireCompiled.outputFiles[0].text } )
			);
			const hpSource = await readFile( `${root}/src/engine/runtime/presentation/presentation.ts`, "utf8" );
			const hpObserved =
				hpSource.replace( "export function createPresentation()", "function createObservedPresentation()" ) + `
export function createPresentation(){const owner=createObservedPresentation();const sample=(kind,data)=>{const rows=globalThis.__combatHpTimeline??=[];if(rows.length<4096)rows.push({kind,at:performance.now(),localGid:owner.gameplay()?.localGid,...data});};return {...owner,apply(batch){const result=owner.apply(batch);for(const event of batch.events)if(event.kind.startsWith('hp-')||event.kind==='cast-finalize')sample('ingress',{event,hp:owner.gameplay()?.vitals?.find(v=>v.gid===event.gid)?.hp});return result;},impact(gid,key,impact,now){const before=owner.gameplay()?.vitals?.find(v=>v.gid===gid)?.hp;owner.impact(gid,key,impact,now);sample('impact',{gid,key,impact,simulationMs:now,before,after:owner.gameplay()?.vitals?.find(v=>v.gid===gid)?.hp});},release(key,now){owner.release(key,now);sample('release',{key,simulationMs:now});}};}`;
			if ( hpObserved === hpSource || !hpObserved.includes( "function createObservedPresentation()" ) ) {
				throw Error( "Effective HP observation did not match" );
			}
			const hpCompiled = await build( {
				stdin: { contents: hpObserved, resolveDir: `${root}/src/engine/runtime/presentation`, loader: "ts" },
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/presentation/presentation.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: hpCompiled.outputFiles[0].text } )
			);
			const source = await readFile( `${root}/src/engine/runtime/renderer/renderer.ts`, "utf8" );
			const observed = source.replace(
				"pickView=scene.matrix;",
				"globalThis.__combatView={matrix:Array.from(scene.matrix),origin:scene.originRegion};pickView=scene.matrix;"
			);
			if ( observed === source ) throw Error( "Combat view instrumentation did not match" );
			const compiled = await build( {
				stdin: { contents: observed, resolveDir: `${root}/src/engine/runtime/renderer`, loader: "ts" },
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/renderer/renderer.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
			);
		}
		if ( process.argv.includes( "--facing" ) || process.argv.includes( "--combat" ) ) {
			const source = await readFile( `${root}/src/engine/runtime/characters/characters.ts`, "utf8" );
			const observed = source.replace(
				"export function createCharacterPresentation",
				"function createObservedCharacterPresentation"
			) +
				`\nexport function createCharacterPresentation(...args){const renderer=args[1];globalThis.__combatRenderer=renderer;args[1]={...renderer,setCharacterModel(path,model,images){const buffers=new Set();for(const clip of model.clips)for(const ch of clip.channels){buffers.add(ch.times.buffer);buffers.add(ch.values.buffer);}for(const p of model.primitives){buffers.add(p.inverseBind.buffer);for(const v of Object.values(p.geometry))if(ArrayBuffer.isView(v))buffers.add(v.buffer);}const rows=globalThis.__combatResources??=[];if(rows.length<2048)rows.push({at:performance.now(),path,geometryAndAnimationBytes:[...buffers].reduce((n,b)=>n+b.byteLength,0),imageBytes:images.reduce((n,i)=>n+i.width*i.height*4,0),clips:model.clips.length,primitives:model.primitives.length});return renderer.setCharacterModel(path,model,images);},setCharacterActors(value){globalThis.__facingActors=value;const watch=globalThis.__combatWatch;if(watch){watch.frames++;for(const expected of watch.actors){const entity=globalThis.__worldProbeRoot?.entity(expected.gid);if(!entity||entity.appearanceState?.[0]===2)continue;const actor=value.find(a=>a.gid===expected.gid);if(!actor||actor.model!==expected.model){watch.badFrames++;if(watch.failures.length<8)watch.failures.push({at:performance.now(),gid:expected.gid,model:actor?.model??null,expected:expected.model});}}}return renderer.setCharacterActors(value);}};const owner=createObservedCharacterPresentation(...args);return {...owner,step(...input){const result=owner.step(...input);globalThis.__combatDamageText=owner.damageText();globalThis.__combatPresentationError=owner.error();return result;}};}`;
			const compiled = await build( {
				stdin: { contents: observed, resolveDir: `${root}/src/engine/runtime/characters`, loader: "ts" },
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/characters/characters.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
			);
		}
		report.allocationCensus = process.argv.includes( "--allocation-census" );
		if ( report.allocationCensus ) {
			const source = await readFile( `${root}/src/engine/runtime/renderer/device/geometry.ts`, "utf8" ),
				marker = "        upload(data: Geometry, image?: ImageDraw) {";
			if ( !source.includes( marker ) ) throw Error( "Geometry allocation census instrumentation did not match" );
			const observed = source.replace(
				marker,
				`${marker}
            const counts=globalThis.__geometryAllocations??={uploads:0,unsharedSkinUploads:0,vertexBytes:0,boneBytes:0};counts.uploads++;counts.unsharedSkinUploads+=Number(!!data.bones&&!data.material?.sharedPose);counts.vertexBytes+=data.positions.length/3*56+data.indices.byteLength+(data.joints?.length??0)*8;counts.boneBytes+=data.bones?.byteLength??0;`
			);
			const compiled = await build( {
				stdin: { contents: observed, resolveDir: `${root}/src/engine/runtime/renderer/device`, loader: "ts" },
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/renderer/device/geometry.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
			);
		}
		report.workloadCensus = process.argv.includes( "--workload-census" );
		if ( report.workloadCensus ) {
			await page.route( "**/src/engine/runtime/renderer/characters/characters.ts*", async route => {
				const response = await route.fetch(), source = await response.text(), marker = "const grouped =";
				if ( !source.includes( marker ) ) {
					throw Error( "Character workload census instrumentation did not match" );
				}
				const observed = source.replace(
					marker,
					`if(!preview){const rows=new Map();for(const actor of frameActors){const resource=models.get(actor.model);if(!resource)continue;let row=rows.get(actor.model);if(!row){row={model:actor.model,radius:resource.radius,nodes:resource.model.nodes.length,actors:0,visible:0};rows.set(actor.model,row);}row.actors++;}for(const actor of visible)rows.get(actor.model).visible++;globalThis.__characterWorkload={actors:frameActors.length,visible:visible.length,models:[...rows.values()]};}
${marker}`
				);
				await route.fulfill( { response, body: observed } );
			} );
		}
		if ( poseCensus ) {
			await page.route( "**/src/engine/foundation/animation/animation-pose.ts*", async route => {
				const response = await route.fetch(), source = await response.text();
				const observed = source.replace(
					/(function createCharacterPose\([^)]*\)\s*\{)/,
					`$1
const censusLast=new Float32Array(model.nodes.length*10).fill(NaN),censusDirty=new Uint8Array(model.nodes.length);`
				).replace(
					"for (const n of order) {",
					`const census=globalThis.__poseCensus??={evaluations:0,nodes:0,localChanges:0,globalChanges:0};census.evaluations++;
for(const n of order){let dirty=!matricesInitialized;const values=[...translations[n],...rotations[n],...scales[n]];for(let c=0;c<10;c++){if(!Object.is(censusLast[n*10+c],values[c]))dirty=true;censusLast[n*10+c]=values[c];}if(model.nodes[n].matrix&&matricesInitialized)dirty=false;const parent=model.nodes[n].parent;censusDirty[n]=dirty||(parent>=0&&censusDirty[parent]);census.nodes++;census.localChanges+=Number(dirty);census.globalChanges+=censusDirty[n];}
for (const n of order) {`
				).replace(
					"primitive = bindings.get(primitive) ?? primitive;",
					`const census=globalThis.__poseCensus??={evaluations:0,nodes:0,localChanges:0,globalChanges:0};census.paletteCalls=(census.paletteCalls??0)+1;census.paletteJoints=(census.paletteJoints??0)+primitive.joints.length;
primitive = bindings.get(primitive) ?? primitive;`
				);
				if ( !observed.includes( "const censusLast=" ) || !observed.includes( "census.evaluations++" ) ) {
					throw Error( "Pose census instrumentation did not match" );
				}
				await route.fulfill( { response, body: observed } );
			} );
		}
		if ( process.argv.includes( "--inventory" ) ) {
			const uiFile = root + "/src/engine/runtime/ui/ui.ts",
				uiSource = await readFile( uiFile, "utf8" ),
				uiObserved = uiSource.replace(
					"event(event:UiEvent){if(disposed)return;",
					"event(event:UiEvent){if(disposed)return;if('id' in event&&event.id?.startsWith('doll'))console.info('[inventory-probe]'+JSON.stringify({owner:'ui-doll',event,yaw:dollYaw}));"
				);
			const uiCompiled = await build( {
				stdin: {
					contents: uiObserved,
					resolveDir: root + "/src/engine/runtime/ui",
					sourcefile: uiFile,
					loader: "ts"
				},
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false,
				tsconfig: root + "/tsconfig.json"
			} );
			await page.route(
				"**/src/engine/runtime/ui/ui.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: uiCompiled.outputFiles[0].text } )
			);
			const file = root + "/src/engine/runtime/renderer/characters/portrait.ts",
				source = await readFile( file, "utf8" ),
				marker = "source=value.model;";
			if ( !source.includes( marker ) ) throw Error( "Inventory preview instrumentation did not match" );
			const observed = source.replace( "let source:", "let observedYaw;let source:" ).replace(
				"  const actor:CharacterActor=",
				"  if(dollYaw!==observedYaw){observedYaw=dollYaw;console.info('[inventory-probe]'+JSON.stringify({owner:'doll-yaw',yaw:dollYaw}));}\n  const actor:CharacterActor="
			).replace(
				marker,
				marker +
					"if(dollYaw!==undefined)console.info('[inventory-probe]'+JSON.stringify({owner:'doll',model:value.actor.model,clip:value.actor.previewClip,primitives:value.model.primitives.map(p=>({name:p.name,node:p.node,joints:p.joints.length})),clips:value.model.clips.map(c=>c.name)}));"
			);
			const compiled = await build( {
				stdin: {
					contents: observed,
					resolveDir: root + "/src/engine/runtime/renderer/characters",
					sourcefile: file,
					loader: "ts"
				},
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false,
				tsconfig: root + "/tsconfig.json"
			} );
			await page.route(
				"**/src/engine/runtime/renderer/characters/portrait.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
			);
			for (
				const [file, marker, insert] of [
					[
						"entities/entities.ts",
						"if(frame.opcode===0x3314||frame.opcode===0x377c){",
						"console.info('[inventory-probe]'+JSON.stringify({owner:'entities',opcode:frame.opcode,payload:Array.from(p)}));"
					],
					[
						"gameplay/inventory/inventory.ts",
						"receive(op: number, p: Uint8Array) {",
						"if(op===0xb06d)console.info('[inventory-probe]'+JSON.stringify({owner:'inventory',opcode:op,payload:Array.from(p)}));"
					]
				]
			) {
				const base = `${root}/src/engine/runtime/simulation/worker/session/world/`,
					source = await readFile( base + file, "utf8" );
				if ( !source.includes( marker ) ) throw Error( "Inventory observer does not match" );
				const compiled = await build( {
					stdin: {
						contents: source.replace( marker, marker + insert ),
						resolveDir: base + file.slice( 0, file.lastIndexOf( "/" ) ),
						loader: "ts"
					},
					tsconfig: `${root}/tsconfig.json`,
					bundle: true,
					platform: "browser",
					format: "esm",
					write: false
				} );
				await page.route(
					"**/src/engine/runtime/simulation/worker/session/world/" + file + "*",
					route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
				);
			}
		}
		if ( process.argv.includes( "--tooltips" ) ) {
			const source = await readFile( `${root}/src/engine/runtime/ui/ui.ts`, "utf8" ),
				marker = "   if(control&&tooltip.length&&phase==='world'";
			if ( !source.includes( marker ) ) throw Error( "Tooltip observer no longer matches owner" );
			const observed = source.replace( marker, "   globalThis.__tooltipProbeRows=tooltip;\n" + marker );
			const compiled = await build( {
				stdin: { contents: observed, resolveDir: `${root}/src/engine/runtime/ui`, loader: "ts" },
				tsconfig: `${root}/tsconfig.json`,
				bundle: true,
				platform: "browser",
				format: "esm",
				write: false
			} );
			await page.route(
				"**/src/engine/runtime/ui/ui.ts*",
				route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
			);
		}
		// Read-only observation of the existing frontend owner. No clock, roster,
		// resource, readiness or transition is replaced to accelerate admission.
		const frontend = await readFile( `${root}/src/engine/runtime/frontend/frontend.ts`, "utf8" );
		const observed = frontend.replace( "export function createFrontend", "function createObservedFrontend" ) +
			`\nexport function createFrontend(...args){const owner=createObservedFrontend(...args);return {...owner,step(...args){const result=owner.step(...args);globalThis.__worldProbeFrontend=result;return result;}};}`;
		const compiled = await build( {
			stdin: { contents: observed, resolveDir: `${root}/src/engine/runtime/frontend`, loader: "ts" },
			tsconfig: `${root}/tsconfig.json`,
			bundle: true,
			platform: "browser",
			format: "esm",
			write: false
		} );
		await page.route(
			"**/src/engine/runtime/frontend/frontend.ts*",
			route => route.fulfill( { contentType: "text/javascript", body: compiled.outputFiles[0].text } )
		);
		const streamSource = await readFile( `${root}/src/engine/runtime/world/world.ts`, "utf8" );
		const streamObserved =
			streamSource.replace( "export function createWorldStream", "function createObservedWorldStream" ) +
			`\nexport function createWorldStream(...args){const renderer=args[1];args[1]={...renderer,setWorldCamera(value){globalThis.__worldProbeCamera=value;return renderer.setWorldCamera(value);}};const owner=createObservedWorldStream(...args);return {...owner,step(...input){const result=owner.step(...input);globalThis.__worldProbeStats=args[1].worldStats();globalThis.__worldProbeCharacters=renderer.characterStats();return result;}};}`;
		const streamCompiled = await build( {
			stdin: { contents: streamObserved, resolveDir: `${root}/src/engine/runtime/world`, loader: "ts" },
			tsconfig: `${root}/tsconfig.json`,
			bundle: true,
			platform: "browser",
			format: "esm",
			write: false
		} );
		await page.route(
			"**/src/engine/runtime/world/world.ts*",
			route => route.fulfill( { contentType: "text/javascript", body: streamCompiled.outputFiles[0].text } )
		);
	}
	for (
		const file of [
			"runtime/ui/ui.ts",
			"runtime/platform/platform.ts",
			"runtime/platform/ui/ui.ts",
			"runtime/renderer/renderer.ts",
			"foundation/ui/ui.ts",
			"runtime/ui/hud/messages.ts",
			"runtime/ui/resources/resources.ts",
			"runtime/renderer/device/ui.ts",
			"runtime/characters/characters.ts",
			"runtime/characters/resources/resources.ts",
			"runtime/input/input.ts",
			"runtime/simulation/worker/input/input.ts",
			"runtime/simulation/host.ts",
			"runtime/simulation/worker/simulation.ts",
			"contracts/runtime.ts",
			"contracts/input.ts",
			"contracts/simulation.ts",
			"foundation/navigation/spatial-index.ts",
			"foundation/navigation/dungeon-ownership.ts",
			"foundation/navigation/object-navigation.ts",
			"runtime/simulation/worker/clock/clock.ts",
			"runtime/renderer/device/timing.ts",
			"runtime/renderer/device/device.ts",
			"runtime/ui/text/text.ts",
			"runtime/runtime.ts",
			"runtime/world/world.ts",
			"runtime/simulation/worker/session/world/entities/entities.ts",
			"runtime/renderer/world/world.ts",
			"runtime/renderer/characters/characters.ts",
			"runtime/renderer/frame/frame.ts",
			"runtime/renderer/device/geometry.ts",
			"foundation/rendering/world-scene.ts",
			"foundation/rendering/world-math.ts",
			"foundation/rendering/follow-camera.ts",
			"foundation/math/pose-math.ts",
			"foundation/animation/animation-pose.ts",
			"foundation/animation/palette-bindings.ts",
			"foundation/animation/character-bounds.ts",
			"foundation/animation/character-budget.ts"
		]
	) {
		const source = release ?
			releaseSources["src/engine/" + file] :
			await readFile( `${root}/src/engine/${file}`, "utf8" );
		if ( source === undefined ) continue;
		sourceArchive[file] = source;
		report.sources[file] = createHash( "sha256" ).update( source ).digest( "hex" );
	}
	const query = new URLSearchParams();
	if ( diagnostics ) query.set( "diagnostics", "" );
	if ( process.argv.includes( "--gpu-timing" ) ) {
		query.set( "gpu-timing", "1" );
		query.set( "frame-stages", "1" );
	}
	if ( process.argv.includes( "--no-hover" ) ) query.set( "hover-picking", "0" );
	report.assets = { before: await assetIdentity() };
	phase( "navigate" );
	await page.goto( `${CLIENT_NEXT_BASE_URL}/?${query}` );
	const control = id => page.locator( `[data-ui-id="${id}"]` );
	await page.waitForFunction(
		() => {
			const state = globalThis.__worldProbeFrontend;
			if ( state?.phase === "failed" ) throw Error( state.error ?? "Frontend failed" );
			return !!document.querySelector( '[data-ui-id="frontend:reveal"]' );
		},
		null,
		{ timeout: 60000 }
	);
	await control( "frontend:reveal" ).click( { timeout: 5000 } );
	await control( "account" ).waitFor( { timeout: 30000 } );
	const { loginId, loginPassword } = resolveProbeCredentials();
	await control( "account" ).fill( loginId );
	await control( "password" ).fill( loginPassword );
	await control( "password" ).press( "Enter" );
	phase( "login-submitted" );
	await page.evaluate( async () => {
		if ( globalThis.__worldProbeRoot ) return;
		const entry = Array.from( document.scripts ).find( s =>
			s.src && new URL( s.src ).pathname === "/src/bootstrap.ts"
		);
		if ( !entry ) throw Error( "Missing runtime" );
		globalThis.__worldProbeRoot = (await import( entry.src )).runtime;
	} );
	await page.waitForFunction(
		() => {
			const state = globalThis.__worldProbeRoot?.sessionState();
			if ( state?.phase === "failed" ) throw Error( state.error ?? "Session failed" );
			return globalThis.__worldProbeFrontend?.phase === "dock" && !globalThis.__worldProbeFrontend.cameraMoving;
		},
		null,
		{ timeout: 60000 }
	);
	await page.waitForFunction(
		name => globalThis.__worldProbeRoot.sessionState()?.characters?.some( row => row.name === name ),
		character,
		{ timeout: 15000 }
	);
	const point = await page.evaluate( async name => {
		const rows = globalThis.__worldProbeRoot.sessionState().characters,
			index = rows.findIndex( r => r.name === name );
		if ( index < 0 || index >= 4 ) throw Error( "Scratch character is absent from visible dock" );
		const { dockSlot } = globalThis.__worldProbeHelpers ??
				await import( "/src/engine/foundation/rendering/dock-slots.ts" ),
			{ screenPoint } = globalThis.__worldProbeHelpers ??
				await import( "/src/engine/foundation/rendering/screen-point.ts" );
		const slot = dockSlot( index, rows.length ),
			canvas = document.querySelector( "canvas" ),
			rect = canvas.getBoundingClientRect();
		return screenPoint(
			globalThis.__worldProbeFrontend.camera,
			[ slot.x, slot.y + 10 * rows[index].visualLoadout.heightScale, slot.z ],
			rect.width,
			rect.height
		);
	}, character );
	if ( !point ) throw Error( "Scratch character is outside dock view" );
	await page.mouse.click( point[0], point[1] );
	await page.waitForFunction(
		name =>
			globalThis.__worldProbeFrontend?.selectedCharacter === name &&
			!globalThis.__worldProbeFrontend.cameraMoving,
		character,
		{ timeout: 15000 }
	);
	phase( "scratch-selected" );
	await control( "enter" ).click();
	await page.waitForFunction(
		() => {
			const status = document.querySelector( "output" )?.textContent ?? "";
			if (
				/(?:Runtime|Renderer|Simulation) failed:|(?:World|Characters|UI):[^\n]*Error:|Frontend error: (?!none)/i
					.test( status )
			) throw Error( status );
			const game = globalThis.__worldProbeRoot.gameplay(), world = globalThis.__worldProbeStats;
			return /Frontend: world\n/.test( status ) &&
				/World: [1-9][0-9]* visible groups; 0 pending textures/.test( status ) &&
				/Characters: [1-9][0-9]* actors, [1-9][0-9]* draws; running/.test( status ) &&
				/Navigation: ready;/.test( status ) && /UI: 0 pending images; 0 failed images/.test( status ) &&
				world?.sceneId === "region:" + game?.pose?.regionId && world.pendingGroups === 0;
		},
		null,
		{ timeout: 90000 }
	);
	const restoreArg = process.argv.find( value => value.startsWith( "--restore-checkpoint=" ) );
	const walkArg = process.argv.find( value => value.startsWith( "--walk-to=" ) );
	if ( restoreArg && walkArg ) throw Error( "Choose restoration or walking to a checkpoint" );
	if ( restoreArg || walkArg ) {
		const coordinates = walkArg?.slice( "--walk-to=".length ).split( "," ).map( Number );
		if ( coordinates && (coordinates.length !== 3 || !coordinates.every( Number.isFinite )) ) {
			throw Error( "Walk checkpoint requires region,x,z" );
		}
		const previous = restoreArg ?
				JSON.parse( await readFile( restoreArg.slice( "--restore-checkpoint=".length ), "utf8" ) ) :
				{
					character,
					crossing: {
						original: { regionId: coordinates[0], x: coordinates[1], y: 0, z: coordinates[2], angle: 0 }
					}
				},
			checkpoint = previous.crossing?.original;
		if (
			previous.character !== character || !checkpoint || !Number.isInteger( checkpoint.regionId ) ||
			checkpoint.regionId < 256 || checkpoint.regionId >= 0x7f00 ||
			![ checkpoint.x, checkpoint.y, checkpoint.z ].every( Number.isFinite ) || checkpoint.x < 0 ||
			checkpoint.x >= 1920 || checkpoint.z < 0 || checkpoint.z >= 1920
		) throw Error( "Invalid matching scratch checkpoint" );
		phase( walkArg ? "walk-to-checkpoint" : "restore-checkpoint" );
		report.checkpointWalk = {
			from: await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose ),
			destination: checkpoint
		};
		await page.evaluate( destination => {
			const current = globalThis.__worldProbeRoot.gameplay().pose;
			if (
				!current ||
				Math.abs( (current.regionId & 255) - (destination.regionId & 255) ) +
							Math.abs( (current.regionId >>> 8) - (destination.regionId >>> 8) ) > 1
			) throw Error( "Scratch restoration requires current or adjacent region" );
			globalThis.__worldProbeRoot.session( { kind: "gameplay", command: { kind: "move", destination } } );
		}, checkpoint );
		await page.waitForFunction(
			destination => {
				const game = globalThis.__worldProbeRoot.gameplay(),
					session = globalThis.__worldProbeRoot.sessionState(),
					world = globalThis.__worldProbeStats;
				if ( game.error || session.phase !== "world" ) {
					throw Error( game.error ?? session.error ?? session.phase );
				}
				return game.pose?.regionId === destination.regionId &&
					Math.hypot( game.pose.x - destination.x, game.pose.z - destination.z ) < .001 &&
					game.navigationRegion === destination.regionId &&
					world?.sceneId === "region:" + destination.regionId && world.pendingGroups === 0 &&
					world.pendingTextures === 0;
			},
			checkpoint,
			{ timeout: 45000 }
		);
		report.restoration = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
	}
	if ( process.argv.includes( "--town-rebirth" ) ) {
		phase( "town-rebirth" );
		report.rebirth = await page.evaluate( () => {
			const root = globalThis.__worldProbeRoot,
				game = root.gameplay(),
				entity = root.entity( game.localGid ),
				vital = game.vitals.find( v => v.gid === game.localGid );
			if ( entity?.appearanceState?.[0] !== 2 && vital?.hp !== 0 ) return { requested: false, pose: game.pose };
			root.session( { kind: "gameplay", command: { kind: "rebirth", choice: 1 } } );
			return { requested: true, pose: game.pose };
		} );
		await page.waitForFunction(
			() => {
				const root = globalThis.__worldProbeRoot, g = root.gameplay(), w = globalThis.__worldProbeStats;
				return root.sessionState()?.phase === "world" && g?.vitals.find( v => v.gid === g.localGid )?.hp > 0 &&
					globalThis.__worldProbeFrontend?.phase === "world" && w?.sceneId === "region:" + g.pose?.regionId &&
					w.pendingGroups === 0 && w.pendingTextures === 0;
			},
			null,
			{ timeout: 60000 }
		);
	}
	report.environment.display = await page.evaluate( () => ({
		dpr: devicePixelRatio,
		screenWidth: screen.width,
		screenHeight: screen.height,
		visibility: document.visibilityState,
		canvasWidth: document.querySelector( "canvas" ).width,
		canvasHeight: document.querySelector( "canvas" ).height
	}) );
	phase( "world-resident" );
	report.entry = await page.evaluate( () => ({
		status: document.querySelector( "output" ).textContent,
		game: globalThis.__worldProbeRoot.gameplay()?.pose
	}) );
	report.entry.life = await page.evaluate( () => {
		const root = globalThis.__worldProbeRoot, g = root.gameplay();
		return {
			hp: g.vitals.find( v => v.gid === g.localGid )?.hp,
			appearance: root.entity( g.localGid )?.appearanceState?.[0]
		};
	} );
	if ( process.argv.includes( "--party" ) ) {
		phase( "party" );
		report.party = await (await import( "./lib/party-probe.mjs" )).probeParty( page, directory );
	}
	if ( process.argv.includes( "--status-notices" ) ) {
		phase( "status-notices" );
		report.statusNotices = await (await import( "./lib/status-notice-probe.mjs" )).probeStatusNotices(
			page,
			directory
		);
	}
	if ( process.argv.includes( "--ui-windows" ) ) {
		phase( "ui-windows" );
		report.uiWindows = await (await import( "./lib/ui-window-probe.mjs" )).probeUiWindows( page, directory );
	}
	if ( process.argv.includes( "--inventory" ) ) {
		phase( "inventory" );
		report.inventory = await (await import( "./lib/inventory-probe.mjs" )).probeInventory( page, directory );
	}
	if ( process.argv.includes( "--tooltips" ) ) {
		phase( "tooltips-inventory" );
		await page.keyboard.press( "KeyI" );
		const tooltipSlot = await page.evaluate( () =>
			globalThis.__worldProbeRoot.gameplay().inventory.find( item =>
				(item.typeFlags & 0x7e) === 0x2c && (item.typeFlags >>> 7 & 15) === 6
			)?.slot
		);
		if ( tooltipSlot === undefined ) throw Error( "Scratch tooltip fixture needs a weapon instance" );
		const tooltipTarget = control( "slot:" + tooltipSlot );
		await tooltipTarget.waitFor( { timeout: 15000 } );
		await tooltipTarget.hover();
		await page.waitForFunction( () => globalThis.__tooltipProbeRows?.some( r => /~/.test( r.value ) ), null, {
			timeout: 15000
		} );
		report.tooltips = { item: await page.evaluate( () => globalThis.__tooltipProbeRows ) };
		await page.screenshot( { path: `${directory}/item-tooltip.png` } );
		const slotBox = await tooltipTarget.boundingBox();
		if ( !slotBox ) throw Error( "Missing inventory hover target" );
		await page.mouse.down();
		await page.mouse.move( slotBox.x + slotBox.width / 2 + 8, slotBox.y + slotBox.height / 2, { steps: 3 } );
		await page.waitForFunction( () => !globalThis.__tooltipProbeRows?.length, null, { timeout: 5000 } );
		report.tooltips.dragDismissed = true;
		await page.mouse.move( slotBox.x + slotBox.width / 2, slotBox.y + slotBox.height / 2 );
		await page.mouse.up();
		await page.mouse.move( 10, 100 );
		await tooltipTarget.hover();
		await page.waitForFunction( () => globalThis.__tooltipProbeRows?.some( r => /~/.test( r.value ) ), null, {
			timeout: 5000
		} );
		report.tooltips.dragRestored = true;
		await page.keyboard.press( "KeyI" );
		await page.keyboard.press( "KeyS" );
		const skill = page.locator( '[data-ui-id^="skill:"]' ).first();
		await skill.waitFor( { timeout: 15000 } );
		await skill.hover();
		await page.waitForFunction( () => globalThis.__tooltipProbeRows?.some( r => r.value.includes( "Lv " ) ), null, {
			timeout: 15000
		} );
		report.tooltips.skill = await page.evaluate( () => globalThis.__tooltipProbeRows );
		await page.screenshot( { path: `${directory}/skill-tooltip.png` } );
		await page.keyboard.press( "KeyS" );
		await page.mouse.move( 10, 100 );
		await page.waitForFunction( () => !globalThis.__tooltipProbeRows?.length, null, { timeout: 5000 } );
		report.tooltips.dismissed = true;
	}
	const expectedRegionArg = process.argv.find( v => v.startsWith( "--expect-region=" ) );
	if (
		expectedRegionArg &&
		report.entry.game?.regionId !== Number( expectedRegionArg.slice( "--expect-region=".length ) )
	) throw Error( "Scratch checkpoint does not match the expected region" );
	if ( process.argv.includes( "--movement" ) && (report.entry.life.hp === 0 || report.entry.life.appearance === 2) ) {
		throw Error( "Movement benchmark requires a living scratch character" );
	}
	if ( process.argv.includes( "--guide-ui" ) ) {
		report.guideUi = await (await import( "./lib/guide-ui-probe.mjs" )).probeGuideUi( page, directory );
	}
	if ( poseCensus ) {
		await page.evaluate( () => {
			globalThis.__poseCensus = undefined;
		} );
	}
	const cdp = await page.context().newCDPSession( page );
	async function sample( name, profile = false, sampleDurationMs = durationMs, movement, camera, active ) {
		const inputStart = await page.evaluate( () => globalThis.__worldProbeInputEvents );
		if ( process.argv.includes( "--pick-census" ) ) {
			await page.evaluate( () => {
				globalThis.__worldProbePickRows = {};
			} );
		}
		if ( report.allocationCensus ) {
			await page.evaluate( () => {
				globalThis.__geometryAllocations = { uploads: 0, unsharedSkinUploads: 0, vertexBytes: 0, boneBytes: 0 };
			} );
		}
		if ( report.submissionCensus ) {
			await page.evaluate( () => {
				globalThis.__worldProbeSubmission = { writes: 0, bytes: 0, submits: 0, labels: {} };
			} );
		}
		if ( report.workerTiming ) {
			await page.evaluate( () => {
				globalThis.__worldProbeWorkerTiming = [];
			} );
		}
		phase( name );
		if ( profile ) {
			await cdp.send( "Profiler.enable" );
			await cdp.send( "Profiler.setSamplingInterval", { interval: 1000 } );
			await cdp.send( "Profiler.start" );
		}
		const machineBefore = cpus().map( cpu => cpu.times ), machineStarted = Date.now();
		await page.evaluate( captureFrameWindow, {
			duration: sampleDurationMs,
			name,
			movement,
			camera,
			active,
			deferExport: true
		} );
		const machineAfter = cpus().map( cpu => cpu.times ), machineEnded = Date.now();
		// Stop sampling before Playwright serializes telemetry and numeric frame rows.
		if ( profile ) {
			await writeFile(
				`${directory}/${name}.cpuprofile`,
				JSON.stringify( (await cdp.send( "Profiler.stop" )).profile )
			);
			await cdp.send( "Profiler.disable" );
		}
		const capture = await page.evaluate( () => {
			const result = globalThis.__worldProbeCompletedWindow;
			delete globalThis.__worldProbeCompletedWindow;
			return { ...result, frames: globalThis.__worldProbeFrameProfiler?.stop() };
		} );
		const { times, telemetry } = capture;
		if ( capture.frames ) await writeFile( `${directory}/${name}.frames.json`, JSON.stringify( capture.frames ) );
		if ( report.workerTiming ) {
			await writeFile(
				`${directory}/${name}.worker-timing.json`,
				JSON.stringify( await page.evaluate( () => globalThis.__worldProbeWorkerTiming ?? [] ) )
			);
		}
		const inputEvents = (await page.evaluate( () => globalThis.__worldProbeInputEvents )) - inputStart;
		if ( /^(stationary(?:-\d+|-repeat)?|cpu|timeline)$/.test( name ) && inputEvents ) {
			report.errors.push( `Input contaminated ${name}: ${inputEvents} events` );
		}
		const intervals = times.slice( 1 ).map( ( t, i ) => t - times[i] ),
			sorted = [ ...intervals ].sort( ( a, b ) => a - b );
		report.windows.push( {
			name,
			profile,
			inputEvents,
			telemetry,
			startFrameId: capture.startFrameId,
			endFrameId: capture.endFrameId,
			fps: 1000 * intervals.length / (times.at( -1 ) - times[0]),
			p50: sorted[Math.floor( sorted.length * .5 )],
			p95: sorted[Math.floor( sorted.length * .95 )],
			p99: sorted[Math.floor( sorted.length * .99 )],
			max: sorted.at( -1 ),
			intervals,
			status: await page.locator( "output" ).textContent()
		} );
		const window = report.windows.at( -1 );
		if ( process.argv.includes( "--animation-ceiling" ) ) {
			window.animationCeiling = await page.evaluate( () => globalThis.__worldProbeAnimationCeiling.stats() );
		}
		if ( process.argv.includes( "--ui-products" ) ) {
			await writeFile(
				`${directory}/${name}.ui-products.json`,
				JSON.stringify( await page.evaluate( () => globalThis.__worldProbeUiProducts.stats() ) )
			);
		}
		if ( process.argv.includes( "--animation-phases" ) ) {
			window.animationPhases = await page.evaluate( () => globalThis.__worldProbeAnimationPhases.stats() );
		}
		if ( movement || active ) {
			window.motion = {
				samples: capture.motion,
				commands: capture.commands,
				start: capture.start,
				end: capture.end
			};
		}
		if ( process.argv.includes( "--pick-census" ) ) {
			window.pickCensus = await page.evaluate( () => globalThis.__worldProbePickRows );
		}
		window.machine = {
			wallMs: machineEnded - machineStarted,
			logicalCpuUtilization: machineAfter.map( ( after, i ) => {
				const before = machineBefore[i];
				if ( !before ) return null;
				const total = Object.keys( after ).reduce( ( sum, key ) => sum + after[key] - before[key], 0 );
				return total > 0 ? 1 - (after.idle - before.idle) / total : null;
			} ),
			qualification:
				"Host-wide logical CPU utilization across the capture call, including unrelated processes; not renderer attribution."
		};
		if ( report.submissionCensus ) {
			window.submission = await page.evaluate( () => globalThis.__worldProbeSubmission );
		}
		if ( report.gpuTiming ) window.gpu = await page.evaluate( () => globalThis.__worldProbeGpuTiming?.() ?? null );
		window.valid =
			!/(?:Runtime|Renderer|Simulation) failed:|(?:World|Characters): Error:|Frontend error: (?!none)/i.test(
				window.status
			) && report.errors.length === 0;
		if ( !window.valid ) throw Error( `Invalid performance window ${name}: ${window.status}` );
		console.log( JSON.stringify( { window: name, fps: window.fps, p95: window.p95, max: window.max, profile } ) );
		if ( report.allocationCensus ) {
			window.geometryAllocations = await page.evaluate( () => globalThis.__geometryAllocations );
		}
		window.state = await page.evaluate( () => {
			const session = globalThis.__worldProbeRoot.sessionState(), game = globalThis.__worldProbeRoot.gameplay();
			return {
				frameTelemetry: globalThis.__worldProbeFrameTelemetry,
				phase: session?.phase,
				error: session?.error,
				pose: game?.pose,
				camera: globalThis.__worldProbeCamera,
				characters: globalThis.__worldProbeCharacters,
				ui: globalThis.__worldProbeUiStats?.(),
				characterWorkload: globalThis.__characterWorkload,
				navigationRegion: game?.navigationRegion,
				gameplayError: game?.error,
				world: globalThis.__worldProbeStats
			};
		} );
		// An active window drives real play: a cast the server refuses (MP, cooldown) is gameplay, not a fault.
		const playRefusal = active && /^Cast rejected: /.test( window.state.gameplayError ?? "" );
		if (
			window.state.phase !== "world" || window.state.error || window.state.gameplayError && !playRefusal ||
			/UI: Error:|UI error: (?!none)/i.test( window.status )
		) {
			window.valid = false;
			throw Error( `Invalid owner state in ${name}: ${JSON.stringify( window.state )}` );
		}
	}
	if ( process.argv.includes( "--facing" ) ) {
		report.facing = await probeCharacterFacing( page, directory );
		if ( report.facing.failures.length ) report.errors.push( ...report.facing.failures );
	} else {
		if ( process.argv.includes( "--hover" ) ) await page.mouse.move( viewport.width / 2, viewport.height / 2 );
		for ( let run = 0; run < warmRuns; run++ ) await sample( run ? "stationary-" + (run + 1) : "stationary" );
		if ( metalComparison ) {
			report.metalComparison = {
				kind: "same-session alternating native Metal Detail gate; off is diagnostic, not shipping",
				order: [ true, false, false, true, false, true, true, false ]
			};
			for ( const [i, enabled] of report.metalComparison.order.entries() ) {
				await page.evaluate( async enabled => {
					globalThis.__worldProbeMetalEnabled = enabled;
					for ( let i = 0; i < 120; i++ ) await new Promise( requestAnimationFrame );
					if ( globalThis.__worldProbeMetalApplied !== Number( enabled ) ) {
						throw Error( "Metal gate was not applied" );
					}
				}, enabled );
				await sample( "metal-" + (i + 1) + "-" + (enabled ? "on" : "off") );
				const window = report.windows.at( -1 );
				window.metal = await page.evaluate( () => ({
					enabled: globalThis.__worldProbeMetalApplied === 1,
					admittedReflectivePrimitives: globalThis.__worldProbeMetalUploads ?? 0,
					visibleReflectiveInstances: (globalThis.__worldProbeMetalDraws ?? []).filter( draw =>
						globalThis.__worldProbeMetalHandles?.has( draw )
					).reduce( ( sum, draw ) => sum + draw.instanceCount, 0 )
				}) );
				if ( !window.metal.visibleReflectiveInstances ) {
					throw Error( "Metal comparison did not draw a reflective material" );
				}
			}
			await page.evaluate( () => {
				delete globalThis.__worldProbeMetalEnabled;
			} );
		}
		if ( process.argv.includes( "--trace" ) ) {
			const trace = await startChromeTraceCapture( page );
			try {
				await sample( "timeline" );
			} finally {
				const captured = await trace.stop( { outputPath: `${directory}/timeline.json.gz` } );
				report.trace = {
					eventCount: captured.eventCount,
					rawBytes: captured.rawBytes,
					categories: captured.categories
				};
			}
		}
		if ( !process.argv.includes( "--no-cpu-profile" ) ) await sample( "cpu", true );
		if ( poseCensus ) report.poseChanges = await page.evaluate( () => globalThis.__poseCensus );
		if ( process.argv.includes( "--repeat-stationary" ) ) await sample( "stationary-repeat" );
		if ( process.argv.includes( "--camera" ) ) {
			const x = viewport.width / 2, y = viewport.height / 2;
			await page.mouse.move( x, y );
			await page.mouse.down( { button: "right" } );
			report.cameraRecipe = {
				kind: "synthetic-pointer-per-animation-frame",
				durationMs,
				physicalInputLatency: false
			};
			try {
				await sample( "camera", process.argv.includes( "--profile-camera" ), durationMs, undefined, { x, y } );
			} finally {
				await page.mouse.move( x, y );
				await page.mouse.up( { button: "right" } );
			}
		}
		if ( process.argv.includes( "--active" ) ) {
			// Learn Fire Force through the ordinary training commands when SP allows;
			// without it the window still walks, drags and attacks.
			const FIRE_MASTERY = 275, FIRE_FORCE = 124;
			const learned = () =>
				page.evaluate( id => globalThis.__worldProbeRoot.gameplay().skills?.includes( id ), FIRE_FORCE );
			const session = command =>
				page.evaluate( command => {
					try {
						globalThis.__worldProbeRoot.session( { kind: "gameplay", command } );
					} catch {}
				}, command );
			for ( let attempt = 0; attempt < 6 && !await learned(); attempt++ ) {
				await session( { kind: "skill-train", id: FIRE_FORCE } );
				await page.waitForTimeout( 1200 );
				if ( !await learned() ) {
					await session( { kind: "mastery-train", id: FIRE_MASTERY } );
					await page.waitForTimeout( 1200 );
				}
			}
			// Walk to a spot with live monsters first (find one with the loopback
			// /internal/diagnostics/monsters query): --active-spot=region,x,z.
			const spotArg = process.argv.find( v => v.startsWith( "--active-spot=" ) );
			if ( spotArg ) {
				const [regionId, x, z] = spotArg.slice( "--active-spot=".length ).split( "," ).map( Number );
				const start = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
				await session( { kind: "move", destination: { ...start, regionId, x, z } } );
				await page.waitForFunction(
					( { regionId, x, z } ) => {
						const p = globalThis.__worldProbeRoot.gameplay()?.pose;
						return p && p.regionId === regionId && Math.hypot( p.x - x, p.z - z ) < 3;
					},
					{ regionId, x, z },
					{ timeout: 60000 }
				);
			}
			await page.evaluate( () => {
				const game = globalThis.__worldProbeRoot.gameplay();
				if ( game?.error ) {
					globalThis.__worldProbeRoot.session( { kind: "gameplay", command: { kind: "release-target" } } );
				}
			} );
			const original = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose ),
				destination = { ...original, x: original.x - 40 };
			const x = viewport.width / 2, y = viewport.height / 2;
			await page.mouse.move( x, y );
			await page.mouse.down( { button: "right" } );
			report.active = { imbueLearned: await learned(), original, destination };
			try {
				await sample( "active", process.argv.includes( "--profile-active" ), durationMs, undefined, { x, y }, {
					original,
					destination,
					imbue: report.active.imbueLearned ? FIRE_FORCE : 0
				} );
			} finally {
				await page.mouse.move( x, y );
				await page.mouse.up( { button: "right" } );
			}
		}
		if ( process.argv.includes( "--camera-panels" ) ) {
			report.panelCamera = [];
			for (
				const [key, panel] of [
					[ null, "closed" ],
					[ "KeyS", "Skills" ],
					[ null, "closed-repeat" ],
					[ "KeyI", "Inventory" ],
					[ "KeyC", "Character" ],
					[ "KeyA", "Actions" ],
					[ "KeyQ", "Quests" ],
					[ "KeyS", "Skills-repeat" ]
				]
			) {
				if ( key ) {
					await page.keyboard.press( key );
					await page.waitForFunction(
						expected => {
							const s = globalThis.__worldProbeUiStats?.();
							return s?.panel === expected && !s.windowMissing?.length && s.windowReady;
						},
						panel.replace( "-repeat", "" ),
						{ timeout: 15000 }
					);
				}
				await page.waitForTimeout( 500 );
				const before = await page.evaluate( () => globalThis.__worldProbeUiStats?.() );
				if ( before?.panel !== (key ? panel.replace( "-repeat", "" ) : "") ) {
					throw Error( "Camera panel comparison selected the wrong window" );
				}
				const x = viewport.width / 2, y = viewport.height / 2;
				await page.mouse.move( x, y );
				await page.mouse.down( { button: "right" } );
				try {
					await sample(
						"panel-" + panel,
						process.argv.includes( "--profile-camera" ),
						durationMs,
						undefined,
						{ x, y }
					);
				} finally {
					await page.mouse.move( x, y );
					await page.mouse.up( { button: "right" } );
				}
				const after = await page.evaluate( () => globalThis.__worldProbeUiStats?.() );
				report.panelCamera.push( { panel, before, after } );
				await page.screenshot( { path: directory + "/panel-" + panel + ".png" } );
				if ( key ) await page.keyboard.press( key );
			}
		}
		if ( process.argv.includes( "--movement" ) ) {
			const original = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
			if ( !original || original.regionId & 0x8000 || original.x < 130 ) {
				throw Error( "Movement recipe requires an outdoor scratch checkpoint" );
			}
			await writeFile( `${directory}/movement-checkpoint.json`, JSON.stringify( original, null, 2 ) );
			const destination = { ...original, x: original.x - 120 }, samples = [];
			const move = pose =>
				page.evaluate(
					destination =>
						globalThis.__worldProbeRoot.session( {
							kind: "gameplay",
							command: { kind: "move", destination }
						} ),
					pose
				);
			report.movement = { original, destination, samples };
			await page.mouse.move( viewport.width / 2, viewport.height / 2 );
			try {
				for ( let run = 0; run < movementRuns; run++ ) {
					const name = run ? "movement-" + (run + 1) : "movement";
					const trace = process.argv.includes( "--trace-movement" ) ?
						await startChromeTraceCapture( page ) :
						null;
					try {
						await sample( name, process.argv.includes( "--profile-movement" ), durationMs, {
							original,
							destination
						} );
					} finally {
						if ( trace ) {
							const captured = await trace.stop( { outputPath: `${directory}/${name}.trace.json.gz` } );
							(report.movementTraces ??= []).push( {
								name,
								eventCount: captured.eventCount,
								rawBytes: captured.rawBytes,
								categories: captured.categories
							} );
						}
					}
					const observations = report.windows.at( -1 ).motion.samples;
					const travelled = observations.reduce(
						( sum, row, i ) =>
							i ?
								sum +
								Math.hypot(
									row.pose.x - observations[i - 1].pose.x,
									row.pose.z - observations[i - 1].pose.z
								) :
								0,
						0
					);
					if ( travelled < 30 ) throw Error( "Movement run did not contain the required travel" );
					samples.push( ...observations );
					await move( original );
					await page.waitForFunction(
						original => {
							const p = globalThis.__worldProbeRoot.gameplay()?.pose;
							return p?.regionId === original.regionId &&
								Math.hypot( p.x - original.x, p.z - original.z ) < 2;
						},
						original,
						{ timeout: 15000 }
					);
				}
				const distance = samples.reduce(
					( sum, row, i ) =>
						i ?
							sum + Math.hypot( row.pose.x - samples[i - 1].pose.x, row.pose.z - samples[i - 1].pose.z ) :
							0,
					0
				);
				report.movement.distance = distance;
				if ( distance < 30 ) throw Error( "Movement window did not contain the required travel" );
			} finally {
				await move( original );
				await page.waitForFunction(
					original => {
						const p = globalThis.__worldProbeRoot.gameplay()?.pose;
						return p?.regionId === original.regionId &&
							Math.hypot( p.x - original.x, p.z - original.z ) < 2;
					},
					original,
					{ timeout: 15000 }
				);
				report.movement.restored = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
			}
		}
		if (
			process.argv.includes( "--combat" ) && !process.argv.includes( "--cross-west" ) &&
			!process.argv.includes( "--cross-nearest" )
		) report.combat = await probeCombat( page, directory );
		if ( process.argv.includes( "--cross-west" ) || process.argv.includes( "--cross-nearest" ) ) {
			const original = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
			if ( original.regionId & 0x8000 ) throw Error( "Crossing recipe requires an outdoor scratch checkpoint" );
			const choices = [
				{ distance: original.x, regionId: original.regionId - 1, x: 1900, z: original.z },
				{ distance: 1920 - original.x, regionId: original.regionId + 1, x: 20, z: original.z },
				{ distance: original.z, regionId: original.regionId - 256, x: original.x, z: 1900 },
				{ distance: 1920 - original.z, regionId: original.regionId + 256, x: original.x, z: 20 }
			];
			const choice = process.argv.includes( "--cross-nearest" ) ?
				choices.sort( ( a, b ) => a.distance - b.distance )[0] :
				choices[0];
			if ( choice.distance > 800 || choice.regionId < 256 || choice.regionId >= 0x7f00 ) {
				throw Error( "Crossing recipe requires a scratch checkpoint within 800 units of a valid boundary" );
			}
			const destination = { ...original, regionId: choice.regionId, x: choice.x, z: choice.z };
			const move = async pose =>
				page.evaluate(
					destination =>
						globalThis.__worldProbeRoot.session( {
							kind: "gameplay",
							command: { kind: "move", destination }
						} ),
					pose
				);
			report.crossing = { original, destination };
			try {
				await move( destination );
				await sample( "outward-crossing", process.argv.includes( "--profile-crossing" ), 18000 );
				await page.waitForFunction(
					region => {
						const game = globalThis.__worldProbeRoot.gameplay(),
							session = globalThis.__worldProbeRoot.sessionState(),
							world = globalThis.__worldProbeStats,
							status = document.querySelector( "output" )?.textContent ?? "";
						if ( game.error || session?.phase !== "world" || /World: Error:/.test( status ) ) {
							throw Error( game.error ?? session?.error ?? status );
						}
						return game.pose?.regionId === region && game.navigationRegion === region &&
							world?.sceneId === "region:" + region && world.pendingGroups === 0 &&
							world.pendingTextures === 0;
					},
					destination.regionId,
					{ timeout: 15000 }
				);
				report.crossing.arrived = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
				await page.screenshot( { path: `${directory}/crossed.png` } );
				if ( process.argv.includes( "--combat" ) ) report.combat = await probeCombat( page, directory );
			} finally {
				await move( original );
				await sample( "return-crossing", false, 18000 );
				report.crossing.restored = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay().pose );
				const restored = report.crossing.restored, last = report.windows.at( -1 ).state;
				if (
					!restored || restored.regionId !== original.regionId ||
					Math.hypot( restored.x - original.x, restored.z - original.z ) > 2 || last.phase !== "world" ||
					last.world?.sceneId !== "region:" + original.regionId || last.world.pendingGroups ||
					last.world.pendingTextures
				) {
					throw Error(
						"Scratch checkpoint restoration is unconfirmed; inspect crossing report and session state"
					);
				}
			}
		}
	}
	await page.screenshot( { path: `${directory}/world.png` } );
	report.final = await page.evaluate( () => globalThis.__worldProbeRoot.gameplay()?.pose );
	if ( process.argv.includes( "--calibrate" ) ) {
		phase( "calibrate-after-disposal" );
		await page.evaluate( () => globalThis.__worldProbeRoot.dispose() );
		report.calibration.after = await calibrateRaf();
	}
	for ( const [file, hash] of Object.entries( report.sources ) ) {
		const current = createHash( "sha256" ).update( await readFile( `${root}/src/engine/${file}` ) ).digest( "hex" );
		if ( current !== hash ) {
			if ( release ) (report.workspaceDrift ??= []).push( file );
			else report.errors.push( "Source changed during capture: " + file );
		}
	}
	report.assets.after = await assetIdentity();
	if ( report.assets.before.sha256 !== report.assets.after.sha256 ) {
		report.errors.push( "Asset pack manifest changed during capture; controlled comparison invalid" );
	}
	report.uiResourceEvents = await Promise.all( uiResourceLogs );
	if ( report.uiResourceFault ) {
		const events = report.uiResourceEvents.filter( e => e.path === report.uiResourceFault.assetPath );
		if (
			report.uiResourceFault.failures !== 1 || !events.some( e => e.kind === "failed" ) ||
			!events.some( e => e.kind === "recovered" )
		) throw Error( "UI resource HTTP failure/recovery witness missing" );
	}
	report.verdict = report.errors.length ? "FAIL" : "CAPTURED";
	if ( report.verdict === "FAIL" ) process.exitCode = 1;
} catch ( error ) {
	report.verdict = "FAIL";
	report.error = String( error );
	report.status = await page.locator( "output" ).textContent().catch( () => null );
	report.roster = await page.evaluate( () =>
		globalThis.__worldProbeRoot?.sessionState()?.characters?.map( c => ({
			name: c.name,
			deletePending: c.deletePending
		}) )
	).catch( () => null );
	report.game = await page.evaluate( () => {
		const game = globalThis.__worldProbeRoot?.gameplay();
		return game ? { pose: game.pose, navigationRegion: game.navigationRegion, error: game.error } : null;
	} ).catch( () => null );
	await page.screenshot( { path: `${directory}/failure.png` } ).catch( () => {} );
	process.exitCode = 1;
} finally {
	report.uiResourceEvents = await Promise.all( uiResourceLogs );
	try {
		await writeFile( `${directory}/sources.json`, JSON.stringify( sourceArchive ) );
		await writeFile( `${directory}/report.json`, JSON.stringify( report, null, 2 ) );
	} finally {
		try {
			await browser.close();
		} finally {
			await captureLease.release();
		}
	}
	console.log(
		JSON.stringify( {
			directory,
			character: report.character,
			verdict: report.verdict,
			error: report.error,
			errors: report.errors,
			windows: report.windows.map( ( { name, fps, p95, p99, max, valid } ) => ({
				name,
				fps,
				p95,
				p99,
				max,
				valid
			}) )
		} )
	);
}
