/*
===========================================================================

liveRelease.mjs - the commit live on opensro.online, as the Latest GitHub release

Contributors asked (2026-11-18) how to know which commit on main is in
production. The release host publishes the live pair at
PRODUCTION_URL (opensro-ops routes.caddy serves its public journal). On a
schedule (.github/workflows/live-release.yml) this script reads it and
makes sure the repository's Latest release is that commit:

- no release names the live server commit yet: create
  release-<UTC date of the deploy>-<9-character commit> at it, marked
  Latest;
- one does but another release is Latest (a revert to an earlier
  release): mark the live one Latest again;
- otherwise nothing.

The release publisher tags a confirmed pair itself (opensro-ops
release_publisher.tag_live); this is the automatic net under it, so a
release made any other way, or a tag that failed, is still recorded
within one run. While a release operation is open the pair is in
motion, and nothing is decided.

The decisions are pure functions over plain records; run() is the one
place that talks to the network. The journal is data: only fields that
match their exact shape (40-hex commits, a finite deploy time) are used.

===========================================================================
*/

export const PRODUCTION_URL = "https://opensro.online/releases/production.json";
export const PRODUCTION_FORMAT = "opensro-production-v1";
export const TAG_PREFIX = "release-";
const COMMIT = /^[0-9a-f]{40}$/;
const SHORT = 9;

/*
================
readLivePair

The live pair from the public journal, or a reason it cannot be read.
Returns { server, client, deployedAt } or { skip }.
================
*/
export function readLivePair( production ) {
	if ( !production || production.format !== PRODUCTION_FORMAT ) {
		return { skip: "the production journal has an unknown format" };
	}
	if ( production.operation ) {
		return { skip: "a release operation is open; the live pair is in motion" };
	}
	const server = production.server?.commit;
	const client = production.client?.commit;
	const deployedAt = Number( production.server?.deployedAt );
	if ( !COMMIT.test( String( server ) ) || !COMMIT.test( String( client ) ) || !Number.isFinite( deployedAt ) ) {
		return { skip: "the production journal names no live commit" };
	}
	return { server, client, deployedAt };
}

/*
================
tagName

release-<UTC date of the deploy>-<9-character commit>, the name the
publisher's tag_live gives the same release.
================
*/
export function tagName( commit, deployedAt ) {
	return TAG_PREFIX + new Date( deployedAt * 1000 ).toISOString().slice( 0, 10 ) + "-" + commit.slice( 0, SHORT );
}

/*
================
releaseNotes
================
*/
export function releaseNotes( pair ) {
	const when = new Date( pair.deployedAt * 1000 ).toISOString().slice( 0, 16 ).replace( "T", " " );
	const client = pair.client === pair.server ? "" : "- Browser client commit: `" + pair.client + "`\n";
	return "Live on opensro.online since **" + when + " UTC**.\n\n" +
		"- Game commit: `" + pair.server + "`\n" + client +
		"\nThe release marked **Latest** on this page is the one running in production. " +
		"What changed and what ships next: https://opensro.online/board\n";
}

/*
================
decideLiveRelease

pair: readLivePair's result. tags: the names of the repository's
release-* tags. latestTag: the Latest release's tag name, or null.
Returns { action: "none" | "create" | "mark-latest", tag, reason }.
A release of the live commit is found by its commit suffix, so the
publisher's tag (named on its confirm day) counts as the same release.
================
*/
export function decideLiveRelease( pair, tags, latestTag ) {
	if ( pair.skip ) {
		return { action: "none", reason: pair.skip };
	}
	const suffix = "-" + pair.server.slice( 0, SHORT );
	const existing = tags.filter( ( tag ) => tag.startsWith( TAG_PREFIX ) && tag.endsWith( suffix ) ).sort().pop();
	if ( !existing ) {
		return {
			action: "create",
			tag: tagName( pair.server, pair.deployedAt ),
			reason: "the live commit has no release"
		};
	}
	if ( latestTag !== existing ) {
		return { action: "mark-latest", tag: existing, reason: "the live commit's release is not Latest" };
	}
	return { action: "none", tag: existing, reason: "the Latest release is the live commit" };
}

/*
================
run

The workflow's entry: read the journal, list the release tags and the
Latest release, apply the decision. fetchJson is injectable for tests.
================
*/
export async function run( { github, context, core, fetchJson = defaultFetchJson } ) {
	const { owner, repo } = context.repo;
	const pair = readLivePair( await fetchJson( PRODUCTION_URL ) );
	const refs = pair.skip ?
		[] :
		await github.paginate( github.rest.git.listMatchingRefs, { owner, repo, ref: "tags/" + TAG_PREFIX } );
	const tags = refs.map( ( ref ) => ref.ref.replace( /^refs\/tags\//, "" ) );
	let latestTag = null;
	if ( !pair.skip ) {
		try {
			latestTag = (await github.rest.repos.getLatestRelease( { owner, repo } )).data.tag_name;
		} catch ( error ) {
			if ( error.status !== 404 ) {
				throw error;
			}
		}
	}
	const decision = decideLiveRelease( pair, tags, latestTag );
	if ( decision.action === "create" ) {
		await github.rest.repos.createRelease( {
			owner,
			repo,
			tag_name: decision.tag,
			target_commitish: pair.server,
			name: "Live: " + decision.tag.slice( TAG_PREFIX.length, TAG_PREFIX.length + 10 ) + " (" +
				pair.server.slice( 0, SHORT ) + ")",
			body: releaseNotes( pair ),
			make_latest: "true"
		} );
	} else if ( decision.action === "mark-latest" ) {
		const release = await github.rest.repos.getReleaseByTag( { owner, repo, tag: decision.tag } );
		await github.rest.repos.updateRelease( { owner, repo, release_id: release.data.id, make_latest: "true" } );
	}
	core.info(
		"live release: " + decision.action + (decision.tag ? " " + decision.tag : "") + " (" + decision.reason + ")"
	);
	return decision;
}

/*
================
defaultFetchJson
================
*/
async function defaultFetchJson( url ) {
	const response = await fetch( url, { headers: { accept: "application/json" } } );
	if ( !response.ok ) {
		throw new Error( "production journal answered " + response.status );
	}
	return response.json();
}
