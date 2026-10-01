import { readFile, readdir, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { build } from "vite";
import { transform } from "esbuild";
import { instrumentAnimationCeiling } from "./animation-ceiling.mjs";
import { instrumentAnimationPhases, instrumentAnimationAdmission } from "./animation-phase-profiler.mjs";

const hash = value => createHash( "sha256" ).update( value ).digest( "hex" );
const normalized = value => path.resolve( value ).replaceAll( "\\", "/" );

// Freeze application inputs before compilation. Both the main and worker Vite
// graphs read this archive; edits in another session cannot alter a loaded run.
export async function buildReleaseProfile( root, directory, {
	metalComparison = false,
	gpuAnimation = true,
	gpuTiming = false,
	workerTiming = false,
	sourceArchivePath,
	presentationSeed,
	hoverPicking = true,
	pickCensus = false,
	frameCensus = false,
	animationPhases = false,
	uiProducts = false,
	statusNotices = false,
	animationCeiling = false,
	freezeAnimation = false,
	alternateAnimation = false,
	combinedCeiling = false
} = {} ) {
	if (
		presentationSeed !== undefined &&
		(!Number.isInteger( presentationSeed ) || presentationSeed < 0 || presentationSeed > 0xffffffff)
	) throw Error( "Invalid release presentation seed" );
	const sources = new Map();
	async function collect( folder ) {
		for ( const entry of await readdir( folder, { withFileTypes: true } ) ) {
			const file = path.join( folder, entry.name );
			if ( entry.isDirectory() ) await collect( file );
			else sources.set( normalized( file ), await readFile( file, "utf8" ) );
		}
	}
	let html;
	if ( sourceArchivePath ) {
		const bytes = await readFile( sourceArchivePath );
		if ( bytes.length > 64 * 1024 * 1024 ) throw Error( "Release source archive exceeds budget" );
		const archive = JSON.parse( bytes.toString( "utf8" ) );
		for ( const [name, source] of Object.entries( archive ) ) {
			if (
				typeof source !== "string" || name.includes( "\\" ) || name.split( "/" ).some( p => p === ".." ) ||
				!name.startsWith( "src/" ) && name !== "index.html"
			) throw Error( "Invalid release source archive entry" );
			if ( name === "index.html" ) html = source;
			else sources.set( normalized( path.join( root, name ) ), source );
		}
		if ( !html || !sources.has( normalized( path.join( root, "src/bootstrap.ts" ) ) ) ) {
			throw Error( "Incomplete release source archive" );
		}
	} else {
		await collect( path.join( root, "src" ) );
		html = await readFile( path.join( root, "index.html" ), "utf8" );
	}
	const plugin = () => ({
		name: "frozen-profile-inputs",
		enforce: "pre",
		resolveId( id, importer ) {
			const base = id.startsWith( "/src/" ) ?
				path.join( root, id.slice( 1 ) ) :
				id.startsWith( "@/" ) ?
				path.join( root, "src", id.slice( 2 ) ) :
				id.startsWith( "." ) && importer ?
				path.resolve( path.dirname( importer.split( "?" )[0] ), id ) :
				id;
			if ( !path.isAbsolute( base ) ) return;
			const file = normalized( base );
			if ( file === normalized( path.join( root, "index.html" ) ) ) return file;
			for ( const suffix of [ "", ".ts", ".tsx", ".js", "/index.ts" ] ) {
				if ( sources.has( file + suffix ) ) {
					return file + suffix;
				}
			}
		},
		load( id ) {
			const file = normalized( id.split( "?" )[0] );
			if ( id.includes( "?" ) ) return;
			if ( file === normalized( path.join( root, "index.html" ) ) ) return html;
			if ( file.startsWith( normalized( path.join( root, "src" ) ) + "/" ) && !sources.has( file ) ) {
				throw Error(
					"Source was not frozen: " + file
				);
			}
			return sources.get( file );
		},
		transformIndexHtml: { order: "pre", handler: () => html },
		async transform( source, id ) {
			const uninstrumented = source;
			const relativeFile = path.posix.relative( normalized( root ), normalized( id.split( "?" )[0] ) );
			if ( metalComparison && relativeFile === "src/engine/runtime/renderer/renderer.ts" ) {
				const marker = "scene.environment[83]=video.records[video.active][6]===1?1:0;";
				if (
					source.split( marker ).length !== 2 || source.split( "                frame!.draw(" ).length !== 2
				) throw Error( "Metal comparison gate no longer matches" );
				source = source.replace(
					"                frame!.draw(",
					"                globalThis.__worldProbeMetalDraws=characterDraws;frame!.draw("
				);
				source = source.replace(
					marker,
					marker +
						"if(typeof globalThis.__worldProbeMetalEnabled==='boolean')scene.environment[83]=Number(globalThis.__worldProbeMetalEnabled);globalThis.__worldProbeMetalApplied=scene.environment[83];"
				);
			}
			if ( metalComparison && relativeFile === "src/engine/runtime/renderer/device/geometry.ts" ) {
				const marker =
					"upload(data: Geometry, image?: ImageDraw,paletteOffsets?:Uint32Array,environmentImage?:ImageDraw) {";
				if (
					source.split( marker ).length !== 2 ||
					source.split( "geometryBuffers.set(draw, buffers);" ).length !== 2 ||
					source.split( "geometryBuffers.set(replacement, buffers);" ).length !== 2
				) throw Error( "Metal material census no longer matches" );
				source = source.replace(
					marker,
					marker +
						"if(data.material?.environmentReflection)globalThis.__worldProbeMetalUploads=(globalThis.__worldProbeMetalUploads??0)+1;"
				);
				source = source.replace(
					"geometryBuffers.set(draw, buffers);",
					"if(mat?.environmentReflection)(globalThis.__worldProbeMetalHandles??=new WeakSet()).add(draw);geometryBuffers.set(draw, buffers);"
				);
				source = source.replace(
					"geometryBuffers.set(replacement, buffers);",
					"if(globalThis.__worldProbeMetalHandles?.has(draw))globalThis.__worldProbeMetalHandles.add(replacement);geometryBuffers.set(replacement, buffers);"
				);
			}
			if ( uiProducts && relativeFile === "src/engine/runtime/ui/ui.ts" ) {
				const marker = "  // A completion here must rebuild texture-dependent quads on the next step.";
				if ( source.split( marker ).length !== 2 ) throw Error( "UI product snapshot boundary changed" );
				source = source.replace(
					marker,
					"globalThis.__worldProbeUiProducts?.record(w,h,quads,semantics);" + marker
				);
			}
			if ( statusNotices && relativeFile === "src/engine/runtime/ui/ui.ts" ) {
				const marker = "step(next:UiView,now=0):UiSemantics|null{";
				if ( source.split( marker ).length !== 2 ) throw Error( "Status probe UI boundary changed" );
				source = source.replace(
					marker,
					marker +
						"if(globalThis.__statusNoticeProbe&&next.gameplay)next={...next,gameplay:{...next.gameplay,notices:[...(next.gameplay.notices??[]),...globalThis.__statusNoticeProbe]}};"
				);
			}
			if ( animationCeiling && relativeFile === "src/engine/foundation/animation/animation-pose.ts" ) {
				source = instrumentAnimationCeiling( source );
			}
			if ( animationPhases ) source = instrumentAnimationAdmission( source, relativeFile );
			if ( animationPhases && relativeFile === "src/engine/foundation/animation/animation-pose.ts" ) {
				source = instrumentAnimationPhases( source );
			}
			const instrument = () => {
				const file = normalized( id.split( "?" )[0] ),
					relative = path.posix.relative( normalized( root ), file );
				if ( relative === "src/bootstrap.ts" ) {
					if ( presentationSeed !== undefined || source.includes( "undefined, diagnostics" ) ) {
						const original = source;
						source = source.replace(
							/startRuntime\(canvas,\s*status(?:,\s*undefined,\s*diagnostics)?\)/,
							`startRuntime(canvas,status,${
								presentationSeed ?? "undefined"
							},{gpuAnimation:${gpuAnimation},stages:${gpuTiming},gpuTiming:${gpuTiming},hoverPicking:${hoverPicking}})`
						);
						if ( source === original ) {
							throw Error( "Release presentation options did not match bootstrap" );
						}
					}
					return source +
						`\nimport {dockSlot} from './engine/foundation/rendering/dock-slots';import {screenPoint} from './engine/foundation/rendering/screen-point';globalThis.__worldProbeRoot=runtime;globalThis.__worldProbeHelpers={dockSlot,screenPoint};`;
				}
				if ( relative === "src/engine/runtime/frontend/frontend.ts" ) {
					return source.replace(
						"export function createFrontend",
						"function createObservedFrontend"
					) +
						`\nexport function createFrontend(...args){const owner=createObservedFrontend(...args);return {...owner,step(...input){const result=owner.step(...input);globalThis.__worldProbeFrontend=result;return result;}};}`;
				}
				if ( relative === "src/engine/runtime/renderer/renderer.ts" ) {
					return source.replace(
						"export function createRenderer",
						"function createObservedRenderer"
					) +
						`\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);return {...owner,frame(...input){const result=owner.frame(...input);globalThis.__worldProbeRenderedFrame=input[2];return result;}};}`;
				}
				if ( relative === "src/engine/runtime/ui/ui.ts" ) {
					return source.replace(
						"export function createUi",
						"function createObservedUi"
					) +
						`\nexport function createUi(...args){const owner=createObservedUi(...args);globalThis.__worldProbeUiStats=owner.stats;return owner;}`;
				}
				if ( relative === "src/engine/runtime/world/world.ts" ) {
					return source.replace(
						"export function createWorldStream",
						"function createObservedWorldStream"
					) +
						`\nexport function createWorldStream(...args){const renderer=args[1];args[1]={...renderer,setWorldCamera(value){globalThis.__worldProbeCamera=value;return renderer.setWorldCamera(value);}};const owner=createObservedWorldStream(...args);return {...owner,step(...input){const result=owner.step(...input);globalThis.__worldProbeStats=renderer.worldStats();globalThis.__worldProbeCharacters=renderer.characterStats();return result;}};}`;
				}
				if ( relative === "src/engine/runtime/renderer/device/device.ts" && gpuTiming ) {
					const result = source.replace(
						"createDevice(timingEnabled=false",
						"createDevice(timingEnabled=true"
					).replace(
						"device = created;",
						"device = created;globalThis.__worldProbeGpuTiming=()=>timing?.stats()??null;"
					);
					if (
						!result.includes( "timingEnabled=true" ) || !result.includes( "__worldProbeGpuTiming" )
					) throw Error( "Release GPU observation no longer matches" );
					return result;
				}
				if ( relative === "src/engine/runtime/platform/platform.ts" ) {
					const marker = "presentTelemetry(sample){";
					if ( !source.includes( marker ) ) throw Error( "Release frame observation no longer matches" );
					return source.replace( marker, marker + "globalThis.__worldProbeFrameTelemetry=sample;" );
				}
				if ( relative === "src/engine/runtime/simulation/host.ts" && workerTiming ) {
					const marker = "sequence = data.sequence;";
					if ( !source.includes( marker ) ) throw Error( "Release worker observation no longer matches" );
					return source.replace(
						marker,
						`${marker}const samples=globalThis.__worldProbeWorkerTiming??=[];if(samples.length<4096)samples.push({...data,clock,receivedAtMs,appliedAtMs:performance.timeOrigin+performance.now()});`
					);
				}
			};
			const observed = instrument() ?? (source !== uninstrumented ? source : undefined);
			if ( observed !== undefined ) {
				// Map to the archived observed TS, including the narrow probe wrappers.
				// This avoids attributing generated wrapper lines to unrelated production lines.
				const result = await transform( observed, {
					loader: "ts",
					sourcefile: id,
					sourcemap: "external",
					sourcesContent: true,
					target: "es2022"
				} );
				return { code: result.code, map: JSON.parse( result.map ) };
			}
		}
	});
	const outDir = path.join( directory, "release" );
	await mkdir( outDir, { recursive: true } );
	const result = await build( {
		root,
		configFile: false,
		publicDir: false,
		logLevel: "warn",
		plugins: [ plugin() ],
		resolve: { alias: { "@": path.join( root, "src" ) } },
		worker: { plugins: () => [ plugin() ] },
		build: { outDir, emptyOutDir: false, sourcemap: "hidden", minify: "esbuild" },
		// A profile build: runtime.ts enables its explicit probe hooks only when this
		// is defined (or in development). Production builds never define it.
		define: { __SRO_PROFILE_BUILD__: "true" }
	} );
	const archive = Object.fromEntries(
		[ ...sources ].map( ( [file, source] ) => [ path.posix.relative( normalized( root ), file ), source ] )
	);
	archive["index.html"] = html;
	await writeFile( path.join( directory, "release-sources.json" ), JSON.stringify( archive ) );
	const outputs = new Map();
	async function emitted( folder ) {
		for ( const entry of await readdir( folder, { withFileTypes: true } ) ) {
			const file = path.join( folder, entry.name );
			if ( entry.isDirectory() ) await emitted( file );
			else outputs.set( "/" + path.relative( outDir, file ).replaceAll( "\\", "/" ), await readFile( file ) );
		}
	}
	await emitted( outDir );
	if ( !outputs.has( "/index.html" ) || !result ) throw Error( "Missing release output" );
	const manifest = {
		mode: "production",
		presentationSeed,
		instrumentation: {
			metalComparison,
			readOnlyFrontend: true,
			readOnlyWorld: true,
			readOnlyFrameTelemetry: true,
			gpuAnimation,
			gpuTiming,
			workerTiming,
			stages: gpuTiming,
			hoverPicking,
			pickCensus,
			frameCensus,
			animationPhases,
			uiProducts,
			animationCeiling,
			freezeAnimation,
			alternateAnimation,
			combinedCeiling
		},
		sources: Object.fromEntries( Object.entries( archive ).map( ( [file, source] ) => [ file, hash( source ) ] ) ),
		outputs: Object.fromEntries( [ ...outputs ].map( ( [file, bytes] ) => [ file, hash( bytes ) ] ) )
	};
	await writeFile( path.join( directory, "release-manifest.json" ), JSON.stringify( manifest, null, 2 ) );
	return {
		manifest,
		sourceArchive: archive,
		async install( context, baseUrl ) {
			const origin = new URL( baseUrl ).origin;
			const served = [];
			for ( const [name, bytes] of outputs ) {
				if ( !name.endsWith( ".js" ) ) continue;
				const url = origin + "/@fs/" + normalized( path.join( outDir, name.slice( 1 ) ) ),
					response = await context.request.get( url );
				if ( !response.ok() ) throw Error( "Compiled release script is not reachable: " + name );
				const body = await response.text(),
					program = value => value.replace( /\/\/# sourceMappingURL=.*$/gm, "" ).trim();
				if ( program( body ) !== program( bytes.toString( "utf8" ) ) ) {
					throw Error(
						"Server transformed release executable code: " + name
					);
				}
				served.push( {
					name,
					compiledHash: hash( bytes ),
					servedHash: hash( body ),
					programHash: hash( program( body ) )
				} );
			}
			await writeFile( path.join( directory, "release-served.json" ), JSON.stringify( served, null, 2 ) );
			await context.route(
				url => url.origin === origin && (url.pathname === "/" || outputs.has( url.pathname )),
				async route => {
					const name = new URL( route.request().url() ).pathname,
						bytes = outputs.get( name === "/" ? "/index.html" : name );
					const type = name === "/" ?
						"text/html" :
						name.endsWith( ".js" ) ?
						"text/javascript" :
						name.endsWith( ".css" ) ?
						"text/css" :
						name.endsWith( ".map" ) ?
						"application/json" :
						"application/octet-stream";
					// Let Chrome fetch worker scripts over the existing loopback server so
					// their network address space remains local. A synthetic fulfilled worker
					// response loses that classification and can block its real API requests.
					if ( name !== "/" ) {
						return route.continue( {
							url: origin + "/@fs/" + normalized( path.join( outDir, name.slice( 1 ) ) )
						} );
					}
					const response = await route.fetch();
					return route.fulfill( {
						response,
						status: 200,
						contentType: type,
						body: bytes,
						headers: { "Cache-Control": "no-store" }
					} );
				}
			);
		}
	};
}
