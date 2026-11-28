/*
===========================================================================

vite.config.mjs - the development server, preview and local build

The /api and shard proxies, tunnel hosts, optional TLS, and the build's own
stamp (the commit it is built from). Beta delivery has its own builder.
The local build ships like a release does: the license banner on every
chunk and source maps with the readable source (build-metadata.mjs).

===========================================================================
*/
import { clientBuildDefinitions, clientStamp, releaseSourceOutput } from "./tools/build-metadata.mjs";
import { fileURLToPath } from "node:url";
import { defineConfig, loadEnv } from "vite";
import basicSsl from "@vitejs/plugin-basic-ssl";
import { publishedAssets } from "./tools/published-assets.mjs";
import { devUpdates } from "./tools/dev/updates-plugin.mjs";
import { sameOriginRelay, shardRoutes, tlsCertificate, tunnelHosts } from "./tools/dev/edge.mjs";

export default defineConfig( ( { mode } ) => {
	// Beta requires the isolated compiler, private maps and publication gate.
	// A mode flag on this development/preview config cannot create a release.
	if ( mode === "beta" ) throw Error( "Use pnpm build:beta; beta delivery requires the verified package builder" );
	const env = loadEnv( mode, fileURLToPath( new URL( ".", import.meta.url ) ), "" );
	const target = env.SRO_AGENT_PROXY_TARGET || "http://127.0.0.1:8787";
	const catalog = env.SRO_SHARD_CATALOG ||
		fileURLToPath( new URL( "../server/config/shards.json", import.meta.url ) );
	// Tunnel hosts (SRO_DEV_TUNNEL_HOSTS) pass Vite's host check and have their
	// forwarded HTTPS scheme believed; without any, the edge is unchanged.
	const tunnels = tunnelHosts( env ),
		relay = sameOriginRelay( tunnels ),
		hosts = tunnels.length ? { allowedHosts: tunnels } : {};
	const proxy = {
		"/api": {
			target,
			changeOrigin: false,
			xfwd: true,
			rewrite: path => path.replace( /^\/api(?=\/|$)/, "" ),
			configure: relay
		},
		...shardRoutes( catalog, relay )
	};
	// `pnpm dev:https`: devices other than this machine need TLS for the secure
	// context WebGPU requires. Without an explicit pair, a self-signed one.
	const https = mode === "https", certificate = https ? tlsCertificate( env ) : null;
	// `pnpm build:bench`: the release bundle with one difference, the entry
	// keeps its `runtime` export so the frame benchmark can drive a built
	// client. Release bundles drop it on purpose: page scripts get no runtime.
	// A bench bundle has its own output folder and is never deployed.
	const bench = mode === "bench";
	// The banner and the published source paths, for page and worker chunks alike.
	const sourceOutput = releaseSourceOutput(
		fileURLToPath( new URL( ".", import.meta.url ) ),
		clientStamp().revision
	);
	return {
		publicDir: false,
		define: clientBuildDefinitions(),
		server: {
			proxy,
			...hosts,
			...(certificate ? { https: certificate } : {}),
			warmup: { clientFiles: [ "./src/bootstrap.ts" ] }
		},
		preview: { proxy, ...hosts, ...(certificate ? { https: certificate } : {}) },
		worker: bench ? {} : { rolldownOptions: { output: sourceOutput } },
		build: bench ?
			{
				outDir: "temp/artifacts/dist-bench",
				// The release build's source maps, so a profile of the bench bundle
				// names client source (tools/perf/analyze/profile.mjs --maps).
				sourcemap: true,
				// SRO_BENCH_MINIFY=0 keeps names and lines for attribution
				// (profile.mjs --lines cannot split one minified line); its fps
				// is not the release bundle's, so timings come from the default.
				minify: process.env.SRO_BENCH_MINIFY !== "0",
				// The page entry is index.html, so bootstrap.ts is only a module it
				// imports and its exports are tree-shaken. Naming it an entry too
				// keeps its signature; the page still loads it exactly once.
				rolldownOptions: {
					input: {
						index: fileURLToPath( new URL( "./index.html", import.meta.url ) ),
						bootstrap: fileURLToPath( new URL( "./src/bootstrap.ts", import.meta.url ) )
					},
					preserveEntrySignatures: "exports-only",
					// The benchmark imports the runtime from one fixed URL; release
					// bundles have no such file.
					output: {
						entryFileNames: chunk =>
							chunk.name === "bootstrap" ? "assets/bench-runtime.js" : "assets/[name]-[hash].js"
					}
				}
			} :
			{
				outDir: "temp/artifacts/dist",
				// The client is AGPL source: a built copy carries the notice and its
				// readable source wherever it is hosted (NOTICE.md).
				sourcemap: true,
				// viewer.html is the embeddable 3D viewer (docs/guides/VIEWER.md), a second
				// page beside the game's.
				rolldownOptions: {
					input: {
						index: fileURLToPath( new URL( "./index.html", import.meta.url ) ),
						viewer: fileURLToPath( new URL( "./viewer.html", import.meta.url ) )
					},
					output: sourceOutput
				}
			},
		plugins: [ devUpdates(), publishedAssets(), ...(https && !certificate ? [ basicSsl() ] : []) ],
		resolve: {
			alias: { "@": fileURLToPath( new URL( "./src", import.meta.url ) ) }
		}
	};
} );
