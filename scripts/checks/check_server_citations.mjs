/*
===========================================================================

check_server_citations.mjs - the server's Go never cites the reference server by name

#1701: repo text describes the v1.188 reference server's behaviour in words.
It never names its classes or functions. v1.150 client citations are allowed.
This gate refuses new reference-server class and function names in the
server's Go sources: comments, strings and code alike, tests included. A
file's count may not exceed its entry in server-citations-baseline.txt, a
shrinking ledger (ledgerRatchet.mjs owns its rules).

Addresses are not checked here. A hex number alone cannot tell a client
address from a server one; that needs the two-database lookup #1701's parts
ran, so it stays a review step.

The gate must not reintroduce the names it refuses, so the server-only
prefixes are kept as (length, SHA-256) digests, not as text. Every
identifier-shaped word in a file is hashed at each listed prefix length,
once per distinct word. A match needs the character after the prefix to
be anything but a lower-case letter, so a prefix never matches inside an
ordinary longer word, and a word with no lower-case letter at all (a
constant or an acronym) never matches: the server's names are all
mixed-case. The few v1.150 client symbols that share a server
prefix (checked against the client database) are exempt by name.

===========================================================================
*/

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { evaluateLedgerRatchet, formatLedger, parseLedger } from "./ledgerRatchet.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const baselinePath = path.join( rebuildRoot, "scripts", "checks", "server-citations-baseline.txt" );
const LEDGER_NAME = "server-citations-baseline.txt";

const BASELINE_HEADER = [
	"# Server Go files that still name reference-server classes or functions (#1701).",
	"# Format: `<path> <count>`. This ledger may only shrink; run",
	"# `node scripts/checks/check_server_citations.mjs --update` after rewording a file."
];

// The reference server's class and function prefixes, as [length, sha256]
// of the prefix text. Each was checked against both Binary Ninja databases:
// the server defines them, and the v1.150 client defines none of them
// beyond CLIENT_SYMBOLS below.
export const SERVER_PREFIX_DIGESTS = [
	[ 5, "9567ff304dbd3d3bcb7e8eae8feb19d4b94154acb64a76246e73d4f58fb4416a" ],
	[ 6, "b4923e8dd1132d400ad1ac1f749b8a4564d169721c267f19b56b2d4d168a183d" ],
	[ 7, "175f6c713ed285a84f85368bbd619a484d7b4ff6c57188b1fdc39e8c0ac76e6b" ],
	[ 13, "836d0e5b34fa59e4e9830a79beb01b809ee37d4d86059d08915399670a9db8fe" ],
	[ 4, "f1c4f5f997fedc18a365b5e74ce218428b5d99cfd53de6470e39ac637ac1852c" ],
	[ 3, "f6a2986e713d30f172d941229ffe410c8350429c4ab73901a55513227ec2726f" ],
	[ 12, "f1138097b81e3b6b096ae05b5a8e21fc614a0b9eb98640134de6414384f0e680" ],
	[ 14, "ad1043efe4954dfd599f272722db3057b861400ad884be77bf909bda36990e88" ],
	[ 11, "868f99b45f1b84bb356f7a2c0e40c89a741d34c882db9acb2e0684aca65fcb5d" ],
	[ 14, "1be91235c1062038089decfd873fdd02f4078df6b5d4842ad8136c336a50eb5a" ],
	[ 13, "024ed78a957f99208a624647f3df62f6354efd7a970092fd9157fc84fc6667fa" ],
	[ 10, "49e829929a9c93d5197564b69b6aca8c735cb8a350e5d7da86b6893659ec5c64" ],
	[ 12, "68a76f4dd3479eb8b0afc7f35a4549c9c30d09f98d2f321809640774f42e00d3" ],
	[ 13, "0e9168c05c70107d11e07d2935a2b3d9c2bef1aed756ba81ffe5e32be0dfca31" ],
	[ 9, "3b5524655e0a7e8ee32a44640b904ffee634f0c1a1cf0591a43b16158bfc362b" ],
	[ 13, "758090c36e909c75376ba2f9f1d590a4198a14b1d33358b6fc333c55d11b9182" ],
	[ 15, "bdb6b7773b67e45ada90d5d6e2beabea8d03aa4a82e670509b063ec329e582ae" ]
];

// v1.150 client symbols that begin with a server prefix. Client citations
// are allowed, so these words never count. Each is a client database name.
export const CLIENT_SYMBOLS = [
	"CGObj_GetRuntimeClass",
	"CSkillManager_CanWithdrawSkillRank",
	"CSiegeFortressData",
	"CTrainingCampData"
];

