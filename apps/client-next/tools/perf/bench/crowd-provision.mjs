/*
===========================================================================

crowd-provision.mjs - scratch accounts and characters for a held crowd

Usage:
  node tools/perf/bench/crowd-provision.mjs --fixture FILE.json --count 600
       [--accounts FILE] [--provisioning http://127.0.0.1:8789] [--token FILE]
  node tools/perf/bench/crowd-provision.mjs --accounts FILE --count 640
  node tools/perf/bench/crowd-provision.mjs --accounts FILE --retire

The first form starts an accounts file for the fixture (or continues the
one at --accounts) and brings it to --count created characters
(core/crowd-accounts.mjs). It spends the Agent's real login budget, one
login per 6 s after a burst of 10, so 600 characters take about an hour;
stop it at any time and run it again to continue. Then crowd-host.mjs
--accounts FILE holds the first N of them in about a minute, for as long
as the saved sessions last (12 hours) and with one paced login for each
expired one after that.

--retire disables every account of the file when the crowd is no longer
wanted, and reads each back from the Agent.

===========================================================================
*/
import path from "node:path";
import { existsSync } from "node:fs";
import { mkdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parseOptions } from "../core/report.mjs";
import {
	createAccountsSaver,
	loadAccountsFile,
	newAccountsFile,
	openAgent,
	provisionAccounts,
	retireAccounts
} from "../core/crowd-accounts.mjs";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../../../.." );
const USAGE = "crowd-provision.mjs [--fixture FILE.json] [--count N] [--accounts FILE] [--retire] " +
	"[--provisioning URL] [--token FILE]";

const options = parseOptions( process.argv.slice( 2 ), {
	fixture: "",
	count: 0,
	accounts: path.join( REPO_ROOT, ".state/crowd-host/crowd-accounts.json" ),
	retire: false,
	provisioning: "http://127.0.0.1:8789",
	token: path.join( REPO_ROOT, "apps/server/.state/cluster/agent-provisioning-token" )
}, USAGE );

const save = createAccountsSaver( options.accounts );
const agent = await openAgent( { provisioningUrl: options.provisioning, tokenPath: options.token } );
if ( options.retire ) {
	const file = await loadAccountsFile( options.accounts );
	const { disabled, accounts } = await retireAccounts( { file, agent, save } );
	console.log(
		`[crowd-provision] retired ${disabled} accounts (${accounts} in ${options.accounts}), each read back disabled`
	);
} else {
	if ( !Number.isInteger( options.count ) || options.count < 1 ) {
		throw Error( `--count N is required\nusage: ${USAGE}` );
	}
	let file;
	if ( existsSync( options.accounts ) ) {
		file = await loadAccountsFile( options.accounts );
		if ( options.fixture ) {
			const fixture = JSON.parse( await readFile( options.fixture, "utf8" ) );
			if ( JSON.stringify( fixture ) !== JSON.stringify( file.fixture ) ) {
				throw Error( `${options.accounts} was provisioned for ${file.fixture.id}, not ${fixture.id}` );
			}
		}
		console.log( `[crowd-provision] continuing ${options.accounts} (${file.accounts.length} accounts planned)` );
	} else {
		if ( !options.fixture ) throw Error( `a new accounts file needs --fixture\nusage: ${USAGE}` );
		file = newAccountsFile( JSON.parse( await readFile( options.fixture, "utf8" ) ) );
		await mkdir( path.dirname( options.accounts ), { recursive: true } );
	}
	const started = performance.now();
	const { created, logins } = await provisionAccounts( { file, count: options.count, agent, save } );
	console.log(
		`[crowd-provision] ${options.count} characters ready in ${options.accounts}: ${created} created, ${logins} logins, ${
			((performance.now() - started) / 60000).toFixed( 1 )
		} min`
	);
}
