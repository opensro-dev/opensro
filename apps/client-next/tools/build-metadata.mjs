/*
===========================================================================

build-metadata.mjs - identical Git stamps for development and beta compilers

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

/*
================
clientBuildDefinitions

One Git read keeps revision and subject from different commits from mixing.
The compiler folds these into the artifact, never the deployment checkout.
================
*/
export function clientBuildDefinitions( cwd = fileURLToPath( new URL( "..", import.meta.url ) ) ) {
	let revision = "", subject = "";
	try {
		const result = execFileSync( "git", [ "log", "-1", "--format=%H%n%s" ], {
			cwd,
			encoding: "utf8",
			stdio: [ "ignore", "pipe", "ignore" ]
		} ).trim().split( "\n" );
		revision = result[0] ?? "";
		subject = result.slice( 1 ).join( " " );
	} catch { /* Source archives may not carry Git metadata. */ }
	return {
		"import.meta.env.SRO_CLIENT_REVISION": JSON.stringify( revision ),
		"import.meta.env.SRO_CLIENT_SUBJECT": JSON.stringify( subject )
	};
}
