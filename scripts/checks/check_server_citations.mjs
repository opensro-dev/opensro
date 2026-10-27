/*
===========================================================================

check_server_citations.mjs - the server's Go never cites the reference server by name

#1701: repo text describes the v1.188 reference server's behaviour in words.
It never names its classes or functions. v1.150 client citations are allowed.
This gate refuses new reference-server class and function names in the
server's Go sources: comments, strings and code alike, tests included. A
file's count may not exceed its entry in server-citations-baseline.txt, a
shrinking ledger (ledgerRatchet.mjs owns its rules).

Addresses in comments and string literals count too: a six-digit image
address (0x-prefixed, zero-padded, or bare with both a digit and a letter
A-F) and the sub_ form. A hex number alone cannot tell a client address
from a server one, so an address on a line that says it is the client's
(the word client, v1.150, or a client class prefix) never counts; every
other address is grandfathered by the ledger and refused when new. The
#1883 lines are counted this way.

The boundary: a name counts only when its prefix is in the table, and the
table holds only prefixes the server defines and the client does not.
Data-global names, and addresses on a client-labelled line, stay review
steps.

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
	"# Server Go files that still cite the reference server by name or address (#1701, #1883).",
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
	[ 15, "bdb6b7773b67e45ada90d5d6e2beabea8d03aa4a82e670509b063ec329e582ae" ],
	// The families the #1701 sweep reworded (quest engine, mission
	// dialogue, tactics, job, flag world, guild, skill manager, world,
	// reference data, item and storage owners), each server-only in both
	// databases.
	[ 11, "7bd0eaa0ab35c1f52528d0b79b83467919a3c2c216209ecc877196633d444f9c" ],
	[ 4, "1222b54efcc71ebe11c5934def1ed178da715a49dcb2cdc6d1da3093aadd228f" ],
	[ 14, "76a4f4519cd6cdea2fb8670ba67711847b7053eaf358593ddc099c2a37c7e283" ],
	[ 10, "9b955dfe6280347ce298e8a695bfe3e7576372a30bd53abd77ee902b72f5ecec" ],
	[ 11, "35f2e258315f0ff8b082376cd70fab732ed56935063ff565498790774453aaf1" ],
	[ 8, "2244e4f45459f9e8569d9f62b4455c4666efff240e4f98903c7cba65886d8d5c" ],
	[ 11, "ac2a4aa0b9eb7b7fc338877d68de4f41c0519e46d6fd0a9fc8b849be3de47563" ],
	[ 10, "559dd4d38b1d6d5f52288548f840957d4cddb2c9f0dcbb325f77571073762a6d" ],
	[ 13, "8418708620dfcefba1df3ca02ab1656eb29a742bf21388ba490ecc30bfc09a38" ],
	[ 10, "2973335e874c49442bc863dd204d4f39d157c40df6c057228d8d383c84f4981a" ],
	[ 8, "088cb577f66cfd7d56a73262f2f1e26a8a70fe9b91a317ef9cb2e6e29948316c" ],
	[ 9, "57535688312cb8a79b467fc5325ca5f1aa9470454e2de66d4c5d231e9dbe1566" ],
	[ 9, "4815b234a0e0b08b764a8749f07c7cf3bea216e20bdd97f4799bacb78c337ae8" ],
	[ 10, "2d93fc2439e4c48074d0893fbc3c4a6564598c9c166efa60f44839e4f6ffe543" ],
	[ 9, "500b11c6fabc168d2114573423ae02566830346b5bb48f05823b296c9d134e41" ],
	[ 11, "51bc1c47f8830a7e553c0acd55261d086f14e55d25639088d19af37a75627c1f" ],
	[ 10, "662d05b5e33f06894dde9d4c7e2e17d2a49bd732d210bf4ab44b559fb4b46f02" ],
	[ 17, "87d310521599869b4fcb547918858a1025fbffdc38fadba52c54591c26a78132" ],
	[ 14, "96f4a1acf40cbf04245c983a5dfe12920dddeb8ce7bbf3ab5e5f1ff1a52d8989" ],
	[ 17, "6e5c3d8c1abedfa988231d2e7ccff70531c46e3625fae35e4908e7712f2c9c35" ],
	[ 9, "833a0cff424a2ab55c351620f65e51e2c252bf376e27e6d0dfe67f46735abd0e" ],
	[ 6, "68b2e460f8efd349abbf369bb1d6b8a8aa70ce22e092cfca89a2e38fea947e81" ],
	[ 8, "49d4ac3ed85cb3c3a7973f82877ecdebc3bda98f178285c0f5301f69ee0e7606" ],
	[ 16, "790db884c38d9ef2c2d34d01d1c167795614e3c77a66e04e0a6b89b3e1d8fa56" ],
	[ 9, "b29147b9311c010da53ad8e3e6772e4247795ae501dcc7bad5a85ab3f7d5a6e1" ],
	[ 11, "a3146aef7038524d03b80a6fba2415a680084eb9cc798d3ac42ec9a3b3aea46f" ],
	[ 17, "4f27e569940ef4882bddcb53599052de5bf13a9478809a504fa627674c497a5c" ],
	[ 14, "8eb685979a631a5bba89aaf67b3d99f411e5e7dd7fda18e2e9d23fd77dbaee51" ],
	[ 15, "33a8bebcfa67807639c7d3cfd387a1463a4f8d13d90901bb615760abff43415f" ],
	[ 11, "e2cceaedc426959d5d5230e39ada8dbd044309a2a9428a25d29f4daa20048d5e" ],
	[ 9, "861c4738533d2bfdc6ca280eea991149c587c514ac1a3353fe2a4c24a76547b9" ],
	[ 12, "dd81131d7805540c485951b70b9d316269a6b76c74357bed99051320c17f6c28" ],
	[ 16, "74a38ca827fc12eb68957ca2b812ff21a4232be1c86367262faffadf146e8631" ],
	[ 16, "2586a2f1f98c1b8726efe344e371a501e65cf4dd609852fd7ebc933b63613445" ],
	[ 13, "61d24604623a3f45ed7e20fcb30373f3681f531f33a7863b9a053e9b7aab987e" ]
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
	introduce: "reference-server class or function names or addresses",
	ledger: LEDGER_NAME,
	updateCommand: "node scripts/checks/check_server_citations.mjs --update"
};

const GO_SOURCE = /^apps\/server\/.*\.go$/;
// A server or client image address: 0x with optional zero padding, a bare
// six-digit form, or a sub_ name. Bare forms need a digit and a letter
// A-F, so decimal numbers and words never count.
const ADDRESS = /\b(?:0[xX]0*([4-9A-Da-d][0-9A-Fa-f]{5})|sub_([0-9A-Fa-f]{6})|([4-9A-Da-d][0-9A-Fa-f]{5}))\b/g;
const CLIENT_LINE = /client|v1\.150|\bCIF|\bCPS|\bCIC|\bCGWnd|\bCGInterface/i;
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
commentAndStringRanges

The [start, end) ranges of a Go file's comments and string, raw string
and rune literals, in source order.
================
*/
export function commentAndStringRanges( text ) {
	const ranges = [];
	let i = 0;
	while ( i < text.length ) {
		const c = text[i];
		const start = i;
		if ( c === "/" && text[i + 1] === "/" ) {
			i = text.indexOf( "\n", i );
			i = i < 0 ? text.length : i;
		} else if ( c === "/" && text[i + 1] === "*" ) {
			i = text.indexOf( "*/", i + 2 );
			i = i < 0 ? text.length : i + 2;
		} else if ( c === "`" ) {
			i = text.indexOf( "`", i + 1 );
			i = i < 0 ? text.length : i + 1;
		} else if ( c === '"' || c === "'" ) {
			i++;
			while ( i < text.length && text[i] !== c && text[i] !== "\n" ) {
				i += text[i] === "\\" ? 2 : 1;
			}
			i++;
		} else {
			i++;
			continue;
		}
		ranges.push( [ start, Math.min( i, text.length ) ] );
	}
	return ranges;
}

