/*
===========================================================================

particle-tick-digest.mjs - the particle graph tick digest of the effect catalogue

	node tools/particle-tick-digest.mjs           check the fixture
	node tools/particle-tick-digest.mjs --write   rewrite it

tests/fixtures/particle-tick-digest.json holds, for every effect of
/assets/effects/programs.json with a particle graph, the digest of its
tick through three scenarios (tests/helpers/particle-tick-digest.mjs).
A change to the tick that must not change results leaves it as it is;
one that changes results on purpose rewrites it with --write, and the
diff names the effects and scenarios it changed. The fixture follows the
built catalogue, so an asset rebuild that changes an effect rewrites it
too.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { particleTickDigests, DIGEST_PATH, SCENARIOS } from "../tests/helpers/particle-tick-digest.mjs";

/*
================
main
================
*/
function main() {
	const started = performance.now();
	const table = {
		generatedBy: "apps/client-next/tools/particle-tick-digest.mjs",
		about: "Do not edit: regenerate with --write. Each value is the " + SCENARIOS.join( ", " ) +
			" digest of the effect's particle graph tick (tests/helpers/particle-tick-digest.mjs).",
		effects: particleTickDigests()
	};
	const text = JSON.stringify( table, null, "\t" ) + "\n";
	const seconds = ((performance.now() - started) / 1000).toFixed( 1 );
	const count = Object.keys( table.effects ).length;
	if ( process.argv.includes( "--write" ) ) {
		fs.writeFileSync( DIGEST_PATH, text );
		console.log( `particle-tick-digest: wrote ${count} effects, ${text.length} bytes, in ${seconds} s` );
		return;
	}
	const current = fs.existsSync( DIGEST_PATH ) ? JSON.parse( fs.readFileSync( DIGEST_PATH, "utf8" ) ).effects : {};
	const changed = Object.keys( { ...current, ...table.effects } ).filter( effect =>
		current[effect] !== table.effects[effect]
	);
	if ( !changed.length ) {
		console.log( `particle-tick-digest: up to date (${count} effects, ${seconds} s)` );
		return;
	}
	console.error( `particle-tick-digest: ${changed.length} effects differ; run with --write after review` );
	for ( const effect of changed.slice( 0, 20 ) ) {
		console.error( `  ${effect}: ${current[effect] ?? "(missing)"} -> ${table.effects[effect] ?? "(missing)"}` );
	}
	process.exit( 1 );
}

if ( import.meta.filename === path.resolve( process.argv[1] ?? "" ) ) main();