const RATCHET_WORDS = {
	introduce: "reference-server class or function names",
	ledger: LEDGER_NAME,
	updateCommand: "node scripts/checks/check_server_citations.mjs --update"
};

const GO_SOURCE = /^apps\/server\/.*\.go$/;
const WORD = /[A-Za-z_][A-Za-z0-9_]*/g;
const LOWER = /[a-z]/;

/*
================
createCitationMatcher

Returns word => boolean: whether the word begins with a listed server
prefix. Digests group by prefix length; results are memoized per word.
================
*/
export function createCitationMatcher( digests = SERVER_PREFIX_DIGESTS, clientSymbols = CLIENT_SYMBOLS ) {
	const byLength = new Map();
	for ( const [length, digest] of digests ) {
		if ( !byLength.has( length ) ) {
			byLength.set( length, new Set() );
		}
		byLength.get( length ).add( digest );
	}
	const lengths = [ ...byLength.keys() ].sort( ( a, b ) => a - b );
	const minimum = lengths[0] ?? Infinity;
	const memo = new Map();
	return ( word ) => {
		if ( word.length < minimum ) {
			return false;
		}
		const known = memo.get( word );
		if ( known !== undefined ) {
			return known;
		}
		let matched = false;
		if ( LOWER.test( word ) && !clientSymbols.some( ( symbol ) => word.startsWith( symbol ) ) ) {
			for ( const length of lengths ) {
				if ( word.length < length ) {
					break;
				}
				if ( length < word.length && LOWER.test( word[length] ) ) {
					continue;
				}
				const digest = createHash( "sha256" ).update( word.slice( 0, length ) ).digest( "hex" );
				if ( byLength.get( length ).has( digest ) ) {
					matched = true;
					break;
				}
			}
		}
		memo.set( word, matched );
		return matched;
	};
}

/*
================
countServerCitations

Counts the words in a file that name a reference-server class or function.
================
*/
export function countServerCitations( text, matches = createCitationMatcher() ) {
	let count = 0;
	for ( const word of text.match( WORD ) ?? [] ) {
		if ( matches( word ) ) {
			count++;
		}
	}
	return count;
}

/*
================
parseBaseline
================
*/
export function parseBaseline( text ) {
	return parseLedger( text, LEDGER_NAME );
}

/*
================
formatBaseline
================
*/
export function formatBaseline( entries ) {
	return formatLedger( BASELINE_HEADER, entries );
}

/*
================
evaluateCitationRatchet
================
*/
export function evaluateCitationRatchet( actual, baseline, exists, update = false ) {
	return evaluateLedgerRatchet( actual, baseline, exists, update, RATCHET_WORDS );
}

/*
================
trackedServerSources
================
*/
function trackedServerSources() {
	return execFileSync( "git", [ "ls-files", "-z", "--", "apps/server" ], { cwd: rebuildRoot, encoding: "utf8" } )
		.split( "\0" )
		.filter( ( file ) => GO_SOURCE.test( file ) );
}

/*
================
main
================
*/
function main() {
	const update = process.argv.includes( "--update" );
	const baseline = existsSync( baselinePath ) ? parseBaseline( readFileSync( baselinePath, "utf8" ) ) : new Map();
	const matches = createCitationMatcher();
	const actual = new Map();

	for ( const file of trackedServerSources() ) {
		const fullPath = path.join( rebuildRoot, file );
		if ( !existsSync( fullPath ) ) {
			continue;
		}
		const count = countServerCitations( readFileSync( fullPath, "utf8" ), matches );
		if ( count > 0 ) {
			actual.set( file, count );
		}
	}

	const { problems, nextBaseline, loweredCount, removedCount } = evaluateCitationRatchet(
		actual,
		baseline,
		( file ) => existsSync( path.join( rebuildRoot, file ) ),
		update
	);

	if ( update && problems.length === 0 ) {
		writeFileSync( baselinePath, formatBaseline( nextBaseline ) );
		process.stdout.write(
			`server-citations-baseline: ${loweredCount} lowered, ${removedCount} removed, ${nextBaseline.size} remain\n`
		);
	}

	if ( problems.length > 0 ) {
		process.stderr.write( `${problems.join( "\n\n" )}\n` );
		process.exit( 1 );
	}

	const activeBaseline = update ? nextBaseline : baseline;
	const total = [ ...activeBaseline.values() ].reduce( ( sum, n ) => sum + n, 0 );
	process.stdout.write(
		`server-citations: ${activeBaseline.size} file(s) (${total} name(s)) remain in the baseline ledger\n`
	);
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main();
}
