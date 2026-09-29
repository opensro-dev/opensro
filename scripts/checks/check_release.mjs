/*
===========================================================================

check_release.mjs - run deployment boundary tests on every source verification.

Python owns the forced SSH receivers. These tests must run even when Go and
browser compilation are unchanged; Linux CI additionally exercises symlinks.

===========================================================================
*/

import { spawnSync } from "node:child_process";

/*
================
main

Use the platform interpreter name without a shell or a visible Windows console.
================
*/
function main() {
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
