/*
===========================================================================

vite.config.mjs - the development server, preview and local build

The /api and shard proxies, tunnel hosts, optional TLS, and the build's own
stamp (the commit it is built from). Beta delivery has its own builder.

===========================================================================
*/
import { clientBuildDefinitions } from "./tools/build-metadata.mjs";
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
		build: bench ?
			{ outDir: "temp/artifacts/dist-bench", rolldownOptions: { preserveEntrySignatures: "exports-only" } } :
			{ outDir: "temp/artifacts/dist" },
		plugins: [ devUpdates(), publishedAssets(), ...(https && !certificate ? [ basicSsl() ] : []) ],
		resolve: {
			alias: { "@": fileURLToPath( new URL( "./src", import.meta.url ) ) }
		}
	};
} );
