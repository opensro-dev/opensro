/*
===========================================================================

report_publication_ledger.mjs - which packed assets no build owner claims

	pnpm assets ledger [-- --json]

Read-only. Compares the published pack index with the publication ledger
(build/shared/publicationLedger.mjs) and prints the expected owners with no
record, the records of owners that no longer exist, and the packed assets
no owner claims, by folder. The full build archives such files itself and
the release packager refuses them; this shows them first. Exits 1 when the
report is not empty.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { indexClaimReport } from "./build/shared/publicationLedger.mjs";
import { publicRoot } from "./build/world/paths.mjs";

const MIB = 1048576;
const index = JSON.parse( await readFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "utf8" ) );
const report = await indexClaimReport( index );

if ( process.argv.includes( "--json" ) ) {
	console.log( JSON.stringify( report, null, "\t" ) );
} else {
	console.log( `Owners with a record: ${report.owners.length}` );
	if ( report.missingOwners.length > 0 ) {
		console.log( `No record (run their task): ${report.missingOwners.join( ", " )}` );
	}
	if ( report.retiredOwners.length > 0 ) {
		console.log( `Records of retired owners: ${report.retiredOwners.join( ", " )}` );
	}
	console.log(
		`Packed assets no build owner claims: ${report.files} (${(report.bytes / MIB).toFixed( 1 )} MiB)` +
			report.folders.map( row =>
				`\n  ${row.folder}: ${row.files} file(s), ${(row.bytes / MIB).toFixed( 1 )} MiB`
			)
				.join( "" )
	);
}
process.exitCode = report.files > 0 || report.missingOwners.length > 0 ? 1 : 0;
