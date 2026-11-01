/*
===========================================================================

ledgerRatchet.mjs - the shrinking per-file ledger behind the ownership gates

A gate that refuses a pattern outside its owner, while code that predates
the owner is migrated away, keeps a ledger of `<path> <count>` lines. The
ledger only shrinks:

- a file not in the ledger that has any occurrence fails the gate;
- a file whose count exceeds its ledger entry fails the gate;
- a ledger entry whose count has decreased (or dropped to zero) fails the
  gate until `--update` lowers or removes the entry;
- a ledger entry whose file no longer exists fails the gate.

check_world_space.mjs (#1279), check_plane_branches.mjs (#1646) and
check_server_citations.mjs (#1701) share it.

===========================================================================
*/

/*
================
parseLedger

The ledger's entries, keyed by path. name labels malformed lines.
================
*/
export function parseLedger( text, name ) {
	const entries = new Map();
	for ( const raw of text.split( /\r?\n/ ) ) {
		const line = raw.trim();
		if ( line.length === 0 || line.startsWith( "#" ) ) {
			continue;
		}
		const match = /^(\S+)\s+(\d+)$/.exec( line );
		if ( !match ) {
			throw new Error( `Malformed ${name} line: ${line}` );
		}
		const file = match[1];
		const count = Number( match[2] );
		if ( count <= 0 || entries.has( file ) ) {
			throw new Error( `Invalid or duplicate ${name} entry: ${line}` );
		}
		entries.set( file, count );
	}
	return entries;
}

/*
================
formatLedger
================
*/
export function formatLedger( header, entries ) {
	const sorted = [ ...entries.entries() ]
		.filter( ( [, count] ) => count > 0 )
		.sort( ( [a], [b] ) => (a < b ? -1 : a > b ? 1 : 0) )
		.map( ( [file, count] ) => `${file} ${count}` );
	return `${[ ...header, ...sorted ].join( "\n" )}\n`;
}

/*
================
evaluateLedgerRatchet

Compares measured per-file counts (`actual` Map<file, count>) and file
existence (`exists` predicate) against `baseline` Map<file, count>.
words names what the gate refuses: { introduce, ledger, updateCommand }.
Returns `{ problems, nextBaseline, loweredCount, removedCount }`.
================
*/
export function evaluateLedgerRatchet( actual, baseline, exists, update, words ) {
	const problems = [];
	const unlisted = [];
	const increased = [];
	const decreased = [];
	const missing = [];
	const nextBaseline = new Map();
	let loweredCount = 0;
	let removedCount = 0;

	for ( const [file, count] of actual.entries() ) {
		if ( count <= 0 ) {
			continue;
		}
		if ( !baseline.has( file ) ) {
			unlisted.push( `\t${file} (${count})` );
			continue;
		}
		const limit = baseline.get( file );
		if ( count > limit ) {
			increased.push( `\t${file} (${count} > baseline ${limit})` );
		}
	}

	for ( const [file, limit] of baseline.entries() ) {
		if ( !exists( file ) ) {
			if ( update ) {
				removedCount++;
			} else {
				missing.push( `\t${file}` );
			}
			continue;
		}
		const count = actual.get( file ) ?? 0;
		if ( count === 0 ) {
			if ( update ) {
				removedCount++;
			} else {
				decreased.push( `\t${file} (0 < baseline ${limit})` );
			}
			continue;
		}
		if ( count < limit ) {
			if ( update ) {
				nextBaseline.set( file, count );
				loweredCount++;
			} else {
				decreased.push( `\t${file} (${count} < baseline ${limit})` );
			}
			continue;
		}
		nextBaseline.set( file, limit );
	}

	if ( unlisted.length > 0 ) {
		problems.push( `${unlisted.length} file(s) introduce ${words.introduce}:\n${unlisted.join( "\n" )}` );
	}
	if ( increased.length > 0 ) {
		problems.push( `${increased.length} file(s) exceed their ${words.ledger} count:\n${increased.join( "\n" )}` );
	}
	if ( decreased.length > 0 ) {
		problems.push(
			`${decreased.length} baseline entr(ies) have fewer occurrences; run \`${words.updateCommand}\`:\n${
				decreased.join( "\n" )
			}`
		);
	}
	if ( missing.length > 0 ) {
		problems.push(
			`${missing.length} baseline entr(ies) name missing files; run \`${words.updateCommand}\`:\n${
				missing.join( "\n" )
			}`
		);
	}

	return { problems, nextBaseline, loweredCount, removedCount };
}
