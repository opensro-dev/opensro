/*
===========================================================================

check_release.mjs - run deployment boundary tests on every source verification.

Python owns the forced SSH receivers. These tests must run even when Go and
browser compilation are unchanged; Linux CI additionally exercises symlinks.

It also holds the client build to its release declaration: the release
protocol and asset schema compiled into the client (foundation/release/
protocol.ts) must be the ones compatibility.json declares, the protocol
inside the declared server range, and the asset pipeline must write the
schema the client reads.
The Go gate checks the server's compiled report the same way.

===========================================================================
*/

import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { ASSET_SCHEMA, RELEASE_PROTOCOL } from "../../apps/client-next/src/engine/foundation/release/protocol.ts";
import { ASSET_SCHEMA as PIPELINE_ASSET_SCHEMA } from "../build/assetSchema.mjs";

const COMPATIBILITY = "apps/server/ops/release/compatibility.json";

/*
================
checkClientProtocol

Returns the problem, or null when the client build speaks the declared
release protocol and the declared server range contains it.
================
*/
function checkClientProtocol() {
	const declared = JSON.parse( readFileSync( COMPATIBILITY, "utf8" ) );
	if ( declared.client.protocol !== RELEASE_PROTOCOL ) {
		return `client speaks release protocol ${RELEASE_PROTOCOL}; ${COMPATIBILITY} declares ${declared.client.protocol}`;
	}
	if ( declared.client.assetSchema !== ASSET_SCHEMA ) {
		return `client reads asset schema ${ASSET_SCHEMA}; ${COMPATIBILITY} declares ${declared.client.assetSchema}`;
	}
	if ( PIPELINE_ASSET_SCHEMA !== ASSET_SCHEMA ) {
		return `asset pipeline writes schema ${PIPELINE_ASSET_SCHEMA}; the client reads ${ASSET_SCHEMA}`;
	}
	if ( RELEASE_PROTOCOL < declared.server.protocolMin || RELEASE_PROTOCOL > declared.server.protocolMax ) {
		return `client release protocol ${RELEASE_PROTOCOL} is outside the declared server range ` +
			`[${declared.server.protocolMin}, ${declared.server.protocolMax}]`;
	}
	return null;
}

/*
================
main

Use the platform interpreter name without a shell or a visible Windows console.
================
*/
function main() {
	const problem = checkClientProtocol();
	if ( problem ) {
		console.error( "release protocol: " + problem );
		process.exitCode = 1;
		return;
	}
	const executable = process.platform === "win32" ? "python" : "python3";
	for (
		const arguments_ of [
			[ "-m", "compileall", "-q", "apps/server/ops/release" ],
			[ "-m", "unittest", "discover", "-s", "apps/server/ops/release", "-p", "test_*.py" ]
		]
	) {
		const result = spawnSync( executable, arguments_, { stdio: "inherit", windowsHide: true } );
		if ( result.error ) throw result.error;
		if ( result.status !== 0 ) {
			process.exitCode = result.status ?? 1;
			return;
		}
	}
}

main();