/*
================
isAddressToken

A matched address that is not a flag mask: at least two non-zero hex
digits, and, in the bare form, both a decimal digit and a letter.
================
*/
function isAddressToken( match ) {
	const [, prefixed, sub, bare] = match;
	if ( sub ) {
		return true;
	}
	const digits = prefixed ?? bare;
	if ( digits.replace( /0/g, "" ).length < 2 ) {
		return false;
	}
	return prefixed !== undefined || (/[0-9]/.test( bare ) && /[A-Fa-f]/.test( bare ));
}

/*
================
countAddressCitations

Counts the image addresses in a file's comments and string literals,
except those on a line that labels itself the client's.
================
*/
export function countAddressCitations( text ) {
	let count = 0;
	for ( const [start, end] of commentAndStringRanges( text ) ) {
		const body = text.slice( start, end );
		for ( const match of body.matchAll( ADDRESS ) ) {
			if ( !isAddressToken( match ) ) {
				continue;
			}
			const at = start + match.index;
			const lineStart = text.lastIndexOf( "\n", at ) + 1;
			const lineEnd = text.indexOf( "\n", at );
			const line = text.slice( lineStart, lineEnd < 0 ? text.length : lineEnd );
			if ( !CLIENT_LINE.test( line ) ) {
				count++;
			}
		}
	}
	return count;
}

/*
================
countServerCitations

Counts the words in a file that name a reference-server class or function,
plus its server image addresses (countAddressCitations).
================
*/
export function countServerCitations( text, matches = createCitationMatcher() ) {
	let count = 0;
	for ( const word of text.match( WORD ) ?? [] ) {
		if ( matches( word ) ) {
			count++;
		}
	}
	return count + countAddressCitations( text );
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
		`server-citations: ${activeBaseline.size} file(s) (${total} citation(s)) remain in the baseline ledger\n`
	);
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	main();
}
